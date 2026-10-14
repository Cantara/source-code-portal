// Package githubtest is a fake GitHub REST server for tests: App and token authentication, organisation listings with
// pagination, single repositories, rate-limit headers, ETags and 304s, fault injection and a request ledger.
package githubtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gh "github.com/google/go-github/v92/github"
)

// Limit is the core rate limit of every credential.
const Limit = 5000

// Server is a fake GitHub. Its methods are safe for concurrent use.
type Server struct {
	*httptest.Server
	AppID int64

	mu       sync.Mutex
	key      *rsa.PrivateKey
	tokens   map[string]bool   // accepted personal tokens
	installs map[int64]string  // installation id -> account login
	issued   map[string]string // installation token -> account login
	orgs     map[string][]*gh.Repository
	commits  map[int64][]Commit          // by repository id, newest first
	files    map[int64]map[string]string // by repository id: path -> content
	nextID   int64
	used     map[string]int // credential -> requests charged this window
	reset    time.Time
	faults   []*fault
	ledger   []string
}

type fault struct {
	pattern    string // method and path prefix, for example "GET /orgs/acme/repos"
	status     int
	retryAfter time.Duration
	times      int
}

// New starts a fake GitHub with a fresh App key; it is closed when the test ends.
func New(t testing.TB) *Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{AppID: 42, key: key, tokens: map[string]bool{}, installs: map[int64]string{}, issued: map[string]string{},
		orgs: map[string][]*gh.Repository{}, commits: map[int64][]Commit{}, files: map[int64]map[string]string{}, nextID: 1000, used: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /app/installations", s.listInstallations)
	mux.HandleFunc("POST /app/installations/{id}/access_tokens", s.accessToken)
	mux.HandleFunc("GET /orgs/{org}/repos", s.listRepos)
	mux.HandleFunc("GET /repos/{owner}/{name}", s.getRepo)
	mux.HandleFunc("GET /repos/{owner}/{name}/commits", s.listCommits)
	mux.HandleFunc("GET /repos/{owner}/{name}/git/trees/{ref}", s.getTree)
	mux.HandleFunc("GET /repos/{owner}/{name}/git/blobs/{sha}", s.getBlob)
	s.Server = httptest.NewServer(s.wrap(mux))
	t.Cleanup(s.Close)
	return s
}

// BaseURL is the REST base with a trailing slash.
func (s *Server) BaseURL() string { return s.URL + "/" }

// PrivateKeyPEM is the App private key.
func (s *Server) PrivateKeyPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(s.key)})
}

// AddToken accepts tok as a personal access token.
func (s *Server) AddToken(tok string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[tok] = true
}

// Install installs the App on login and returns the installation id.
func (s *Server) Install(login string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	s.installs[s.nextID] = login
	return s.nextID
}

// AddRepo adds a repository to org and returns its id. Fields left empty get defaults.
func (s *Server) AddRepo(org string, r gh.Repository) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	if r.ID == nil {
		r.ID = new(s.nextID)
	}
	if r.Owner == nil {
		r.Owner = &gh.User{Login: new(org)}
	}
	if r.DefaultBranch == nil {
		r.DefaultBranch = new("main")
	}
	if r.HTMLURL == nil {
		r.HTMLURL = new("https://github.com/" + org + "/" + r.GetName())
	}
	if r.PushedAt == nil {
		r.PushedAt = &gh.Timestamp{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(s.nextID) * time.Minute)}
	}
	key := strings.ToLower(org)
	s.orgs[key] = append(s.orgs[key], &r)
	return r.GetID()
}

// Update changes the repository with id in place, for example to rename it or touch pushed_at.
func (s *Server) Update(id int64, fn func(*gh.Repository)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, repos := range s.orgs {
		for _, r := range repos {
			if r.GetID() == id {
				fn(r)
			}
		}
	}
}

// Remove deletes the repository with id.
func (s *Server) Remove(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for org, repos := range s.orgs {
		s.orgs[org] = slices.DeleteFunc(repos, func(r *gh.Repository) bool { return r.GetID() == id })
	}
}

// Fail makes the next times requests whose "METHOD /path" starts with pattern answer status. A 429 or 403 with retryAfter
// sends Retry-After (a secondary rate limit).
func (s *Server) Fail(pattern string, status, times int, retryAfter time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = append(s.faults, &fault{pattern: pattern, status: status, retryAfter: retryAfter, times: times})
}

