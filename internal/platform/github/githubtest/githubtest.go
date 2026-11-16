// Package githubtest is a fake GitHub REST server for tests: App and token authentication, organisation and user account listings
// with pagination, single repositories, rate-limit headers, ETags and 304s, fault injection and a request ledger.
package githubtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	tokens   map[string]string         // accepted personal tokens -> the account login they belong to ("" for none)
	reach    map[string]map[int64]bool // a personal token -> the only repositories it reads (Reach); absent: every one
	expiry   map[string]time.Time      // a personal token -> its end, sent in GitHub's header (Expires)
	asked    map[string][]string       // a personal token with a login or a reach -> what was asked with it (Asked)
	users    map[string]bool           // lower-case logins of user accounts; every other owner is an organisation
	installs map[int64]string          // installation id -> account login
	grants   map[string]grant          // by account login (lower case); absent: every permission and event
	selected map[string]map[int64]bool // by account login (lower case): the repositories its installation was given (Select); absent: all
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
	s := &Server{AppID: 42, key: key, tokens: map[string]string{}, reach: map[string]map[int64]bool{}, expiry: map[string]time.Time{}, asked: map[string][]string{}, users: map[string]bool{}, installs: map[int64]string{}, grants: map[string]grant{}, selected: map[string]map[int64]bool{}, t: t, issued: map[string]string{},
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

// Reach limits what the personal token tok reads to the repositories ids, as a person's token reads only what GitHub lets that person
// read: listings leave the others out and every /repos/{owner}/{name} call of another answers 404. Without it a token reads every one.
func (s *Server) Reach(tok string, ids ...int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reach[tok] = map[int64]bool{}
	for _, id := range ids {
		s.reach[tok][id] = true
	}
}

// Asked is every request made with the personal token tok (one with a login or a reach), as "METHOD /path status".
func (s *Server) Asked(tok string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.asked[tok])
}

// Expires makes the personal token tok end at end: every answer to it carries GitHub's GitHub-Authentication-Token-Expiration header,
// and after end it is refused (401).
func (s *Server) Expires(tok string, end time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expiry[tok] = end
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

// Uninstall removes the App's installation on login: it is no longer listed and its tokens read nothing.
func (s *Server) Uninstall(login string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, l := range s.installs {
		if strings.EqualFold(l, login) {
			delete(s.installs, id)
		}
	}
	for tok, l := range s.issued {
		if strings.EqualFold(l, login) {
			delete(s.issued, tok)
		}
	}
}

// Select gives the App's installation on login only the repositories ids ("Only select repositories"; none at all with no ids):
// /installation/repositories lists only them and the installation listing says "selected". Without it, an installation has all.
func (s *Server) Select(login string, ids ...int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sel := map[int64]bool{}
	for _, id := range ids {
		sel[id] = true
	}
	s.selected[strings.ToLower(login)] = sel
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
			if tok := bearer(req); s.tokens[tok] != "" || s.reach[tok] != nil {
				s.asked[tok] = append(s.asked[tok], fmt.Sprintf("%s %s %d", req.Method, req.URL.Path, rec.status))
			}
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
