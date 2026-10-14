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
		_, _ = fmt.Fprintf(w, "%s %s %d", os.Getenv("SCP_API"), wd, len(os.Environ()))
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
	t.Setenv("SCP_SECRET_THING", "must not leak")
	out := &logs{}
	s, err := New(dir, os.Args[0], slog.New(slog.NewTextHandler(out, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, "http://127.0.0.1:9") }()

	deadline := time.Now().Add(10 * time.Second) // the first start crashes; the restart a second later stays up
	for !s.Ready() {
		if time.Now().After(deadline) {
			t.Fatalf("not ready; log:\n%s", out)
		}
		time.Sleep(20 * time.Millisecond)
	}
	resp, err := http.Get(s.URL().String()) //nolint:forbidigo // loopback test server
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if got := string(body); !strings.HasPrefix(got, "http://127.0.0.1:9 ") || !strings.Contains(got, dir[strings.LastIndex(dir, "/"):]) ||
		!strings.HasSuffix(got, " 6") {
		t.Errorf("server saw %q: want SCP_API, its directory %s and six variables", got, dir)
	}
	if !strings.Contains(out.String(), "restarting") || !strings.Contains(out.String(), "line=listening") {
		t.Errorf("log:\n%s", out)
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
