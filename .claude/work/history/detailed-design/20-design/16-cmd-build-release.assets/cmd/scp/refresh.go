package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/cmd/scp/internal/bridge"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/platform/lifecycle"
	"github.com/cantara/source-code-portal/internal/refresh"
)

// plannerTickTask is the scheduler task that runs Planner.Tick; bridge.PlannerTickTask is the same name (Tracker nudges it).
const plannerTickTask = bridge.PlannerTickTask

// refreshIn is what newRefreshCore needs. Only Cfg and Seed are required; the rest is nil in tests that do not persist anything.
type refreshIn struct {
	Cfg         RefreshConfig
	Seed        uint64
	Deps        refresh.Deps
	Gate        refresh.Gate        // bridge.RateGate over the GitHub client; nil admits everything (tests)
	Plans       refresh.PlanSink    // bridge.PlanSink; nil keeps plan state in memory only
	Parked      *bridge.ParkedSink  // persists parked keys; nil does not
	Restore     []refresh.ParkedKey // warm start: keys parked before the restart
	Now         func() time.Time    // planner clock for the tick task; nil is time.Now
	OnExhausted func(k job.Key, attempts int, err error)
}

// refreshCore is startup step S8's sync machinery: the engine, the planner that is the Enqueuer of every caller that is not a
// Spec's own Run (04 D2: reactors, features, ops retry, the first jobs) and the scheduler of local housekeeping tasks.
type refreshCore struct {
	Engine  *refresh.Engine
	Planner *refresh.Planner
	Sched   *refresh.Scheduler

	cfg       RefreshConfig
	upstreams map[string]bool
	kinds     map[string]bool
}

// derive gives the engine, the planner and the scheduler independent jitter streams from the one process seed (splitmix64).
func derive(seed, n uint64) uint64 {
	z := seed + n*0x9e3779b97f4a7c15
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return z ^ z>>31
}

// newRefreshCore builds the trio from the configuration. Every key of RefreshConfig is read here.
func newRefreshCore(in refreshIn) (*refreshCore, error) {
	c := in.Cfg
	var opts []refresh.Option
	opts = append(opts, refresh.WithDeps(in.Deps), refresh.WithGrace(c.Grace), refresh.WithMaxKeys(c.MaxKeys),
		refresh.WithTunables(refresh.Tunables{ParkRetry: c.Parking.Retry}))
	if in.Gate != nil {
		opts = append(opts, refresh.WithGate(in.Gate))
	}
	if in.Parked != nil {
		opts = append(opts, refresh.WithOnParked(in.Parked.Park), refresh.WithOnUnparked(in.Parked.Unpark))
	}
	if in.OnExhausted != nil {
		opts = append(opts, refresh.WithOnExhausted(in.OnExhausted))
	}
	if len(in.Restore) > 0 {
		opts = append(opts, refresh.WithRestoreParked(in.Restore))
	}
	eng := refresh.New(refresh.Backoff{Base: c.Backoff.Base, Cap: c.Backoff.Cap, MaxAttempts: c.Backoff.MaxAttempts}, derive(in.Seed, 0), opts...)

	r := &refreshCore{Engine: eng, cfg: c, upstreams: map[string]bool{}, kinds: map[string]bool{}}
	names := make([]string, 0, len(c.Upstreams))
	for n := range c.Upstreams {
		names = append(names, n)
	}
	slices.Sort(names) // registration order is part of /status: deterministic
	for _, n := range names {
		u := c.Upstreams[n]
		eng.AddUpstream(n, refresh.UpstreamConfig{MaxConcurrent: u.Concurrency, Pace: u.Pace, JobTimeout: u.JobTimeout})
		r.upstreams[n] = true
	}

	sink := in.Plans
	if sink == nil {
		sink = discardPlans{}
	}
	r.Planner = refresh.NewPlanner(eng, sink, derive(in.Seed, 1), refresh.WithDeps(in.Deps))
	eng.Observe(r.Planner.OnResult)
	r.Sched = refresh.NewScheduler(derive(in.Seed, 2), refresh.WithDeps(in.Deps))
	now := in.Now
	if now == nil {
		now = time.Now
	}
	r.Sched.Add(refresh.Task{Name: plannerTickTask, Every: c.PlannerTick, Run: func(context.Context) error { r.Planner.Tick(now()); return nil }}) //nolint:contextcheck // Planner.Tick takes no context by design (04): it only enqueues
	return r, nil
}

type discardPlans struct{}

func (discardPlans) SaveState(job.Key, refresh.PlanState) {}

// Register adds specs to the engine and the planner. It returns an error where the library would panic: a startup problem
// must be a refusal with a message (architecture 10.1 step 6), never a stack trace.
func (r *refreshCore) Register(specs ...job.Spec) error {
	var errs []error
	for _, s := range specs {
		switch {
		case s.Validate() != nil:
			errs = append(errs, s.Validate())
		case r.kinds[s.Kind]:
			errs = append(errs, fmt.Errorf("job kind %q is registered twice", s.Kind))
		case !r.upstreams[s.Upstream]:
			errs = append(errs, fmt.Errorf("job kind %q names upstream %q, which refresh.upstreams does not list (add it to the configuration defaults)", s.Kind, s.Upstream))
		default:
			r.Engine.Register(s)
			r.Planner.Track(s)
			r.kinds[s.Kind] = true
		}
	}
	return errors.Join(errs...)
}

// Stage is the lifecycle stage "refresh": the engine dispatcher and the scheduler. Its stop timeout is RefreshStop: the engine's
// grace for in-flight jobs plus its abandon wait plus one second (04 section 2.5). Run returns when both have returned; if one
// ends early (a bug: both run until canceled) the other is canceled and the first error is returned.
func (r *refreshCore) Stage() lifecycle.Stage {
	return lifecycle.Stage{Name: "refresh", StopTimeout: r.cfg.stop(), Run: func(ctx context.Context) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		var (
			wg    sync.WaitGroup
			mu    sync.Mutex
			first error
		)
		note := func(err error) {
			mu.Lock()
			if first == nil && err != nil {
				first = err
			}
			mu.Unlock()
			cancel()
		}
		wg.Go(func() { note(r.Engine.Run(ctx)) })
		wg.Go(func() { note(r.Sched.Run(ctx)) })
		wg.Wait()
		return first
	}}
}

// stop is RefreshStop on the section type (Config.RefreshStop delegates here).
func (c RefreshConfig) stop() time.Duration { return c.Grace + abandonAfter + time.Second }
