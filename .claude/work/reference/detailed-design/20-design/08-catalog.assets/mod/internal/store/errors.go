package store

import (
	"errors"
	"fmt"
)

var (
	// ErrNoRepo: the repository has no row (never catalogued, or removed by ReplaceCatalogue). Satisfies job.Gone.
	ErrNoRepo = errors.New("store: no such repository")
	// ErrNoItem: Revalidated on an item that has no good value to validate.
	ErrNoItem = errors.New("store: no such item or nothing to revalidate")
	// ErrDegraded: writes are suspended (disk full, I/O error, corruption detected at run time). Reads may still work.
	ErrDegraded = errors.New("store: degraded, writes suspended")
	// ErrClosed: the store was closed.
	ErrClosed = errors.New("store: closed")
	// ErrInvalid: a malformed argument (see InvalidError).
	ErrInvalid = errors.New("store: invalid argument")
	// ErrUnknownNamespace: GetCache/PutCache with a namespace that was not registered at construction.
	ErrUnknownNamespace = errors.New("store: unknown cache namespace")
	// ErrTooLarge: a payload or cache value above its bound (a cache value above its namespace cap would evict itself).
	ErrTooLarge = errors.New("store: value too large")
)

// NoRepoError carries the key; Is(ErrNoRepo) and Gone() (job.Gone, structurally) hold.
type NoRepoError struct{ Key RepoKey }

func (e *NoRepoError) Error() string   { return fmt.Sprintf("store: no such repository %s", e.Key) }
func (e *NoRepoError) Is(t error) bool { return t == ErrNoRepo }
func (e *NoRepoError) Gone() bool      { return true }

// ErrCorrupt: an integrity check failed (the database is a rebuildable cache: the remedy is quarantine and cold start).
var ErrCorrupt = errors.New("store: integrity check failed")

// CorruptError carries the first lines SQLite reported.
type CorruptError struct{ Messages []string }

func (e *CorruptError) Error() string {
	return fmt.Sprintf("store: integrity check failed: %v", e.Messages)
}
func (e *CorruptError) Is(t error) bool { return t == ErrCorrupt }
