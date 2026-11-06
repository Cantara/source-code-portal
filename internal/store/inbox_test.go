package store

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParkedSurvivesReopenUntilUnparked(t *testing.T) {
	db, path := openTemp(t)
	ctx := t.Context()
	for id, v := range map[string]string{"a": "one", "b": "two", "c": "three"} {
		if err := db.Park(ctx, id, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetMeta(ctx, "other", "kept"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, path, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	got, err := db.Parked(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"a": "one", "b": "two", "c": "three"}; !cmp.Equal(got, want) {
		t.Fatalf("parked after a restart: %v", cmp.Diff(want, got))
	}
	if err := db.Unpark(ctx, []string{"a", "c", "missing"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.Parked(ctx); !cmp.Equal(got, map[string]string{"b": "two"}) {
		t.Errorf("parked after unparking: %v", got)
	}
	if v, _ := db.Meta(ctx, "other"); v != "kept" {
		t.Errorf("unparking touched another meta row: %q", v)
	}
}

// Parking syncs its own write and leaves the connection as the store sets it up.
func TestParkRestoresSynchronous(t *testing.T) {
	db, _ := openTemp(t)
	ctx := t.Context()
	for range 5 {
		if err := db.Park(ctx, "a", "x"); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := db.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	var mode int
	if err := conn.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&mode); err != nil || mode != 1 { // 1: NORMAL
		t.Errorf("synchronous after parking: %d (%v), want 1 (NORMAL)", mode, err)
	}
}
