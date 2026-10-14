package ops_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/ops"
	"github.com/cantara/source-code-portal/internal/ops/opsapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

var t0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// world is a publisher with `n` repositories, each with a commits Info.
type world struct {
	pub  *readmodel.Publisher
	repo readmodel.Slot[int]
	com  readmodel.Slot[freshness.Info]
}

func newWorld(t *testing.T) *world {
	t.Helper()
	reg := &readmodel.Registry{}
	w := &world{}
	w.repo = readmodel.Register[int](reg, "catalog.repo", readmodel.Existence())
	w.com = readmodel.Register[freshness.Info](reg, "commits.view")
	w.pub = readmodel.NewPublisher(reg, nil, nil, readmodel.Deps{})
	return w
}

func (w *world) put(t *testing.T, name string, in *freshness.Info) {
	t.Helper()
	_, err := w.pub.Apply(func(b *readmodel.Builder) error {
		if err := readmodel.Put(b, readmodel.EntryID(name), w.repo, 1); err != nil {
			return err
		}
		if in != nil {
			return readmodel.Put(b, readmodel.EntryID(name), w.com, *in)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (w *world) kind() ops.Kind {
	return ops.Kind{Family: "commits", Job: "commits.refresh", Upstream: "github", TTL: 2 * time.Hour,
		Infos: func(s *readmodel.Snapshot) iter.Seq[freshness.Info] {
			return func(yield func(freshness.Info) bool) {
				for _, id := range s.Repos() {
					in, _ := readmodel.Get(s, id, w.com) // absent: zero Info, State "" == never
					if !yield(in) {
						return
					}
				}
			}
		}}
}

type fake struct {
	store ops.StoreView
	eng   ops.EngineView
	cat   ops.CatalogueView
	retry func(string) error
	pub   *readmodel.Publisher
}

func (f *fake) Current() *readmodel.Snapshot    { return f.pub.Current() }
func (f *fake) Status() readmodel.PublishStatus { return f.pub.Status() }
func (f *fake) StoreView() ops.StoreView        { return f.store }
func (f *fake) EngineView() ops.EngineView      { return f.eng }
func (f *fake) Catalogue() ops.CatalogueView    { return f.cat }
func (f *fake) Retry(k string) error            { return f.retry(k) }

func okInfo(at time.Time) *freshness.Info {
	return &freshness.Info{State: freshness.StateOK, FetchedAt: at, CheckedAt: at, TTL: 2 * time.Hour}
}

func newReadiness(t *testing.T, w *world, f *fake, cfg ops.ReadinessConfig, started time.Time) *ops.Readiness {
	t.Helper()
	r, err := ops.NewReadiness(cfg, f, f, []ops.Kind{w.kind()}, started)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReadinessStateMachine(t *testing.T) {
	w := newWorld(t)
	for i := range 10 {
		w.put(t, fmt.Sprintf("r%d", i), nil) // 10 repos, none fetched
	}
	f := &fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true}}
	r := newReadiness(t, w, f, ops.DefaultReadiness(), t0)
	at := func(d time.Duration) time.Time { return t0.Add(d) }

	if got := r.Evaluate(at(time.Minute)).Phase; got != ops.PhaseWarming {
		t.Fatalf("cold start, no catalogue: %v", got)
	}
	// even after the deadline a cold start without a catalogue never becomes ready
	if got := r.Evaluate(at(time.Hour)).Phase; got != ops.PhaseWarming {
		t.Fatalf("no catalogue after the deadline: %v", got)
	}
	f.cat = ops.CatalogueView{StoreOpened: true, Live: true, Repos: 10}
	if got := r.Evaluate(at(2 * time.Hour)).Phase; got != ops.PhaseDegraded {
		t.Fatalf("catalogue present, deadline passed, coverage 0: %v", got)
	}
	for i := range 9 { // 9 of 10 = 0.9 >= threshold
		w.put(t, fmt.Sprintf("r%d", i), okInfo(at(2*time.Hour)))
	}
	rep := r.Evaluate(at(2*time.Hour + time.Second))
	if rep.Phase != ops.PhaseReady || rep.ReadySince.IsZero() {
		t.Fatalf("degraded must become ready when coverage arrives: %+v", rep)
	}
	// never regresses: the catalogue gate and coverage collapse, readiness stays
	f.cat = ops.CatalogueView{StoreOpened: false}
	w.put(t, "r0", &freshness.Info{State: freshness.StateError, Attempts: 3})
	w.put(t, "r1", &freshness.Info{State: freshness.StateError, Attempts: 3})
	if got := r.Evaluate(at(3 * time.Hour)).Phase; got != ops.PhaseReady {
		t.Fatalf("regressed to %v", got)
	}
	r.SetShuttingDown()
	if got := r.Evaluate(at(3 * time.Hour)).Phase; got != ops.PhaseShuttingDown {
		t.Fatalf("shutdown: %v", got)
	}
	if got := r.DataPhase(); got != "ready" {
		t.Fatalf("DataPhase during shutdown must keep describing data: %q", got)
	}
}

func TestReadinessWarmCatalogueAge(t *testing.T) {
	w := newWorld(t)
	w.put(t, "a", okInfo(t0))
	f := &fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true, Warm: true, WarmFetched: t0.Add(-100 * time.Hour)}}
	r := newReadiness(t, w, f, ops.DefaultReadiness(), t0)
	rep := r.Evaluate(t0)
	if rep.Phase != ops.PhaseWarming || !strings.Contains(rep.Gates[1].Detail, "too old") {
		t.Fatalf("a warm catalogue older than warm_max_age must not open the gate: %+v", rep)
	}
	f.cat.WarmFetched = t0.Add(-71 * time.Hour)
	if got := r.Evaluate(t0).Phase; got != ops.PhaseReady {
		t.Fatalf("young warm catalogue + full coverage: %v", got)
	}
}

func TestReadinessEmptyCatalogueIsCovered(t *testing.T) {
	w := newWorld(t)
	f := &fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true, Live: true}}
	if got := newReadiness(t, w, f, ops.DefaultReadiness(), t0).Evaluate(t0).Phase; got != ops.PhaseReady {
		t.Fatalf("0 repositories, live reconcile: %v", got)
	}
}

