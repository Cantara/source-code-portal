package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/ghlogin"
	"github.com/cantara/source-code-portal/internal/prereq"
)

// The Connect step (design/sync-modes.md, EV3): for each account the person can create a GitHub App of their own, with GitHub's
// manifest flow, and install it. An account with the App installed is kept fresh by GitHub's events (the portal reads the App's
// delivery log); every other account is polled with the person's token. The page never sees the App's key; it waits here until Finish.

const (
	homepage     = "https://github.com/Cantara/source-code-portal"
	pendingFor   = time.Hour // GitHub's code lives an hour
	maxPending   = 20
	maxAppName   = 34
	callbackPath = "/setup/app/callback"
)

// appRec is an App the person created on GitHub for an account.
type appRec struct {
	Account   string
	ID        int64
	Slug      string
	Key       string // PEM: held in memory until Finish seals it
	Installed bool
}

// pendingApp is a manifest posted to GitHub whose redirect has not come back.
type pendingApp struct {
	Account string
	Until   time.Time
}

// AppKey is an App the person installed, for Result: the portal reads the account with it.
type AppKey struct {
	Account string
	ID      int64
	Slug    string
	Key     string
}

type appOut struct {
	Account    string `json:"account"`
	Status     string `json:"status"` // created, installed
	Slug       string `json:"slug"`
	AppURL     string `json:"appUrl"`
	InstallURL string `json:"installUrl"`
}

func (s *Server) now() time.Time {
	if s.cfg.Now != nil {
		return s.cfg.Now()
	}
	return time.Now()
}

func (s *Server) appsOut() []appOut {
	out := make([]appOut, 0, len(s.apps))
	for _, a := range s.apps {
		o := appOut{Account: a.Account, Status: "created", Slug: a.Slug, AppURL: s.host.AppURL(a.Slug), InstallURL: s.host.AppInstallURL(a.Slug)}
		if a.Installed {
			o.Status = "installed"
		}
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b appOut) int { return strings.Compare(a.Account, b.Account) })
	return out
}

// appRights are the read permissions and the events the portal needs of an App, from the one table of prerequisites.
func appRights() (permissions map[string]string, events []string) {
	permissions = map[string]string{}
	for _, n := range prereq.Needs {
		for _, p := range n.Permissions {
			permissions[p] = "read"
		}
		for _, e := range n.Events {
			if !slices.Contains(events, e) {
				events = append(events, e)
			}
		}
	}
	return permissions, events
}

// accountOK is whether the person may name the account: their own login or one of their organisations. Callers hold s.mu.
func (s *Server) accountOK(a string) (name string, self bool, ok bool) {
	if !loginPattern.MatchString(a) {
		return "", false, false
	}
	if strings.EqualFold(a, s.user) {
		return s.user, true, true
	}
	i := slices.IndexFunc(s.orgs, func(o string) bool { return strings.EqualFold(o, a) })
	if i < 0 {
		return "", false, false
	}
	return s.orgs[i], false, true
}

type startOut struct {
	Action   string `json:"action"`   // where the page posts the manifest
	Manifest string `json:"manifest"` // the manifest, as the form field GitHub reads
}

