package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/lifecycle"
)

type rec struct {
	mu sync.Mutex
	ev []string
	at map[string]time.Duration
	t0 time.Time
}

func newRec() *rec { return &rec{at: map[string]time.Duration{}, t0: time.Now()} }

func (r *rec) mark(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ev = append(r.ev, name)
	r.at[name] = time.Since(r.t0)
}

func (r *rec) index(t *testing.T, name string) int {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.ev {
		if e == name {
			return i
		}
	}
	t.Fatalf("event %q missing in %v", name, r.events())
	return -1
}

func (r *rec) when(name string) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.at[name]
}

func (r *rec) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ev...)
}

// worker blocks until canceled, then takes quiesce to return.
func (r *rec) worker(name string, quiesce time.Duration) func(context.Context) error {
	return func(ctx context.Context) error {
		r.mark(name + ".start")
		<-ctx.Done()
		time.Sleep(quiesce)
		r.mark(name + ".stopped")
		return nil
	}
}

func (r *rec) closer(name string) func(context.Context) error {
	return func(context.Context) error { r.mark(name + ".close"); return nil }
}

// features builds the stages the product's wiring contributes, with the timeouts the wiring must use (design 16 section 4.2 S8).
func (r *rec) features(cfg Config, refreshQuiesce time.Duration) []lifecycle.Stage {
	return []lifecycle.Stage{
		{Name: "store", Run: r.worker("store", 0), Close: r.closer("store"), StopTimeout: cfg.Shutdown.StoreTimeout},
		{Name: "refresh", Run: r.worker("refresh", refreshQuiesce), StopTimeout: cfg.RefreshStop()},
	}
}

func (r *rec) parts(cfg Config, refreshQuiesce time.Duration) Parts {
	return Parts{
		TelemetryClose: r.closer("telemetry"),
		Features:       r.features(cfg, refreshQuiesce),
		Sweep:          r.worker("sweeper", 0),
		Draining:       func(context.Context) error { r.mark("draining"); return nil },
		ServersRun:     r.worker("servers", 300*time.Millisecond),
	}
}

func defaults(t *testing.T) Config {
	t.Helper()
	l, err := readConfig(loadOptions{})
	if err != nil || len(l.violations) > 0 {
		t.Fatalf("defaults do not load: %v %v", err, l.violations)
	}
	return l.cfg
}

func TestShutdownOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := defaults(t)
		r := newRec()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- lifecycle.Run(ctx, buildPlan(cfg, r.parts(cfg, 2*time.Second))) }()
		time.Sleep(time.Second)
		for _, s := range []string{"store.start", "refresh.start", "sweeper.start", "servers.start"} {
			r.index(t, s)
		}
		// lifecycle.Run launches every Run at once: "start order" is the order of the slice, which fixes the STOP order. No stage may
		// depend on another's Run having begun; what they need (store opened, warm load, registry frozen) exists before Run is called.
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run: %v", err)
		}
		chain := []string{"draining", "servers.stopped", "sweeper.stopped", "refresh.stopped", "store.stopped", "store.close", "telemetry.close"}
		for i := 1; i < len(chain); i++ {
			if r.index(t, chain[i-1]) >= r.index(t, chain[i]) {
				t.Errorf("%s must precede %s; events: %v", chain[i-1], chain[i], r.events())
			}
		}
		if got := r.when("servers.stopped") - r.when("draining"); got < cfg.Shutdown.DrainDelay {
			t.Errorf("servers stopped %s after the readiness drain; want >= drain delay %s (still serving)", got, cfg.Shutdown.DrainDelay)
		}
	})
}

// A refresh stage that never quiesces must not hold the process past the sum of the caps, and the store must not be closed under it.
func TestShutdownBudgetHolds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := defaults(t)
		r := newRec()
		p := r.parts(cfg, 0)
		release := make(chan struct{})
		p.Features[1].Run = func(ctx context.Context) error { <-ctx.Done(); <-release; return nil } // ignores its deadline
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- lifecycle.Run(ctx, buildPlan(cfg, p)) }()
		time.Sleep(time.Second)
		start := time.Now()
		cancel()
		err := <-done
		took := time.Since(start)
		close(release)
		if !errors.Is(err, lifecycle.ErrStopTimeout) {
			t.Fatalf("a stuck stage must be reported as ErrStopTimeout, got %v", err)
		}
		if took > cfg.ShutdownBudget() {
			t.Fatalf("shutdown took %s, budget %s", took, cfg.ShutdownBudget())
		}
		synctest.Wait()
		for _, e := range r.events() {
			if e == "refresh.stopped" {
				t.Error("the stuck stage cannot have stopped")
			}
		}
	})
}

// The number the operator reads (config) and the number the plan can reach must agree up to the documented margins.
func TestBudgetFormulaMatchesPlan(t *testing.T) {
	cfg := defaults(t)
	r := newRec()
	plan := buildPlan(cfg, r.parts(cfg, 0))
	if got, want := planCeiling(plan), cfg.ShutdownBudget()+planMargins; got != want {
		t.Fatalf("planCeiling = %s, ShutdownBudget+planMargins = %s", got, want)
	}
	// One servers stage, not one per listener: two stages would double the server grace (02 appendix C).
	n := 0
	for _, s := range plan.Stages {
		if s.Name == "servers" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d servers stages", n)
	}
	// The shipped defaults are 02's: 5 + 15 + 3 + 2 + refresh (15 + 3 + 1) = 44 s, one second inside the 45 s reference grace.
	if b := cfg.ShutdownBudget(); b != 44*time.Second || b > cfg.Shutdown.SupervisorGrace {
		t.Fatalf("default budget %s, supervisor grace %s", b, cfg.Shutdown.SupervisorGrace)
	}
	if len(cfg.startupWarnings()) != 1 { // the open admin listener, nothing else
		t.Errorf("default warnings: %v", cfg.startupWarnings())
	}
	cfg.Shutdown.SupervisorGrace = 30 * time.Second
	found := false
	for _, w := range cfg.startupWarnings() {
		found = found || len(w) > 0 && w[:5] == "worst"
	}
	if !found {
		t.Errorf("a supervisor grace below the budget must warn: %v", cfg.startupWarnings())
	}
}
