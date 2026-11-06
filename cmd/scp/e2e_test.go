package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	gosync "sync"
	"syscall"
	"testing"
	"time"

	gogh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/auth/oidctest"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/sync"
)

// logoPNG is a 1x1 PNG for the README image.
const logoPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

// TestServeForPlaywright serves the portal against a fake GitHub on E2E_SCP_ADDR until it is interrupted: the server of the web
// e2e suite (web/playwright.config.ts runs it), with the web UI server of web/bundle (`pnpm -C web build`) as its child. Without
// E2E_SCP_ADDR it is skipped. E2E_CONTROL_ADDR, when set,
// serves POST /push?repo={name}&message={text}: a new commit on the fake GitHub and a push to the engine, as a webhook would.
// Sign-in is on: a fake provider signs in Ada Lovelace (ada@example.org, an admin, in staff), the token e2e-token-… is in staff,
// and staff sees the private repository vault.
func TestServeForPlaywright(t *testing.T) {
	addr := os.Getenv("E2E_SCP_ADDR")
	if addr == "" {
		t.Skip("E2E_SCP_ADDR not set")
	}
	gh := githubtest.New(t)
	gh.AddToken("ghp_e2e")
	core := gh.AddRepo("acme", gogh.Repository{Name: new("Whydah-Core"), Description: new("Identity and access server"),
		Visibility: new("public"), Topics: []string{"iam"}, License: &gogh.License{SPDXID: new("Apache-2.0")}})
	sso := gh.AddRepo("acme", gogh.Repository{Name: new("whydah-sso"), Description: new("Single sign-on"), Visibility: new("public")})
	tool := gh.AddRepo("acme", gogh.Repository{Name: new("tool"), Description: new("A tool"), Visibility: new("public")})
	gh.AddRepo("acme", gogh.Repository{Name: new("vault"), Description: new("Secrets of the staff"), Visibility: new("private")})
	legacy := gh.AddRepo("acme", gogh.Repository{Name: new("legacy-sso"), Description: new("Retired single sign-on"), Visibility: new("public"),
		Archived: new(true)})
	idp := oidctest.New("scp-e2e")
	defer idp.Close()
	idp.SetUser(oidctest.User{Subject: "u-1", Email: "ada@example.org", Name: "Ada Lovelace", Groups: []string{"staff"}})
	start := time.Now().Add(-48 * time.Hour)
	history := map[int64][]githubtest.Commit{}
	for i, id := range []int64{core, sso, tool} {
		var cs []githubtest.Commit
		for j := range 30 {
			cs = append(cs, githubtest.Commit{SHA: fmt.Sprintf("%02d%038d", i, 30-j), Message: fmt.Sprintf("Change %d of repository %d", 30-j, i),
				AuthorName: "Ann Example", AuthorLogin: "ann", Date: start.Add(time.Duration(30-j)*time.Hour + time.Duration(i)*time.Minute),
				Additions: 3 * (j + 1), Deletions: j % 4, Files: 1 + j%3}) // diff stats for the lists (design/diffstat.md)
		}
		if id == core { // a feature branch (4, 6) forked from 7 and merged back at 1, beside the main line 2, 3, 5
			for j, ps := range map[int][]int{1: {2, 4}, 2: {3}, 3: {5}, 4: {6}, 5: {7}, 6: {7}} {
				for _, p := range ps {
					cs[j].Parents = append(cs[j].Parents, cs[p].SHA)
				}
			}
			cs[1].Message = "Merge pull request #7 from acme/feature"
		}
		if id == tool { // a long history: 600 commits, of which the portal keeps 500
			for j := 30; j < 600; j++ {
				cs = append(cs, githubtest.Commit{SHA: fmt.Sprintf("%02d%038d", i, 1000+600-j), Message: fmt.Sprintf("Old change %d of repository %d", 600-j, i),
					AuthorName: "Ann Example", AuthorLogin: "ann", Date: start.Add(-time.Duration(j-30) * time.Hour)})
			}
		}
		gh.SetCommits(id, cs...)
		history[id] = cs
	}
	gh.SetCommits(legacy, githubtest.Commit{SHA: fmt.Sprintf("%040x", 0xd1), Message: "Archive the repository", AuthorName: "Ann Example",
		AuthorLogin: "ann", Date: start.Add(-400 * 24 * time.Hour)}) // the alumnus, last active long ago
	// A branch off the main line at 3, a renovate branch the portal leaves out, and tags: four at the head, one further down.
	core0 := history[core]
	gh.SetBranch(core, "feature/sso", githubtest.Commit{SHA: fmt.Sprintf("%040x", 0xb2), Message: "Bridge the SSO session",
		AuthorName: "Bo Example", AuthorLogin: "bo", Date: start.Add(27*time.Hour + 40*time.Minute)},
		githubtest.Commit{SHA: fmt.Sprintf("%040x", 0xb1), Message: "Start the SSO bridge\n\nThe bridge keeps one session per user.",
			AuthorName: "Bo Example", AuthorLogin: "bo", Date: start.Add(27*time.Hour + 30*time.Minute), Parents: []string{core0[3].SHA}})
	gh.SetBranch(core, "renovate/slf4j", githubtest.Commit{SHA: fmt.Sprintf("%040x", 0xc1), Message: "chore(deps): update slf4j",
		AuthorName: "renovate[bot]", Date: start.Add(29*time.Hour + 30*time.Minute), Parents: []string{core0[0].SHA}})
	for _, tag := range []string{"v1.3.0", "v1.3.0-rc2", "v1.3.0-rc1", "nightly"} {
		gh.SetTag(core, tag, core0[0].SHA)
	}
	gh.SetTag(core, "v1.2.0", core0[10].SHA)
	gh.SetFile(core, ".github/scp.yaml", "schema: 1\ntitle: Whydah Core\nlifecycle: production\nowners: ['@acme/iam']\ntags: [java]\n"+
		"links: [{title: Runbook, url: 'https://runbook.example/core'}]\n")
	logo, _ := base64.StdEncoding.DecodeString(logoPNG)
	gh.SetFile(core, "docs/logo.png", string(logo))
	gh.SetFile(core, "README.md", "# Whydah Core\n\n![Whydah logo](docs/logo.png)\n\nIdentity. See [setup](docs/setup.md).\n\n> [!NOTE]\n> Read the docs.\n\n"+
		"```go\nfunc main() { println(\"hi\") }\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n")
	gh.SetFile(core, "docs/setup.md", "# Setup\n\n## Install\n\nRun it.\n\n## Configure\n\n<script>alert(1)</script>\n")
	gh.SetFile(sso, "README.md", "# SSO\n")
	gh.SetFile(core, "pom.xml", "<project><groupId>no.acme</groupId><artifactId>whydah-core</artifactId><version>1.0</version>"+
		"<properties><maven.compiler.release>21</maven.compiler.release></properties><dependencies>"+
		"<dependency><groupId>org.slf4j</groupId><artifactId>slf4j-api</artifactId><version>2.0.17</version></dependency></dependencies></project>")
	gh.SetFile(sso, "pom.xml", "<project><groupId>no.acme</groupId><artifactId>whydah-sso</artifactId><dependencies>"+
		"<dependency><groupId>no.acme</groupId><artifactId>whydah-core</artifactId><version>1.0</version></dependency></dependencies></project>")
	gh.SetRelease(core, "v1.2.0", "1.2.0", start)
	// Open pull requests (design/pulls.md): one waiting for a review on the SSO branch, one ready, one failing, a draft and
	// Renovate's update.
	gh.SetPulls(core,
		githubtest.Pull{Number: 12, Title: "Bridge the SSO session", Author: "bo", Created: start.Add(20 * time.Hour), Updated: start.Add(28 * time.Hour),
			Base: "main", Head: "feature/sso", HeadSHA: fmt.Sprintf("%040x", 0xb2), Labels: []string{"enhancement"}, Requested: 1,
			Review: "REVIEW_REQUIRED", Checks: "SUCCESS", Additions: 120, Deletions: 8, Files: 4},
		githubtest.Pull{Number: 11, Title: "Explain the token lifetime", Author: "ann", Created: start, Updated: start.Add(40 * time.Hour),
			Base: "main", Head: "docs/token", HeadSHA: fmt.Sprintf("%040x", 0xd1), Approved: true, Checks: "SUCCESS", Additions: 14, Deletions: 2, Files: 1},
		githubtest.Pull{Number: 10, Title: "Draft: rotate the signing keys", Author: "ann", Draft: true, Created: start, Updated: start.Add(time.Hour),
			Base: "main", Head: "keys", HeadSHA: fmt.Sprintf("%040x", 0xd2)},
		githubtest.Pull{Number: 9, Title: "chore(deps): update slf4j", Author: "renovate[bot]", Bot: true, Created: start, Updated: start.Add(29 * time.Hour),
			Base: "main", Head: "renovate/slf4j", HeadSHA: fmt.Sprintf("%040x", 0xc1), Checks: "SUCCESS", Additions: 1, Deletions: 1, Files: 1})
	gh.SetPulls(sso, githubtest.Pull{Number: 3, Title: "Keep the session on refresh", Author: "bo", Created: start, Updated: start.Add(30 * time.Hour),
		Base: "main", Head: "refresh", HeadSHA: fmt.Sprintf("%040x", 0xe1), Approved: true, Checks: "FAILURE", Additions: 30, Deletions: 12, Files: 2})
	gh.SetIssues(core,
		githubtest.Issue{Number: 21, Title: "Login loops after a password reset", Author: "cy", Created: start.Add(2 * time.Hour), Updated: start.Add(3 * time.Hour)},
		githubtest.Issue{Number: 20, Title: "Tokens expire an hour early", Author: "ann", Created: start, Updated: start.Add(26 * time.Hour),
			Labels: []string{"bug"}, Comments: 3},
		githubtest.Issue{Number: 19, Title: "Support passkeys", Author: "bo", Created: start, Updated: start.Add(10 * time.Hour),
			Type: "Feature", Assignees: []string{"ann"}, Milestone: "2.0"},
		githubtest.Issue{Number: 2, Title: "Dependency Dashboard", Author: "renovate[bot]", Bot: true, Created: start, Updated: start.Add(29 * time.Hour)})

	bundle, err := filepath.Abs("../../web/bundle")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	token, key, e2eToken := filepath.Join(dir, "token"), filepath.Join(dir, "session-key"), filepath.Join(dir, "e2e-token")
	llmKey, model := filepath.Join(dir, "llm-key"), fakeAnthropic(t)
	path, layouts := filepath.Join(dir, "scp.yaml"), filepath.Join(dir, "layouts")
	if err := os.Mkdir(layouts, 0o700); err != nil {
		t.Fatal(err)
	}
	// The announcements: an event in six days at a fixed offset (shown as written), and a notice for signed-in viewers alone.
	now, oslo := time.Now().Truncate(time.Hour), time.FixedZone("CET", 3600)
	body := `store: {path: ` + filepath.Join(dir, "scp.db") + `}
server: {public_addr: "` + addr + `", admin_addr: "127.0.0.1:0", public_url: "http://` + addr + `"}
web: {dir: ` + bundle + `}
layout: {dir: ` + layouts + `}
access: {private_groups: [staff]}
auth:
  session_key_file: ` + key + `
  admin_emails: [ada@example.org]
  tokens: [{name: e2e, secret_file: ` + e2eToken + `, groups: [staff]}]
  oidc: {issuer: "` + idp.Issuer() + `", client_id: scp-e2e, scopes: [openid, email, profile, groups]}
llm: {provider: anthropic, model: claude-e2e, base_url: "` + model.URL + `", api_key_file: ` + llmKey + `}
github:
  sources:
    - {id: gh, base_url: "` + gh.BaseURL() + `", orgs: [acme], token_file: ` + token + `, include_private: true}
catalog:
  groups:
    - {id: whydah, name: Whydah, description: Identity and access, default_repo: Whydah-Core, match: [{prefix: whydah-}]}
    - {id: other, name: Other, default_repo: tool, match: [{catch_all: true}]}
announcements:
  - id: juleol
    title: Juleølsmaking på Rebel Makerspace
    text: Det avholdes juleølsmaking i år.
    tone: event
    starts: "` + now.Add(6*24*time.Hour).In(oslo).Format(time.RFC3339) + `"
    ends: "` + now.Add(6*24*time.Hour+5*time.Hour).In(oslo).Format(time.RFC3339) + `"
    place: Rebel Makerspace
    link: {label: Meld deg på her, url: "https://example.org/pamelding"}
    lang: nb
  - {id: vault-move, title: The vault moves to the new cluster, tone: notice, audience: signed-in, pages: [dashboard],
     until: "` + now.Add(30*24*time.Hour).Format(time.RFC3339) + `"}
`
	if os.WriteFile(token, []byte("ghp_e2e"), 0o600) != nil || os.WriteFile(path, []byte(body), 0o600) != nil ||
		os.WriteFile(key, []byte("e2e-session-key-e2e-session-key-0123"), 0o600) != nil ||
		os.WriteFile(e2eToken, []byte("e2e-token-0123456789abcdef0123456789abcdef"), 0o600) != nil ||
		os.WriteFile(llmKey, []byte("sk-test"), 0o600) != nil {
		t.Fatal("write config")
	}
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	registry := newFakeRegistry(t) // both publish to Maven Central: whydah-sso shows its package where it has no GitHub release
	for _, a := range []string{"whydah-core", "whydah-sso"} {
		registry.set("repo1.maven.org", "/maven2/no/acme/"+a+"/maven-metadata.xml",
			"<metadata><versioning><release>1.4.0</release><lastUpdated>"+start.Add(-48*time.Hour).UTC().Format("20060102150405")+"</lastUpdated></versioning></metadata>")
	}
	srv, err := newServer(t.Context(), cfg, deps{registries: registry}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if control := os.Getenv("E2E_CONTROL_ADDR"); control != "" {
		repos := map[string]int64{"whydah-core": core, "whydah-sso": sso, "tool": tool}
		var mu gosync.Mutex
		mux := http.NewServeMux()
		mux.HandleFunc("POST /push", func(w http.ResponseWriter, r *http.Request) {
			name := r.URL.Query().Get("repo")
			id, ok := repos[name]
			if !ok {
				http.NotFound(w, r)
				return
			}
			mu.Lock()
			history[id] = append([]githubtest.Commit{{SHA: fmt.Sprintf("%040x", time.Now().UnixNano()), Message: r.URL.Query().Get("message"),
				AuthorName: "Ann Example", AuthorLogin: "ann", Date: time.Now()}}, history[id]...)
			gh.SetCommits(id, history[id]...)
			mu.Unlock()
			srv.engine.Notify(sync.EventPush, snapshot.KeyOf("acme", name))
			w.WriteHeader(http.StatusNoContent)
		})
		ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", control)
		if err != nil {
			t.Fatal(err)
		}
		cs := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = cs.Serve(ln) }()
		defer func() { _ = cs.Shutdown(context.Background()) }()
	}
	if err := srv.run(ctx); err != nil {
		t.Fatal(err)
	}
}
