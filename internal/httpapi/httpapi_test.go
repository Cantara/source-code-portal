package httpapi

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// stub serves the snapshot endpoint for real and nothing else.
type stub struct {
	SnapshotAPI
	oapi.StrictServerInterface
}

func (s stub) GetSnapshot(ctx context.Context, r oapi.GetSnapshotRequestObject) (oapi.GetSnapshotResponseObject, error) {
	return s.SnapshotAPI.GetSnapshot(ctx, r)
}

func public(cur *snapshot.Snapshot) http.Handler {
	spa := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html>")},
		"assets/app-1a2b3c.js": {Data: []byte("console.log(1)")},
	}
	return Public(testOptions(), spa, []byte("openapi: 3.1.0\n"), stub{SnapshotAPI: SnapshotAPI{Current: func() *snapshot.Snapshot { return cur }}},
		Route{Pattern: "POST /upload", MaxBody: 8, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := io.ReadAll(r.Body); err != nil {
				http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			}
		})},
		Route{Pattern: "GET /panic", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })},
		Route{Pattern: "GET /big", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
		})})
}

func testOptions() Options {
	return Options{Log: slog.New(slog.DiscardHandler), HSTS: true, TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
}

func get(h http.Handler, path string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSPA(t *testing.T) {
	h := public(&snapshot.Snapshot{})
	tests := []struct {
		path, cache string
		status      int
	}{
		{"/", "no-cache", http.StatusOK},
		{"/assets/app-1a2b3c.js", "public, max-age=31536000, immutable", http.StatusOK},
		{"/assets/missing.js", "", http.StatusNotFound},
		{"/assets", "", http.StatusNotFound},
		{"/../index.html", "", http.StatusTemporaryRedirect},
	}
	for _, tc := range tests {
		rec := get(h, tc.path)
		if rec.Code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.path, rec.Code, tc.status)
		}
		if got := rec.Header().Get("Cache-Control"); tc.cache != "" && got != tc.cache {
			t.Errorf("%s: Cache-Control %q, want %q", tc.path, got, tc.cache)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: no nosniff", tc.path)
		}
	}
}

func TestSnapshotAndValidators(t *testing.T) {
	cur := &snapshot.Snapshot{Version: 3, BuiltAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), Catalogued: true,
		Sources: []snapshot.SourceStatus{{ID: "gh", Repos: 2, Fresh: snapshot.Freshness{State: snapshot.StateOK, CheckedAt: time.Now(), TTL: time.Hour}}}}
	h := public(cur)
	rec := get(h, "/api/v1/snapshot")
	var got oapi.SnapshotInfo
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("snapshot: %d %s", rec.Code, rec.Body)
	}
	if got.Version != 3 || !got.Ready || len(got.Sources) != 1 || got.Sources[0].Freshness.Status != "fresh" {
		t.Errorf("snapshot = %+v", got)
	}
	etag := rec.Header().Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("validators: ETag %q, Cache-Control %q", etag, rec.Header().Get("Cache-Control"))
	}
	if rec := get(h, "/api/v1/snapshot", "If-None-Match", etag); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 ||
		rec.Header().Get("ETag") != etag {
		t.Errorf("If-None-Match: %d, ETag %q", rec.Code, rec.Header().Get("ETag"))
	}
	cur.Version = 4
	if rec := get(h, "/api/v1/snapshot", "If-None-Match", etag); rec.Code != http.StatusOK {
		t.Errorf("changed body answered %d", rec.Code)
	}
}

