package releases

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	gh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

func day(n int) time.Time { return time.Date(2026, 10, n, 12, 0, 0, 0, time.UTC) }

func TestFromGitHub(t *testing.T) {
	srv := githubtest.New(t)
	many := srv.AddRepo("acme", gh.Repository{Name: new("many")})
	srv.AddRepo("acme", gh.Repository{Name: new("none")})
	var rels []githubtest.Release
	for i := range Keep + 5 {
		rels = append(rels, githubtest.Release{Tag: fmt.Sprintf("v1.%d.0", i), Name: "R", Published: day(1).Add(time.Duration(i) * time.Hour)})
	}
	rels = append(rels,
		githubtest.Release{Tag: "v2.0.0-rc1", Pre: true, Published: day(20), Author: "renovate[bot]"},
		githubtest.Release{Tag: "v9.9.9", Draft: true, Published: day(21)},
		githubtest.Release{Tag: "v2.0.0", Name: "Two", Body: "## What's new\n\n- faster\n", Published: day(10), Author: "ann"})
	srv.SetReleases(many, rels...)
	srv.AddToken("ghp_test")
	c, err := github.New(config.GitHub{Sources: []config.Source{{ID: "gh", BaseURL: srv.BaseURL(), Orgs: []string{"acme"}, TokenFile: "x",
		Token: config.NewSecret([]byte("ghp_test"))}}}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	src := FromGitHub(c.Source("gh"))

	got, err := src.Releases(t.Context(), "acme", "many")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > Keep {
		t.Errorf("%d releases, at most %d are asked for", len(got), Keep)
	}
	for _, r := range got {
		if r.Tag == "v9.9.9" {
			t.Errorf("a draft is listed: %+v", r)
		}
	}
	if got[0].Tag != "v2.0.0-rc1" || !got[0].Pre || got[0].Author != "renovate[bot]" {
		t.Errorf("newest = %+v, want the pre-release", got[0])
	}
	if got[1].Tag != "v2.0.0" || got[1].Summary != "What's new" || got[1].Assets != 1 || !got[1].Published.Equal(day(10)) {
		t.Errorf("second = %+v", got[1])
	}
	if r, err := src.Releases(t.Context(), "acme", "none"); err != nil || len(r) != 0 {
		t.Errorf("no release = %+v, %v", r, err)
	}
	if r, err := src.Releases(t.Context(), "acme", "gone"); err != nil || len(r) != 0 {
		t.Errorf("an unknown repository is none, not a failure: %+v, %v", r, err)
	}
}

type fetcher struct {
	rels []Release
	err  error
}

func (f fetcher) Releases(context.Context, string, string) ([]Release, error) { return f.rels, f.err }

func TestJob(t *testing.T) {
	hostile := "Name \u202e with bidi\n"
	rels := []Release{
		{Tag: "v1.0.0", Name: "One", URL: "https://github.com/acme/x/releases/v1.0.0", Published: day(1), Author: "ann"},
		{Tag: "v1.1.0", Name: hostile, Summary: hostile, Published: day(3), Author: "dependabot[bot]"},
		{Tag: "v1.2.0-rc1", Pre: true, Published: day(5)},
	}
	spec := New(map[string]Fetcher{"gh": fetcher{rels: rels}}).Spec()
	job := sync.Job{Key: Kind + "/acme/x", Source: "gh", Repo: "acme/x"}
	res, err := spec.Run(t.Context(), job)
	if err != nil || res.Unchanged || res.Again != 0 {
		t.Fatalf("first run: %+v, %v", res, err)
	}
	var set Set
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	fresh := snapshot.Freshness{State: snapshot.StateOK}
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "x"}}}, nil,
			snapshot.SourceStatus{ID: "gh"})
		spec.Publish(b, store.Item{Repo: "acme/x", State: snapshot.StateOK, Payload: res.Payload}, fresh)
	})
	p.Flush()
	e := p.Current().Repos["acme/x"]
	s, _ := Of(e)
	if s == nil {
		t.Fatal("no releases published")
	}
	set = *s
	if tags := fmt.Sprint(set.Releases[0].Tag, set.Releases[1].Tag, set.Releases[2].Tag); tags != "v1.2.0-rc1v1.1.0v1.0.0" {
		t.Errorf("newest first: %s", tags)
	}
	if r := set.Releases[1]; strings.ContainsRune(r.Name+r.Summary, '\u202e') || !r.Bot || strings.Contains(r.Name, "\n") {
		t.Errorf("repository text is cleaned and a bot author told: %+v", r)
	}
	if latest, f := Latest(e); latest == nil || latest.Tag != "v1.1.0" || f.State != snapshot.StateOK {
		t.Errorf("latest = %+v %v, want the newest stable one, not the pre-release", latest, f.State)
	}

	again := job
	again.Prev = store.Item{State: snapshot.StateOK, Payload: res.Payload}
	if r, _ := spec.Run(t.Context(), again); !r.Unchanged || r.Again != 0 {
		t.Errorf("the same releases again: %+v, want Unchanged and no second read", r)
	}
	again.Event = true
	if r, _ := spec.Run(t.Context(), again); !r.Unchanged || r.Again != ConfirmAfter {
		t.Errorf("an event that found nothing new: %+v, want a second read after %v", r, ConfirmAfter)
	}
	again.Prev = store.Item{}
	if r, _ := spec.Run(t.Context(), again); r.Again != 0 {
		t.Errorf("an event that found a release needs no second read: %+v", r)
	}

	if _, err := New(map[string]Fetcher{"gh": fetcher{err: errors.New("boom")}}).Spec().Run(t.Context(), job); err == nil {
		t.Error("a failing read is a failure")
	}
	if _, err := New(nil).Spec().Run(t.Context(), job); err == nil {
		t.Error("an unknown source is a failure")
	}
}

// TestLatest: a repository with only pre-releases has no latest one and is read to have none (the badge says none); one not read yet
// is unknown.
func TestLatest(t *testing.T) {
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	spec := New(map[string]Fetcher{"gh": fetcher{rels: []Release{{Tag: "v1-rc", Pre: true, Published: day(1)}}}}).Spec()
	res, _ := spec.Run(t.Context(), sync.Job{Source: "gh", Repo: "acme/pre"})
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{
			{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "pre"}},
			{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 2}, Owner: "acme", Name: "unread"}},
		}, nil, snapshot.SourceStatus{ID: "gh"})
		spec.Publish(b, store.Item{Repo: "acme/pre", State: snapshot.StateOK, Payload: res.Payload}, snapshot.Freshness{State: snapshot.StateOK})
	})
	p.Flush()
	s := p.Current()
	if rel, f := Latest(s.Repos["acme/pre"]); rel != nil || f.State != snapshot.StateNotFound {
		t.Errorf("only a pre-release: %+v %v, want none and not found", rel, f.State)
	}
	if rel, f := Latest(s.Repos["acme/unread"]); rel != nil || f.State == snapshot.StateNotFound {
		t.Errorf("not read yet: %+v %v, want unknown", rel, f.State)
	}
}