func TestNewReadinessRejectsUnknownCoreKind(t *testing.T) {
	w := newWorld(t)
	f := &fake{pub: w.pub}
	cfg := ops.DefaultReadiness()
	cfg.CoreKinds = []string{"docs"}
	if _, err := ops.NewReadiness(cfg, f, f, []ops.Kind{w.kind()}, t0); err == nil || !strings.Contains(err.Error(), `"docs"`) {
		t.Fatalf("got %v", err)
	}
}

func TestReadyzBodies(t *testing.T) {
	w := newWorld(t)
	w.put(t, "a", nil)
	f := &fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true}}
	r := newReadiness(t, w, f, ops.DefaultReadiness(), time.Now())
	do := func(detail bool) (int, string) {
		rec := httptest.NewRecorder()
		r.ReadyHandler(detail).ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
		return rec.Code, rec.Body.String()
	}
	code, pub := do(false)
	if code != 503 || strings.Contains(pub, "0.00") || !strings.Contains(pub, "#/gates/catalogue") || !strings.Contains(pub, `"code":"not-ready"`) {
		t.Fatalf("public: %d %s", code, pub)
	}
	_, adm := do(true)
	if !strings.Contains(adm, "no catalogue yet") {
		t.Fatalf("admin form must carry details: %s", adm)
	}
	f.cat.Live = true
	w.put(t, "a", okInfo(time.Now()))
	if code, body := do(false); code != 200 || !strings.Contains(body, `"ready"`) {
		t.Fatalf("%d %s", code, body)
	}
}

