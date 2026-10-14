package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/lifecycle"
	"github.com/cantara/source-code-portal/internal/platform/logx"
)

// Parts is what the serve command hands to buildPlan: closures over components built in the startup sequence (design 16
// section 4.2). buildPlan owns ORDER and TIMEOUTS only; it knows no component type, so the shutdown order and the shutdown
// budget are tested under synctest without a store, a network or GitHub.
type Parts struct {
	TelemetryClose func(context.Context) error // flush providers with their own context; Close only (started first, stopped last)
	Features       []lifecycle.Stage           // store, refresh ... in START order; their StopTimeouts come from the wiring (store_timeout, RefreshStop)
	Sweep          func(context.Context) error // httpx.RunSweepers over every limiter; the same stage also answers SIGHUP (Reload)
	Hup            <-chan struct{}             // Env.Reload; nil = no reload
	Reload         func(context.Context) error // Wired.Reload; nil = nothing to reload
	Draining       func(context.Context) error // readiness -> 503, SSE retry; runs FIRST at shutdown while everything still serves
	ServersRun     func(context.Context) error // httpx.ServeAll(ctx, server_timeout, public, admin): ONE stage, both listeners stop together

	Log        *slog.Logger
	OnShutdown func(cause error)
	Observe    func(stage, phase string, d time.Duration, err error)
}

// buildPlan returns the lifecycle plan. Start order is the slice order; stop order is its reverse:
//
//	start:  telemetry -> features (store, refresh) -> sweeper -> readiness -> servers
//	stop:   drain hooks (readiness off) -> DrainDelay -> servers (public and admin together) -> readiness -> sweeper -> refresh -> store -> telemetry
//
// The servers start last and stop first, so nothing can enqueue work while the engine quiesces; the engine joins before the
// store stage's Close runs the final plan flush; telemetry outlives everything so the shutdown itself is observable.
func buildPlan(cfg Config, p Parts) lifecycle.Plan {
	s := cfg.Shutdown
	st := []lifecycle.Stage{{Name: "telemetry", Close: p.TelemetryClose, StopTimeout: s.TelemetryTimeout}}
	st = append(st, p.Features...)
	st = append(st,
		lifecycle.Stage{Name: "ratelimit-sweeper", Run: housekeeping(p), StopTimeout: sweeperStop},
		lifecycle.Stage{Name: "readiness", Drain: p.Draining, StopTimeout: readinessCap},
		lifecycle.Stage{Name: "servers", Run: p.ServersRun, StopTimeout: s.ServerTimeout + serversJoin},
	)
	return lifecycle.Plan{Stages: st, DrainDelay: s.DrainDelay, Log: p.Log, OnShutdown: p.OnShutdown, Observe: p.Observe}
}

// planCeiling is the longest a plan can take if EVERY stage runs into its StopTimeout: the drain delay plus, per stage, the largest
// of its phase timeouts (a stage whose Run does not stop is reported ErrStopTimeout and its Close is skipped, so one stage never
// costs two). Config.ShutdownBudget plus planMargins is the same number computed from the keys; a test holds them equal.
func planCeiling(p lifecycle.Plan) time.Duration {
	d := p.DrainDelay
	for _, s := range p.Stages {
		if s.Run != nil || s.Drain != nil || s.Close != nil {
			d += s.StopTimeout
		}
	}
	return d
}

// housekeeping is the Run of the "ratelimit-sweeper" stage: the limiter sweepers plus the SIGHUP loop. They share a stage because a
// further stage would add its stop cap to the shutdown budget of section 8.3 for a loop that stops in microseconds. Either half
// ending ends the stage; a Reload error is logged and the loop continues (a failed key rotation keeps the old key in service, 05 4.5).
func housekeeping(p Parts) func(context.Context) error {
	return func(ctx context.Context) error {
		if p.Hup == nil || p.Reload == nil {
			return p.Sweep(ctx)
		}
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- p.Sweep(ctx) }()
		for {
			select {
			case <-ctx.Done():
				return <-done
			case err := <-done:
				return err
			case <-p.Hup:
				if err := p.Reload(ctx); err != nil && p.Log != nil {
					p.Log.WarnContext(ctx, "reload", slog.String("trigger", "SIGHUP"), logx.Err(err))
				} else if p.Log != nil {
					p.Log.InfoContext(ctx, "reload", slog.String("trigger", "SIGHUP"))
				}
			}
		}
	}
}