// Exhaust uses up the rate limit of credential (a token, or an installation's account login).
func (s *Server) Exhaust(credential string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reset.IsZero() {
		s.reset = time.Now().Add(time.Hour).Truncate(time.Second)
	}
	s.used[credential] = Limit
}

// Ledger returns "METHOD /path?query status" for every request so far.
func (s *Server) Ledger() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.ledger)
}

// Count returns how many requests started with prefix ("GET /orgs/acme/repos").
func (s *Server) Count(prefix string) int {
	n := 0
	for _, l := range s.Ledger() {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (s *Server) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			s.mu.Lock()
			s.ledger = append(s.ledger, fmt.Sprintf("%s %s %d", req.Method, req.URL.RequestURI(), rec.status))
			s.mu.Unlock()
		}()
		line := req.Method + " " + req.URL.Path
		s.mu.Lock()
		for _, f := range s.faults {
			if f.times > 0 && strings.HasPrefix(line, f.pattern) {
				f.times--
				s.mu.Unlock()
				if f.retryAfter > 0 {
					w.Header().Set("Retry-After", strconv.Itoa(int(f.retryAfter/time.Second)))
					writeJSON(rec, f.status, map[string]string{"message": "You have exceeded a secondary rate limit",
						"documentation_url": "https://docs.github.com/rest/overview/rate-limits-for-the-rest-api#about-secondary-rate-limits"})
					return
				}
				writeError(rec, f.status, "injected fault")
				return
			}
		}
		s.mu.Unlock()
		next.ServeHTTP(rec, req)
	})
}

