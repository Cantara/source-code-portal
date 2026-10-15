package sync

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	gosync "sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

var discard = slog.New(slog.DiscardHandler) //nolint:gochecknoglobals // test logger

// rig is an engine over a real store in a temp dir, inside a synctest bubble.
type rig struct {
	t      *testing.T
	st     *store.DB
	pub    *snapshot.Publisher
	eng    *Engine
	cancel context.CancelFunc
	done   chan error

	mu        gosync.Mutex
	published []store.Item
}

func newRig(t *testing.T, dir string, budget Budget, concurrency int, specs ...Spec) *rig {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(dir, "scp.db"), discard)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, st: st, pub: snapshot.NewPublisher(200*time.Millisecond, time.Second, discard)}
	r.eng = New(st, r.pub, budget, []string{"gh"}, concurrency, discard)
	for _, s := range specs {
		s.Publish = func(_ *snapshot.Builder, it store.Item, _ snapshot.Freshness) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.published = append(r.published, it)
		}
		if err := r.eng.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r *rig) run() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel, r.done = cancel, make(chan error, 1)
	go func() { r.done <- r.eng.Run(ctx) }()
	synctest.Wait()
}

func (r *rig) stop() {
	r.cancel()
	if err := <-r.done; err != nil {
		r.t.Error(err)
	}
	r.pub.Close()
	if err := r.st.Close(); err != nil {
		r.t.Error(err)
	}
}

func (r *rig) stored(key string) store.Item {
	_, items, err := r.st.Load(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	for _, it := range items {
		if it.Key == key {
			return it
		}
	}
	return store.Item{}
}

// counter is a Run that counts calls and returns the next scripted error (nil when the script is exhausted).
type counter struct {
	n      atomic.Int32
	mu     gosync.Mutex
	errs   []error
	result Result
}

func (c *counter) run(context.Context, Job) (Result, error) {
	c.n.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.errs) > 0 {
		err := c.errs[0]
		c.errs = c.errs[1:]
		return Result{}, err
	}
	return c.result, nil
}

type fakeErr struct {
	rate    bool
	retryAt time.Time
	auth    bool
	perm    bool
	code    string
}

func (e fakeErr) Error() string                       { return "fake " + e.code }
func (e fakeErr) RateLimitedUntil() (time.Time, bool) { return e.retryAt, e.rate }
func (e fakeErr) AuthFailed() bool                    { return e.auth }
func (e fakeErr) Permanent() bool                     { return e.perm }
func (e fakeErr) ErrorCode() string                   { return e.code }

func sourceSpec(c *counter, ttl time.Duration) Spec {
	return Spec{Kind: "list", PerSource: true, Lists: true, Prio: PrioScheduled, TTL: ttl, On: []Event{EventSourceChanged}, Run: c.run}
}

func TestRunsAndReschedules(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &counter{result: Result{Payload: []byte("v1")}}
		r := newRig(t, t.TempDir(), nil, 4, sourceSpec(c, time.Hour))
		r.run()
		if c.n.Load() != 1 {
			t.Errorf("runs at start = %d, want 1", c.n.Load())
		}
		it := r.stored("list/gh")
		if it.State != snapshot.StateOK || string(it.Payload) != "v1" || it.NextDue.Sub(it.CheckedAt) < 54*time.Minute {
			t.Errorf("stored = %+v", it)
		}
		time.Sleep(50 * time.Minute)
		if c.n.Load() != 1 {
			t.Errorf("ran before the TTL")
		}
		time.Sleep(20 * time.Minute)
		if c.n.Load() != 2 {
			t.Errorf("runs after the TTL = %d, want 2", c.n.Load())
		}
		r.stop()
	})
}

func TestAgainRunsSooner(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &counter{result: Result{Payload: []byte("v1"), Again: 30 * time.Second}}
		r := newRig(t, t.TempDir(), nil, 4, sourceSpec(c, time.Hour))
		r.run()
		time.Sleep(31 * time.Second)
		if c.n.Load() != 2 {
			t.Errorf("runs 31s after a result asking again in 30s = %d, want 2", c.n.Load())
		}
		c.mu.Lock()
		c.result.Again = 0
		c.mu.Unlock()
		time.Sleep(31 * time.Second)
		if c.n.Load() != 3 {
			t.Errorf("runs = %d, want 3", c.n.Load())
		}
		time.Sleep(10 * time.Minute)
		if c.n.Load() != 3 {
			t.Errorf("without Again the TTL holds: runs = %d", c.n.Load())
		}
		r.stop()
	})
}

func TestTransientKeepsPayloadAndBacksOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &counter{result: Result{Payload: []byte("good")}}
		r := newRig(t, t.TempDir(), nil, 4, sourceSpec(c, time.Hour))
		r.run()
		c.mu.Lock()
		c.errs = []error{fakeErr{code: "transient"}, fakeErr{code: "transient"}}
		c.mu.Unlock()
		time.Sleep(70 * time.Minute) // past the TTL; the two retries back off at most 2s and 4s
		r.mu.Lock()
		var sawFailure bool
		for _, it := range r.published {
			if it.Attempts == 1 && it.ErrCode == "transient" && string(it.Payload) == "good" && it.State == snapshot.StateOK {
				sawFailure = true
			}
		}
		r.mu.Unlock()
		if !sawFailure {
			t.Errorf("no published failure that kept the payload: %+v", r.published)
		}
		if it := r.stored("list/gh"); it.Attempts != 0 || c.n.Load() != 4 {
			t.Errorf("after retries: attempts %d, runs %d; want recovered after 4 runs", it.Attempts, c.n.Load())
		}
		r.stop()
	})
}

func TestRateLimitPausesWithoutAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &counter{errs: []error{fakeErr{rate: true, retryAt: time.Now().Add(10 * time.Minute)}}}
		r := newRig(t, t.TempDir(), nil, 4, sourceSpec(c, time.Hour))
		r.run()
		if it := r.stored("list/gh"); it.Key != "" {
			t.Errorf("a rate-limited run wrote %+v", it)
		}
		time.Sleep(9 * time.Minute)
		if c.n.Load() != 1 {
			t.Errorf("ran during the pause")
		}
		time.Sleep(2 * time.Minute)
		if c.n.Load() != 2 || r.stored("list/gh").State != snapshot.StateOK {
			t.Errorf("runs after the pause = %d", c.n.Load())
		}
		r.stop()
	})
}

func TestPermanentRetriesAfterADay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &counter{errs: []error{fakeErr{perm: true, code: "forbidden"}}}
		r := newRig(t, t.TempDir(), nil, 4, sourceSpec(c, time.Hour))
		r.run()
		if it := r.stored("list/gh"); it.State != snapshot.StateError || it.ErrCode != "forbidden" {
			t.Errorf("stored = %+v, want error forbidden", it)
		}
		time.Sleep(23 * time.Hour)
		if c.n.Load() != 1 {
			t.Errorf("retried before 24h")
		}
		time.Sleep(90 * time.Minute) // the retry at 24h succeeds; the next run is a TTL (at least 54m) later
		if c.n.Load() != 2 {
			t.Errorf("not retried after 24h")
		}
		r.stop()
	})
}

func TestPanicIsPermanent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, t.TempDir(), nil, 4, Spec{Kind: "list", PerSource: true, TTL: time.Hour,
			Run: func(context.Context, Job) (Result, error) { panic("boom") }})
		r.run()
		if it := r.stored("list/gh"); it.ErrCode != "panic" {
			t.Errorf("stored = %+v, want panic recorded", it)
		}
		r.stop()
	})
}

// catalogue lists two repositories; repo jobs record their runs.
func catalogueRig(t *testing.T, dir string, budget Budget, repoRun func(context.Context, Job) (Result, error)) (*rig, *counter) {
	t.Helper()
	list := &counter{result: Result{Catalogue: []store.Repo{
		{Key: "acme/a", ID: snapshot.RepoID{Source: "gh", GitHubID: 1}},
		{Key: "acme/b", ID: snapshot.RepoID{Source: "gh", GitHubID: 2}},
	}}}
	return newRig(t, dir, budget, 4, sourceSpec(list, 5*time.Minute),
		Spec{Kind: "commits", Prio: PrioScheduled, TTL: 2 * time.Hour, On: []Event{EventPush}, Run: repoRun}), list
}

