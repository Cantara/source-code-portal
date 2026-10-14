package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type addrs struct {
	mu sync.Mutex
	l  []string
	ch chan struct{}
}

func (a *addrs) listen(addr string) (net.Listener, error) {
	ln, err := listenTCP(addr)
	if err == nil {
		a.mu.Lock()
		a.l = append(a.l, ln.Addr().String())
		if len(a.l) == 2 {
			close(a.ch)
		}
		a.mu.Unlock()
	}
	return ln, err
}

var fastArgs = []string{"serve", "--set", "server.public.listen=127.0.0.1:0", "--set", "server.admin.listen=127.0.0.1:0", "--set", "shutdown.drain_delay=50ms"}

// run serves over real sockets: /livez on the public listener, /metrics on the admin listener, a clean stop on cancel.
func TestServeAndStop(t *testing.T) {
	a := &addrs{ch: make(chan struct{})}
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, Env{Args: fastArgs, Stdout: io.Discard, Stderr: &stderr, Listen: a.listen, Wire: productWire, Seed: 7})
	}()
	select {
	case <-a.ch:
	case code := <-done:
		t.Fatalf("serve exited %d: %s", code, stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatal("listeners did not open")
	}
	// Order of Listen calls: public first, admin second (the log line says which is which).
	pub, adm := a.l[0], a.l[1]
	get := func(url string) (int, string) {
		resp, err := getURL(nil, url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if c, b := get("http://" + pub + "/livez"); c == 200 && b == "ok" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("/livez never answered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if c, b := get("http://" + adm + "/metrics"); c != 200 || !strings.Contains(b, "scp_build_info") {
		t.Errorf("/metrics = %d %.200s", c, b)
	}
	if c, _ := get("http://" + pub + "/metrics"); c == 200 {
		t.Error("/metrics must not be served on the public listener")
	}
	cancel()
	select {
	case code := <-done:
		if code != exitOK {
			t.Fatalf("exit %d after a signal; stderr:\n%s", code, stderr.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop")
	}
	for _, want := range []string{`"step":"telemetry"`, `"step":"wire"`, `"msg":"listening"`, `"msg":"stage started"`, `"msg":"shutdown complete"`} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("log lacks %s:\n%s", want, stderr.String())
		}
	}
	if resp, err := getURL(nil, "http://"+pub+"/livez"); err == nil {
		_ = resp.Body.Close()
		t.Error("the public listener must be closed after the stop")
	}
}

func TestServeRefusals(t *testing.T) {
	run1 := func(mod func(*Env), args ...string) (int, string) {
		var stderr bytes.Buffer
		env := Env{Args: args, Stdout: io.Discard, Stderr: &stderr, Listen: func(a string) (net.Listener, error) { return listenTCP(a) }, Wire: productWire}
		if mod != nil {
			mod(&env)
		}
		return run(t.Context(), env), stderr.String()
	}
	// Invalid configuration: every violation printed, exit 2, nothing started.
	code, out := run1(nil, "serve", "--set", "log.level=loud", "--set", "server.timeouts.read=1ms")
	if code != exitUsage || !strings.Contains(out, "log.level") || !strings.Contains(out, "server.timeouts.read") {
		t.Errorf("invalid config: %d\n%s", code, out)
	}
	// A secret on the command line is refused (it would show in ps).
	if code, out := run1(nil, "serve", "--set", "server.admin.bearer_token=hunter22hunter22"); code != exitUsage || strings.Contains(out, "hunter22") {
		t.Errorf("secret via --set: %d\n%s", code, out)
	}
	// A bind failure is a startup error (1), not a stage failure.
	busy, _ := listenTCP("127.0.0.1:0")
	defer busy.Close()
	code, out = run1(nil, "serve", "--set", "server.public.listen="+busy.Addr().String(), "--set", "server.admin.listen=127.0.0.1:0")
	if code != exitFail || !strings.Contains(out, "public listener") {
		t.Errorf("bind failure: %d\n%s", code, out)
	}
	// A wiring failure (store cannot open, credentials fail on a cold start ...) is 1 and closes telemetry.
	code, out = run1(func(e *Env) {
		e.Wire = func(context.Context, WireIn) (Wired, error) {
			return Wired{}, errors.New("store: version guard refused the file")
		}
	}, fastArgs...)
	if code != exitFail || !strings.Contains(out, "version guard") {
		t.Errorf("wire failure: %d\n%s", code, out)
	}
	if code, _ := run1(func(e *Env) { e.Wire = nil }, fastArgs...); code != exitFail {
		t.Errorf("no wiring: %d", code)
	}
	if code, _ := run1(nil, "serve", "extra"); code != exitUsage {
		t.Errorf("positional: %d", code)
	}
}

// SIGHUP is a reload request, not a termination: the Go runtime's default action for it is to exit at once, skipping the shutdown plan
// (found by verification of design 16: `kill -HUP` killed the skeleton with no "shutdown complete"). main.go turns it into a value
// on Env.Reload; serve answers it with Wired.Reload inside the housekeeping stage and keeps serving.
func TestServeAnswersReloadRequestsAndKeepsServing(t *testing.T) {
	a := &addrs{ch: make(chan struct{})}
	var stderr safeBuffer
	hup := make(chan struct{}, 1)
	reloads := make(chan struct{}, 4)
	wire := func(ctx context.Context, in WireIn) (Wired, error) {
		w, err := productWire(ctx, in)
		w.Reload = func(context.Context) error { reloads <- struct{}{}; return errors.New("key file unreadable") } // a failing reload must not stop the process
		return w, err
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, Env{Args: fastArgs, Stdout: io.Discard, Stderr: &stderr, Listen: a.listen, Wire: wire, Seed: 7, Reload: hup})
	}()
	select {
	case <-a.ch:
	case code := <-done:
		t.Fatalf("serve exited %d: %s", code, stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatal("listeners did not open")
	}
	hup <- struct{}{}
	select {
	case <-reloads:
	case <-time.After(10 * time.Second):
		t.Fatal("Wired.Reload was not called after a reload request")
	}
	hup <- struct{}{} // a second one: the loop is still there after an error
	select {
	case <-reloads:
	case <-time.After(10 * time.Second):
		t.Fatal("the reload loop ended after a failing reload")
	}
	resp, err := getURL(nil, "http://"+a.l[0]+"/livez")
	if err != nil {
		t.Fatalf("not serving after a reload request: %v", err)
	}
	_ = resp.Body.Close()
	cancel()
	if code := <-done; code != exitOK {
		t.Fatalf("exit %d after the stop:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), `"msg":"reload"`) || !strings.Contains(stderr.String(), "key file unreadable") {
		t.Errorf("the reload (and its error) is not logged:\n%s", stderr.String())
	}
}

// An early exit after the wire (a bind failure) releases what the wire built: Wired.Abort runs, telemetry is shut down.
func TestServeFailureAfterWireCallsAbort(t *testing.T) {
	aborted := false
	busy, err := listenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	var stderr bytes.Buffer
	env := Env{
		Args:   []string{"serve", "--set", "server.public.listen=" + busy.Addr().String(), "--set", "server.admin.listen=127.0.0.1:0"},
		Stdout: io.Discard, Stderr: &stderr, Listen: listenTCP,
		Wire: func(ctx context.Context, in WireIn) (Wired, error) {
			w, err := productWire(ctx, in)
			w.Abort = func(context.Context) error { aborted = true; return nil }
			return w, err
		},
	}
	if code := run(t.Context(), env); code != exitFail {
		t.Fatalf("bind failure: exit %d\n%s", code, stderr.String())
	}
	if !aborted {
		t.Error("Wired.Abort was not called: the store opened by the wire would stay open")
	}
}

// safeBuffer is a bytes.Buffer that the log handler and the test can use at the same time.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *safeBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }
