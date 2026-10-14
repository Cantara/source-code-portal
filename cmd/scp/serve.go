package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/commits"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
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
			srv, err := newServer(cmd.Context(), cfg, log)
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

// api composes the strict server from the features that implement its methods.
type api struct {
	catalog.API
	httpapi.SnapshotAPI
}

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

// newServer opens the store, wires the features and binds both listeners.
func newServer(ctx context.Context, cfg config.Config, log *slog.Logger) (_ *server, err error) {
	spa, err := web.FS()
	if err != nil {
		return nil, err
	}
	gh, err := github.New(cfg.GitHub, nil, log)
	if err != nil {
		return nil, err
	}
	listers := map[string]catalog.Lister{}
	fetchers := map[string]commits.Fetcher{}
	sources := make([]string, 0, len(cfg.GitHub.Sources))
	for _, s := range cfg.GitHub.Sources {
		listers[s.ID], fetchers[s.ID] = gh.Source(s.ID), gh.Source(s.ID)
		sources = append(sources, s.ID)
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
	s.engine = sync.New(s.store, s.pub, gh, sources, cfg.Sync.Concurrency, log)
	cat, err := catalog.New(cfg.Catalog, cfg.GitHub.Sources, listers, s.engine.Notify, log)
	if err != nil {
		return nil, err
	}
	// Registration order is publish order at the warm start: the catalogue first, then what hangs off its repositories.
	for _, spec := range []sync.Spec{cat.Spec(), commits.New(fetchers).Spec()} {
		if err := s.engine.Register(spec); err != nil {
			return nil, err
		}
	}

	ready := func() bool { return !s.stopping.Load() && s.pub.Current().Catalogued }
	s.public = newHTTPServer(httpapi.Public(spa, api{catalog.API{Current: s.pub.Current}, httpapi.SnapshotAPI{Current: s.pub.Current}}), log)
	s.admin = newHTTPServer(httpapi.Admin(ready), log)
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

func newHTTPServer(h http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
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
