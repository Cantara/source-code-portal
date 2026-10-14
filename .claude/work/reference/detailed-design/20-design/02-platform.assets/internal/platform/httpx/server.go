package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// NewServer is the only constructor of *http.Server in the module (G4).
// errLog receives net/http's own error lines (TLS handshake noise, panics
// outside handlers) at Warn.
func NewServer(addr string, h http.Handler, t TimeoutsConfig, errLog *slog.Logger) *http.Server {
	srv := &http.Server{
		Addr:                addr,
		Handler:             h,
		ReadHeaderTimeout:   t.ReadHeader,
		ReadTimeout:         t.Read,
		WriteTimeout:        t.Write,
		IdleTimeout:         t.Idle,
		MaxHeaderBytes:      int(t.MaxHeaderBytes),
		MaxHeaderValueCount: t.MaxHeaderValues,
	}
	if errLog != nil {
		srv.ErrorLog = slog.NewLogLogger(errLog.Handler(), slog.LevelWarn)
	}
	return srv
}

// Serve serves ln until ctx is cancelled, then shuts down gracefully for at
// most grace and force-closes what remains. It returns nil on an orderly stop.
// It exists so cmd/scp can build a lifecycle.Stage without httpx importing
// lifecycle: Stage{Run: func(ctx) error { return httpx.Serve(ctx, srv, ln, grace) }}.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, grace time.Duration) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grace)
	defer cancel()
	err := srv.Shutdown(sctx)
	if err != nil {
		_ = srv.Close()
	}
	if serr := <-errc; serr != nil && !errors.Is(serr, http.ErrServerClosed) {
		return serr
	}
	return err
}

// Listener pairs a server with its already-bound listener (a bind error is a startup error, found before lifecycle.Run).
type Listener struct {
	Srv *http.Server
	Ln  net.Listener
}

// ServeAll serves every listener concurrently and stops them together: on ctx cancellation each is shut down gracefully
// for at most grace in parallel (so the budget is grace, not grace times the number of listeners); if one fails on its own,
// the others are stopped the same way and the errors are joined, the failing one first. It is the body of the single
// "servers" lifecycle stage.
func ServeAll(ctx context.Context, grace time.Duration, ls ...Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make([]error, len(ls))
	var wg sync.WaitGroup
	first := make(chan int, len(ls))
	for i, l := range ls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = Serve(ctx, l.Srv, l.Ln, grace)
			if ctx.Err() == nil && errs[i] != nil {
				first <- i
				cancel()
			}
		}()
	}
	wg.Wait()
	var out []error
	select {
	case i := <-first:
		out = append(out, errs[i])
		errs[i] = nil
	default:
	}
	return errors.Join(append(out, errs...)...)
}
