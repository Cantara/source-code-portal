package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestDocumentsAreRevisionedAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scp.db")
	ctx := t.Context()
	db, err := Open(ctx, path, quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, _, ok, err := db.Document(ctx, "layout-dashboard"); err != nil || ok {
		t.Fatalf("a new store holds no document: %v %v", ok, err)
	}
	if err := db.PutDocument(ctx, "layout-dashboard", []byte(`{"v":1}`), "r1", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.PutDocument(ctx, "layout-dashboard", []byte(`{"v":2}`), "r2", ""); !errors.Is(err, ErrRevision) {
		t.Fatalf("a save that did not see the first one: %v", err)
	}
	if err := db.PutDocument(ctx, "layout-dashboard", []byte(`{"v":2}`), "r2", "r1"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteDocument(ctx, "layout-dashboard", "r1"); !errors.Is(err, ErrRevision) {
		t.Fatalf("a removal on a stale revision: %v", err)
	}
	// Reopened without Close, as after a kill: what was acknowledged is there.
	again, err := Open(ctx, path, quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	body, rev, ok, err := again.Document(ctx, "layout-dashboard")
	if err != nil || !ok || string(body) != `{"v":2}` || rev != "r2" {
		t.Fatalf("after reopening: %q %q %v %v", body, rev, ok, err)
	}
	if err := again.DeleteDocument(ctx, "layout-dashboard", "r2"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := again.Document(ctx, "layout-dashboard"); ok {
		t.Error("the document was not removed")
	}
}
