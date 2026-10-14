package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
)

// TestPrivateRepositories is the M8 acceptance test: a private repository is absent from every public surface and present for a
// request from the trusted proxy that names a permitted group.
func TestPrivateRepositories(t *testing.T) {
	gh := githubtest.New(t)
	gh.AddToken("ghp_test")
	open := gh.AddRepo("acme", gogh.Repository{Name: new("open"), Visibility: new("public")})
	secret := gh.AddRepo("acme", gogh.Repository{Name: new("secret"), Visibility: new("private")})
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	gh.SetCommits(open, githubtest.Commit{SHA: strings.Repeat("a", 40), Message: "open change", AuthorName: "Ann", Date: at})
	gh.SetCommits(secret, githubtest.Commit{SHA: strings.Repeat("b", 40), Message: "secret change", AuthorName: "Ann", Date: at.Add(time.Hour)})
	gh.SetFile(secret, "README.md", "# Secret\n")

	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("ghp_test"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "scp.yaml")
	body := `store: {path: ` + filepath.Join(dir, "scp.db") + `}
server: {trusted_proxies: [127.0.0.1/32]}
access: {private_groups: [staff]}
github:
  sources:
    - {id: gh, base_url: "` + gh.BaseURL() + `", orgs: [acme], token_file: ` + token + `, include_private: true}
catalog:
  groups:
    - {id: all, name: All, default_repo: secret, match: [{catch_all: true}]}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"
	srv, err := newServer(t.Context(), cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	public := "http://" + srv.publicLn.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	// get fetches as the anonymous public, or as a staff member through the proxy (the test is the proxy: it is on 127.0.0.1).
	get := func(path string, staff bool) (int, string, http.Header) {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, public+path, nil)
		if staff {
			req.Header.Set("X-Forwarded-User", "ann")
			req.Header.Set("X-Forwarded-Groups", "dev, staff")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), resp.Header
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var d oapi.Dashboard
		_, b, _ := get("/api/v1/dashboard", true)
		if json.Unmarshal([]byte(b), &d) == nil && d.Freshness.Fresh == 2 && len(d.Latest) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not loaded within 10s: %s", b)
		}
	}

	for _, tc := range []struct{ path, secret string }{
		{"/api/v1/repos", `"address":"acme/secret"`},
		{"/api/v1/groups/all", `"address":"acme/secret"`},
		{"/api/v1/commits", "secret change"},
		{"/api/v1/dashboard", "secret change"},
		{"/llms.txt", "acme/secret"},
		{"/llms-full.txt", "# Secret"},
	} {
		if code, b, _ := get(tc.path, false); code != http.StatusOK || strings.Contains(b, tc.secret) {
			t.Errorf("public %s: %d, shows the private repository:\n%s", tc.path, code, b)
		}
		code, b, h := get(tc.path, true)
		if code != http.StatusOK || !strings.Contains(b, tc.secret) || !strings.HasPrefix(h.Get("Cache-Control"), "private") {
			t.Errorf("staff %s: %d %q, does not show the private repository:\n%s", tc.path, code, h.Get("Cache-Control"), b)
		}
	}
	for _, path := range []string{"/api/v1/repos/acme/secret", "/api/v1/repos/acme/secret/docs", "/api/v1/repos/acme/secret/manifests",
		"/badges/repos/acme/secret/last-commit.svg", "/repos/acme/secret/readme.md"} {
		if code, _, _ := get(path, false); code != http.StatusNotFound {
			t.Errorf("public %s: %d, want 404", path, code)
		}
	}
	if code, b, _ := get("/api/v1/repos/acme/secret", true); code != http.StatusOK || !strings.Contains(b, `"visibility":"private"`) {
		t.Errorf("staff detail: %d %s", code, b)
	}
	var g oapi.Group
	if _, b, _ := get("/api/v1/groups/all", false); json.Unmarshal([]byte(b), &g) != nil || g.DefaultRepo != nil || g.DefaultRepoState != oapi.GroupDefaultRepoStateOk {
		t.Errorf("public group shows the private default or reports it missing: %s", b)
	}
	if _, _, h := get("/api/v1/repos", false); !strings.Contains(h.Get("Vary"), "X-Forwarded-Groups") {
		t.Errorf("Vary = %q", h.Get("Vary"))
	}
}
