package commits

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

func sha(c byte) string { return strings.Repeat(string(c), 40) }

func TestNormalise(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	in := []CommitInfo{
		{SHA: sha('a'), Message: "older", Date: now.Add(-2 * time.Hour)},
		{SHA: strings.ToUpper(sha('b')), Message: "\nfix: thing\u202e\n\nbody", AuthorName: "Ann", AuthorLogin: "ann",
			HTMLURL: "https://github.com/acme/x/commit/b", Date: now.Add(-time.Hour).Add(300 * time.Millisecond)},
		{SHA: sha('a'), Message: "duplicate", Date: now},
		{SHA: "nothex", Message: "bad sha", Date: now},
		{SHA: sha('c'), Message: "no date"},
		{SHA: sha('d'), Message: strings.Repeat("x", 300), AuthorLogin: "not a login", HTMLURL: "http://insecure", Date: now.Add(72 * time.Hour)},
	}
	firstSeen := now.Add(-30 * time.Minute)
	got := normalise(in, []Commit{{SHA: sha('d'), CommittedAt: firstSeen}}, now)
	if len(got) != 3 {
		t.Fatalf("got %d commits: %+v", len(got), got)
	}
	if d := got[0]; d.SHA != sha('d') || !d.CommittedAt.Equal(firstSeen) || d.AuthorLogin != "" || d.URL != "" ||
		len([]rune(d.Subject)) != maxSubject || !strings.HasSuffix(d.Subject, "…") {
		t.Errorf("future-dated commit = %+v", d)
	}
	if b := got[1]; b.SHA != sha('b') || b.Subject != "fix: thing" || b.AuthorLogin != "ann" || b.CommittedAt.Nanosecond() != 0 {
		t.Errorf("commit b = %+v", b)
	}
	if got[2].SHA != sha('a') || got[2].Subject != "older" {
		t.Errorf("commit a = %+v (the first of duplicate SHAs wins)", got[2])
	}
	if got[2].Parents == nil || len(got[2].Parents) != 0 {
		t.Errorf("root commit parents = %#v, want empty, not nil", got[2].Parents)
	}
}

func TestNormaliseParents(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	many := []string{strings.ToUpper(sha('1')), "nothex", sha('1'), sha('2')}
	for _, c := range "3456789ab" {
		many = append(many, sha(byte(c)))
	}
	got := normalise([]CommitInfo{{SHA: sha('f'), Message: "octopus", Date: now, Parents: many}}, nil, now)
	want := []string{sha('1'), sha('2'), sha('3'), sha('4'), sha('5'), sha('6'), sha('7'), sha('8')}
	if !slices.Equal(got[0].Parents, want) {
		t.Errorf("parents = %v, want the valid ones lower-case, deduplicated, at most %d", got[0].Parents, maxParents)
	}
}

type fetcher struct {
	cs  []CommitInfo
	err error
}

func (f fetcher) Commits(context.Context, string, string, int) ([]CommitInfo, error) {
	return f.cs, f.err
}

func TestRunStates(t *testing.T) {
	ok := fetcher{cs: []CommitInfo{{SHA: sha('a'), Message: "m", Date: time.Now()}}}
	tests := map[string]struct {
		f       Fetcher
		state   snapshot.State
		commits int
		err     bool
	}{
		"ok":        {f: ok, state: snapshot.StateOK, commits: 1},
		"empty":     {f: fetcher{err: &github.Error{Kind: github.NotFound, Code: "empty", Err: errors.New("409")}}, state: snapshot.StateNoContent},
		"not found": {f: fetcher{err: &github.Error{Kind: github.NotFound, Code: "not_found", Err: errors.New("404")}}, state: snapshot.StateNotFound},
		"transient": {f: fetcher{err: &github.Error{Kind: github.Transient, Code: "transient", Err: errors.New("502")}}, err: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			spec := New(map[string]Fetcher{"gh": tc.f}).Spec()
			res, err := spec.Run(t.Context(), sync.Job{Key: Kind + "/acme/x", Source: "gh", Repo: "acme/x"})
			if (err != nil) != tc.err {
				t.Fatalf("err = %v", err)
			}
			if err != nil {
				return
			}
			var p payload
			if res.State != tc.state || json.Unmarshal(res.Payload, &p) != nil || len(p.Commits) != tc.commits {
				t.Errorf("result = %+v", res)
			}
			again, _ := spec.Run(t.Context(), sync.Job{Key: Kind + "/acme/x", Source: "gh", Repo: "acme/x",
				Prev: store.Item{State: res.State, Payload: res.Payload}})
			if !again.Unchanged {
				t.Error("the same commits again are not Unchanged")
			}
		})
	}
}

func TestPublish(t *testing.T) {
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), Feeds())
	r := snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "x"}
	b, _ := json.Marshal(payload{Schema: payloadSchema, Commits: []Commit{{SHA: sha('a'), CommittedAt: time.Now()}}})
	p.Update(func(bd *snapshot.Builder) {
		bd.ReplaceSource("gh", []*snapshot.RepoEntry{{Repo: r}}, nil, snapshot.SourceStatus{ID: "gh"})
		publish(bd, store.Item{Repo: "acme/x", State: snapshot.StateOK, Payload: b}, snapshot.Freshness{State: snapshot.StateOK})
		publish(bd, store.Item{Repo: "acme/gone", Payload: b}, snapshot.Freshness{}) // a removed repository: ignored
	})
	p.Flush()
	s := p.Current()
	if cs, _ := Of(s.Repos["acme/x"]); len(cs) != 1 || len(s.Repos) != 1 {
		t.Errorf("published: %+v", s.Repos)
	}
}

func TestLastAuthored(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	bot := Commit{AuthorName: "renovate[bot]", Subject: "chore(deps): update", CommittedAt: day(9)}
	release := Commit{AuthorName: "Jenkins", Subject: "[maven-release-plugin] prepare release v1", CommittedAt: day(8)}
	person := Commit{AuthorName: "Ada", AuthorLogin: "ada", Subject: "Fix the parser", CommittedAt: day(7)}
	app := Commit{AuthorLogin: "github-actions[bot]", CommittedAt: day(6)}
	for name, tc := range map[string]struct {
		cs     []Commit
		at     time.Time
		before bool
	}{
		"a person after automation": {[]Commit{bot, release, person, app}, day(7), false},
		"only automation":           {[]Commit{bot, release, app}, day(6), true},
		"none":                      {nil, time.Time{}, false},
	} {
		e := &snapshot.RepoEntry{}
		facet.Set(e, tc.cs, snapshot.Freshness{State: snapshot.StateOK})
		if at, before := LastAuthored(e); !at.Equal(tc.at) || before != tc.before {
			t.Errorf("%s: %v %v, want %v %v", name, at, before, tc.at, tc.before)
		}
	}
}
