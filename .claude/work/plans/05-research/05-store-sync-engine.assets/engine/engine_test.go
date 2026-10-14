package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

var bo = Backoff{Base: time.Second, Cap: time.Minute, MaxAttempts: 5}

func start(t *testing.T, e *Engine) (cancel func()) {
	ctx, c := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()
	return func() { c(); <-done }
}

func TestCoalesceQueuedAndDirtyWhileRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 2})
		var runs atomic.Int32
		release := make(chan struct{})
		e.Register("refresh", func(ctx context.Context, j Job) error {
			runs.Add(1)
			<-release
			return nil
		})
		stop := start(t, e)
		defer stop()
		j := Job{Key: "repo/a", Kind: "refresh", Upstream: "gh", Prio: PrioScheduled}
		for range 10 { // burst of 10 webhooks for one repo while it is queued -> 1 job
			e.Enqueue(j, time.Time{}, false)
		}
		synctest.Wait()
		if runs.Load() != 1 {
			t.Fatalf("runs=%d want 1", runs.Load())
		}
		for range 10 { // burst while running -> exactly one follow-up
			e.Enqueue(j, time.Time{}, false)
		}
		release <- struct{}{}
		synctest.Wait()
		if runs.Load() != 2 {
			t.Fatalf("runs=%d want 2 (one follow-up)", runs.Load())
		}
		release <- struct{}{}
		synctest.Wait()
		st, pending, _ := e.Inspect()
		if pending != 0 || st.Started != 2 {
			t.Fatalf("pending=%d started=%d", pending, st.Started)
		}
	})
}

func TestPerUpstreamConcurrencyAndPace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 3, Pace: 100 * time.Millisecond})
		e.AddUpstream("jenkins", UpstreamConfig{MaxConcurrent: 1})
		var cur, peak atomic.Int32
		var starts []time.Duration
		var smu sync.Mutex
		t0 := time.Now()
		e.Register("refresh", func(ctx context.Context, j Job) error {
			smu.Lock()
			starts = append(starts, time.Since(t0))
			smu.Unlock()
			n := cur.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(time.Second)
			cur.Add(-1)
			return nil
		})
		stop := start(t, e)
		defer stop()
		for i := range 9 {
			e.Enqueue(Job{Key: Key("r" + string(rune('a'+i))), Kind: "refresh", Upstream: "gh"}, time.Time{}, false)
		}
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if peak.Load() != 3 {
			t.Fatalf("peak=%d want 3", peak.Load())
		}
		if len(starts) != 9 || starts[1]-starts[0] != 100*time.Millisecond {
			t.Fatalf("starts=%v", starts)
		}
		t.Logf("start offsets: %v", starts)
	})
}

func TestTransientRetryBackoffThenSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 7)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		var at []time.Duration
		t0 := time.Now()
		e.Register("refresh", func(ctx context.Context, j Job) error {
			at = append(at, time.Since(t0))
			if len(at) < 4 {
				return errors.New("502")
			}
			return nil
		})
		stop := start(t, e)
		defer stop()
		e.Enqueue(Job{Key: "a", Kind: "refresh", Upstream: "gh"}, time.Time{}, false)
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if len(at) != 4 {
			t.Fatalf("attempts=%v", at)
		}
		for i := 1; i < len(at); i++ { // delay i in [base*2^(i-1)/2, base*2^(i-1)]
			lo, hi := bo.Base, bo.Base<<i
			if d := at[i] - at[i-1]; d < lo || d > hi {
				t.Fatalf("retry %d delay %v not in [%v,%v]", i, d, lo, hi)
			}
		}
		t.Logf("attempt times: %v", at)
	})
}

func TestRateLimitPausesWholeUpstreamWithoutConsumingAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 3)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		reset := time.Now().Add(10 * time.Minute)
		var first atomic.Bool
		var startedAfterReset, startedBefore atomic.Int32
		e.Register("refresh", func(ctx context.Context, j Job) error {
			if first.CompareAndSwap(false, true) {
				return &RateLimitedError{ResetAt: reset}
			}
			if time.Now().Before(reset) {
				startedBefore.Add(1)
			} else {
				startedAfterReset.Add(1)
			}
			return nil
		})
		stop := start(t, e)
		defer stop()
		for _, k := range []Key{"a", "b", "c", "d"} {
			e.Enqueue(Job{Key: k, Kind: "refresh", Upstream: "gh"}, time.Time{}, false)
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		st, pending, _ := e.Inspect()
		if startedBefore.Load() != 0 || startedAfterReset.Load() != 4 || pending != 0 {
			t.Fatalf("before=%d after=%d pending=%d st=%+v", startedBefore.Load(), startedAfterReset.Load(), pending, st)
		}
	})
}

func TestPermanentFailureIsParkedAndNotResurrectedByScheduler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 3)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		var runs atomic.Int32
		var parkedN atomic.Int32
		e.OnParked = func(Job, error) { parkedN.Add(1) }
		e.Register("refresh", func(ctx context.Context, j Job) error {
			if runs.Add(1) == 1 {
				return &PermanentError{Err: errors.New("404 repo deleted")}
			}
			return nil
		})
		stop := start(t, e)
		defer stop()
		j := Job{Key: "gone", Kind: "refresh", Upstream: "gh"}
		e.Enqueue(j, time.Time{}, false)
		synctest.Wait()
		e.Enqueue(j, time.Time{}, false) // scheduler tick: suppressed
		synctest.Wait()
		if runs.Load() != 1 || parkedN.Load() != 1 {
			t.Fatalf("runs=%d parked=%d", runs.Load(), parkedN.Load())
		}
		e.Enqueue(j, time.Time{}, true) // webhook "repository"/admin: force
		synctest.Wait()
		if runs.Load() != 2 {
			t.Fatalf("runs=%d", runs.Load())
		}
	})
}

