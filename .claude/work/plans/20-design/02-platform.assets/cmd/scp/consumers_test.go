package main

import (
	"context"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/cantara/source-code-portal/internal/platform/config"
	"github.com/cantara/source-code-portal/internal/platform/httpx"
	"github.com/cantara/source-code-portal/internal/platform/lifecycle"
	"github.com/cantara/source-code-portal/internal/platform/logx"
	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/platform/telemetry"
)

// Consumer-defined interfaces, written exactly as the consuming packages declare
// them (section 2.3 of the design). These compile-time assertions are the only
// place the producer and the consumer meet, which is cmd/scp's job.

// webhook.Limiter (unit 04, section 3: "type Limiter interface{ Allow(key string) bool }")
type strikeLimiter interface{ Allow(key string) bool }

// webhook's clientIP parameter: func(*http.Request) string, bridged from the resolver.
var _ func(*http.Request) string = (*httpx.ClientIPResolver)(nil).ClientIP

// every feature: a Meter factory and a Tracer factory, never the otel globals.
type meterSource interface {
	Meter(name string) metric.Meter
}
type tracerSource interface {
	Tracer(name string) trace.Tracer
}

// ops: the metrics endpoint and a ready flag.
type metricsHandler interface{ MetricsHandler() http.Handler }

// any feature with a worker: the lifecycle stage function shape.
type workerFunc = func(ctx context.Context) error

// config.Validator as features implement it, with no config import.
type validator interface {
	Validate(report func(path, msg string))
}

var (
	_ strikeLimiter                = (*httpx.Limiter)(nil)
	_ meterSource                  = (*telemetry.Provider)(nil)
	_ tracerSource                 = (*telemetry.Provider)(nil)
	_ metricsHandler               = (*telemetry.Provider)(nil)
	_ workerFunc                   = (*httpx.Limiter)(nil).Run
	_ validator                    = httpx.Config{}
	_ validator                    = httpx.RateConfig{}
	_ validator                    = telemetry.Config{}
	_ validator                    = lifecycle.Config{}
	_ validator                    = config.Validator(nil)
	_ slog.LogValuer               = secret.Secret{}
	_ func(context.Context) string = logx.RequestID
	_ func(context.Context) string = logx.ClientIP
)
