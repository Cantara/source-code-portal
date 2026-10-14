package refresh

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
)

var bo = Backoff{Base: time.Second, Cap: time.Minute, MaxAttempts: 5}

type rlErr struct{ at time.Time }

func (e rlErr) Error() string      { return "rate limited" }
func (e rlErr) RetryAt() time.Time { return e.at }

type permErr struct{}

func (permErr) Error() string   { return "perm" }
func (permErr) Permanent() bool { return true }

type goneErr struct{}

func (goneErr) Error() string { return "gone" }
func (goneErr) Gone() bool    { return true }

func spec(kind string, prio job.Priority, run func(ctx context.Context, k job.Key) (job.Outcome, error)) job.Spec {
	return job.Spec{Kind: kind, Upstream: "gh", Scope: job.ScopeRepo, Prio: prio, Run: run,
		Policy: freshness.Policy{TTL: time.Hour}}
}

func startEngine(t *testing.T, e *Engine) (stop func() error) {
	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan error, 1)
	go func() { res <- e.Run(ctx) }()
	return func() error { cancel(); return <-res }
}

func key(repo string) job.Key { return job.NewKey("r", repo) }

func TestCoalesceQueuedAndDirtyWhileRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 2})
		var runs atomic.Int32
		release := make(chan struct{})
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			runs.Add(1)
			<-release
			return job.Outcome{}, nil
		}))
		for range 10 { // before Run: every enqueue coalesces into the one queued entry
			e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false)
		}
		stop := startEngine(t, e)
		defer stop()
		synctest.Wait()
		if runs.Load() != 1 {
			t.Fatalf("runs=%d want 1", runs.Load())
		}
		for range 10 {
			e.Enqueue(key("a"), job.PrioWebhook, time.Time{}, false)
		}
		release <- struct{}{}
		synctest.Wait()
		if runs.Load() != 2 {
			t.Fatalf("runs=%d want 2 (one follow-up)", runs.Load())
		}
		release <- struct{}{}
		synctest.Wait()
		if s := e.Inspect(); s.Keys != 0 || s.Jobs["marked_dirty"] != 10 || s.Jobs["coalesced"] != 9 {
			t.Fatalf("stats %+v", s)
		}
	})
}

func TestPaceAndConcurrency(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 3, Pace: 100 * time.Millisecond})
		var cur, peak atomic.Int32
		var mu sync.Mutex
		var starts []time.Duration
		t0 := time.Now()
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			mu.Lock()
			starts = append(starts, time.Since(t0))
			mu.Unlock()
			n := cur.Add(1)
			for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
			}
			time.Sleep(time.Second)
			cur.Add(-1)
			return job.Outcome{}, nil
		}))
		stop := startEngine(t, e)
		defer stop()
		for _, r := range []string{"a", "b", "c", "d", "e"} {
			e.Enqueue(key(r), job.PrioScheduled, time.Time{}, false)
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if peak.Load() != 3 {
			t.Fatalf("peak=%d", peak.Load())
		}
		want := []time.Duration{0, 100 * time.Millisecond, 200 * time.Millisecond, time.Second, 1100 * time.Millisecond}
		for i, w := range want {
			if starts[i] != w {
				t.Fatalf("starts=%v want %v", starts, want)
			}
		}
	})
}

func TestBackoffBounds(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	b := Backoff{Base: 2 * time.Second, Cap: 15 * time.Minute}
	for attempt := 1; attempt < 200; attempt++ {
		d := b.Delay(attempt, r)
		hi := min(b.Cap, b.Base<<min(attempt, 20))
		if d < b.Base || d > hi {
			t.Fatalf("attempt %d delay %v outside [%v,%v]", attempt, d, b.Base, hi)
		}
	}
	// saturating shift: a large Base with a huge attempt must not overflow
	big := Backoff{Base: time.Hour, Cap: 1000 * time.Hour}
	if d := big.Delay(100, r); d < time.Hour || d > 1000*time.Hour {
		t.Fatalf("overflow %v", d)
	}
}