func TestAPINotFoundIsProblem(t *testing.T) {
	rec := get(public(&snapshot.Snapshot{}), "/api/v1/nope")
	var p oapi.Problem
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/problem+json" ||
		json.Unmarshal(rec.Body.Bytes(), &p) != nil || p.Code != "not-found" || rec.Header().Get("ETag") != "" {
		t.Errorf("unknown API path: %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
}

func TestProbes(t *testing.T) {
	ready := false
	h := Admin(testOptions(), func() bool { return true }, func() bool { return ready })
	if rec := get(h, "/livez"); rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Errorf("livez: %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(h, "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("readyz while warming: %d", rec.Code)
	}
	ready = true
	if rec := get(h, "/readyz"); rec.Code != http.StatusOK {
		t.Errorf("readyz when ready: %d", rec.Code)
	}
}

func TestFallback(t *testing.T) {
	h := public(&snapshot.Snapshot{})
	for _, tc := range []struct {
		path   string
		status int
		shell  bool
	}{
		{"/commits", 200, true},
		{"/groups/core", 200, true},
		{"/groups/core/commits", 200, true},
		{"/repos/acme/core", 200, true},
		{"/repos/vercel/next.js", 200, true},
		{"/repos/acme/core/commits", 200, true},
		{"/repos/acme/core/docs/docs/setup", 200, true},
		{"/nope/deeper", 404, true},
		{"/groups/core/extra", 404, true},
		{"/favicon.ico", 404, false},
		{"/badges/x", 404, false},
		{"/webhooks/github", 404, false},
		{"/.well-known/security.txt", 404, false},
		{"/assets", 404, false},
	} {
		rec := get(h, tc.path)
		isShell := rec.Body.String() == "<!doctype html>"
		if rec.Code != tc.status || isShell != tc.shell {
			t.Errorf("%s: %d shell %v, want %d shell %v", tc.path, rec.Code, isShell, tc.status, tc.shell)
		}
		if isShell && rec.Header().Get("Content-Security-Policy") != cspShell {
			t.Errorf("%s: shell CSP %q", tc.path, rec.Header().Get("Content-Security-Policy"))
		}
	}
}

func TestHeaders(t *testing.T) {
	h := public(&snapshot.Snapshot{})
	for _, tc := range []struct{ path, csp string }{
		{"/", cspShell},
		{"/repos/a/b", cspShell},
		{"/api/v1/snapshot", cspDefault},
		{"/api/v1/nope", cspDefault},
		{"/api/v1/openapi.yaml", cspDefault},
		{"/favicon.ico", cspDefault},
	} {
		rec := get(h, tc.path, "X-Request-Id", "req-1")
		hd := rec.Header()
		if hd.Get("Content-Security-Policy") != tc.csp || hd.Get("X-Content-Type-Options") != "nosniff" ||
			hd.Get("Referrer-Policy") != "strict-origin-when-cross-origin" || hd.Get("Strict-Transport-Security") == "" ||
			hd.Get("X-Request-Id") != "req-1" || hd.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: headers %v", tc.path, hd)
		}
	}
	if id := get(h, "/", "X-Request-Id", "bad id!").Header().Get("X-Request-Id"); !requestIDPattern.MatchString(id) || id == "bad id!" {
		t.Errorf("an invalid request id was echoed: %q", id)
	}
}

func TestAPIConventions(t *testing.T) {
	h := public(&snapshot.Snapshot{})
	problem := func(rec *httptest.ResponseRecorder) oapi.Problem {
		var p oapi.Problem
		if rec.Header().Get("Content-Type") != "application/problem+json" || rec.Header().Get("Cache-Control") != "no-store" ||
			json.Unmarshal(rec.Body.Bytes(), &p) != nil {
			t.Errorf("not a problem: %v %s", rec.Header(), rec.Body)
		}
		return p
	}
	rec := get(h, "/api/v1/snapshot", "Accept", "text/html")
	if p := problem(rec); rec.Code != http.StatusNotAcceptable || p.Code != "not-acceptable" {
		t.Errorf("Accept text/html: %d %+v", rec.Code, p)
	}
	for _, accept := range []string{"application/json", "*/*", "text/html, application/*;q=0.5", "application/yaml"} {
		if rec := get(h, "/api/v1/snapshot", "Accept", accept); rec.Code != http.StatusOK {
			t.Errorf("Accept %q: %d", accept, rec.Code)
		}
	}
	if rec := get(h, "/api/v1/snapshot", "Accept", "application/json;q=0"); rec.Code != http.StatusNotAcceptable {
		t.Errorf("Accept q=0: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/snapshot", nil)
	req.Header.Set("X-Request-Id", "abc")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if p := problem(rec); rec.Code != http.StatusMethodNotAllowed || p.Code != "method-not-allowed" || p.Instance == nil ||
		*p.Instance != "urn:scp:request:abc" {
		t.Errorf("POST: %d %+v", rec.Code, p)
	}
	if rec := get(h, "/api/v1/openapi.yaml"); rec.Code != 200 || rec.Header().Get("Content-Type") != "application/yaml" {
		t.Errorf("contract: %d %v", rec.Code, rec.Header())
	}
	if rec := get(h, "/api/v1/nope/<script>"); strings.Contains(rec.Body.String(), "script") {
		t.Errorf("the problem reflects input: %s", rec.Body)
	}
}

func TestMiddleware(t *testing.T) {
	h := public(&snapshot.Snapshot{})
	rec := get(h, "/panic")
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "internal") {
		t.Errorf("panic: %d %s", rec.Code, rec.Body)
	}
	rec = get(h, "/big", "Accept-Encoding", "gzip")
	if rec.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("big text: %v", rec.Header())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); len(b) != 4096 {
		t.Errorf("gunzipped %d bytes", len(b))
	}
	if rec := get(h, "/big"); rec.Header().Get("Content-Encoding") != "" || rec.Body.Len() != 4096 {
		t.Errorf("without Accept-Encoding: %v", rec.Header())
	}
	if rec := get(h, "/api/v1/nope", "Accept-Encoding", "gzip"); rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("a small problem was compressed")
	}
	for body, want := range map[string]int{"12345678": 200, "123456789": http.StatusRequestEntityTooLarge} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(body)))
		if rec.Code != want {
			t.Errorf("body of %d bytes: %d, want %d", len(body), rec.Code, want)
		}
	}
}

func TestClientIP(t *testing.T) {
	o := testOptions()
	for _, tc := range []struct{ peer, xff, want string }{
		{"203.0.113.9:1", "198.51.100.1", "203.0.113.9"},
		{"10.0.0.1:1", "198.51.100.1, 10.0.0.2", "198.51.100.1"},
		{"10.0.0.1:1", "1.1.1.1, 198.51.100.1", "198.51.100.1"},
		{"10.0.0.1:1", "garbage", "10.0.0.1"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("X-Forwarded-For", tc.xff)
		if got := o.clientIP(r); got != tc.want {
			t.Errorf("%s via %s: %s, want %s", tc.xff, tc.peer, got, tc.want)
		}
	}
}
