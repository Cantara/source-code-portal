package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
)

func tokenSource(t *testing.T, srv *githubtest.Server, orgs ...string) *Source {
	t.Helper()
	srv.AddToken("ghp_test")
	c, err := New(config.GitHub{Sources: []config.Source{{
		ID: "gh", BaseURL: srv.BaseURL(), Orgs: orgs, TokenFile: "x", Token: config.NewSecret([]byte("ghp_test")),
	}}}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return c.Source("gh")
}

func TestListReposPaginatesAndConverts(t *testing.T) {
	srv := githubtest.New(t)
	for i := range 250 {
		srv.AddRepo("acme", gh.Repository{Name: new(fmt.Sprintf("repo-%03d", i)), Topics: []string{"t"}})
	}
	s := tokenSource(t, srv, "acme")
	repos, err := s.ListRepos(t.Context(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 250 || srv.Count("GET /orgs/acme/repos") != 3 {
		t.Fatalf("got %d repos in %d requests, want 250 in 3", len(repos), srv.Count("GET /orgs/acme/repos"))
	}
	if r := repos[0]; r.Name != "repo-249" || r.Owner != "acme" || r.ID.Source != "gh" || r.DefaultBranch != "main" || len(r.Topics) != 1 {
		t.Errorf("first repo = %+v, want repo-249 (most recently pushed) converted", r)
	}
}

func TestConditionalRequestsAreFree(t *testing.T) {
	srv := githubtest.New(t)
	id := srv.AddRepo("acme", gh.Repository{Name: new("one")})
	s := tokenSource(t, srv, "acme")
	ctx := t.Context()
	if _, err := s.ListRepos(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	before := s.token.rate.left
	repos, err := s.ListRepos(ctx, "acme")
	if err != nil || len(repos) != 1 {
		t.Fatalf("repeat listing = %v, %v", repos, err)
	}
	if got := srv.Ledger(); !strings.HasSuffix(got[len(got)-1], " 304") {
		t.Errorf("repeat listing was not conditional: %v", got)
	}
	if s.token.rate.left != before {
		t.Errorf("a 304 was charged: %d -> %d", before, s.token.rate.left)
	}
	srv.Update(id, func(r *gh.Repository) { r.Description = new("changed") })
	repos, err = s.ListRepos(ctx, "acme")
	if err != nil || repos[0].Description != "changed" {
		t.Fatalf("after a change = %v, %v", repos, err)
	}
	if frac, _ := s.token.rate.remaining(); frac >= 1 {
		t.Errorf("remaining = %v, want below 1", frac)
	}
}

func TestAppMode(t *testing.T) {
	srv := githubtest.New(t)
	srv.Install("acme")
	srv.Install("other")
	srv.AddRepo("acme", gh.Repository{Name: new("one")})
	c, err := New(config.GitHub{Sources: []config.Source{{
		ID: "gh", BaseURL: srv.BaseURL(), Orgs: []string{"acme", "missing"},
		App: &config.App{ID: srv.AppID, Installations: []string{"acme", "missing"}, PrivateKey: config.NewSecret(srv.PrivateKeyPEM())},
	}}}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	s := c.Source("gh")
	repos, err := s.ListRepos(t.Context(), "ACME")
	if err != nil || len(repos) != 1 {
		t.Fatalf("ListRepos = %v, %v", repos, err)
	}
	if frac, _ := c.Remaining("gh", "acme"); frac >= 1 {
		t.Errorf("installation rate not observed: %v", frac)
	}
	if frac, _ := c.Remaining("gh", "other"); frac != 1 {
		t.Errorf("filtered installation has a rate: %v", frac)
	}
	_, err = s.ListRepos(t.Context(), "missing")
	var e *Error
	if !errors.As(err, &e) || e.Code != "no_installation" || !e.Permanent() {
		t.Errorf("uninstalled owner: err = %v, want no_installation", err)
	}
}

func TestClassify(t *testing.T) {
	tests := map[string]struct {
		setup func(*githubtest.Server)
		repo  string // default "present"
		token string
		kind  Kind
		retry bool
	}{
		"not found":   {setup: func(*githubtest.Server) {}, repo: "absent", kind: NotFound},
		"server":      {setup: func(s *githubtest.Server) { s.Fail("GET /repos/", http.StatusBadGateway, 1, 0) }, kind: Transient},
		"bad token":   {setup: func(*githubtest.Server) {}, token: "wrong", kind: Auth},
		"forbidden":   {setup: func(s *githubtest.Server) { s.Fail("GET /repos/", http.StatusForbidden, 1, 0) }, kind: Permanent},
		"rate":        {setup: func(s *githubtest.Server) { s.Exhaust("ghp_test") }, kind: RateLimited, retry: true},
		"secondary":   {setup: func(s *githubtest.Server) { s.Fail("GET /repos/", http.StatusForbidden, 1, time.Minute) }, kind: RateLimited, retry: true},
		"too many":    {setup: func(s *githubtest.Server) { s.Fail("GET /repos/", http.StatusTooManyRequests, 1, time.Minute) }, kind: RateLimited, retry: true},
		"conflicting": {setup: func(s *githubtest.Server) { s.Fail("GET /repos/", http.StatusConflict, 1, 0) }, kind: NotFound},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := githubtest.New(t)
			srv.AddToken("ghp_test")
			srv.AddRepo("acme", gh.Repository{Name: new("present")})
			tok := "ghp_test"
			if tc.token != "" {
				tok = tc.token
			}
			c, err := New(config.GitHub{Sources: []config.Source{{ID: "gh", BaseURL: srv.BaseURL(), Orgs: []string{"acme"},
				Token: config.NewSecret([]byte(tok))}}}, nil, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}
			tc.setup(srv)
			name := "present"
			if tc.repo != "" {
				name = tc.repo
			}
			_, err = c.Source("gh").Repo(t.Context(), "acme", name)
			var e *Error
			if !errors.As(err, &e) || e.Kind != tc.kind {
				t.Fatalf("err = %v, want kind %d", err, tc.kind)
			}
			if at, _ := e.RateLimitedUntil(); tc.retry && !at.After(time.Now()) {
				t.Errorf("RetryAt = %v, want in the future", at)
			}
		})
	}
}

func TestCanceledIsNotClassified(t *testing.T) {
	srv := githubtest.New(t)
	s := tokenSource(t, srv, "acme")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := s.ListRepos(ctx, "acme")
	var e *Error
	if !errors.Is(err, context.Canceled) || errors.As(err, &e) {
		t.Errorf("err = %v, want context.Canceled unclassified", err)
	}
}

func TestETagCacheEvicts(t *testing.T) {
	c := newETagCache(2)
	for _, k := range []string{"a", "b"} {
		c.put(k, &cacheEntry{etag: k})
	}
	c.get("a") // a is now the most recent
	c.put("c", &cacheEntry{etag: "c"})
	if c.get("b") != nil || c.get("a") == nil || c.get("c") == nil {
		t.Errorf("want b evicted and a, c kept")
	}
}
