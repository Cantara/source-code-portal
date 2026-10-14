package sqlite

import (
	"context"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Metric names (constants next to their instruments, G11).
const (
	MetricState          = "scp_store_state"
	MetricErrors         = "scp_store_errors_total"
	MetricRebuilds       = "scp_store_rebuilds_total"
	MetricTxSeconds      = "scp_store_tx_seconds"
	MetricCacheBytes     = "scp_store_cache_bytes"
	MetricCacheOps       = "scp_store_cache_ops_total"
	MetricCacheEvictions = "scp_store_cache_evictions_total"
	MetricCommitsTrimmed = "scp_store_commits_trimmed_total"
	MetricPruned         = "scp_store_pruned_total"
	MetricPlanSkipped    = "scp_store_plan_rows_skipped_total"
	MetricDBBytes        = "scp_store_db_bytes"
	MetricWALBytes       = "scp_store_wal_bytes"
	MetricBackupTotal    = "scp_store_backup_total"
	MetricBackupSeconds  = "scp_store_backup_seconds"
	MetricBackupAge      = "scp_store_backup_age_seconds"
)

type metrics struct {
	errors, rebuilds, cacheOps, cacheEvict, trimmed, pruned, backups, planSkipped metric.Int64Counter
	txSeconds, backupSeconds                                                      metric.Float64Histogram
}

func newMetrics(m metric.Meter) *metrics {
	x := &metrics{}
	x.errors, _ = m.Int64Counter(MetricErrors, metric.WithDescription("Store operations that failed with a SQLite error, by op."))
	x.rebuilds, _ = m.Int64Counter(MetricRebuilds, metric.WithDescription("Starts from a quarantined database, by reason."))
	x.cacheOps, _ = m.Int64Counter(MetricCacheOps, metric.WithDescription("GetCache lookups by namespace and result (hit|miss)."))
	x.cacheEvict, _ = m.Int64Counter(MetricCacheEvictions, metric.WithDescription("Cache entries evicted by the LRU cap, by namespace."))
	x.trimmed, _ = m.Int64Counter(MetricCommitsTrimmed, metric.WithDescription("Commit rows deleted by retention."))
	x.pruned, _ = m.Int64Counter(MetricPruned, metric.WithDescription("Rows deleted at open because their kind or namespace is no longer registered, by table."))
	x.planSkipped, _ = m.Int64Counter(MetricPlanSkipped, metric.WithDescription("SavePlans rows not persisted, by reason (global|no_item)."))
	x.backups, _ = m.Int64Counter(MetricBackupTotal, metric.WithDescription("Backups by result (ok|error)."))
	x.txSeconds, _ = m.Float64Histogram(MetricTxSeconds, metric.WithUnit("s"), metric.WithDescription("Duration of one store call (one transaction), by op."),
		metric.WithExplicitBucketBoundaries(0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5))
	x.backupSeconds, _ = m.Float64Histogram(MetricBackupSeconds, metric.WithUnit("s"), metric.WithDescription("Duration of one backup."),
		metric.WithExplicitBucketBoundaries(0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30))
	return x
}

// Deps are injected by cmd/scp.
type Deps struct {
	Log     *slog.Logger
	Meter   metric.Meter
	Tracer  trace.Tracer
	BuildID string
	// CacheNamespaces maps every cache namespace to its byte cap; any other namespace is rejected.
	CacheNamespaces map[string]int64
	// KnownKinds, when non-empty, makes Open delete item rows of other kinds (and cache rows of unregistered namespaces).
	KnownKinds []string

	migrations []migration // tests only
	drv        driver      // tests only: the escape-hatch driver
}

func (d *Deps) defaults() {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.Meter == nil {
		d.Meter = metricnoop.NewMeterProvider().Meter("store")
	}
	if d.Tracer == nil {
		d.Tracer = tracenoop.NewTracerProvider().Tracer("store")
	}
}

func (s *Store) registerGauges(m metric.Meter) {
	state, _ := m.Int64ObservableGauge(MetricState, metric.WithDescription("0 ok, 1 degraded (writes suspended)."))
	cb, _ := m.Int64ObservableGauge(MetricCacheBytes, metric.WithUnit("By"), metric.WithDescription("Bytes held per cache namespace."))
	db, _ := m.Int64ObservableGauge(MetricDBBytes, metric.WithUnit("By"), metric.WithDescription("Size of the database file."))
	wal, _ := m.Int64ObservableGauge(MetricWALBytes, metric.WithUnit("By"), metric.WithDescription("Size of the -wal file."))
	age, _ := m.Float64ObservableGauge(MetricBackupAge, metric.WithUnit("s"), metric.WithDescription("Seconds since the last successful backup (absent when none)."))
	_, _ = m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		if s.Health().OK {
			o.ObserveInt64(state, 0)
		} else {
			o.ObserveInt64(state, 1)
		}
		s.cacheMu.Lock()
		for ns, n := range s.cacheBytes {
			o.ObserveInt64(cb, n, metric.WithAttributes(attribute.String("ns", ns)))
		}
		s.cacheMu.Unlock()
		if fi, err := os.Stat(s.path); err == nil {
			o.ObserveInt64(db, fi.Size())
		}
		if fi, err := os.Stat(s.path + "-wal"); err == nil {
			o.ObserveInt64(wal, fi.Size())
		}
		if t := s.lastBackup.Load(); t > 0 {
			o.ObserveFloat64(age, time.Since(time.Unix(t, 0)).Seconds())
		}
		return nil
	}, state, cb, db, wal, age)
}
