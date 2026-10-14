package refresh

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
)

// Regression tests added by the independent verification of design unit 04 (2026-10-14). Each one failed on
// the first prototype (see the verification log in the design document).

// One secondary-limit event hits every job that was in flight when it began; the floor must not escalate
// once per job (4 jobs used to give 60+120+240+480 s = an 8 minute pause for one event).
func TestRateLimitOneEventManyInFlightJobsDoesNotEscalate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 3)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 4})
		var n atomic.Int32
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			if n.Add(1) <= 4 {
				time.Sleep(time.Second) // all four in flight together
				return job.Outcome{}, rlErr{time.Time{}}
			}
			return job.Outcome{}, nil
		}))
		stop := startEngine(t, e)
		defer stop()
		for _, r := range []string{"a", "b", "c", "d"} {
			e.Enqueue(key(r), job.PrioScheduled, time.Time{}, false)
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		s := e.Inspect().Upstreams["gh"]
		if d := time.Until(s.PausedUntil); d > 62*time.Second || s.RateLimitStreak != 1 {
			t.Fatalf("one limit event paused %v, streak %d", d, s.RateLimitStreak)
		}
	})
}

type denyGate struct{ deny atomic.Bool }

func (g *denyGate) Admit(up string, p job.Priority) time.Duration {
	if g.deny.Load() && p == job.PrioBackfill {
		return time.Hour
	}
	return 0
}

// An entry promoted from the delayed heap in the same pass must still pass the gate (it used to bypass it
// because Admit was only asked about classes whose ready heap was non-empty before the promotion).
func TestGateAppliesToPromotedDelayedEntries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := &denyGate{}
		e := New(bo, 3, WithGate(g))
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 4})
		var runs atomic.Int32
		e.Register(spec("r", job.PrioBackfill, func(ctx context.Context, k job.Key) (job.Outcome, error) { runs.Add(1); return job.Outcome{}, nil }))
		stop := startEngine(t, e)
		defer stop()
		g.deny.Store(true)
		e.Enqueue(key("a"), job.PrioBackfill, time.Now().Add(time.Minute), false) // delayed backfill
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if runs.Load() != 0 {
			t.Fatalf("gate bypassed: runs=%d", runs.Load())
		}
		g.deny.Store(false)
		time.Sleep(10 * time.Second) // GateRecheck 5 s
		synctest.Wait()
		if runs.Load() != 1 {
			t.Fatalf("not released after the gate opened: runs=%d", runs.Load())
		}
	})
}

// A panicking hook (a store write in a bridge, the planner) must not kill the process; the other hooks and
// the other jobs keep running.
func TestHookPanicIsContained(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var parkedCalls, ok atomic.Int32
		e := New(bo, 1, WithOnParked(func(ParkedKey) { parkedCalls.Add(1); panic("store exploded") }))
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 2})
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			if k == key("bad") {
				return job.Outcome{}, permErr{}
			}
			ok.Add(1)
			return job.Outcome{}, nil
		}))
		var obs atomic.Int32
		e.Observe(func(job.Key, job.Outcome, error) { obs.Add(1); panic("observer exploded") })
		stop := startEngine(t, e)
		defer stop()
		e.Enqueue(key("bad"), job.PrioScheduled, time.Time{}, false)
		e.Enqueue(key("good"), job.PrioScheduled, time.Time{}, false)
		synctest.Wait()
		if parkedCalls.Load() != 1 || ok.Load() != 1 || obs.Load() != 2 {
			t.Fatalf("parked=%d ok=%d observed=%d", parkedCalls.Load(), ok.Load(), obs.Load())
		}
	})
}

type rlPermErr struct{ at time.Time }

func (rlPermErr) Error() string        { return "403 secondary limit on a blocked repository" }
func (rlPermErr) Permanent() bool      { return true }
func (e rlPermErr) RetryAt() time.Time { return e.at }

