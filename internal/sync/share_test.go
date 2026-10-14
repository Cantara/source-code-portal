package sync

import (
	"context"
	"slices"
	gosync "sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

// TestLeaderSharesFollowerApplies wires two engines directly: the leader's outcomes go to the follower, which runs nothing and ends
// with the same store; when the lead moves, the other way round.
func TestLeaderSharesFollowerApplies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runsA, runsB atomic.Int32
		a, listA := catalogueRig(t, t.TempDir(), nil, func(context.Context, Job) (Result, error) {
			runsA.Add(1)
			return Result{Payload: []byte("a")}, nil
		})
		b, listB := catalogueRig(t, t.TempDir(), nil, func(context.Context, Job) (Result, error) {
			runsB.Add(1)
			return Result{Payload: []byte("b")}, nil
		})
		var mu gosync.Mutex
		var shared []Shared
		a.eng.Share(func(s Shared) {
			mu.Lock()
			shared = append(shared, s)
			mu.Unlock()
			b.eng.Receive(context.Background(), s)
		})
		b.eng.Share(func(s Shared) { a.eng.Receive(context.Background(), s) })
		a.eng.Lead(true)
		a.run()
		b.run()
		time.Sleep(time.Second)
		if runsA.Load() != 2 || runsB.Load() != 0 || listB.n.Load() != 0 {
			t.Fatalf("runs: leader %d, follower %d and %d listings", runsA.Load(), runsB.Load(), listB.n.Load())
		}
		if it := b.stored("commits/acme/b"); string(it.Payload) != "a" || it.NextDue.IsZero() || it.NextDue != a.stored("commits/acme/b").NextDue {
			t.Errorf("follower's item = %+v", it)
		}
		if w, r := b.eng.Depth(); w != 0 || r != 0 {
			t.Errorf("follower depth = %d, %d", w, r)
		}
		mu.Lock()
		if len(shared) != 3 || shared[0].Catalogue == nil || shared[1].Rev <= shared[0].Rev || shared[2].Rev <= shared[1].Rev {
			t.Errorf("shared = %+v", shared)
		}
		mu.Unlock()

		listA.mu.Lock()
		listA.result.Catalogue = listA.result.Catalogue[:1]
		listA.mu.Unlock()
		a.eng.NotifySource(EventSourceChanged, "gh")
		time.Sleep(time.Second)
		mu.Lock()
		if last := shared[len(shared)-1]; !slices.Equal(last.Removed, []string{"commits/acme/b"}) {
			t.Errorf("removed = %v", last.Removed)
		}
		mu.Unlock()
		if it := b.stored("commits/acme/b"); it.Key != "" {
			t.Errorf("the follower keeps an item of a removed repository: %+v", it)
		}

		a.eng.Lead(false)
		b.eng.Lead(true)
		b.eng.Notify(EventPush, "acme/a")
		time.Sleep(time.Second)
		if runsB.Load() != 1 || string(a.stored("commits/acme/a").Payload) != "b" {
			t.Errorf("after the lead moved: follower runs %d, old leader's item %q", runsB.Load(), a.stored("commits/acme/a").Payload)
		}
		a.stop()
		b.stop()
	})
}

func TestReceiveOrdersAndWaitsForTheCatalogue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, _ := catalogueRig(t, t.TempDir(), nil, func(context.Context, Job) (Result, error) { return Result{}, nil })
		r.eng.Share(func(Shared) {})
		r.run()
		ctx := context.Background()
		item := func(key string, repo snapshot.RepoKey, payload string) store.Item {
			kind := "commits"
			if repo == "" {
				kind = "list"
			}
			return store.Item{Key: key, Kind: kind, Repo: repo, State: snapshot.StateOK, CheckedAt: time.Now(), FetchedAt: time.Now(),
				NextDue: time.Now().Add(time.Hour), Payload: []byte(payload)}
		}
		repos := []store.Repo{{Key: "acme/a", ID: snapshot.RepoID{Source: "gh", GitHubID: 1}}}
		r.eng.Receive(ctx, Shared{Rev: 10, Source: "gh", Item: item("commits/acme/a", "acme/a", "early")})
		r.eng.Receive(ctx, Shared{Rev: 11, Source: "gh", Item: item("commits/acme/z", "acme/z", "never listed")})
		if it := r.stored("commits/acme/a"); it.Key != "" {
			t.Fatalf("an item of an unknown repository was stored: %+v", it)
		}
		r.eng.Receive(ctx, Shared{Rev: 5, Source: "gh", Item: item("list/gh", "", "l"), Catalogue: repos})
		if it := r.stored("commits/acme/a"); string(it.Payload) != "early" {
			t.Errorf("the waiting item was not applied with the catalogue: %+v", it)
		}
		r.eng.mu.Lock()
		orphans := len(r.eng.orphans)
		r.eng.mu.Unlock()
		if orphans != 0 {
			t.Errorf("%d items still wait after the source's catalogue", orphans)
		}
		r.eng.Receive(ctx, Shared{Rev: 9, Source: "gh", Item: item("commits/acme/a", "acme/a", "older")})
		r.eng.Receive(ctx, Shared{Rev: 10, Source: "gh", Item: item("commits/acme/a", "acme/a", "echo")})
		if it := r.stored("commits/acme/a"); string(it.Payload) != "early" {
			t.Errorf("an older or equal revision was applied: %q", it.Payload)
		}
		r.eng.Receive(ctx, Shared{Rev: 12, Source: "gh", Item: item("commits/acme/a", "acme/a", "newer")})
		if it := r.stored("commits/acme/a"); string(it.Payload) != "newer" {
			t.Errorf("a newer revision was not applied: %q", it.Payload)
		}
		recs := r.eng.Records()
		if len(recs) != 2 || recs[0].Item.Key != "list/gh" || len(recs[0].Catalogue) != 1 || recs[1].Rev != 12 {
			t.Errorf("records = %+v", recs)
		}
		r.eng.mu.Lock()
		next := r.eng.nextRev()
		r.eng.mu.Unlock()
		if next <= 12 {
			t.Errorf("next revision %d is not newer than the ones received", next)
		}
		r.stop()
	})
}

func TestResultAfterLostLeadIsDropped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		r, _ := catalogueRig(t, t.TempDir(), nil, func(ctx context.Context, j Job) (Result, error) {
			if j.Repo == "acme/a" {
				<-release
			}
			return Result{Payload: []byte("x")}, nil
		})
		var shares atomic.Int32
		r.eng.Share(func(Shared) { shares.Add(1) })
		r.eng.Lead(true)
		r.run()
		time.Sleep(time.Second)
		before := shares.Load()
		r.eng.Lead(false)
		close(release)
		time.Sleep(time.Second)
		if it := r.stored("commits/acme/a"); it.Key != "" || shares.Load() != before {
			t.Errorf("a result after the lead was lost was recorded: %+v, %d shares", it, shares.Load()-before)
		}
		r.stop()
	})
}
