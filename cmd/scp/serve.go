package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	contract "github.com/cantara/source-code-portal/api"
	"github.com/cantara/source-code-portal/internal/badges"
	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/commits"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/docs"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/llms"
	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
	"github.com/cantara/source-code-portal/internal/webhook"
	"github.com/cantara/source-code-portal/web"
)

const (
	publishDebounce = 200 * time.Millisecond
	publishMaxDelay = time.Second
)

func newServeCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the portal until SIGINT or SIGTERM",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := loadConfig(cmd, opts)
			if err != nil {
				return err
			}
			log := newLogger(cfg.Log, cmd.ErrOrStderr())
			slog.SetDefault(log)
			srv, err := newServer(cmd.Context(), cfg, nil, log)
			if err != nil {
				return failure{err}
			}
			if err := srv.run(cmd.Context()); err != nil {
				return failure{err}
			}
			return nil
		},
	}
	cmd.Flags().String("public-addr", "", "public listener address (default :9090)")
	cmd.Flags().String("admin-addr", "", "admin listener address (default :9091)")
	return cmd
}

// api composes the strict server from the features that implement its methods. The aliases give the embedded fields distinct names.
type (
	catalogAPI   = catalog.API
	commitsAPI   = commits.API
	docsAPI      = docs.API
	manifestsAPI = manifests.API
	api          struct {
		catalogAPI
		commitsAPI
		docsAPI
		manifestsAPI
		httpapi.SnapshotAPI
	}
)

// server owns the store, the snapshot, the sync engine and the two listeners.
type server struct {
	log               *slog.Logger
	grace             time.Duration
	store             *store.DB
	pub               *snapshot.Publisher
	engine            *sync.Engine
	stopping          atomic.Bool
	public, admin     *http.Server
	publicLn, adminLn net.Listener
}

// newServer opens the store, wires the features and binds both listeners. base is the transport to GitHub (nil: the default).
func newServer(ctx context.Context, cfg config.Config, base http.RoundTripper, log *slog.Logger) (_ *server, err error) {
	spa, err := web.FS()
	if err != nil {
		return nil, err
	}
	gh, err := github.New(cfg.GitHub, base, log)
	if err != nil {
		return nil, err
	}
	sources := map[string]catalog.GitHub{}
	fetchers := map[string]commits.Fetcher{}
	trees := map[string]docs.GitHub{}
	manifestTrees := map[string]manifests.GitHub{}
	releases := map[string]badges.Releases{}
	ids := make([]string, 0, len(cfg.GitHub.Sources))
	for _, s := range cfg.GitHub.Sources {
		src := gh.Source(s.ID)
		sources[s.ID], fetchers[s.ID], trees[s.ID], manifestTrees[s.ID], releases[s.ID] = src, src, src, src, src
		ids = append(ids, s.ID)
	}

	s := &server{log: log, grace: cfg.Server.ShutdownGrace, pub: snapshot.NewPublisher(publishDebounce, publishMaxDelay, log)}
	if s.store, err = store.Open(ctx, cfg.Store.Path, log); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = s.store.Close()
		}
	}()
	s.engine = sync.New(s.store, s.pub, gh, ids, cfg.Sync.Concurrency, log)
	cat, err := catalog.New(cfg.Catalog, cfg.GitHub.Sources, sources, s.engine.Notify, log)
	if err != nil {
		return nil, err
	}
	// Registration order is publish order at the warm start: the catalogue first, then what hangs off its repositories.
	for _, spec := range []sync.Spec{cat.Spec(), commits.New(fetchers).Spec(), docs.New(trees, safehtml.New()).Spec(), cat.DescriptorSpec(),
		badges.New(releases).ReleaseSpec(), manifests.New(manifestTrees).Spec()} {
		if err := s.engine.Register(spec); err != nil {
			return nil, err
		}
	}

	m := newMetrics(gauges{current: s.pub.Current, depth: s.engine.Depth, rate: gh.RateRemaining})
	gh.Observe(m.githubResponse)
	s.engine.Observe(m.job)
	ready := func() bool { return !s.stopping.Load() && s.pub.Current().Catalogued }
	st := &status{ready: ready, alive: s.engine.Alive, current: s.pub.Current, depth: s.engine.Depth, rate: gh.RateRemaining,
		webhook: cfg.Webhook.Secret.IsSet(), deliveries: map[string]int{}}

	md := llms.New(s.pub.Current, cfg.Server.PublicURL)
	routes := []httpapi.Route{{Pattern: "GET /badges/", Handler: badges.Handler(s.pub.Current)},
		{Pattern: "GET /llms.txt", Handler: md.Index()}, {Pattern: "GET /llms-full.txt", Handler: md.Full()},
		{Pattern: "GET /repos/{owner}/{name}/readme.md", Handler: md.Readme()},
		{Pattern: "GET /repos/{owner}/{name}", Handler: md.Repo(), Markdown: true},
		{Pattern: "GET /repos/{owner}/{name}/docs/{path...}", Handler: md.Doc(), Markdown: true},
		{Pattern: "GET /groups/{group}", Handler: md.Group(), Markdown: true}}
	if cfg.Webhook.Secret.IsSet() {
		rc := webhook.New(cfg.Webhook.Secret.Reveal(), cfg.Webhook.PreviousSecret.Reveal(), webhookSink(s.engine, cfg.GitHub.Sources), log)
		rc.Observe(func(event, result string) { m.delivery(event, result); st.delivery(event, result) })
		routes = append(routes, httpapi.Route{Pattern: webhook.Pattern, MaxBody: webhook.MaxBody, Handler: rc})
	}
	opts := httpapi.Options{Log: log, Build: build, HSTS: strings.HasPrefix(cfg.Server.PublicURL, "https://"), TrustedProxies: cfg.Server.TrustedProxies,
		Observe: m.request}
	features := api{catalog.API{Current: s.pub.Current}, commits.API{Current: s.pub.Current}, docs.API{Current: s.pub.Current},
		manifests.API{Current: s.pub.Current}, httpapi.SnapshotAPI{Current: s.pub.Current}}
	s.public = httpapi.Server(httpapi.Public(opts, spa, contract.OpenAPI, features, routes...), log)
	admin := []httpapi.Route{{Pattern: "GET /metrics", Handler: m.handler()}, {Pattern: "GET /api/v1/status", Handler: st}}
	if cfg.Ops.Pprof {
		admin = append(admin, httpapi.Route{Pattern: "GET /debug/pprof/", Handler: http.HandlerFunc(pprof.Index)},
			httpapi.Route{Pattern: "GET /debug/pprof/cmdline", Handler: http.HandlerFunc(pprof.Cmdline)},
			httpapi.Route{Pattern: "GET /debug/pprof/profile", Handler: http.HandlerFunc(pprof.Profile)},
			httpapi.Route{Pattern: "GET /debug/pprof/symbol", Handler: http.HandlerFunc(pprof.Symbol)},
			httpapi.Route{Pattern: "GET /debug/pprof/trace", Handler: http.HandlerFunc(pprof.Trace)})
	}
	s.admin = httpapi.Server(httpapi.Admin(opts, s.engine.Alive, ready, admin...), log)
	var lc net.ListenConfig
	if s.publicLn, err = lc.Listen(ctx, "tcp", cfg.Server.PublicAddr); err != nil {
		return nil, err
	}
	if s.adminLn, err = lc.Listen(ctx, "tcp", cfg.Server.AdminAddr); err != nil {
		_ = s.publicLn.Close()
		return nil, err
	}
	return s, nil
}

