package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// stream reads server-sent events.
type stream struct {
	resp *http.Response
	r    *bufio.Reader
}

func openStream(t *testing.T, url string) *stream {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultTransport.RoundTrip(req) // no transparent decompression: a compressed stream would show
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return &stream{resp: resp, r: bufio.NewReader(resp.Body)}
}

// next returns the next event or comment block, lines joined by "|".
func (s *stream) next(t *testing.T) string {
	t.Helper()
	var lines []string
	for {
		l, err := s.r.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended: %v (read %q)", err, lines)
		}
		if l = strings.TrimSuffix(l, "\n"); l == "" {
			return strings.Join(lines, "|")
		}
		lines = append(lines, l)
	}
}

func version(t *testing.T, ev string) int64 {
	t.Helper()
	data, ok := strings.CutPrefix(ev, "event: snapshot|data: ")
	if !ok {
		t.Fatalf("not a snapshot event: %q", ev)
	}
	var info oapi.SnapshotInfo
	if err := json.Unmarshal([]byte(data), &info); err != nil {
		t.Fatal(err)
	}
	return info.Version
}

func TestEvents(t *testing.T) {
	pub := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	defer pub.Close()
	current := func(context.Context) *snapshot.Snapshot { return pub.Current() }
	ev := NewEvents(current, pub.Published)
	ev.beat, ev.max = 100*time.Millisecond, 2
	deadline := make(chan bool, 1)
	h := Public(testOptions(), page, nil,
		stub{SnapshotAPI: SnapshotAPI{Current: current}}, ev.Route(),
		Route{Pattern: "GET /api/v1/deadline", Stream: true, Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})},
		Route{Pattern: "GET /deadline", Stream: true, Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			_, ok := r.Context().Deadline()
			deadline <- ok
		})})
	srv := httptest.NewServer(h)
	srv.Config.RegisterOnShutdown(ev.Close)
	defer srv.Close()

	s := openStream(t, srv.URL+"/api/v1/events")
	if ct, ce := s.resp.Header.Get("Content-Type"), s.resp.Header.Get("Content-Encoding"); s.resp.StatusCode != http.StatusOK ||
		ct != "text/event-stream" || ce != "" || s.resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d, type %q, encoding %q", s.resp.StatusCode, ct, ce)
	}
	if got := s.next(t); got != "retry: 5000" {
		t.Fatalf("first block %q", got)
	}
	if v := version(t, s.next(t)); v != 0 {
		t.Fatalf("version at connect %d, want 0", v)
	}

	pub.Update(func(b *snapshot.Builder) { b.Mark("a", 42) })
	pub.Flush()
	got := s.next(t)
	for got == ": ping" {
		got = s.next(t)
	}
	if v := version(t, got); v != 42 {
		t.Errorf("version after publish %d, want 42", v)
	}
	pub.Update(func(*snapshot.Builder) {}) // the same version: no event
	pub.Flush()
	if got := s.next(t); got != ": ping" {
		t.Errorf("after a publish with the same version: %q, want the heartbeat", got)
	}

	second := openStream(t, srv.URL+"/api/v1/events")
	_ = second.next(t)
	if third := openStream(t, srv.URL+"/api/v1/events"); third.resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("a stream beyond the cap: %d, want 503", third.resp.StatusCode)
	}
	if ev.Streams() != 2 {
		t.Errorf("streams %d, want 2", ev.Streams())
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(srv.URL + "/deadline")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if <-deadline {
		t.Error("a stream route has a request deadline")
	}
	resp, err = client.Get(srv.URL + "/api/v1/deadline") // no 406 although JSON is not accepted, no buffering
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("stream path under /api/: %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Config.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown with open streams: %v", err)
	}
	if _, err := io.ReadAll(s.r); err != nil {
		t.Errorf("stream after shutdown: %v", err)
	}
}
