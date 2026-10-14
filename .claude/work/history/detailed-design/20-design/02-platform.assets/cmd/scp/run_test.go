package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunServesAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	addrs := map[string]string{}
	var mu sync.Mutex
	ready := make(chan struct{})
	var logs bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"server.public.listen=127.0.0.1:0", "server.admin.listen=127.0.0.1:0", "shutdown.drain_delay=0s"},
			[]string{"SCP_LOG_LEVEL=debug", "SCP_SERVICE_PORT=tcp://10.0.0.1:80", "SCP_LOG_LEVL=debug"}, &logs,
			func(name string, a net.Addr) {
				mu.Lock()
				addrs[name] = a.String()
				if len(addrs) == 2 {
					close(ready)
				}
				mu.Unlock()
			})
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("run returned early: %v", err)
	}
	get := func(u string) (int, string, http.Header) {
		resp, err := http.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), resp.Header
	}
	var code int
	var body string
	var hdr http.Header
	for range 50 { // the servers start a moment after the listeners are bound
		func() {
			defer func() { recover() }()
			code, body, hdr = get("http://" + addrs["public"] + "/livez")
		}()
		if code == 200 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if code != 200 || body != "ok" || hdr.Get("X-Request-Id") == "" || hdr.Get("Content-Security-Policy") == "" {
		t.Fatalf("%d %q %v", code, body, hdr)
	}
	if _, m, _ := get("http://" + addrs["admin"] + "/metrics"); !strings.Contains(m, "scp_build_info") || !strings.Contains(m, "http_server_request_duration_seconds") {
		t.Fatalf("metrics:\n%s", m)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown hung")
	}
	if !strings.Contains(logs.String(), "SCP_LOG_LEVL") || strings.Contains(logs.String(), "SCP_SERVICE_PORT") {
		t.Fatalf("unknown-env warning rules:\n%s", logs.String())
	}
}
