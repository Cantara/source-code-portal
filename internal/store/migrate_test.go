package store

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

// latestSchema is the number of the newest embedded migration.
func latestSchema(t *testing.T) int {
	t.Helper()
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil || len(names) == 0 {
		t.Fatal(err)
	}
	return len(names)
}

// generationOne is the schema the deployed releases (v2.0.0 to v2.2.9) made: one migration, no application mark, no visibility, no
// credential or document table. SC1 replaced it with one baseline.
const generationOne = `-- The store is a rebuildable cache of the last good state. Times are UTC unix seconds.
CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;

-- Catalogued repositories: identity and address. Items of a repository go with it.
CREATE TABLE repo (
  key       TEXT PRIMARY KEY,          -- lower-case owner/name
  source    TEXT NOT NULL,
  github_id INTEGER NOT NULL,
  UNIQUE (source, github_id)
) STRICT;

-- One row per sync job key: its last outcome, schedule and payload.
CREATE TABLE item (
  key         TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  repo_key    TEXT REFERENCES repo (key) ON DELETE CASCADE, -- NULL for source jobs
  status      TEXT NOT NULL CHECK (status IN ('never', 'ok', 'not_found', 'too_large', 'no_content', 'unsupported', 'error')),
  fetched_at  INTEGER,
  checked_at  INTEGER,
  next_due_at INTEGER,
  attempts    INTEGER NOT NULL DEFAULT 0,
  content_sha TEXT,
  error       TEXT,
  payload     BLOB
) STRICT;
`

// earlierStore writes a store as a deployed release left it: generationOne at user_version 1, with a catalogue listing and two items.
func earlierStore(t *testing.T, path string) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	listing := `{"repos":[{"repo":{"id":{"Source":"gh","GitHubID":1},"owner":"acme","name":"open","visibility":"public"}}]}`
	for _, q := range []string{generationOne,
		"INSERT INTO repo (key, source, github_id) VALUES ('acme/open','gh',1),('x/other','nolisting',9)",
		"INSERT INTO item (key, kind, repo_key, status, fetched_at, checked_at, next_due_at, attempts, content_sha, payload) VALUES " +
			"('catalog.reconcile/gh','catalog.reconcile',NULL,'ok',100,100,200,0,'abc',CAST('" + listing + "' AS BLOB))," +
			"('commits/acme/open','commits','acme/open','ok',101,102,300,2,'def',CAST('{\"commits\":[1,2,3]}' AS BLOB))",
		"PRAGMA user_version = 1"} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q[:40], err)
		}
	}
}

func pragma(t *testing.T, path, name string) int {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var v int
	if err := raw.QueryRow("PRAGMA " + name).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// A new store is the baseline: its mark, its one version and every table, with nothing copied.
func TestAFreshStoreIsTheBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scp.db")
	db, err := Open(t.Context(), path, quiet)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"SELECT visibility FROM repo", "SELECT sealed FROM credential", "SELECT revision FROM document", "SELECT value FROM meta"} {
		if _, err := db.db.Exec(q); err != nil {
			t.Errorf("%s: %v", q, err)
		}
	}
	_ = db.Close()
	if got := pragma(t, path, "application_id"); got != applicationID {
		t.Errorf("application_id %#x, want %#x", got, applicationID)
	}
	if got, want := pragma(t, path, "user_version"), latestSchema(t); got != want {
		t.Errorf("user_version %d, want %d", got, want)
	}
	if left, _ := filepath.Glob(path + ".*"); len(left) != 0 {
		t.Errorf("a new store left files behind: %v", left)
	}
}

// TestAnEarlierGenerationIsMovedAside: a store of the deployed releases is not migrated. It is renamed (the way back, with its data as it
// was), and the portal starts on a new baseline store that the catalogue is read into again.
func TestAnEarlierGenerationIsMovedAside(t *testing.T) {
	ctx := t.Context()
	for _, mode := range []Mode{"", Shared} {
		t.Run("mode "+string(mode), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "scp.db")
			earlierStore(t, path)
			var db *DB
			var err error
			if mode == "" {
				db, err = Open(ctx, path, quiet)
			} else {
				db, err = OpenAs(ctx, path, mode, quiet)
			}
			if err != nil {
				t.Fatal(err)
			}
			repos, items, _ := db.Load(ctx)
			_ = db.Close()
			if len(repos) != 0 || len(items) != 0 {
				t.Errorf("the new store holds %d repositories and %d items of the old one", len(repos), len(items))
			}
			if got := pragma(t, path, "application_id"); got != applicationID {
				t.Errorf("the new store's application_id %#x", got)
			}
			aside, _ := filepath.Glob(path + ".quarantine-*")
			if len(aside) != 1 {
				t.Fatalf("moved aside: %v", aside)
			}
			// The old store is whole where it was put: renaming it back is the way back.
			if v := pragma(t, aside[0], "user_version"); v != 1 {
				t.Errorf("the old store's user_version %d", v)
			}
			raw, err := sql.Open("sqlite", "file:"+aside[0]+"?mode=ro")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = raw.Close() }()
			var n int
			if err := raw.QueryRow("SELECT count(*) FROM item").Scan(&n); err != nil || n != 2 {
				t.Errorf("the old store holds %d items (%v), want its 2", n, err)
			}
		})
	}
}