func TestCatalogueAddsAndRemovesRepoJobs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32
		r, list := catalogueRig(t, t.TempDir(), nil, func(context.Context, Job) (Result, error) {
			runs.Add(1)
			return Result{Payload: []byte("c")}, nil
		})
		r.run()
		time.Sleep(time.Second)
		if runs.Load() != 2 || r.stored("commits/acme/b").State != snapshot.StateOK {
			t.Errorf("repo jobs after the first listing = %d, want 2", runs.Load())
		}
		list.mu.Lock()
		list.result.Catalogue = list.result.Catalogue[:1]
		list.mu.Unlock()
		time.Sleep(6 * time.Minute)
		if it := r.stored("commits/acme/b"); it.Key != "" {
			t.Errorf("items of a removed repository remain: %+v", it)
		}
		r.eng.mu.Lock()
		_, left := r.eng.entries["commits/acme/b"]
		r.eng.mu.Unlock()
		if left {
			t.Errorf("entry of a removed repository remains")
		}
		r.stop()
	})
}

func TestNotifyCoalesces(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32
		release := make(chan struct{})
		var block atomic.Bool
		r, _ := catalogueRig(t, t.TempDir(), nil, func(ctx context.Context, j Job) (Result, error) {
			if j.Repo == "acme/a" {
				runs.Add(1)
				if block.Load() {
					select {
					case <-release:
					case <-ctx.Done():
					}
				}
			}
			return Result{}, nil
		})
		r.run()
		time.Sleep(time.Second)
		base := runs.Load()

		block.Store(true)
		r.eng.Notify(EventPush, "acme/a")
		synctest.Wait() // running and blocked
		block.Store(false)
		for range 10 {
			r.eng.Notify(EventPush, "acme/a")
		}
		close(release)
		time.Sleep(time.Second)
		if got := runs.Load() - base; got != 2 {
			t.Errorf("ten pushes while running gave %d runs, want 2 (the running one and one follow-up)", got)
		}
		r.stop()
	})
}

func TestConcurrencyLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cur, peak atomic.Int32
		repos := make([]store.Repo, 10)
		for i := range repos {
			repos[i] = store.Repo{Key: snapshot.KeyOf("acme", string(rune('a'+i))), ID: snapshot.RepoID{Source: "gh", GitHubID: int64(i + 1)}}
		}
		list := &counter{result: Result{Catalogue: repos}}
		r := newRig(t, t.TempDir(), nil, 2, sourceSpec(list, time.Hour), Spec{Kind: "commits", TTL: time.Hour,
			Run: func(context.Context, Job) (Result, error) {
				peak.Store(max(peak.Load(), cur.Add(1)))
				time.Sleep(time.Second)
				cur.Add(-1)
				return Result{}, nil
			}})
		r.run()
		time.Sleep(time.Minute)
		if peak.Load() != 2 {
			t.Errorf("peak concurrency = %d, want 2", peak.Load())
		}
		r.stop()
	})
}

type budget struct{ frac atomic.Value }

func (b *budget) Remaining(string, string) (float64, time.Time) {
	return b.frac.Load().(float64), time.Now().Add(time.Hour) //nolint:forcetypeassert // test
}

func TestBudgetHoldsBackfill(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &budget{}
		b.frac.Store(0.30) // below the backfill reserve, above the scheduled one
		var runs atomic.Int32
		r, list := catalogueRig(t, t.TempDir(), b, func(context.Context, Job) (Result, error) {
			runs.Add(1)
			return Result{}, nil
		})
		r.run()
		time.Sleep(time.Minute)
		if list.n.Load() != 0 || runs.Load() != 0 {
			t.Errorf("backfill ran under the reserve: list %d, repo %d", list.n.Load(), runs.Load())
		}
		b.frac.Store(0.9)
		time.Sleep(time.Hour)
		if runs.Load() != 2 {
			t.Errorf("backfill after the reset = %d, want 2", runs.Load())
		}
		r.stop()
	})
}

