// Package compilecheck is the signature check behind design 16 section 2.3: the consumer interfaces of architecture 8.7 declared
// verbatim, and ONE adapter type that must satisfy all of them at once. It exists in the scratch module only to prove the
// table is implementable; the product's equivalent is cmd/scp/assert.go against the real types.
package compilecheck

import (
	"context"
	"net/http"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

type RepositoryLister interface {
	ListRepositories(ctx context.Context, src scm.SourceID, cursor string, v scm.Validators) (scm.Result[scm.Page[scm.Repository]], error)
}
type HeadProber interface {
	ProbeHeads(ctx context.Context, repos []scm.RepoRef) (map[scm.RepoID]scm.RepoStats, error)
}
type CommitLister interface {
	LatestCommits(ctx context.Context, r scm.RepoRef, ref string, n int, v scm.Validators) (scm.Result[[]scm.Commit], error)
}
type FileReader interface {
	Readme(ctx context.Context, r scm.RepoRef, ref string, v scm.Validators) (scm.Result[scm.File], error)
	ReadFile(ctx context.Context, r scm.RepoRef, ref, path string, v scm.Validators) (scm.Result[scm.File], error)
}
type TreeLister interface {
	ListTree(ctx context.Context, r scm.RepoRef, ref string, opt scm.TreeOptions) (scm.Result[scm.Tree], error)
}
type RenderedReader interface {
	RenderedFile(ctx context.Context, r scm.RepoRef, ref, path string, v scm.Validators) (scm.Result[string], error)
}
type TreeReader interface {
	ListTree(ctx context.Context, r scm.RepoRef, ref string, opt scm.TreeOptions) (scm.Result[scm.Tree], error)
}
type BlobReader interface {
	ReadBlob(ctx context.Context, r scm.RepoRef, sha string, maxBytes int64) (scm.Blob, error)
}
type RepoImageReader interface {
	ReadRepoImage(ctx context.Context, r scm.RepoRef, ref, path string, maxBytes int64) (scm.File, error)
}
type Verifier interface {
	Verify(src scm.SourceID, header http.Header, body []byte) (scm.Event, error)
}
type Redeliverer interface {
	FailedDeliveries(ctx context.Context, src scm.SourceID, since time.Time) ([]int64, error)
	Redeliver(ctx context.Context, src scm.SourceID, deliveryID int64) error
}

// Client is the one type that must satisfy every direct row. Bodies are empty: only signatures are checked.
type Client struct{}

func (*Client) ListRepositories(context.Context, scm.SourceID, string, scm.Validators) (scm.Result[scm.Page[scm.Repository]], error) {
	panic("signature only")
}
func (*Client) ProbeHeads(context.Context, []scm.RepoRef) (map[scm.RepoID]scm.RepoStats, error) {
	panic("signature only")
}
func (*Client) LatestCommits(context.Context, scm.RepoRef, string, int, scm.Validators) (scm.Result[[]scm.Commit], error) {
	panic("signature only")
}
func (*Client) Readme(context.Context, scm.RepoRef, string, scm.Validators) (scm.Result[scm.File], error) {
	panic("signature only")
}
func (*Client) ReadFile(context.Context, scm.RepoRef, string, string, scm.Validators) (scm.Result[scm.File], error) {
	panic("signature only")
}
func (*Client) ListTree(context.Context, scm.RepoRef, string, scm.TreeOptions) (scm.Result[scm.Tree], error) {
	panic("signature only")
}
func (*Client) RenderedFile(context.Context, scm.RepoRef, string, string, scm.Validators) (scm.Result[string], error) {
	panic("signature only")
}
func (*Client) ReadBlob(context.Context, scm.RepoRef, string, int64) (scm.Blob, error) {
	panic("signature only")
}
func (*Client) ReadRepoImage(context.Context, scm.RepoRef, string, string, int64) (scm.File, error) {
	panic("signature only")
}
func (*Client) FailedDeliveries(context.Context, scm.SourceID, time.Time) ([]int64, error) {
	panic("signature only")
}
func (*Client) Redeliver(context.Context, scm.SourceID, int64) error { panic("signature only") }

type VerifierImpl struct{}

func (*VerifierImpl) Verify(scm.SourceID, http.Header, []byte) (scm.Event, error) {
	panic("signature only")
}

var (
	_ RepositoryLister = (*Client)(nil)
	_ HeadProber       = (*Client)(nil)
	_ CommitLister     = (*Client)(nil)
	_ FileReader       = (*Client)(nil)
	_ TreeLister       = (*Client)(nil)
	_ RenderedReader   = (*Client)(nil)
	_ TreeReader       = (*Client)(nil)
	_ BlobReader       = (*Client)(nil)
	_ RepoImageReader  = (*Client)(nil)
	_ Redeliverer      = (*Client)(nil)
	_ Verifier         = (*VerifierImpl)(nil)
)
