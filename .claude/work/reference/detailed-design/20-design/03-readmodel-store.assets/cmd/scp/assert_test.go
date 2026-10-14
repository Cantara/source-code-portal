package scp_test

// Compile-time proof (architecture 5.5) that the unit's adapters satisfy the consumer-defined carve-outs listed in the design
// (section 2.4) and the optional capabilities cmd/scp uses. The carve-outs are written the way their consumers declare them.

import (
	"context"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/store/memory"
	"github.com/cantara/source-code-portal/internal/store/sqlite"
)

type commitStore interface { // package commits
	AppendCommits(ctx context.Context, repo store.RepoKey, cs []store.CommitRow, keep int) error
	LoadCommits(ctx context.Context, repo store.RepoKey, limit int) ([]store.CommitRow, error)
	PutItem(ctx context.Context, it store.Item) error
	Revalidated(ctx context.Context, k store.ItemKey, now time.Time) error
	RecordFailure(ctx context.Context, k store.ItemKey, f store.Failure) error
}
type itemStore interface { // packages docs, catalog, integrations, manifests (items)
	PutItem(ctx context.Context, it store.Item) error
	Revalidated(ctx context.Context, k store.ItemKey, now time.Time) error
	RecordFailure(ctx context.Context, k store.ItemKey, f store.Failure) error
}
type catalogStore interface { // package catalog
	ReplaceCatalogue(ctx context.Context, repos []store.Repo) (removed []store.RepoKey, err error)
	itemStore
}
type blobCache interface { // package manifests
	GetCache(ctx context.Context, ns, key string) ([]byte, bool, error)
	PutCache(ctx context.Context, ns, key string, data []byte) error
}
type warmLoader interface { // cmd/scp warm start
	LoadCatalogue(ctx context.Context) ([]store.Repo, error)
	LoadItems(ctx context.Context, fn func(store.Item) error) error
	LoadPlans(ctx context.Context, fn func(store.PlanRow) error) error
	LoadCommits(ctx context.Context, repo store.RepoKey, limit int) ([]store.CommitRow, error)
	LoadTasks(ctx context.Context) (map[string]store.TaskState, error)
	LoadParked(ctx context.Context) ([]store.Parked, error)
}
type planSaver interface { // cmd/scp/internal/bridge PlanSink flusher
	SavePlans(ctx context.Context, rows []store.PlanRow) error
}
type engineHooks interface { // cmd/scp: refresh OnParked and the scheduler's persisted cadence
	Park(ctx context.Context, key, reason string, retryAt time.Time) error
	Unpark(ctx context.Context, key string) error
	SaveTask(ctx context.Context, name string, s store.TaskState) error
}
type opsView interface { // package ops (bridged: ops-owned view types) reads Health and the optional Info
	Health() store.Health
}

var (
	_ commitStore  = (*sqlite.Store)(nil)
	_ commitStore  = (*memory.Store)(nil)
	_ catalogStore = (*sqlite.Store)(nil)
	_ blobCache    = (*sqlite.Store)(nil)
	_ warmLoader   = (*sqlite.Store)(nil)
	_ planSaver    = (*sqlite.Store)(nil)
	_ engineHooks  = (*sqlite.Store)(nil)
	_ opsView      = (*sqlite.Store)(nil)

	_ store.Store        = (*sqlite.Store)(nil)
	_ store.Store        = (*memory.Store)(nil)
	_ store.InfoProvider = (*sqlite.Store)(nil)
	_ store.Optimizer    = (*sqlite.Store)(nil)
	_ store.Optimizer    = (*memory.Store)(nil)

	// the snapshot kernel: Projector and Validator are plain func types
	_ readmodel.Projector = func(*readmodel.Builder) error { return nil }
	_ readmodel.Validator = func(*readmodel.Snapshot) error { return nil }
)

func TestAssertionsCompile(t *testing.T) {}
