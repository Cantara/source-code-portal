package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/web"
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

// server owns the two listeners and their HTTP servers.
type server struct {
	log               *slog.Logger
	grace             time.Duration
	public, admin     *http.Server
	publicLn, adminLn net.Listener
}

// newServer builds the handlers and binds both listeners.
func newServer(ctx context.Context, cfg config.Config, log *slog.Logger) (*server, error) {
	spa, err := web.FS()
	if err != nil {
		return nil, err
	}
	s := &server{
		log:    log,
		grace:  cfg.Server.ShutdownGrace,
		public: newHTTPServer(httpapi.Public(spa), log),
		admin:  newHTTPServer(httpapi.Admin(), log),
	}
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

// run serves until ctx is cancelled, then shuts both servers down within the grace period.
func (s *server) run(ctx context.Context) error {
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
	select {
	case <-ctx.Done():
		s.log.Info("shutting down", "grace", s.grace.String())
	case serveErr = <-errc:
		s.log.Error("listener failed", "err", serveErr)
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.grace)
	defer cancel()
	err := errors.Join(serveErr, s.public.Shutdown(shutdownCtx), s.admin.Shutdown(shutdownCtx))
	pending := 2
	if serveErr != nil {
		pending = 1
	}
	for range pending {
		if e := <-errc; !errors.Is(e, http.ErrServerClosed) {
			err = errors.Join(err, e)
		}
	}
	return err
}