// migrations2 is the embedded baseline and one more migration, to test the runner as the next change will use it.
func migrations2(t *testing.T) fs.FS {
	t.Helper()
	base, err := migrations.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{
		"migrations/0001_init.sql": {Data: base},
		"migrations/0002_note.sql": {Data: []byte("ALTER TABLE meta ADD COLUMN note TEXT;")},
	}
}

// TestMigrationsAreAdditiveWithACopy: a store that holds data is copied before a later migration (once, 0600) and keeps every row; a new
// store is not copied; a copy that exists is not replaced; a store newer than the binary is refused.
func TestMigrationsAreAdditiveWithACopy(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "scp.db")
	db, err := Open(ctx, path, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetMeta(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}
	files := migrations2(t)
	if err := migrate(ctx, db.db, path, quiet, files); err != nil {
		t.Fatal(err)
	}
	if got := pragma(t, path, "user_version"); got != 2 {
		t.Errorf("user_version %d, want 2", got)
	}
	if v, _ := db.Meta(ctx, "k"); v != "v" {
		t.Errorf("a row was lost: %q", v)
	}
	fi, err := os.Stat(path + ".pre-0002")
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("copy %v %v, want a 0600 file", fi, err)
	}
	if got := pragma(t, path+".pre-0002", "user_version"); got != 1 {
		t.Errorf("the copy is of user_version %d, want the store as it was (1)", got)
	}
	_ = db.Close()

	// A new store is not copied, whatever it migrates through.
	fresh := filepath.Join(dir, "fresh.db")
	raw, err := sql.Open("sqlite", fresh)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, raw, fresh, quiet, files); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	if left, _ := filepath.Glob(fresh + ".pre-*"); len(left) != 0 {
		t.Errorf("a new store was copied: %v", left)
	}

	// A copy that exists is kept, not replaced.
	again := filepath.Join(dir, "again.db")
	db, err = Open(ctx, again, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.SetMeta(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(again+".pre-0002", []byte("an operator's earlier copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db.db, again, quiet, files); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(again + ".pre-0002"); string(b) != "an operator's earlier copy" {
		t.Error("an existing copy was replaced")
	}
}

// TestVisibilityFollowsTheListing: a listing that says writes it; one that does not say keeps what is known.
func TestVisibilityFollowsTheListing(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "scp.db"), quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	list := func(vis string) {
		t.Helper()
		err := db.Apply(ctx, Change{Source: "gh", Catalogue: []Repo{{Key: "acme/a", ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Visibility: vis}},
			Item: Item{Key: "catalog.reconcile/gh", Kind: "catalog.reconcile", State: snapshot.StateOK}})
		if err != nil {
			t.Fatal(err)
		}
	}
	got := func() string { repos, _, _ := db.Load(ctx); return repos[0].Visibility }
	list("public")
	if got() != "public" {
		t.Fatalf("visibility %q", got())
	}
	if err := db.Apply(ctx, Change{Item: Item{Key: "commits/acme/a", Kind: "commits", Repo: "acme/a", State: snapshot.StateOK}}); err != nil {
		t.Fatal(err)
	}
	list("private") // turned private: the repository and its items stay, the visibility moves
	if _, items, _ := db.Load(ctx); got() != "private" || len(items) != 2 {
		t.Errorf("after turning private: %q, %d items", got(), len(items))
	}
	list("") // a listing that does not say
	if got() != "private" {
		t.Errorf("an unsaid visibility replaced what was known: %q", got())
	}
}

// TestAddressHeldByTwoSources: two sources listing one address no longer take it from each other. The source whose id sorts first owns
// it, whichever listed first; the other is not stored; the owner's items are never deleted by the other's listing.
func TestAddressHeldByTwoSources(t *testing.T) {
	ctx := context.Background()
	for _, order := range [][]string{{"a-first", "b-second"}, {"b-second", "a-first"}} {
		t.Run(order[0]+" lists first", func(t *testing.T) {
			db, err := Open(ctx, filepath.Join(t.TempDir(), "scp.db"), quiet)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			id := map[string]int64{"a-first": 1, "b-second": 7}
			for round := range 3 {
				for _, src := range order {
					err := db.Apply(ctx, Change{Source: src, Catalogue: []Repo{{Key: "acme/core", ID: snapshot.RepoID{Source: src, GitHubID: id[src]}}},
						Item: Item{Key: "catalog.reconcile/" + src, Kind: "catalog.reconcile", State: snapshot.StateOK}})
					if err != nil {
						t.Fatal(err)
					}
					if round == 0 && src == "a-first" || round > 0 {
						// the engine fetches the owner's commits once the address is the owner's
						_ = db.Apply(ctx, Change{Item: Item{Key: "commits/acme/core", Kind: "commits", Repo: "acme/core", State: snapshot.StateOK, Payload: []byte("a-first's")}})
					}
				}
			}
			repos, items, _ := db.Load(ctx)
			if len(repos) != 1 || repos[0].ID.Source != "a-first" {
				t.Fatalf("repositories %+v, want acme/core owned by a-first", repos)
			}
			kept := 0
			for _, it := range items {
				if it.Kind == "commits" {
					kept++
				}
			}
			if kept != 1 {
				t.Errorf("%d commits items, want the owner's one kept through every listing", kept)
			}
		})
	}
}
