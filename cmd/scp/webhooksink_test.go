package main

import (
	gosync "sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/sync"
)

type took struct {
	ev     sync.Event
	repo   snapshot.RepoKey
	source string
}

// recorder is a sink's take that remembers what it was given.
type recorder struct {
	mu  gosync.Mutex
	got []took
}

func (r *recorder) take(ev sync.Event, repo snapshot.RepoKey, source string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, took{ev, repo, source})
	return nil
}

func (r *recorder) listings() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, t := range r.got {
		if t.ev == sync.EventSourceChanged {
			n++
		}
	}
	return n
}

func (r *recorder) has(want took) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.got {
		if t == want {
			return true
		}
	}
	return false
}

// An event of a repository the portal does not know yet is kept (the inbox passes it on once the repository is known, after a start)
// and lists its source at once; a burst of them costs one more listing when the window closes, which catches a repository created in
// it; an address that already asked does not ask again within the hour (a repository the source leaves out); a known repository goes to
// its own jobs; an owner no source lists is ignored.
func TestWebhookSinkDiscovers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &recorder{}
		known := func(k snapshot.RepoKey) bool { return k == "acme/known" }
		sink := webhookSink(r.take, known, []config.Source{{ID: "src", Orgs: []string{"acme"}}})
		push := func(owner, name string) {
			t.Helper()
			if err := sink(sync.EventPush, owner, name); err != nil {
				t.Fatal(err)
			}
		}
		push("acme", "known")
		push("stranger", "x")
		push("Acme", "first")
		if !r.has(took{sync.EventPush, "acme/known", ""}) || !r.has(took{sync.EventPush, "acme/first", ""}) || r.listings() != 1 {
			t.Fatalf("took %+v, want both pushes kept and one listing", r.got)
		}
		time.Sleep(5 * time.Second)
		push("acme", "second") // created inside the window
		push("acme", "third")
		if r.listings() != 1 {
			t.Fatalf("listings = %d inside the window, want 1", r.listings())
		}
		time.Sleep(discoverEvery)
		synctest.Wait()
		if r.listings() != 2 {
			t.Fatalf("listings = %d after the window, want the trailing one", r.listings())
		}
		time.Sleep(discoverEvery)
		for range 10 { // a repository the source leaves out keeps pushing
			push("acme", "excluded")
			time.Sleep(discoverEvery)
			synctest.Wait()
		}
		if r.listings() != 3 {
			t.Errorf("listings = %d, want one for the left-out repository's pushes", r.listings())
		}
		time.Sleep(declinedFor)
		push("acme", "excluded")
		if r.listings() != 4 {
			t.Errorf("listings = %d, want it to ask again after %v", r.listings(), declinedFor)
		}
	})
}
