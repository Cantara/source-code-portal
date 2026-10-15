// Package webui runs the web UI's server, the Next.js standalone build in web.dir, as a child process on a loopback port: started
// with the portal, restarted when it exits or its bundle is replaced, stopped at shutdown. httpapi.Options.Pages proxies the pages to it.
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
	"path/filepath"
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
	buildEvery = 2 * time.Second // how often the bundle on disk is compared with the running one
)

// errReplaced ends a run whose bundle was replaced: the next one starts at once from the new files.
var errReplaced = errors.New("bundle replaced")

// Server is the supervised web UI server.
type Server struct {
	dir, node string
	version   string // the portal's release, shown by the pages (SCP_VERSION)
	log       *slog.Logger
	addr      string // 127.0.0.1:port the child listens on
	ready     atomic.Bool
	served    atomic.Pointer[string] // the build id of the bundle the running server was started from
}

// New picks a free loopback port for the server in dir (server.js), run with node; version is the portal's release the pages show.
func New(dir, node, version string, log *slog.Logger) (*Server, error) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("web UI port: %w", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return &Server{dir: dir, node: node, version: version, log: log.With("component", "web"), addr: addr}, nil
}

// URL is the address the pages are proxied to.
func (s *Server) URL() *url.URL { return &url.URL{Scheme: "http", Host: s.addr} }

// Build is the build id of the bundle the server runs (its BUILD file, which the pages know as theirs); empty before the first start
// or when the bundle has none.
func (s *Server) Build() string {
	if b := s.served.Load(); b != nil {
		return *b
	}
	return ""
}

// Ready reports whether the server accepts connections.
func (s *Server) Ready() bool { return s.ready.Load() }

// Run starts the server and restarts it after every exit (1 s doubling to 30 s), and at once when the bundle is replaced (its BUILD
// file is rewritten: a running server cannot load the files of another build), until ctx ends, then stops it: SIGTERM, and
// SIGKILL after 5 s. api is the portal's API base the server calls (SCP_API, for example http://127.0.0.1:9090).
func (s *Server) Run(ctx context.Context, api string) error {
	backoff := minBackoff
	for {
		start := time.Now()
		err := s.once(ctx, api)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errReplaced) {
			s.log.Info("web UI bundle replaced; restarting")
			backoff = minBackoff
			continue
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
	run, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	stamp := s.stamp()
	served := s.read("BUILD")
	s.served.Store(&served)
	cmd := exec.CommandContext(run, s.node, "server.js") //nolint:gosec // G204: the operator's web.node runs the operator's web.dir
	cmd.Dir = s.dir
	// Only what the server needs: the portal's own environment can hold its configuration.
	cmd.Env = []string{"PORT=" + port, "HOSTNAME=127.0.0.1", "NODE_ENV=production", "NEXT_TELEMETRY_DISABLED=1", "SCP_API=" + api,
		"SCP_VERSION=" + s.version, "PATH=" + os.Getenv("PATH")}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = stopGrace
	if err := cmd.Start(); err != nil {
		return err
	}
	s.log.Info("web UI server started", "pid", cmd.Process.Pid, "addr", s.addr)
	done := make(chan struct{})
	go s.probe(run, done)
	go s.watch(stamp, done, stop)
	err := cmd.Wait()
	close(done)
	if errors.Is(context.Cause(run), errReplaced) {
		err = errReplaced
	}
	s.ready.Store(false)
	out.flush()
	if err == nil {
		err = errors.New("exit status 0")
	}
	return err
}

// stamp identifies the bundle on disk: its BUILD file's content and modification time, new with every assembly (Next.js's own
// .next/BUILD_ID is not); empty without the file, as while a bundle is being replaced.
func (s *Server) stamp() string {
	fi, err := os.Stat(filepath.Join(s.dir, "BUILD")) //nolint:gosec // G703: the operator's web.dir
	if err != nil {
		return ""
	}
	return s.read("BUILD") + "@" + fi.ModTime().String()
}

// read is a one-line file of the bundle, trimmed; empty when it cannot be read.
func (s *Server) read(name string) string {
	b, _ := os.ReadFile(filepath.Join(s.dir, name)) //nolint:gosec // G703: the operator's web.dir
	return string(bytes.TrimSpace(b))
}

// watch ends the run when the bundle's stamp changes (a new bundle has been written in place).
func (s *Server) watch(stamp string, done <-chan struct{}, stop context.CancelCauseFunc) {
	t := time.NewTicker(buildEvery)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
		}
		if now := s.stamp(); now != stamp && now != "" {
			stop(errReplaced)
			return
		}
	}
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