// webhookSink hands webhook events to the engine: repository events to the repository's jobs, owner-wide events to the listing of
// the source that has the owner among its organisations. Owners no source lists are ignored.
func webhookSink(e *sync.Engine, sources []config.Source) webhook.Sink {
	sourceOf := map[string]string{}
	for _, s := range sources {
		for _, org := range s.Orgs {
			sourceOf[strings.ToLower(org)] = s.ID
		}
	}
	return func(ev sync.Event, owner, name string) {
		id, ok := sourceOf[strings.ToLower(owner)]
		switch {
		case !ok:
		case name == "" || ev == sync.EventSourceChanged:
			e.NotifySource(ev, id)
		default:
			e.Notify(ev, snapshot.KeyOf(owner, name))
		}
	}
}

// run serves until ctx is cancelled, then shuts down within the grace period: readiness drops, the servers drain, the engine
// stops (jobs in flight are cancelled and run again after a restart), pending snapshot updates are dropped and the store closes.
func (s *server) run(ctx context.Context) error {
	engineCtx, stopEngine := context.WithCancel(context.WithoutCancel(ctx))
	engineDone := make(chan error, 1)
	go func() { engineDone <- s.engine.Run(engineCtx) }()

	errc := make(chan error, 2)
	for _, p := range []struct {
		name string
		srv  *http.Server
		ln   net.Listener
	}{{"public", s.public, s.publicLn}, {"admin", s.admin, s.adminLn}} {
		s.log.Info("listening", "listener", p.name, "addr", p.ln.Addr().String())
		go func() { errc <- p.srv.Serve(p.ln) }()
	}

	var serveErr error
	serversLeft, engineRunning := 2, true
	select {
	case <-ctx.Done():
		s.log.Info("shutting down", "grace", s.grace.String())
	case serveErr = <-errc:
		serversLeft--
		s.log.Error("listener failed", "err", serveErr)
	case serveErr = <-engineDone:
		engineRunning = false
		s.log.Error("sync engine stopped", "err", serveErr)
	}
	s.stopping.Store(true)

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.grace)
	defer cancel()
	err := errors.Join(serveErr, s.public.Shutdown(shutdownCtx), s.admin.Shutdown(shutdownCtx))
	for range serversLeft {
		if e := <-errc; !errors.Is(e, http.ErrServerClosed) {
			err = errors.Join(err, e)
		}
	}
	stopEngine()
	if engineRunning {
		err = errors.Join(err, <-engineDone)
	}
	s.pub.Close()
	return errors.Join(err, s.store.Close())
}
