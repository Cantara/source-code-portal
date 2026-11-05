package commits

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

func TestFromGitHub(t *testing.T) {
	srv := githubtest.New(t)
	id := srv.AddRepo("acme", gh.Repository{Name: new("one")})
	srv.AddRepo("acme", gh.Repository{Name: new("empty")})
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	srv.SetCommits(id, githubtest.Commit{SHA: strings.Repeat("b", 40), Message: "second\n\nbody", AuthorName: "Ann", AuthorLogin: "ann", Date: at},
		githubtest.Commit{SHA: strings.Repeat("a", 40), Message: "first", AuthorName: "Bo", Date: at.Add(-time.Hour)})
	srv.AddToken("ghp_test")
	c, err := github.New(config.GitHub{Sources: []config.Source{{ID: "gh", BaseURL: srv.BaseURL(), Orgs: []string{"acme"}, TokenFile: "x",
		Token: config.NewSecret([]byte("ghp_test"))}}}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	f := FromGitHub(c.Source("gh"))
	cs, more, err := f.Commits(t.Context(), "acme", "one", "", 1)
	if err != nil || more || len(cs) != 2 || cs[0].Message != "second\n\nbody" || cs[0].AuthorLogin != "ann" || !cs[0].Date.Equal(at) ||
		!strings.HasSuffix(cs[0].HTMLURL, "/commit/"+strings.Repeat("b", 40)) {
		t.Fatalf("commits = %+v, %v", cs, err)
	}
	if len(cs[0].Parents) != 1 || cs[0].Parents[0] != strings.Repeat("a", 40) || len(cs[1].Parents) != 0 {
		t.Errorf("parents = %v, %v; want the linear history b -> a", cs[0].Parents, cs[1].Parents)
	}
	if _, _, err := f.Commits(t.Context(), "acme", "empty", "", 1); !github.IsNotFound(err) {
		t.Errorf("empty repository: err = %v, want not found", err)
	}
	// The diff stats come through GraphQL beside the REST base, by object id; a commit GitHub has none for is left out.
	srv.SetCommits(id, githubtest.Commit{SHA: strings.Repeat("b", 40), Message: "second", AuthorName: "Ann", Date: at, Additions: 12, Deletions: 3, Files: 2},
		githubtest.Commit{SHA: strings.Repeat("a", 40), Message: "first", AuthorName: "Bo", Date: at.Add(-time.Hour)})
	stats, err := f.Stats(t.Context(), "acme", "one", []string{strings.Repeat("b", 40), strings.Repeat("a", 40), strings.Repeat("c", 40)})
	if err != nil || len(stats) != 1 || stats[strings.Repeat("b", 40)] != (Stats{Additions: 12, Deletions: 3, Files: 2}) {
		t.Errorf("stats = %v, %v", stats, err)
	}
	if _, err := f.Stats(t.Context(), "acme", "gone", []string{strings.Repeat("b", 40)}); err == nil {
		t.Error("stats of a repository GitHub cannot resolve: no error")
	}
}

// A page of squashed pull requests, each message tens of kilobytes, streams: each commit keeps its message's start and its closing
// trailers, so the commit is still known as made with an AI.
func TestFromGitHubReadsLongMessages(t *testing.T) {
	srv := githubtest.New(t)
	id := srv.AddRepo("acme", gh.Repository{Name: new("squashed")})
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	long := "Squash a large pull request (#1)\n\n" + strings.Repeat("- one of many changes, described at length\n", 2000) +
		"\nCo-authored-by: Claude <noreply@anthropic.com>"
	cs := make([]githubtest.Commit, 100)
	for i := range cs {
		cs[i] = githubtest.Commit{SHA: fmt.Sprintf("%040x", i+1), Message: long, AuthorName: "Ann", AuthorLogin: "ann", Date: at.Add(-time.Duration(i) * time.Hour)}
	}
	srv.SetCommits(id, cs...) // a page of about 9 MB
	srv.AddToken("ghp_test")
	c, err := github.New(config.GitHub{Sources: []config.Source{{ID: "gh", BaseURL: srv.BaseURL(), Orgs: []string{"acme"}, TokenFile: "x",
		Token: config.NewSecret([]byte("ghp_test"))}}}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := FromGitHub(c.Source("gh")).Commits(t.Context(), "acme", "squashed", "", 1)
	if err != nil || len(got) != 100 {
		t.Fatalf("commits = %d, %v", len(got), err)
	}
	m := got[0].Message
	if len(m) > maxMessageHead+maxMessageTail+16 || !strings.HasPrefix(m, "Squash a large pull request (#1)") ||
		!strings.Contains(m, "\n\n…\n\n") || !strings.HasSuffix(m, "Co-authored-by: Claude <noreply@anthropic.com>") {
		t.Fatalf("message: %d bytes, %q … %q", len(m), m[:40], m[len(m)-60:])
	}
	if got[99].AuthorLogin != "ann" || len(got[99].Parents) != 0 || len(got[0].Parents) != 1 || !got[0].Date.Equal(at) {
		t.Errorf("fields = %+v", got[99])
	}
	if origin, ai := Classify(&Commit{Subject: "Squash", Body: body(m), AuthorName: "Ann", AuthorLogin: "ann"}); origin != OriginAssisted || ai != "Claude" {
		t.Errorf("classified %v %q, want assisted by Claude", origin, ai)
	}
}

// TestHistory is acceptance criterion 1 of the commit navigator: what a cold, a warm and a pushed run read and keep.
func TestHistory(t *testing.T) {
	srv := githubtest.New(t)
	id := srv.AddRepo("acme", gh.Repository{Name: new("big")})
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	hex := func(i int) string { return fmt.Sprintf("%040x", i) }
	main := make([]githubtest.Commit, 700)
	for i := range main {
		main[i] = githubtest.Commit{SHA: hex(i + 1), Message: fmt.Sprintf("change %d\n\nwhy %d", i, i), AuthorName: "Ann", Date: at.Add(-time.Duration(i) * time.Hour)}
	}
	srv.SetCommits(id, main...)
	side := func(base, n int, off string) []githubtest.Commit {
		cs := make([]githubtest.Commit, n)
		for i := range cs {
			cs[i] = githubtest.Commit{SHA: hex(base + i), Message: "work", AuthorName: "Bo", Date: at.Add(time.Duration(n-i) * time.Minute)}
		}
		cs[n-1].Parents = []string{off}
		return cs
	}
	srv.SetBranch(id, "feature/a", side(5000, 3, hex(11))...)
	srv.SetBranch(id, "feature/b", side(6000, 2, hex(401))...)
	srv.SetBranch(id, "renovate/lib-2.x", side(7000, 1, hex(1))...)
	srv.SetBranch(id, "snyk-fix-0123abcd", side(8000, 1, hex(2))...)
	srv.SetBranch(id, "old", main[5])
	srv.SetTag(id, "v1", hex(1))
	srv.SetTag(id, "v0", hex(601)) // beyond the kept history
	srv.AddToken("ghp_test")
	c, err := github.New(config.GitHub{Sources: []config.Source{{ID: "gh", BaseURL: srv.BaseURL(), Orgs: []string{"acme"}, TokenFile: "x",
		Token: config.NewSecret([]byte("ghp_test"))}}}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	spec := New(map[string]Fetcher{"gh": FromGitHub(c.Source("gh"))}).Spec()
	run := func(prev store.Item) (sync.Result, payload) {
		t.Helper()
		res, err := spec.Run(t.Context(), sync.Job{Key: Kind + "/acme/big", Source: "gh", Repo: "acme/big", Prev: prev})
		var p payload
		if err != nil || json.Unmarshal(res.Payload, &p) != nil {
			t.Fatalf("run: %v", err)
		}
		return res, p
	}
	calls := func() map[string]int {
		n := map[string]int{}
		for _, l := range srv.Ledger() {
			f := strings.Fields(l)
			path, _, _ := strings.Cut(f[1], "?")
			n[path]++
			n[f[2]]++
		}
		return n
	}

	res, p := run(store.Item{})
	if len(p.Main) != maxMain || p.Main[0].SHA != hex(1) || p.Main[0].Body != "why 0" || p.Main[0].CommitterName != "Ann" {
		t.Fatalf("main: %d commits, first %+v", len(p.Main), p.Main[0])
	}
	if len(p.Side) != 5 {
		t.Errorf("side: %+v", p.Side)
	}
	var names []string
	for _, l := range p.Labels {
		names = append(names, l.Kind+":"+l.Name)
	}
	if got := strings.Join(names, ","); got != "branch:main,branch:feature/a,branch:feature/b,branch:old,tag:v1" {
		t.Errorf("labels = %s", got)
	}
	cold := calls()
	if cold["/repos/acme/big/commits"] != 7 || cold["/repos/acme/big/branches"] != 1 || cold["/repos/acme/big/tags"] != 1 {
		t.Errorf("a cold run read %v; want 5 default pages, the two feature branches, the branches and the tags", cold)
	}

	// 505 recent commits: a run asks the stats of 300 (three queries) and runs again soon for the rest, which the next run asks.
	if cold["/graphql"] != 3 || res.Again != cutAgain {
		t.Errorf("a cold run asked %d stats queries, again %v; want 3 and soon", cold["/graphql"], res.Again)
	}
	res, _ = run(store.Item{State: res.State, Payload: res.Payload})
	if res.Again != 0 {
		t.Errorf("the second run left stats to ask: again %v", res.Again)
	}
	prev := store.Item{State: res.State, Payload: res.Payload}
	cold = calls()
	res, _ = run(prev)
	warm := calls()
	if !res.Unchanged || warm["/repos/acme/big/commits"]-cold["/repos/acme/big/commits"] != 1 || warm["304"]-cold["304"] != 3 ||
		warm["200"] != cold["200"] {
		t.Errorf("a warm run without changes: unchanged %v, calls %v (cold %v); want one page and three 304s", res.Unchanged, warm, cold)
	}

	srv.SetCommits(id, append([]githubtest.Commit{{SHA: hex(9999), Message: "pushed", AuthorName: "Ann", Date: at.Add(time.Hour)}}, main...)...)
	res, p = run(prev)
	pushed := calls()
	if res.Unchanged || p.Main[0].SHA != hex(9999) || len(p.Main) != maxMain ||
		pushed["/repos/acme/big/commits"]-warm["/repos/acme/big/commits"] != 1 {
		t.Errorf("a push: first %s, %d commits, %d commit pages", p.Main[0].SHA, len(p.Main),
			pushed["/repos/acme/big/commits"]-warm["/repos/acme/big/commits"])
	}

	h := newHistory(p.Main, p.Side, p.Labels, "main")
	if l := h.LabelsAt(hex(9999)); len(l) != 1 || !l[0].Default {
		t.Errorf("labels at the new head: %+v", l)
	}
	if l := h.LabelsAt(hex(1)); len(l) != 1 || l[0].Name != "v1" {
		t.Errorf("labels at the old head: %+v", l)
	}
}
