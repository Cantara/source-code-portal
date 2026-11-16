package githubtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// grant is what an installation of the App may read and which webhook events it is sent.
type grant struct {
	permissions map[string]string
	events      []string
}

// AllPermissions and AllEvents are what the fake's installations grant unless SetGrants says otherwise: every permission and event
// the portal asks for.
var (
	AllPermissions = map[string]string{"contents": "read", "metadata": "read", "issues": "read", "pull_requests": "read", //nolint:gochecknoglobals // test data
		"checks": "read", "statuses": "read"}
	AllEvents = []string{"push", "release", "repository", "issues", "pull_request", "pull_request_review"} //nolint:gochecknoglobals // test data
)

// SetGrants sets what the App's installation on login may read and which events it is sent.
func (s *Server) SetGrants(login string, permissions map[string]string, events []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[strings.ToLower(login)] = grant{permissions: maps.Clone(permissions), events: slices.Clone(events)}
}

// charge authenticates a REST call and counts it against the credential's limit. It writes the error and returns "" when the
// call must not proceed.
func (s *Server) charge(w http.ResponseWriter, req *http.Request) string {
	tok := bearer(req)
	s.mu.Lock()
	defer s.mu.Unlock()
	cred := ""
	_, personal := s.tokens[tok]
	switch {
	case personal:
		cred = tok
	case s.issued[tok] != "":
		cred = s.issued[tok]
	default:
		writeError(w, http.StatusUnauthorized, "Bad credentials")
		return ""
	}
	if end, ok := s.expiry[tok]; personal && ok {
		if time.Now().After(end) {
			writeError(w, http.StatusUnauthorized, "Bad credentials")
			return ""
		}
		w.Header().Set("GitHub-Authentication-Token-Expiration", end.UTC().Format("2006-01-02 15:04:05 MST"))
	}
	if reach := s.reach[tok]; personal && reach != nil {
		if rest, ok := strings.CutPrefix(req.URL.Path, "/repos/"); ok {
			if parts := strings.SplitN(rest, "/", 3); len(parts) < 2 || !reach[s.lookup(parts[0], parts[1]).GetID()] {
				writeError(w, http.StatusNotFound, "Not Found")
				return ""
			}
		}
	}
	if now := time.Now(); s.reset.IsZero() || now.After(s.reset) {
		s.reset = now.Add(time.Hour).Truncate(time.Second)
		clear(s.used)
	}
	notModified := req.Header.Get("If-None-Match") != ""
	if s.used[cred] >= Limit {
		s.rateHeaders(w, cred)
		writeError(w, http.StatusForbidden, "API rate limit exceeded")
		return ""
	}
	if !notModified { // conditional requests are charged only when the body changed; see respond
		s.used[cred]++
	}
	s.rateHeaders(w, cred)
	return cred
}

func bearer(req *http.Request) string {
	tok, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !ok {
		tok, _ = strings.CutPrefix(req.Header.Get("Authorization"), "token ")
	}
	return tok
}

func (s *Server) rateHeaders(w http.ResponseWriter, cred string) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(max(Limit-s.used[cred], 0)))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(s.reset.Unix(), 10))
	w.Header().Set("X-RateLimit-Resource", "core")
}

// respond writes v as JSON with an ETag, or 304 when the client already holds it.
func (s *Server) respond(w http.ResponseWriter, req *http.Request, cred string, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	if inm := req.Header.Get("If-None-Match"); inm != "" {
		if inm == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		s.mu.Lock()
		s.used[cred]++
		s.rateHeaders(w, cred)
		s.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(body)
}
