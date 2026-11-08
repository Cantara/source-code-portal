package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

func modeOf(t *testing.T, db *DB) string {
	t.Helper()
	v, err := db.Meta(t.Context(), "mode")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// A store is made for one mode on its first use, and the other mode is refused for good, before anything is read or written.
func TestAStoreBelongsToOneMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scp.db")
	db, err := OpenAs(t.Context(), path, Personal, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if got := modeOf(t, db); got != "personal" {
		t.Errorf("mode %q after the first open", got)
	}
	_ = db.Close()
	for i := range 2 { // again, and again: its own mode opens as often as asked
		db, err = OpenAs(t.Context(), path, Personal, quiet)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		_ = db.Close()
	}
	before, _ := os.ReadFile(path)
	_, err = OpenAs(t.Context(), path, Shared, quiet)
	var me *ModeError
	if !errors.As(err, &me) || me.Have != Personal || me.Want != Shared {
		t.Fatalf("the other mode opened it: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if e.Name() != "scp.db" && e.Name() != "scp.db-wal" && e.Name() != "scp.db-shm" {
			t.Errorf("the refusal left %s behind (a quarantine or a copy)", e.Name())
		}
	}
	after, _ := os.ReadFile(path)
	if len(before) == 0 || string(before) != string(after) {
		t.Error("the refused open changed the file")
	}
	if _, err := OpenAs(t.Context(), path, "other", quiet); err == nil {
		t.Error("a mode nobody defined was accepted")
	}
}

// A store from before the mode was written is taken by the mode its contents fit, and by no other.
func TestAStoreWithoutAModeIsJudgedByWhatItHolds(t *testing.T) {
	ctx := t.Context()
	site := func() string { // a shared site's store: a catalogue, no credential, no mode
		path := filepath.Join(t.TempDir(), "scp.db")
		db, err := Open(ctx, path, quiet)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Apply(ctx, Change{Item: Item{Key: "k", Kind: "catalog.reconcile", State: snapshot.StateOK}, Source: "gh", Catalogue: []Repo{repo("acme/a", 1)}}); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		return path
	}
	person := func() string { // one person's: a sealed credential, and a catalogue
		path := site()
		db, err := Open(ctx, path, quiet)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.PutCredential(ctx, Credential{ID: "github", Source: "https://github.com", Kind: "token", Sealed: []byte("x")}); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		return path
	}
	var me *ModeError
	if _, err := OpenAs(ctx, site(), Personal, quiet); !errors.As(err, &me) || me.Have != Shared {
		t.Errorf("scp personal opened a shared site's store: %v", err)
	}
	if _, err := OpenAs(ctx, person(), Shared, quiet); !errors.As(err, &me) || me.Have != Personal {
		t.Errorf("scp serve opened a personal store: %v", err)
	}
	if db, err := OpenAs(ctx, site(), Shared, quiet); err != nil {
		t.Errorf("a site's store from before the mode was refused: %v", err)
	} else {
		if got := modeOf(t, db); got != "shared" {
			t.Errorf("mode %q", got)
		}
		_ = db.Close()
	}
	if db, err := OpenAs(ctx, person(), Personal, quiet); err != nil {
		t.Errorf("a personal store from before the mode was refused: %v", err)
	} else {
		_ = db.Close()
	}
}

// The refusal comes before the migration: a store of the other mode on an older schema is neither migrated nor copied nor moved aside.
func TestARefusedStoreIsNotMigrated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scp.db")
	schemaOne(t, path) // the first release's store: a catalogue, no credential table, no mode
	before, _ := os.ReadFile(path)
	var me *ModeError
	if _, err := OpenAs(t.Context(), path, Personal, quiet); !errors.As(err, &me) {
		t.Fatalf("%v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("the file changed")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("the refusal left files behind: %d", len(entries))
	}
	// Taken by the mode it fits, it migrates, with the copy, and keeps its mode.
	db, err := OpenAs(t.Context(), path, Shared, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if got := modeOf(t, db); got != "shared" {
		t.Errorf("mode %q", got)
	}
	_ = db.Close()
}
