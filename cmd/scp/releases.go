package main

import (
	"context"

	"github.com/cantara/source-code-portal/internal/docs"
	"github.com/cantara/source-code-portal/internal/packages"
	"github.com/cantara/source-code-portal/internal/releases"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// publishedOf hands the releases what a repository publishes to package registries (design/releases.md, RL4): each package's newest
// release.
func publishedOf(e *snapshot.RepoEntry) []releases.Published {
	ps, _ := packages.Of(e)
	var out []releases.Published
	for _, pk := range ps {
		if pk.Latest != nil {
			out = append(out, releases.Published{Ecosystem: pk.Ecosystem, Name: pk.Name, Version: pk.Latest.Version, URL: pk.Latest.URL, Published: pk.Latest.Published})
		}
	}
	return out
}

// newReleases is the releases API over a viewer's view: the GitHub releases and the published packages, with their notes rendered by
// the docs pipeline.
func newReleases(view func(ctx context.Context) *snapshot.Snapshot, documents *docs.Docs) releases.API {
	return releases.API{Current: view, Packages: publishedOf, Render: documents.Notes}
}