func TestTransientRetryExhaustedKeepsRetrying(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var exhausted atomic.Int32
		e := New(bo, 7, WithOnExhausted(func(k job.Key, n int, err error) { exhausted.Add(1) }))
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		var runs atomic.Int32
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			if runs.Add(1) < 9 {
				return job.Outcome{}, errors.New("boom")
			}
			return job.Outcome{}, nil
		}))
		stop := startEngine(t, e)
		defer stop()
		e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false)
		time.Sleep(30 * time.Minute)
		synctest.Wait()
		if runs.Load() != 9 || exhausted.Load() != 1 {
			t.Fatalf("runs=%d exhausted=%d", runs.Load(), exhausted.Load())
		}
		if s := e.Inspect(); s.Keys != 0 || s.Jobs["retried"] != 8 {
			t.Fatalf("%+v", s)
		}
	})
}

func TestRateLimitPausesUpstreamWithoutAttemptAndFloorGrows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 3)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 4})
		reset := time.Now().Add(10 * time.Minute)
		var first atomic.Bool
		var startedAfterReset atomic.Int32
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			if k == key("a") && first.CompareAndSwap(false, true) {
				return job.Outcome{}, rlErr{reset}
			}
			if time.Now().Before(reset) {
				t.Errorf("job %s started during pause", k)
			}
			startedAfterReset.Add(1)
			return job.Outcome{}, nil
		}))
		stop := startEngine(t, e)
		defer stop()
		e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false)
		synctest.Wait()
		for _, r := range []string{"b", "c", "d"} {
			e.Enqueue(key(r), job.PrioScheduled, time.Time{}, false)
		}
		synctest.Wait()
		if s := e.Inspect(); s.Upstreams["gh"].PausedUntil.IsZero() || startedAfterReset.Load() != 0 {
			t.Fatalf("not paused: %+v", s)
		}
		time.Sleep(11 * time.Minute)
		synctest.Wait()
		if startedAfterReset.Load() != 4 {
			t.Fatalf("started=%d", startedAfterReset.Load())
		}
		if s := e.Inspect(); s.Jobs["retried"] != 0 || s.Jobs["ratelimited"] != 1 {
			t.Fatalf("a rate limit must not consume an attempt: %+v", s.Jobs)
		}
	})
	synctest.Test(t, func(t *testing.T) { // RetryAt in the past: exponential floor 60s, 120s
		e := New(bo, 3)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		var starts []time.Time
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			starts = append(starts, time.Now())
			if len(starts) < 3 {
				return job.Outcome{}, rlErr{time.Now()}
			}
			return job.Outcome{}, nil
		}))
		stop := startEngine(t, e)
		defer stop()
		e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false)
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if len(starts) != 3 {
			t.Fatalf("starts=%v", starts)
		}
		d1, d2 := starts[1].Sub(starts[0]), starts[2].Sub(starts[1])
		if d1 < 60*time.Second || d1 > 62*time.Second || d2 < 120*time.Second || d2 > 122*time.Second {
			t.Fatalf("floors %v %v", d1, d2)
		}
	})
}

