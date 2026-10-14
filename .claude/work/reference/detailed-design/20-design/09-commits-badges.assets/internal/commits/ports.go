package commits

import (
	"context"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// CommitLister is satisfied directly by *github.Client (architecture section 8.7). Contract the adapter must keep:
//   - the result is the newest n commits of ref in branch order (tip first), not re-sorted by date;
//   - an empty repository (GitHub answers 409) is an empty slice and a nil error;
//   - v.SHA is the head SHA of the previous result; when the adapter learns the head is unchanged it may return
//     NotModified (no value). Validators.SHA of a fresh value is the head SHA it observed;
//   - an error that wraps scm.ErrNotFound for the repository itself also satisfies job.Gone.
type CommitLister interface {
	LatestCommits(ctx context.Context, r scm.RepoRef, ref string, n int, v scm.Validators) (scm.Result[[]scm.Commit], error)
}

// CommitStore is the slice of store.Store that commits uses (carved out by this consumer; *sqlite.Store and the
// test fake satisfy it).
type CommitStore interface {
	AppendCommits(ctx context.Context, repo store.RepoKey, cs []store.CommitRow, keep int) error
	LoadCommits(ctx context.Context, repo store.RepoKey, limit int) ([]store.CommitRow, error)
	PutItem(ctx context.Context, it store.Item) error
	Revalidated(ctx context.Context, k store.ItemKey, now time.Time) error
	RecordFailure(ctx context.Context, k store.ItemKey, f store.Failure) error
}
