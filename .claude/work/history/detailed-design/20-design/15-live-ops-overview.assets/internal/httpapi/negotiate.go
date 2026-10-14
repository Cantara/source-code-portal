package httpapi

import "net/http"

type negotiateConfig struct {
	alternate func(*http.Request) string
}

// NegotiateOption configures Negotiate.
type NegotiateOption func(*negotiateConfig)

// WithAlternate adds `Link: <url>; rel="alternate"; type="text/markdown"` to the HTML branch; fn returns the
// path-absolute URL of the Markdown representation, or "" for none.
func WithAlternate(fn func(*http.Request) string) NegotiateOption {
	return func(c *negotiateConfig) { c.alternate = fn }
}

// Negotiate serves markdown when the client explicitly ranks text/markdown above text/html, else spa.
// It never answers 406: human URLs always have an HTML (SPA) representation, and `*/*` means HTML.
// Both branches get Vary: Accept and Link: </llms.txt>; rel="describedby".
func Negotiate(spa, markdown http.Handler, opts ...NegotiateOption) http.Handler {
	var c negotiateConfig
	for _, o := range opts {
		o(&c)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Add("Vary", "Accept")
		h.Add("Link", `</llms.txt>; rel="describedby"`)
		if preferMarkdown(r.Header.Get("Accept")) {
			markdown.ServeHTTP(w, r)
			return
		}
		if c.alternate != nil {
			if u := c.alternate(r); u != "" {
				h.Add("Link", "<"+u+`>; rel="alternate"; type="text/markdown"`)
			}
		}
		spa.ServeHTTP(w, r)
	})
}
