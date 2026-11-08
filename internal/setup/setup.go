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
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/ghlogin"
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

// ensureHost reads the host the page starts with (github.com when none was given). Callers hold s.mu.
func (s *Server) ensureHost() {
	if s.hostSet {
		return
	}
	if s.hostText == "" {
		s.hostText = "github.com"
	}
	if h, err := ghlogin.ParseHost(s.hostText); err == nil {
		s.host, s.hostSet = h, true
	}
}

func (s *Server) state(_ context.Context, _ []byte) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureHost()
	return s.snapshot(), nil
}

func (s *Server) client() *ghlogin.Client {
	return &ghlogin.Client{Host: s.host, HTTP: s.cfg.HTTP, Now: s.cfg.Now}
}

func (s *Server) reset() {
	s.gen++
	s.flow, s.token, s.user, s.orgs, s.repos, s.listed, s.more = flow{}, ghlogin.Token{}, "", nil, nil, false, false
	s.apps, s.pending = nil, map[string]pendingApp{}
	s.dropListing()
}

// dropListing stops the repository read in progress and forgets it. Callers hold s.mu.
func (s *Server) dropListing() {
	if s.job != nil {
		s.job.cancel()
	}
	s.job, s.listedFor = nil, nil
}

func (s *Server) setHost(_ context.Context, body []byte) (any, error) {
	var in struct {
		Host     string `json:"host"`
		ClientID string `json:"clientId"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	h, err := ghlogin.ParseHost(in.Host)
	if err != nil {
		return nil, bad("%v", err)
	}
	id := strings.TrimSpace(in.ClientID)
	if len(id) > 100 || strings.ContainsAny(id, " \t\r\n") {
		return nil, bad("A client id is one word.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reset()
	s.host, s.hostSet, s.hostText = h, true, strings.TrimSpace(in.Host)
	if s.hostText == "" {
		s.hostText = "github.com"
	}
	s.clientID = id
	if id == "" && h.Web == "https://github.com" {
		s.clientID = s.cfg.ClientID
	}
	return s.snapshot(), nil
}

func (s *Server) startDevice(ctx context.Context, _ []byte) (any, error) {
	s.mu.Lock()
	s.ensureHost()
	if !s.hostSet || s.clientID == "" {
		s.mu.Unlock()
		return nil, bad("There is no client id to sign in with; paste a token instead.")
	}
	c, id, scope := s.client(), s.clientID, s.cfg.Scope
	s.mu.Unlock()
	code, err := c.StartDevice(ctx, id, scope)
	if err != nil {
		return nil, bad("GitHub did not start the sign-in: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reset()
	s.flow = flow{State: "pending", UserCode: code.UserCode, URI: code.VerificationURI}
	gen := s.gen
	s.wg.Go(func() { s.wait(c, id, code, gen) })
	return s.snapshot(), nil
}

// wait polls until the person confirms; the token stays here.
func (s *Server) wait(c *ghlogin.Client, id string, code ghlogin.Code, gen int) {
	tok, err := c.WaitDevice(s.ctx, id, code)
	if err == nil {
		err = s.signedInWith(s.ctx, c, tok, gen)
		if err == nil {
			return
		}
	}
	msg := "The sign-in did not finish: " + err.Error()
	switch {
	case errors.Is(err, ghlogin.ErrDenied):
		msg = "The sign-in was cancelled on GitHub."
	case errors.Is(err, ghlogin.ErrExpired):
		msg = "The code ran out before it was confirmed."
	case s.ctx.Err() != nil:
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen == gen && s.flow.State == "pending" {
		s.flow = flow{State: "failed", Error: msg}
	}
}

// signedInWith checks the token and keeps it with who it is and the organisations it reaches.
func (s *Server) signedInWith(ctx context.Context, c *ghlogin.Client, tok ghlogin.Token, gen int) error {
	u, err := c.Whoami(ctx, tok.Value)
	if err != nil {
		return err
	}
	if tok.Expires.IsZero() {
		tok.Expires = u.Expires
	}
	orgs, err := c.Orgs(ctx, tok.Value)
	if err != nil {
		orgs = nil // the organisations are a convenience; the person's own account is enough
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		return nil // the person went back; this sign-in is no longer the one wanted
	}
	s.flow, s.repos, s.listed, s.more = flow{}, nil, false, false
	s.dropListing()
	s.token, s.user, s.orgs = tok, u.Login, orgs
	return nil
}

func (s *Server) pasteToken(ctx context.Context, body []byte) (any, error) {
	var in struct {
		Token string `json:"token"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	tok := strings.TrimSpace(in.Token)
	if tok == "" || len(tok) > 512 || strings.ContainsAny(tok, " \t\r\n") {
		return nil, bad("Paste the token, nothing else.")
	}
	s.mu.Lock()
	s.ensureHost()
	if !s.hostSet {
		s.mu.Unlock()
		return nil, bad("Choose the host first.")
	}
	c, gen := s.client(), s.gen
	s.mu.Unlock()
	if err := s.signedInWith(ctx, c, ghlogin.Token{Value: tok}, gen); err != nil {
		if errors.Is(err, ghlogin.ErrBadToken) {
			return nil, bad("GitHub does not accept this token.")
		}
		return nil, bad("Checking the token failed: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot(), nil
}

// cliLogin takes the token the GitHub CLI holds for the host and checks it like a pasted one. The token is read on this computer by
// this process and never leaves it for the page.
func (s *Server) cliLogin(ctx context.Context, _ []byte) (any, error) {
	if s.cfg.CLIToken == nil {
		return nil, bad("The GitHub CLI is not available.")
	}
	s.mu.Lock()
	s.ensureHost()
	if !s.hostSet {
		s.mu.Unlock()
		return nil, bad("Choose the host first.")
	}
	web := s.host.Web
	s.mu.Unlock()
	u, err := url.Parse(web)
	if err != nil || u.Hostname() == "" {
		return nil, bad("Choose the host first.")
	}
	tok, err := s.cfg.CLIToken(ctx, u.Hostname())
	tok = strings.TrimSpace(tok)
	if err != nil || tok == "" || len(tok) > 512 || strings.ContainsAny(tok, " \t\r\n") {
		return nil, bad("The GitHub CLI has no login for %s. Run `gh auth login --hostname %s` in a terminal, or use another way.", u.Hostname(), u.Hostname())
	}
	return s.pasteToken(ctx, mustJSON(map[string]string{"token": tok}))
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (s *Server) signOut(_ context.Context, _ []byte) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reset()
	return s.snapshot(), nil
}

type repoOut struct {
	Owner    string   `json:"owner"`
	Name     string   `json:"name"`
	Private  bool     `json:"private,omitempty"`
	Archived bool     `json:"archived,omitempty"`
	Topics   []string `json:"topics,omitempty"`
}

type reposOut struct {
	Repos     []repoOut `json:"repos"`
	More      bool      `json:"more,omitempty"`
	Suggested []Product `json:"suggested"`
	Limits    struct {
		Products int `json:"products"`
		Matchers int `json:"matchers"`
		Hidden   int `json:"hidden"`
	} `json:"limits"`
}

// chosen validates the accounts the person ticked against the ones the token has.
func (s *Server) chosen(accounts []string) ([]string, error) {
	var out []string
	for _, a := range accounts {
		if !loginPattern.MatchString(a) || a != s.user && !slices.Contains(s.orgs, a) {
			return nil, bad("%q is not one of your accounts.", a)
		}
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return nil, bad("Choose at least one account.")
	}
	return out, nil
}

// listRepos answers with the repositories the read (startListing) found, for the accounts asked for.
func (s *Server) listRepos(_ context.Context, body []byte) (any, error) {
	var in struct {
		Accounts []string `json:"accounts"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.token.Value == "" {
		s.mu.Unlock()
		return nil, bad("Sign in first.")
	}
	accounts, err := s.chosen(in.Accounts)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	slices.Sort(accounts)
	if !s.listed || !slices.Equal(s.listedFor, accounts) {
		s.mu.Unlock()
		return nil, bad("The repositories are not read yet.")
	}
	all, more := s.repos, s.more
	s.mu.Unlock()
	var mine []ghlogin.Repo
	for _, r := range all {
		if !r.Fork && slices.ContainsFunc(accounts, func(a string) bool { return strings.EqualFold(a, r.Owner) }) && namePattern.MatchString(r.Name) {
			mine = append(mine, r)
		}
	}
	mine = OnePerName(mine)
	out := reposOut{More: more, Suggested: Suggest(mine)}
	out.Limits.Products, out.Limits.Matchers, out.Limits.Hidden = MaxProducts, MaxMatchers, MaxHidden
	for _, r := range mine {
		out.Repos = append(out.Repos, repoOut{Owner: r.Owner, Name: r.Name, Private: r.Private, Archived: r.Archived, Topics: r.Topics[:min(len(r.Topics), 5)]})
	}
	return out, nil
}

func (s *Server) finish(ctx context.Context, body []byte) (any, error) {
	var in struct {
		Accounts []string  `json:"accounts"`
		Products []Product `json:"products"`
		Hidden   []string  `json:"hidden"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return s.snapshot(), nil
	}
	if s.token.Value == "" || !s.listed {
		return nil, bad("Sign in and look at your repositories first.")
	}
	accounts, err := s.chosen(in.Accounts)
	if err != nil {
		return nil, err
	}
	if sorted := slices.Sorted(slices.Values(accounts)); !slices.Equal(sorted, s.listedFor) {
		return nil, bad("The repositories of these accounts are not read yet.")
	}
	known := map[string]bool{}
	for _, r := range s.repos {
		known[r.Name] = true
	}
	if err := checkProducts(in.Products, in.Hidden, known); err != nil {
		return nil, err
	}
	def := ""
	for _, r := range s.repos { // the most recently pushed first
		if !r.Fork && slices.ContainsFunc(accounts, func(a string) bool { return strings.EqualFold(a, r.Owner) }) && !slices.Contains(in.Hidden, r.Name) {
			def = r.Name
			break
		}
	}
	for i := range in.Products {
		in.Products[i].Name = strings.TrimSpace(in.Products[i].Name)
	}
	mine := s.mine(accounts)
	derived := Derive(mine, in.Products, in.Hidden)
	for i, d := range derived {
		if d.TooMany {
			return nil, bad("The product %q needs more than %d matchers. Add a rule that covers some of its repositories, or split it.", in.Products[i].Name, MaxMatchers)
		}
		in.Products[i].Matchers = d.Entries
	}
	var rest []string // what no product selects: the portal's "Other"
	for _, r := range mine {
		if slices.Contains(in.Hidden, r.Name) || !namePattern.MatchString(r.Name) {
			continue
		}
		if !slices.ContainsFunc(in.Products, func(p Product) bool { return selects(p.Matchers, r) }) {
			rest = append(rest, r.Name)
		}
	}
	launch, err := s.cfg.Finish(ctx, Result{Host: s.host, Token: s.token, User: s.user, Accounts: accounts, Products: in.Products,
		Hidden: in.Hidden, Unassigned: rest, Default: def, Apps: s.installedApps(accounts)})
	if err != nil {
		return nil, fmt.Errorf("writing your settings: %w", err)
	}
	s.finished, s.launch = true, launch
	close(s.done)
	return s.snapshot(), nil
}

// selects reports whether any of the matchers selects the repository.
func selects(matchers []Rule, r ghlogin.Repo) bool {
	for _, m := range matchers {
		if fn, err := compile(m.Kind, m.Value); err == nil && fn(r.Name, r.Topics) {
			return true
		}
	}
	return false
}

// mine are the listed repositories the portal would take from the chosen accounts: no forks, valid names, one per name. Callers
// hold s.mu.
func (s *Server) mine(accounts []string) []ghlogin.Repo {
	var out []ghlogin.Repo
	for _, r := range s.repos {
		if !r.Fork && slices.ContainsFunc(accounts, func(a string) bool { return strings.EqualFold(a, r.Owner) }) && namePattern.MatchString(r.Name) {
			out = append(out, r)
		}
	}
	return OnePerName(out)
}

// previewRules answers, for the products as the page has them now, what each comes to: its rules and what they catch, the pinned
// repositories, the matchers.
func (s *Server) previewRules(_ context.Context, body []byte) (any, error) {
	var in struct {
		Accounts []string  `json:"accounts"`
		Products []Product `json:"products"`
		Hidden   []string  `json:"hidden"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	if len(in.Products) > MaxProducts {
		return nil, bad("At most %d products.", MaxProducts)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.chosen(in.Accounts)
	if err != nil {
		return nil, err
	}
	if !s.listed || !slices.Equal(slices.Sorted(slices.Values(accounts)), s.listedFor) {
		return nil, bad("The repositories are not read yet.")
	}
	return map[string]any{"products": Derive(s.mine(accounts), in.Products, in.Hidden)}, nil
}

// testRule tells what one rule would select among the chosen accounts' repositories, for the page's "add a rule".
func (s *Server) testRule(_ context.Context, body []byte) (any, error) {
	var in struct {
		Accounts []string `json:"accounts"`
		Kind     string   `json:"kind"`
		Value    string   `json:"value"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	fn, err := compile(in.Kind, strings.TrimSpace(in.Value))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.chosen(in.Accounts)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, r := range s.mine(accounts) {
		if fn(r.Name, r.Topics) {
			names = append(names, r.Name)
		}
	}
	return map[string]any{"matches": len(names), "names": names}, nil
}

// checkProducts holds the products and the hidden repositories to the configuration's limits and to the repositories listed.
func checkProducts(products []Product, hidden []string, known map[string]bool) error {
	if len(products) > MaxProducts {
		return bad("At most %d products.", MaxProducts)
	}
	if len(hidden) > MaxHidden {
		return bad("At most %d hidden repositories; leave out an account instead.", MaxHidden)
	}
	seen := map[string]bool{}
	names := map[string]bool{}
	use := func(n string) error {
		if !known[n] {
			return bad("%q is not one of your repositories.", n)
		}
		if seen[n] {
			return bad("%q is in two places.", n)
		}
		seen[n] = true
		return nil
	}
	for _, p := range products {
		name := strings.TrimSpace(p.Name)
		switch {
		case name == "" || len(name) > 120 || strings.ContainsAny(name, "\r\n"):
			return bad("A product needs a name of up to 120 characters.")
		case names[strings.ToLower(name)]:
			return bad("There are two products called %q.", name)
		case len(p.Repos) == 0:
			return bad("The product %q has no repositories.", name)
		}
		if len(p.Rules) > MaxRules {
			return bad("The product %q has more than %d rules of your own.", name, MaxRules)
		}
		for _, r := range p.Rules {
			if _, err := compile(r.Kind, r.Value); err != nil {
				return bad("A rule of %q (%s %s): %v", name, r.Kind, r.Value, err)
			}
		}
		names[strings.ToLower(name)] = true
		for _, n := range p.Repos {
			if err := use(n); err != nil {
				return err
			}
		}
	}
	for _, n := range hidden {
		if err := use(n); err != nil {
			return err
		}
	}
	return nil
}
