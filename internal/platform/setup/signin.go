package setup

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/cantara/source-code-portal/internal/platform/ghlogin"
)

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
