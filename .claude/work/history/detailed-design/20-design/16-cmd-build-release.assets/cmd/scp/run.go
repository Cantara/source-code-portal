package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/platform/httpx"
	"github.com/cantara/source-code-portal/internal/platform/lifecycle"
	"github.com/cantara/source-code-portal/internal/platform/logx"
	"github.com/cantara/source-code-portal/internal/platform/telemetry"
)

// setFlags collects repeated --set path=value.
type setFlags []string

func (s *setFlags) String() string     { return "" }
func (s *setFlags) Set(v string) error { *s = append(*s, v); return nil }

// cmdServe is startup steps 1 to 9 of design 16 section 4.2 around the product's feature wiring (env.Wire).
// Exit codes: 0 clean stop, 1 runtime failure (an unreadable file, a bind error, a stage that failed), 2 configuration refused (every violation printed).
func cmdServe(ctx context.Context, env Env, args []string) int {
	fl := newFlagSet(env, "serve")
	path := configPathFlag(fl, env)
	var sets setFlags
	fl.Var(&sets, "set", "override one key: path=value (repeatable; secrets are refused)")
	if code, done := parseFlags(fl, args); done {
		return code
	}
	if fl.NArg() != 0 {
		_, _ = fmt.Fprintln(env.Stderr, "scp serve: no positional arguments")
		return exitUsage
	}

	// S1 configuration: strict decode, validate, resolve secrets. Every violation is printed; nothing has started yet.
	l, err := readConfig(loadOptions{file: *path, environ: env.Environ, set: sets})
	if err != nil { // the file cannot be read at all: an I/O failure, not a refusal of its content
		_, _ = fmt.Fprintf(env.Stderr, "scp: %v\n", err)
		return exitFail
	}
	if len(l.violations) > 0 {
		_, _ = fmt.Fprintf(env.Stderr, "scp: %d configuration problem(s):\n%s", len(l.violations), l.violationText())
		return exitInvalid
	}
	cfg := l.cfg
	bi := readBuildInfo()
	// The OTLP headers file is read here, not by telemetry (rank 0 imports neither secret nor config): a problem in it is a
	// configuration violation, exit 2, with line numbers and never a value (02 section 4.4).
	var otlpHeaders map[string]string
	scrub := secretsOf(&cfg)
	if cfg.Telemetry.OTLP.Enabled && cfg.Telemetry.OTLP.HeadersFile != "" {
		h, hs, problems := parseHeadersFile(cfg.Telemetry.OTLP.HeadersFile)
		if len(problems) > 0 {
			_, _ = fmt.Fprintf(env.Stderr, "scp: %d configuration problem(s):\n  %s\n", len(problems), strings.Join(problems, "\n  "))
			return exitInvalid
		}
		otlpHeaders, scrub = h, append(scrub, hs...)
	}
	lvl, _ := logx.ParseLevel(cfg.Log.Level)
	log := slog.New(logx.NewHandler(env.Stderr, logx.Options{Level: lvl, Format: cfg.Log.Format, AddSource: cfg.Log.AddSource, Service: "scp", Version: bi.Version, Build: bi.Build, Scrub: scrub}))
	routeOTelErrors(ctx, log)
	for _, w := range l.warnings {
		log.WarnContext(ctx, "config", slog.String("warning", w))
	}
	step := func(name string, fn func() error) error {
		t0 := time.Now()
		err := fn()
		log.InfoContext(ctx, "startup step", slog.String("step", name), slog.Duration("took", time.Since(t0)), logx.Err(err))
		return err
	}

	var tel *telemetry.Provider
	if err := step("telemetry", func() (err error) {
		client, cerr := otlpClient(cfg.Telemetry.OTLP)
		if cerr != nil {
			return cerr
		}
		tel, err = telemetry.New(ctx, cfg.Telemetry, telemetry.BuildInfo{Service: "scp", Version: bi.Version, Commit: bi.Build, Instance: env.Hostname}, telemetry.Options{Headers: otlpHeaders, HTTPClient: client})
		return err
	}); err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp: telemetry: %v\n", err)
		return exitFail
	}

	// From here until lifecycle.Run owns them, every early return must release what was built: telemetry (its last export carries the
	// startup error, the one signal worth having) and whatever the wire opened (the store). started flips just before lifecycle.Run.
	var (
		w       Wired
		started bool
	)
	defer func() {
		if started {
			return
		}
		bg := context.WithoutCancel(ctx)
		if w.Abort != nil {
			_ = w.Abort(bg)
		}
		_ = tel.Shutdown(bg)
	}()

	trusted, err := httpx.ParseTrusted(cfg.Server.Public.TrustedProxies)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp: %v\n", err)
		return exitFail
	}
	rl := cfg.RateLimit
	rejected, err := tel.Meter("scp/httpx").Int64Counter("scp_ratelimit_rejected_total", metric.WithDescription("Requests refused by a keyed rate limiter."))
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp: %v\n", err)
		return exitFail
	}
	newLimiter := func(class string, rule httpx.Rate) *httpx.Limiter {
		return httpx.NewLimiter(httpx.LimiterOptions{Rule: rule, MaxKeys: rl.MaxKeys, IdleTTL: rl.IdleTTL, Class: class, Rejected: rejected})
	}
	limiters := map[string]*httpx.Limiter{
		"api":             newLimiter("api", rl.API),
		"badges":          newLimiter("badges", rl.Badges),
		"webhook_invalid": newLimiter("webhook_invalid", rl.WebhookInvalid),
		"csp_reports":     newLimiter("csp_reports", rl.CSPReports),
		"media_ext":       newLimiter("media_ext", rl.MediaExt),
	}
	if _, err := tel.Meter("scp/httpx").Int64ObservableGauge("scp_ratelimit_keys", metric.WithDescription("Keys tracked by each rate limiter; at ratelimit.max_keys the overflow bucket is in use."),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			for class, lim := range limiters {
				o.Observe(int64(lim.Len()), metric.WithAttributes(attribute.String("class", class)))
			}
			return nil
		})); err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp: %v\n", err)
		return exitFail
	}
	seed := env.Seed
	if seed == 0 {
		var b [8]byte
		_, _ = rand.Read(b[:])
		seed = binary.LittleEndian.Uint64(b[:]) | 1
	}

	// S2-S7 the features (store, readmodel, credentials, warm load, engine, handlers): the product's wire.go.
	if env.Wire == nil {
		_, _ = fmt.Fprintln(env.Stderr, "scp serve: not available in this build")
		return exitFail
	}
	if err := step("wire", func() (err error) {
		w, err = env.Wire(ctx, WireIn{Cfg: cfg, Log: log, Tel: tel, Limiters: limiters, Seed: seed})
		return err
	}); err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp: %v\n", err)
		return exitFail
	}

	deps := httpx.Deps{
		Log: log, MeterProvider: tel.MeterProvider(), TracerProvider: tel.TracerProvider(), Propagator: tel.Propagator(),
		Clients: httpx.NewClientIPResolver(trusted), Limiters: limiters, BodyLimit: 1 << 20, PublicURL: cfg.Server.Public.PublicURL, HSTS: cfg.Server.Public.HSTS, AdminHosts: cfg.Server.Admin.AllowedHosts,
		Classify: w.Classify, Hooks: w.Hooks, AfterClientIP: w.AfterClientIP,
	}
	pubSrv := httpx.NewServer(cfg.Server.Public.Listen, httpx.PublicChain(deps)(w.Public), cfg.Server.Timeouts, log)
	admSrv := httpx.NewServer(cfg.Server.Admin.Listen, httpx.AdminChain(deps, cfg.Server.Admin.BearerToken)(w.Admin), cfg.Server.Timeouts, log)

	// S8a both listeners open BEFORE lifecycle.Run: a bind error is a startup error with a clear message, not a stage failure.
	// They open after the wire (store open, migrated, warm start decoded), so /livez answers while the engine catches up (V16-11).
	pln, err := env.Listen(cfg.Server.Public.Listen)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp: public listener: %v\n", err)
		return exitFail
	}
	aln, err := env.Listen(cfg.Server.Admin.Listen)
	if err != nil {
		_ = pln.Close()
		_, _ = fmt.Fprintf(env.Stderr, "scp: admin listener: %v\n", err)
		return exitFail
	}
	log.InfoContext(ctx, "listening", slog.String("public", pln.Addr().String()), slog.String("admin", aln.Addr().String()))

	phase, _ := tel.Meter("scp/lifecycle").Float64Histogram("scp_lifecycle_phase_seconds",
		metric.WithUnit("s"), metric.WithDescription("Duration of each shutdown or startup phase of each lifecycle stage."))
	observe := func(stage, ph string, d time.Duration, err error) {
		out := "ok"
		switch {
		case errors.Is(err, lifecycle.ErrStopTimeout):
			out = "timeout" // CD-MT-01: the vocabulary of unit 02 (ok|error|timeout)
		case err != nil:
			out = "error"
		}
		phase.Record(context.WithoutCancel(ctx), d.Seconds(), metric.WithAttributes(attribute.String("stage", stage), attribute.String("phase", ph), attribute.String("outcome", out)))
	}
	sh := cfg.Shutdown
	plan := buildPlan(cfg, Parts{
		TelemetryClose: tel.Shutdown,
		Features:       w.Features,
		Sweep:          func(c context.Context) error { return httpx.RunSweepers(c, sweepList(limiters)...) },
		Hup:            env.Reload,
		Reload:         w.Reload,
		Draining:       w.Draining,
		ServersRun: func(c context.Context) error {
			return httpx.ServeAll(c, sh.ServerTimeout, httpx.Listener{Srv: pubSrv, Ln: pln}, httpx.Listener{Srv: admSrv, Ln: aln})
		},
		Log:     log,
		Observe: observe,
	})
	log.InfoContext(ctx, "shutdown budget", slog.Duration("worst_case", cfg.ShutdownBudget()), slog.Duration("supervisor_grace", sh.SupervisorGrace))
	// S8b run until a signal (ctx) or a stage fails; the stages stop in reverse order.
	started = true
	if err := lifecycle.Run(ctx, plan); err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintf(env.Stderr, "scp: %v\n", err)
		return exitFail
	}
	return exitOK
}

func sweepList(m map[string]*httpx.Limiter) []*httpx.Limiter {
	out := make([]*httpx.Limiter, 0, len(m))
	for _, k := range []string{"api", "badges", "webhook_invalid", "csp_reports", "media_ext"} { // fixed order: deterministic
		if l, ok := m[k]; ok {
			out = append(out, l)
		}
	}
	return out
}