func TestLiveness(t *testing.T) {
	now := time.Now()
	cfg := ops.DefaultConfig().Liveness
	cases := []struct {
		name    string
		ev      ops.EngineView
		started time.Time
		want    int
	}{
		{"fresh heartbeat", ops.EngineView{Heartbeat: now.Add(-5 * time.Second)}, now.Add(-time.Hour), 200},
		{"stale heartbeat", ops.EngineView{Heartbeat: now.Add(-31 * time.Second)}, now.Add(-time.Hour), 503},
		{"no heartbeat inside the startup grace", ops.EngineView{}, now.Add(-10 * time.Second), 200},
		{"no heartbeat after the grace", ops.EngineView{}, now.Add(-time.Minute), 503},
		{"paused upstream is not a liveness failure", ops.EngineView{Heartbeat: now, Upstreams: []ops.UpstreamView{{Name: "github", PausedUntil: now.Add(time.Hour), Pending: 900}}}, now.Add(-time.Hour), 200},
		{"all slots stuck 1m", ops.EngineView{Heartbeat: now, Upstreams: []ops.UpstreamView{{Name: "github", AllStuckSince: now.Add(-time.Minute)}}}, now.Add(-time.Hour), 200},
		{"all slots stuck 3m", ops.EngineView{Heartbeat: now, Upstreams: []ops.UpstreamView{{Name: "github", AllStuckSince: now.Add(-3 * time.Minute)}}}, now.Add(-time.Hour), 503},
	}
	for _, c := range cases {
		f := &fake{eng: c.ev}
		rec := httptest.NewRecorder()
		ops.LiveHandler(cfg, f, c.started).ServeHTTP(rec, httptest.NewRequest("GET", "/livez", nil))
		if rec.Code != c.want {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
}

func TestRetryHandler(t *testing.T) {
	var got string
	var rerr error
	var counts []string
	h := ops.RetryHandler(retrierFn(func(k string) error { got = k; return rerr }), nil2log(), func(r string) { counts = append(counts, r) })
	do := func(ct, csrf, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/admin/jobs/retry", strings.NewReader(body))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if csrf != "" {
			req.Header.Set("X-SCP-Request", csrf)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	const j = "application/json"
	cases := []struct {
		name         string
		ct, csrf, in string
		rerr         error
		want         int
		wantCode     string
	}{
		{"accepted", j, "1", `{"key":"docs.refresh/acme/acme/billing"}`, nil, 202, ""},
		{"global key", j, "1", `{"key":"catalog.reconcile/_"}`, nil, 202, ""},
		{"charset param is fine", j + "; charset=utf-8", "1", `{"key":"a.b/c"}`, nil, 202, ""},
		{"wrong media type", "text/plain", "1", `{"key":"a.b/c"}`, nil, 415, "unsupported-media-type"},
		{"no csrf header", j, "", `{"key":"a.b/c"}`, nil, 403, "forbidden"},
		{"csrf header wrong", j, "0", `{"key":"a.b/c"}`, nil, 403, "forbidden"},
		{"unknown field", j, "1", `{"key":"a.b/c","force":true}`, nil, 400, "validation-failed"},
		{"trailing data", j, "1", `{"key":"a.b/c"}{}`, nil, 400, "validation-failed"},
		{"not json", j, "1", `key=a`, nil, 400, "validation-failed"},
		{"bad key: no slash", j, "1", `{"key":"docs.refresh"}`, nil, 400, "validation-failed"},
		{"bad key: whitespace", j, "1", `{"key":"docs.refresh/a b"}`, nil, 400, "validation-failed"},
		{"bad key: control char", j, "1", `{"key":"docs.refresh/a\u0000b"}`, nil, 400, "validation-failed"},
		{"bad key: upper case kind", j, "1", `{"key":"Docs/a"}`, nil, 400, "validation-failed"},
		{"too large", j, "1", `{"key":"` + strings.Repeat("a", 5000) + `"}`, nil, 413, "payload-too-large"},
		{"unknown kind", j, "1", `{"key":"nope.kind/x"}`, ops.ErrUnknownKey, 404, "not-found"},
		{"queue full", j, "1", `{"key":"a.b/c"}`, ops.ErrQueueFull, 503, "queue-full"},
		{"closed", j, "1", `{"key":"a.b/c"}`, ops.ErrClosed, 503, "not-ready"},
		{"other error", j, "1", `{"key":"a.b/c"}`, errors.New("boom"), 500, "internal"},
	}
	for _, c := range cases {
		rerr, got = c.rerr, ""
		rec := do(c.ct, c.csrf, c.in)
		if rec.Code != c.want || (c.wantCode != "" && !strings.Contains(rec.Body.String(), `"code":"`+c.wantCode+`"`)) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
		if c.want == 202 && !strings.Contains(rec.Body.String(), `"accepted":true`) {
			t.Errorf("%s: body %s", c.name, rec.Body)
		}
		if c.want != 202 && c.rerr == nil && got != "" {
			t.Errorf("%s: the retrier was called with %q although the request is invalid", c.name, got)
		}
	}
	if len(counts) != len(cases) {
		t.Errorf("every request must be counted exactly once: %d of %d", len(counts), len(cases))
	}
}

func TestWarningsAreRankedAndDeterministic(t *testing.T) {
	w := newWorld(t)
	w.put(t, "a", okInfo(t0.Add(-10*time.Hour))) // 10 h old against a 2 h TTL: stale
	now := t0
	f := &fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true, Live: true},
		store: ops.StoreView{OK: false, Reason: "write: disk full", SizeBytes: -1},
		eng: ops.EngineView{Heartbeat: now, ParkedTotal: 2, Parked: []ops.ParkedView{{Key: "k/a", Since: now.Add(-time.Hour), Reason: "perm"}},
			Upstreams: []ops.UpstreamView{{Name: "github", Concurrency: 4, Stuck: 4, AllStuckSince: now.Add(-time.Minute)}}}}
	c := newCollector(t, w, f)
	doc := c.Status.Status(now)
	var codes []string
	for _, x := range doc.Warnings {
		codes = append(codes, x.Severity+":"+x.Code)
	}
	want := "critical:upstream.stuck,critical:store.degraded,warning:jobs.stale,warning:jobs.parked" // severity, then oldest Since first (nil Since last), then code
	if strings.Join(codes, ",") != want {
		t.Fatalf("got %v\nwant %s", codes, want)
	}
	again := c.Status.Status(now)
	a, _ := json.Marshal(doc)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Fatal("two assemblies of the same state differ")
	}
}

