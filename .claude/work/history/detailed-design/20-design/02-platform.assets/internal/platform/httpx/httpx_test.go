package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/cantara/source-code-portal/internal/platform/logx"
	"github.com/cantara/source-code-portal/internal/platform/secret"
)

func mustTrusted(t *testing.T, c ...string) *ClientIPResolver {
	t.Helper()
	p, err := ParseTrusted(c)
	if err != nil {
		t.Fatal(err)
	}
	return NewClientIPResolver(p)
}

func TestParseTrustedRefusesAll(t *testing.T) {
	for _, s := range []string{"0.0.0.0/0", "::/0", "nonsense"} {
		if _, err := ParseTrusted([]string{s}); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
	if p, err := ParseTrusted([]string{"10.0.0.1"}); err != nil || p[0].Bits() != 32 {
		t.Fatalf("bare address: %v %v", p, err)
	}
}

func TestClientIP(t *testing.T) {
	res := mustTrusted(t, "10.0.0.0/8", "192.168.1.5")
	cases := []struct {
		name, remote string
		xff          []string
		want         string
		via          bool
	}{
		{"direct", "203.0.113.9:1234", nil, "203.0.113.9", false},
		{"untrusted peer spoofs xff", "203.0.113.9:1", []string{"1.2.3.4"}, "203.0.113.9", false},
		{"one proxy", "10.1.1.1:1", []string{"198.51.100.7"}, "198.51.100.7", true},
		{"chain skips trusted", "10.1.1.1:1", []string{"6.6.6.6, 198.51.100.7, 10.2.2.2"}, "198.51.100.7", true},
		{"merged lines", "10.1.1.1:1", []string{"6.6.6.6", "198.51.100.7, 192.168.1.5"}, "198.51.100.7", true},
		{"client forged left of real", "10.1.1.1:1", []string{"1.1.1.1, 203.0.113.50"}, "203.0.113.50", true},
		{"garbage hop stops at nearest parsed", "10.1.1.1:1", []string{"1.1.1.1, bogus, 10.9.9.9"}, "10.9.9.9", true},
		{"all trusted", "10.1.1.1:1", []string{"10.2.2.2"}, "10.2.2.2", true},
		{"mapped v6 peer", "[::ffff:10.1.1.1]:1", []string{"198.51.100.7"}, "198.51.100.7", true},
		{"no port", "203.0.113.9", nil, "203.0.113.9", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		for _, x := range c.xff {
			r.Header.Add("X-Forwarded-For", x)
		}
		ip, via := res.Resolve(r)
		if ip.String() != c.want || via != c.via {
			t.Errorf("%s: got %v via=%v, want %s via=%v", c.name, ip, via, c.want, c.via)
		}
	}
}

func TestRateKeyBucketsIPv6(t *testing.T) {
	a := addrOf("2001:db8:1:2:aaaa::1")
	b := addrOf("2001:db8:1:2:bbbb::9")
	if RateKey(a) != RateKey(b) || RateKey(a) != "2001:db8:1:2::/64" {
		t.Fatalf("%s %s", RateKey(a), RateKey(b))
	}
	if RateKey(addrOf("203.0.113.9")) != "203.0.113.9" || RateKey(addrOf("")) != "unknown" {
		t.Fatal("ipv4/unknown")
	}
}

func TestLimiterRefillAndSweep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := NewLimiter(LimiterOptions{Rule: Rate{PerMinute: 60, Burst: 3}, IdleTTL: time.Minute})
		for i := 0; i < 3; i++ {
			if !l.Allow("k") {
				t.Fatalf("allow %d", i)
			}
		}
		if l.Allow("k") {
			t.Fatal("exhausted bucket allowed")
		}
		time.Sleep(time.Second)
		if !l.Allow("k") || l.Allow("k") {
			t.Fatal("one token per second refill")
		}
		time.Sleep(2 * time.Minute)
		if n := l.Sweep(); n != 1 || l.Len() != 0 {
			t.Fatalf("sweep %d len %d", n, l.Len())
		}
	})
}

func TestLimiterCapUsesOverflowBucket(t *testing.T) {
	l := NewLimiter(LimiterOptions{Rule: Rate{PerMinute: 1, Burst: 1}, MaxKeys: shardCount}) // one key per shard
	allowed := 0
	for i := 0; i < 5000; i++ {
		if l.Allow(fmt.Sprintf("attacker-%d", i)) {
			allowed++
		}
	}
	if l.Len() > shardCount {
		t.Fatalf("keys %d exceed cap", l.Len())
	}
	if allowed > 2*shardCount { // 1 tracked key + 1 overflow token per shard
		t.Fatalf("rotating keys got %d allowances", allowed)
	}
}

