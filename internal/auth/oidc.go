package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/cantara/source-code-portal/internal/httpapi"
)

// flowTTL is how long a sign-in at the provider may take.
const flowTTL = 10 * time.Minute

// flow is a pending sign-in, sealed into its cookie: nothing is kept on the instance, so the callback may land on another one.
type flow struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	Nonce    string `json:"nonce"`
	ReturnTo string `json:"return"`
	Expires  int64  `json:"exp"`
}

// provider is the OIDC provider, discovered at its first use and kept once discovery succeeds.
type provider struct {
	o        OIDC
	client   *http.Client
	redirect string
	log      *slog.Logger

	mu         sync.Mutex
	p          *oidc.Provider
	verifier   *oidc.IDTokenVerifier
	conf       oauth2.Config
	endSession string
}

func (p *provider) get(ctx context.Context) (*provider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.p != nil {
		return p, nil
	}
	op, err := oidc.NewProvider(oidc.ClientContext(ctx, p.client), p.o.Issuer)
	if err != nil {
		return nil, err
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = op.Claims(&meta)
	p.p, p.endSession = op, meta.EndSession
	p.verifier = op.Verifier(&oidc.Config{ClientID: p.o.ClientID})
	p.conf = oauth2.Config{ClientID: p.o.ClientID, ClientSecret: p.o.ClientSecret, Endpoint: op.Endpoint(), RedirectURL: p.redirect,
		Scopes: p.o.Scopes}
	return p, nil
}

// Routes are the sign-in routes; none without a session key.
func (a *Auth) Routes() []httpapi.Route {
	if a.sessions.aead == nil {
		return nil
	}
	routes := []httpapi.Route{{Pattern: "POST /auth/logout", Handler: http.HandlerFunc(a.logout), MaxBody: 1 << 10}}
	if a.provider != nil {
		routes = append(routes, httpapi.Route{Pattern: "GET /auth/login", Handler: http.HandlerFunc(a.login)},
			httpapi.Route{Pattern: "GET /auth/callback", Handler: http.HandlerFunc(a.callback)})
	}
	if len(a.tokens) > 0 {
		routes = append(routes, httpapi.Route{Pattern: "POST /auth/token", Handler: http.HandlerFunc(a.tokenSignIn), MaxBody: 4 << 10})
	}
	return routes
}

func random() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// login sends the browser to the provider, the pending sign-in sealed into its cookie.
func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	p, err := a.provider.get(r.Context())
	if err != nil {
		a.o.Log.Warn("oidc discovery failed", "issuer", a.provider.o.Issuer, "err", err)
		signInError(w, r, "provider")
		return
	}
	f := flow{State: random(), Verifier: oauth2.GenerateVerifier(), Nonce: random(), ReturnTo: returnTo(r.URL.Query().Get("return_to")),
		Expires: a.o.Now().Add(flowTTL).Unix()}
	v, err := a.flows.seal(a.flow, f)
	if err != nil {
		signInError(w, r, "provider")
		return
	}
	a.cookie(w, a.flow, v, int(flowTTL.Seconds()))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, p.conf.AuthCodeURL(f.State, oauth2.S256ChallengeOption(f.Verifier), oidc.Nonce(f.Nonce)), http.StatusFound)
}

// callback finishes a sign-in: the state matches the pending one, the code is exchanged and the ID token verified; the provider's
// tokens are discarded and the session cookie set.
func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var f flow
	c, err := r.Cookie(a.flow)
	a.clear(w, a.flow)
	q := r.URL.Query()
	switch {
	case err != nil || !a.flows.open(a.flow, c.Value, &f) || a.o.Now().Unix() >= f.Expires:
		signInError(w, r, "expired")
		return
	case q.Get("error") != "":
		signInError(w, r, "denied")
		return
	case q.Get("state") == "" || q.Get("state") != f.State:
		signInError(w, r, "state")
		return
	}
	s, err := a.exchange(r.Context(), q.Get("code"), f)
	if err != nil {
		a.o.Log.Warn("oidc sign-in failed", "issuer", a.provider.o.Issuer, "err", err)
		signInError(w, r, "provider")
		return
	}
	if err := a.start(w, s); err != nil {
		a.o.Log.Warn("session not sealed", "err", err)
		signInError(w, r, "provider")
		return
	}
	http.Redirect(w, r, f.ReturnTo, http.StatusSeeOther)
}

