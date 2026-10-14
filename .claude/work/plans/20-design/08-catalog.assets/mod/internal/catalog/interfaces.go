package catalog

import (
	"context"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// The consumer-defined interfaces of package catalog (10-architecture.md section 8.7). Direct rows use stdlib and kernel types only.

// RepositoryLister is satisfied directly by *github.Client. One call returns one page; Next == "" ends the listing.
// A NotModified result carries no Value: the caller keeps its stored page for that cursor.
type RepositoryLister interface {
	ListRepositories(ctx context.Context, src scm.SourceID, cursor string, v scm.Validators) (scm.Result[scm.Page[scm.Repository]], error)
}

// HeadProber is satisfied directly by *github.Client (GraphQL bulk read or REST fallback; the caller cannot tell which).
// A repository missing from the returned map was not found or not visible to the credential; the caller never treats that as removal.
type HeadProber interface {
	ProbeHeads(ctx context.Context, repos []scm.RepoRef) (map[scm.RepoID]scm.RepoStats, error)
}

// Tracker is BRIDGED (cmd/scp/internal/bridge): it adapts refresh.Planner.SetSubjects for every per-repository job kind,
// including catalog.stats. It receives the complete tracked set after every committed change; after Warm, cmd/scp passes Service.Repos().
type Tracker interface{ SetRepos(repos []scm.RepoRef) }

// EventSink is the wired *reactor.Fan; catalog emits scm.EventHeadChanged into it.
type EventSink interface {
	Handle(ctx context.Context, ev scm.Event) error
}

// CatalogueStore is the carve-out of store.Store that catalog uses; *sqlite.Store satisfies it directly.
type CatalogueStore interface {
	LoadCatalogue(ctx context.Context) ([]store.Repo, error)
	LoadItems(ctx context.Context, fn func(store.Item) error) error
	ReplaceCatalogue(ctx context.Context, repos []store.Repo) (removed []store.RepoKey, err error)
	PutItem(ctx context.Context, it store.Item) error
	Revalidated(ctx context.Context, k store.ItemKey, now time.Time) error
}

// Reader gives handlers the current snapshot; *readmodel.Publisher satisfies it. A handler calls Current once per request.
type Reader interface{ Current() *readmodel.Snapshot }

// Names of the slots and globals, and of the store item kind.
const (
	SlotRepo         = "catalog.repo"
	SlotStats        = "catalog.stats"
	GlobalMembership = "catalog.membership"
	GlobalAliases    = "catalog.aliases"
	GlobalGroupAlias = "catalog.group_aliases"
	GlobalSync       = "catalog.sync"

	KindStats = "catalog.stats" // store.ItemKey.Kind and job kind (per repository)

	JobReconcile = "catalog.reconcile" // global
	JobHeads     = "catalog.heads"     // global
	JobStats     = "catalog.stats"     // per repository
)

// Slots are the typed handles catalog reads and writes. cmd/scp calls RegisterSlots once, before Registry.Freeze, and hands the
// individual handles to the features that read them (they hold readmodel handles of kernel types and never import catalog).
type Slots struct {
	Repo         readmodel.Slot[freshness.Value[scm.Repository]] // the existence slot
	Stats        readmodel.Slot[freshness.Value[scm.RepoStats]]
	Membership   readmodel.Global[scm.Membership]
	Aliases      readmodel.Global[map[scm.Slug]scm.Slug]
	GroupAliases readmodel.Global[map[scm.GroupID]scm.GroupID]
	Sync         readmodel.Global[Status]
}

func RegisterSlots(r *readmodel.Registry) Slots {
	return Slots{
		Repo:         readmodel.Register[freshness.Value[scm.Repository]](r, SlotRepo, readmodel.Existence()),
		Stats:        readmodel.Register[freshness.Value[scm.RepoStats]](r, SlotStats),
		Membership:   readmodel.RegisterGlobal[scm.Membership](r, GlobalMembership),
		Aliases:      readmodel.RegisterGlobal[map[scm.Slug]scm.Slug](r, GlobalAliases),
		GroupAliases: readmodel.RegisterGlobal[map[scm.GroupID]scm.GroupID](r, GlobalGroupAlias),
		Sync:         readmodel.RegisterGlobal[Status](r, GlobalSync),
	}
}

// Status is the catalogue's own health, published as the global catalog.sync. It is ADMIN-class data (it counts repositories
// that the visibility policy excluded): it is read by ops and by overview for LastReconcile only and is never serialised to a public API.
type Status struct {
	Sources       []SourceStatus
	Groups        []GroupHealth
	Findings      []Finding
	LastReconcile time.Time // oldest LastSuccess over all sources; zero until every source has succeeded once
}

type SourceStatus struct {
	ID          scm.SourceID
	State       string // never, ok, empty, failed, held
	LastAttempt time.Time
	LastSuccess time.Time
	ErrCode     string
	Listed      int
	Tracked     int
	Excluded    map[string]int
	HeldCount   int
	Quarantined int
}