func TestPanicBecomesParkedNotCrash(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 3)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		e.Register("refresh", func(ctx context.Context, j Job) error { panic("boom") })
		stop := start(t, e)
		defer stop()
		e.Enqueue(Job{Key: "x", Kind: "refresh", Upstream: "gh"}, time.Time{}, false)
		synctest.Wait()
		st, _, parked := e.Inspect()
		if st.Panics != 1 || parked != 1 {
			t.Fatalf("%+v parked=%d", st, parked)
		}
	})
}

func TestPriorityOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 3)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		var order []Key
		gate := make(chan struct{})
		e.Register("refresh", func(ctx context.Context, j Job) error {
			order = append(order, j.Key)
			if j.Key == "first" {
				<-gate
			}
			return nil
		})
		stop := start(t, e)
		defer stop()
		e.Enqueue(Job{Key: "first", Kind: "refresh", Upstream: "gh", Prio: PrioBackfill}, time.Time{}, false)
		synctest.Wait()
		e.Enqueue(Job{Key: "bf", Kind: "refresh", Upstream: "gh", Prio: PrioBackfill}, time.Time{}, false)
		e.Enqueue(Job{Key: "sch", Kind: "refresh", Upstream: "gh", Prio: PrioScheduled}, time.Time{}, false)
		e.Enqueue(Job{Key: "hook", Kind: "refresh", Upstream: "gh", Prio: PrioWebhook}, time.Time{}, false)
		close(gate)
		synctest.Wait()
		want := []Key{"first", "hook", "sch", "bf"}
		for i := range want {
			if order[i] != want[i] {
				t.Fatalf("order=%v", order)
			}
		}
	})
}

func TestShutdownGraceDrainsThenCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 3)
		e.Grace = 10 * time.Second
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 2})
		var shortDone, longCancelled atomic.Bool
		var shortCtxErr atomic.Value
		e.Register("short", func(ctx context.Context, j Job) error {
			time.Sleep(5 * time.Second) // finishes inside Grace and must NOT see a cancelled ctx
			shortCtxErr.Store(ctx.Err() == nil)
			shortDone.Store(true)
			return nil
		})
		e.Register("long", func(ctx context.Context, j Job) error {
			<-ctx.Done() // only cancelled once Grace expires
			longCancelled.Store(true)
			return ctx.Err()
		})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { e.Run(ctx); close(done) }()
		e.Enqueue(Job{Key: "s", Kind: "short", Upstream: "gh"}, time.Time{}, false)
		e.Enqueue(Job{Key: "l", Kind: "long", Upstream: "gh"}, time.Time{}, false)
		synctest.Wait()
		t0 := time.Now()
		cancel()
		e.Enqueue(Job{Key: "late", Kind: "short", Upstream: "gh"}, time.Time{}, false) // must not start while draining
		<-done
		if el := time.Since(t0); el != 10*time.Second {
			t.Fatalf("Run returned after %v, want exactly Grace", el)
		}
		if !shortDone.Load() || shortCtxErr.Load() != true || !longCancelled.Load() {
			t.Fatalf("short=%v ctxOK=%v longCancelled=%v", shortDone.Load(), shortCtxErr.Load(), longCancelled.Load())
		}
		if st, _, _ := e.Inspect(); st.Started != 2 {
			t.Fatalf("started=%d, the late job must not start during drain", st.Started)
		}
	})
}

func TestGracefulShutdownWaitsForInflight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 3)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		var finished atomic.Bool
		e.Register("refresh", func(ctx context.Context, j Job) error {
			select {
			case <-ctx.Done():
				finished.Store(true)
				return ctx.Err()
			case <-time.After(time.Hour):
				return nil
			}
		})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { e.Run(ctx); close(done) }()
		e.Enqueue(Job{Key: "a", Kind: "refresh", Upstream: "gh"}, time.Time{}, false)
		synctest.Wait()
		cancel()
		<-done
		if !finished.Load() {
			t.Fatal("Run returned before in-flight job observed cancellation")
		}
	})
}

func TestSchedulerJitterSkipAndPanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewScheduler(5)
		var runs atomic.Int32
		var at []time.Duration
		t0 := time.Now()
		s.Add(Task{Name: "poll", Every: time.Minute, Jitter: 0.1, Run: func(ctx context.Context) error {
			at = append(at, time.Since(t0))
			if runs.Add(1) == 2 {
				panic("bad")
			}
			return nil
		}})
		slowStarted := make(chan struct{}, 1)
		s.Add(Task{Name: "slow", Every: time.Second, Run: func(ctx context.Context) error {
			select {
			case slowStarted <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ctx.Err()
		}})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { s.Run(ctx); close(done) }()
		time.Sleep(10*time.Minute + time.Second)
		synctest.Wait()
		<-slowStarted
		if s.Trigger("slow") { // already running -> skipped
			t.Fatal("trigger on running task must be skipped")
		}
		for i := 1; i < len(at); i++ {
			if d := at[i] - at[i-1]; d < 54*time.Second || d > 66*time.Second {
				t.Fatalf("interval %v outside +-10%% of 60s", d)
			}
		}
		st, _ := s.Status("poll")
		if st.Failures != 1 || st.Runs < 9 {
			t.Fatalf("%+v", st)
		}
		t.Logf("poll run offsets: %v ; status=%+v", at, st)
		cancel()
		<-done
	})
}
