package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/auth/oidctest"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
)

// TestSignIn is the S1 acceptance test: signed in through the provider, a member of a private group sees a private repository and
// an admin reads the status; a token reads /llms.txt with its groups; signing out ends it.
func TestSignIn(t *testing.T) {
	gh := githubtest.New(t)
	gh.AddToken("ghp_test")
	gh.AddRepo("acme", gogh.Repository{Name: new("open"), Visibility: new("public")})
	gh.AddRepo("acme", gogh.Repository{Name: new("secret"), Visibility: new("private")})
	idp := oidctest.New("scp")
	defer idp.Close()

	dir := t.TempDir()
	file := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0") // a free port: the redirect URI is under server.public_url
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	public := "http://" + addr
	ci := strings.Repeat("c", 40)
	cfg, _, err := config.Load(file("scp.yaml", `store: {path: `+filepath.Join(dir, "scp.db")+`}
server: {public_url: "`+public+`"}
access: {private_groups: [staff]}
auth:
  session_key_file: `+file("key", strings.Repeat("k", 32))+`
  admin_emails: [ada@example.org]
  tokens: [{name: ci, secret_file: `+file("ci", ci)+`, groups: [staff]}]
  oidc: {issuer: "`+idp.Issuer()+`", client_id: scp, scopes: [openid, email, profile, groups]}
github:
  sources:
    - {id: gh, base_url: "`+gh.BaseURL()+`", orgs: [acme], token_file: `+file("token", "ghp_test")+`, include_private: true}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = addr, "127.0.0.1:0"
	srv, err := newServer(t.Context(), cfg, deps{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
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

	type browser struct{ *http.Client }
	newBrowser := func() browser {
		jar, _ := cookiejar.New(nil)
		return browser{&http.Client{Jar: jar, Timeout: 5 * time.Second}}
	}
	do := func(c *http.Client, method, path string, header ...string) (int, string) {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), method, public+path, nil)
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
	me := func(c *http.Client) oapi.Me {
		t.Helper()
		var m oapi.Me
		if _, b := do(c, http.MethodGet, "/api/v1/me"); json.Unmarshal([]byte(b), &m) != nil {
			t.Fatalf("me: %s", b)
		}
		return m
	}
	anon := &http.Client{Timeout: 5 * time.Second}
	defer anon.CloseIdleConnections()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, b := do(anon, http.MethodGet, "/api/v1/repos", "Authorization", "Bearer "+ci); strings.Contains(b, `"acme/secret"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not catalogued within 10s")
		}
	}
	if m := me(anon); m.SignedIn || m.Via != oapi.None || !m.SignIn.Oidc || !m.SignIn.Token {
		t.Errorf("anonymous: %+v", m)
	}
	if _, b := do(anon, http.MethodGet, "/api/v1/repos"); strings.Contains(b, "acme/secret") {
		t.Error("the public sees the private repository")
	}
	if code, _ := do(anon, http.MethodGet, "/api/v1/admin/status"); code != http.StatusUnauthorized {
		t.Errorf("anonymous status: %d", code)
	}

	// Ada is an admin by her email.
	ada := newBrowser()
	defer ada.CloseIdleConnections()
	if code, b := do(ada.Client, http.MethodGet, "/auth/login?return_to=/api/v1/repos"); code != http.StatusOK || !strings.Contains(b, "acme/secret") {
		t.Fatalf("Ada after sign-in: %d %s", code, b)
	}
	if m := me(ada.Client); !m.SignedIn || m.Name != "Ada Lovelace" || m.Via != oapi.Oidc || !m.Admin || !m.Private {
		t.Errorf("Ada: %+v", m)
	}
	if code, b := do(ada.Client, http.MethodGet, "/api/v1/admin/status"); code != http.StatusOK || !strings.Contains(b, `"ready"`) {
		t.Errorf("Ada's status: %d %s", code, b)
	}
	stay := *ada.Client
	stay.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if code, _ := do(&stay, http.MethodPost, "/auth/logout", "Sec-Fetch-Site", "same-origin"); code != http.StatusSeeOther {
		t.Errorf("sign-out: %d", code)
	}
	if m := me(ada.Client); m.SignedIn {
		t.Errorf("Ada after sign-out: %+v", m)
	}

	// Bo is in staff: private repositories, not the status.
	idp.SetUser(oidctest.User{Subject: "u-2", Email: "bo@example.org", Name: "Bo", Groups: []string{"dev", "staff"}})
	bo := newBrowser()
	defer bo.CloseIdleConnections()
	do(bo.Client, http.MethodGet, "/auth/login")
	if m := me(bo.Client); !m.Private || m.Admin {
		t.Errorf("Bo: %+v", m)
	}
	if code, _ := do(bo.Client, http.MethodGet, "/api/v1/admin/status"); code != http.StatusForbidden {
		t.Errorf("Bo's status: %d", code)
	}

	// The token reads the Markdown surface with its groups; a wrong one is refused, never served anonymously.
	if code, b := do(anon, http.MethodGet, "/llms.txt", "Authorization", "Bearer "+ci); code != http.StatusOK || !strings.Contains(b, "acme/secret") {
		t.Errorf("token llms.txt: %d %s", code, b)
	}
	if code, _ := do(anon, http.MethodGet, "/llms.txt", "Authorization", "Bearer wrong"); code != http.StatusUnauthorized {
		t.Errorf("a wrong token: %d", code)
	}
}
