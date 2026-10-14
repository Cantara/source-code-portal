// Package telemetry builds the process-wide OpenTelemetry providers and the
// Prometheus scrape handler. Rank 0: it imports no internal package. Nothing
// here touches the otel globals (G11): callers receive a Meter/Tracer from the
// Provider and pass them down as dependencies.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	runtimemetrics "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// Config is the "telemetry" section of the config file. Platform owns it.
type Config struct {
	Prometheus bool          `yaml:"prometheus" jsonschema_description:"Serve Prometheus metrics at GET /metrics on the admin listener."`
	OTLP       OTLPConfig    `yaml:"otlp" jsonschema_description:"Optional OTLP/HTTP export of traces, metrics and logs."`
	Traces     TracesConfig  `yaml:"traces" jsonschema_description:"Trace sampling."`
	Runtime    RuntimeConfig `yaml:"runtime" jsonschema_description:"Go runtime metrics."`
}

type OTLPConfig struct {
	Enabled           bool              `yaml:"enabled" jsonschema_description:"Export over OTLP/HTTP. Off by default."`
	Endpoint          string            `yaml:"endpoint" jsonschema_description:"Collector base URL, for example https://otel.example.com:4318. Required when enabled."`
	HeadersFile       string            `yaml:"headers_file" jsonschema_description:"Absolute path of a file with one Name: value header per line (credentials live here, not in the config file)."`
	Timeout           time.Duration     `yaml:"timeout" scp:"min=1s,max=2m" jsonschema_description:"Per-export timeout."`
	AllowPrivateCIDRs []string          `yaml:"allow_private_cidrs" jsonschema_description:"CIDRs the collector may live in when it is on a private network (cmd/scp builds the netguard policy). A /0 range is refused."`
	Ports             []int             `yaml:"ports" jsonschema_description:"Extra destination ports besides 80 and 443, for example 4318."`
	Signals           OTLPSignalsConfig `yaml:"signals" jsonschema_description:"Which signals to export."`
}

type OTLPSignalsConfig struct {
	Traces  bool `yaml:"traces" jsonschema_description:"Export traces."`
	Metrics bool `yaml:"metrics" jsonschema_description:"Export metrics."`
	Logs    bool `yaml:"logs" jsonschema_description:"Export logs (bridged from slog by cmd/scp)."`
}

type TracesConfig struct {
	SampleRatio float64 `yaml:"sample_ratio" jsonschema:"minimum=0,maximum=1" jsonschema_description:"Parent-based head sampling ratio for root spans, 0 to 1."`
}

type RuntimeConfig struct {
	Enabled bool `yaml:"enabled" jsonschema_description:"Emit go.* runtime metrics."`
}

// BuildInfo labels scp_build_info and the otel resource.
type BuildInfo struct {
	Service  string
	Version  string
	Commit   string
	Instance string
}

// Options carries what must not come from the config file.
type Options struct {
	// Headers are the parsed OTLP headers (cmd/scp reads HeadersFile).
	Headers map[string]string
	// LatencyBuckets override the default duration histogram boundaries (seconds).
	LatencyBuckets []float64
	// HTTPClient is used by the OTLP exporters (cmd/scp injects a netguard client).
	HTTPClient *http.Client
}

// Provider owns the SDK providers. Create with New; call Shutdown once.
type Provider struct {
	mp      *sdkmetric.MeterProvider
	tp      *sdktrace.TracerProvider
	lp      *sdklog.LoggerProvider // nil unless OTLP logs enabled
	reg     *prometheus.Registry
	handler http.Handler
	prop    propagation.TextMapPropagator
	cfg     Config
}

// DefaultBuckets are the histogram boundaries in seconds for *_duration_seconds.
var DefaultBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60}

