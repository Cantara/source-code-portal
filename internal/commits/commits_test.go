package commits

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
	in := []github.CommitInfo{
		{SHA: sha('a'), Message: "older", Date: now.Add(-2 * time.Hour)},
		{SHA: strings.ToUpper(sha('b')), Message: "\nfix: thing\u202e\n\nbody", AuthorName: "Ann", AuthorLogin: "ann",
			HTMLURL: "https://github.com/acme/x/commit/b", Date: now.Add(-time.Hour).Add(300 * time.Millisecond)},
		{SHA: sha('a'), Message: "duplicate", Date: now},
		{SHA: "nothex", Message: "bad sha", Date: now},
		{SHA: sha('c'), Message: "no date"},
		{SHA: sha('d'), Message: strings.Repeat("x", 300), AuthorLogin: "not a login", HTMLURL: "http://insecure", Date: now.Add(72 * time.Hour)},
	}
	firstSeen := now.Add(-30 * time.Minute)
	got := normalise(in, []snapshot.Commit{{SHA: sha('d'), CommittedAt: firstSeen}}, now)
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
}

type fetcher struct {
	cs  []github.CommitInfo
	err error
}

func (f fetcher) Commits(context.Context, string, string, int) ([]github.CommitInfo, error) {
	return f.cs, f.err
}

func TestRunStates(t *testing.T) {
	ok := fetcher{cs: []github.CommitInfo{{SHA: sha('a'), Message: "m", Date: time.Now()}}}
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
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	r := snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "x"}
	b, _ := json.Marshal(payload{Schema: payloadSchema, Commits: []snapshot.Commit{{SHA: sha('a'), CommittedAt: time.Now()}}})
	p.Update(func(bd *snapshot.Builder) {
		bd.ReplaceSource("gh", []*snapshot.RepoEntry{{Repo: r}}, nil, snapshot.SourceStatus{ID: "gh"})
		publish(bd, store.Item{Repo: "acme/x", State: snapshot.StateOK, Payload: b}, snapshot.Freshness{State: snapshot.StateOK})
		publish(bd, store.Item{Repo: "acme/gone", Payload: b}, snapshot.Freshness{}) // a removed repository: ignored
	})
	p.Flush()
	if s := p.Current(); len(s.Repos["acme/x"].Commits) != 1 || len(s.Latest) != 1 || len(s.Repos) != 1 {
		t.Errorf("published: %+v", s.Repos)
	}
}
