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

	gogh "github.com/google/go-github/v92/github"

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
	idp := oidctest.New("scp-e2e")
	defer idp.Close()
	idp.SetUser(oidctest.User{Subject: "u-1", Email: "ada@example.org", Name: "Ada Lovelace", Groups: []string{"staff"}})
	start := time.Now().Add(-48 * time.Hour)
	history := map[int64][]githubtest.Commit{}
	for i, id := range []int64{core, sso, tool} {
		var cs []githubtest.Commit
		for j := range 30 {
			cs = append(cs, githubtest.Commit{SHA: fmt.Sprintf("%02d%038d", i, 30-j), Message: fmt.Sprintf("Change %d of repository %d", 30-j, i),
				AuthorName: "Ann Example", AuthorLogin: "ann", Date: start.Add(time.Duration(30-j)*time.Hour + time.Duration(i)*time.Minute)})
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

	bundle, err := filepath.Abs("../../web/bundle")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	token, key, e2eToken := filepath.Join(dir, "token"), filepath.Join(dir, "session-key"), filepath.Join(dir, "e2e-token")
	path := filepath.Join(dir, "scp.yaml")
	body := `store: {path: ` + filepath.Join(dir, "scp.db") + `}
server: {public_addr: "` + addr + `", admin_addr: "127.0.0.1:0", public_url: "http://` + addr + `"}
web: {dir: ` + bundle + `}
access: {private_groups: [staff]}
auth:
  session_key_file: ` + key + `
  admin_emails: [ada@example.org]
  tokens: [{name: e2e, secret_file: ` + e2eToken + `, groups: [staff]}]
  oidc: {issuer: "` + idp.Issuer() + `", client_id: scp-e2e, scopes: [openid, email, profile, groups]}
github:
  sources:
    - {id: gh, base_url: "` + gh.BaseURL() + `", orgs: [acme], token_file: ` + token + `, include_private: true}
catalog:
  groups:
    - {id: whydah, name: Whydah, description: Identity and access, default_repo: Whydah-Core, match: [{prefix: whydah-}]}
    - {id: other, name: Other, default_repo: tool, match: [{catch_all: true}]}
`
	if os.WriteFile(token, []byte("ghp_e2e"), 0o600) != nil || os.WriteFile(path, []byte(body), 0o600) != nil ||
		os.WriteFile(key, []byte("e2e-session-key-e2e-session-key-0123"), 0o600) != nil ||
		os.WriteFile(e2eToken, []byte("e2e-token-0123456789abcdef0123456789abcdef"), 0o600) != nil {
		t.Fatal("write config")
	}
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := newServer(t.Context(), cfg, deps{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