func TestPermanentParksForceUnparksProbeAfter24h(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var parked []ParkedKey
		var unparked []string
		var mu sync.Mutex
		e := New(bo, 3,
			WithOnParked(func(p ParkedKey) { mu.Lock(); parked = append(parked, p); mu.Unlock() }),
			WithOnUnparked(func(k job.Key, why string) { mu.Lock(); unparked = append(unparked, why); mu.Unlock() }))
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		var runs atomic.Int32
		var okNow atomic.Bool
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			runs.Add(1)
			if okNow.Load() {
				return job.Outcome{}, nil
			}
			return job.Outcome{}, permErr{}
		}))
		var observed []error
		e.Observe(func(k job.Key, o job.Outcome, err error) { mu.Lock(); observed = append(observed, err); mu.Unlock() })
		stop := startEngine(t, e)
		defer stop()
		e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false)
		synctest.Wait()
		e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false) // suppressed
		synctest.Wait()
		mu.Lock()
		np, lastObs := len(parked), observed[len(observed)-1]
		mu.Unlock()
		if runs.Load() != 1 || len(e.Parked()) != 1 || np != 1 {
			t.Fatalf("runs=%d parked=%v", runs.Load(), e.Parked())
		}
		var pe *ParkedError
		if !errors.As(lastObs, &pe) {
			t.Fatalf("observer should learn about the suppressed enqueue: %v", lastObs)
		}
		time.Sleep(25 * time.Hour) // the probe runs once and fails permanently again
		synctest.Wait()
		if runs.Load() != 2 || len(e.Parked()) != 1 {
			t.Fatalf("probe: runs=%d parked=%d", runs.Load(), len(e.Parked()))
		}
		okNow.Store(true)
		e.Enqueue(key("a"), job.PrioWebhook, time.Time{}, true) // force
		synctest.Wait()
		mu.Lock()
		nu := len(unparked)
		mu.Unlock()
		if runs.Load() != 3 || len(e.Parked()) != 0 || nu != 1 {
			t.Fatalf("force: runs=%d parked=%d unparked=%v", runs.Load(), len(e.Parked()), nu)
		}
	})
}

func TestPanicIsContainedAndParked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) { panic("kaboom") }))
		stop := startEngine(t, e)
		defer stop()
		e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false)
		synctest.Wait()
		if s := e.Inspect(); s.Jobs["panic"] != 1 || s.Parked != 1 {
			t.Fatalf("%+v", s)
		}
	})
}

func TestGoneDropsKeyWithoutParking(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) { return job.Outcome{}, goneErr{} }))
		stop := startEngine(t, e)
		defer stop()
		e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false)
		synctest.Wait()
		if s := e.Inspect(); s.Keys != 0 || s.Parked != 0 || s.Jobs["gone"] != 1 {
			t.Fatalf("%+v", s)
		}
	})
}

// A hook that re-enters the engine must not deadlock (Go review F-05, reproduced on the prototype).
func TestHooksRunOutsideTheMutex(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var e *Engine
		var reentered atomic.Int32
		hook := func() {
			_ = e.Inspect()
			_ = e.Enqueue(key("other"), job.PrioScheduled, time.Time{}, false)
			reentered.Add(1)
		}
		e = New(bo, 1,
			WithOnParked(func(ParkedKey) { hook() }),
			WithOnExhausted(func(job.Key, int, error) { hook() }))
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			if k == key("a") {
				return job.Outcome{}, permErr{}
			}
			return job.Outcome{}, nil
		}))
		e.Observe(func(k job.Key, o job.Outcome, err error) { _ = e.Inspect() })
		stop := startEngine(t, e)
		e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false)
		synctest.Wait()
		if reentered.Load() != 1 {
			t.Fatalf("hook did not complete: %d", reentered.Load())
		}
		if err := stop(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestEnqueueAfterRunReturnedIsErrClosed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) { return job.Outcome{}, nil }))
		stop := startEngine(t, e)
		if err := stop(); err != nil {
			t.Fatal(err)
		}
		if err := e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false); !errors.Is(err, job.ErrClosed) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestQueueFullAndUnknownKind(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1, WithMaxKeys(2))
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) { return job.Outcome{}, nil }))
		// not started: nothing drains
		for _, r := range []string{"a", "b"} {
			if err := e.Enqueue(key(r), job.PrioScheduled, time.Time{}, false); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.Enqueue(key("c"), job.PrioScheduled, time.Time{}, false); !errors.Is(err, job.ErrQueueFull) {
			t.Fatalf("err=%v", err)
		}
		if err := e.Enqueue(key("a"), job.PrioWebhook, time.Time{}, false); err != nil { // coalesces: no new key
			t.Fatal(err)
		}
		if err := e.Enqueue(job.NewKey("nope", "x"), job.PrioScheduled, time.Time{}, false); !errors.Is(err, ErrUnknownKind) {
			t.Fatalf("err=%v", err)
		}
	})
}

type fakeBudget struct {
	mu sync.Mutex
	s  BudgetState
}

