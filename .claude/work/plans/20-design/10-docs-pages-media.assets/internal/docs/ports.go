package docs

import (
	"context"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// FileReader is satisfied directly by *github.Client (architecture 8.7).
type FileReader interface {
	Readme(ctx context.Context, r scm.RepoRef, ref string, v scm.Validators) (scm.Result[scm.File], error)
	ReadFile(ctx context.Context, r scm.RepoRef, ref, path string, v scm.Validators) (scm.Result[scm.File], error)
}

// TreeLister is satisfied directly by *github.Client; the same call is shared with manifests through the adapter's memo.
type TreeLister interface {
	ListTree(ctx context.Context, r scm.RepoRef, ref string, opt scm.TreeOptions) (scm.Result[scm.Tree], error)
}

// RenderedReader is the AsciiDoc bridge (Accept: application/vnd.github.html+json); satisfied directly by *github.Client.
type RenderedReader interface {
	RenderedFile(ctx context.Context, r scm.RepoRef, ref, path string, v scm.Validators) (scm.Result[string], error)
}

// ImageSigner mints signed /media/ext paths; satisfied directly by media.Service (feature to feature through stdlib types
// only, wired in cmd/scp). KeyID identifies the current signing key (8 hex characters, not secret): it is part of the
// render identity so that a key change re-renders stored documents.
type ImageSigner interface {
	SignExternal(rawURL string) (signedPath string, ok bool)
	KeyID() string
}

// ItemStore is the slice of store.Store that docs uses; *sqlite.Store and the in-memory fake satisfy it.
type ItemStore interface {
	PutItem(ctx context.Context, it store.Item) error
	Revalidated(ctx context.Context, k store.ItemKey, now time.Time) error
	RecordFailure(ctx context.Context, k store.ItemKey, f store.Failure) error
}
