// Package githubtest is a fake GitHub REST server for tests: App and token authentication, organisation and user account listings
// with pagination, single repositories, rate-limit headers, ETags and 304s, fault injection and a request ledger.
package githubtest

import (
	"cmp"
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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gh "github.com/google/go-github/v93/github"
)

// Limit is the core rate limit of every credential.
const Limit = 5000

// Server is a fake GitHub. Its methods are safe for concurrent use.
type Server struct {
	*httptest.Server
	AppID int64

	deliveries []Delivery // the App's webhook delivery log, newest first

	mu       sync.Mutex
	key      *rsa.PrivateKey
	tokens   map[string]string // accepted personal tokens -> the account login they belong to ("" for none)
	users    map[string]bool   // lower-case logins of user accounts; every other owner is an organisation
	installs map[int64]string  // installation id -> account login
	grants   map[string]grant  // by account login (lower case); absent: every permission and event
	t        testing.TB
	issued   map[string]string // installation token -> account login
	orgs     map[string][]*gh.Repository
	commits  map[int64][]Commit            // by repository id, newest first
	branches map[int64]map[string][]Commit // by repository id: branch name -> its own commits, newest first
	tags     map[int64]map[string]string   // by repository id: tag name -> commit SHA
	files    map[int64]map[string]string   // by repository id: path -> content
	releases map[int64][]*gh.RepositoryRelease
	pulls    map[int64][]Pull // by repository id
	issues   map[int64][]Issue
	treeMax  int // entries of a recursive tree before it is truncated, as GitHub's 100,000; 0: never
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
	s := &Server{AppID: 42, key: key, tokens: map[string]string{}, users: map[string]bool{}, installs: map[int64]string{}, grants: map[string]grant{}, t: t, issued: map[string]string{},
		orgs: map[string][]*gh.Repository{}, commits: map[int64][]Commit{}, branches: map[int64]map[string][]Commit{},
		tags: map[int64]map[string]string{}, files: map[int64]map[string]string{},
		releases: map[int64][]*gh.RepositoryRelease{}, pulls: map[int64][]Pull{}, issues: map[int64][]Issue{}, nextID: 1000, used: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /app/installations", s.listInstallations)
	mux.HandleFunc("GET /app/hook/deliveries", s.listDeliveries)
	mux.HandleFunc("POST /app/installations/{id}/access_tokens", s.accessToken)
	mux.HandleFunc("GET /orgs/{org}/repos", s.listRepos)
	mux.HandleFunc("GET /users/{user}", s.getUser)
	mux.HandleFunc("GET /user", s.getUser)
	mux.HandleFunc("GET /users/{user}/repos", s.listRepos)
	mux.HandleFunc("GET /user/repos", s.listRepos)
	mux.HandleFunc("GET /installation/repositories", s.listRepos)
	mux.HandleFunc("GET /repos/{owner}/{name}", s.getRepo)
	mux.HandleFunc("GET /repos/{owner}/{name}/commits", s.listCommits)
	mux.HandleFunc("GET /repos/{owner}/{name}/branches", s.listRefs)
	mux.HandleFunc("GET /repos/{owner}/{name}/tags", s.listRefs)
	mux.HandleFunc("GET /repos/{owner}/{name}/git/trees/{ref}", s.getTree)
	mux.HandleFunc("GET /repos/{owner}/{name}/git/blobs/{sha}", s.getBlob)
	mux.HandleFunc("GET /repos/{owner}/{name}/releases", s.listReleases)
	mux.HandleFunc("GET /repos/{owner}/{name}/pulls", s.listPulls)
	mux.HandleFunc("GET /repos/{owner}/{name}/issues", s.listIssues)
	mux.HandleFunc("POST /graphql", s.graphQL)
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
func (s *Server) AddToken(tok string) { s.AddUserToken(tok, "") }

// AddUserToken accepts tok as a personal access token of the user account login.
func (s *Server) AddUserToken(tok, login string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[tok] = login
}

// AddUser makes login a user account (owners are organisations otherwise): GET /orgs/{login}/repos answers 404, its repositories
// list at /users/{login}/repos (public ones), /user/repos (for its own token) and /installation/repositories.
func (s *Server) AddUser(login string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[strings.ToLower(login)] = true
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

// Heal drops every injected fault.
func (s *Server) Heal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = nil
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
		if !readOnly(req) {
			// The portal is read-only towards GitHub (P-71): a write fails the test that sent it.
			s.t.Errorf("githubtest: the portal sent a write to GitHub: %s", line)
			writeError(rec, http.StatusForbidden, "the portal must not write to GitHub")
			return
		}
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

// readOnly reports whether req only reads: a GET or HEAD, a GraphQL query (checked for mutations where it is answered), or the
// App's request for an installation token.
func readOnly(req *http.Request) bool {
	switch {
	case req.Method == http.MethodGet || req.Method == http.MethodHead:
		return true
	case req.Method != http.MethodPost:
		return false
	case req.URL.Path == "/graphql":
		return true
	}
	return strings.HasPrefix(req.URL.Path, "/app/installations/") && strings.HasSuffix(req.URL.Path, "/access_tokens")
}

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
		out = append(out, &gh.Installation{ID: new(id), Account: &gh.User{Login: new(s.installs[id]), Type: new(kind)}, Permissions: &perms,
			Events: slices.Clone(g.events)})
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
	repos = slices.DeleteFunc(slices.Clone(repos), func(r *gh.Repository) bool { return public && r.GetVisibility() == "private" })
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
	if login == "" {
		login = s.tokens[bearer(req)]
	}
	_, known := s.orgs[strings.ToLower(login)]
	user := s.users[strings.ToLower(login)]
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
	Parents                               []string // nil: the next commit of the list (a linear history), none for the last
	// Additions, Deletions and Files are its diff stats, answered by POST /graphql; all zero: GitHub answers null for it.
	Additions, Deletions, Files int
}

// SetCommits replaces the default-branch commits of the repository with id (newest first). No commits means an empty repository.
func (s *Server) SetCommits(id int64, cs ...Commit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits[id] = cs
}

// SetBranch sets a branch of the repository with id to its own commits, newest first; the last one names its parents in the
// default branch (or another branch). No commits deletes the branch.
func (s *Server) SetBranch(id int64, name string, cs ...Commit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.branches[id] == nil {
		s.branches[id] = map[string][]Commit{}
	}
	if len(cs) == 0 {
		delete(s.branches[id], name)
		return
	}
	s.branches[id][name] = cs
}

// SetTag points a tag of the repository with id at a commit; an empty SHA deletes it.
func (s *Server) SetTag(id int64, name, sha string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tags[id] == nil {
		s.tags[id] = map[string]string{}
	}
	if sha == "" {
		delete(s.tags[id], name)
		return
	}
	s.tags[id][name] = sha
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

// SetTreeLimit makes a recursive tree of more than n entries come truncated, its first n entries only, as GitHub truncates one past
// 100,000 entries or 7 MB; 0 lifts the limit.
func (s *Server) SetTreeLimit(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.treeMax = n
}

// Release is a release as GitHub lists it (SetReleases).
type Release struct {
	Tag, Name, Body, Author string
	Published               time.Time
	Pre, Draft              bool
}

// SetRelease sets the one release of the repository with id; an empty tag removes it.
func (s *Server) SetRelease(id int64, tag, name string, published time.Time) {
	if tag == "" {
		s.SetReleases(id)
		return
	}
	s.SetReleases(id, Release{Tag: tag, Name: name, Published: published})
}

// SetReleases sets the releases of the repository with id, replacing its earlier ones; GitHub lists them newest first.
func (s *Server) SetReleases(id int64, rels ...Release) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]*gh.RepositoryRelease, 0, len(rels))
	for _, r := range rels {
		x := &gh.RepositoryRelease{TagName: r.Tag, Name: new(r.Name), Body: new(r.Body), Prerelease: r.Pre, Draft: r.Draft,
			PublishedAt: &gh.Timestamp{Time: r.Published}, HTMLURL: fmt.Sprintf("https://github.com/releases/%d/%s", id, r.Tag),
			Assets: []*gh.ReleaseAsset{{Name: new("asset")}}}
		if r.Author != "" {
			x.Author = &gh.User{Login: new(r.Author)}
		}
		list = append(list, x)
	}
	slices.SortStableFunc(list, func(x, y *gh.RepositoryRelease) int { return y.GetPublishedAt().Compare(x.GetPublishedAt().Time) })
	if len(list) == 0 {
		delete(s.releases, id)
		return
	}
	s.releases[id] = list
}