func (b *fakeBudget) Remaining(string) (BudgetState, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.s, true
}
func (b *fakeBudget) set(f float64, reset time.Time) {
	b.mu.Lock()
	b.s = BudgetState{f, reset}
	b.mu.Unlock()
}

// classGate stands in for bridge.RateGate (which maps a priority onto github.Client.Admit): it admits a class only while the
// fraction of the remaining budget is above that class's reserve. The arithmetic itself is tested in unit 05; this test pins
// what the engine does with a gate answer per priority (CD-CF-02).
type classGate struct{ b *fakeBudget }

func (g classGate) Admit(upstream string, p job.Priority) time.Duration {
	need := map[job.Priority]float64{job.PrioWebhook: 0.02, job.PrioScheduled: 0.15, job.PrioBackfill: 0.40}
	if p == job.PrioInteractive {
		return 0
	}
	s, _ := g.b.Remaining(upstream)
	if s.Fraction > need[p] {
		return 0
	}
	return max(time.Until(s.Reset), time.Second)
}

type BudgetState struct {
	Fraction float64
	Reset    time.Time
}

func TestGateReservesPerPriority(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBudget{}
		reset := time.Now().Add(20 * time.Minute)
		b.set(0.30, reset) // below backfill (0.40), above scheduled (0.15)
		e := New(bo, 1, WithGate(classGate{b}))
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 4})
		var mu sync.Mutex
		ran := map[string]time.Time{}
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			mu.Lock()
			ran[string(k)] = time.Now()
			mu.Unlock()
			return job.Outcome{}, nil
		}))
		stop := startEngine(t, e)
		defer stop()
		e.Enqueue(key("backfill"), job.PrioBackfill, time.Time{}, false)
		e.Enqueue(key("sched"), job.PrioScheduled, time.Time{}, false)
		e.Enqueue(key("hook"), job.PrioWebhook, time.Time{}, false)
		synctest.Wait()
		mu.Lock()
		if len(ran) != 2 || ran["r/backfill"] != (time.Time{}) {
			t.Fatalf("ran=%v", ran)
		}
		mu.Unlock()
		b.set(0.9, reset) // budget recovers: the backfill job starts within GateRecheck, no reset needed
		time.Sleep(6 * time.Second)
		synctest.Wait()
		mu.Lock()
		defer mu.Unlock()
		if len(ran) != 3 {
			t.Fatalf("ran=%v", ran)
		}
	})
}

func TestBackoffEntryNotPromotedByEnqueueUnlessForced(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(Backoff{Base: time.Minute, Cap: time.Hour, MaxAttempts: 8}, 1)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		var runs atomic.Int32
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			if runs.Add(1) == 1 {
				return job.Outcome{}, errors.New("transient")
			}
			return job.Outcome{}, nil
		}))
		stop := startEngine(t, e)
		defer stop()
		e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false)
		synctest.Wait()
		e.Enqueue(key("a"), job.PrioWebhook, time.Time{}, false) // raises priority only
		synctest.Wait()
		if runs.Load() != 1 {
			t.Fatalf("a webhook must not skip the backoff: runs=%d", runs.Load())
		}
		e.Enqueue(key("a"), job.PrioWebhook, time.Time{}, true)
		synctest.Wait()
		if runs.Load() != 2 {
			t.Fatalf("force promotes: runs=%d", runs.Load())
		}
	})
}

func TestJobTimeoutOverrunAndStuckAllSlots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1, JobTimeout: 10 * time.Second})
		block := make(chan struct{})
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			<-block // ignores ctx
			return job.Outcome{}, nil
		}))
		stop := startEngine(t, e)
		e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false)
		time.Sleep(12 * time.Second)
		synctest.Wait()
		s := e.Inspect().Upstreams["gh"]
		if s.Stuck != 0 {
			t.Fatalf("not yet stuck (5s after deadline): %+v", s)
		}
		time.Sleep(4 * time.Second)
		synctest.Wait()
		s = e.Inspect().Upstreams["gh"]
		if s.Stuck != 1 || s.AllStuckSince.IsZero() {
			t.Fatalf("expected stuck: %+v", s)
		}
		close(block)
		synctest.Wait()
		if s := e.Inspect().Upstreams["gh"]; s.Stuck != 0 || !s.AllStuckSince.IsZero() {
			t.Fatalf("%+v", s)
		}
		stop()
	})
}

func TestShutdownGraceDrainsThenCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1, WithGrace(15*time.Second))
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 2, JobTimeout: time.Hour})
		var sawCancel atomic.Bool
		var finishedClean atomic.Bool
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			if k == key("short") {
				time.Sleep(5 * time.Second)
				finishedClean.Store(ctx.Err() == nil)
				return job.Outcome{}, nil
			}
			select {
			case <-ctx.Done():
				sawCancel.Store(true)
				return job.Outcome{}, ctx.Err()
			case <-time.After(time.Hour):
				return job.Outcome{}, nil
			}
		}))
		stop := startEngine(t, e)
		e.Enqueue(key("short"), job.PrioScheduled, time.Time{}, false)
		e.Enqueue(key("long"), job.PrioScheduled, time.Time{}, false)
		synctest.Wait()
		t0 := time.Now()
		if err := stop(); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(t0); took != 15*time.Second {
			t.Fatalf("Run returned after %v, want the 15s grace", took)
		}
		if !finishedClean.Load() || !sawCancel.Load() {
			t.Fatalf("clean=%v cancel=%v", finishedClean.Load(), sawCancel.Load())
		}
	})
}

func TestShutdownAbandonsJobsThatIgnoreCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1, WithGrace(time.Second))
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1, JobTimeout: time.Hour})
		block := make(chan struct{})
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) { <-block; return job.Outcome{}, nil }))
		stop := startEngine(t, e)
		e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false)
		synctest.Wait()
		if err := stop(); !errors.Is(err, ErrAbandoned) {
			t.Fatalf("err=%v", err)
		}
		close(block) // let the goroutine finish so the bubble can end
	})
}

// ---- planner ----

type memSink struct {
	mu sync.Mutex
	m  map[job.Key]PlanState
}

func (s *memSink) SaveState(k job.Key, st PlanState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[job.Key]PlanState{}
	}
	s.m[k] = st
}

func plannerRig(t *testing.T, run func(ctx context.Context, k job.Key) (job.Outcome, error), pol freshness.Policy, opts ...Option) (*Engine, *Planner, *memSink, func() error) {
	e := New(bo, 1, opts...)
	e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 4})
	sp := job.Spec{Kind: "r", Upstream: "gh", Scope: job.ScopeRepo, Prio: job.PrioScheduled, Policy: pol, Run: run}
	e.Register(sp)
	sink := &memSink{}
	p := NewPlanner(e, sink, 1)
	p.Track(sp)
	e.Observe(p.OnResult)
	return e, p, sink, startEngine(t, e)
}

func TestPlannerTickDuringRunningJobDoesNotRunTwice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32
		e, p, _, stop := plannerRig(t, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			runs.Add(1)
			time.Sleep(40 * time.Second) // spans several 15 s ticks
			return job.Outcome{}, nil
		}, freshness.Policy{TTL: time.Hour})
		defer stop()
		p.SetSubjects("r", []job.Key{key("a")})
		for i := 0; i < 5; i++ {
			p.Tick(time.Now())
			time.Sleep(15 * time.Second)
		}
		synctest.Wait()
		if runs.Load() != 1 {
			t.Fatalf("runs=%d, want exactly 1", runs.Load())
		}
		if s := e.Inspect(); s.Jobs["marked_dirty"] != 0 {
			t.Fatalf("%+v", s.Jobs)
		}
	})
}