var errNonce = errors.New("ID token nonce does not match the sign-in")

func (a *Auth) exchange(ctx context.Context, code string, f flow) (session, error) {
	ctx = oidc.ClientContext(ctx, a.o.Client)
	p, err := a.provider.get(ctx)
	if err != nil {
		return session{}, err
	}
	tok, err := p.conf.Exchange(ctx, code, oauth2.VerifierOption(f.Verifier))
	if err != nil {
		return session{}, err
	}
	raw, _ := tok.Extra("id_token").(string)
	idt, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return session{}, err
	}
	if idt.Nonce != f.Nonce {
		return session{}, errNonce
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return session{}, err
	}
	if _, ok := claims["email"]; !ok || claims[p.o.GroupsClaim] == nil { // some providers give these only from userinfo
		if ui, err := p.p.UserInfo(ctx, oauth2.StaticTokenSource(tok)); err == nil {
			var more map[string]any
			if ui.Claims(&more) == nil && more["sub"] == idt.Subject {
				for k, v := range more {
					if _, ok := claims[k]; !ok {
						claims[k] = v
					}
				}
			}
		}
	}
	s := session{Issuer: idt.Issuer, Subject: idt.Subject, Email: str(claims["email"]), Verified: claims["email_verified"] != false,
		Groups: a.o.Keep(stringList(claims[p.o.GroupsClaim]))}
	for _, k := range []string{"name", "preferred_username", "email"} {
		if s.Name = str(claims[k]); s.Name != "" {
			break
		}
	}
	if s.Name == "" {
		s.Name = idt.Subject
	}
	return s, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// stringList reads a claim that is a list of strings or one string.
func stringList(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return slices.Compact(out)
	}
	return nil
}

// logout ends the session and, for a provider's session, sends the browser to the provider's end-session endpoint.
func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if !httpapi.SameOrigin(r, a.o.PublicURL) {
		httpapi.WriteProblem(w, http.StatusForbidden, "cross-origin", "Sign out from the portal's own pages.")
		return
	}
	v := httpapi.ViewerOf(r.Context())
	a.clear(w, a.session)
	w.Header().Set("Cache-Control", "no-store")
	target := "/"
	if a.provider != nil && v.Via == "oidc" {
		if p, err := a.provider.get(r.Context()); err == nil && p.endSession != "" {
			if u, err := url.Parse(p.endSession); err == nil {
				q := u.Query()
				q.Set("client_id", p.o.ClientID)
				q.Set("post_logout_redirect_uri", a.o.PublicURL+"/")
				u.RawQuery = q.Encode()
				target = u.String()
			}
		}
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// tokenSignIn exchanges a token for a session, for a person without a provider.
func (a *Auth) tokenSignIn(w http.ResponseWriter, r *http.Request) {
	if !httpapi.SameOrigin(r, a.o.PublicURL) {
		httpapi.WriteProblem(w, http.StatusForbidden, "cross-origin", "Sign in from the portal's own pages.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	t, ok := a.match(r.PostFormValue("token"))
	if !ok {
		signInError(w, r, "token")
		return
	}
	if err := a.start(w, session{Subject: "token:" + t.Name, Name: t.Name, Token: t.Name}); err != nil {
		signInError(w, r, "token")
		return
	}
	http.Redirect(w, r, returnTo(r.PostFormValue("return_to")), http.StatusSeeOther) //nolint:gosec // G710: returnTo keeps same-origin paths only
}