// Validate reports config violations (same shape as config.Validator).
func (c Config) Validate(report func(path, msg string)) {
	if c.OTLP.Enabled {
		if c.OTLP.Endpoint == "" {
			report("otlp.endpoint", "required when otlp.enabled is true")
		} else if u, err := url.Parse(c.OTLP.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			report("otlp.endpoint", "must be an http(s) URL")
		} else if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			report("otlp.endpoint", "must not contain userinfo, a query or a fragment (credentials belong in headers_file)")
		}
		for _, cidr := range c.OTLP.AllowPrivateCIDRs {
			if p, err := netip.ParsePrefix(cidr); err != nil || p.Bits() == 0 {
				report("otlp.allow_private_cidrs", cidr+" is not a CIDR narrower than /0")
			}
		}
		for _, port := range c.OTLP.Ports {
			if port < 1 || port > 65535 {
				report("otlp.ports", "port out of range")
			}
		}
		s := c.OTLP.Signals
		if !s.Traces && !s.Metrics && !s.Logs {
			report("otlp.signals", "enable at least one signal or set otlp.enabled to false")
		}
	}
}

// New builds the providers. It never installs globals. When construction fails half way, everything already built is shut
// down before the error is returned (no exporter goroutine survives a failed New).
func New(ctx context.Context, c Config, b BuildInfo, o Options) (p *Provider, err error) {
	c.OTLP.Endpoint = strings.TrimRight(c.OTLP.Endpoint, "/") // the signal path is appended below
	var built []func(context.Context) error
	defer func() {
		if err != nil {
			for _, f := range built {
				_ = f(context.WithoutCancel(ctx))
			}
		}
	}()
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(b.Service),
			semconv.ServiceVersion(b.Version),
			semconv.ServiceInstanceID(b.Instance),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: resource: %w", err)
	}
	p = &Provider{cfg: c, prop: propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})}

	buckets := o.LatencyBuckets
	if len(buckets) == 0 {
		buckets = DefaultBuckets
	}
	views := []sdkmetric.View{
		sdkmetric.NewView(
			sdkmetric.Instrument{Kind: sdkmetric.InstrumentKindHistogram, Unit: "s"},
			sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: buckets}},
		),
	}
	mopts := []sdkmetric.Option{sdkmetric.WithResource(res), sdkmetric.WithView(views...)}

	p.reg = prometheus.NewRegistry()
	if c.Prometheus {
		exp, err := otelprom.New(
			otelprom.WithRegisterer(p.reg),
			otelprom.WithoutScopeInfo(),
			otelprom.WithoutTargetInfo(),
		)
		if err != nil {
			return nil, fmt.Errorf("telemetry: prometheus exporter: %w", err)
		}
		mopts = append(mopts, sdkmetric.WithReader(exp))
	}
	p.handler = promhttp.HandlerFor(p.reg, promhttp.HandlerOpts{Registry: p.reg})

	var traceExp sdktrace.SpanExporter
	if c.OTLP.Enabled {
		so := c.OTLP.Signals
		if so.Metrics {
			mo := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpointURL(c.OTLP.Endpoint + "/v1/metrics"), otlpmetrichttp.WithTimeout(c.OTLP.Timeout), otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression)}
			if len(o.Headers) > 0 {
				mo = append(mo, otlpmetrichttp.WithHeaders(o.Headers))
			}
			if o.HTTPClient != nil {
				mo = append(mo, otlpmetrichttp.WithHTTPClient(o.HTTPClient))
			}
			me, err := otlpmetrichttp.New(ctx, mo...)
			if err != nil {
				return nil, fmt.Errorf("telemetry: otlp metrics: %w", err)
			}
			built = append(built, me.Shutdown)
			mopts = append(mopts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(me, sdkmetric.WithInterval(30*time.Second))))
		}
		if so.Traces {
			to := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(c.OTLP.Endpoint + "/v1/traces"), otlptracehttp.WithTimeout(c.OTLP.Timeout), otlptracehttp.WithCompression(otlptracehttp.GzipCompression)}
			if len(o.Headers) > 0 {
				to = append(to, otlptracehttp.WithHeaders(o.Headers))
			}
			if o.HTTPClient != nil {
				to = append(to, otlptracehttp.WithHTTPClient(o.HTTPClient))
			}
			traceExp, err = otlptracehttp.New(ctx, to...)
			if err != nil {
				return nil, fmt.Errorf("telemetry: otlp traces: %w", err)
			}
			built = append(built, traceExp.Shutdown)
		}
		if so.Logs {
			lo := []otlploghttp.Option{otlploghttp.WithEndpointURL(c.OTLP.Endpoint + "/v1/logs"), otlploghttp.WithTimeout(c.OTLP.Timeout), otlploghttp.WithCompression(otlploghttp.GzipCompression)}
			if len(o.Headers) > 0 {
				lo = append(lo, otlploghttp.WithHeaders(o.Headers))
			}
			if o.HTTPClient != nil {
				lo = append(lo, otlploghttp.WithHTTPClient(o.HTTPClient))
			}
			le, err := otlploghttp.New(ctx, lo...)
			if err != nil {
				return nil, fmt.Errorf("telemetry: otlp logs: %w", err)
			}
			p.lp = sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(le)))
			built = append(built, p.lp.Shutdown)
		}
	}
	p.mp = sdkmetric.NewMeterProvider(mopts...)
	built = append(built, p.mp.Shutdown)

	topts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(c.Traces.SampleRatio))),
	}
	if traceExp != nil {
		topts = append(topts, sdktrace.WithBatcher(traceExp))
	}
	p.tp = sdktrace.NewTracerProvider(topts...)
	built = append(built, p.tp.Shutdown)

	m := p.mp.Meter("scp")
	if _, err := m.Int64ObservableGauge("scp_build_info",
		metric.WithDescription("Build information; value is always 1."),
		metric.WithInt64Callback(func(_ context.Context, ob metric.Int64Observer) error {
			ob.Observe(1, metric.WithAttributes(attribute.String("version", b.Version), attribute.String("commit", b.Commit), attribute.String("go", runtime.Version())))
			return nil
		})); err != nil {
		return nil, fmt.Errorf("telemetry: build info: %w", err)
	}
	if c.Runtime.Enabled {
		if err := runtimemetrics.Start(runtimemetrics.WithMeterProvider(p.mp)); err != nil {
			return nil, fmt.Errorf("telemetry: runtime metrics: %w", err)
		}
	}
	return p, nil
}

