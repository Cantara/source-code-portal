package validate_test

import (
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/validate"
)

//go:embed testdata/openapi.yaml
var spec []byte

func chain(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "{}")
	}
	mux.HandleFunc("GET /api/v1/commits", ok)
	mux.HandleFunc("GET /api/v1/repos/{repo}", ok)
	mux.HandleFunc("POST /api/v1/admin/jobs/retry", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.WriteHeader(202)
		w.Write(b)
	})
	mux.HandleFunc("GET /api/v1/forgot", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }) // handler bug: ProblemMux must catch it
	mux.HandleFunc("GET /{path...}", func(w http.ResponseWriter, r *http.Request) {                            // SPA catch-all stand-in refusing reserved prefixes
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, "shell")
	})
	mw, err := validate.RequestValidator(spec)
	if err != nil {
		t.Fatal(err)
	}
	return validate.ProblemMux(mw(mux))
}

func do(h http.Handler, method, target, body string, hdr ...string) (*httptest.ResponseRecorder, map[string]any) {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var p map[string]any
	if strings.HasPrefix(w.Header().Get("Content-Type"), "application/problem+json") {
		_ = json.Unmarshal(w.Body.Bytes(), &p)
	}
	return w, p
}

func TestValidator(t *testing.T) {
	h := chain(t)
	type tc struct {
		name, method, target, body string
		hdr                        []string
		status                     int
		code, pointer, allow       string
	}
	for _, c := range []tc{
		{name: "ok", method: "GET", target: "/api/v1/commits?limit=50", status: 200},
		{name: "head resolves as GET", method: "HEAD", target: "/api/v1/commits", status: 200},
		{name: "limit too big", method: "GET", target: "/api/v1/commits?limit=201", status: 400, code: "validation-failed", pointer: "#/query/limit"},
		{name: "limit not a number", method: "GET", target: "/api/v1/commits?limit=abc", status: 400, code: "validation-failed", pointer: "#/query/limit"},
		{name: "bad cursor charset", method: "GET", target: "/api/v1/commits?cursor=a%20b", status: 400, code: "validation-failed", pointer: "#/query/cursor"},
		{name: "unknown query param", method: "GET", target: "/api/v1/commits?limt=5", status: 400, code: "validation-failed", pointer: "#/query/limt"},
		{name: "bad slug", method: "GET", target: "/api/v1/repos/a%20b", status: 400, code: "validation-failed", pointer: "#/path/repo"},
		{name: "unknown path", method: "GET", target: "/api/v1/nope", status: 404, code: "not-found"},
		{name: "unknown path POST is 404 not 405", method: "POST", target: "/api/v1/nope", status: 404, code: "not-found"},
		{name: "wrong method", method: "POST", target: "/api/v1/commits", status: 405, code: "method-not-allowed", allow: "GET, HEAD"},
		{name: "DELETE on POST-only", method: "DELETE", target: "/api/v1/admin/jobs/retry", status: 405, code: "method-not-allowed", allow: "POST"},
		{name: "post ok", method: "POST", target: "/api/v1/admin/jobs/retry", body: `{"key":"commits.refresh/a/b/c"}`, hdr: []string{"Content-Type", "application/json"}, status: 202},
		{name: "post wrong content type", method: "POST", target: "/api/v1/admin/jobs/retry", body: `key=x`, hdr: []string{"Content-Type", "text/plain"}, status: 415, code: "unsupported-media-type"},
		{name: "post body violates schema", method: "POST", target: "/api/v1/admin/jobs/retry", body: `{"key":"","x":1}`, hdr: []string{"Content-Type", "application/json"}, status: 400, code: "validation-failed"},
		{name: "handler http.NotFound is rewritten", method: "GET", target: "/api/v1/forgot", status: 404, code: "not-found"},
		{name: "non-api untouched", method: "GET", target: "/dashboard", status: 200},
	} {
		w, p := do(h, c.method, c.target, c.body, c.hdr...)
		if w.Code != c.status {
			t.Errorf("%s: status %d want %d (%s)", c.name, w.Code, c.status, w.Body.String())
			continue
		}
		if c.code != "" {
			if p == nil || p["code"] != c.code {
				t.Errorf("%s: problem %v", c.name, p)
			}
		}
		if c.pointer != "" {
			errs, _ := p["errors"].([]any)
			found := false
			for _, e := range errs {
				if e.(map[string]any)["pointer"] == c.pointer {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: pointer %s not in %v", c.name, c.pointer, p["errors"])
			}
		}
		if c.allow != "" && w.Header().Get("Allow") != c.allow {
			t.Errorf("%s: Allow %q", c.name, w.Header().Get("Allow"))
		}
	}
}

func TestValidatorRejectsBadSpec(t *testing.T) {
	if _, err := validate.RequestValidator([]byte("openapi: 3.1.0\npaths: {a: b}")); err == nil {
		t.Fatal("expected error")
	}
	_ = httpapi.CodeInternal
}

func TestMultiErrorAndBodyPointers(t *testing.T) {
	h := chain(t)
	_, p := do(h, "GET", "/api/v1/commits?limit=201&zzz=1", "")
	got := map[string]bool{}
	for _, e := range p["errors"].([]any) {
		got[e.(map[string]any)["pointer"].(string)] = true
	}
	for _, want := range []string{"#/query/limit", "#/query/zzz"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	_, p = do(h, "POST", "/api/v1/admin/jobs/retry", `{"key":"","x":1}`, "Content-Type", "application/json")
	t.Logf("body errors: %v", p["errors"])
	_, p = do(h, "GET", "/api/v1/commits?limit=1&limit=2", "")
	if p == nil || p["code"] != "validation-failed" {
		t.Fatalf("repeated scalar parameter must be rejected: %v", p)
	}
}

func TestProblemMuxAloneRewritesServeMuxDefaults(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/x", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{}") })
	h := validate.ProblemMux(mux)
	w, p := do(h, "POST", "/api/v1/x", "")
	if w.Code != 405 || p["code"] != "method-not-allowed" || w.Header().Get("Allow") != "GET, HEAD" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("%d %v %v", w.Code, p, w.Header())
	}
	w, p = do(h, "GET", "/api/v1/missing", "")
	if w.Code != 404 || p["code"] != "not-found" {
		t.Fatalf("%d %v", w.Code, p)
	}
	w, _ = do(h, "GET", "/elsewhere", "") // outside /api/: the mux's own text/plain stays
	if w.Code != 404 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatal(w.Header())
	}
}

