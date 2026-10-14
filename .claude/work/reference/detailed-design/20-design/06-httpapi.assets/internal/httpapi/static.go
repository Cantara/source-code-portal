package httpapi

import (
	"net/http"
	"strconv"
)

// ContentTypeYAML is the media type of the OpenAPI bundles (RFC 9512).
const ContentTypeYAML = "application/yaml"

// Static serves immutable embedded bytes with a strong ETag computed once, Cache-Control: no-cache,
// If-None-Match handling (weak comparison) and HEAD. Other methods get a 405 problem with Allow.
func Static(contentType string, body []byte) http.Handler {
	etag := StrongETag(body)
	length := strconv.Itoa(len(body))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			WriteProblem(w, r, MethodNotAllowed(http.MethodGet, http.MethodHead))
			return
		}
		h := w.Header()
		h.Set("ETag", etag)
		h.Set("Cache-Control", "no-cache")
		if NotModified(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h.Set("Content-Type", contentType)
		h.Set("Content-Length", length)
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	})
}