// Meter returns a named meter. Features call it once in their constructor.
func (p *Provider) Meter(name string) metric.Meter { return p.mp.Meter(name) }

// Tracer returns a named tracer.
func (p *Provider) Tracer(name string) trace.Tracer { return p.tp.Tracer(name) }

// MeterProvider and TracerProvider are for otelhttp and similar adapters.
func (p *Provider) MeterProvider() metric.MeterProvider  { return p.mp }
func (p *Provider) TracerProvider() trace.TracerProvider { return p.tp }

// Propagator is the W3C trace-context + baggage propagator.
func (p *Provider) Propagator() propagation.TextMapPropagator { return p.prop }

// LoggerProvider is non-nil only when OTLP logs are enabled.
func (p *Provider) LoggerProvider() log.LoggerProvider {
	if p.lp == nil {
		return nil
	}
	return p.lp
}

// MetricsHandler serves the Prometheus text format. It is the same handler
// whether or not the exporter is enabled; the route is mounted by cmd/scp only
// when Config.Prometheus is true.
func (p *Provider) MetricsHandler() http.Handler { return p.handler }

// Shutdown flushes and stops all providers. Safe to call once; errors joined.
func (p *Provider) Shutdown(ctx context.Context) error {
	var errs []error
	if p.tp != nil {
		errs = append(errs, p.tp.Shutdown(ctx))
	}
	if p.mp != nil {
		errs = append(errs, p.mp.Shutdown(ctx))
	}
	if p.lp != nil {
		errs = append(errs, p.lp.Shutdown(ctx))
	}
	return errors.Join(errs...)
}
