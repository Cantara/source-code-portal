package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v92/github"

	contract "github.com/cantara/source-code-portal/api"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
)

// The private repository's traces: its name, its commit subject, its README and its description. None may reach a public viewer.
var hiddenMarks = []string{"zq-hidden", "zq hidden", "Zq Hidden", "q7 vault"} //nolint:gochecknoglobals // test table

// TestNoLeak walks every GET of the API contract and every route outside it as the public: no answer carries a trace of the private
// repository, and each of its addresses answers as an address that does not exist (no existence leak). A staff member sees it.
func TestNoLeak(t *testing.T) {
	gh := githubtest.New(t)
	gh.AddToken("ghp_test")
	open := gh.AddRepo("acme", gogh.Repository{Name: new("open"), Visibility: new("public"), Description: new("open tools")})
	hidden := gh.AddRepo("acme", gogh.Repository{Name: new("zq-hidden"), Visibility: new("private"), Description: new("the q7 vault"),
		Topics: []string{"zq-hidden"}})
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	gh.SetCommits(open, githubtest.Commit{SHA: strings.Repeat("a", 40), Message: "open change", AuthorName: "Ann", Date: at})
	gh.SetCommits(hidden, githubtest.Commit{SHA: strings.Repeat("b", 40), Message: "zq hidden change", AuthorName: "Ann", Date: at.Add(time.Hour)})
	gh.SetFile(open, "README.md", "# Open\n")
	gh.SetFile(open, "go.mod", "module example.com/open\n\ngo 1.27\n\nrequire example.com/lib/q7 v1.0.0\n")
	gh.SetFile(hidden, "README.md", "# Zq Hidden\n\n![x](docs/x.png)\n")
	gh.SetFile(hidden, "docs/guide.md", "# Zq Hidden guide\n")
	gh.SetFile(hidden, "go.mod", "module example.com/lib/q7\n\ngo 1.27\n")

	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("ghp_test"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "scp.yaml")
	body := `store: {path: ` + filepath.Join(dir, "scp.db") + `}
server: {trusted_proxies: [127.0.0.1/32], public_url: "https://scp.example"}
access: {private_groups: [staff]}
github:
  sources:
    - {id: gh, base_url: "` + gh.BaseURL() + `", orgs: [acme], token_file: ` + token + `, include_private: true}
catalog:
  groups:
    - {id: all, name: All, default_repo: zq-hidden, match: [{catch_all: true}]}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"
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
	base := "http://" + srv.publicLn.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	get := func(path string, staff bool, accept string) (int, string) {
		t.Helper()
		rctx, stop := context.WithTimeout(t.Context(), 2*time.Second)
		defer stop()
		req, _ := http.NewRequestWithContext(rctx, http.MethodGet, base+path, nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		if staff {
			req.Header.Set("X-Forwarded-User", "ann")
			req.Header.Set("X-Forwarded-Groups", "staff")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body) // a stream ends at the deadline with what it sent
		return resp.StatusCode, string(b)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var d oapi.Dashboard
		_, b := get("/api/v1/dashboard", true, "")
		_, docs := get("/api/v1/repos/acme/zq-hidden/docs", true, "")
		_, mf := get("/api/v1/repos/acme/open/manifests", true, "")
		if json.Unmarshal([]byte(b), &d) == nil && d.Freshness.Fresh == 2 && len(d.Latest) == 2 && strings.Contains(docs, "guide") &&
			strings.Contains(mf, "zq-hidden") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not loaded within 10s: %s\n%s\n%s", b, docs, mf)
		}
	}

	// Every GET of the contract, with the private repository and with one that does not exist.
	param := regexp.MustCompile(`\{(\w+)\}`)
	var paths []string
	for _, m := range regexp.MustCompile(`(?m)^  (/\S+):\n    get:`).FindAllStringSubmatch(string(contract.OpenAPI), -1) {
		paths = append(paths, "/api/v1"+param.ReplaceAllStringFunc(m[1], func(p string) string {
			return map[string]string{"{owner}": "acme", "{name}": "zq-hidden", "{group}": "all", "{docId}": "docs%2Fguide.md"}[p]
		}))
	}
	if len(paths) < 12 {
		t.Fatalf("contract paths: %v", paths)
	}
	paths = append(paths, "/llms.txt", "/llms-full.txt", "/knowledge.yaml", "/.well-known/kcp.json", "/sitemap.xml", "/sitemaps/1.xml",
		"/robots.txt", "/groups/all.md", "/repos/acme/open/readme.md", "/repos/acme/zq-hidden/readme.md", "/repos/acme/zq-hidden/docs/docs/guide.md",
		"/badges/repos/acme/zq-hidden/last-commit.svg", "/badges/repos/acme/zq-hidden/release.svg", "/badges/repos/acme/zq-hidden/license.svg",
		"/badges/repos/acme/zq-hidden/issues.svg", "/badges/repos/acme/zq-hidden/health.svg", "/badges/groups/all/repos.svg",
		"/badges/groups/all/health.svg", "/badges/groups/all/last-commit.svg", "/badges/groups/all/release.svg",
		"/images/acme/zq-hidden/00000000000000000000000000000000", "/badge/license/zq-hidden/main", "/commits/zq-hidden",
		"/commits/acme/zq-hidden", "/contents/acme/zq-hidden/main", "/api/v1/events")
	clean := regexp.MustCompile(`urn:scp:request:\w+`)
	for _, p := range paths {
		for _, accept := range []string{"", "text/markdown"} {
			code, b := get(p, false, accept)
			if !strings.Contains(p, "zq-hidden") {
				for _, m := range hiddenMarks {
					if strings.Contains(b, m) {
						t.Errorf("public %s (%s): %d carries %q:\n%s", p, accept, code, m, b)
					}
				}
				continue
			}
			// An address naming the repository may echo it; it must answer word for word as one that does not exist.
			absent := strings.ReplaceAll(p, "zq-hidden", "nothere")
			ac, ab := get(absent, false, accept)
			if ac != code || clean.ReplaceAllString(ab, "") != clean.ReplaceAllString(strings.ReplaceAll(b, "zq-hidden", "nothere"), "") {
				t.Errorf("public %s (%s) answers %d, unlike an address that does not exist (%d):\n%s\n%s", p, accept, code, ac, b, ab)
			}
		}
	}
	for _, p := range []string{"/api/v1/repos/acme/zq-hidden", "/llms.txt", "/knowledge.yaml", "/api/v1/repos/acme/open/manifests"} {
		if _, b := get(p, true, ""); !strings.Contains(b, "zq-hidden") {
			t.Errorf("staff %s does not show the private repository:\n%s", p, b)
		}
	}
}
