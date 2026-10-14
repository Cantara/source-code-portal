package readmodel

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Projector derives cross-repository indexes (latest N, membership) from the candidate. Pure and deterministic; runs under
// the publish mutex and must not block, do I/O or call back into the Publisher.
type Projector func(b *Builder) error

// Validator gates publication (referential integrity). A failing candidate is not published.
type Validator func(*Snapshot) error

// Deps are injected by cmd/scp (G11). Zero values are valid: no-op meter and tracer, discard logger.
type Deps struct {
	Meter  metric.Meter
	Tracer trace.Tracer
	Log    *slog.Logger
}

// PublishStatus is what /status shows.
type PublishStatus struct {
	Version        uint64
	BuiltAt        time.Time
	Published      uint64
	Rejected       uint64 // projector or validator failure
	Aborted        uint64 // the Apply callback returned an error (for example ErrUnknownRepo)
	LastRejected   error
	LastRejectedAt time.Time
}

type Publisher struct {
	reg        *Registry
	cur        atomic.Pointer[Snapshot]
	mu         sync.Mutex // serialises builders
	projectors []Projector
	validators []Validator
	log        *slog.Logger
	tracer     trace.Tracer

	pubTotal metric.Int64Counter
	pubSecs  metric.Float64Histogram

	stMu sync.Mutex
	st   PublishStatus
}

const MetricPublishTotal, MetricPublishSeconds = "scp_snapshot_publish_total", "scp_snapshot_publish_seconds"

// NewPublisher freezes reg (idempotent), installs the registry's built-in validator (every entry has an existence value)
// and publishes the empty version-0 snapshot.
func NewPublisher(reg *Registry, projectors []Projector, validators []Validator, d Deps) *Publisher {
	reg.Freeze()
	if d.Meter == nil {
		d.Meter = metricnoop.NewMeterProvider().Meter("readmodel")
	}
	if d.Tracer == nil {
		d.Tracer = tracenoop.NewTracerProvider().Tracer("readmodel")
	}
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	p := &Publisher{reg: reg, projectors: slices.Clone(projectors), validators: slices.Clone(validators), log: d.Log, tracer: d.Tracer}
	p.cur.Store(&Snapshot{reg: reg, repos: map[EntryID]*entry{}, globals: make([]any, len(reg.globals)), builtAt: time.Now()})
	p.pubTotal, _ = d.Meter.Int64Counter(MetricPublishTotal, metric.WithDescription("Snapshot publish attempts by result."))
	p.pubSecs, _ = d.Meter.Float64Histogram(MetricPublishSeconds, metric.WithUnit("s"), metric.WithDescription("Duration of one Apply (build, project, validate, swap)."),
		metric.WithExplicitBucketBoundaries(0.00005, 0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.05, 0.25))
	ver, _ := d.Meter.Int64ObservableGauge("scp_snapshot_version", metric.WithDescription("Current snapshot version."))
	age, _ := d.Meter.Float64ObservableGauge("scp_snapshot_age_seconds", metric.WithUnit("s"), metric.WithDescription("Seconds since the current snapshot was built."))
	repos, _ := d.Meter.Int64ObservableGauge("scp_snapshot_repos", metric.WithDescription("Entries in the current snapshot."))
	_, _ = d.Meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := p.cur.Load()
		o.ObserveInt64(ver, int64(s.version))
		o.ObserveFloat64(age, time.Since(s.builtAt).Seconds())
		o.ObserveInt64(repos, int64(len(s.repos)))
		return nil
	}, ver, age, repos)
	return p
}

// Current is wait-free. Handlers call it once per request.
func (p *Publisher) Current() *Snapshot { return p.cur.Load() }

// Status returns publication counters; it never blocks behind a running Apply.
func (p *Publisher) Status() PublishStatus {
	p.stMu.Lock()
	defer p.stMu.Unlock()
	st := p.st
	s := p.cur.Load()
	st.Version, st.BuiltAt = s.version, s.builtAt
	return st
}

func (p *Publisher) count(result string) {
	p.pubTotal.Add(context.Background(), 1, metric.WithAttributes(attribute.String("result", result)))
}

// existenceValidator is the registry's built-in rule: the converse of every referential rule.
func existenceValidator(s *Snapshot) error {
	for id, e := range s.repos {
		if e.slots[s.reg.existence] == nil {
			return fmt.Errorf("entry %q has no value in the existence slot %q", id, s.reg.slots[s.reg.existence])
		}
	}
	return nil
}

// Apply builds, projects, validates and atomically publishes the next snapshot. fn runs under the publish mutex: it MUST NOT
// block or do I/O. On any error the previous snapshot stays current and is returned together with the error:
//   - fn error: returned unchanged (counted "aborted");
//   - projector or validator error: *InvalidSnapshotError (Is ErrInvalidSnapshot, Permanent) (counted "invalid").
func (p *Publisher) Apply(fn func(*Builder) error) (*Snapshot, error) {
	t0 := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	_, span := p.tracer.Start(context.Background(), "snapshot.apply")
	defer span.End()
	prev := p.cur.Load()
	b := newBuilder(prev)
	if err := fn(b); err != nil {
		p.stMu.Lock()
		p.st.Aborted++
		p.stMu.Unlock()
		p.count("aborted")
		return prev, err
	}
	reject := func(stage string, i int, err error) (*Snapshot, error) {
		ie := &InvalidSnapshotError{Stage: stage, Index: i, Err: err}
		p.stMu.Lock()
		p.st.Rejected++
		p.st.LastRejected, p.st.LastRejectedAt = ie, time.Now()
		p.stMu.Unlock()
		p.count("invalid")
		p.log.Error("snapshot candidate rejected", "stage", stage, "index", i, "err", err, "snapshot_version", prev.version)
		return prev, ie
	}
	for i, pr := range p.projectors {
		if err := pr(b); err != nil {
			return reject("projector", i, err)
		}
	}
	next := &Snapshot{reg: p.reg, version: prev.version + 1, builtAt: time.Now(), repos: b.repos, names: b.sortedNames(), globals: b.globals}
	for id := range b.touched { // unpublished entries: stamping is safe
		if e := b.repos[id]; e != nil {
			e.rev = next.version
		}
	}
	if err := existenceValidator(next); err != nil {
		return reject("validator", -1, err)
	}
	for i, v := range p.validators {
		if err := v(next); err != nil {
			return reject("validator", i, err)
		}
	}
	p.cur.Store(next) // the only publication point
	p.stMu.Lock()
	p.st.Published++
	p.stMu.Unlock()
	p.count("ok")
	p.pubSecs.Record(context.Background(), time.Since(t0).Seconds())
	return next, nil
}

var _ = maps.Keys[map[EntryID]struct{}]