// charge authenticates a REST call and counts it against the credential's limit. It writes the error and returns "" when the
// call must not proceed.
func (s *Server) charge(w http.ResponseWriter, req *http.Request) string {
	tok, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !ok {
		tok, _ = strings.CutPrefix(req.Header.Get("Authorization"), "token ")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cred := ""
	switch {
	case s.tokens[tok]:
		cred = tok
	case s.issued[tok] != "":
		cred = s.issued[tok]
	default:
		writeError(w, http.StatusUnauthorized, "Bad credentials")
		return ""
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
		out = append(out, &gh.Installation{ID: new(id), Account: &gh.User{Login: new(s.installs[id])}})
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

func (s *Server) listRepos(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	repos, ok := s.orgs[strings.ToLower(req.PathValue("org"))]
	repos = slices.Clone(repos)
	s.mu.Unlock()
	if !ok {
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
	s.respond(w, req, cred, repos[lo:hi])
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

// verifyJWT checks the App's RS256 JWT: signature, issuer and expiry.
func (s *Server) verifyJWT(req *http.Request) bool {
	tok, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(tok, ".")
	if !ok || len(parts) != 3 {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(&s.key.PublicKey, crypto.SHA256, digest[:], sig) != nil {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Iss json.RawMessage `json:"iss"`
		Exp int64           `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return false
	}
	return strings.Trim(string(claims.Iss), `"`) == strconv.FormatInt(s.AppID, 10) && time.Now().Unix() < claims.Exp
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"message": msg})
}

// Commit is a commit on the fake default branch.
type Commit struct {
	SHA, Message, AuthorName, AuthorLogin string
	Date                                  time.Time
}

// SetCommits replaces the default-branch commits of the repository with id (newest first). No commits means an empty repository.
func (s *Server) SetCommits(id int64, cs ...Commit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits[id] = cs
}

// SetFile writes a file on the default branch of the repository with id; an empty content deletes it.
func (s *Server) SetFile(id int64, path, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files[id] == nil {
		s.files[id] = map[string]string{}
	}
	if content == "" {
		delete(s.files[id], path)
		return
	}
	s.files[id][path] = content
}

// lookup returns the repository at owner/name. Callers hold s.mu.
func (s *Server) lookup(owner, name string) *gh.Repository {
	for _, r := range s.orgs[strings.ToLower(owner)] {
		if strings.EqualFold(r.GetName(), name) {
			return r
		}
	}
	return nil
}

func (s *Server) listCommits(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	var cs []Commit
	if r != nil {
		cs = slices.Clone(s.commits[r.GetID()])
	}
	s.mu.Unlock()
	switch {
	case r == nil:
		writeError(w, http.StatusNotFound, "Not Found")
		return
	case len(cs) == 0:
		writeError(w, http.StatusConflict, "Git Repository is empty.")
		return
	}
	per, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
	if per <= 0 || per > 100 {
		per = 30
	}
	out := make([]*gh.RepositoryCommit, 0, min(per, len(cs)))
	for _, c := range cs[:min(per, len(cs))] {
		date := &gh.Timestamp{Time: c.Date}
		rc := &gh.RepositoryCommit{SHA: new(c.SHA), HTMLURL: new(r.GetHTMLURL() + "/commit/" + c.SHA),
			Commit: &gh.Commit{Message: new(c.Message), Author: &gh.CommitAuthor{Name: new(c.AuthorName), Date: date},
				Committer: &gh.CommitAuthor{Name: new(c.AuthorName), Date: date}}}
		if c.AuthorLogin != "" {
			rc.Author = &gh.User{Login: new(c.AuthorLogin)}
		}
		out = append(out, rc)
	}
	s.respond(w, req, cred, out)
}

// tree builds the git tree at dir ("" is the root) from the repository's files. Callers hold s.mu.
func (s *Server) tree(id int64, dir string) *gh.Tree {
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	seen := map[string]bool{}
	var entries []*gh.TreeEntry
	for _, p := range slices.Sorted(maps.Keys(s.files[id])) {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok {
			continue
		}
		if sub, _, isDir := strings.Cut(rest, "/"); isDir {
			if !seen[sub] {
				seen[sub] = true
				t := s.tree(id, prefix+sub)
				entries = append(entries, &gh.TreeEntry{Path: new(sub), Type: new("tree"), Mode: new("040000"), SHA: t.SHA})
			}
			continue
		}
		content := s.files[id][p]
		entries = append(entries, &gh.TreeEntry{Path: new(rest), Type: new("blob"), Mode: new("100644"), SHA: new(blobSHA(content)),
			Size: new(len(content))})
	}
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s %s\n", e.GetPath(), e.GetSHA())
	}
	return &gh.Tree{SHA: new(hex.EncodeToString(h.Sum(nil))[:40]), Entries: entries}
}

// flatten replaces the tree entries of t with everything below them, paths relative to t. Callers hold s.mu.
func (s *Server) flatten(id int64, dir string, t *gh.Tree) *gh.Tree {
	var entries []*gh.TreeEntry
	for _, e := range t.Entries {
		entries = append(entries, e)
		if e.GetType() == "tree" {
			sub := e.GetPath()
			if dir != "" {
				sub = dir + "/" + sub
			}
			for _, c := range s.flatten(id, sub, s.tree(id, sub)).Entries {
				entries = append(entries, &gh.TreeEntry{Path: new(e.GetPath() + "/" + c.GetPath()), Type: c.Type, Mode: c.Mode, SHA: c.SHA, Size: c.Size})
			}
		}
	}
	return &gh.Tree{SHA: t.SHA, Entries: entries}
}

func blobSHA(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:40]
}

func (s *Server) getTree(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	var found *gh.Tree
	foundDir := ""
	empty := true
	if r != nil {
		id := r.GetID()
		empty = len(s.commits[id]) == 0
		ref := req.PathValue("ref")
		if root := s.tree(id, ""); ref == "HEAD" || ref == r.GetDefaultBranch() || ref == root.GetSHA() {
			found = root
		} else {
			for p := range s.files[id] {
				for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
					if t := s.tree(id, dir); t.GetSHA() == ref {
						found, foundDir = t, dir
					}
				}
			}
		}
		if found != nil && req.URL.Query().Get("recursive") != "" {
			found = s.flatten(id, foundDir, found)
		}
	}
	s.mu.Unlock()
	switch {
	case r == nil:
		writeError(w, http.StatusNotFound, "Not Found")
	case empty:
		writeError(w, http.StatusConflict, "Git Repository is empty.")
	case found == nil:
		writeError(w, http.StatusNotFound, "Not Found")
	default:
		s.respond(w, req, cred, found)
	}
}

func (s *Server) getBlob(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	content, ok := "", false
	if r != nil {
		for _, c := range s.files[r.GetID()] {
			if blobSHA(c) == req.PathValue("sha") {
				content, ok = c, true
			}
		}
	}
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	if strings.Contains(req.Header.Get("Accept"), "raw") {
		w.Header().Set("Content-Type", "application/vnd.github.raw")
		_, _ = w.Write([]byte(content))
		return
	}
	s.respond(w, req, cred, map[string]any{"sha": req.PathValue("sha"), "size": len(content), "encoding": "base64",
		"content": base64.StdEncoding.EncodeToString([]byte(content))})
}
