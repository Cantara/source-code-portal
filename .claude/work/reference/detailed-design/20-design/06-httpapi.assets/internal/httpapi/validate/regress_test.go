package validate_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cantara/source-code-portal/internal/httpapi/validate"
)

// Verification (V6): the first draft deleted X-Content-Type-Options when it rewrote a mux 404/405.
func TestProblemMuxRewriteKeepsNosniff(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/forgot", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	h := validate.ProblemMux(mux)
	for _, c := range []struct{ method, target string }{{"GET", "/api/v1/forgot"}, {"POST", "/api/v1/forgot"}, {"GET", "/api/v1/none"}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(c.method, c.target, nil))
		if w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s %s: %v", c.method, c.target, w.Header())
		}
	}
}
