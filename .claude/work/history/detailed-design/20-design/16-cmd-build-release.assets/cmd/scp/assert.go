package main

// Compile-time proofs that every producer satisfies every consumer-defined interface it is wired to (architecture 5.5, 10.1
// step 6). No behavior: a signature that no adapter can satisfy is a compile error in the first CI run, not a design
// discovery. One block per source of the contract; a unit that adds an interface adds its line here in the same change.

import (
	"context"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/cantara/source-code-portal/cmd/scp/internal/bridge"
	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/ops"
	"github.com/cantara/source-code-portal/internal/platform/config"
	"github.com/cantara/source-code-portal/internal/platform/httpx"
	"github.com/cantara/source-code-portal/internal/platform/lifecycle"
	"github.com/cantara/source-code-portal/internal/platform/logx"
	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/platform/telemetry"
	"github.com/cantara/source-code-portal/internal/reactor"
	"github.com/cantara/source-code-portal/internal/refresh"
	"github.com/cantara/source-code-portal/internal/webhook"
)

// Interfaces whose consumers live in packages that may not import the producer (written as the consumers declare them, 02 section 2.3).
type (
	meterSource interface {
		Meter(name string) metric.Meter
	}
	tracerSource interface {
		Tracer(name string) trace.Tracer
	}
	metricsHandler interface{ MetricsHandler() http.Handler }
	validator      interface {
		Validate(report func(path, msg string))
	}
)

// 02-platform (section 2.3).
var (
	_ webhook.Limiter              = (*httpx.Limiter)(nil)
	_ func(*http.Request) string   = (*httpx.ClientIPResolver)(nil).ClientIP
	_ meterSource                  = (*telemetry.Provider)(nil)
	_ tracerSource                 = (*telemetry.Provider)(nil)
	_ metricsHandler               = (*telemetry.Provider)(nil)
	_ func(context.Context) error  = (*httpx.Limiter)(nil).Run
	_ validator                    = httpx.Config{}
	_ validator                    = httpx.RateConfig{}
	_ validator                    = telemetry.Config{}
	_ validator                    = lifecycle.Config{}
	_ validator                    = Config{}
	_ config.Validator             = Config{}
	_ slog.LogValuer               = secret.Secret{}
	_ func(context.Context) string = logx.RequestID
	_ func(context.Context) string = logx.ClientIP
)

// 01-kernel and 04-refresh-webhook (04 section 2.4, 01 section 8.3), plus the bridges of this unit (design 16 section 5).
var (
	_ job.Enqueuer         = (*refresh.Engine)(nil)
	_ job.Enqueuer         = (*refresh.Planner)(nil) // THE Enqueuer of reactors, features, ops.Retrier and the first jobs (04 D2)
	_ webhook.Sink         = (*reactor.Fan)(nil)
	_ catalog.EventSink    = (*reactor.Fan)(nil)
	_ refresh.PlanSink     = (*bridge.PlanSink)(nil)
	_ refresh.Gate         = bridge.RateGate{}
	_ bridge.RateAdmitter  = (*github.Client)(nil)
	_ webhook.Watermarks   = (*bridge.Watermarks)(nil)
	_ ops.Retrier          = (*bridge.Retrier)(nil)
	_ catalog.Tracker      = (*bridge.Tracker)(nil)
	_ bridge.SubjectSetter = (*refresh.Planner)(nil)
	_ bridge.Trigger       = (*refresh.Scheduler)(nil)
	_ planFlusher          = (*bridge.PlanSink)(nil) // the store stage's second worker (storestage.go)
)
