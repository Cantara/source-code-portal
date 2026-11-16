package githubtest

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	gh "github.com/google/go-github/v93/github"
)

func (s *Server) listInstallations(w http.ResponseWriter, req *http.Request) {
	if !s.verifyJWT(req) {
		writeError(w, http.StatusUnauthorized, "A JSON web token could not be decoded")
		return
	}
	s.mu.Lock()
	ids := make([]int64, 0, len(s.installs))
	for id := range s.installs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]*gh.Installation, 0, len(ids))
	for _, id := range ids {
		kind := "Organization"
		if s.users[strings.ToLower(s.installs[id])] {
			kind = "User"
		}
		g, ok := s.grants[strings.ToLower(s.installs[id])]
		if !ok {
			g = grant{permissions: AllPermissions, events: AllEvents}
		}
		var perms gh.InstallationPermissions
		b, _ := json.Marshal(g.permissions)
		_ = json.Unmarshal(b, &perms)
		selection := "all"
		if _, ok := s.selected[strings.ToLower(s.installs[id])]; ok {
			selection = "selected"
		}
		out = append(out, &gh.Installation{ID: new(id), Account: &gh.User{Login: new(s.installs[id]), Type: new(kind)}, Permissions: &perms,
			Events: slices.Clone(g.events), RepositorySelection: new(selection)})
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) accessToken(w http.ResponseWriter, req *http.Request) {
	if !s.verifyJWT(req) {
		writeError(w, http.StatusUnauthorized, "A JSON web token could not be decoded")
		return
	}
	id, _ := strconv.ParseInt(req.PathValue("id"), 10, 64)
	s.mu.Lock()
	login, ok := s.installs[id]
	tok := ""
	if ok {
		buf := make([]byte, 12)
		_, _ = rand.Read(buf)
		tok = "ghs_" + hex.EncodeToString(buf)
		s.issued[tok] = login
	}
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": tok, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
}

// listRepos answers the four listings: an organisation's, a user's public repositories, the token's own account's, an
// installation's.
func (s *Server) listRepos(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	var owner string
	public := false
	switch {
	case req.PathValue("org") != "":
		if owner = strings.ToLower(req.PathValue("org")); s.users[owner] {
			owner = "" // GitHub has no organisation of that name
		}
	case req.PathValue("user") != "":
		if owner = strings.ToLower(req.PathValue("user")); !s.users[owner] {
			owner = ""
		}
		public = true
	case req.URL.Path == "/user/repos":
		owner = strings.ToLower(s.tokens[bearer(req)])
	default: // /installation/repositories
		owner = strings.ToLower(s.issued[bearer(req)])
	}
	repos, ok := s.orgs[owner]
	reach := s.reach[bearer(req)]
	var sel map[int64]bool
	if req.URL.Path == "/installation/repositories" {
		sel = s.selected[owner]
	}
	repos = slices.DeleteFunc(slices.Clone(repos), func(r *gh.Repository) bool {
		return public && r.GetVisibility() == "private" || reach != nil && !reach[r.GetID()] || sel != nil && !sel[r.GetID()]
	})
	s.mu.Unlock()
	if !ok && req.URL.Path != "/user/repos" && req.URL.Path != "/installation/repositories" {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	slices.SortStableFunc(repos, func(a, b *gh.Repository) int { return b.GetPushedAt().Compare(a.GetPushedAt().Time) })
	per, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
	if per <= 0 || per > 100 {
		per = 30
	}
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	page = max(page, 1)
	lo, hi := min((page-1)*per, len(repos)), min(page*per, len(repos))
	if hi < len(repos) {
		next := *req.URL
		q := next.Query()
		q.Set("page", strconv.Itoa(page+1))
		next.RawQuery = q.Encode()
		w.Header().Set("Link", fmt.Sprintf(`<%s%s>; rel="next"`, s.URL, next.RequestURI()))
	}
	if req.URL.Path == "/installation/repositories" {
		s.respond(w, req, cred, &gh.ListRepositories{TotalCount: new(len(repos)), Repositories: repos[lo:hi]})
		return
	}
	s.respond(w, req, cred, repos[lo:hi])
}

// getUser answers GET /users/{user} and, for a personal token, GET /user.
func (s *Server) getUser(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	login, kind := req.PathValue("user"), "Organization"
	self := login == ""
	if self {
		login = s.tokens[bearer(req)]
	}
	_, known := s.orgs[strings.ToLower(login)]
	user := s.users[strings.ToLower(login)] || self && !known // a token's own account is a user's
	s.mu.Unlock()
	if user {
		kind = "User"
	}
	if login == "" || !known && !user {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	s.respond(w, req, cred, &gh.User{Login: new(login), Type: new(kind)})
}

func (s *Server) getRepo(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	var found *gh.Repository
	for _, r := range s.orgs[strings.ToLower(req.PathValue("owner"))] {
		if strings.EqualFold(r.GetName(), req.PathValue("name")) {
			c := *r
			found = &c
		}
	}
	s.mu.Unlock()
	if found == nil {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	s.respond(w, req, cred, found)
}
