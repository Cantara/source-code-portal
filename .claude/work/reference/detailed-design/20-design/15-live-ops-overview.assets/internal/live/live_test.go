package live_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/live"
	"github.com/cantara/source-code-portal/internal/readmodel"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// fakeFeed is the test double of the v1.1 *readmodel.Publisher: it wraps the real publisher (so versions move for real)
// and adds the ring and the wake channels that arrive with SSE.
type fakeFeed struct {
	mu    sync.Mutex
	ring  []live.ChangeSet // oldest first
	subs  map[int]chan struct{}
	next  int
	floor uint64 // versions <= floor are not in the ring
	pub   *readmodel.Publisher
	slot  readmodel.Slot[int]
	n     int
}

func newFakeFeed() *fakeFeed {
	reg := &readmodel.Registry{}
	slot := readmodel.Register[int](reg, "catalog.repo", readmodel.Existence())
	return &fakeFeed{subs: map[int]chan struct{}{}, pub: readmodel.NewPublisher(reg, nil, nil, readmodel.Deps{}), slot: slot}
}
func (f *fakeFeed) Current() *readmodel.Snapshot { return f.pub.Current() }
func (f *fakeFeed) Subscribe() (<-chan struct{}, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan struct{}, 1)
	id := f.next
	f.next++
	f.subs[id] = ch
	return ch, func() { f.mu.Lock(); delete(f.subs, id); f.mu.Unlock() }
}
func (f *fakeFeed) ChangesSince(v uint64) ([]live.ChangeSet, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v < f.floor {
		return nil, false
	}
	var out []live.ChangeSet
	for _, c := range f.ring {
		if c.Version > v {
			out = append(out, c)
		}
	}
	return out, true
}
func (f *fakeFeed) publish(slots ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	snap, err := f.pub.Apply(func(b *readmodel.Builder) error { return readmodel.Put(b, "a", f.slot, f.n) })
	if err != nil {
		panic(err)
	}
	f.ring = append(f.ring, live.ChangeSet{Version: snap.Version(), Repos: []readmodel.EntryID{"a"}, Slots: slots})
	for _, ch := range f.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// forget drops ring entries up to and including version v (the 256-entry ring wrapped).
func (f *fakeFeed) forget(v uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.floor = v
}

type phase string

func (p phase) DataPhase() string { return string(p) }

func newSvc(t *testing.T, f *fakeFeed, mut func(*live.Config), d func(*live.Deps)) (*live.Service, http.Handler) {
	t.Helper()
	cfg := live.DefaultConfig()
	cfg.SSE.Enabled = true
	if mut != nil {
		mut(&cfg)
	}
	deps := live.Deps{Feed: f, Changes: f, Readiness: phase("ready"), BuildID: "b1", Epoch: "e1", MaxPerClient: 4, ClientKey: func(r *http.Request) string { return r.Header.Get("X-Client") }}
	if d != nil {
		d(&deps)
	}
	s, err := live.New(cfg, deps)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Mount(muxAdapter{mux})
	return s, mux
}

type muxAdapter struct{ *http.ServeMux }

func (m muxAdapter) HandleFunc(p string, h func(http.ResponseWriter, *http.Request)) {
	m.ServeMux.HandleFunc(p, h)
}

var _ httpapi.Mux = muxAdapter{}

// events reads SSE blocks (separated by a blank line) from body into a channel.
func events(body io.Reader) <-chan string {
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(body)
		var blk []string
		for sc.Scan() {
			if sc.Text() == "" {
				ch <- strings.Join(blk, "|")
				blk = nil
				continue
			}
			blk = append(blk, sc.Text())
		}
	}()
	return ch
}

