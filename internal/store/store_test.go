package store

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil)) //nolint:gochecknoglobals // test logger

func openTemp(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scp.db")
	db, err := Open(t.Context(), path, quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func repo(key string, id int64) Repo {
	return Repo{Key: snapshot.RepoKey(key), ID: snapshot.RepoID{Source: "gh", GitHubID: id}}
}

func TestRoundTrip(t *testing.T) {
	db, path := openTemp(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	src := Item{Key: "catalog.reconcile/gh", Kind: "catalog.reconcile", State: snapshot.StateOK, FetchedAt: at, CheckedAt: at,
		NextDue: at.Add(5 * time.Minute), ContentSHA: "abc", Payload: []byte(`{"schema":1}`)}
	if err := db.Apply(ctx, Change{Item: src, Source: "gh", Catalogue: []Repo{repo("cantara/a", 1), repo("cantara/b", 2)}}); err != nil {
		t.Fatal(err)
	}
	perRepo := Item{Key: "commits.refresh/cantara/a", Kind: "commits.refresh", Repo: "cantara/a", State: snapshot.StateError,
		CheckedAt: at, Attempts: 3, ErrCode: "transient"}
	if err := db.Apply(ctx, Change{Item: perRepo}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := Open(ctx, path, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db2.Close() }()
	repos, items, err := db2.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]Repo{repo("cantara/a", 1), repo("cantara/b", 2)}, repos); diff != "" {
		t.Errorf("repos (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]Item{src, perRepo}, items); diff != "" {
		t.Errorf("items (-want +got):\n%s", diff)
	}
}

func TestCatalogueReplace(t *testing.T) {
	db, _ := openTemp(t)
	ctx := t.Context()
	src := Item{Key: "catalog.reconcile/gh", Kind: "catalog.reconcile", State: snapshot.StateOK}
	apply := func(c Change) {
		t.Helper()
		if err := db.Apply(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	apply(Change{Item: src, Source: "gh", Catalogue: []Repo{repo("cantara/a", 1), repo("cantara/b", 2), repo("cantara/c", 3)}})
	for _, k := range []string{"cantara/a", "cantara/b", "cantara/c"} {
		apply(Change{Item: Item{Key: "commits.refresh/" + k, Kind: "commits.refresh", Repo: snapshot.RepoKey(k), State: snapshot.StateOK}})
	}
	// 1 is renamed, 2 is gone, 3 stays, 4 is new and takes the address of 2.
	apply(Change{Item: src, Source: "gh", Catalogue: []Repo{repo("cantara/a2", 1), repo("cantara/c", 3), repo("cantara/b", 4)}})

	repos, items, err := db.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]Repo{repo("cantara/a2", 1), repo("cantara/b", 4), repo("cantara/c", 3)}, repos); diff != "" {
		t.Errorf("repos (-want +got):\n%s", diff)
	}
	var keys []string
	for _, it := range items {
		keys = append(keys, it.Key)
	}
	if diff := cmp.Diff([]string{"catalog.reconcile/gh", "commits.refresh/cantara/c"}, keys); diff != "" {
		t.Errorf("items after rename and removal (-want +got):\n%s", diff)
	}

	// A late result for a removed repository writes nothing.
	apply(Change{Item: Item{Key: "commits.refresh/cantara/gone", Kind: "commits.refresh", Repo: "cantara/gone", State: snapshot.StateOK}})
	if _, items, _ := db.Load(ctx); len(items) != 2 {
		t.Errorf("late result was written: %d items", len(items))
	}
}

func TestRetainSources(t *testing.T) {
	db, _ := openTemp(t)
	ctx := t.Context()
	for _, src := range []string{"gh", "old"} {
		r := Repo{Key: snapshot.RepoKey(src + "/x"), ID: snapshot.RepoID{Source: src, GitHubID: 1}}
		if err := db.Apply(ctx, Change{Item: Item{Key: "catalog.reconcile/" + src, Kind: "catalog.reconcile", State: snapshot.StateOK},
			Source: src, Catalogue: []Repo{r}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Apply(ctx, Change{Item: Item{Key: "retired.kind/gh", Kind: "retired.kind", State: snapshot.StateOK}}); err != nil {
		t.Fatal(err)
	}
	if err := db.RetainSources(ctx, []string{"gh"}, []string{"catalog.reconcile"}); err != nil {
		t.Fatal(err)
	}
	repos, items, err := db.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].ID.Source != "gh" || len(items) != 1 || items[0].Key != "catalog.reconcile/gh" {
		t.Errorf("after retain: repos %v items %v", repos, items)
	}
}

func TestQuarantine(t *testing.T) {
	ctx := context.Background()
	t.Run("corrupt file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "scp.db")
		if err := os.WriteFile(path, []byte("this is not a database, but it is long enough to look like one. "), 0o600); err != nil {
			t.Fatal(err)
		}
		db, err := Open(ctx, path, quiet)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		if moved, _ := filepath.Glob(path + ".quarantine-*"); len(moved) != 1 {
			t.Errorf("quarantined files: %v", moved)
		}
	})
	t.Run("newer schema", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "scp.db")
		raw, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.ExecContext(ctx, "PRAGMA user_version = 99"); err != nil {
			t.Fatal(err)
		}
		_ = raw.Close()
		db, err := Open(ctx, path, quiet)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		var v int
		if err := db.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil || v != 1 {
			t.Errorf("user_version %d %v, want a fresh store at 1", v, err)
		}
	})
	t.Run("keeps two", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "scp.db")
		base := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
		for i := range 4 {
			if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := quarantine(path, base.Add(time.Duration(i)*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		if moved, _ := filepath.Glob(path + ".quarantine-*"); len(moved) != 2 {
			t.Errorf("kept %v, want the newest two", moved)
		}
	})
}
