package main

import (
	"context"
	"sync"

	"github.com/cantara/source-code-portal/internal/docs"
	"github.com/cantara/source-code-portal/internal/lru"
	"github.com/cantara/source-code-portal/internal/packages"
	"github.com/cantara/source-code-portal/internal/releases"
	"github.com/cantara/source-code-portal/internal/safehtml"
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

// notesCacheBytes bounds the rendered release notes kept: a page of a few hundred rows is rendered once, not at every request.
const notesCacheBytes = 4 << 20

// memoNotes renders a release's notes through render and keeps the answer by repository and text (the rendering depends on both and
// on nothing else); a changed release has other text, so it is rendered again.
func memoNotes(render func(*snapshot.RepoEntry, string) safehtml.SafeHTML) func(*snapshot.RepoEntry, string) safehtml.SafeHTML {
	var mu sync.Mutex
	cache := lru.New[string, safehtml.SafeHTML](notesCacheBytes, func(h safehtml.SafeHTML) int { return len(h.String()) + 64 })
	return func(e *snapshot.RepoEntry, md string) safehtml.SafeHTML {
		key := string(e.Repo.Key()) + "\x00" + md
		mu.Lock()
		h, ok := cache.Get(key)
		mu.Unlock()
		if ok {
			return h
		}
		h = render(e, md)
		mu.Lock()
		cache.Put(key, h)
		mu.Unlock()
		return h
	}
}

// newReleases is the releases API over a viewer's view: the GitHub releases and the published packages, with their notes rendered by
// the docs pipeline.
func newReleases(view func(ctx context.Context) *snapshot.Snapshot, documents *docs.Docs) releases.API {
	return releases.API{Current: view, Packages: publishedOf, Render: memoNotes(documents.Notes)}
}
