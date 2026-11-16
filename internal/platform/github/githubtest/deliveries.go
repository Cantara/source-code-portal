package githubtest

import (
	"net/http"
	"slices"
	"strconv"
	"time"
)

// Delivery is a line of the App's webhook delivery log (SetDeliveries).
type Delivery struct {
	GUID         string
	Event        string
	Action       string
	RepositoryID int64
	At           time.Time
	Status       int // the portal's answer; 0: none
}

// SetDeliveries sets the App's webhook delivery log, newest first.
func (s *Server) SetDeliveries(d ...Delivery) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveries = slices.Clone(d)
}

// atoiOr is the integer in v, or def when v is not one.
func atoiOr(v string, def int) int {
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return def
}

func (s *Server) listDeliveries(w http.ResponseWriter, req *http.Request) {
	if !s.verifyJWT(req) {
		writeError(w, http.StatusUnauthorized, "A JSON web token could not be decoded")
		return
	}
	per := min(max(atoiOr(req.URL.Query().Get("per_page"), 30), 1), 100)
	from := atoiOr(req.URL.Query().Get("cursor"), 0)
	s.mu.Lock()
	defer s.mu.Unlock()
	to := min(from+per, len(s.deliveries))
	out := make([]map[string]any, 0, per)
	for i := min(from, to); i < to; i++ {
		d := s.deliveries[i]
		out = append(out, map[string]any{"id": i + 1, "guid": d.GUID, "event": d.Event, "action": d.Action, "repository_id": d.RepositoryID,
			"delivered_at": d.At.UTC().Format(time.RFC3339), "status_code": d.Status})
	}
	if to < len(s.deliveries) { // GitHub pages this log by cursor, newest first
		next := *req.URL
		q := next.Query()
		q.Set("cursor", strconv.Itoa(to))
		next.RawQuery = q.Encode()
		w.Header().Set("Link", `<`+s.URL+next.Path+`?`+next.RawQuery+`>; rel="next"`)
	}
	writeJSON(w, http.StatusOK, out)
}
