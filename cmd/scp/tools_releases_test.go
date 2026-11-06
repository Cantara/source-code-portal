package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/releases"
	"github.com/cantara/source-code-portal/internal/search"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

// TestRecentReleasesTool: the tool reads the releases API as the viewer's view has it and says what a failed check means.
func TestRecentReleasesTool(t *testing.T) {
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	now := time.Now().UTC()
	spec := releases.New(nil).Spec()
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "core", Visibility: "public"}}}, nil,
			snapshot.SourceStatus{ID: "gh"})
		payload, _ := json.Marshal(releases.Set{Releases: []releases.Release{
			{Tag: "v2.0.0-rc1", Pre: true, Published: now.AddDate(0, 0, -1), Author: "renovate[bot]", Bot: true},
			{Tag: "v1.0.0", Name: "One", URL: "https://github.com/acme/core/releases/v1.0.0", Published: now.AddDate(0, 0, -9), Author: "ann"},
		}})
		spec.Publish(b, store.Item{Repo: "acme/core", State: snapshot.StateOK, Payload: payload}, snapshot.Freshness{State: snapshot.StateOK, FetchedAt: now, CheckedAt: now, TTL: time.Hour})
	})
	p.Flush()
	r := toolReads{releases: releasesAPI{Current: func(context.Context) *snapshot.Snapshot { return p.Current() }}}

	res, err := r.recentReleases(t.Context(), map[string]any{"variant": "all"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 releases held (1 stable, 1 pre-releases)", "acme/core v2.0.0-rc1", "pre-release", "(automation)", "acme/core v1.0.0", ": One", "by ann"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, res.Text)
		}
	}
	if res, _ := r.recentReleases(t.Context(), nil); strings.Contains(res.Text, "v2.0.0-rc1") || !strings.Contains(res.Text, "v1.0.0") {
		t.Errorf("default is the latest stable release:\n%s", res.Text)
	}
	if _, err := r.recentReleases(t.Context(), map[string]any{"repo": "acme/nope"}); err == nil {
		t.Error("an unknown repository is an error")
	}
	if _, err := r.recentReleases(t.Context(), map[string]any{"variant": "everything"}); err == nil {
		t.Error("an unknown variant is an error")
	}
}

// TestSearchReadsReleases: each release held is a document of the kind release that opens the repository's releases.
func TestSearchReadsReleases(t *testing.T) {
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	now := time.Now().UTC()
	spec := releases.New(nil).Spec()
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "core", Visibility: "public"}}}, nil,
			snapshot.SourceStatus{ID: "gh"})
		payload, _ := json.Marshal(releases.Set{Releases: []releases.Release{
			{Tag: "v2.0.0", Name: "Two", Summary: "Single sign-on", Published: now, Author: "ann"},
			{Tag: "v1.0.0", Published: now.AddDate(0, 0, -30)},
		}})
		spec.Publish(b, store.Item{Repo: "acme/core", State: snapshot.StateOK, Payload: payload}, snapshot.Freshness{State: snapshot.StateOK})
	})
	p.Flush()
	var got []search.Document
	for _, d := range searchRead(p.Current().Repos["acme/core"]).Documents {
		if d.Kind == search.KindRelease {
			got = append(got, d)
		}
	}
	if len(got) != 2 || got[0].Title != "v2.0.0 Two" || got[1].Title != "v1.0.0" || got[0].URL != "/releases?repo=acme%2Fcore&variant=all" ||
		got[0].Fields[search.FieldText] != "Single sign-on" || got[0].Fields[search.FieldAuthor] != "ann" {
		t.Errorf("release documents = %+v", got)
	}
}
