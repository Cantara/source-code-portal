// Command scp is the composition root: the only place that reads the process
// environment, installs signal handling and wires the platform packages.
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/platform/config"
	"github.com/cantara/source-code-portal/internal/platform/httpx"
	"github.com/cantara/source-code-portal/internal/platform/lifecycle"
	"github.com/cantara/source-code-portal/internal/platform/logx"
	"github.com/cantara/source-code-portal/internal/platform/telemetry"
)

//go:embed default.yaml
var defaultYAML []byte

var (
	version = "dev"
	commit  = "none"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Environ(), os.Stderr, nil); err != nil {
		fmt.Fprintln(os.Stderr, "scp:", err)
		os.Exit(1)
	}
}

// run is main without process-global side effects, so tests drive it directly.
// onListen reports each bound listener (tests use ":0").
func run(ctx context.Context, set []string, environ []string, stderr io.Writer, onListen func(name string, a net.Addr)) error {
	var cfg Config
	res, err := config.Load(&cfg, config.Options{Defaults: defaultYAML, Environ: environ, Set: set})
	if err != nil {
		return err
	}
	lvl, _ := logx.ParseLevel(cfg.Log.Level)
	log := slog.New(logx.NewHandler(stderr, logx.Options{Level: lvl, Format: cfg.Log.Format, AddSource: cfg.Log.AddSource, Service: "scp", Version: version, Build: commit}))
	for _, w := range res.Warnings {
		log.Warn("config", slog.String("warning", w))
	}

	tel, err := telemetry.New(ctx, cfg.Telemetry, telemetry.BuildInfo{Service: "scp", Version: version, Commit: commit}, telemetry.Options{})
	if err != nil {
		return err
	}
	trusted, err := httpx.ParseTrusted(cfg.Server.Public.TrustedProxies)
	if err != nil {
		return err
	}
	rejected, err := tel.Meter("scp/httpx").Int64Counter("scp_ratelimit_rejected_total", metric.WithDescription("Requests refused by a keyed rate limiter."))
	if err != nil {
		return err
	}
	lim := map[string]*httpx.Limiter{
		"api": httpx.NewLimiter(httpx.LimiterOptions{Rule: cfg.RateLimit.API, MaxKeys: cfg.RateLimit.MaxKeys, IdleTTL: cfg.RateLimit.IdleTTL, Class: "api", Rejected: rejected}),
	}
	deps := httpx.Deps{
		Log: log, MeterProvider: tel.MeterProvider(), TracerProvider: tel.TracerProvider(), Propagator: tel.Propagator(),
		Clients: httpx.NewClientIPResolver(trusted), Limiters: lim, BodyLimit: 1 << 20, PublicURL: cfg.Server.Public.PublicURL, HSTS: cfg.Server.Public.HSTS, AdminHosts: cfg.Server.Admin.AllowedHosts,
		Classify: func(r *http.Request) httpx.Class { return httpx.Class{Headers: httpx.HeadersAPI} },
		Hooks: httpx.Hooks{
			Recovered:       func(w http.ResponseWriter, r *http.Request, rec any) { http.Error(w, "internal", 500) },
			RateLimited:     func(w http.ResponseWriter, r *http.Request, ra time.Duration) { http.Error(w, "rate-limited", 429) },
			DenyCrossOrigin: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "forbidden", 403) }),
		},
	}
	pub := http.NewServeMux()
	pub.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	adm := http.NewServeMux()
	if cfg.Telemetry.Prometheus {
		adm.Handle("GET /metrics", tel.MetricsHandler()) // the route table's admin.metrics entry (fixed path)
	}
	pubSrv := httpx.NewServer(cfg.Server.Public.Listen, httpx.PublicChain(deps)(pub), cfg.Server.Timeouts, log)
	admSrv := httpx.NewServer(cfg.Server.Admin.Listen, httpx.AdminChain(deps, cfg.Server.Admin.BearerToken)(adm), cfg.Server.Timeouts, log)

	listen := func(name, addr string) (net.Listener, error) {
		ln, err := net.Listen("tcp", addr)
		if err == nil && onListen != nil {
			onListen(name, ln.Addr())
		}
		return ln, err
	}
	pln, err := listen("public", cfg.Server.Public.Listen)
	if err != nil {
		return err
	}
	aln, err := listen("admin", cfg.Server.Admin.Listen)
	if err != nil {
		pln.Close()
		return err
	}
	sh := cfg.Shutdown
	plan := lifecycle.Plan{
		DrainDelay: sh.DrainDelay,
		Log:        log,
		Stages: []lifecycle.Stage{
			{Name: "telemetry", Close: tel.Shutdown, StopTimeout: sh.TelemetryTimeout},
			{Name: "ratelimit-sweeper", Run: func(c context.Context) error { return httpx.RunSweepers(c, lim["api"]) }, StopTimeout: 2 * time.Second},
			// one stage for both listeners: they stop together within one grace, not one after the other
			{Name: "servers", Run: func(c context.Context) error {
				return httpx.ServeAll(c, sh.ServerTimeout, httpx.Listener{Srv: pubSrv, Ln: pln}, httpx.Listener{Srv: admSrv, Ln: aln})
			}, StopTimeout: sh.ServerTimeout + time.Second},
		},
	}
	err = lifecycle.Run(ctx, plan)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
