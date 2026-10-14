// Package httpapi: scratch subset of design unit 06 (problem details only). Semantics copied from 06 section 4.1:
// WriteProblem copies ONLY Allow, Retry-After and WWW-Authenticate from Problem.Header; RetryAfterSeconds is a body
// member and does not set a header by itself.
package httpapi

import (
	"encoding/json"
	"net/http"
)

type Problem struct {
	Type, Title, Detail, Instance string
	Status                        int
	Code                          string
	RetryAfterSeconds             int
	Header                        http.Header // Go-only; allow-list on write
}

func WriteProblem(w http.ResponseWriter, r *http.Request, p Problem) {
	for _, h := range []string{"Allow", "Retry-After", "WWW-Authenticate"} {
		if v := p.Header.Values(h); len(v) > 0 {
			w.Header().Set(h, v[0])
		}
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": p.Type, "title": p.Title, "status": p.Status, "detail": p.Detail, "code": p.Code, "retryAfterSeconds": p.RetryAfterSeconds})
}
