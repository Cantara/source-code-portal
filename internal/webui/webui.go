// Package webui runs the web UI's server, the Next.js standalone build in web.dir, as a child process on a loopback port: started
// with the portal, restarted when it exits, stopped at shutdown. httpapi.Options.Pages proxies the pages to it.
package webui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	minBackoff = time.Second
	maxBackoff = 30 * time.Second
	steady     = time.Minute     // a child that ran this long restarts after minBackoff again
	stopGrace  = 5 * time.Second // SIGTERM to SIGKILL at shutdown
	probeEvery = 100 * time.Millisecond
)

// Server is the supervised web UI server.
type Server struct {
	dir, node string
	log       *slog.Logger
	addr      string // 127.0.0.1:port the child listens on
	ready     atomic.Bool
}

// New picks a free loopback port for the server in dir (server.js), run with node.
func New(dir, node string, log *slog.Logger) (*Server, error) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("web UI port: %w", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return &Server{dir: dir, node: node, log: log.With("component", "web"), addr: addr}, nil
}

// URL is the address the pages are proxied to.
func (s *Server) URL() *url.URL { return &url.URL{Scheme: "http", Host: s.addr} }

// Ready reports whether the server accepts connections.
func (s *Server) Ready() bool { return s.ready.Load() }

// Run starts the server and restarts it after every exit (1 s doubling to 30 s) until ctx ends, then stops it: SIGTERM, and
// SIGKILL after 5 s. api is the portal's API base the server calls (SCP_API, for example http://127.0.0.1:9090).
func (s *Server) Run(ctx context.Context, api string) error {
	backoff := minBackoff
	for {
		start := time.Now()
		err := s.once(ctx, api)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(start) > steady {
			backoff = minBackoff
		}
		s.log.Error("web UI server exited; restarting", "err", err, "in", backoff.String())
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, maxBackoff)
	}
}

// once runs the server until it exits or ctx ends.
func (s *Server) once(ctx context.Context, api string) error {
	_, port, _ := net.SplitHostPort(s.addr)
	out := &lines{log: s.log}
	cmd := exec.CommandContext(ctx, s.node, "server.js") //nolint:gosec // G204: the operator's web.node runs the operator's web.dir
	cmd.Dir = s.dir
	// Only what the server needs: the portal's own environment can hold its configuration.
	cmd.Env = []string{"PORT=" + port, "HOSTNAME=127.0.0.1", "NODE_ENV=production", "NEXT_TELEMETRY_DISABLED=1", "SCP_API=" + api,
		"PATH=" + os.Getenv("PATH")}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = stopGrace
	if err := cmd.Start(); err != nil {
		return err
	}
	s.log.Info("web UI server started", "pid", cmd.Process.Pid, "addr", s.addr)
	done := make(chan struct{})
	go s.probe(ctx, done)
	err := cmd.Wait()
	close(done)
	s.ready.Store(false)
	out.flush()
	if err == nil {
		err = errors.New("exit status 0")
	}
	return err
}

// probe marks the server ready once it accepts a connection.
func (s *Server) probe(ctx context.Context, done <-chan struct{}) {
	t := time.NewTicker(probeEvery)
	defer t.Stop()
	d := net.Dialer{Timeout: time.Second}
	for {
		select {
		case <-done:
			return
		case <-t.C:
		}
		if c, err := d.DialContext(ctx, "tcp", s.addr); err == nil {
			_ = c.Close()
			s.ready.Store(true)
			s.log.Info("web UI server ready")
			return
		}
	}
}

// lines logs what the server writes, one record per line.
type lines struct {
	log *slog.Logger
	mu  sync.Mutex
	buf []byte
}

func (l *lines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.emit(l.buf[:i])
		l.buf = l.buf[i+1:]
	}
	if len(l.buf) > 8<<10 { // a very long line is logged in parts
		l.emit(l.buf)
		l.buf = nil
	}
	return len(p), nil
}

func (l *lines) flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) > 0 {
		l.emit(l.buf)
		l.buf = nil
	}
}

func (l *lines) emit(b []byte) {
	if b = bytes.TrimSpace(b); len(b) > 0 {
		l.log.Info("web UI server output", "line", string(b))
	}
}