func get(t *testing.T, c *http.Client, url string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestSnapshotETagIncludesPhase(t *testing.T) {
	f := newFakeFeed()
	_, h := newSvc(t, f, nil, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/snapshot", nil))
	if rec.Code != 200 || rec.Header().Get("ETag") != `W/"v0-ready"` || !strings.Contains(rec.Body.String(), `"readiness":"ready"`) {
		t.Fatalf("%d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	rec2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/snapshot", nil)
	req.Header.Set("If-None-Match", `W/"v0-ready"`)
	h.ServeHTTP(rec2, req)
	if rec2.Code != 304 {
		t.Fatalf("want 304, got %d", rec2.Code)
	}
	// a readiness flip without a publish must change the validator
	_, h2 := newSvc(t, f, nil, func(d *live.Deps) { d.Readiness = phase("degraded") })
	rec3 := httptest.NewRecorder()
	h2.ServeHTTP(rec3, req)
	if rec3.Code != 200 || rec3.Header().Get("ETag") != `W/"v0-degraded"` {
		t.Fatalf("%d %v", rec3.Code, rec3.Header())
	}
}

func TestEventsDisabled(t *testing.T) {
	_, h := newSvc(t, newFakeFeed(), func(c *live.Config) { c.SSE.Enabled = false }, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/events", nil))
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "feature-disabled") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestStreamHelloInvalidateHeartbeatDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeFeed()
		s, h := newSvc(t, f, nil, func(d *live.Deps) { d.StatusTopic = true })
		srv := httptest.NewTestServer(t, h)
		resp := get(t, srv.Client(), srv.URL+"/api/v1/events", map[string]string{"X-Client": "1.1.1.1"})
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("%d %v", resp.StatusCode, resp.Header)
		}
		ev := events(resp.Body)
		synctest.Wait()
		if got := <-ev; got != "retry: 3000" {
			t.Fatalf("first block %q", got)
		}
		if got := <-ev; !strings.Contains(got, "event: hello") || !strings.Contains(got, `"epoch":"e1"`) {
			t.Fatalf("hello %q", got)
		}
		// three publishes inside the 300 ms window coalesce into one event
		f.publish("commits.view")
		time.Sleep(100 * time.Millisecond)
		f.publish("commits.view", "docs.repo")
		time.Sleep(100 * time.Millisecond)
		f.publish("pages.set")
		synctest.Wait()
		select {
		case b := <-ev:
			t.Fatalf("event before the debounce elapsed: %q", b)
		default:
		}
		time.Sleep(300 * time.Millisecond)
		synctest.Wait()
		if got := <-ev; got != `id: 3|event: invalidate|data: {"topics":["site","commits","docs","pages","status"],"generation":3}` {
			t.Fatalf("invalidate %q", got)
		}
		time.Sleep(15 * time.Second)
		synctest.Wait()
		if got := <-ev; got != ": hb" {
			t.Fatalf("heartbeat %q", got)
		}
		if err := s.Drain(context.Background()); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if got := <-ev; got != "retry: 30000" {
			t.Fatalf("drain %q", got)
		}
		if _, open := <-ev; open {
			t.Fatal("stream still open after drain")
		}
		resp.Body.Close()
		// a stream opened during drain gets 200 + retry and closes (never a non-200)
		resp2 := get(t, srv.Client(), srv.URL+"/api/v1/events", map[string]string{"X-Client": "2.2.2.2"})
		b, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		if resp2.StatusCode != 200 || string(b) != "retry: 30000\n\n" {
			t.Fatalf("%d %q", resp2.StatusCode, b)
		}
		srv.Close()
	})
}

func TestPerClientLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeFeed()
		s, h := newSvc(t, f, nil, func(d *live.Deps) { d.MaxPerClient = 2 })
		srv := httptest.NewTestServer(t, h)
		var bodies []io.Closer
		for range 2 {
			r := get(t, srv.Client(), srv.URL+"/api/v1/events", map[string]string{"X-Client": "9.9.9.9"})
			if r.StatusCode != 200 {
				t.Fatal(r.StatusCode)
			}
			bodies = append(bodies, r.Body)
		}
		r3 := get(t, srv.Client(), srv.URL+"/api/v1/events", map[string]string{"X-Client": "9.9.9.9"})
		if r3.StatusCode != 429 || r3.Header.Get("Retry-After") != "30" {
			t.Fatalf("%d %v", r3.StatusCode, r3.Header)
		}
		r3.Body.Close()
		other := get(t, srv.Client(), srv.URL+"/api/v1/events", map[string]string{"X-Client": "8.8.8.8"})
		if other.StatusCode != 200 {
			t.Fatal(other.StatusCode)
		}
		bodies = append(bodies, other.Body)
		if err := s.Drain(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, b := range bodies {
			b.Close()
		}
		srv.Close()
	})
}

