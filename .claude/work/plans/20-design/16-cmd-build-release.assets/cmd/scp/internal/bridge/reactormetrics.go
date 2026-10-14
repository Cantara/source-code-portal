package bridge

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/reactor"
)

// Instrument names (01 section 9.1). Constants next to the instruments, as G11 asks; the generated metrics reference lists them.
const (
	ReactorRunsMetric    = "scp_reactor_runs_total"
	ReactorSecondsMetric = "scp_reactor_seconds"
)

// NewReactorMetrics returns reactor.Deps.Observe: one counter increment and one histogram sample per reactor run. The label
// sets are closed (reactor names are chosen in cmd/scp, event kinds and outcomes are enums), so the series count is bounded.
func NewReactorMetrics(m metric.Meter) (func(reactor.Observation), error) {
	runs, err := m.Int64Counter(ReactorRunsMetric, metric.WithDescription("Reactor invocations by reactor, event kind and outcome."))
	if err != nil {
		return nil, err
	}
	secs, err := m.Float64Histogram(ReactorSecondsMetric, metric.WithUnit("s"), metric.WithDescription("Time spent inside one reactor."),
		metric.WithExplicitBucketBoundaries(10e-6, 50e-6, 100e-6, 500e-6, 1e-3, 5e-3, 10e-3, 50e-3, 100e-3, 500e-3, 1))
	if err != nil {
		return nil, err
	}
	return func(o reactor.Observation) {
		ctx := context.Background() // Observe has no context; the instruments never read it
		runs.Add(ctx, 1, metric.WithAttributes(attribute.String("reactor", o.Reactor), attribute.String("kind", string(o.Kind)), attribute.String("outcome", string(o.Outcome))))
		secs.Record(ctx, o.Duration.Seconds(), metric.WithAttributes(attribute.String("reactor", o.Reactor)))
	}, nil
}
