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
	"strconv"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/auth/oidctest"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/people"
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
	peopleCfg := enterpriseConfig(t, filepath.Join(dir, "e.db"), "https://api.ghe.example.test/", "https://idp.example.test", "https://scp.example")
	if _, err := standUp(t, peopleCfg, deps{}); err == nil || !strings.Contains(err.Error(), "run scp enterprise") {
		t.Errorf("serve with people's tokens: %v", err)
	}
	if _, err := standUp(t, peopleCfg, deps{mode: store.Personal, launch: launchFor("owner")}); err == nil {
		t.Error("personal with people's tokens started")
	}
	if _, err := standUp(t, modeConfig(t, filepath.Join(dir, "s.db")), deps{mode: store.Enterprise}); err == nil ||
		!strings.Contains(err.Error(), "people's tokens") {
		t.Errorf("enterprise without people's tokens: %v", err)
	}
	if _, err := standUp(t, peopleCfg, deps{mode: store.Enterprise, launch: launchFor("owner")}); err == nil {
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
	e, err := standUp(t, peopleCfg, deps{mode: store.Enterprise})
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
	if !slices.Contains(e.routes, "POST "+people.TokensPattern) || slices.ContainsFunc(shared.routes, func(r string) bool { return strings.Contains(r, people.TokensPattern) }) {
		t.Error("the people's token routes are not the enterprise mode's own")
	}
	for _, r := range e.routes {
		if strings.Contains(r, "/setup") || r == "GET /auth/launch" || strings.Contains(r, "webhook") && r != "POST /github/webhook" {
			t.Errorf("the enterprise mode registers %s", r)
		}
		if !slices.Contains(shared.routes, r) && !strings.HasPrefix(strings.Fields(r)[1], people.TokensPattern) {
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
	srv, public := enterprisePortal(t, gh, idp, filepath.Join(t.TempDir(), "scp.db"))
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
		return call(t, c, http.MethodGet, public+path, "", header...)
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

// enterprisePortal runs scp enterprise against gh and idp on the store at path until the test ends; public is its address.
func enterprisePortal(t *testing.T, gh *githubtest.Server, idp *oidctest.Provider, path string) (*server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0") // a free port: the redirect URI is under server.public_url
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	public := "http://" + addr
	cfg := enterpriseConfig(t, path, gh.BaseURL(), idp.Issuer(), public)
	cfg.Server.PublicAddr = addr
	srv, err := newServer(t.Context(), cfg, deps{mode: store.Enterprise, registries: newFakeRegistry(t)}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	})
	return srv, public
}

// call sends method to url with body (JSON when not empty) and the header pairs, and returns the status and the body.
func call(t *testing.T, c *http.Client, method, url, body string, header ...string) (int, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, _ := http.NewRequestWithContext(t.Context(), method, url, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
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

// browserFor is a browser signed in to the portal at public as the provider's current user.
func browserFor(t *testing.T, public string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	t.Cleanup(c.CloseIdleConnections)
	if code, b := call(t, c, http.MethodGet, public+"/auth/login?return_to=/api/v1/me", ""); code != http.StatusOK || !strings.Contains(b, `"signedIn":true`) {
		t.Fatalf("sign-in: %d %s", code, b)
	}
	return c
}

// A person signed in links their own token through the API, reads it back without its secret, and removes it; another person's
// token, a cross-origin request and an anonymous one are refused; the token is kept across a restart, sealed.
func TestEnterpriseLinksTokens(t *testing.T) {
	gh := githubtest.New(t)
	inner := gh.AddRepo("acme", gogh.Repository{Name: new("inner"), Visibility: new("internal")})
	for tok, login := range map[string]string{"github_pat_ada": "ada_octo", "github_pat_bo": "bo_octo"} {
		gh.AddUserToken(tok, login)
		gh.Reach(tok, inner)
	}
	idp := oidctest.New("scp")
	defer idp.Close()
	path := filepath.Join(t.TempDir(), "scp.db")
	srv, public := enterprisePortal(t, gh, idp, path)
	url := public + people.TokensPattern
	anon := &http.Client{Timeout: 5 * time.Second}
	defer anon.CloseIdleConnections()
	if code, _ := call(t, anon, http.MethodGet, url, ""); code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", code)
	}
	ada := browserFor(t, public) // the fake provider's user is ada@example.org
	same := []string{"Sec-Fetch-Site", "same-origin"}
	if code, b := call(t, ada, http.MethodPost, url, `{"source":"ghe","owner":"acme","token":"github_pat_ada"}`, "Sec-Fetch-Site", "cross-site"); code != http.StatusForbidden {
		t.Errorf("cross-origin: %d %s", code, b)
	}
	if code, b := call(t, ada, http.MethodPost, url, `{"source":"ghe","owner":"acme","token":"github_pat_bo"}`, same...); code != http.StatusUnprocessableEntity ||
		!strings.Contains(b, "not-yours") {
		t.Errorf("another's token: %d %s", code, b)
	}
	if code, b := call(t, ada, http.MethodPost, url, `{"source":"ghe","owner":"acme","token":"github_pat_ada"}`, same...); code != http.StatusCreated ||
		!strings.Contains(b, `"login":"ada_octo"`) || strings.Contains(b, "github_pat_ada") {
		t.Fatalf("link: %d %s", code, b)
	}
	code, b := call(t, ada, http.MethodGet, url, "")
	if code != http.StatusOK || !strings.Contains(b, `"login":"ada_octo"`) || strings.Contains(b, "github_pat_ada") ||
		!strings.Contains(b, "settings/personal-access-tokens/new?") {
		t.Errorf("list: %d %s", code, b)
	}
	if srv.people.Tokens(people.Who{Subject: "u-1"}) == nil {
		t.Error("the token is not the signed-in person's")
	}
	if code, _ := call(t, ada, http.MethodDelete, url+"/ghe/acme", "", same...); code != http.StatusNoContent {
		t.Errorf("remove: %d", code)
	}
	if _, b := call(t, ada, http.MethodGet, url, ""); strings.Contains(b, "ada_octo") {
		t.Errorf("after removal: %s", b)
	}
}

// Two people with different access to one organisation: each repository is read with the token of a person who reads it, never with
// another's; each person sees exactly the repositories their token lists, in every list, page and search; a person who removes their
// token sees nothing.
func TestEnterpriseTwoPeople(t *testing.T) {
	gh := githubtest.New(t)
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	sha := 0
	repo := func(name, vis string) int64 {
		sha++
		id := gh.AddRepo("acme", gogh.Repository{Name: new(name), Visibility: new(vis), Description: new(name + " tools")})
		gh.SetCommits(id, githubtest.Commit{SHA: strings.Repeat(strconv.Itoa(sha), 40), Message: name + " change", AuthorName: "Ann", Date: at})
		gh.SetFile(id, "README.md", "# "+name+"\n")
		return id
	}
	inner, adas, bos := repo("inner", "internal"), repo("zq-ada", "private"), repo("xk-bo", "private")
	gh.AddUserToken("github_pat_ada", "ada_octo")
	gh.Reach("github_pat_ada", inner, adas)
	gh.AddUserToken("github_pat_bo", "bo_octo")
	gh.Reach("github_pat_bo", inner, bos)
	idp := oidctest.New("scp")
	defer idp.Close()
	_, public := enterprisePortal(t, gh, idp, filepath.Join(t.TempDir(), "scp.db"))
	same := []string{"Sec-Fetch-Site", "same-origin"}
	link := func(c *http.Client, token string) {
		t.Helper()
		if code, b := call(t, c, http.MethodPost, public+people.TokensPattern, `{"source":"ghe","owner":"acme","token":"`+token+`"}`, same...); code != http.StatusCreated {
			t.Fatalf("link %s: %d %s", token, code, b)
		}
	}
	ada := browserFor(t, public)
	idp.SetUser(oidctest.User{Subject: "u-2", Email: "bo@example.org", Name: "Bo"})
	bo := browserFor(t, public)
	link(ada, "github_pat_ada")
	link(bo, "github_pat_bo")

	waitFor := func(c *http.Client, path, want string) string {
		t.Helper()
		var b string
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if _, b = call(t, c, http.MethodGet, public+path, ""); strings.Contains(b, want) {
				return b
			}
		}
		t.Fatalf("%s never held %q: %.300s", path, want, b)
		return ""
	}
	waitFor(ada, "/api/v1/repos/acme/zq-ada", "zq-ada change")
	waitFor(bo, "/api/v1/repos/acme/xk-bo", "xk-bo change")
	waitFor(ada, "/api/v1/repos/acme/inner", "inner change")

	for _, tc := range []struct {
		who       *http.Client
		mine, not string
		name      string
	}{{ada, "zq-ada", "xk-bo", "Ada"}, {bo, "xk-bo", "zq-ada", "Bo"}} {
		for _, p := range []string{"/api/v1/repos", "/api/v1/dashboard", "/api/v1/groups/all", "/api/v1/commits", "/api/v1/search?q=tools",
			"/llms.txt", "/llms-full.txt", "/sitemap.xml", "/knowledge.yaml", "/api/v1/snapshot"} {
			code, b := call(t, tc.who, http.MethodGet, public+p, "")
			if strings.Contains(b, tc.not) {
				t.Errorf("%s reads %s in %s: %.300s", tc.name, tc.not, p, b)
			}
			if p == "/api/v1/repos" && (code != http.StatusOK || !strings.Contains(b, tc.mine) || !strings.Contains(b, "acme/inner")) {
				t.Errorf("%s's repositories: %d %.300s", tc.name, code, b)
			}
		}
		if code, _ := call(t, tc.who, http.MethodGet, public+"/api/v1/repos/acme/"+tc.not, ""); code != http.StatusNotFound {
			t.Errorf("%s reads the other's repository: %d", tc.name, code)
		}
		if code, _ := call(t, tc.who, http.MethodGet, public+"/badges/repos/acme/"+tc.not+"/health.svg", ""); code != http.StatusNotFound {
			t.Errorf("%s reads the other's badge: %d", tc.name, code)
		}
	}

	// Nothing was asked with a token about a repository it does not read.
	for tok, reach := range map[string][]string{"github_pat_ada": {"inner", "zq-ada"}, "github_pat_bo": {"inner", "xk-bo"}} {
		asked := gh.Asked(tok)
		if len(asked) == 0 {
			t.Errorf("nothing asked with %s", tok)
		}
		for _, a := range asked {
			if rest, ok := strings.CutPrefix(strings.Fields(a)[1], "/repos/acme/"); ok {
				if name, _, _ := strings.Cut(rest, "/"); !slices.Contains(reach, name) || strings.HasSuffix(a, " 404") {
					t.Errorf("%s asked %s", tok, a)
				}
			}
		}
	}

	if code, _ := call(t, bo, http.MethodDelete, public+people.TokensPattern+"/ghe/acme", "", same...); code != http.StatusNoContent {
		t.Fatalf("Bo removes the token: %d", code)
	}
	if _, b := call(t, bo, http.MethodGet, public+"/api/v1/repos", ""); strings.Contains(b, "acme/") {
		t.Errorf("Bo without a token: %.300s", b)
	}
}