func TestTopics(t *testing.T) {
	cases := []struct {
		name   string
		cs     []live.ChangeSet
		status bool
		want   string
		resync bool
	}{
		{"commits only", []live.ChangeSet{{Slots: []string{"commits.view"}}}, false, "commits", false},
		{"two sets union in canonical order", []live.ChangeSet{{Slots: []string{"docs.repo"}}, {Globals: []string{"commits.latest", "site.meta"}}}, false, "site,commits,docs", false},
		{"pages feeds site", []live.ChangeSet{{Globals: []string{"pages.set"}}}, false, "site,pages", false},
		{"status topic on admin", []live.ChangeSet{{Slots: []string{"catalog.stats"}}}, true, "catalog,status", false},
		{"repos without slots: every content topic", []live.ChangeSet{{Repos: []readmodel.EntryID{"a"}}}, false, "site,catalog,commits,docs,manifests,pages", false},
		{"unknown name: every content topic", []live.ChangeSet{{Slots: []string{"newfeature.x"}}}, false, "site,catalog,commits,docs,manifests,pages", false},
		{"resync wins", []live.ChangeSet{{Slots: []string{"docs.repo"}}, {Resync: true}}, false, "", true},
		{"empty", nil, false, "", false},
	}
	for _, c := range cases {
		got, rs := live.Topics(c.cs, c.status)
		if strings.Join(got, ",") != c.want || rs != c.resync {
			t.Errorf("%s: got %v resync=%v want %q %v", c.name, got, rs, c.want, c.resync)
		}
	}
	// names registered by units 08 to 14 that the first draft did not know (verification: catalog.group_aliases, catalog.sync)
	if got, _ := live.Topics([]live.ChangeSet{{Globals: []string{"catalog.sync"}}}, false); len(got) != 0 {
		t.Errorf("catalog.sync (admin-class status) must not invalidate public content: %v", got)
	}
	if got, _ := live.Topics([]live.ChangeSet{{Globals: []string{"catalog.sync"}}}, true); strings.Join(got, ",") != "status" {
		t.Errorf("catalog.sync on the admin stream: %v", got)
	}
	if got, _ := live.Topics([]live.ChangeSet{{Globals: []string{"catalog.group_aliases"}}}, false); strings.Join(got, ",") != "catalog" {
		t.Errorf("catalog.group_aliases: %v", got)
	}
	for _, n := range []string{"catalog.repo", "catalog.stats", "catalog.membership", "catalog.aliases", "catalog.group_aliases", "catalog.sync", "commits.view", "commits.latest", "docs.repo", "manifests.repo", "manifests.graph", "integrations.status", "pages.set", "site.meta"} {
		if !live.KnownName(n) {
			t.Errorf("registry name %q has no topic mapping", n)
		}
	}
}

func TestReconnectCatchUpAndResync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeFeed()
		s, h := newSvc(t, f, nil, nil)
		srv := httptest.NewTestServer(t, h)
		f.publish("commits.view") // v1
		f.publish("docs.repo")    // v2
		f.publish("commits.view") // v3

		open := func(lastID string) <-chan string {
			r := get(t, srv.Client(), srv.URL+"/api/v1/events", map[string]string{"X-Client": "1.1.1.1", "Last-Event-ID": lastID})
			t.Cleanup(func() { r.Body.Close() })
			return events(r.Body)
		}
		// client saw v1: it missed docs and commits
		ev := open("1")
		synctest.Wait()
		<-ev // retry
		<-ev // hello (generation 3)
		if got := <-ev; got != `id: 3|event: invalidate|data: {"topics":["commits","docs"],"generation":3}` {
			t.Fatalf("catch-up %q", got)
		}
		// client is current: no catch-up event
		ev2 := open("3")
		synctest.Wait()
		<-ev2
		<-ev2
		select {
		case b := <-ev2:
			t.Fatalf("unexpected %q", b)
		default:
		}
		// ring wrapped past the client's id
		f.forget(2)
		ev3 := open("1")
		synctest.Wait()
		<-ev3
		<-ev3
		if got := <-ev3; got != `id: 3|event: resync|data: {"generation":3}` {
			t.Fatalf("resync %q", got)
		}
		// a client from a previous process lifetime (id above the current version)
		ev4 := open("99")
		synctest.Wait()
		<-ev4
		<-ev4
		if got := <-ev4; got != `id: 3|event: resync|data: {"generation":3}` {
			t.Fatalf("future id %q", got)
		}
		if err := s.Drain(context.Background()); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		srv.Close()
	})
}

