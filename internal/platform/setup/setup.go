// Package setup is the personal edition's first-run page (design/personal.md): in the browser the person signs in to GitHub, chooses
// the accounts, and sorts their repositories into products. It serves a loopback listener only, behind a one-use code, and gives
// what was chosen to Config.Finish, which writes the files; it holds the token in memory until then and never sends it to the page.
package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/ghlogin"
)

//go:embed assets/*
var assets embed.FS

// Limits the configuration allows, kept here so the page cannot make a file the portal refuses.
const (
	MaxProducts = 50
	MaxMatchers = 64 // matchers of a group
	MaxRules    = 20 // rules a person writes for one product
	MaxHidden   = 64 // matchers of a source's exclude
	maxBody     = 256 << 10
)

var (
	namePattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	loginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
)

// Product is a group the person made: a name and the repositories (by name) in it.
type Product struct {
	Name  string   `json:"name"`
	Repos []string `json:"repos"`
	// Fixed: no rules are derived, the product holds exactly its repositories (and the person's own rules).
	Fixed bool `json:"fixed,omitempty"`
	// Rules are the person's own rules for the product (kind and value); the others are derived from Repos (Derive).
	Rules []Rule `json:"rules,omitempty"`
	// Matchers is what the configuration gets for the product (set when the person has finished): rules, then the pinned repositories.
	Matchers []Rule `json:"-"`
}

// Result is what the person chose.
type Result struct {
	Host       ghlogin.Host
	Token      ghlogin.Token
	User       string
	Accounts   []string
	Products   []Product
	Hidden     []string // repository names kept out of the portal
	Unassigned []string // the listed repositories of the chosen accounts that no product holds and that are not hidden
	Default    string   // the repository pushed to last, for groups that need a default
	// Apps are the GitHub Apps the person made and installed, for chosen accounts: events instead of polling.
	Apps []AppKey
}

// Config configures the setup server.
type Config struct {
	Addr     string // the listener's host:port: the Host header of every request must be it, or localhost on its port
	Code     string // the one-use code that opens the page
	Host     string // the host text the page starts with
	ClientID string // the client id of the app for the code sign-in on Host; empty: the person pastes a token
	Scope    string // OAuth scopes asked with the code
	// CLIToken returns the token the GitHub CLI holds for a host name (github.com, github.example.org); nil: the page does not offer it.
	CLIToken func(ctx context.Context, hostname string) (string, error)
	HTTP     *http.Client
	Now      func() time.Time
	// Finish writes the person's choices and returns the one-use path that signs them in to the running portal.
	Finish func(ctx context.Context, r Result) (launch string, err error)
}

// Server is the setup page's state and handler.
type Server struct {
	cfg     Config
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	done    chan struct{}
	session string // the token the page sends in X-Setup-Token

	mu        sync.Mutex
	entered   bool
	hostText  string
	host      ghlogin.Host
	hostSet   bool
	clientID  string
	flow      flow
	gen       int // bumped when the sign-in is reset: a late result of an older one is dropped
	token     ghlogin.Token
	user      string
	orgs      []string
	repos     []ghlogin.Repo
	more      bool
	listed    bool
	listedFor []string // the accounts s.repos was read for, sorted
	job       *listing
	apps      map[string]*appRec    // by lower-case account
	pending   map[string]pendingApp // manifests posted to GitHub, by state
	jobs      int
	launch    string
	finished  bool
}

type flow struct {
	State    string `json:"state"` // "", pending, failed
	UserCode string `json:"userCode,omitempty"`
	URI      string `json:"verificationUri,omitempty"`
	Error    string `json:"error,omitempty"`
}

// New returns a setup server. Close stops its background sign-in.
func New(cfg Config) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	if cfg.Scope == "" {
		cfg.Scope = "repo read:org"
	}
	return &Server{cfg: cfg, ctx: ctx, cancel: cancel, done: make(chan struct{}), session: base64.RawURLEncoding.EncodeToString(b),
		hostText: cfg.Host, clientID: cfg.ClientID, pending: map[string]pendingApp{}}
}

// Done is closed once the person has finished and Config.Finish succeeded.
func (s *Server) Done() <-chan struct{} { return s.done }

// Close stops the sign-in in progress and waits for it.
func (s *Server) Close() {
	s.cancel()
	s.wg.Wait()
}

// Handler is the setup page.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(assets, "assets")
	files := http.FileServerFS(sub)
	mux.HandleFunc("GET /setup/{$}", s.page)
	mux.Handle("GET /setup/app.js", http.StripPrefix("/setup/", files))
	mux.Handle("GET /setup/app.css", http.StripPrefix("/setup/", files))
	mux.Handle("GET /setup/callback.js", http.StripPrefix("/setup/", files))
	mux.HandleFunc("GET "+callbackPath, s.appCallback)
	mux.Handle("POST /setup/api/app/start", s.api(s.startApp))
	mux.Handle("POST /setup/api/app/check", s.api(s.checkApp))
	mux.Handle("POST /setup/api/app/forget", s.api(s.forgetApp))
	mux.Handle("GET /setup/api/state", s.api(s.state))
	mux.Handle("POST /setup/api/host", s.api(s.setHost))
	mux.Handle("POST /setup/api/device", s.api(s.startDevice))
	mux.Handle("POST /setup/api/token", s.api(s.pasteToken))
	mux.Handle("POST /setup/api/cli", s.api(s.cliLogin))
	mux.Handle("POST /setup/api/signout", s.api(s.signOut))
	mux.Handle("POST /setup/api/list", s.api(s.startListing))
	mux.Handle("POST /setup/api/list/cancel", s.api(s.cancelListing))
	mux.Handle("POST /setup/api/repos", s.api(s.listRepos))
	mux.Handle("POST /setup/api/rules", s.api(s.previewRules))
	mux.Handle("POST /setup/api/rule-test", s.api(s.testRule))
	mux.Handle("POST /setup/api/finish", s.api(s.finish))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/setup/", http.StatusSeeOther) })
	return s.guard(mux)
}

