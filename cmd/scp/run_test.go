package main

import (
	"bytes"
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
	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func execute(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(t.Context(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// configFile writes a valid configuration (one token source) followed by extra YAML and returns its path.
func configFile(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	path := filepath.Join(dir, "scp.yaml")
	body := "github:\n  sources:\n    - {id: gh, orgs: [acme], token_file: " + token + "}\n" + extra
	if os.WriteFile(token, []byte("ghp_test"), 0o600) != nil || os.WriteFile(path, []byte(body), 0o600) != nil {
		t.Fatal("write config")
	}
	return path
}

func TestExitCodes(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.yaml")
	valid := configFile(t, "")
	tests := []struct {
		args []string
		code int
	}{
		{[]string{"version"}, exitOK},
		{[]string{"--help"}, exitOK},
		{[]string{"config", "check", "-c", valid}, exitOK},
		{[]string{"config", "check"}, exitUsage}, // no sources
		{[]string{"bogus"}, exitUsage},
		{[]string{"version", "extra"}, exitUsage},
		{[]string{"serve", "--no-such-flag"}, exitUsage},
		{[]string{"config", "check", "-c", valid, "--log-level", "loud"}, exitUsage},
		{[]string{"config", "check", "--config", missing}, exitFail},
	}
	for _, tc := range tests {
		if code, _, stderr := execute(t, tc.args...); code != tc.code {
			t.Errorf("scp %s: exit %d, want %d (stderr %q)", strings.Join(tc.args, " "), code, tc.code, stderr)
		}
	}
}

func TestVersionJSON(t *testing.T) {
	code, stdout, _ := execute(t, "version", "--json")
	var bi BuildInfo
	if code != exitOK || json.Unmarshal([]byte(stdout), &bi) != nil || bi.Version != "dev" || bi.SPA != "placeholder" {
		t.Fatalf("version --json: exit %d, %q", code, stdout)
	}
}

func TestConfigCheckPrintsEffectiveSettings(t *testing.T) {
	path := configFile(t, "server:\n  public_addr: \":8080\"\n")
	code, stdout, stderr := execute(t, "config", "check", "-c", path)
	if code != exitOK || !strings.Contains(stdout, `public_addr: :8080`) || !strings.Contains(stdout, "admin_addr: :9091") {
		t.Fatalf("config check: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestConfigCheckRefusesBadGroups(t *testing.T) {
	path := configFile(t, "catalog:\n  groups:\n    - {id: a, name: A, default_repo: other, match: [{prefix: whydah-}]}\n")
	code, _, stderr := execute(t, "config", "check", "-c", path)
	if code != exitUsage || !strings.Contains(stderr, "not selected") {
		t.Errorf("exit %d, stderr %q; want refused with the group problem", code, stderr)
	}
}

// TestServeEndToEnd is the M1 acceptance test: a test organisation on a fake GitHub shows in /api/v1/groups, and a restart serves it
// from the store before any GitHub call.
func TestServeEndToEnd(t *testing.T) {
	gh := githubtest.New(t)
	gh.AddToken("ghp_test")
	gh.AddRepo("acme", gogh.Repository{Name: new("Whydah-Core"), Visibility: new("public"), Topics: []string{"iam"}})
	gh.AddRepo("acme", gogh.Repository{Name: new("whydah-sso"), Visibility: new("public")})
	gh.AddRepo("acme", gogh.Repository{Name: new("tool"), Visibility: new("public")})
	gh.AddRepo("acme", gogh.Repository{Name: new("secret"), Visibility: new("private")})

	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("ghp_test"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "scp.yaml")
	body := `store: {path: ` + filepath.Join(dir, "scp.db") + `}
github:
  sources:
    - {id: gh, base_url: "` + gh.BaseURL() + `", orgs: [acme], token_file: ` + token + `}
catalog:
  groups:
    - {id: whydah, name: Whydah, default_repo: Whydah-Core, match: [{prefix: whydah-}]}
    - {id: other, name: Other, default_repo: tool, match: [{catch_all: true}]}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"

	serve := func() (stop func(), public, admin string) {
		srv, err := newServer(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- srv.run(ctx) }()
		return func() {
			cancel()
			if err := <-done; err != nil {
				t.Errorf("run: %v", err)
			}
		}, "http://" + srv.publicLn.Addr().String(), "http://" + srv.adminLn.Addr().String()
	}
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	fetch := func(url string, v any) int {
		t.Helper()
		resp, err := client.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if v != nil && resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
				t.Fatal(err)
			}
		}
		return resp.StatusCode
	}
	waitReady := func(admin string) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); fetch(admin+"/readyz", nil) != http.StatusOK; {
			if time.Now().After(deadline) {
				t.Fatal("not ready within 10s")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	check := func(public string) {
		t.Helper()
		var groups oapi.GroupList
		if code := fetch(public+"/api/v1/groups", &groups); code != http.StatusOK || len(groups.Groups) != 2 {
			t.Fatalf("groups: %d %+v", code, groups)
		}
		if w := groups.Groups[0]; w.Id != "whydah" || w.RepoCount != 2 || w.DefaultRepo == nil || *w.DefaultRepo != "acme/whydah-core" {
			t.Errorf("whydah = %+v", w)
		}
		if o := groups.Groups[1]; o.RepoCount != 3 {
			t.Errorf("other has %d repositories, want 3 (the private one is not admitted)", o.RepoCount)
		}
		var repo oapi.Repo
		if code := fetch(public+"/api/v1/repos/ACME/whydah-core", &repo); code != http.StatusOK || repo.Name != "Whydah-Core" ||
			repo.Freshness.Status != "fresh" || strings.Join(repo.Groups, ",") != "whydah,other" {
			t.Errorf("repo: %d %+v", code, repo)
		}
		if code := fetch(public+"/api/v1/repos/acme/secret", nil); code != http.StatusNotFound {
			t.Errorf("private repository: %d, want 404", code)
		}
		if code := fetch(public+"/", nil); code != http.StatusOK {
			t.Errorf("shell: %d", code)
		}
	}

	stop, public, admin := serve()
	waitReady(admin)
	check(public)
	stop()
	listings := gh.Count("GET /orgs/acme/repos")

	stop, public, admin = serve()
	waitReady(admin)
	check(public)
	stop()
	if got := gh.Count("GET /orgs/acme/repos"); got != listings {
		t.Errorf("the warm start listed again: %d listings, want %d", got, listings)
	}
}