func TestObserverAndBodyLimit(t *testing.T) {
	var got []string
	mw, err := validate.RequestValidator(spec, validate.WithObserver(func(status int, code string) { got = append(got, code) }))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/commits", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{}") })
	mux.HandleFunc("POST /api/v1/admin/jobs/retry", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
	var h http.Handler = mw(mux)
	h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { // what httpx's body limit does
		r.Body = http.MaxBytesReader(w, r.Body, 16)
		mw(mux).ServeHTTP(w, r)
	})
	if w, _ := do(h, "GET", "/api/v1/commits?limit=999", ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	w, p := do(h, "POST", "/api/v1/admin/jobs/retry", `{"key":"`+strings.Repeat("a", 64)+`"}`, "Content-Type", "application/json")
	if w.Code != 413 || p["code"] != "payload-too-large" {
		t.Fatalf("413 expected: %d %v", w.Code, p)
	}
	if len(got) != 2 || got[0] != "validation-failed" || got[1] != "payload-too-large" {
		t.Fatalf("observer saw %v", got)
	}
}

func TestDetailIsClean(t *testing.T) {
	h := chain(t)
	for target, want := range map[string]string{
		"/api/v1/commits?limit=500": "maximum: got 500, want 200",
		"/api/v1/repos/%20x":        "does not match pattern",
	} {
		_, p := do(h, "GET", target, "")
		d := p["errors"].([]any)[0].(map[string]any)["detail"].(string)
		if !strings.Contains(d, want) || strings.ContainsAny(d, "\n") || strings.Contains(d, "example.com") {
			t.Errorf("%s: detail %q", target, d)
		}
	}
}