// guard checks the Host (against DNS rebinding) and sets the headers every answer carries.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		s.mu.Lock()
		s.ensureHost()
		forms := "'self'"
		if s.hostSet { // the manifest is posted to the host the person signed in to
			forms += " " + s.host.Web
		}
		s.mu.Unlock()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; "+
			"base-uri 'none'; form-action "+forms+"; frame-ancestors 'none'")
		if !s.hostOK(r.Host) {
			http.Error(w, "This address is not the setup page's.", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hostOK(host string) bool {
	if host == s.cfg.Addr {
		return true
	}
	_, port, ok := strings.Cut(s.cfg.Addr, ":")
	if i := strings.LastIndex(s.cfg.Addr, ":"); i >= 0 {
		port, ok = s.cfg.Addr[i+1:], true
	}
	return ok && host == "localhost:"+port
}

// page serves the page. The page itself is the same for everyone and holds nothing secret; the session token in it is given once, to
// whoever opens the link with the one-use code. The browser keeps the token in the tab's session storage (per origin, so not per host
// only, as a cookie is: another server on this computer, on another port, never gets it) and sends it as a header with every call.
func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	b, _ := assets.ReadFile("assets/index.html")
	page := string(b)
	if code := r.URL.Query().Get("code"); code != "" {
		s.mu.Lock()
		ok := !s.entered && subtle.ConstantTimeCompare([]byte(code), []byte(s.cfg.Code)) == 1
		if ok {
			s.entered = true
		}
		s.mu.Unlock()
		if ok {
			page = strings.Replace(page, `<meta name="setup-token" content="">`, `<meta name="setup-token" content="`+s.session+`">`, 1)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, page)
}

// signedIn is whether the request carries the session token.
func (s *Server) signedIn(r *http.Request) bool {
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Setup-Token")), []byte(s.session)) == 1
}

type problem struct {
	Status int
	Msg    string
}

func (p problem) Error() string { return p.Msg }

// api wraps a JSON handler: the cookie, the same origin for a POST, a bounded JSON body, and the answer or the problem as JSON.
func (s *Server) api(h func(ctx context.Context, body []byte) (any, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fail := func(status int, msg string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
		}
		if !s.signedIn(r) {
			fail(http.StatusForbidden, "Open the page with the link from the terminal (it works once, in one tab).")
			return
		}
		var body []byte
		if r.Method == http.MethodPost {
			if site := r.Header.Get("Sec-Fetch-Site"); site != "same-origin" && (site != "" || r.Header.Get("Origin") != "http://"+r.Host) {
				fail(http.StatusForbidden, "Use the setup page itself.")
				return
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				fail(http.StatusUnsupportedMediaType, "Send JSON.")
				return
			}
			var err error
			if body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody)); err != nil {
				fail(http.StatusRequestEntityTooLarge, "That is too much.")
				return
			}
		}
		out, err := h(r.Context(), body)
		var p problem
		switch {
		case errors.As(err, &p):
			fail(p.Status, p.Msg)
		case err != nil:
			fail(http.StatusBadGateway, err.Error())
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
		}
	})
}

func bad(format string, a ...any) error {
	return problem{http.StatusBadRequest, fmt.Sprintf(format, a...)}
}

func decode(body []byte, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return bad("That is not what the page sends.")
	}
	return nil
}

type stateOut struct {
	Host     string   `json:"host"` // the host text
	Web      string   `json:"web"`  // the host's web address, when it is valid
	HostOK   bool     `json:"hostOk"`
	CanCLI   bool     `json:"canCli"`   // the GitHub CLI's login can be used
	CanCode  bool     `json:"canCode"`  // a client id is known: the code sign-in is offered
	ClientID string   `json:"clientId"` // the id in use (not a secret)
	Flow     flow     `json:"flow"`
	User     string   `json:"user,omitempty"`
	Orgs     []string `json:"orgs,omitempty"`
	Ends     string   `json:"ends,omitempty"` // when the token ends, if known
	Done     bool     `json:"done,omitempty"`
	Launch   string   `json:"launch,omitempty"`
	Job      *jobOut  `json:"job,omitempty"`  // the repositories being read
	Apps     []appOut `json:"apps,omitempty"` // the GitHub Apps made for accounts
}

func (s *Server) snapshot() stateOut {
	out := stateOut{Host: s.hostText, HostOK: s.hostSet, CanCLI: s.cfg.CLIToken != nil, CanCode: s.hostSet && s.clientID != "", ClientID: s.clientID, Flow: s.flow,
		User: s.user, Orgs: s.orgs, Done: s.finished, Launch: s.launch, Job: s.job.out(), Apps: s.appsOut()}
	if s.hostSet {
		out.Web = s.host.Web
	}
	if !s.token.Expires.IsZero() {
		out.Ends = s.token.Expires.UTC().Format(time.RFC3339)
	}
	return out
}
