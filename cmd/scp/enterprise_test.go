package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/auth/oidctest"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

// enterpriseConfig is a configuration of the enterprise mode: one source read with people's tokens, sign-in through issuer, the
// store at storePath.
func enterpriseConfig(t *testing.T, storePath, base, issuer, public string) config.Config {
	t.Helper()
	dir := t.TempDir()
	file := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg, _, err := config.Load(file("scp.yaml", `store: {path: `+storePath+`}
server: {public_url: "`+public+`"}
auth:
  session_key_file: `+file("key", strings.Repeat("k", 32))+`
  admin_emails: [ada@example.org]
  oidc: {issuer: "`+issuer+`", client_id: scp, scopes: [openid, email, profile]}
github:
  sources:
    - {id: ghe, base_url: "`+base+`", orgs: [acme]}
catalog:
  groups:
    - {id: all, name: All, default_repo: zq-secret, match: [{catch_all: true}]}
enterprise:
  enabled: true
  token_key_file: `+file("tokens", strings.Repeat("t", 32))+`
  github: [{source: ghe, web_url: "https://ghe.example.test/", people: true}]
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"
	return cfg
}

// The modes are not mixed: scp serve refuses a configuration read with people's tokens, scp enterprise any other, and the enterprise
// mode's store is refused by the other two as theirs are by it.
func TestEnterpriseModeKeepsApart(t *testing.T) {
	dir := t.TempDir()
	people := enterpriseConfig(t, filepath.Join(dir, "e.db"), "https://api.ghe.example.test/", "https://idp.example.test", "https://scp.example")
	if _, err := standUp(t, people, deps{}); err == nil || !strings.Contains(err.Error(), "run scp enterprise") {
		t.Errorf("serve with people's tokens: %v", err)
	}
	if _, err := standUp(t, people, deps{mode: store.Personal, launch: launchFor("owner")}); err == nil {
		t.Error("personal with people's tokens started")
	}
	if _, err := standUp(t, modeConfig(t, filepath.Join(dir, "s.db")), deps{mode: store.Enterprise}); err == nil ||
		!strings.Contains(err.Error(), "people's tokens") {
		t.Errorf("enterprise without people's tokens: %v", err)
	}
	if _, err := standUp(t, people, deps{mode: store.Enterprise, launch: launchFor("owner")}); err == nil {
		t.Error("enterprise took a launch link")
	}

	for _, other := range []store.Mode{store.Shared, store.Personal} {
		path := filepath.Join(dir, string(other)+".db")
		db, err := store.OpenAs(t.Context(), path, other, quietLog())
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetMeta(t.Context(), "probe", "1"); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		var me *store.ModeError
		if _, err := standUp(t, enterpriseConfig(t, path, "https://api.ghe.example.test/", "https://idp.example.test", "https://scp.example"),
			deps{mode: store.Enterprise}); !errors.As(err, &me) {
			t.Errorf("enterprise on a %s store: %v", other, err)
		}
	}
	e, err := standUp(t, people, deps{mode: store.Enterprise})
	if err != nil {
		t.Fatal(err)
	}
	if e.mode != store.Enterprise {
		t.Errorf("mode %q", e.mode)
	}
	shared, err := standUp(t, modeConfig(t, filepath.Join(dir, "shared-routes.db")), deps{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range e.routes {
		if strings.Contains(r, "/setup") || r == "GET /auth/launch" || strings.Contains(r, "webhook") && r != "POST /github/webhook" {
			t.Errorf("the enterprise mode registers %s", r)
		}
		if !slices.Contains(shared.routes, r) {
			t.Errorf("the enterprise mode registers %s, which the shared site does not", r)
		}
	}
}

// The enterprise mode starts on an empty store and sends GitHub nothing until a person links a token; nothing answers anonymously
// but the sign-in and the open paths; and a person signed in without an access list sees nothing of what the snapshot holds.
func TestEnterpriseFailsClosed(t *testing.T) {
	gh := githubtest.New(t)
	gh.AddRepo("acme", gogh.Repository{Name: new("zq-secret"), Visibility: new("private")})
	idp := oidctest.New("scp")
	defer idp.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	public := "http://" + addr
	cfg := enterpriseConfig(t, filepath.Join(t.TempDir(), "scp.db"), gh.BaseURL(), idp.Issuer(), public)
	cfg.Server.PublicAddr = addr
	srv, err := newServer(t.Context(), cfg, deps{mode: store.Enterprise, github: nil, registries: newFakeRegistry(t)},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	}()
	// What a shared fetch would have published: the views must not show it to a person without a list.
	srv.pub.Update(func(b *snapshot.Builder) {
		e := &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "ghe", GitHubID: 1}, Owner: "acme", Name: "zq-secret", Visibility: "private"}}
		b.ReplaceSource("ghe", []*snapshot.RepoEntry{e}, nil, snapshot.SourceStatus{ID: "ghe", Repos: 1})
		b.SetGroups([]snapshot.Group{{ID: "all", Name: "All", DefaultRepo: "acme/zq-secret", Members: []snapshot.RepoKey{"acme/zq-secret"}}})
		b.SetCatalogued()
	})
	srv.pub.Flush()

	do := func(c *http.Client, path string, header ...string) (int, string) {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, public+path, nil)
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	anon := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer anon.CloseIdleConnections()
	ready, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+srv.adminLn.Addr().String()+"/readyz", nil)
	if resp, err := anon.Do(ready); err != nil || resp.StatusCode != http.StatusOK {
		t.Errorf("readyz on an empty store: %v %v", resp, err)
	} else {
		_ = resp.Body.Close()
	}
	for _, p := range append(publicPaths(t), "/", "/repos/acme/zq-secret", "/groups/all", "/llms.txt", "/sitemap.xml", "/badges/repos/acme/zq-secret/health.svg") {
		if p == "/api/v1/me" || p == "/robots.txt" {
			continue
		}
		if code, b := do(anon, p, "X-Forwarded-User", "ann"); code != http.StatusUnauthorized || strings.Contains(b, "zq-secret") {
			t.Errorf("anonymous %s: %d %.120s", p, code, b)
		}
	}
	if code, b := do(anon, "/robots.txt"); code != http.StatusOK || b != "User-agent: *\nDisallow: /\n" {
		t.Errorf("robots.txt: %d %q", code, b)
	}

	jar, _ := cookiejar.New(nil)
	ada := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	defer ada.CloseIdleConnections()
	if code, b := do(ada, "/auth/login?return_to=/api/v1/me"); code != http.StatusOK || !strings.Contains(b, `"signedIn":true`) {
		t.Fatalf("sign-in: %d %s", code, b)
	}
	if len(srv.pub.Current().Repos) != 1 {
		t.Fatal("the snapshot does not hold the repository: the views below prove nothing")
	}
	for _, p := range []string{"/api/v1/repos", "/api/v1/groups", "/api/v1/dashboard", "/api/v1/search?q=zq", "/llms.txt", "/sitemap.xml",
		"/api/v1/repos/acme/zq-secret", "/badges/repos/acme/zq-secret/health.svg", "/api/v1/snapshot"} {
		if _, b := do(ada, p); strings.Contains(b, "zq-secret") || strings.Contains(b, `"All"`) {
			t.Errorf("signed in without a list, %s: %.200s", p, b)
		}
	}
	if code, _ := do(ada, "/api/v1/repos/acme/zq-secret"); code != http.StatusNotFound {
		t.Errorf("the repository's page without a list: %d", code)
	}
	if got := gh.Ledger(); len(got) != 0 {
		t.Errorf("GitHub was asked without a person's token: %v", got)
	}
}