func (s *Server) listReleases(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	var rels []*gh.RepositoryRelease
	if r != nil {
		rels = s.releases[r.GetID()]
	}
	s.mu.Unlock()
	if r == nil {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	if n, err := strconv.Atoi(req.URL.Query().Get("per_page")); err == nil && n > 0 && n < len(rels) {
		rels = rels[:n]
	}
	s.respond(w, req, cred, append([]*gh.RepositoryRelease{}, rels...))
}

// lookup returns the repository at owner/name. Callers hold s.mu.
// ID is the id of the repository owner/name, 0 when there is none.
func (s *Server) ID(owner, name string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookup(owner, name).GetID()
}

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
	ref := req.URL.Query().Get("sha")
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	var cs []Commit
	if r != nil {
		cs = s.history(r, ref)
	}
	s.mu.Unlock()
	switch {
	case r == nil:
		writeError(w, http.StatusNotFound, "Not Found")
		return
	case len(cs) == 0 && ref == "":
		writeError(w, http.StatusConflict, "Git Repository is empty.")
		return
	case len(cs) == 0:
		writeError(w, http.StatusNotFound, "No commit found for SHA: "+ref)
		return
	}
	lo, hi := s.page(w, req, len(cs))
	out := make([]*gh.RepositoryCommit, 0, hi-lo)
	for _, c := range cs[lo:hi] {
		date := &gh.Timestamp{Time: c.Date}
		rc := &gh.RepositoryCommit{SHA: new(c.SHA), HTMLURL: new(r.GetHTMLURL() + "/commit/" + c.SHA),
			Commit: &gh.Commit{Message: new(c.Message), Author: &gh.CommitAuthor{Name: new(c.AuthorName), Date: date},
				Committer: &gh.CommitAuthor{Name: new(c.AuthorName), Date: date}}}
		for _, p := range c.Parents {
			rc.Parents = append(rc.Parents, &gh.Commit{SHA: new(p)})
		}
		if c.AuthorLogin != "" {
			rc.Author = &gh.User{Login: new(c.AuthorLogin)}
		}
		out = append(out, rc)
	}
	s.respond(w, req, cred, out)
}