func newCollector(t *testing.T, w *world, f *fake) *ops.Service {
	t.Helper()
	svc, err := ops.New(ops.DefaultConfig(), ops.DefaultReadiness(), ops.Deps{
		Build: ops.BuildInfo{Version: "1.0.0", Commit: "abc", BuildID: "abc", GoVersion: "go1.27.1"}, Started: t0.Add(-time.Hour),
		Sources: ops.Sources{Snapshot: f, Store: f, Engine: f, Catalogue: f}, Kinds: []ops.Kind{w.kind()}, Retrier: f})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestStatusHandlerETagAndShape(t *testing.T) {
	w := newWorld(t)
	w.put(t, "a", okInfo(time.Now()))
	f := &fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true, Live: true}, store: ops.StoreView{OK: true, SizeBytes: 4096}, eng: ops.EngineView{Heartbeat: time.Now()}}
	svc := newCollector(t, w, f)
	rec := httptest.NewRecorder()
	svc.Status.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/status", nil))
	var doc opsapi.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || rec.Code != 200 {
		t.Fatalf("%d %v", rec.Code, err)
	}
	if doc.Readiness.State != "ready" || doc.Snapshot.RepoCount != 1 || doc.Store.State != "ok" || *doc.Store.SizeBytes != 4096 || len(doc.Jobs.Kinds) != 1 {
		t.Fatalf("%+v", doc)
	}
	if !strings.Contains(rec.Body.String(), `"warnings":[]`) || !strings.Contains(rec.Body.String(), `"installations":[]`) {
		t.Fatalf("empty collections must be [], never null: %s", rec.Body)
	}
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	rec2 := httptest.NewRecorder()
	svc.Status.Handler().ServeHTTP(rec2, req)
	if rec2.Code != 304 && rec2.Code != 200 { // 304 unless the second boundary was crossed between the two calls
		t.Fatal(rec2.Code)
	}
}

