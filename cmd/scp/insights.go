package main

import (
	"context"

	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/commits"
	"github.com/cantara/source-code-portal/internal/insights"
	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/releases"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// insightFacts hands the insights what the features hold of a repository (design/insights.md): its stable releases, the technology its
// manifests declare, who depends on it, how the people's recent commits are shared and when it last changed.
func insightFacts(s *snapshot.Snapshot, e *snapshot.RepoEntry) insights.Facts {
	var f insights.Facts
	if set, _ := releases.Of(e); set != nil {
		for _, r := range set.Releases {
			if !r.Pre {
				f.Stable = append(f.Stable, r.Published.UTC())
			}
		}
		if rel := set.Latest(); rel != nil {
			f.LatestTag = rel.Tag
		}
	}
	ms, _ := manifests.Of(e)
	for _, m := range ms {
		if f.Language == "" {
			f.Language = m.Language
		}
		if f.BuildTool == "" {
			f.BuildTool = m.BuildTool
		}
		for _, t := range m.Toolchain {
			f.Runtimes = append(f.Runtimes, [2]string{t.Name, t.Version})
		}
	}
	f.UsedBy = manifests.Summarise(s, e).UsedBy
	f.People, f.Top = commits.Authorship(s, e)
	f.LastChange, _ = commits.LastCommit(e)
	if f.LastChange.IsZero() {
		f.LastChange = e.Repo.PushedAt.UTC()
	}
	return f
}

func newInsights(view func(ctx context.Context) *snapshot.Snapshot) insights.API {
	return insights.API{Current: view, Facts: insightFacts, Retired: catalog.Retired,
		Fresh: func(e *snapshot.RepoEntry) snapshot.Freshness { _, f := commits.Of(e); return f }}
}
