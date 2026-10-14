package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/httpx"
	"github.com/cantara/source-code-portal/internal/platform/lifecycle"
	"github.com/cantara/source-code-portal/internal/platform/telemetry"
	"github.com/cantara/source-code-portal/internal/routes"
)

// WireIn is what the startup sequence has built when it calls the feature wiring (steps 1 of design 16 section 4.2 are done).
type WireIn struct {
	Cfg      Config
	Log      *slog.Logger
	Tel      *telemetry.Provider
	Limiters map[string]*httpx.Limiter // "api", "badges", "webhook_invalid", ... by class; the sweeper stage runs over all of them
	Seed     uint64
}

// Wired is what steps 2 to 7 produce: handlers for both listeners, the stages of the features, and the hooks the HTTP chains need.
type Wired struct {
	Public, Admin http.Handler
	Features      []lifecycle.Stage // in START order: store (worker + plan flusher; Close = final flush, checkpoint), refresh (engine, scheduler)
	Classify      func(*http.Request) httpx.Class
	Hooks         httpx.Hooks
	AfterClientIP []func(http.Handler) http.Handler // access.Middleware (14 section 4.7.2)
	Draining      func(context.Context) error       // ops readiness.SetShuttingDown
	// Reload answers SIGHUP (Env.Reload): github.Client.ReloadKeys and the pages reload trigger, errors joined; nil when there is nothing to
	// reload. It runs inside the housekeeping stage, never concurrently with itself, and an error is logged, never fatal.
	Reload func(context.Context) error
	// Abort releases what S4 to S9 built when serve fails BEFORE lifecycle.Run took ownership of it (a bind error, a bad CIDR): the store
	// handle and the idle HTTP connections. Optional; telemetry is shut down by serve itself.
	Abort func(context.Context) error
}

// WireFunc is the seam between the generic serve command and the product's feature wiring (wire_*.go in the product).
type WireFunc func(ctx context.Context, in WireIn) (Wired, error)

// productWire in the scratch skeleton wires nothing but the probes and /metrics, by route NAME through the real kernel tables
// (01 section 7.5); the real one is design 16 section 4.2 steps 2-7 and ends with Verify on both tables (here Verify would
// correctly report the routes nobody registered).
func productWire(_ context.Context, in WireIn) (Wired, error) {
	pub, adm := http.NewServeMux(), http.NewServeMux()
	pt, at := routes.Public(), routes.Admin()
	live := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	ready := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ready")) })
	pt.Handle(pub, "probe.live", live)
	pt.Handle(pub, "probe.ready", ready)
	at.Handle(adm, "probe.live", live)
	at.Handle(adm, "probe.ready", ready)
	if in.Cfg.Telemetry.Prometheus {
		at.Handle(adm, "admin.metrics", in.Tel.MetricsHandler())
	}
	return Wired{
		Public:   pub,
		Admin:    adm,
		Classify: func(*http.Request) httpx.Class { return httpx.Class{Headers: httpx.HeadersAPI} },
		Hooks: httpx.Hooks{
			Recovered: func(w http.ResponseWriter, _ *http.Request, _ any) {
				http.Error(w, "internal", http.StatusInternalServerError)
			},
			RateLimited: func(w http.ResponseWriter, _ *http.Request, _ time.Duration) {
				http.Error(w, "rate limited", http.StatusTooManyRequests)
			},
			DenyCrossOrigin: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "forbidden", http.StatusForbidden) }),
		},
	}, nil
}
