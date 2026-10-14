package commits

import (
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Names of the snapshot handles; cmd/scp registers them with exactly these names.
const (
	SlotName   = "commits.view"
	GlobalName = "commits.latest"
	// ItemKind is the store item kind of the ring record (membership list), JobKind the engine job kind.
	ItemKind = "commits.ring"
	JobKind  = "commits.refresh"
)

// View is the per-repository slot value. Latest is ordered newest first by Order (see Compare) and holds at most
// Config.Keep entries. It is immutable once handed to readmodel.Put (Put takes ownership).
type View struct {
	Latest []scm.Commit
	Info   freshness.Info // Info.ContentSHA is the branch head SHA the ring was built from
}

// Newest returns the newest commit of the view.
func (v View) Newest() (scm.Commit, bool) {
	if len(v.Latest) == 0 {
		return scm.Commit{}, false
	}
	return v.Latest[0], true
}

// CommitRef points at one commit inside the commits.view slot of Slug.
type CommitRef struct {
	Slug scm.Slug
	SHA  string
	At   time.Time
}

// FeedInfo summarises the freshness of the repositories behind one feed; Derive turns it into a Status at read time.
type FeedInfo struct {
	Repos           int           // repositories in scope
	Never           int           // without any good value yet
	Failing         int           // last attempt failed (value may still be served)
	OldestFetchedAt time.Time     // oldest FetchedAt among repositories with a good value; zero when none
	ErrCode         string        // ErrCode of the failing repository with the smallest slug; "" when none fails
	TTL             time.Duration // policy TTL (identical for all repositories)
}

// Derive maps the summary onto the shared vocabulary: Unknown when nothing was ever fetched, Error when nothing good
// exists and attempts failed, Stale when anything is missing, failing or older than 1.5 TTL, otherwise Fresh.
func (f FeedInfo) Derive(now time.Time) freshness.Status {
	if f.Repos == 0 {
		return freshness.StatusFresh // an empty scope has nothing to be stale about
	}
	switch {
	case f.Never == f.Repos && f.Failing == 0:
		return freshness.StatusUnknown
	case f.Never == f.Repos:
		return freshness.StatusError
	case f.Never > 0 || f.Failing > 0 || now.Sub(f.OldestFetchedAt) > f.TTL*3/2:
		return freshness.StatusStale
	}
	return freshness.StatusFresh
}

// Latest is the commits.latest global (one per scm.ViewClass). All and ByGroup are the newest LatestN+1 refs: the extra
// one is a sentinel that lets a first page of exactly LatestN entries know that more exist.
type Latest struct {
	All       []CommitRef
	ByGroup   map[scm.GroupID][]CommitRef
	AllInfo   FeedInfo
	GroupInfo map[scm.GroupID]FeedInfo
}
