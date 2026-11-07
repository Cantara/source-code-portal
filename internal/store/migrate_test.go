package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

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

// schemaOne writes a store as the first release left it: migration 0001 only, with a catalogue listing and two items.
func schemaOne(t *testing.T, path string) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	body, err := migrations.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	listing := `{"repos":[{"repo":{"id":{"Source":"gh","GitHubID":1},"owner":"acme","name":"open","visibility":"public"}},` +
		`{"repo":{"id":{"Source":"gh","GitHubID":2},"owner":"acme","name":"secret","visibility":"private"}},` +
		`{"repo":{"id":{"Source":"gh","GitHubID":3},"owner":"acme","name":"inner","visibility":"internal"}}]}`
	for _, q := range []string{string(body),
		"INSERT INTO repo (key, source, github_id) VALUES ('acme/open','gh',1),('acme/secret','gh',2),('acme/inner','gh',3),('x/other','nolisting',9)",
		"INSERT INTO item (key, kind, repo_key, status, fetched_at, checked_at, next_due_at, attempts, content_sha, payload) VALUES " +
			"('catalog.reconcile/gh','catalog.reconcile',NULL,'ok',100,100,200,0,'abc',CAST('" + listing + "' AS BLOB))," +
			"('commits/acme/secret','commits','acme/secret','ok',101,102,300,2,'def',CAST('{\"commits\":[1,2,3]}' AS BLOB))",
		"PRAGMA user_version = 1"} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q[:40], err)
		}
	}
}

// fingerprint hashes every repository and item of a store, payload bytes included, apart from the visibility the migration adds.
func fingerprint(t *testing.T, s *DB) string {
	t.Helper()
	repos, items, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for _, r := range repos {
		fmt.Fprintf(h, "R %s %s %d\n", r.Key, r.ID.Source, r.ID.GitHubID)
	}
	for _, it := range items {
		fmt.Fprintf(h, "I %s %s %s %s %d %d %d %d %s %s\n", it.Key, it.Kind, it.Repo, it.State, it.FetchedAt.Unix(), it.CheckedAt.Unix(),
			it.NextDue.Unix(), it.Attempts, it.ErrCode, it.ContentSHA)
		h.Write(it.Payload)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestMigrationKeepsTheData: a store of the first release opens, keeps every repository, item and payload byte, learns each
// repository's visibility from its source's listing, and leaves a copy of itself as it was.
func TestMigrationKeepsTheData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "scp.db")
	schemaOne(t, path)

	// The fingerprint of the old store, read without migrating it (the same Load queries minus visibility are what 0001 offers).
	raw, _ := sql.Open("sqlite", path)
	var rows, items int
	_ = raw.QueryRow("SELECT count(*) FROM repo").Scan(&rows)
	_ = raw.QueryRow("SELECT count(*) FROM item").Scan(&items)
	var beforeSum string
	{
		h := sha256.New()
		r1, _ := raw.Query("SELECT key, source, github_id FROM repo ORDER BY key")
		for r1.Next() {
			var k, s string
			var id int64
			_ = r1.Scan(&k, &s, &id)
			fmt.Fprintf(h, "R %s %s %d\n", k, s, id)
		}
		_ = r1.Close()
		r2, _ := raw.Query("SELECT key, kind, coalesce(repo_key,''), status, coalesce(fetched_at,0), coalesce(checked_at,0), coalesce(next_due_at,0), attempts, coalesce(error,''), coalesce(content_sha,''), payload FROM item ORDER BY key")
		for r2.Next() {
			var k, kind, rk, st, e, sha string
			var f, c, n int64
			var a int
			var p []byte
			_ = r2.Scan(&k, &kind, &rk, &st, &f, &c, &n, &a, &e, &sha, &p)
			fmt.Fprintf(h, "I %s %s %s %s %d %d %d %d %s %s\n", k, kind, rk, st, f, c, n, a, e, sha)
			h.Write(p)
		}
		_ = r2.Close()
		beforeSum = hex.EncodeToString(h.Sum(nil))
	}
	_ = raw.Close()

	db, err := Open(ctx, path, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if got := fingerprint(t, db); got != beforeSum {
		t.Errorf("the migration changed what the store holds:\n before %s\n after  %s", beforeSum, got)
	}
	repos, _, _ := db.Load(ctx)
	want := map[snapshot.RepoKey]string{"acme/open": "public", "acme/secret": "private", "acme/inner": "internal", "x/other": ""}
	for _, r := range repos {
		if r.Visibility != want[r.Key] {
			t.Errorf("%s: visibility %q, want %q (learned from its source's listing; unknown without one)", r.Key, r.Visibility, want[r.Key])
		}
	}
	var v int
	_ = db.db.QueryRow("PRAGMA user_version").Scan(&v)
	if v != latestSchema(t) {
		t.Errorf("user_version %d", v)
	}

	// The copy is the store as it was: schema 1, no visibility, the same rows.
	cp, err := sql.Open("sqlite", path+".pre-0002")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cp.Close() }()
	var cv, n int
	_ = cp.QueryRow("PRAGMA user_version").Scan(&cv)
	_ = cp.QueryRow("SELECT count(*) FROM item").Scan(&n)
	if cv != 1 || n != items {
		t.Errorf("copy: user_version %d, %d items; want 1 and %d", cv, n, items)
	}
	if err := cp.QueryRow("SELECT visibility FROM repo LIMIT 1").Scan(new(string)); err == nil {
		t.Error("the copy already has the new column")
	}
	if fi, err := os.Stat(path + ".pre-0002"); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("copy mode %v %v, want 0600", fi, err)
	}
}

// TestNoCopyOfAnEmptyStoreOrTwice: a new store has nothing to keep, and a copy that exists is not replaced.
func TestNoCopyOfAnEmptyStoreOrTwice(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	fresh := filepath.Join(dir, "fresh.db")
	db, err := Open(ctx, fresh, quiet)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if left, _ := filepath.Glob(fresh + ".pre-*"); len(left) != 0 {
		t.Errorf("a new store was copied: %v", left)
	}

	path := filepath.Join(dir, "scp.db")
	schemaOne(t, path)
	copyPath := path + ".pre-0002"
	if err := os.WriteFile(copyPath, []byte("an operator's earlier copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, path, quiet)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if b, _ := os.ReadFile(copyPath); string(b) != "an operator's earlier copy" {
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
