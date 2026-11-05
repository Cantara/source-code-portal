package webui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestMain doubles as the fake node: run as "<test binary> server.js" it serves like the web UI server would.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "server.js" {
		fakeServer()
		return
	}
	goleak.VerifyTestMain(m)
}

// fakeServer answers on HOSTNAME:PORT with its environment and working directory until SIGTERM; a file "crash" in its directory
// makes it exit at once (and removes the file, so the restart stays up).
func fakeServer() {
	if os.Remove("crash") == nil {
		os.Exit(3)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(os.Getenv("HOSTNAME"), os.Getenv("PORT")))
	if err != nil {
		os.Exit(2)
	}
	fmt.Println("listening")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM)
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		wd, _ := os.Getwd()
		_, _ = fmt.Fprintf(w, "%s %s %s %d", os.Getenv("SCP_API"), os.Getenv("SCP_VERSION"), wd, len(os.Environ()))
	})}
	go func() { _ = srv.Serve(ln) }()
	<-stop
	_ = srv.Close()
}

// logs collects the log lines.
type logs struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestServer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/crash", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/BUILD", []byte("commit-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCP_SECRET_THING", "must not leak")
	out := &logs{}
	s, err := New(dir, os.Args[0], "2.0.1", slog.New(slog.NewTextHandler(out, nil)))
	if err != nil {
		t.Fatal(err)
	}
	// The intervals shortened, and every step told in order: the test waits on what happened, not on time.
	s.minBackoff, s.buildEvery, s.probeEvery = 10*time.Millisecond, 10*time.Millisecond, 5*time.Millisecond
	events := make(chan string, 16)
	s.observe = func(e string) { events <- e }
	expect := func(want ...string) {
		t.Helper()
		for _, w := range want {
			select {
			case got := <-events:
				if got != w {
					t.Fatalf("event %q, want %q; log:\n%s", got, w, out)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("no %q; log:\n%s", w, out)
			}
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, "http://127.0.0.1:9") }()

	expect("exited", "ready")               // the first start crashes; the restart stays up
	resp, err := http.Get(s.URL().String()) //nolint:forbidigo // loopback test server
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if got := string(body); !strings.HasPrefix(got, "http://127.0.0.1:9 2.0.1 ") || !strings.Contains(got, dir[strings.LastIndex(dir, "/"):]) ||
		!strings.HasSuffix(got, " 7") {
		t.Errorf("server saw %q: want SCP_API, SCP_VERSION, its directory %s and seven variables", got, dir)
	}
	if !strings.Contains(out.String(), "restarting") || !strings.Contains(out.String(), "line=listening") {
		t.Errorf("log:\n%s", out)
	}
	if got := s.Build(); got != "commit-1" {
		t.Errorf("Build = %q, want the bundle's commit-1", got)
	}
	// A new bundle written under the running server restarts it at once, and Build names the new one. A rebuild of the same commit
	// rewrites BUILD with the same content: its new modification time still counts.
	if err := os.WriteFile(dir+"/BUILD", []byte("commit-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir+"/BUILD", time.Time{}, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	expect("replaced", "ready")
	if err := os.WriteFile(dir+"/BUILD", []byte("commit-2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	expect("replaced", "ready")
	if got := s.Build(); got != "commit-2" {
		t.Errorf("Build = %q after a new commit's bundle, want commit-2", got)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the context ended")
	}
	if s.Ready() {
		t.Error("ready after stop")
	}
}
