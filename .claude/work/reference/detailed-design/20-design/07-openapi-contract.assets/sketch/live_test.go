package live_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/apitest"
	"github.com/cantara/source-code-portal/internal/live"
)

type snap struct{ epoch string }

func (snap) Version() uint64    { return 1841 }
func (snap) BuiltAt() time.Time { return time.Date(2026, 10, 14, 9, 41, 7, 0, time.UTC) }
func (snap) Ready() bool        { return true }
func (s snap) Epoch() string    { return s.epoch }

// chain is the API chain of unit 06 reduced to what the contract needs from it: FoldIfNoneMatch (an addition of this contract),
// WithRequest (unit 06) and the mux. cmd/scp adds ProblemMux, the request validator, APIDefaults and Gzip around it.
func chain(epoch string) http.Handler {
	mux := http.NewServeMux()
	(&live.Service{Snap: snap{epoch}, BuildID: "9c4e1f2", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Mount(mux)
	return httpapi.FoldIfNoneMatch(httpapi.WithRequest(mux))
}

func do(h http.Handler, method, path string, hdr http.Header) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, nil)
	for k, vs := range hdr {
		for _, v := range vs {
			r.Header.Add(k, v)
		}
	}
	h.ServeHTTP(rr, r)
	return rr
}

func TestConditionalGetHeadAndSSE(t *testing.T) {
	h := chain("a1b2c3d4")
	rr := do(h, "GET", "/api/v1/snapshot", nil)
	tag := rr.Header().Get("ETag")
	if rr.Code != 200 || tag != `W/"va1b2c3d4-1841"` || !strings.Contains(rr.Body.String(), `"version":1841`) {
		t.Fatalf("200 case: %d %q %s", rr.Code, tag, rr.Body.String())
	}
	if rr := do(h, "GET", "/api/v1/snapshot", http.Header{"If-None-Match": {tag}}); rr.Code != 304 || rr.Body.Len() != 0 || rr.Header().Get("ETag") != tag {
		t.Fatalf("304 case: %d %q", rr.Code, rr.Body.String())
	}
	if rr := do(h, "GET", "/api/v1/snapshot", http.Header{"If-None-Match": {`W/"va1b2c3d4-1"`}}); rr.Code != 200 {
		t.Fatalf("stale validator must be 200, got %d", rr.Code)
	}
	// HEAD reaches the same handler (the GET pattern also matches HEAD); net/http drops the body of a HEAD response on a real
	// connection, httptest.ResponseRecorder does not, so only status and headers are asserted here.
	if rr := do(h, "HEAD", "/api/v1/snapshot", nil); rr.Code != 200 || rr.Header().Get("ETag") != tag {
		t.Fatalf("HEAD: %d %q", rr.Code, rr.Header().Get("ETag"))
	}
	if rr := do(h, "GET", "/api/v1/snapshot", http.Header{"If-None-Match": {`*`}}); rr.Code != 304 {
		t.Fatalf("* must match any current representation, got %d", rr.Code)
	}
}

// A restarted process publishes the same version number (a warm start begins at 1 and climbs): without the epoch in the tag a
// browser holding the previous process's tag would be told 304 for a different body.
func TestTagDoesNotSurviveARestart(t *testing.T) {
	before := do(chain("0000aaaa"), "GET", "/api/v1/snapshot", nil).Header().Get("ETag")
	after := chain("0000bbbb")
	if rr := do(after, "GET", "/api/v1/snapshot", http.Header{"If-None-Match": {before}}); rr.Code != 200 {
		t.Fatalf("a tag of another process must not match, got %d", rr.Code)
	}
}

// The generated binder of a header parameter accepts exactly one header line. RFC 9110 lets a client or a proxy split the
// list over two lines; without the fold such a request is a 400 although it is valid HTTP.
func TestRepeatedIfNoneMatchLines(t *testing.T) {
	h := chain("a1b2c3d4")
	tag := do(h, "GET", "/api/v1/snapshot", nil).Header().Get("ETag")
	two := http.Header{"If-None-Match": {`W/"x"`, tag}}
	if rr := do(h, "GET", "/api/v1/snapshot", two); rr.Code != 304 {
		t.Fatalf("two lines with the current tag in the second: want 304, got %d %s", rr.Code, rr.Body.String())
	}
	bare := httpapi.WithRequest(http.NewServeMux()) // no fold, same mux
	mux := http.NewServeMux()
	(&live.Service{Snap: snap{"a1b2c3d4"}, BuildID: "x", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Mount(mux)
	bare = httpapi.WithRequest(mux)
	if rr := do(bare, "GET", "/api/v1/snapshot", two); rr.Code != 400 {
		t.Fatalf("without the fold the generated binder rejects two lines with a 400 (documented limitation), got %d", rr.Code)
	}
}

func TestStream(t *testing.T) {
	h := chain("a1b2c3d4")
	rr := do(h, "GET", "/api/v1/events", nil)
	if rr.Code != 200 || rr.Header().Get("Content-Type") != "text/event-stream" || !strings.HasPrefix(rr.Body.String(), "retry: 3000\n\nid: 1841\nevent: hello\n") || strings.Contains(rr.Body.String(), "resync") {
		t.Fatalf("sse: %d %q", rr.Code, rr.Body.String())
	}
	// Last-Event-ID ahead of this process (the server restarted): resync follows hello
	rr = do(h, "GET", "/api/v1/events", http.Header{"Last-Event-Id": {"99999"}})
	if !strings.Contains(rr.Body.String(), "event: resync\n") {
		t.Fatalf("a Last-Event-ID ahead of the process must be answered with resync: %q", rr.Body.String())
	}
	// 20 digits that overflow uint64 are ahead of every real version
	rr = do(h, "GET", "/api/v1/events", http.Header{"Last-Event-Id": {"99999999999999999999"}})
	if !strings.Contains(rr.Body.String(), "event: resync\n") {
		t.Fatalf("an overflowing Last-Event-ID must be answered with resync: %q", rr.Body.String())
	}
	// a malformed Last-Event-ID never reaches the handler in production (pattern ^[0-9]{1,20}$), and here it is simply ignored
	rr = do(h, "GET", "/api/v1/events", http.Header{"Last-Event-Id": {"zzz"}})
	if strings.Contains(rr.Body.String(), "resync") {
		t.Fatalf("malformed id must not resync: %q", rr.Body.String())
	}
}

// Every response of the feature is validated against the real bundled contract (apitest of unit 06): status declared, headers
// typed, body schema, null versus absent.
func TestResponsesSatisfyTheContract(t *testing.T) {
	spec, err := os.ReadFile("testdata/openapi.admin.bundled.yaml")
	if err != nil {
		t.Skip("run through tools/prove.sh, which copies the bundle")
	}
	doc := apitest.Load(t, spec)
	h := chain("a1b2c3d4")
	for _, p := range []string{"/api/v1/snapshot"} {
		req := httptest.NewRequest("GET", p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		apitest.ValidateResponse(t, doc, req, rec)
	}
	if v := apitest.Lint(doc); len(v) > 0 {
		t.Fatalf("unit 06's lint (L1-L11) on the real admin bundle:\n%s", strings.Join(v, "\n"))
	}
}