// Unit 01 JB-05: Gone > Permanent > RateLimited, yet the rate-limit signal is never lost.
func TestPermanentAndRateLimitedParksAndStillPauses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			return job.Outcome{}, rlPermErr{time.Now().Add(5 * time.Minute)}
		}))
		stop := startEngine(t, e)
		defer stop()
		e.Enqueue(key("a"), job.PrioScheduled, time.Time{}, false)
		synctest.Wait()
		s := e.Inspect()
		if s.Parked != 1 || s.Upstreams["gh"].PausedUntil.IsZero() || s.Jobs["parked"] != 1 {
			t.Fatalf("%+v", s)
		}
	})
}

// Kernel priorities are 1-based (unit 01): the numeric value must never index an array, and the unset zero
// value is refused loudly.
func TestPrioritiesAreOneBasedAndZeroIsInvalid(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(bo, 1)
		e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
		var order []job.Key
		var mu sync.Mutex
		e.Register(spec("r", job.PrioScheduled, func(ctx context.Context, k job.Key) (job.Outcome, error) {
			mu.Lock()
			order = append(order, k)
			mu.Unlock()
			return job.Outcome{}, nil
		}))
		if err := e.Enqueue(key("zero"), 0, time.Time{}, false); !errors.Is(err, ErrInvalidPriority) {
			t.Fatalf("priority 0: %v", err)
		}
		if err := e.Enqueue(key("big"), job.Priority(200), time.Time{}, false); err != nil { // clamped to backfill
			t.Fatal(err)
		}
		e.Enqueue(key("backfill"), job.PrioBackfill, time.Time{}, false)
		e.Enqueue(key("webhook"), job.PrioWebhook, time.Time{}, false)
		e.Enqueue(key("interactive"), job.PrioInteractive, time.Time{}, false)
		stop := startEngine(t, e)
		defer stop()
		synctest.Wait()
		want := []job.Key{key("interactive"), key("webhook"), key("big"), key("backfill")}
		if len(order) != 4 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] || order[3] != want[3] {
			t.Fatalf("order %v want %v", order, want)
		}
	})
}

func TestWithTunablesKeepsDefaultsForZeroFields(t *testing.T) {
	o := buildOptions([]Option{WithGrace(7 * time.Second), WithTunables(Tunables{AbandonAfter: time.Second, MaxKeys: 5})})
	d := DefaultTunables()
	if o.t.AbandonAfter != time.Second || o.t.MaxKeys != 5 || o.t.Heartbeat != d.Heartbeat || o.t.Grace != 7*time.Second {
		t.Fatalf("%+v", o.t)
	}
}

// Wiring rule of design section 2.5: the first jobs of a process (catalog.reconcile) go through the planner,
// which marks the idle item in flight. Handing them to the engine directly lets the first tick enqueue the
// item again while it runs: one needless dirty follow-up (a second full reconcile).
func TestFirstGlobalJobThroughPlannerIsNotRunTwice(t *testing.T) {
	for _, viaPlanner := range []bool{true, false} {
		synctest.Test(t, func(t *testing.T) {
			var runs atomic.Int32
			e := New(bo, 1)
			e.AddUpstream("gh", UpstreamConfig{MaxConcurrent: 1})
			sp := job.Spec{Kind: "g", Upstream: "gh", Scope: job.ScopeGlobal, Prio: job.PrioScheduled,
				Policy: freshness.Policy{TTL: time.Hour}, Run: func(ctx context.Context, k job.Key) (job.Outcome, error) {
					runs.Add(1)
					time.Sleep(10 * time.Second)
					return job.Outcome{}, nil
				}}
			e.Register(sp)
			p := NewPlanner(e, nil, 1)
			p.Track(sp)
			e.Observe(p.OnResult)
			stop := startEngine(t, e)
			defer stop()
			first := job.NewKey("g", "_")
			if viaPlanner {
				p.Enqueue(first, job.PrioWebhook, time.Time{}, false)
			} else {
				e.Enqueue(first, job.PrioWebhook, time.Time{}, false)
			}
			time.Sleep(time.Second)
			p.Tick(time.Now()) // the 15 s task ticks while the job runs
			time.Sleep(time.Minute)
			synctest.Wait()
			want := int32(1)
			if !viaPlanner {
				want = 2 // documents the defect of the wrong wiring
			}
			if runs.Load() != want {
				t.Fatalf("viaPlanner=%v runs=%d want %d", viaPlanner, runs.Load(), want)
			}
		})
	}
}