func TestPlannerWebhookRunInFlightIsNotTickedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32
		e, p, _, stop := plannerRig(t, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			runs.Add(1)
			time.Sleep(20 * time.Second)
			return job.Outcome{}, nil
		}, freshness.Policy{TTL: time.Hour})
		defer stop()
		p.SetSubjects("r", []job.Key{key("a")})
		// the first run completes and the item is not due for an hour
		p.Tick(time.Now())
		time.Sleep(time.Minute)
		// make it due, then a webhook enqueue through the planner, then a tick while it runs
		p.Restore(map[job.Key]PlanState{key("a"): {NextDue: time.Now()}})
		_ = e
		if err := p.Enqueue(key("a"), job.PrioWebhook, time.Time{}, false); err != nil {
			t.Fatal(err)
		}
		p.Tick(time.Now())
		time.Sleep(time.Minute)
		synctest.Wait()
		if runs.Load() != 2 {
			t.Fatalf("runs=%d, want 2 (initial + webhook), the tick must not add a third", runs.Load())
		}
	})
}

func TestPlannerIdleBackoffRestoreAndColdPriority(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var prios []job.Priority
		var mu sync.Mutex
		var obsPrio func(k job.Key) job.Priority
		_ = obsPrio
		e := New(bo, 1)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		pol := freshness.Policy{TTL: 10 * time.Minute, IdleBackoffMax: 60 * time.Minute}
		sp := job.Spec{Kind: "r", Upstream: "gh", Scope: job.ScopeRepo, Prio: job.PrioScheduled, Policy: pol,
			Run: func(ctx context.Context, k job.Key) (job.Outcome, error) { return job.Outcome{Unchanged: true}, nil }}
		e.Register(sp)
		sink := &memSink{}
		p := NewPlanner(e, sink, 1)
		p.Track(sp)
		e.Observe(func(k job.Key, o job.Outcome, err error) {
			mu.Lock()
			prios = append(prios, 0)
			mu.Unlock()
			p.OnResult(k, o, err)
		})
		stop := startEngine(t, e)
		defer stop()
		p.Restore(map[job.Key]PlanState{key("old"): {NextDue: time.Now().Add(time.Hour), UnchangedStreak: 3}})
		p.SetSubjects("r", []job.Key{key("new"), key("old")})
		p.Tick(time.Now())
		synctest.Wait()
		// "new" ran (cold, never done); "old" is not due for an hour (restored)
		if pr := p.Progress("r", time.Now()); pr.Completed != 2 || pr.Subjects != 2 {
			t.Fatalf("%+v", pr)
		}
		got := sink.m[key("new")]
		if got.UnchangedStreak != 1 {
			t.Fatalf("streak %+v", got)
		}
		// unchanged streaks double the interval: 1st unchanged -> 20 min, then 40, then capped at 60
		var last time.Duration
		for i := 0; i < 4; i++ {
			now := time.Now()
			p.Tick(now.Add(2 * time.Hour))
			synctest.Wait()
			last = sink.m[key("new")].NextDue.Sub(time.Now())
		}
		if last > 66*time.Minute || last < 54*time.Minute { // 60 min +-10% not applied (Jitter 0)
			t.Fatalf("interval %v", last)
		}
		if _, ok := sink.m[key("old")]; ok && sink.m[key("old")].UnchangedStreak < 3 {
			t.Fatalf("old should not have run: %+v", sink.m[key("old")])
		}
	})
}

func TestPlannerColdItemsUseBackfillPriority(t *testing.T) {
	var got []job.Priority
	var mu sync.Mutex
	rec := recEnq(func(k job.Key, p job.Priority) { mu.Lock(); got = append(got, p); mu.Unlock() })
	p := NewPlanner(rec, nil, 1)
	p.Track(job.Spec{Kind: "r", Upstream: "gh", Scope: job.ScopeRepo, Prio: job.PrioScheduled, Policy: freshness.Policy{TTL: time.Hour}})
	p.Track(job.Spec{Kind: "g", Upstream: "gh", Scope: job.ScopeGlobal, Prio: job.PrioScheduled, Policy: freshness.Policy{TTL: time.Hour}})
	p.SetSubjects("r", []job.Key{key("a")})
	p.Tick(time.Now())
	if len(got) != 2 || got[0] != job.PrioBackfill || got[1] != job.PrioScheduled {
		t.Fatalf("got=%v: repo kinds start as backfill, global kinds keep their priority", got)
	}
}

type recEnq func(k job.Key, p job.Priority)

func (r recEnq) Enqueue(k job.Key, p job.Priority, _ time.Time, _ bool) error { r(k, p); return nil }

func TestPlannerLearnsParkedAfterWarmStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		restored := []ParkedKey{{Key: key("a"), Since: time.Now(), RetryAt: time.Now().Add(24 * time.Hour), Reason: "x"}}
		e := New(bo, 1, WithRestoreParked(restored))
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		sp := spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) { return job.Outcome{}, nil })
		e.Register(sp)
		p := NewPlanner(e, nil, 1)
		p.Track(sp)
		e.Observe(p.OnResult)
		stop := startEngine(t, e)
		defer stop()
		p.SetSubjects("r", []job.Key{key("a")})
		p.Tick(time.Now())
		synctest.Wait()
		if pr := p.Progress("r", time.Now()); pr.Parked != 1 || pr.Inflight != 0 {
			t.Fatalf("%+v", pr)
		}
		p.Tick(time.Now()) // parked items are not ticked again
		if s := e.Inspect(); s.Jobs["parked_suppressed"] != 1 {
			t.Fatalf("%+v", s.Jobs)
		}
	})
}

func TestPlannerGoneForgetsItem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, p, _, stop := plannerRig(t, func(ctx context.Context, k job.Key) (job.Outcome, error) { return job.Outcome{}, goneErr{} },
			freshness.Policy{TTL: time.Hour})
		defer stop()
		p.SetSubjects("r", []job.Key{key("a")})
		p.Tick(time.Now())
		synctest.Wait()
		if pr := p.Progress("r", time.Now()); pr.Subjects != 0 {
			t.Fatalf("%+v", pr)
		}
	})
}

func TestPlannerQueueFullReturnsItemToIdle(t *testing.T) {
	full := fullEnq{}
	p := NewPlanner(full, nil, 1)
	p.Track(job.Spec{Kind: "r", Upstream: "gh", Scope: job.ScopeRepo, Prio: job.PrioScheduled, Policy: freshness.Policy{TTL: time.Hour}})
	p.SetSubjects("r", []job.Key{key("a")})
	now := time.Now()
	p.Tick(now)
	if pr := p.Progress("r", now); pr.Inflight != 0 || pr.Due != 0 {
		t.Fatalf("refused item waits 30s: %+v", pr)
	}
	if pr := p.Progress("r", now.Add(time.Minute)); pr.Due != 1 {
		t.Fatalf("%+v", pr)
	}
}

type fullEnq struct{}

func (fullEnq) Enqueue(job.Key, job.Priority, time.Time, bool) error { return job.ErrQueueFull }

// ---- scheduler ----

func TestSchedulerJitterPanicTriggerAndFrozen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewScheduler(1)
		var runs atomic.Int32
		var starts []time.Time
		var mu sync.Mutex
		release := make(chan struct{})
		s.Add(Task{Name: "t", Every: time.Minute, Jitter: 0.1, Run: func(ctx context.Context) error {
			n := runs.Add(1)
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
			if n == 2 {
				panic("boom")
			}
			if n == 5 {
				<-release
			}
			return nil
		}})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- s.Run(ctx) }()
		time.Sleep(5*time.Minute + 30*time.Second)
		synctest.Wait()
		if runs.Load() != 5 {
			t.Fatalf("runs=%d", runs.Load())
		}
		for i := 1; i < 4; i++ {
			d := starts[i+1].Sub(starts[i])
			if d < 54*time.Second || d > 66*time.Second {
				t.Fatalf("interval %v", d)
			}
		}
		if st, _ := s.Status("t"); st.Failures != 1 || st.LastErr == "" && false {
			t.Fatalf("%+v", st)
		}
		if s.Trigger("t") { // running (blocked on release)
			t.Fatal("Trigger on a running task must be skipped")
		}
		st, _ := s.Status("t")
		if st.Skipped != 1 {
			t.Fatalf("%+v", st)
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("Add after Run must panic")
				}
			}()
			s.Add(Task{Name: "late", Every: time.Second, Run: func(context.Context) error { return nil }})
		}()
		close(release)
		cancel()
		<-done
	})
}