func TestLimiterRace(t *testing.T) {
	l := NewLimiter(LimiterOptions{Rule: Rate{PerMinute: 6000, Burst: 100}})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				k := fmt.Sprint(i % 50)
				l.Allow(k)
				if i%100 == 0 {
					l.Sweep()
				}
			}
		}()
	}
	wg.Wait()
}

func TestHeaderGolden(t *testing.T) {
	got := func(c HeaderClass, hsts bool) string {
		var b strings.Builder
		for _, p := range c.Defaults(hsts) {
			fmt.Fprintf(&b, "%s: %s\n", p.Name, p.Value)
		}
		return b.String()
	}
	const common = "X-Content-Type-Options: nosniff\nReferrer-Policy: strict-origin-when-cross-origin\nPermissions-Policy: accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()\n"
	want := map[HeaderClass]string{
		HeadersHTML:  common + "Cross-Origin-Opener-Policy: same-origin\nX-Frame-Options: DENY\n",
		HeadersAsset: "X-Content-Type-Options: nosniff\n",
		HeadersAPI:   common + "Content-Security-Policy: default-src 'none'; frame-ancestors 'none'; sandbox\nCross-Origin-Resource-Policy: same-origin\n",
		HeadersBadge: common + "Content-Security-Policy: default-src 'none'; sandbox\nCross-Origin-Resource-Policy: cross-origin\n",
		HeadersMedia: common + "Content-Security-Policy: default-src 'none'; sandbox\nCross-Origin-Resource-Policy: same-origin\n",
		HeadersNone:  "",
	}
	for c, w := range want {
		if g := got(c, false); g != w {
			t.Errorf("class %q:\n%s\nwant:\n%s", c, g, w)
		}
	}
	if g := got(HeadersAPI, true); !strings.HasSuffix(g, "Strict-Transport-Security: max-age=31536000\n") {
		t.Errorf("hsts: %s", g)
	}
}

func problem(w http.ResponseWriter, r *http.Request, status int, code string, ra time.Duration) {
	w.Header().Set("Content-Type", "application/problem+json")
	if ra > 0 {
		w.Header().Set("Retry-After", RetryAfterSeconds(ra))
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"status": status, "code": code, "instance": "urn:uuid:" + logx.RequestID(r.Context())})
}

type rig struct {
	h       http.Handler
	logs    *bytes.Buffer
	spans   *tracetest.InMemoryExporter
	metrics *sdkmetric.ManualReader
}

func newRig(t *testing.T, mux http.Handler, token secret.Secret, admin bool) *rig {
	t.Helper()
	return newRigWith(t, mux, token, admin, nil)
}

func newRigWith(t *testing.T, mux http.Handler, token secret.Secret, admin bool, tweak func(*Deps)) *rig {
	t.Helper()
	logs := &bytes.Buffer{}
	log := slog.New(logx.NewHandler(logs, logx.Options{Level: slog.LevelDebug, Format: "json", Service: "scp"}))
	spans := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))
	rd := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rd))
	rejected, _ := mp.Meter("t").Int64Counter("scp_ratelimit_rejected_total")
	lims := map[string]*Limiter{"api": NewLimiter(LimiterOptions{Rule: Rate{PerMinute: 60, Burst: 2}, Class: "api", Rejected: rejected})}
	d := Deps{
		Log: log, MeterProvider: mp, TracerProvider: tp,
		Propagator: propagation.TraceContext{}, Clients: mustTrusted(t, "10.0.0.0/8"),
		Hooks: Hooks{
			Recovered: func(w http.ResponseWriter, r *http.Request, rec any) {
				problem(w, r, 500, "internal", 0)
				w.Write([]byte(" pattern=" + r.Pattern))
			},
			RateLimited:     func(w http.ResponseWriter, r *http.Request, ra time.Duration) { problem(w, r, 429, "rate-limited", ra) },
			DenyCrossOrigin: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { problem(w, r, 403, "forbidden", 0) }),
		},
		Limiters: lims, BodyLimit: 16, PublicURL: "https://scp.example.org", HSTS: true,
		CSRFBypass: []string{"POST /webhooks/github"},
		Classify: func(r *http.Request) Class {
			switch {
			case strings.HasPrefix(r.URL.Path, "/api/"):
				return Class{RateClass: "api", Headers: HeadersAPI}
			case r.URL.Path == "/livez":
				return Class{Quiet: true}
			case strings.HasPrefix(r.URL.Path, "/webhooks/"):
				return Class{BodyLimit: 64}
			}
			return Class{Headers: HeadersHTML}
		},
	}
	if tweak != nil {
		tweak(&d)
	}
	var h http.Handler
	if admin {
		h = AdminChain(d, token)(mux)
	} else {
		h = PublicChain(d)(mux)
	}
	return &rig{h: h, logs: logs, spans: spans, metrics: rd}
}

