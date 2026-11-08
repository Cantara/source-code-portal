package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
)

// Forks are listed always and hidden unless a source shows them or a group names one: in every view, the counts, the groups, the
// search and the badges, until the request asks for them with ?forks=show.
func TestForksOnDemand(t *testing.T) {
	gh := githubtest.New(t)
	gh.AddToken("ghp_test")
	for _, r := range []struct {
		name string
		fork bool
	}{{"core", false}, {"tools", false}, {"Reporter", true}, {"LibSearch", true}, {"LibIndex", true}, {"upstream-copy", true}} {
		gh.AddRepo("acme", gogh.Repository{Name: new(r.name), Fork: new(r.fork), Visibility: new("public")})
	}
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
    - {id: libs, name: Libs, default_repo: LibSearch, match: [{prefix: Lib}]}
    - {id: tools, name: Tools, default_repo: tools, match: [{exact: tools}, {exact: Reporter}]}
    - {id: rest, name: Rest, default_repo: core, match: [{unclaimed: true}]}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"
	srv, err := newServer(t.Context(), cfg, testDeps(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	get := func(p string, into any) int {
		t.Helper()
		resp, err := client.Get(public + p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		if into != nil && resp.StatusCode == http.StatusOK {
			if err := json.Unmarshal(b, into); err != nil {
				t.Fatalf("%s: %v: %s", p, err, b)
			}
		}
		return resp.StatusCode
	}
	for deadline := time.Now().Add(10 * time.Second); !srv.pub.Current().Catalogued; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("not catalogued within 10s")
		}
	}

	type dash struct {
		RepoCount   int
		HiddenForks int
		Groups      []struct {
			ID          string
			RepoCount   int
			DefaultRepo *string
			Members     []struct{ Name string }
		}
	}
	names := func(d dash, group string) []string {
		var out []string
		for _, g := range d.Groups {
			if g.ID == group {
				for _, m := range g.Members {
					out = append(out, m.Name)
				}
			}
		}
		slices.Sort(out)
		return out
	}

	var d dash
	if code := get("/api/v1/dashboard", &d); code != http.StatusOK {
		t.Fatalf("dashboard: %d", code)
	}
	// LibSearch (a group's default) and Reporter (an exact matcher) are pinned; LibIndex and upstream-copy are hidden.
	if d.RepoCount != 4 || d.HiddenForks != 2 || !slices.Equal(names(d, "libs"), []string{"LibSearch"}) ||
		!slices.Equal(names(d, "tools"), []string{"Reporter", "tools"}) || !slices.Equal(names(d, "rest"), []string{"core"}) {
		t.Errorf("the default view: %d repositories, %d hidden, libs %v, tools %v, rest %v", d.RepoCount, d.HiddenForks, names(d, "libs"),
			names(d, "tools"), names(d, "rest"))
	}
	var shown dash
	get("/api/v1/dashboard?forks=show", &shown)
	if shown.RepoCount != 6 || shown.HiddenForks != 2 || !slices.Equal(names(shown, "libs"), []string{"LibIndex", "LibSearch"}) ||
		!slices.Equal(names(shown, "rest"), []string{"core", "upstream-copy"}) {
		t.Errorf("with forks: %d repositories, %d hidden, libs %v, rest %v", shown.RepoCount, shown.HiddenForks, names(shown, "libs"), names(shown, "rest"))
	}

	var g struct {
		RepoCount, HiddenForks int
		DefaultRepoState       string
	}
	if get("/api/v1/groups/libs", &g); g.RepoCount != 1 || g.HiddenForks != 1 || g.DefaultRepoState != "ok" {
		t.Errorf("group libs: %+v", g)
	}
	// A hidden fork's address is an address that does not exist, until the request asks for forks.
	if code := get("/api/v1/repos/acme/upstream-copy", nil); code != http.StatusNotFound {
		t.Errorf("a hidden fork: %d", code)
	}
	if code := get("/api/v1/repos/acme/upstream-copy?forks=show", nil); code != http.StatusOK {
		t.Errorf("a hidden fork with forks=show: %d", code)
	}
	if code := get("/api/v1/repos/acme/reporter", nil); code != http.StatusOK {
		t.Errorf("a pinned fork: %d", code)
	}
	var found struct{ Hits []json.RawMessage }
	get("/api/v1/search?q=upstream-copy", &found)
	if strings.Contains(strings.ToLower(toJSON(found)), "upstream-copy") {
		t.Errorf("search finds a hidden fork: %+v", found)
	}
	get("/api/v1/search?q=upstream-copy&forks=show", &found)
	if !strings.Contains(strings.ToLower(toJSON(found)), "upstream-copy") {
		t.Errorf("search with forks=show does not find the fork: %+v", found)
	}
	if code := get("/badges/repos/acme/upstream-copy/license.svg", nil); code != http.StatusNotFound {
		t.Errorf("a hidden fork's badge: %d", code)
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
