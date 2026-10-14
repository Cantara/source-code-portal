package validate

import (
	"net/http"
	"strings"

	"github.com/cantara/source-code-portal/internal/httpapi"
)

// ProblemMux rewrites the plain-text 404/405 that http.ServeMux, http.NotFound and http.Error produce for
// paths under /api/ into problem+json (Allow is kept on 405). It is a safety net behind RequestValidator:
// the validator answers unknown paths and wrong methods from the OpenAPI document, but the mux's catch-all
// routes (SPA fallback) and any handler calling http.NotFound would otherwise leak text/plain into the API.
// A response is rewritten only when status is 404/405 AND Content-Type is text/plain AND nosniff is set,
// which is exactly what net/http's helpers emit; our own problems carry application/problem+json.
func ProblemMux(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, APIPrefix) {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(&pmWriter{ResponseWriter: w, r: r}, r)
	})
}

type pmWriter struct {
	http.ResponseWriter
	r       *http.Request
	wrote   bool
	swallow bool
}

func (p *pmWriter) Unwrap() http.ResponseWriter { return p.ResponseWriter }

func (p *pmWriter) WriteHeader(code int) {
	if p.wrote {
		return
	}
	p.wrote = true
	h := p.Header()
	if (code == http.StatusNotFound || code == http.StatusMethodNotAllowed) &&
		strings.HasPrefix(h.Get("Content-Type"), "text/plain") && h.Get("X-Content-Type-Options") == "nosniff" {
		p.swallow = true
		var pr httpapi.Problem
		if code == http.StatusMethodNotAllowed {
			var allow []string
			for _, a := range strings.Split(h.Get("Allow"), ",") {
				if a = strings.TrimSpace(a); a != "" {
					allow = append(allow, a)
				}
			}
			pr = httpapi.MethodNotAllowed(allow...)
		} else {
			pr = httpapi.NotFound("no such API route")
		}
		h.Del("Allow")
		httpapi.WriteProblem(p.ResponseWriter, p.r, pr)
		return
	}
	p.ResponseWriter.WriteHeader(code)
}

func (p *pmWriter) Write(b []byte) (int, error) {
	if !p.wrote {
		p.WriteHeader(http.StatusOK)
	}
	if p.swallow {
		return len(b), nil
	}
	return p.ResponseWriter.Write(b)
}

func (p *pmWriter) Flush() {
	if !p.wrote {
		p.WriteHeader(http.StatusOK)
	}
	if f, ok := p.ResponseWriter.(http.Flusher); ok && !p.swallow {
		f.Flush()
	}
}