func testMux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v1/items/{id}", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	m.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) { panic("kaput") })
	m.HandleFunc("GET /own-csp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Write([]byte("x"))
	})
	m.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("up")) })
	m.HandleFunc("POST /webhooks/github", func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 100)
		if _, err := r.Body.Read(b); err != nil && ErrBodyTooLarge(err) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	m.HandleFunc("POST /api/v1/mutate", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return m
}

func do(h http.Handler, method, target, remote string, hdr map[string]string, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
		if k == "Host" {
			r.Host = v
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestChainHeadersRequestIDAndLog(t *testing.T) {
	rg := newRig(t, testMux(), secret.Secret{}, false)
	w := do(rg.h, "GET", "/api/v1/items/7?token=hunter2", "203.0.113.9:5", map[string]string{"X-Request-Id": "abc-123"}, "")
	if w.Code != 200 || w.Header().Get("Content-Security-Policy") != cspLocked {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if id := w.Header().Get(HeaderRequestID); len(id) != 36 || id == "abc-123" {
		t.Fatalf("request id %q must be ours (uuid), not the inbound one", id)
	}
	if w.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("hsts on plain http")
	}
	line := rg.logs.String()
	for _, want := range []string{`"route":"GET /api/v1/items/{id}"`, `"status":200`, `"client_ip":"203.0.113.9"`, `"inbound_request_id":"abc-123"`, `"request_id":"` + w.Header().Get(HeaderRequestID)} {
		if !strings.Contains(line, want) {
			t.Errorf("log missing %s:\n%s", want, line)
		}
	}
	if strings.Contains(line, "hunter2") || strings.Contains(line, "/items/7") {
		t.Errorf("raw path or query leaked:\n%s", line)
	}
	// own CSP survives; HSTS arrives via a trusted proxy's X-Forwarded-Proto.
	w = do(rg.h, "GET", "/own-csp", "10.0.0.1:9", map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-For": "198.51.100.1"}, "")
	if w.Header().Get("Content-Security-Policy") != "default-src 'self'" || w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("Strict-Transport-Security") == "" {
		t.Fatalf("%v", w.Header())
	}
	if m := collect(t, rg.metrics); !strings.Contains(m, `"http.route"`) || !strings.Contains(m, "/api/v1/items/{id}") || strings.Contains(m, "/items/7") {
		t.Errorf("otelhttp metric lacks the bounded route label: %s", m)
	}
	if sp := rg.spans.GetSpans(); len(sp) == 0 || sp[0].Name != "GET /api/v1/items/{id}" {
		t.Errorf("span name: %+v", sp)
	}
}

func TestChainRateLimit429(t *testing.T) {
	rg := newRig(t, testMux(), secret.Secret{}, false)
	var last *httptest.ResponseRecorder
	for i := 0; i < 3; i++ {
		last = do(rg.h, "GET", "/api/v1/items/1", "203.0.113.9:1", nil, "")
	}
	if last.Code != 429 || last.Header().Get("Retry-After") == "" || last.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("%d %v", last.Code, last.Header())
	}
	if !strings.Contains(last.Body.String(), "urn:uuid:") {
		t.Fatal(last.Body.String())
	}
	if other := do(rg.h, "GET", "/api/v1/items/1", "203.0.113.10:1", nil, ""); other.Code != 200 {
		t.Fatal("other key must not be limited")
	}
	var rm = collect(t, rg.metrics)
	if !strings.Contains(rm, "scp_ratelimit_rejected_total") {
		t.Fatalf("no rejection counter: %s", rm)
	}
}

func collect(t *testing.T, rd *sdkmetric.ManualReader) string {
	t.Helper()
	var rm = new(metricData)
	if err := rd.Collect(context.Background(), &rm.v); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(rm.v)
	return string(b)
}

func TestChainCSRFAndBypassAndBodyLimit(t *testing.T) {
	rg := newRig(t, testMux(), secret.Secret{}, false)
	if w := do(rg.h, "POST", "/api/v1/mutate", "203.0.113.9:1", map[string]string{"Origin": "https://evil.example", "Host": "scp.example.org"}, ""); w.Code != 403 {
		t.Fatalf("cross-origin POST: %d", w.Code)
	}
	if w := do(rg.h, "POST", "/webhooks/github", "203.0.113.9:1", map[string]string{"Origin": "https://github.com"}, "{}"); w.Code != 202 {
		t.Fatalf("webhook bypass: %d", w.Code)
	}
	if w := do(rg.h, "POST", "/webhooks/github", "203.0.113.9:1", nil, strings.Repeat("x", 80)); w.Code != 413 {
		t.Fatalf("class body limit: %d", w.Code)
	}
}

func TestChainRecoversPanicWithRequestID(t *testing.T) {
	rg := newRig(t, testMux(), secret.Secret{}, false)
	w := do(rg.h, "GET", "/boom", "203.0.113.9:1", nil, "")
	if w.Code != 500 || !strings.Contains(w.Body.String(), "urn:uuid:"+w.Header().Get(HeaderRequestID)) || !strings.Contains(w.Body.String(), "pattern=GET /boom") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for _, h := range []string{"X-Content-Type-Options", "Content-Security-Policy"} { // class html has no CSP: only nosniff is required here
		if h == "X-Content-Type-Options" && w.Header().Get(h) != "nosniff" {
			t.Fatalf("a recovered 500 must carry the class headers: %v", w.Header())
		}
	}
	if !strings.Contains(rg.logs.String(), `"status":500`) || !strings.Contains(rg.logs.String(), "panic in handler") {
		t.Fatal(rg.logs.String())
	}
}

func TestQuietProbeNoSpanDebugLog(t *testing.T) {
	rg := newRig(t, testMux(), secret.Secret{}, false)
	do(rg.h, "GET", "/livez", "127.0.0.1:1", nil, "")
	if n := len(rg.spans.GetSpans()); n != 0 {
		t.Fatalf("probe produced %d spans", n)
	}
	do(rg.h, "GET", "/api/v1/items/1", "203.0.113.9:1", map[string]string{"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}, "")
	sp := rg.spans.GetSpans()
	if len(sp) != 1 || sp[0].Parent.IsValid() || len(sp[0].Links) != 1 {
		t.Fatalf("inbound trace must be a link, not a parent: %+v", sp)
	}
}

func TestAdminBearer(t *testing.T) {
	tok := secret.New("s3cr3t-token-value")
	rg := newRig(t, testMux(), tok, true)
	w401 := do(rg.h, "GET", "/api/v1/items/1", "127.0.0.1:1", nil, "")
	if w401.Code != 401 || w401.Header().Get("WWW-Authenticate") == "" || !strings.Contains(w401.Body.String(), "urn:uuid:"+w401.Header().Get(HeaderRequestID)) {
		t.Fatalf("%d %s", w401.Code, w401.Body)
	}
	if w := do(rg.h, "GET", "/api/v1/items/1", "127.0.0.1:1", map[string]string{"Authorization": "Bearer wrong"}, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := do(rg.h, "GET", "/livez", "127.0.0.1:1", nil, ""); w.Code != 200 { // probes are exempt (unauthenticated on the public listener too)
		t.Fatalf("probe without token: %d", w.Code)
	}
	if w := do(rg.h, "GET", "/api/v1/items/1", "127.0.0.1:1", map[string]string{"Authorization": "bearer s3cr3t-token-value"}, ""); w.Code != 200 {
		t.Fatalf("scheme is case-insensitive: %d", w.Code)
	}
	w := do(rg.h, "GET", "/api/v1/items/1", "127.0.0.1:1", map[string]string{"Authorization": "Bearer s3cr3t-token-value"}, "")
	if w.Code != 200 || w.Header().Get("Content-Security-Policy") != cspLocked {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if strings.Contains(rg.logs.String(), "s3cr3t") {
		t.Fatal("token logged")
	}
}

func TestServeGracefulStop(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Write([]byte("done"))
	})
	srv := NewServer("127.0.0.1:0", h, TimeoutsConfig{ReadHeader: time.Second, Read: 5 * time.Second, Write: 5 * time.Second, Idle: time.Second, MaxHeaderBytes: 16 << 10, MaxHeaderValues: 100}, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, ln, 2*time.Second) }()
	resc := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err != nil {
			resc <- err.Error()
			return
		}
		defer resp.Body.Close()
		b := new(bytes.Buffer)
		b.ReadFrom(resp.Body)
		resc <- b.String()
	}()
	<-started
	cancel()
	time.Sleep(50 * time.Millisecond)
	close(release)
	if got := <-resc; got != "done" {
		t.Fatalf("in-flight request not drained: %q", got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidate(t *testing.T) {
	var got []string
	c := Config{
		Public:   PublicConfig{Listen: ":9090", PublicURL: "https://scp.example.org/path", TrustedProxies: []string{"0.0.0.0/0"}},
		Admin:    AdminConfig{Listen: ":9090"},
		Timeouts: TimeoutsConfig{ReadHeader: time.Minute, Read: time.Second},
	}
	c.Validate(func(p, m string) { got = append(got, p) })
	want := map[string]bool{"public.trusted_proxies": true, "public.public_url": true, "admin.listen": true, "timeouts.read_header": true}
	for _, p := range got {
		delete(want, p)
	}
	if len(want) != 0 {
		t.Fatalf("missing %v in %v", want, got)
	}
}

func TestRateText(t *testing.T) {
	good := map[string]Rate{"120/min burst 60": {120, 60}, "30/min": {30, 30}, "5/s burst 20": {300, 20}, "120/h": {2, 2}, "60/h burst 5": {1, 5}}
	for in, want := range good {
		var r Rate
		if err := r.UnmarshalText([]byte(in)); err != nil || r != want {
			t.Errorf("%q -> %+v %v, want %+v", in, r, err, want)
		}
	}
	for _, in := range []string{"", "fast", "0/min", "10/min burst 0", "10/day", "30/h", "1/h", "10/min burst", "-1/min", "1e3/min"} {
		var r Rate
		if err := r.UnmarshalText([]byte(in)); err == nil {
			t.Errorf("%q accepted as %+v", in, r)
		}
	}
	if s := (Rate{10, 10}).String(); s != "10/min burst 10" {
		t.Fatal(s)
	}
}

func TestExtraMiddlewareRunsAfterClientIPBeforeHeaders(t *testing.T) {
	var seenIP, seenHdr string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("x")) })
	rg := newRig(t, mux, secret.Secret{}, false)
	_ = rg
	d := Deps{
		Log: slog.New(slog.DiscardHandler), MeterProvider: sdkmetric.NewMeterProvider(),
		TracerProvider: sdktrace.NewTracerProvider(), Propagator: propagation.TraceContext{}, Clients: mustTrusted(t),
		Hooks:    Hooks{Recovered: func(w http.ResponseWriter, r *http.Request, rec any) { w.WriteHeader(500) }, RateLimited: func(w http.ResponseWriter, r *http.Request, ra time.Duration) { w.WriteHeader(429) }, DenyCrossOrigin: http.NotFoundHandler()},
		Classify: func(*http.Request) Class { return Class{Headers: HeadersAPI} },
		AfterClientIP: []func(http.Handler) http.Handler{func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seenIP = logx.ClientIP(r.Context())
				seenHdr = w.Header().Get("Content-Security-Policy")
				next.ServeHTTP(w, r)
			})
		}},
	}
	w := do(PublicChain(d)(mux), "GET", "/x", "203.0.113.9:1", nil, "")
	if seenIP != "203.0.113.9" || seenHdr != "" || w.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("ip=%q hdr-before=%q after=%q", seenIP, seenHdr, w.Header().Get("Content-Security-Policy"))
	}
}