func TestPprofHandlers(t *testing.T) {
	cfg := ops.DefaultConfig().Pprof
	hs := ops.PprofHandlers(cfg)
	get := func(h http.Handler, target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
		return rec
	}
	if r := get(hs[ops.RoutePprofProfile], "/debug/pprof/profile?seconds=61"); r.Code != 400 || !strings.Contains(r.Body.String(), "#/query/seconds") {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if r := get(hs[ops.RoutePprofTrace], "/debug/pprof/trace?seconds=abc"); r.Code != 400 {
		t.Fatalf("%d", r.Code)
	}
	if r := get(hs[ops.RoutePprofIndex], "/debug/pprof/goroutineleak?debug=1"); r.Code != 200 {
		t.Fatalf("goroutineleak profile: %d %s", r.Code, r.Body)
	}
	if r := get(hs[ops.RoutePprofIndex], "/debug/pprof/heap?debug=1"); r.Code != 200 || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("heap: %d %v", r.Code, r.Header())
	}
	cfg.Enabled = false
	for name, h := range ops.PprofHandlers(cfg) {
		if r := get(h, "/debug/pprof/"); r.Code != 404 || !strings.Contains(r.Body.String(), "feature-disabled") {
			t.Errorf("%s disabled: %d", name, r.Code)
		}
	}
}

func TestMetricsExposition(t *testing.T) {
	reg := prometheus.NewRegistry()
	exp, err := otelprom.New(otelprom.WithRegisterer(reg), otelprom.WithoutScopeInfo(), otelprom.WithoutTargetInfo())
	if err != nil {
		t.Fatal(err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exp))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	w := newWorld(t)
	w.put(t, "a", okInfo(time.Now().Add(-time.Hour)))
	f := &fake{pub: w.pub, cat: ops.CatalogueView{StoreOpened: true, Live: true}, store: ops.StoreView{OK: true}, eng: ops.EngineView{Heartbeat: time.Now()}}
	svc, err := ops.New(ops.DefaultConfig(), ops.DefaultReadiness(), ops.Deps{Meter: mp.Meter("scp"), Build: ops.BuildInfo{Version: "1", Commit: "c", GoVersion: "go"}, Started: time.Now().Add(-time.Hour),
		Sources: ops.Sources{Snapshot: f, Store: f, Engine: f, Catalogue: f}, Kinds: []ops.Kind{w.kind()}, Retrier: f})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc
	rec := httptest.NewRecorder()
	promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)
	for _, name := range []string{ops.MetricStartTime, ops.MetricReadinessState, ops.MetricReadinessGate, ops.MetricReadySince, ops.MetricCoverageRatio,
		ops.MetricItemAge, ops.MetricItems, ops.MetricHeartbeatAge, ops.MetricStatusWarnings} {
		if !strings.Contains(string(body), "# TYPE "+name+" gauge") {
			t.Errorf("metric %s missing or renamed by the exporter:\n%s", name, body)
		}
	}
	if !strings.Contains(string(body), `scp_item_age_seconds{kind="commits",stat="max"}`) || !strings.Contains(string(body), `scp_readiness_state{phase="ready"} 1`) {
		t.Errorf("labels or values wrong:\n%s", body)
	}
}

type retrierFn func(string) error

func (f retrierFn) Retry(k string) error { return f(k) }