// page reads per_page (default 30, at most 100) and page, sets the Link header to the next page and returns the bounds of n items.
func (s *Server) page(w http.ResponseWriter, req *http.Request, n int) (lo, hi int) {
	per, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
	if per <= 0 || per > 100 {
		per = 30
	}
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	page = max(page, 1)
	lo, hi = min((page-1)*per, n), min(page*per, n)
	if hi < n {
		next := *req.URL
		q := next.Query()
		q.Set("page", strconv.Itoa(page+1))
		next.RawQuery = q.Encode()
		w.Header().Set("Link", fmt.Sprintf(`<%s%s>; rel="next"`, s.URL, next.RequestURI()))
	}
	return lo, hi
}

// history is the commits reachable from ref, newest first, each with its parents filled in. The default branch ("" or its name)
// lists in the order SetCommits gave; another branch, a tag or a SHA walks the parents and orders by date. Callers hold s.mu.
func (s *Server) history(r *gh.Repository, ref string) []Commit {
	id := r.GetID()
	all := map[string]Commit{}
	line := func(cs []Commit) []Commit {
		out := slices.Clone(cs)
		for i := range out {
			if out[i].Parents == nil && i+1 < len(out) {
				out[i].Parents = []string{out[i+1].SHA}
			}
			all[out[i].SHA] = out[i]
		}
		return out
	}
	main := line(s.commits[id])
	heads := map[string]string{}
	for name, cs := range s.branches[id] {
		heads[name] = line(cs)[0].SHA
	}
	if ref == "" || ref == r.GetDefaultBranch() {
		return main
	}
	head, ok := heads[ref]
	if !ok {
		head, ok = s.tags[id][ref]
	}
	if !ok {
		head = ref
	}
	var out []Commit
	seen := map[string]bool{}
	for stack := []string{head}; len(stack) > 0; {
		sha := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		c, known := all[sha]
		if !known || seen[sha] {
			continue
		}
		seen[sha] = true
		out = append(out, c)
		stack = append(stack, c.Parents...)
	}
	slices.SortStableFunc(out, func(a, b Commit) int {
		if c := b.Date.Compare(a.Date); c != 0 {
			return c
		}
		return strings.Compare(b.SHA, a.SHA)
	})
	return out
}

