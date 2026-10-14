package httpapi

import (
	"net/http"
	"strconv"
)

// Redirect answers every request with status and a path-absolute Location produced by target. If target
// returns "" the request is a 404 problem. The body is empty, the query string is NOT carried unless
// target does so. Only same-origin, path-absolute targets are accepted ("/x", never "//x" or "http:").
func Redirect(status int, target func(*http.Request) string, maxAge int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loc := target(r)
		if loc == "" || loc[0] != '/' || (len(loc) > 1 && (loc[1] == '/' || loc[1] == '\\')) {
			WriteProblem(w, r, NotFound("no such page"))
			return
		}
		h := w.Header()
		h.Set("Location", loc)
		if maxAge > 0 {
			h.Set("Cache-Control", "public, max-age="+strconv.Itoa(maxAge))
		} else {
			h.Set("Cache-Control", "no-store")
		}
		h.Set("Content-Length", "0")
		w.WriteHeader(status)
	})
}