func TestNewServerLimits(t *testing.T) {
	srv := NewServer(":0", http.NotFoundHandler(), TimeoutsConfig{ReadHeader: 5 * time.Second, Read: 30 * time.Second, Write: time.Minute, Idle: 2 * time.Minute, MaxHeaderBytes: 16 << 10, MaxHeaderValues: 100}, slog.New(slog.DiscardHandler))
	if srv.ReadHeaderTimeout != 5*time.Second || srv.ReadTimeout != 30*time.Second || srv.WriteTimeout != time.Minute || srv.IdleTimeout != 2*time.Minute || srv.MaxHeaderBytes != 16<<10 || srv.MaxHeaderValueCount != 100 || srv.ErrorLog == nil {
		t.Fatalf("%+v", srv)
	}
}

func TestChainsRefuseMissingDeps(t *testing.T) {
	defer func() {
		r := recover()
		if s, ok := r.(string); !ok || !strings.Contains(s, "Hooks.Recovered") || !strings.Contains(s, "Log") {
			t.Fatalf("panic %v", r)
		}
	}()
	PublicChain(Deps{Clients: mustTrusted(t)})
}

func TestRunSweepersEvictsAndStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := NewLimiter(LimiterOptions{Rule: Rate{60, 5}, IdleTTL: 40 * time.Second})
		b := NewLimiter(LimiterOptions{Rule: Rate{60, 5}, IdleTTL: 10 * time.Minute})
		a.Allow("x")
		b.Allow("y")
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error)
		go func() { done <- RunSweepers(ctx, a, b) }()
		time.Sleep(time.Minute)
		synctest.Wait()
		if a.Len() != 0 || b.Len() != 1 {
			t.Fatalf("a=%d b=%d", a.Len(), b.Len())
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestRecoveredAndSilentHandlersAndInterimResponsesKeepHeaders(t *testing.T) {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v1/silent", func(w http.ResponseWriter, r *http.Request) {}) // writes nothing: implicit 200
	m.HandleFunc("GET /api/v1/early", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusEarlyHints) // 1xx must not consume the final status
		w.WriteHeader(http.StatusNotFound)
	})
	rg := newRig(t, m, secret.Secret{}, false)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	r1, err := http.Get(srv.URL + "/api/v1/silent")
	if err != nil {
		t.Fatal(err)
	}
	r1.Body.Close()
	if r1.StatusCode != 200 || r1.Header.Get("Content-Security-Policy") != cspLocked {
		t.Fatalf("silent handler: %d %v", r1.StatusCode, r1.Header)
	}
	r2, err := http.Get(srv.URL + "/api/v1/early")
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != 404 || r2.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("after a 103 the final status must survive: %d %v", r2.StatusCode, r2.Header)
	}
}

