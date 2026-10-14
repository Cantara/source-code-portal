package ops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/cantara/source-code-portal/internal/httpapi"
)

// Metric names (constants next to their instruments, G11). scp_build_info is owned by telemetry (unit 02), not here. All are final Prometheus names: the exporter does not
// add a second suffix to a name that already carries _total / _seconds / _bytes (verified with exporter v0.69.0).
const (
	MetricStartTime      = "scp_start_time_seconds"
	MetricReadinessState = "scp_readiness_state"
	MetricReadinessGate  = "scp_readiness_gate"
	MetricReadySince     = "scp_ready_since_timestamp_seconds"
	MetricCoverageRatio  = "scp_coverage_ratio"
	MetricItemAge        = "scp_item_age_seconds"
	MetricItems          = "scp_items"
	MetricHeartbeatAge   = "scp_liveness_heartbeat_age_seconds"
	MetricStatusWarnings = "scp_status_warnings"
	MetricStatusBuild    = "scp_status_build_seconds"
	MetricAdminActions   = "scp_admin_actions_total"
)

type Deps struct {
	Log     *slog.Logger
	Meter   metric.Meter
	Build   BuildInfo
	Started time.Time // process start; fixed by cmd/scp
	Sources Sources
	Kinds   []Kind
	Retrier Retrier
}

type Service struct {
	cfg     Config
	d       Deps
	Ready   *Readiness
	Status  *Collector
	actions metric.Int64Counter
	build   metric.Float64Histogram
}

// New validates the configuration and the wiring and registers the instruments.
func New(cfg Config, rcfg ReadinessConfig, d Deps) (*Service, error) {
	errs := append(cfg.Validate("ops"), rcfg.Validate("readiness")...)
	s := d.Sources
	switch {
	case s.Snapshot == nil, s.Store == nil, s.Engine == nil, s.Catalogue == nil:
		errs = append(errs, errors.New("ops: Sources.Snapshot, Store, Engine and Catalogue are required"))
	}
	if d.Retrier == nil {
		errs = append(errs, errors.New("ops: Retrier is required"))
	}
	seen := map[string]bool{}
	for _, k := range d.Kinds {
		switch {
		case k.Family == "" || k.Job == "" || k.Infos == nil:
			errs = append(errs, fmt.Errorf("ops: kind %q is incomplete", k.Family))
		case seen[k.Family]:
			errs = append(errs, fmt.Errorf("ops: duplicate kind family %q", k.Family))
		}
		seen[k.Family] = true
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.Meter == nil {
		d.Meter = metricnoop.NewMeterProvider().Meter("ops")
	}
	rd, err := NewReadiness(rcfg, s.Catalogue, s.Snapshot, d.Kinds, d.Started)
	if err != nil {
		return nil, err
	}
	svc := &Service{cfg: cfg, d: d, Ready: rd}
	svc.Status = &Collector{cfg: cfg, src: s, kinds: d.Kinds, ready: rd, build: d.Build, started: d.Started, core: append([]string(nil), rcfg.CoreKinds...)}
	svc.actions, _ = d.Meter.Int64Counter(MetricAdminActions, metric.WithDescription("Operator actions by action and result."))
	svc.build, _ = d.Meter.Float64Histogram(MetricStatusBuild, metric.WithUnit("s"), metric.WithDescription("Time to assemble the status document."),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 1))
	svc.instruments()
	return svc, nil
}

// Mount registers api.status and api.admin.retry. cmd/scp calls it with the ADMIN recorder only.
func (s *Service) Mount(m httpapi.Mux) {
	m.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		t0 := time.Now()
		s.Status.Handler().ServeHTTP(w, r)
		s.build.Record(r.Context(), time.Since(t0).Seconds())
	})
	retry := RetryHandler(s.d.Retrier, s.d.Log, func(result string) {
		s.actions.Add(context.Background(), 1, metric.WithAttributes(attribute.String("action", "retry"), attribute.String("result", result)))
	})
	m.HandleFunc("POST /api/v1/admin/jobs/retry", retry.ServeHTTP)
}

func (s *Service) LiveHandler() http.Handler {
	return LiveHandler(s.cfg.Liveness, s.d.Sources.Engine, s.d.Started)
}
func (s *Service) ReadyHandler(detail bool) http.Handler { return s.Ready.ReadyHandler(detail) }

// Drain is the first lifecycle Drain action of the servers stage: readiness goes off, liveness stays on.
func (s *Service) Drain(context.Context) error { s.Ready.SetShuttingDown(); return nil }

func (s *Service) instruments() {
	m := s.d.Meter
	st, _ := m.Float64ObservableGauge(MetricStartTime, metric.WithUnit("s"), metric.WithDescription("Process start, unix seconds."))
	rs, _ := m.Int64ObservableGauge(MetricReadinessState, metric.WithDescription("1 for the current readiness phase, 0 for the others."))
	rg, _ := m.Int64ObservableGauge(MetricReadinessGate, metric.WithDescription("1 when a readiness gate is satisfied."))
	rsince, _ := m.Float64ObservableGauge(MetricReadySince, metric.WithUnit("s"), metric.WithDescription("When readiness was first reached, unix seconds; absent before."))
	cov, _ := m.Float64ObservableGauge(MetricCoverageRatio, metric.WithDescription("Share of catalogue repositories with a definitive result, per item family."))
	age, _ := m.Float64ObservableGauge(MetricItemAge, metric.WithUnit("s"), metric.WithDescription("Age of fetched items per item family (stat max or p95)."))
	items, _ := m.Int64ObservableGauge(MetricItems, metric.WithDescription("Items per family by derived freshness status."))
	hb, _ := m.Float64ObservableGauge(MetricHeartbeatAge, metric.WithUnit("s"), metric.WithDescription("Seconds since the dispatcher last passed; absent before it ran."))
	warn, _ := m.Int64ObservableGauge(MetricStatusWarnings, metric.WithDescription("Active /status warnings by severity."))
	_, _ = m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		now := time.Now()
		o.ObserveFloat64(st, float64(s.d.Started.Unix()))
		doc := s.Status.Status(now)
		for _, p := range []Phase{PhaseWarming, PhaseDegraded, PhaseReady, PhaseShuttingDown} {
			o.ObserveInt64(rs, b2i(p.String() == doc.Readiness.State), metric.WithAttributes(attribute.String("phase", p.String())))
		}
		for _, g := range doc.Readiness.Gates {
			o.ObserveInt64(rg, b2i(g.OK), metric.WithAttributes(attribute.String("gate", g.Name)))
		}
		if doc.Readiness.ReadySince != nil {
			o.ObserveFloat64(rsince, float64(doc.Readiness.ReadySince.Unix()))
		}
		snap := s.d.Sources.Snapshot.Current()
		for _, k := range s.d.Kinds {
			ks := k.Stats(snap, now)
			kv := attribute.String("kind", k.Family)
			o.ObserveFloat64(cov, ks.Ratio(), metric.WithAttributes(kv))
			if ks.HasAges {
				o.ObserveFloat64(age, ks.Oldest.Seconds(), metric.WithAttributes(kv, attribute.String("stat", "max")))
				o.ObserveFloat64(age, ks.P95.Seconds(), metric.WithAttributes(kv, attribute.String("stat", "p95")))
			}
			for i, n := range []string{"unknown", "fresh", "stale", "error"} {
				o.ObserveInt64(items, int64(ks.ByStatus[i]), metric.WithAttributes(kv, attribute.String("status", n)))
			}
		}
		if h := s.d.Sources.Engine.EngineView().Heartbeat; !h.IsZero() {
			o.ObserveFloat64(hb, now.Sub(h).Seconds())
		}
		counts := map[string]int64{SevInfo: 0, SevWarning: 0, SevCritical: 0}
		for _, w := range doc.Warnings {
			counts[w.Severity]++
		}
		for sev, n := range counts {
			o.ObserveInt64(warn, n, metric.WithAttributes(attribute.String("severity", sev)))
		}
		return nil
	}, st, rs, rg, rsince, cov, age, items, hb, warn)
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
