package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	gosync "sync"
	"testing"
	"testing/synctest"
	"time"

	gogh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

const loadRepos = 1000

// inMemory calls the fake GitHub's handler directly, so a synctest bubble can run the whole engine on fake time.
type inMemory struct{ h http.Handler }

func (m inMemory) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	m.h.ServeHTTP(rec, req)
	resp := rec.Result()
	resp.Request = req
	return resp, nil
}

// TestLoad1000 is the M4 load check: a cold start of 1,000 repositories completes within the GitHub budget without hitting the rate
// limit, a warm start serves them at once, and every page of the API answers quickly under concurrent load.
func TestLoad1000(t *testing.T) {
	gh := githubtest.New(t)
	gh.AddToken("ghp_load")
	commit := func(i, n int) githubtest.Commit {
		return githubtest.Commit{SHA: fmt.Sprintf("%040x", i*10+n), Message: fmt.Sprintf("change %d", n), AuthorName: "Ann", AuthorLogin: "ann",
			Date: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i*10+n) * time.Minute)}
	}
	for i := range loadRepos {
		name := fmt.Sprintf("g%d-svc-%03d", i%10, i)
		id := gh.AddRepo("acme", gogh.Repository{Name: new(name), Visibility: new("public"), Description: new("Service " + name)})
		gh.SetCommits(id, commit(i, 3), commit(i, 2), commit(i, 1))
		gh.SetFile(id, "README.md", "# "+name+"\n\nWhat "+name+" does.\n\n## Use\n\nRun it.\n")
		if i%2 == 0 {
			gh.SetRelease(id, "v1.0."+fmt.Sprint(i), "Release", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
		}
		if i%10 == 0 {
			gh.SetFile(id, "docs/guide.md", "# Guide\n\nSteps.\n")
			gh.SetFile(id, ".github/scp.yaml", "schema: 1\nlifecycle: production\n")
		}
	}
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("ghp_load"), 0o600); err != nil {
		t.Fatal(err)
	}
	var groups strings.Builder
	for g := range 10 {
		fmt.Fprintf(&groups, "    - {id: g%d, name: Group %d, default_repo: g%d-svc-%03d, match: [{prefix: g%d-}]}\n", g, g, g, g, g)
	}
	path := filepath.Join(dir, "scp.yaml")
	body := "store: {path: " + filepath.Join(dir, "scp.db") + "}\nsync: {concurrency: 8}\ngithub:\n  sources:\n    - {id: gh, base_url: \"" +
		gh.BaseURL() + "\", orgs: [acme], token_file: " + token + "}\ncatalog:\n  groups:\n" + groups.String() +
		"    - {id: all, name: All, default_repo: g0-svc-000, match: [{catch_all: true}]}\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.PublicAddr, cfg.Server.AdminAddr = "127.0.0.1:0", "127.0.0.1:0"
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Cold start on fake time.
	synctest.Test(t, func(t *testing.T) {
		srv, err := newServer(t.Context(), cfg, deps{github: inMemory{gh.Config.Handler}}, log)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- srv.engine.Run(ctx) }()
		start := time.Now()
		for !complete(srv.pub.Current()) {
			if time.Since(start) > 4*time.Hour {
				t.Fatalf("cold start incomplete after %v", time.Since(start))
			}
			time.Sleep(30 * time.Second)
		}
		took := time.Since(start)
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		srv.pub.Close()
		_ = srv.publicLn.Close()
		_ = srv.adminLn.Close()
		if err := srv.store.Close(); err != nil {
			t.Error(err)
		}
		ledger := gh.Ledger()
		limited := slices.IndexFunc(ledger, func(l string) bool { return strings.HasSuffix(l, " 403") || strings.HasSuffix(l, " 429") })
		charged := len(slices.DeleteFunc(slices.Clone(ledger), func(l string) bool { return strings.HasSuffix(l, " 304") }))
		t.Logf("cold start: %v of GitHub time, %d calls (%d charged)", took, len(ledger), charged)
		if limited >= 0 {
			t.Errorf("the rate limit was hit: %s", ledger[limited])
		}
		if charged > 5*loadRepos {
			t.Errorf("cold start was charged %d calls, want at most %d", charged, 5*loadRepos)
		}
	})

	// Warm start on real time, then concurrent load on the public handler.
	srv, err := newServer(t.Context(), cfg, deps{}, log)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	warm := time.Now()
	for !loaded(srv.pub.Current()) {
		if time.Since(warm) > 10*time.Second*slowdown {
			t.Fatal("the warm start did not serve the stored catalogue")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("warm start: %v", time.Since(warm))
	pages := []string{"/api/v1/dashboard", "/api/v1/groups", "/api/v1/groups/g3", "/api/v1/repos", "/api/v1/repos?group=g5",
		"/api/v1/repos/acme/g3-svc-003", "/api/v1/repos/acme/g3-svc-003/docs", "/api/v1/commits?limit=200",
		"/api/v1/commits?group=g7&limit=50", "/api/v1/snapshot", "/badges/repos/acme/g4-svc-004/last-commit.svg",
		"/badges/groups/all/repos.svg", "/llms.txt", "/llms-full.txt", "/repos/acme/g0-svc-010/readme.md"}
	var mu gosync.Mutex
	took := map[string][]time.Duration{}
	var wg gosync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 25 {
				for _, p := range pages {
					req := httptest.NewRequest(http.MethodGet, p, nil)
					req.Header.Set("Accept-Encoding", "gzip")
					rec := httptest.NewRecorder()
					t0 := time.Now()
					srv.public.Handler.ServeHTTP(rec, req)
					d := time.Since(t0)
					if rec.Code != http.StatusOK || rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
						!strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'none'") {
						t.Errorf("%s: %d %v", p, rec.Code, rec.Header())
						return
					}
					mu.Lock()
					took[p] = append(took[p], d)
					mu.Unlock()
				}
			}
		})
	}
	wg.Wait()
	for _, p := range pages {
		ds := took[p]
		slices.Sort(ds)
		p99 := ds[len(ds)*99/100]
		if bound := 100 * time.Millisecond * slowdown; p99 > bound {
			t.Errorf("%s: p99 %v over %v", p, p99, bound)
		}
	}
}

// complete reports whether every repository is catalogued with fresh commits, documents, descriptor and release.
func complete(s *snapshot.Snapshot) bool {
	if len(s.Repos) != loadRepos {
		return false
	}
	now := time.Now()
	for _, e := range s.Repos {
		for _, f := range []snapshot.Freshness{e.CommitsFresh, e.DocsFresh, e.DescriptorFresh, e.ReleaseFresh} {
			if f.Derive(now) != snapshot.StatusFresh {
				return false
			}
		}
	}
	return true
}

// loaded reports whether the stored catalogue with its commits and READMEs is in the snapshot (its times are the bubble's, long past).
func loaded(s *snapshot.Snapshot) bool {
	if !s.Catalogued || len(s.Repos) != loadRepos {
		return false
	}
	for _, e := range s.Repos {
		if len(e.Commits) != 3 || len(e.Docs) == 0 || !e.Docs[0].README {
			return false
		}
	}
	return true
}
