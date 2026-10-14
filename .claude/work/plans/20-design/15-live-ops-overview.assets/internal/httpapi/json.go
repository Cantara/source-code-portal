package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
)

// WriteJSON is the one encoder for hand-written JSON endpoints (generated strict handlers have their own).
// It marshals first, so an encoding error yields a problem instead of a half-written body.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		WriteProblem(w, r, Internal())
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(buf.Bytes())
	}
}
