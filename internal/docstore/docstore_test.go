package docstore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil)) //nolint:gochecknoglobals // test logger

type note struct{ Text string }

func parse(b []byte) (note, error) {
	var n note
	return n, json.Unmarshal(b, &n)
}

func open(t *testing.T, path string) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), path, quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSaveSurvivesTheProcessDying(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scp.db")
	ctx := t.Context()
	d := Open(ctx, open(t, path), "n", note{"built-in"}, parse, quiet)
	if st := d.Get(); st.Stored || st.Value.Text != "built-in" || st.Revision != "" {
		t.Fatalf("nothing stored yet: %+v", st)
	}
	rev, err := d.Save(ctx, []byte(`{"Text":"one"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Save(ctx, []byte(`{"Text":"two"}`), ""); !errors.Is(err, ErrConflict) {
		t.Errorf("a save that did not see the first: %v", err)
	}
	// Another handle on the file, never closing the first: the process died after the save was acknowledged.
	again := Open(ctx, open(t, path), "n", note{"built-in"}, parse, quiet)
	if st := again.Get(); !st.Stored || st.Value.Text != "one" || st.Revision != rev {
		t.Errorf("after the restart: %+v", st)
	}
	if err := again.Remove(ctx, "stale"); !errors.Is(err, ErrConflict) {
		t.Errorf("removal on a stale revision: %v", err)
	}
	if err := again.Remove(ctx, rev); err != nil || again.Get().Stored {
		t.Errorf("removal: %v %+v", err, again.Get())
	}
}

func TestABrokenDocumentKeepsTheFallbackAndCanBeReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scp.db")
	ctx := t.Context()
	db := open(t, path)
	if err := db.PutDocument(ctx, "n", []byte(`{broken`), "r0", ""); err != nil {
		t.Fatal(err)
	}
	d := Open(ctx, db, "n", note{"built-in"}, parse, quiet)
	if st := d.Get(); st.Problem == "" || st.Value.Text != "built-in" || st.Revision != "r0" {
		t.Fatalf("broken: %+v", st)
	}
	if _, err := d.Save(ctx, []byte(`{"Text":"fixed"}`), "r0"); err != nil || d.Get().Problem != "" {
		t.Errorf("saving over it: %v %+v", err, d.Get())
	}
}

func TestNoStoreIsReadOnly(t *testing.T) {
	d := Open[note](t.Context(), nil, "n", note{"x"}, parse, quiet)
	if _, err := d.Save(t.Context(), []byte(`{}`), ""); !errors.Is(err, ErrReadOnly) || d.Writable() || d.Get().Value.Text != "x" {
		t.Errorf("read-only: %v", err)
	}
}

func TestImportTakesFilesOnceAndNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{"Text":"from a file"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.json"), []byte(`{"Text":"b file"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db := open(t, filepath.Join(t.TempDir(), "scp.db"))
	if err := db.PutDocument(ctx, "b", []byte(`{"Text":"b stored"}`), "rb", ""); err != nil {
		t.Fatal(err)
	}
	done, err := Import(ctx, db, dir, []string{"a", "b", "c"})
	if err != nil || len(done) != 1 || done[0] != "a" {
		t.Fatalf("imported %v, %v", done, err)
	}
	if a := Open(ctx, db, "a", note{}, parse, quiet).Get(); a.Value.Text != "from a file" || !a.Stored {
		t.Errorf("a: %+v", a)
	}
	if b := Open(ctx, db, "b", note{}, parse, quiet).Get(); b.Value.Text != "b stored" {
		t.Errorf("b was overwritten: %+v", b)
	}
	if done, _ = Import(ctx, db, dir, []string{"a"}); len(done) != 0 {
		t.Errorf("a second import: %v", done)
	}
}
