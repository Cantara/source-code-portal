// Package refresh is the sync engine: keyed coalescing queue, per-upstream dispatch, level-triggered
// planner, local scheduler and budget gate. It imports only job, freshness (and logx in production).
package refresh

import (
	"io"
	"log/slog"
	"time"

	"github.com/cantara/source-code-portal/internal/job"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Deps is handed to every constructor by cmd/scp (G11): logger, OpenTelemetry API meter and tracer.
// A zero Deps is valid (discard logger, no-op instruments), which is what unit tests use.
type Deps struct {
	Log    *slog.Logger
	Meter  metric.Meter
	Tracer trace.Tracer
}

func (d Deps) norm() Deps {
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if d.Meter == nil {
		d.Meter = noop.NewMeterProvider().Meter("refresh")
	}
	if d.Tracer == nil {
		d.Tracer = tracenoop.NewTracerProvider().Tracer("refresh")
	}
	return d
}

// Tunables are the engine knobs that have defaults and a config key (section 6 of the design document).
type Tunables struct {
	Grace        time.Duration // in-flight drain after shutdown starts (15s)
	AbandonAfter time.Duration // extra wait after Grace + cancel before Run gives up on a job (3s)
	MaxKeys      int           // keyed-table cap (50000)
	ParkRetry    time.Duration // retry_at probe interval for parked keys (24h)
	StuckAfter   time.Duration // a job that ignores its deadline this long is Stuck (5s)
	GateRecheck  time.Duration // upper bound of a gate denial before the dispatcher asks again (5s)
	Heartbeat    time.Duration // dispatcher wakes at least this often (10s)
	MaxPause     time.Duration // clamp of a RetryAt far in the future (65m)
	RateLimitMin time.Duration // floor of a repeated rate-limit pause when RetryAt is not in the future (60s)
	RateLimitMax time.Duration // cap of that floor (15m)
}

func DefaultTunables() Tunables {
	return Tunables{
		Grace: 15 * time.Second, AbandonAfter: 3 * time.Second, MaxKeys: 50000, ParkRetry: 24 * time.Hour,
		StuckAfter: 5 * time.Second, GateRecheck: 5 * time.Second, Heartbeat: 10 * time.Second,
		MaxPause: 65 * time.Minute, RateLimitMin: 60 * time.Second, RateLimitMax: 15 * time.Minute,
	}
}

type options struct {
	t             Tunables
	deps          Deps
	gate          Gate
	onParked      func(ParkedKey)
	onUnparked    func(k job.Key, reason string)
	onExhausted   func(k job.Key, attempts int, err error)
	restoreParked []ParkedKey
}

// Option configures New, NewPlanner and NewScheduler (options that do not apply are ignored).
type Option func(*options)

func WithDeps(d Deps) Option           { return func(o *options) { o.deps = d } }
func WithGate(g Gate) Option           { return func(o *options) { o.gate = g } }
func WithGrace(d time.Duration) Option { return func(o *options) { o.t.Grace = d } }
func WithMaxKeys(n int) Option         { return func(o *options) { o.t.MaxKeys = n } }

// WithTunables overrides the non-zero fields of t; a zero field keeps its default (a zero Heartbeat would spin
// the dispatcher, a zero Grace would cancel every job at shutdown at once).
func WithTunables(t Tunables) Option { return func(o *options) { o.t = mergeTunables(o.t, t) } }

func mergeTunables(base, t Tunables) Tunables {
	pick := func(dst *time.Duration, v time.Duration) {
		if v > 0 {
			*dst = v
		}
	}
	pick(&base.Grace, t.Grace)
	pick(&base.AbandonAfter, t.AbandonAfter)
	pick(&base.ParkRetry, t.ParkRetry)
	pick(&base.StuckAfter, t.StuckAfter)
	pick(&base.GateRecheck, t.GateRecheck)
	pick(&base.Heartbeat, t.Heartbeat)
	pick(&base.MaxPause, t.MaxPause)
	pick(&base.RateLimitMin, t.RateLimitMin)
	pick(&base.RateLimitMax, t.RateLimitMax)
	if t.MaxKeys > 0 {
		base.MaxKeys = t.MaxKeys
	}
	return base
}
func WithOnParked(f func(ParkedKey)) Option { return func(o *options) { o.onParked = f } }
func WithOnUnparked(f func(k job.Key, reason string)) Option {
	return func(o *options) { o.onUnparked = f }
}
func WithOnExhausted(f func(k job.Key, attempts int, err error)) Option {
	return func(o *options) { o.onExhausted = f }
}
func WithRestoreParked(p []ParkedKey) Option { return func(o *options) { o.restoreParked = p } }

func buildOptions(opts []Option) options {
	o := options{t: DefaultTunables()}
	for _, f := range opts {
		f(&o)
	}
	o.deps = o.deps.norm()
	return o
}