// startApp prepares the creation of an account's App: the manifest and the address the person's browser posts it to. Nothing is
// created until the person confirms on GitHub.
func (s *Server) startApp(_ context.Context, body []byte) (any, error) {
	var in struct {
		Account string `json:"account"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureHost()
	if s.token.Value == "" || !s.hostSet {
		return nil, bad("Sign in first.")
	}
	account, self, ok := s.accountOK(in.Account)
	if !ok {
		return nil, bad("%q is not one of your accounts.", in.Account)
	}
	if s.apps[strings.ToLower(account)] != nil {
		return nil, bad("The App for %s is already made. Install it, or remove it to make another.", account)
	}
	now := s.now()
	for k, p := range s.pending { // forget what ran out
		if now.After(p.Until) {
			delete(s.pending, k)
		}
	}
	if len(s.pending) >= maxPending {
		return nil, bad("Too many Apps were started without finishing. Reload this page and try again.")
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	state := base64.RawURLEncoding.EncodeToString(b)
	s.pending[state] = pendingApp{Account: account, Until: now.Add(pendingFor)}
	suffix := make([]byte, 2)
	_, _ = rand.Read(suffix)
	name := "scp-" + strings.ToLower(account)
	if len(name) > maxAppName-5 {
		name = name[:maxAppName-5]
	}
	name += "-" + hex.EncodeToString(suffix) // an App's name is unique on the host
	permissions, events := appRights()
	manifest := ghlogin.AppManifest(name, homepage, "http://"+s.cfg.Addr+"/webhooks/github", "http://"+s.cfg.Addr+callbackPath, permissions, events)
	return startOut{Action: s.host.AppCreateURL(account, self, state), Manifest: manifest}, nil
}

// appCallback is where GitHub sends the person back with the one-hour code. The page's cookie is not sent on a redirect from another
// site (SameSite=Strict), so the state, a secret only this process and the person's browser know, is the proof; it works once. The
// answer is a page that goes on to the setup page from here, where the cookie is sent again.
func (s *Server) appCallback(w http.ResponseWriter, r *http.Request) {
	state, code := r.URL.Query().Get("state"), r.URL.Query().Get("code")
	s.mu.Lock()
	var p pendingApp
	var found bool
	for k, v := range s.pending { // compare in constant time, whichever it is
		if subtle.ConstantTimeCompare([]byte(k), []byte(state)) == 1 {
			p, found = v, true
			delete(s.pending, k)
		}
	}
	valid := found && !s.now().After(p.Until) && s.hostSet
	gen, c := s.gen, s.client()
	s.mu.Unlock()
	failed := false // the page says why in fixed words: nothing from the request or from GitHub is echoed
	switch {
	case !valid:
		failed = true
	default:
		app, err := c.ConvertManifest(r.Context(), code)
		if err != nil {
			failed = true
			break
		}
		s.mu.Lock()
		if s.gen == gen { // the person did not sign out meanwhile
			if s.apps == nil {
				s.apps = map[string]*appRec{}
			}
			s.apps[strings.ToLower(p.Account)] = &appRec{Account: p.Account, ID: app.ID, Slug: app.Slug, Key: app.Key}
		}
		s.mu.Unlock()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if failed {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, callbackFailed)
		return
	}
	_, _ = io.WriteString(w, callbackMade)
}

const (
	callbackMade = `<!doctype html><meta charset="utf-8"><title>Source Code Portal setup</title><body><h1>The App is made</h1>` +
		`<p>Going on to the setup page…</p><p><a href="/setup/">Continue</a></p><script src="/setup/callback.js"></script>`
	callbackFailed = `<!doctype html><meta charset="utf-8"><title>Source Code Portal setup</title><body><h1>The App was not made</h1>` +
		`<p>This link is not the one the setup page made, it ran out, or GitHub did not hand the App over (its code works once, within an hour). ` +
		`Go back to the setup page and start again.</p><p><a href="/setup/">Back to the setup page</a></p>`
)

// checkApp asks GitHub where the App is installed and records whether it is on the account.
func (s *Server) checkApp(ctx context.Context, body []byte) (any, error) {
	var in struct {
		Account string `json:"account"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	a := s.apps[strings.ToLower(in.Account)]
	if a == nil {
		s.mu.Unlock()
		return nil, bad("There is no App for %q yet.", in.Account)
	}
	c, id, key, gen := s.client(), a.ID, a.Key, s.gen
	s.mu.Unlock()
	installs, err := c.Installations(ctx, id, key)
	if err != nil {
		return nil, bad("GitHub did not tell where the App is installed: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen == gen && s.apps[strings.ToLower(in.Account)] == a {
		a.Installed = slices.ContainsFunc(installs, func(i ghlogin.Installation) bool { return strings.EqualFold(i.Account, a.Account) && !i.Suspended })
	}
	return s.snapshot(), nil
}

// forgetApp drops an App the person does not want to use here (it stays on GitHub; they can delete it in its settings).
func (s *Server) forgetApp(_ context.Context, body []byte) (any, error) {
	var in struct {
		Account string `json:"account"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.apps, strings.ToLower(in.Account))
	return s.snapshot(), nil
}

// installedApps are the Apps installed on the chosen accounts. Callers hold s.mu.
func (s *Server) installedApps(accounts []string) []AppKey {
	var out []AppKey
	for _, acc := range accounts {
		if a := s.apps[strings.ToLower(acc)]; a != nil && a.Installed {
			out = append(out, AppKey{Account: acc, ID: a.ID, Slug: a.Slug, Key: a.Key})
		}
	}
	return out
}