// listRefs answers GET /repos/{owner}/{name}/branches (the default branch first, then by name) and .../tags (by name, descending).
func (s *Server) listRefs(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	type ref struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	var refs []ref
	add := func(name, sha string) {
		var x ref
		x.Name, x.Commit.SHA = name, sha
		refs = append(refs, x)
	}
	if r != nil {
		id := r.GetID()
		if strings.HasSuffix(req.URL.Path, "/branches") {
			if cs := s.commits[id]; len(cs) > 0 {
				add(r.GetDefaultBranch(), cs[0].SHA)
			}
			for _, name := range slices.Sorted(maps.Keys(s.branches[id])) {
				add(name, s.branches[id][name][0].SHA)
			}
		} else {
			for _, name := range slices.Backward(slices.Sorted(maps.Keys(s.tags[id]))) {
				add(name, s.tags[id][name])
			}
		}
	}
	s.mu.Unlock()
	if r == nil {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	lo, hi := s.page(w, req, len(refs))
	s.respond(w, req, cred, refs[lo:hi])
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
			if s.treeMax > 0 && len(found.Entries) > s.treeMax {
				found.Entries, found.Truncated = found.Entries[:s.treeMax], new(true)
			}
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

// graphAlias is one `alias:object(oid:"sha")` of a GraphQL query, as the commits feature asks diff stats.
var graphAlias = regexp.MustCompile(`(\w+):object\(oid:"([0-9a-f]{40,64})"\)`)

// graphQL answers the one GraphQL query the portal sends: a repository's commits by object id, each with its diff stats.
func (s *Server) graphQL(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	var body struct {
		Query     string            `json:"query"`
		Variables map[string]string `json:"variables"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Problems parsing JSON")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if q := strings.TrimSpace(body.Query); strings.HasPrefix(q, "mutation") {
		s.t.Errorf("githubtest: the portal sent a GraphQL mutation (P-71): %.80s", q)
		writeError(w, http.StatusForbidden, "the portal must not write to GitHub")
		return
	}
	r := s.lookup(body.Variables["owner"], body.Variables["name"])
	if r == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": nil},
			"errors": []map[string]string{{"type": "NOT_FOUND", "message": "Could not resolve to a Repository"}}})
		return
	}
	all := map[string]Commit{}
	for _, c := range s.commits[r.GetID()] {
		all[c.SHA] = c
	}
	for _, cs := range s.branches[r.GetID()] {
		for _, c := range cs {
			all[c.SHA] = c
		}
	}
	repo := map[string]any{}
	s.answerPulls(r.GetID(), body.Query, repo)
	for _, m := range graphAlias.FindAllStringSubmatch(body.Query, -1) {
		c, ok := all[m[2]]
		if !ok || c.Additions+c.Deletions+c.Files == 0 {
			repo[m[1]] = nil
			continue
		}
		repo[m[1]] = map[string]int{"additions": c.Additions, "deletions": c.Deletions, "changedFilesIfAvailable": c.Files}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": repo}})
}

// Pull is an open pull request of the fake: what the REST listing gives and what POST /graphql answers for it.
type Pull struct {
	Number                      int
	Title, Author               string
	Bot, Draft, Fork            bool // Bot: the author's account type is Bot
	Created, Updated            time.Time
	Base, Head, HeadSHA         string
	Labels                      []string
	Requested                   int      // people asked to review, named reviewer0, reviewer1, ... after Reviewers
	Reviewers                   []string // people asked to review, by login
	Review, Mergeable, Checks   string   // GraphQL's reviewDecision, mergeable and the head's statusCheckRollup state
	Approved, Changes           bool     // a reviewer's latest opinion
	Additions, Deletions, Files int
}

// SetPulls replaces the open pull requests of the repository id.
func (s *Server) SetPulls(id int64, ps ...Pull) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pulls[id] = slices.Clone(ps)
}

func (s *Server) listPulls(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	var ps []Pull
	if r != nil {
		ps = slices.Clone(s.pulls[r.GetID()])
	}
	s.mu.Unlock()
	if r == nil {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	slices.SortStableFunc(ps, func(a, b Pull) int { return b.Updated.Compare(a.Updated) })
	n, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
	if n <= 0 || n > 100 {
		n = 30
	}
	ps = ps[:min(n, len(ps))]
	full := r.GetOwner().GetLogin() + "/" + r.GetName()
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		typ := "User"
		if p.Bot {
			typ = "Bot"
		}
		head := full
		if p.Fork {
			head = "someone/" + r.GetName()
		}
		labels := []map[string]string{}
		for _, l := range p.Labels {
			labels = append(labels, map[string]string{"name": l, "color": "0e8a16"})
		}
		reviewers := make([]map[string]string, 0, max(p.Requested, len(p.Reviewers)))
		for _, l := range p.Reviewers {
			reviewers = append(reviewers, map[string]string{"login": l})
		}
		for i := 0; len(reviewers) < p.Requested; i++ {
			reviewers = append(reviewers, map[string]string{"login": fmt.Sprintf("reviewer%d", i)})
		}
		out = append(out, map[string]any{"number": p.Number, "title": p.Title, "draft": p.Draft,
			"html_url":   fmt.Sprintf("https://github.com/%s/pull/%d", full, p.Number),
			"user":       map[string]string{"login": p.Author, "type": typ},
			"created_at": p.Created.UTC().Format(time.RFC3339), "updated_at": p.Updated.UTC().Format(time.RFC3339),
			"base":   map[string]any{"ref": p.Base, "repo": map[string]string{"full_name": full}},
			"head":   map[string]any{"ref": p.Head, "sha": p.HeadSHA, "repo": map[string]string{"full_name": head}},
			"labels": labels, "requested_reviewers": reviewers, "requested_teams": []any{}, "body": "not read"})
	}
	s.respond(w, req, cred, out)
}

// Issue is an open issue of the fake.
type Issue struct {
	Number           int
	Title, Author    string
	Bot              bool // the author's account type is Bot
	Created, Updated time.Time
	Labels           []string
	Assignees        []string
	Comments         int
	Milestone, Type  string
}

// SetIssues replaces the open issues of the repository id.
func (s *Server) SetIssues(id int64, is ...Issue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issues[id] = slices.Clone(is)
}

// listIssues is GitHub's listing of open issues: the issues and the open pull requests together, the most recently updated first,
// in pages; a pull request carries `pull_request`.
func (s *Server) listIssues(w http.ResponseWriter, req *http.Request) {
	cred := s.charge(w, req)
	if cred == "" {
		return
	}
	s.mu.Lock()
	r := s.lookup(req.PathValue("owner"), req.PathValue("name"))
	var items []map[string]any
	var updated []time.Time
	if r != nil {
		full := r.GetOwner().GetLogin() + "/" + r.GetName()
		user := func(login string, bot bool) map[string]string {
			if bot {
				return map[string]string{"login": login, "type": "Bot"}
			}
			return map[string]string{"login": login, "type": "User"}
		}
		for _, p := range s.pulls[r.GetID()] {
			items = append(items, map[string]any{"number": p.Number, "title": p.Title, "user": user(p.Author, p.Bot),
				"html_url": fmt.Sprintf("https://github.com/%s/pull/%d", full, p.Number), "labels": []any{}, "assignees": []any{},
				"created_at": p.Created.UTC().Format(time.RFC3339), "updated_at": p.Updated.UTC().Format(time.RFC3339),
				"pull_request": map[string]string{"url": "https://api.github.com/repos/" + full + "/pulls/" + strconv.Itoa(p.Number)}})
			updated = append(updated, p.Updated)
		}
		for _, is := range s.issues[r.GetID()] {
			labels := []map[string]string{}
			for _, l := range is.Labels {
				labels = append(labels, map[string]string{"name": l, "color": "d73a4a"})
			}
			assignees := []map[string]string{}
			for _, a := range is.Assignees {
				assignees = append(assignees, map[string]string{"login": a})
			}
			it := map[string]any{"number": is.Number, "title": is.Title, "user": user(is.Author, is.Bot),
				"html_url": fmt.Sprintf("https://github.com/%s/issues/%d", full, is.Number), "labels": labels, "assignees": assignees,
				"comments": is.Comments, "created_at": is.Created.UTC().Format(time.RFC3339), "updated_at": is.Updated.UTC().Format(time.RFC3339),
				"body": "not read", "milestone": nil, "type": nil}
			if is.Milestone != "" {
				it["milestone"] = map[string]any{"title": is.Milestone, "number": 1}
			}
			if is.Type != "" {
				it["type"] = map[string]any{"name": is.Type, "id": 1}
			}
			items = append(items, it)
			updated = append(updated, is.Updated)
		}
	}
	s.mu.Unlock()
	if r == nil {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return updated[b].Compare(updated[a]) })
	lo, hi := s.page(w, req, len(items))
	out := make([]map[string]any, 0, hi-lo)
	for _, i := range order[lo:hi] {
		out = append(out, items[i])
	}
	s.respond(w, req, cred, out)
}

var (
	pullAlias  = regexp.MustCompile(`(\w+):pullRequest\(number:(\d+)\)`)
	totalAlias = regexp.MustCompile(`(\w+):pullRequests\(states:OPEN[^)]*\)\{totalCount\}`)
)

// answerPulls answers a query's pull requests by number and its open count. Callers hold s.mu.
func (s *Server) answerPulls(id int64, query string, repo map[string]any) {
	for _, m := range totalAlias.FindAllStringSubmatch(query, -1) {
		repo[m[1]] = map[string]int{"totalCount": len(s.pulls[id])}
	}
	for _, m := range pullAlias.FindAllStringSubmatch(query, -1) {
		n, _ := strconv.Atoi(m[2])
		repo[m[1]] = nil
		for _, p := range s.pulls[id] {
			if p.Number != n {
				continue
			}
			var opinions []map[string]string
			if p.Approved {
				opinions = append(opinions, map[string]string{"state": "APPROVED"})
			}
			if p.Changes {
				opinions = append(opinions, map[string]string{"state": "CHANGES_REQUESTED"})
			}
			var rollup any
			if p.Checks != "" {
				rollup = map[string]string{"state": p.Checks}
			}
			var review any
			if p.Review != "" {
				review = p.Review
			}
			repo[m[1]] = map[string]any{"reviewDecision": review, "mergeable": cmp.Or(p.Mergeable, "MERGEABLE"),
				"additions": p.Additions, "deletions": p.Deletions, "changedFiles": p.Files,
				"latestOpinionatedReviews": map[string]any{"nodes": opinions},
				"commits":                  map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{"statusCheckRollup": rollup}}}}}
		}
	}
}

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

func (s *Server) listDeliveries(w http.ResponseWriter, req *http.Request) {
	if !s.verifyJWT(req) {
		writeError(w, http.StatusUnauthorized, "A JSON web token could not be decoded")
		return
	}
	s.mu.Lock()
	out := make([]map[string]any, 0, len(s.deliveries))
	for i, d := range s.deliveries {
		out = append(out, map[string]any{"id": i + 1, "guid": d.GUID, "event": d.Event, "action": d.Action, "repository_id": d.RepositoryID,
			"delivered_at": d.At.UTC().Format(time.RFC3339), "status_code": d.Status})
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}
