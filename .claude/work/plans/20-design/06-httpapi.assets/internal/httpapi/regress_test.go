package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
)

// Verification 2026-10-14 (V2): APIDefaults acts under /api/ only. The first draft answered 406 to
// `Accept: text/markdown` on a human URL and on llms.txt and overwrote the Cache-Control of every 2xx.
func TestAPIDefaultsOnlyUnderAPI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /llms.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("x"))
	})
	mux.HandleFunc("GET /badges/a/b.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write([]byte("<svg/>"))
	})
	mux.HandleFunc("GET /api/v1/x", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	})
	h := httpapi.APIDefaults(httpapi.DefaultsOptions{})(mux)
	do := func(target, accept string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", target, nil)
		if accept != "" {
			r.Header.Set("Accept", accept)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := do("/llms.txt", "text/markdown"); w.Code != 200 || w.Header().Get("Cache-Control") != "" {
		t.Errorf("llms.txt: %d cc=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	if w := do("/badges/a/b.svg", "image/svg+xml"); w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Errorf("badge: %d cc=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	if w := do("/dashboard", "text/html"); w.Code == 406 {
		t.Error("a human URL must never be 406")
	}
	if w := do("/api/v1/x", "text/html"); w.Code != 406 {
		t.Errorf("api with text/html: %d", w.Code)
	}
	if w := do("/api/v1/x", ""); w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("api default: %d cc=%q", w.Code, w.Header().Get("Cache-Control"))
	}
}

// Verification (V10): neither strict-server decode errors nor binding errors render library text.
func TestStrictAndBindingErrorsDoNotRenderLibraryText(t *testing.T) {
	reqErr, _ := httpapi.StrictErrors(nil)
	for name, fn := range map[string]func(http.ResponseWriter, *http.Request, error){"request": reqErr, "binding": httpapi.BindingError} {
		w := httptest.NewRecorder()
		fn(w, httptest.NewRequest("POST", "/api/v1/x", nil), errors.New("json: cannot unmarshal string into Go struct field RetryJobRequest.Key of type int hunter2"))
		if w.Code != 400 || strings.Contains(w.Body.String(), "hunter2") || strings.Contains(w.Body.String(), "Go struct") {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
}

// Verification (V3): strict handlers receive (ctx, request object) only; WithRequest + RequestFrom give them the request.
func TestWithRequestAndHelpers(t *testing.T) {
	var got *http.Request
	h := httpapi.WithRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = httpapi.RequestFrom(r.Context()) }))
	r := httptest.NewRequest("GET", "/api/v1/commits?limit=5", nil)
	h.ServeHTTP(httptest.NewRecorder(), r)
	if got == nil || got.URL.RawQuery != "limit=5" {
		t.Fatal("request not in context")
	}
	if httpapi.RequestFrom(context.Background()) != nil {
		t.Error("nil expected without the middleware")
	}
	var np *int
	if httpapi.Deref(np) != 0 || httpapi.Deref(httpapi.Ptr(7)) != 7 {
		t.Error("Deref/Ptr")
	}
}

// Verification (V6): every problem carries nosniff, including one written after a handler set other headers.
func TestWriteProblemSetsNosniff(t *testing.T) {
	w := httptest.NewRecorder()
	httpapi.WriteProblem(w, httptest.NewRequest("GET", "/api/v1/x", nil), httpapi.NotFound("x"))
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff missing")
	}
}

// TestCrossDesignCodes pins the problem constants and the 308 exception that unit 15 and the catalogue of unit 07 need (CD-ER-02).
func TestCrossDesignCodes(t *testing.T) {
	for code, want := range map[string]string{httpapi.CodeFeatureDisabled: "feature-disabled", httpapi.CodeQueueFull: "queue-full", httpapi.CodeRepoRenamed: "repo-renamed", httpapi.CodeUnhealthy: "unhealthy"} {
		if code != want {
			t.Fatalf("code %q != %q", code, want)
		}
	}
	rec := httptest.NewRecorder()
	httpapi.WriteProblem(rec, httptest.NewRequest("GET", "/api/v1/repos/old", nil), httpapi.RepoRenamed("/repos/new"))
	if rec.Code != 308 || rec.Header().Get("Location") != "/repos/new" || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("308 problem: %d %v", rec.Code, rec.Header())
	}
	rec = httptest.NewRecorder()
	httpapi.WriteProblem(rec, httptest.NewRequest("GET", "/x", nil), httpapi.Problem{Status: 308, Code: "other"}) // any other status below 400 is still a 500
	if rec.Code != 500 {
		t.Fatalf("clamp: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	httpapi.WriteProblem(rec, httptest.NewRequest("GET", "/x", nil), httpapi.QueueFull(30*time.Second))
	if rec.Code != 503 || rec.Header().Get("Retry-After") != "30" {
		t.Fatalf("queue-full: %d %v", rec.Code, rec.Header())
	}
}