// HEAD on the stream URL must answer the headers and return: a handler that streamed would hold a slot and the keep-alive
// connection for max_lifetime (verification finding V15-02).
func TestHeadOnEventsOpensNoStream(t *testing.T) {
	f := newFakeFeed()
	s, h := newSvc(t, f, nil, func(d *live.Deps) { d.MaxPerClient = 1; d.ClientKey = func(*http.Request) string { return "c" } })
	srv := httptest.NewServer(h)
	defer srv.Close()
	defer func() { _ = s.Drain(context.Background()) }() // runs before srv.Close: a failing assertion must not hang the test
	c := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest("HEAD", srv.URL+"/api/v1/events", nil)
	resp, err := c.Do(req)
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("%v %v", resp, err)
	}
	resp.Body.Close()
	// same keep-alive connection, next request must not block behind a still-running HEAD handler
	r2, err := c.Get(srv.URL + "/api/v1/snapshot")
	if err != nil {
		t.Fatalf("request after HEAD blocked: %v", err)
	}
	r2.Body.Close()
	// and the per-client slot is free: the single allowed stream still opens
	ctx, cancel := context.WithCancel(context.Background())
	rq, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/events", nil)
	r3, err := (&http.Client{}).Do(rq)
	if err != nil || r3.StatusCode != 200 {
		t.Fatalf("%v %v", r3, err)
	}
	cancel()
	r3.Body.Close()
	_ = s.Drain(context.Background())
}

// Without Deps.ClientKey the key is the host of RemoteAddr, not host:port (verification finding V15-01).
func TestDefaultClientKeyIsTheHostNotTheConnection(t *testing.T) {
	f := newFakeFeed()
	cfg := live.DefaultConfig()
	cfg.SSE.Enabled = true
	s, err := live.New(cfg, live.Deps{Feed: f, Changes: f, Readiness: phase("ready"), MaxPerClient: 1})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Mount(muxAdapter{mux})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer func() { _ = s.Drain(context.Background()) }()
	var codes []int
	var bodies []io.Closer
	for range 2 {
		cl := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}} // a new source port every time
		r, err := cl.Get(srv.URL + "/api/v1/events")
		if err != nil {
			t.Fatal(err)
		}
		codes = append(codes, r.StatusCode)
		bodies = append(bodies, r.Body)
	}
	if codes[0] != 200 || codes[1] != 429 {
		t.Fatalf("second stream from the same host must be refused: %v", codes)
	}
	_ = s.Drain(context.Background())
	for _, b := range bodies {
		b.Close()
	}
}

// Every live metric of design 15 section 9.2 appears under its documented name through the real exporter (the unit-`s`
// histogram keeps its _seconds suffix and the counters keep _total; verification finding V15-15: this was untested).
func TestMetricsExposition(t *testing.T) {
	reg := prometheus.NewRegistry()
	exp, err := otelprom.New(otelprom.WithRegisterer(reg), otelprom.WithoutScopeInfo(), otelprom.WithoutTargetInfo())
	if err != nil {
		t.Fatal(err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exp))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	f := newFakeFeed()
	s, h := newSvc(t, f, func(c *live.Config) { c.SSE.Enabled = false }, func(d *live.Deps) { d.Meter = mp.Meter("scp") })
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/snapshot", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/events", nil)) // counted "disabled"
	_ = s
	rec := httptest.NewRecorder()
	promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{"# TYPE " + live.MetricSSEClients + " gauge", "# TYPE " + live.MetricSnapshotPolls + " counter",
		`scp_snapshot_polls_total{result="ok"} 1`, `scp_sse_connections_total{result="disabled"} 1`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}
