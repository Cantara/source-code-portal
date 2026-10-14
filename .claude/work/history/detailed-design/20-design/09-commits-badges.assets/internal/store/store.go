// Package store is the durable-state contract (rank 1: imports freshness and scm only). Implementations live in
// store/sqlite and store/memory; the conformance suite is store/storetest.
package store

import (
	"context"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
)

// Item is one (repository, kind) row: freshness metadata plus a feature-owned payload.
type Item struct {
	Key                  ItemKey
	State                freshness.State
	FetchedAt, CheckedAt time.Time // UTC, second precision, zero = unset
	Attempts             int       // consecutive failures; owned by the store: RecordFailure increments, PutItem/Revalidated reset
	ETag, ContentSHA     string
	Error                string // stable machine code of the last failed attempt, "" when the last attempt succeeded
	Payload              []byte // nil on revalidation; feature-owned codec with an inner schema field; never trusted as HTML
}

// PlanRow is the planner's persisted state. The planner is the ONLY writer of next_due_at and unchanged_streak.
type PlanRow struct {
	Key             ItemKey
	NextDue         time.Time
	UnchangedStreak int
}

type CommitRow struct {
	SHA         string
	CommittedAt time.Time
	Payload     []byte
}

type TaskState struct {
	LastAttempt, LastSuccess time.Time
	LastError                string
}

type Parked struct {
	Key            string
	Since, RetryAt time.Time
	Reason         string
}

// Repo is the persisted catalogue row. Meta is JSON (description, default branch, topics, visibility, aliases).
type Repo struct {
	Key      RepoKey
	GitHubID int64
	Slug     string
	Meta     []byte
	SeenAt   time.Time
}

type Failure struct {
	Code    string // stable machine code, persisted in item.error
	Message string // for the caller's log; not persisted
	At      time.Time
}

type Health struct {
	OK     bool
	Reason string
}

// Info is a point-in-time summary for /status (optional interface, see InfoProvider).
type Info struct {
	Driver        string
	Path          string
	SchemaVersion int
	SQLiteVersion string
	DBBytes       int64
	WALBytes      int64
	CacheBytes    map[string]int64
	LastBackup    time.Time
	LastBackupErr string
	Rebuilt       string // non-empty when this process started from a quarantined database: the reason
	Health        Health
}

// InfoProvider and Optimizer are optional capabilities of an implementation; cmd/scp asserts them on the concrete types.
type InfoProvider interface{ Info() Info }
type Optimizer interface {
	Optimize(ctx context.Context) error
}

// Store is the narrow durable contract. Every method is safe for concurrent use. Contract rules (pinned by storetest):
//  1. A LoadItems/LoadPlans callback MUST NOT call any Store method.
//  2. No method calls another Store method inside its transaction.
//  3. Writes on a repository that has no row return an error that Is ErrNoRepo and satisfies job.Gone.
//  4. Time is stored as UTC unix seconds; sub-second precision is dropped.
type Store interface {
	// warm start (read handle, one read transaction, streaming)
	LoadCatalogue(ctx context.Context) ([]Repo, error)
	LoadItems(ctx context.Context, fn func(Item) error) error
	LoadPlans(ctx context.Context, fn func(PlanRow) error) error
	LoadCommits(ctx context.Context, repo RepoKey, limit int) ([]CommitRow, error)

	// writes (write handle, each call one transaction)
	ReplaceCatalogue(ctx context.Context, repos []Repo) (removed []RepoKey, err error)
	PutItem(ctx context.Context, it Item) error
	Revalidated(ctx context.Context, k ItemKey, now time.Time) error
	RecordFailure(ctx context.Context, k ItemKey, f Failure) error
	SavePlans(ctx context.Context, rows []PlanRow) error
	AppendCommits(ctx context.Context, repo RepoKey, cs []CommitRow, keep int) error
	SaveTask(ctx context.Context, name string, s TaskState) error
	LoadTasks(ctx context.Context) (map[string]TaskState, error)
	Park(ctx context.Context, key, reason string, retryAt time.Time) error
	Unpark(ctx context.Context, key string) error
	LoadParked(ctx context.Context) ([]Parked, error)

	// generic bounded cache
	GetCache(ctx context.Context, ns, key string) (data []byte, ok bool, err error)
	PutCache(ctx context.Context, ns, key string, data []byte) error

	// operations
	Check(ctx context.Context, full bool) error
	Backup(ctx context.Context, path string) error
	Health() Health
	Run(ctx context.Context) error
	Close(ctx context.Context) error
}
