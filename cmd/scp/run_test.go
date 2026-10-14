package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// TestServeEndToEnd is the M1 and M2 acceptance test: a test organisation on a fake GitHub shows in /api/v1/groups, with commits,
// descriptors, the dashboard and paged feeds; a restart serves it from the store before any GitHub call; a changed pushed_at
// refetches the commits of just that repository.
func TestServeEndToEnd(t *testing.T) {
	gh := githubtest.New(t)
	gh.AddToken("ghp_test")
	core := gh.AddRepo("acme", gogh.Repository{Name: new("Whydah-Core"), Visibility: new("public"), Topics: []string{"iam"}})
	sso := gh.AddRepo("acme", gogh.Repository{Name: new("whydah-sso"), Visibility: new("public")})
	tool := gh.AddRepo("acme", gogh.Repository{Name: new("tool"), Visibility: new("public")})
	at := func(h int) time.Time { return time.Date(2026, 10, 1, h, 0, 0, 0, time.UTC) }
	commit := func(c byte, h int) githubtest.Commit {
		return githubtest.Commit{SHA: strings.Repeat(string(c), 40), Message: "change " + string(c) + "\n\nbody", AuthorName: "Ann",
			AuthorLogin: "ann", Date: at(h)}
	}
	gh.SetCommits(core, commit('a', 9), commit('b', 7), commit('c', 5))
	gh.SetCommits(sso, commit('d', 8), commit('e', 6))
	gh.SetCommits(tool, commit('f', 4))
	gh.SetFile(core, ".github/scp.yaml", "schema: 1\ntitle: Whydah Core\nlifecycle: production\n")
	gh.SetFile(core, "scp.yaml", "schema: 1\ntitle: loses to .github/\n")
	gh.SetRelease(core, "v2.0.0", "Two", at(3))
	gh.SetFile(core, "README.md", "# Whydah Core\n\nIdentity. See [setup](docs/setup.md).\n")
	gh.SetFile(core, "docs/setup.md", "# Setup\n\n## Install\n\n<script>alert(1)</script>\n")
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
		srv, err := newServer(t.Context(), cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	read := func(url string) string {
		t.Helper()
		resp, err := client.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
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
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !ok(); {
			if time.Now().After(deadline) {
				t.Fatalf("%s: not within 10s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	check := func(public string) {
		t.Helper()
		waitFor("commits and descriptors", func() bool {
			var d oapi.Dashboard
			var r oapi.RepoDetail
			var l oapi.DocList
			return fetch(public+"/api/v1/dashboard", &d) == http.StatusOK && d.Freshness.Fresh == 3 &&
				fetch(public+"/api/v1/repos/acme/whydah-core", &r) == http.StatusOK && r.Descriptor.Present &&
				fetch(public+"/api/v1/repos/acme/whydah-core/docs", &l) == http.StatusOK && len(l.Docs) == 2
		})
		var doc oapi.Doc
		if code := fetch(public+"/api/v1/repos/acme/whydah-core/docs/docs%2Fsetup.md", &doc); code != http.StatusOK || doc.Title != "Setup" ||
			len(doc.Toc) != 1 || doc.Toc[0].Id != "doc-install" || strings.Contains(doc.Html, "script") ||
			!strings.HasSuffix(doc.SourceUrl, "/acme/Whydah-Core/blob/HEAD/docs/setup.md") {
			t.Errorf("doc: %d %+v", code, doc)
		}
		if code := fetch(public+"/api/v1/repos/acme/whydah-core/docs/nope.md", nil); code != http.StatusNotFound {
			t.Errorf("unknown document: %d", code)
		}
		waitFor("the release badge", func() bool {
			return strings.Contains(read(public+"/badges/repos/acme/whydah-core/release.svg"), ">v2.0.0<")
		})
		if b := read(public + "/badges/groups/whydah/repos.svg"); !strings.Contains(b, ">2</text>") {
			t.Errorf("group badge: %s", b)
		}
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
		var repo oapi.RepoDetail
		if code := fetch(public+"/api/v1/repos/ACME/whydah-core", &repo); code != http.StatusOK || repo.Repo.Name != "Whydah-Core" ||
			repo.Repo.Freshness.Status != "fresh" || strings.Join(repo.Repo.Groups, ",") != "whydah,other" {
			t.Errorf("repo: %d %+v", code, repo.Repo)
		}
		if repo.Title != "Whydah Core" || repo.Descriptor.File != ".github/scp.yaml" || repo.Descriptor.Lifecycle != "production" ||
			len(repo.Commits) != 3 || repo.Commits[0].Subject != "change a" || repo.CommitsFreshness.Status != "fresh" {
			t.Errorf("repo detail: title %q, descriptor %+v, commits %+v", repo.Title, repo.Descriptor, repo.Commits)
		}
		var dash oapi.Dashboard
		if code := fetch(public+"/api/v1/dashboard", &dash); code != http.StatusOK || dash.RepoCount != 3 || len(dash.Latest) != 6 ||
			dash.Latest[0].Sha != strings.Repeat("a", 40) || len(dash.Groups) != 2 || dash.Groups[0].LastActivity == nil ||
			!dash.Groups[0].LastActivity.Equal(at(9)) {
			t.Errorf("dashboard: %d %+v", code, dash)
		}
		var shas []string
		url := public + "/api/v1/commits?group=whydah&limit=2"
		for pages := 0; ; pages++ {
			var page oapi.CommitPage
			if code := fetch(url, &page); code != http.StatusOK || pages > 3 || page.Freshness.Status != "fresh" {
				t.Fatalf("commit page %d: %d %+v", pages, code, page)
			}
			for _, c := range page.Commits {
				shas = append(shas, c.Sha[:1])
			}
			if page.Page.NextCursor == nil {
				break
			}
			url = public + "/api/v1/commits?group=whydah&limit=2&cursor=" + *page.Page.NextCursor
		}
		if got := strings.Join(shas, ""); got != "adbec" {
			t.Errorf("whydah feed = %s, want adbec", got)
		}
		if code := fetch(public+"/api/v1/commits?group=other&cursor=eyJrIjoieCIsInMiOiJ5IiwidCI6MH0", nil); code != http.StatusBadRequest {
			t.Errorf("a cursor of another filter: %d, want 400", code)
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

	coreFetches, toolFetches := gh.Count("GET /repos/acme/whydah-core/commits"), gh.Count("GET /repos/acme/tool/commits")
	gh.SetCommits(tool, commit('9', 10), commit('f', 4))
	gh.Update(tool, func(r *gogh.Repository) { r.PushedAt = &gogh.Timestamp{Time: at(10)} })
	cfg.Catalog.ReconcileInterval = 100 * time.Millisecond
	stop, public, admin = serve()
	waitReady(admin)
	waitFor("the pushed repository's new commit", func() bool {
		var r oapi.RepoDetail
		return fetch(public+"/api/v1/repos/acme/tool", &r) == http.StatusOK && len(r.Commits) == 2
	})
	stop()
	if got := gh.Count("GET /repos/acme/tool/commits"); got != toolFetches+1 {
		t.Errorf("tool commits fetched %d times after the push, want 1", got-toolFetches)
	}
	if got := gh.Count("GET /repos/acme/whydah-core/commits"); got != coreFetches {
		t.Errorf("an untouched repository's commits were fetched again: %d", got-coreFetches)
	}

	// A GitHub outage serves the last good data, marked stale; once GitHub is back a signed push lands within seconds.
	secret := filepath.Join(dir, "webhook-secret")
	if err := os.WriteFile(secret, []byte("hooks"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body+"webhook: {secret_file: "+secret+"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, _, err = config.Load(path, nil); err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"
	push := func(public, delivery, name string) {
		t.Helper()
		body := `{"ref":"refs/heads/main","repository":{"name":"` + name + `","default_branch":"main","owner":{"login":"acme"}}}`
		m := hmac.New(sha256.New, []byte("hooks"))
		m.Write([]byte(body))
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, public+"/webhooks/github", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-GitHub-Delivery", delivery)
		req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(m.Sum(nil)))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("webhook: %d", resp.StatusCode)
		}
	}
	gh.Fail("GET /", http.StatusBadGateway, 1<<20, 0)
	stop, public, admin = serve()
	waitReady(admin)
	push(public, "outage-1", "Whydah-Core")
	waitFor("stale commits during the outage", func() bool {
		var r oapi.RepoDetail
		return fetch(public+"/api/v1/repos/acme/whydah-core", &r) == http.StatusOK && len(r.Commits) == 3 &&
			r.CommitsFreshness.Status == "stale"
	})
	if code := fetch(admin+"/readyz", nil); code != http.StatusOK {
		t.Errorf("readiness during the outage: %d", code)
	}
	gh.Heal()
	gh.SetCommits(sso, commit('7', 11), commit('d', 8), commit('e', 6))
	start := time.Now()
	push(public, "after-1", "whydah-sso")
	waitFor("the pushed commit", func() bool {
		var r oapi.RepoDetail
		return fetch(public+"/api/v1/repos/acme/whydah-sso", &r) == http.StatusOK && len(r.Commits) == 3
	})
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the push took %v to show", d)
	}
	exposition := read(admin + "/metrics")
	for _, want := range []string{`scp_http_requests_total{code="200",listener="public",method="GET",pattern="GET /api/v1/repos/{owner}/{name}"}`,
		`scp_webhook_deliveries_total{event="push",result="accepted"} 2`, `scp_sync_jobs_total{kind="commits",result="ok"}`,
		`scp_github_requests_total{code="502",source="gh"}`, `scp_items{freshness="fresh",kind="commits"}`, "scp_snapshot_age_seconds",
		"scp_build_info", "go_goroutines"} {
		if !strings.Contains(exposition, want) {
			t.Errorf("metrics lack %s", want)
		}
	}
	var st struct {
		Ready   bool
		Sources []struct{ ID string }
		Webhook struct{ Deliveries map[string]int }
	}
	if code := fetch(admin+"/api/v1/status", &st); code != 200 || !st.Ready || len(st.Sources) != 1 || st.Webhook.Deliveries["accepted"] != 2 {
		t.Errorf("status: %d %+v", code, st)
	}
	if code := fetch(admin+"/debug/pprof/", nil); code != http.StatusNotFound {
		t.Errorf("pprof without ops.pprof: %d", code)
	}
	if code := fetch(public+"/metrics", nil); code != http.StatusNotFound {
		t.Errorf("metrics on the public listener: %d", code)
	}
	stop()
}
