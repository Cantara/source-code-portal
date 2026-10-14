package webhook

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/scm"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Redeliverer is satisfied directly by *github.Client (App JWT endpoints). FailedDeliveries returns ONE
// delivery id per GUID, OLDEST first, for GUIDs that have no successful attempt since `since` (a redelivery
// keeps its GUID, so grouping is the adapter's job). When the adapter stopped early (page cap) it returns the
// ids it has TOGETHER WITH an error whose Truncated() bool is true; Redelivery then does not advance the
// watermark. Redeliver is POST /app/hook/deliveries/{id}/attempts.
type Redeliverer interface {
	FailedDeliveries(ctx context.Context, src scm.SourceID, since time.Time) ([]int64, error)
	Redeliver(ctx context.Context, src scm.SourceID, deliveryID int64) error
}

// Watermarks persists the per-source scan watermark (bridge over store.LoadTasks/SaveTask).
type Watermarks interface {
	Load(ctx context.Context, name string) (time.Time, error)
	Save(ctx context.Context, name string, t time.Time) error
}

// RedeliveryConfig is mounted at github.sources[].webhook.redelivery.
type RedeliveryConfig struct {
	Enabled   bool          `yaml:"enabled"`
	Interval  time.Duration `yaml:"interval"`    // 6h
	MaxPerRun int           `yaml:"max_per_run"` // 200
	Rate      time.Duration `yaml:"rate"`        // 1s between redeliveries
	MaxAge    time.Duration `yaml:"max_age"`     // 72h: github.com keeps 3 days of deliveries
}

func DefaultRedelivery() RedeliveryConfig {
	return RedeliveryConfig{Interval: 6 * time.Hour, MaxPerRun: 200, Rate: time.Second, MaxAge: 72 * time.Hour}
}

type RedeliverySource struct {
	ID  scm.SourceID
	Cfg RedeliveryConfig
}

const (
	RedeliveryKind = "webhook.redelivery"
	overlap        = time.Minute
)

type Redelivery struct {
	srcs []RedeliverySource
	rd   Redeliverer
	sink Sink
	wm   Watermarks
	log  *slog.Logger
	m    metric.Int64Counter
}

func NewRedelivery(srcs []RedeliverySource, rd Redeliverer, sink Sink, wm Watermarks, d Deps) *Redelivery {
	d = d.norm()
	r := &Redelivery{rd: rd, sink: sink, wm: wm, log: d.Log}
	for _, s := range srcs {
		if s.Cfg.Enabled {
			r.srcs = append(r.srcs, s)
		}
	}
	r.m, _ = d.Meter.Int64Counter(MetricRedelivery)
	return r
}

// Jobs returns the single global job (nil when no source enabled redelivery). Policy TTL is the shortest
// enabled interval; the run covers every enabled source.
func (r *Redelivery) Jobs() []job.Spec {
	if len(r.srcs) == 0 {
		return nil
	}
	ttl, per, rate := r.srcs[0].Cfg.Interval, 0, time.Duration(0)
	for _, s := range r.srcs {
		ttl = min(ttl, s.Cfg.Interval)
		per += s.Cfg.MaxPerRun
		rate = max(rate, s.Cfg.Rate)
	}
	// The Policy fields are assigned through the selector so that this package does not have to name
	// freshness.Policy (architecture 5.2 does not list freshness in the webhook row; no deviation needed).
	var sp job.Spec
	sp.Kind, sp.Upstream, sp.Scope, sp.Prio = RedeliveryKind, "github", job.ScopeGlobal, job.PrioScheduled
	sp.Policy.TTL, sp.Policy.Jitter = ttl, 0.1
	sp.Timeout = time.Duration(per)*rate + 2*time.Minute // redeliveries at Rate plus the paged listing
	sp.Run = r.Run
	return []job.Spec{sp}
}

func (r *Redelivery) count(ctx context.Context, src scm.SourceID, res string, n int) {
	r.m.Add(ctx, int64(n), metric.WithAttributes(attribute.String("source", string(src)), attribute.String("result", res)))
}

// Run scans every enabled source. A source's error stops the run (the engine classifies it: rate limit pause,
// transient retry, permanent park); sources already done keep their watermark.
func (r *Redelivery) Run(ctx context.Context, _ job.Key) (job.Outcome, error) {
	var total int
	for _, s := range r.srcs {
		n, err := r.runSource(ctx, s)
		total += n
		if err != nil {
			return job.Outcome{}, err
		}
	}
	return job.Outcome{Unchanged: total == 0}, nil
}

func (r *Redelivery) runSource(ctx context.Context, s RedeliverySource) (int, error) {
	name := RedeliveryKind + "/" + string(s.ID)
	scanStart := time.Now()
	var w time.Time
	if r.wm != nil {
		var err error
		if w, err = r.wm.Load(ctx, name); err != nil {
			return 0, err
		}
	}
	since := w
	switch {
	case w.IsZero():
		since = scanStart.Add(-24 * time.Hour)
	case scanStart.Sub(w) > s.Cfg.MaxAge:
		// deliveries older than the retention window cannot be redelivered: ask the reactors for a full reconcile
		since = scanStart.Add(-s.Cfg.MaxAge)
		r.count(ctx, s.ID, "gap", 1)
		ev := scm.Event{Kind: scm.EventInstallationRepos, Action: "resync", Source: s.ID, Delivery: "redelivery-gap/" + string(s.ID) + "/" + scanStart.UTC().Format(time.RFC3339)}
		if err := r.sink.Handle(ctx, ev); err != nil && (errors.Is(err, job.ErrQueueFull) || errors.Is(err, job.ErrClosed)) {
			return 0, err
		}
	}
	ids, err := r.rd.FailedDeliveries(ctx, s.ID, since)
	adapterTruncated := false
	if err != nil {
		var tr interface{ Truncated() bool }
		if !errors.As(err, &tr) || !tr.Truncated() {
			return 0, err
		}
		adapterTruncated = true
	}
	truncated := adapterTruncated || len(ids) > s.Cfg.MaxPerRun
	if len(ids) > s.Cfg.MaxPerRun {
		ids = ids[:s.Cfg.MaxPerRun] // oldest first: the oldest failures are the ones about to age out of the window
	}
	done := 0
	for i, id := range ids {
		if i > 0 {
			t := time.NewTimer(s.Cfg.Rate)
			select {
			case <-ctx.Done():
				t.Stop()
				return done, ctx.Err()
			case <-t.C:
			}
		}
		switch err := r.rd.Redeliver(ctx, s.ID, id); {
		case err == nil:
			done++
			r.count(ctx, s.ID, "redelivered", 1)
		case job.IsGone(err): // expired between list and redeliver
			r.count(ctx, s.ID, "gone", 1)
		default:
			r.count(ctx, s.ID, "failed", 1)
			return done, err
		}
	}
	if !truncated && r.wm != nil {
		if err := r.wm.Save(ctx, name, scanStart.Add(-overlap)); err != nil {
			r.log.LogAttrs(ctx, slog.LevelWarn, "redelivery watermark not saved", slog.String("source", string(s.ID)), slog.String("err", err.Error()))
		}
	}
	return done, nil
}
