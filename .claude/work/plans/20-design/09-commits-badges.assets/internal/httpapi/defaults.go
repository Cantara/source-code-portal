package httpapi

import (
	"net/http"
	"strings"
)

// APIPrefix is the only path prefix APIDefaults (and the validate package) act on.
const APIPrefix = "/api/"

// DefaultsOptions configures APIDefaults.
type DefaultsOptions struct {
	// Private marks API responses `private` (set when access.mode != public: responses depend on the identity).
	Private bool
}

// apiTypes are the media types an /api/ response can have.
func apiTypes() []string {
	return []string{"application/json", "application/problem+json", ContentTypeYAML, "application/vnd.cyclonedx+json", "text/event-stream"}
}

// APIDefaults acts under /api/ ONLY: every other path passes through untouched (the human URLs must answer
// `Accept: text/markdown` and `Accept: text/html`, llms.txt `text/plain`, badges `image/svg+xml`, and their
// Cache-Control belongs to their own handlers; the first draft applied the 406 and the default to every path).
// It (1) answers 406 not-acceptable when the Accept header is present,
// well-formed and excludes every API media type, and (2) adds `Cache-Control: no-cache` (or `private, no-cache`)
// to 2xx/304 responses to GET and HEAD that carry none (handlers need not declare it per operation).
func APIDefaults(o DefaultsOptions) func(http.Handler) http.Handler {
	cc := "no-cache"
	if o.Private {
		cc = "private, no-cache"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, APIPrefix) {
				next.ServeHTTP(w, r)
				return
			}
			if a := r.Header.Get("Accept"); a != "" && !AcceptsAny(a, apiTypes()...) {
				WriteProblem(w, r, NotAcceptable(apiTypes()...))
				return
			}
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				w = &ccWriter{ResponseWriter: w, cc: cc}
			}
			next.ServeHTTP(w, r)
		})
	}
}

type ccWriter struct {
	http.ResponseWriter
	cc   string
	done bool
}

func (c *ccWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }
func (c *ccWriter) WriteHeader(code int) {
	if !c.done {
		c.done = true
		if (code >= 200 && code < 300 || code == http.StatusNotModified) && c.Header().Get("Cache-Control") == "" {
			c.Header().Set("Cache-Control", c.cc)
		}
	}
	c.ResponseWriter.WriteHeader(code)
}
func (c *ccWriter) Write(p []byte) (int, error) {
	if !c.done {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(p)
}
func (c *ccWriter) Flush() {
	if !c.done {
		c.WriteHeader(http.StatusOK)
	}
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
