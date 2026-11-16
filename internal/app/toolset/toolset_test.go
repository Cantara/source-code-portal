package toolset

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/feature/insights"
	"github.com/cantara/source-code-portal/internal/feature/releases"
	"github.com/cantara/source-code-portal/internal/platform/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/platform/snapshot"
	"github.com/cantara/source-code-portal/internal/platform/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

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
	r := Reads{Releases: releases.API{Current: func(context.Context) *snapshot.Snapshot { return p.Current() }}}

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

// TestFleetInsightsTool: the tool tells the five insights as the API has them and never names a person.
func TestFleetInsightsTool(t *testing.T) {
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	now := time.Now().UTC()
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{
			{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "core", Visibility: "public"}},
			{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 2}, Owner: "acme", Name: "app", Visibility: "public"}},
		}, nil, snapshot.SourceStatus{ID: "gh"})
	})
	p.Flush()
	api := insights.API{Current: func(context.Context) *snapshot.Snapshot { return p.Current() },
		Retired: func(*snapshot.RepoEntry) oapi.ArchiveReason { return "" },
		Fresh:   func(*snapshot.RepoEntry) snapshot.Freshness { return snapshot.Freshness{State: snapshot.StateOK} }}
	api.Facts = func(_ *snapshot.Snapshot, e *snapshot.RepoEntry) insights.Facts {
		if e.Repo.Name == "core" {
			return insights.Facts{Language: "Go", UsedBy: []snapshot.RepoKey{"acme/app"}, People: 30, Top: 30, LastChange: now}
		}
		return insights.Facts{Language: "Go", People: 5, LastChange: now.AddDate(-3, 0, 0)}
	}
	r := Reads{Insights: api}
	res, err := r.fleetInsights(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 repositories", "## Cadence", "- Go: 2 repositories (100%)", "- acme/core: 1 dependants", "## Mostly one person's", "- acme/core\n", "1 dormant", "- acme/app, last changed"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, res.Text)
		}
	}
	if _, err := r.fleetInsights(t.Context(), map[string]any{"repo": "acme/nope"}); err == nil {
		t.Error("an unknown repository is an error")
	}
}