func TestAdminHostGuardRefusesRebindingHosts(t *testing.T) {
	rg := newRigWith(t, testMux(), secret.Secret{}, true, func(d *Deps) { d.AdminHosts = []string{"scp-admin.internal:9091", "10.1.2.3"} })
	for host, want := range map[string]int{"evil.example:9091": 421, "scp-admin.internal:9091": 200, "SCP-ADMIN.internal:9091": 200, "10.1.2.3:9091": 200, "scp-admin.internal:80": 421} {
		if w := do(rg.h, "GET", "/api/v1/items/1", "10.9.9.9:1", map[string]string{"Host": host}, ""); w.Code != want {
			t.Errorf("Host %s: %d want %d", host, w.Code, want)
		}
	}
	if w := do(rg.h, "GET", "/livez", "10.9.9.9:1", map[string]string{"Host": "evil.example"}, ""); w.Code != 200 {
		t.Errorf("probes are exempt: %d", w.Code)
	}
}

func TestLimiterCountsItsOwnRejections(t *testing.T) { // webhook's invalid-signature budget uses the limiter directly, no bridge
	rd := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rd))
	c, _ := mp.Meter("t").Int64Counter("scp_ratelimit_rejected_total")
	l := NewLimiter(LimiterOptions{Rule: Rate{PerMinute: 1, Burst: 1}, Class: "webhook_invalid", Rejected: c})
	l.Allow("k")
	l.Allow("k")
	if m := collect(t, rd); !strings.Contains(m, "scp_ratelimit_rejected_total") || !strings.Contains(m, "webhook_invalid") {
		t.Fatalf("%s", m)
	}
}

func TestServeAllStopsTogetherAndOneFailureStopsAll(t *testing.T) {
	mk := func() Listener {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		return Listener{Srv: NewServer("", http.NotFoundHandler(), TimeoutsConfig{ReadHeader: time.Second, Read: time.Second, Write: time.Second, Idle: time.Second, MaxHeaderBytes: 16 << 10, MaxHeaderValues: 100}, nil), Ln: ln}
	}
	a, b := mk(), mk()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeAll(ctx, time.Second, a, b) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// one listener dies by itself: the other is stopped and the error is reported first.
	c, d := mk(), mk()
	go func() { time.Sleep(50 * time.Millisecond); c.Ln.Close() }()
	err := ServeAll(context.Background(), time.Second, c, d)
	if err == nil {
		t.Fatal("a failed listener must fail ServeAll")
	}
	if _, derr := net.Dial("tcp", d.Ln.Addr().String()); derr == nil {
		t.Fatal("the surviving listener must have been stopped")
	}
}
