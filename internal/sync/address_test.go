package sync

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

// TestAddressOwnedByOneSource: two sources listing one address, in either order and again and again, leave it to the source whose id
// sorts first, with its entries intact (design/store-model.md): no listing takes it back and no entry is dropped twice. A change of
// visibility alone keeps the entries.
func TestAddressOwnedByOneSource(t *testing.T) {
	repoOf := func(src string, id int64, vis string) store.Repo {
		return store.Repo{Key: "acme/core", ID: snapshot.RepoID{Source: src, GitHubID: id}, Visibility: vis}
	}
	for _, order := range [][]string{{"a", "b"}, {"b", "a"}} {
		t.Run(order[0]+" lists first", func(t *testing.T) {
			st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "scp.db"), discard)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			pub := snapshot.NewPublisher(time.Hour, time.Hour, discard)
			defer pub.Close()
			e := New(st, pub, nil, []string{"a", "b"}, 1, discard)
			run := func(context.Context, Job) (Result, error) { return Result{}, nil }
			if err := e.Register(Spec{Kind: "commits", Prio: PrioScheduled, TTL: time.Hour, Run: run,
				Publish: func(*snapshot.Builder, store.Item, snapshot.Freshness) {}}); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			id := map[string]int64{"a": 1, "b": 7}
			var dropped []string
			for range 3 {
				for _, src := range order {
					e.mu.Lock()
					dropped = append(dropped, e.setCatalogue(src, []store.Repo{repoOf(src, id[src], "public")}, now)...)
					e.mu.Unlock()
				}
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			if got := e.repos["acme/core"]; got.ID.Source != "a" {
				t.Fatalf("acme/core is %+v, want source a's", got)
			}
			if en := e.entries["commits/acme/core"]; en == nil || en.source != "a" {
				t.Errorf("entry %+v, want the owner's", en)
			}
			// b first and then a: b's entries are dropped once, when a takes over; a first: never
			if len(dropped) > 1 {
				t.Errorf("dropped %v: an address changes hands at most once", dropped)
			}
			// visibility alone does not drop the entries
			before := e.entries["commits/acme/core"]
			if removed := e.setCatalogue("a", []store.Repo{repoOf("a", 1, "private")}, now); len(removed) != 0 || e.entries["commits/acme/core"] != before {
				t.Errorf("a visibility change dropped %v", removed)
			}
			if e.repos["acme/core"].Visibility != "private" {
				t.Error("the visibility was not taken")
			}
		})
	}
}