func TestWarmStartPublishesAndKeepsSchedule(t *testing.T) {
	dir := t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		r, _ := catalogueRig(t, dir, nil, func(context.Context, Job) (Result, error) { return Result{Payload: []byte("c")}, nil })
		r.run()
		time.Sleep(time.Second)
		r.stop()
	})
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32
		r, list := catalogueRig(t, dir, nil, func(context.Context, Job) (Result, error) { runs.Add(1); return Result{}, nil })
		r.run()
		r.mu.Lock()
		n := len(r.published)
		r.mu.Unlock()
		if n != 3 {
			t.Errorf("warm start published %d items, want 3", n)
		}
		if list.n.Load() != 0 || runs.Load() != 0 {
			t.Errorf("warm start inside the TTLs ran jobs: list %d, repo %d", list.n.Load(), runs.Load())
		}
		r.stop()
	})
}

func TestBackoffBounds(t *testing.T) {
	for n := 1; n < 40; n++ {
		for range 50 {
			d := backoff(n)
			ceil := retryMax
			if n < 20 {
				ceil = min(retryBase<<(n-1), retryMax)
			}
			if d < retryBase || d > max(ceil, retryBase) {
				t.Fatalf("backoff(%d) = %v, want %v..%v", n, d, retryBase, ceil)
			}
		}
	}
}

func TestRegisterRefuses(t *testing.T) {
	e := New(nil, nil, nil, nil, 1, discard)
	ok := Spec{Kind: "a", Run: (&counter{}).run, Publish: func(*snapshot.Builder, store.Item, snapshot.Freshness) {}}
	if err := e.Register(ok); err != nil {
		t.Fatal(err)
	}
	for _, s := range []Spec{ok, {Kind: "b/c", Run: ok.Run, Publish: ok.Publish}, {Kind: "d"}} {
		if err := e.Register(s); err == nil {
			t.Errorf("Register(%q) accepted", s.Kind)
		}
	}
}

func TestWarmStartHonoursAShorterTTL(t *testing.T) {
	dir := t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		c := &counter{}
		r := newRig(t, dir, nil, 1, sourceSpec(c, time.Hour))
		r.run()
		time.Sleep(time.Second)
		r.stop()
	})
	synctest.Test(t, func(t *testing.T) {
		c := &counter{}
		r := newRig(t, dir, nil, 1, sourceSpec(c, time.Minute))
		r.run()
		time.Sleep(30 * time.Second)
		if c.n.Load() != 0 {
			t.Errorf("ran %d times before the new TTL", c.n.Load())
		}
		time.Sleep(2 * time.Minute)
		if c.n.Load() == 0 {
			t.Error("the stored schedule of the longer TTL held")
		}
		r.stop()
	})
}

func TestNotifySource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &counter{}
		r := newRig(t, t.TempDir(), nil, 1, sourceSpec(c, time.Hour))
		r.run()
		time.Sleep(time.Second)
		base := c.n.Load()
		r.eng.NotifySource(EventPush, "gh") // not an event of the listing
		r.eng.NotifySource(EventSourceChanged, "other")
		time.Sleep(time.Second)
		if c.n.Load() != base {
			t.Errorf("unrelated events ran the listing")
		}
		r.eng.NotifySource(EventSourceChanged, "gh")
		time.Sleep(time.Second)
		if c.n.Load() != base+1 {
			t.Errorf("EventSourceChanged gave %d runs, want 1", c.n.Load()-base)
		}
		r.stop()
	})
}

func TestObserveDepthAlive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &counter{errs: []error{fakeErr{code: "x"}}}
		r := newRig(t, t.TempDir(), nil, 1, sourceSpec(c, time.Hour))
		var mu gosync.Mutex
		var results []string
		r.eng.Observe(func(kind, result string, _ time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			results = append(results, kind+":"+result)
		})
		if !r.eng.Alive() {
			t.Error("not alive before Run")
		}
		r.run()
		time.Sleep(2 * time.Minute) // the failure retries after its backoff
		mu.Lock()
		got := strings.Join(results, ",")
		mu.Unlock()
		if !strings.HasPrefix(got, "list:error,list:ok") {
			t.Errorf("observed %s", got)
		}
		if waiting, running := r.eng.Depth(); waiting != 0 || running != 0 {
			t.Errorf("depth %d waiting, %d running", waiting, running)
		}
		if !r.eng.Alive() {
			t.Error("the dispatcher is not alive")
		}
		r.stop()
	})
}
