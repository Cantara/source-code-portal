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
		gh.SetCommits(id, cs...)
		history[id] = cs
	}
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
	token := filepath.Join(dir, "token")
	path := filepath.Join(dir, "scp.yaml")
	body := `store: {path: ` + filepath.Join(dir, "scp.db") + `}
server: {public_addr: "` + addr + `", admin_addr: "127.0.0.1:0"}
web: {dir: ` + bundle + `}
github:
  sources:
    - {id: gh, base_url: "` + gh.BaseURL() + `", orgs: [acme], token_file: ` + token + `}
catalog:
  groups:
    - {id: whydah, name: Whydah, description: Identity and access, default_repo: Whydah-Core, match: [{prefix: whydah-}]}
    - {id: other, name: Other, default_repo: tool, match: [{catch_all: true}]}
`
	if os.WriteFile(token, []byte("ghp_e2e"), 0o600) != nil || os.WriteFile(path, []byte(body), 0o600) != nil {
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
