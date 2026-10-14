package refresh

import (
	"context"

	"github.com/cantara/source-code-portal/internal/job"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Instrument names are constants next to their instruments (G11); the cmd/scp registry test compares them
// with docs/reference/metrics.md.
const (
	MetricSyncPending      = "scp_sync_pending"
	MetricSyncRunning      = "scp_sync_running"
	MetricSyncParked       = "scp_sync_parked"
	MetricUpstreamPaused   = "scp_upstream_paused_seconds"
	MetricSyncJobs         = "scp_sync_jobs_total"
	MetricJobDuration      = "scp_job_duration_seconds"
	MetricJobWait          = "scp_job_wait_seconds"
	MetricJobOverrun       = "scp_job_overrun_total"
	MetricJobIgnoredCancel = "scp_job_ignored_cancel_total"
	MetricGateDenied       = "scp_gate_denied_total"
	MetricToPublish        = "scp_webhook_to_publish_seconds"
	MetricPlannerTick      = "scp_planner_tick_seconds"
	MetricPlannerEnqueue   = "scp_planner_enqueue_total"
	MetricPlannerSubjects  = "scp_planner_subjects"
	MetricTaskRuns         = "scp_task_runs_total"
	MetricTaskOverruns     = "scp_task_overruns_total"
)

type metrics struct {
	jobs, overrun, ignoredCancel, gateDenied metric.Int64Counter
	duration, wait, toPublish                metric.Float64Histogram
}

func kindAttr(kind string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("kind", kind))
}
func kindOutcome(kind, outcome string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("kind", kind), attribute.String("outcome", outcome))
}
func upPrio(up string, p job.Priority) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("upstream", up), attribute.String("priority", prioName(p)))
}

func newMetrics(m metric.Meter, e *Engine) *metrics {
	x := &metrics{}
	x.jobs, _ = m.Int64Counter(MetricSyncJobs, metric.WithDescription("job outcomes by kind"))
	x.overrun, _ = m.Int64Counter(MetricJobOverrun)
	x.ignoredCancel, _ = m.Int64Counter(MetricJobIgnoredCancel)
	x.gateDenied, _ = m.Int64Counter(MetricGateDenied)
	x.duration, _ = m.Float64Histogram(MetricJobDuration, metric.WithUnit("s"))
	x.wait, _ = m.Float64Histogram(MetricJobWait, metric.WithUnit("s"))
	x.toPublish, _ = m.Float64Histogram(MetricToPublish, metric.WithUnit("s"))
	up := func(name string, f func(UpstreamStats) int64) {
		_, _ = m.Int64ObservableGauge(name, metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			for n, s := range e.Inspect().Upstreams {
				o.Observe(f(s), metric.WithAttributes(attribute.String("upstream", n)))
			}
			return nil
		}))
	}
	up(MetricSyncPending, func(s UpstreamStats) int64 { return int64(s.Ready + s.Delayed) })
	up(MetricSyncRunning, func(s UpstreamStats) int64 { return int64(s.Running) })
	_, _ = m.Int64ObservableGauge(MetricSyncParked, metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
		o.Observe(int64(e.Inspect().Parked))
		return nil
	}))
	_, _ = m.Float64ObservableGauge(MetricUpstreamPaused, metric.WithUnit("s"), metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
		st := e.Inspect()
		for n, s := range st.Upstreams {
			v := 0.0
			if !s.PausedUntil.IsZero() {
				v = s.PausedUntil.Sub(st.At).Seconds()
			}
			o.Observe(v, metric.WithAttributes(attribute.String("upstream", n)))
		}
		return nil
	}))
	return x
}
