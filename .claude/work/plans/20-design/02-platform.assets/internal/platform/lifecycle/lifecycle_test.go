package lifecycle_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/lifecycle"
)

type rec struct {
	mu    sync.Mutex
	start time.Time
	ev    []string
}

func (r *rec) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ev = append(r.ev, fmt.Sprintf("%s@%ds", s, int(time.Since(r.start).Seconds())))
}
func (r *rec) String() string { return strings.Join(r.ev, " ") }

func stage(r *rec, name string, stopAfter time.Duration) lifecycle.Stage {
	return lifecycle.Stage{
		Name: name,
		Run: func(ctx context.Context) error {
			<-ctx.Done()
			time.Sleep(stopAfter)
			r.add(name + ".run-returned")
			return nil
		},
		Drain: func(context.Context) error { r.add(name + ".drain"); return nil },
		Close: func(context.Context) error { r.add(name + ".close"); return nil },
	}
}

func TestOrderedShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &rec{start: time.Now()}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			done <- lifecycle.Run(ctx, lifecycle.Plan{DrainDelay: 5 * time.Second, Stages: []lifecycle.Stage{
				stage(r, "telemetry", 0), stage(r, "store", 2*time.Second), stage(r, "refresh", 3*time.Second), stage(r, "servers", time.Second),
			}})
		}()
		time.Sleep(60 * time.Second)
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		want := "servers.drain@60s refresh.drain@60s store.drain@60s telemetry.drain@60s " +
			"servers.run-returned@66s servers.close@66s refresh.run-returned@69s refresh.close@69s " +
			"store.run-returned@71s store.close@71s telemetry.run-returned@71s telemetry.close@71s"
		if r.String() != want {
			t.Fatalf("\n got %s\nwant %s", r, want)
		}
	})
}

func TestFailedStageStartsShutdownAndIsFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		boom := errors.New("boom")
		r := &rec{start: time.Now()}
		failing := lifecycle.Stage{Name: "engine", Run: func(context.Context) error { time.Sleep(10 * time.Second); return boom },
			Close: func(context.Context) error { r.add("engine.close"); return nil }}
		err := lifecycle.Run(t.Context(), lifecycle.Plan{Stages: []lifecycle.Stage{stage(r, "store", 0), failing}})
		var se *lifecycle.StageError
		if !errors.As(err, &se) || se.Stage != "engine" || se.Phase != "run" || !errors.Is(err, boom) {
			t.Fatalf("err=%v", err)
		}
		if !strings.HasPrefix(err.Error(), "stage engine run") {
			t.Fatalf("failed stage must come first: %v", err)
		}
		// exact: the failed stage is not waited on (its Close runs at once), then the healthy one stops; nothing takes longer than the failure itself
		if got := r.String(); got != "store.drain@10s engine.close@10s store.run-returned@10s store.close@10s" {
			t.Fatal(got)
		}
	})
}

func TestStageIgnoringCancelIsSkippedAndBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		closed := false
		stuck := lifecycle.Stage{Name: "stuck", StopTimeout: 3 * time.Second,
			Run:   func(context.Context) error { <-release; return nil },
			Close: func(context.Context) error { closed = true; return nil }}
		ctx, cancel := context.WithCancel(t.Context())
		start := time.Now()
		go func() { time.Sleep(time.Second); cancel() }()
		err := lifecycle.Run(ctx, lifecycle.Plan{Stages: []lifecycle.Stage{stuck}})
		if !errors.Is(err, lifecycle.ErrStopTimeout) || closed || time.Since(start) != 4*time.Second {
			t.Fatalf("err=%v closed=%v took=%v", err, closed, time.Since(start))
		}
		close(release)
	})
}

func TestEarlyNilExitAndPanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		err := lifecycle.Run(t.Context(), lifecycle.Plan{Stages: []lifecycle.Stage{{Name: "x", Run: func(context.Context) error { return nil }}}})
		if !errors.Is(err, lifecycle.ErrStageExited) {
			t.Fatal(err)
		}
		err = lifecycle.Run(t.Context(), lifecycle.Plan{Stages: []lifecycle.Stage{{Name: "p", Run: func(context.Context) error { panic("x") }}}})
		var pe *lifecycle.PanicError
		if !errors.As(err, &pe) {
			t.Fatal(err)
		}
	})
}

func TestStagesNeverSeeRootCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		sawCancelAtDrain := false
		s := lifecycle.Stage{Name: "s", Run: func(c context.Context) error { <-c.Done(); return nil },
			Drain: func(c context.Context) error { sawCancelAtDrain = c.Err() != nil; return nil }}
		go func() { time.Sleep(time.Second); cancel() }()
		if err := lifecycle.Run(ctx, lifecycle.Plan{Stages: []lifecycle.Stage{s}}); err != nil || sawCancelAtDrain {
			t.Fatal(err, sawCancelAtDrain)
		}
	})
}

func TestWorstCaseOfTheDefaultsFitsTheReferenceTerminationGrace(t *testing.T) {
	c := lifecycle.Config{DrainDelay: 5 * time.Second, ServerTimeout: 15 * time.Second, StoreTimeout: 3 * time.Second, TelemetryTimeout: 2 * time.Second}
	if got := c.WorstCase(19 * time.Second); got != 44*time.Second || got > 45*time.Second {
		t.Fatalf("worst case %v must fit terminationGracePeriodSeconds 45 (04: refresh stage StopTimeout 19 s)", got)
	}
}

func TestDrainAndCloseHooksArePanicSafeAndBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		block := make(chan struct{})
		s := lifecycle.Stage{Name: "h", StopTimeout: 2 * time.Second,
			Run:   func(c context.Context) error { <-c.Done(); return nil },
			Drain: func(context.Context) error { panic("drain") },
			Close: func(context.Context) error { <-block; return nil }}
		ctx, cancel := context.WithCancel(t.Context())
		go func() { time.Sleep(time.Second); cancel() }()
		start := time.Now()
		err := lifecycle.Run(ctx, lifecycle.Plan{Stages: []lifecycle.Stage{s}})
		var pe *lifecycle.PanicError
		var se *lifecycle.StageError
		if !errors.As(err, &pe) || !errors.As(err, &se) || !errors.Is(err, lifecycle.ErrStopTimeout) {
			t.Fatalf("a panicking Drain and a stuck Close must both be reported: %v", err)
		}
		if d := time.Since(start); d != 3*time.Second { // 1 s until the signal + 0 (panic) + 2 s Close bound
			t.Fatalf("hooks must be bounded by StopTimeout: %v", d)
		}
		close(block)
	})
}
