# Sync, snapshot and store (the data engine)
Status: as built. Binding order: ../plan.md > this note.

## Scope
`internal/sync` (scheduler, keyed jobs, retry, rate budget, pauses), `internal/snapshot` (immutable serving state, shared value types,
freshness), `internal/store` (SQLite last-good cache). One instance runs the jobs; several share its outcomes as `cluster.md` describes.
Features register jobs and contribute snapshot parts; handlers read only the snapshot. This note is the one description of the rate
budget, retries and pauses.

## Decisions (bullets, each with a one-line reason)
- Own small engine in `sync` with its own retry loop: one entry per key with a due time, in-process, in one file.
- Level-triggered: the due time per item is the state of record; events only make an entry due earlier, so a lost event is repaired by
  the TTL.
- `sync` is the SQLite writer: a job returns a `Result` and `sync` writes the item row and schedule in one transaction, giving one write path.
- One `Publish` function per job kind serves both warm start (from rows) and live results: the snapshot is built the same way in both cases.
- Shared value types (`Repo`, `Group`, `Descriptor`, `Commit`, `Doc`, `Release`, `Manifest`, `Freshness`) live in `internal/snapshot`:
  every feature reads and writes them through one core package, and fresh/stale has one derivation for API, pages, badges and `llms`.
- Commit freshness: webhooks give seconds; the `catalog.reconcile` listing sorted by `pushed_at` calls `Notify(EventPush, repo)` for every
  repository whose `pushed_at` changed; the 2 h commits TTL is the safety net.
- Publish is debounced (200 ms quiet, 1 s max, constants in `cmd/scp`): one rebuild per batch keeps CPU flat on cold start and webhook
  latency under seconds. One build runs at a time, each from the snapshot the previous one stored.
- `modernc.org/sqlite`, WAL, one connection: pure Go; the database is a cache, so `synchronous=NORMAL` and quarantine-on-doubt fit.
- Own migration runner on `PRAGMA user_version`: forward-only, a rollback is deleting the cache file.
- Content SHAs are the persisted validators; ETags stay in the GitHub client's memory (github.md): they are bound to a rotating token.
- A permanent error is recorded on the item, which is next due after 24 h: the item row carries all retry state.
- Time is `time.Now()`/`time.Timer`; tests use `testing/synctest`: virtual time comes from the bubble.

## Shapes (minimal Go types/interfaces and table sketches)
```go
package snapshot
type State string // persisted in item.status: "never","ok","not_found","too_large","no_content","unsupported","error"
type Freshness struct { State State; FetchedAt, CheckedAt time.Time; Attempts int; ErrCode string; TTL time.Duration }
type Status uint8 // Unknown, Fresh, Stale, Error; derived at read time
func (f Freshness) Derive(now time.Time) Status
type RepoID struct { Source string; GitHubID int64 } // identity; survives rename and transfer
type RepoKey string                                  // "owner/name", lower-case: the URL address

type Snapshot struct {               // immutable after publish; handlers read one per request
	Version uint64; BuiltAt time.Time; Catalogued bool
	Repos   map[RepoKey]*RepoEntry   // entries shared by pointer between versions
	Aliases map[RepoKey]RepoKey      // old address -> current (308)
	Groups  []Group                  // config order, membership resolved by catalog
	Sources []SourceStatus           // last listing per source
	/* groupsOf, the indexers and what they built, public view, item digests */
}
type RepoEntry struct { Repo Repo; Fresh Freshness; /* facets by name */ }
func (s *Snapshot) Repo(k RepoKey) (e *RepoEntry, moved bool) // follows aliases
func (s *Snapshot) Public() *Snapshot; Group(id string) (Group, bool); GroupsOf(k RepoKey) []string

// A feature's data on an entry is a facet, declared as a constant of the feature: const facet snapshot.Facet[[]Commit] = "commits".
type Facet[T any] string
func (f Facet[T]) Get(e *RepoEntry) (T, Freshness)       // zero values until the feature contributes
func (f Facet[T]) Set(e *RepoEntry, v T, fresh Freshness) // inside UpdateRepo, on the copy; copies the facet map
func (e *RepoEntry) Facets() iter.Seq2[string, Freshness] // name and freshness of every facet, for the metrics
// What a feature derives from every repository is an index, built at every publish and again for the public view.
type Index[T any] string
func (i Index[T]) Get(s *Snapshot) T
func (i Index[T]) Build(fn func(*Snapshot) T) Indexer     // passed to NewPublisher
type Builder struct{ /* copy-on-write over the current Snapshot */ }
func (b *Builder) ReplaceSource(source string, repos []*RepoEntry, aliases map[RepoKey]RepoKey, st SourceStatus)
func (b *Builder) UpdateRepo(k RepoKey, fn func(*RepoEntry)) bool // false if not in the catalogue
func (b *Builder) Mark(itemKey string, repo RepoKey, digest uint64) // the item's part of Version; repo "" for a source's items
type Publisher struct{ /* atomic pointer, pending updates, debounce timer */ }
func NewPublisher(debounce, maxDelay time.Duration, log *slog.Logger, indexers ...Indexer) *Publisher
func (p *Publisher) Current() *Snapshot
func (p *Publisher) Update(fn func(*Builder)) // queued; applied in order at the next debounced publish
func (p *Publisher) Published() <-chan struct{} // closed at the next publish (events.go)
```
```go
package sync
type Priority uint8 // PrioEvent=1, PrioScheduled=2, PrioBackfill=3; lower runs first
type Event string   // a name: EventPush "push", EventTag "tag", EventSourceChanged "source", or another GitHub event name (github.md behaviour 9)
type Job struct { Key, Source string; Repo snapshot.RepoKey; Prev store.Item } // key "<kind>/<owner>/<name>" or "<kind>/<source>"
type Result struct { State snapshot.State; Unchanged bool; ContentSHA string; Payload []byte; Catalogue []store.Repo; Again time.Duration } // Catalogue: listing jobs; Again (< TTL): run again that soon
type Spec struct {
	Kind string; PerSource, Lists bool; Prio Priority; TTL, Timeout time.Duration // Timeout default 2 m
	On      []Event                                                        // events that make this kind due for the repository or source
	Run     func(ctx context.Context, j Job) (Result, error)
	Publish func(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness) // warm start and live
}
type Budget interface{ Remaining(source, owner string) (frac float64, reset time.Time) } // satisfied by *github.Client
func (e *Engine) Register(s Spec) error                        // before Run only
func (e *Engine) Notify(ev Event, repo snapshot.RepoKey)       // never blocks
func (e *Engine) NotifySource(ev Event, source string)         // per-source specs only
func (e *Engine) Run(ctx context.Context) error
// Errors from Run are classified by behaviour (errors.As): RateLimitedUntil() (time.Time, bool), AuthFailed() bool, Permanent() bool,
// ErrorCode() string; anything else is transient.
```
```sql
-- migrations/0001_init.sql; all tables STRICT; times are UTC unix seconds
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT;
CREATE TABLE repo (key TEXT PRIMARY KEY, source TEXT NOT NULL, github_id INTEGER NOT NULL, UNIQUE (source, github_id)) STRICT;
CREATE TABLE item (key TEXT PRIMARY KEY, kind TEXT NOT NULL,
  repo_key TEXT REFERENCES repo (key) ON DELETE CASCADE,                          -- NULL for source jobs
  status TEXT NOT NULL CHECK (status IN ('never','ok','not_found','too_large','no_content','unsupported','error')),
  fetched_at INTEGER, checked_at INTEGER, next_due_at INTEGER, attempts INTEGER NOT NULL DEFAULT 0,
  content_sha TEXT, error TEXT, payload BLOB) STRICT;                             -- payload: feature JSON with an inner "schema" field
```

## Behaviour (numbered essentials and the edge cases that matter)
1. **Entries**: every per-source spec has an entry per configured source; every per-repository spec has an entry per catalogued
   repository. A new entry with no stored item is due now at `PrioBackfill`; registration order (catalogue, commits, docs, descriptor,
   release, manifests) is also publish order at the warm start.
2. **Dispatcher**: one loop starts the most urgent due entry (priority, then due time, then key) while fewer than `sync.concurrency` jobs
   run, at most one start per 100 ms; it sleeps until the next due time, a wake-up or 1 minute.
3. **Notify(ev, repo)** makes every spec whose `On` contains `ev` due now at `PrioEvent`: per-repository specs for `repo`, per-source
   specs for its source; `NotifySource` does the same for per-source specs of a source. A running job runs once more after it finishes;
   an entry in retry backoff keeps its retry time. A burst of pushes gives one run, or one run plus one follow-up.
4. **Budget gate**: before a start the job's priority must leave `Budget.Remaining(source, owner)` above its reserve (backfill 40%,
   scheduled 15%, event 2%); a held entry waits until `reset`. Each job runs under its `Timeout`.
5. **Result handling** (one transaction for item row and `next_due_at`, then `Publish` via `Publisher.Update`):
   - success -> `attempts=0`, error cleared, `next_due_at = now + TTL ± 10%`; `Unchanged` (304 or equal SHA) updates `checked_at` only.
   - transient -> `attempts++`, error code stored, payload and `fetched_at` kept (stale-while-error), retry after full jitter between
     2 s and `min(15 min, 2 s << (attempts-1))`, forever at the cap.
   - rate limited -> pause all starts for the source until `RetryAt` + 0..1 s stagger; without a `RetryAt` 1 min doubling to 15 min,
     reset by one success. The attempt is not counted and the row is untouched; a pause in force is not extended, so one limit seen
     by several jobs in flight pauses once.
   - auth failed (401) -> pause the source 5 min, doubling to 1 h, likewise without an attempt.
   - permanent (permission errors, a recovered panic with code `panic`) -> error recorded, next due after 24 h.
   - a job that reports `not_found`, `no_content`, `too_large` or `unsupported` stores that state as a normal result.
6. **Catalogue**: a listing job's `Result.Catalogue` replaces the source's `repo` rows in the same transaction (repositories that left or
   changed address are deleted and their items cascade; an address taken by another identity moves over); the engine drops the entries
   of those repositories, adds entries for new ones and clears their digests from the snapshot. A result arriving for a repository no
   longer catalogued writes nothing.
7. **Snapshot build**: `Update` mutations are queued; the publisher applies them in order to a builder over the current snapshot 200 ms
   after the last update or 1 s after the first pending one, one build at a time; it derives the group memberships per repository,
   runs the features' indexers (the commit feeds, the dependency graph) and builds the public view (access.md) with its own indexes,
   sets `Version` from the item digests (images-events.md) and stores the atomic pointer. A mutation that panics is dropped alone and
   logged; a panic while indexing keeps the old snapshot.
8. **Store open**: pragmas `journal_mode=WAL`, `synchronous=NORMAL`, `busy_timeout=5000`, `foreign_keys=1`, `temp_store=MEMORY`, one
   connection, `quick_check`, migrate. Failed check, failed migration or `user_version` newer than the binary -> rename the file and its
   `-wal`/`-shm` to `<path>.quarantine-<UTC>` (keep 2) and start empty. Close runs `PRAGMA optimize` and `wal_checkpoint(TRUNCATE)`.
9. **Migrations**: embedded `NNNN_name.sql`, contiguous; each runs in one transaction together with `PRAGMA user_version = N`; payload
   changes use the inner `schema` field. Rows of sources no longer configured and of unknown kinds are deleted at the warm start.
10. **Warm start**: read repo and item rows, call each kind's `Publish` into one builder, publish at once, restore the schedule (a stored
    due time is capped at now + TTL, so a shortened TTL applies). A restart inside the TTLs makes almost no GitHub calls. HTML read back
    from the store is sanitised again (content.md).
11. **Degraded store**: a write error is logged; the snapshot is still published (it is what serving reads).
12. **Readiness** (`/readyz` 200): a snapshot with a catalogue is published (warm start or every source listed once) and the web UI server
    answers when one is configured; it stays ready through any GitHub outage until shutdown. `/livez` fails when the dispatcher loop has
    not run for 3 minutes.
13. **Shutdown** (SIGTERM, within `server.shutdown_grace`): `/readyz` 503; the HTTP servers drain; the web UI server stops; the cluster
    member leaves; the engine stops and cancels jobs in flight (rows untouched, due again after a restart); pending `Update`s are dropped;
    the store closes last.

## Config (Viper dotted names, defaults)
| Key | Default |
|---|---|
| `sync.concurrency` | 4 (1..64) |
| `store.path` | `/data/scp.db` |

Pace (100 ms), TTL jitter (10%), retry and backoff bounds, budget reserves and the publisher's debounce (200 ms) and maximum delay (1 s)
are constants in `internal/sync` and `cmd/scp`.

## Tests (what proves it)
- `internal/sync` in `synctest` bubbles with a real store in a temp dir: `TestRunsAndReschedules`, `TestTransientKeepsPayloadAndBacksOff`,
  `TestRateLimitPausesWithoutAttempt`, `TestPermanentRetriesAfterADay`, `TestPanicIsPermanent`, `TestCatalogueAddsAndRemovesRepoJobs`,
  `TestNotifyCoalesces`, `TestConcurrencyLimit`, `TestBudgetHoldsBackfill`, `TestWarmStartPublishesAndKeepsSchedule`,
  `TestWarmStartHonoursAShorterTTL`, `TestBackoffBounds`, `TestRegisterRefuses`, `TestNotifySource`, `TestObserveDepthAlive`; the
  cluster side in `share_test.go`; `goleak`.
- `internal/snapshot`: `TestDerive` (including the 1.5 x TTL boundary), `TestDebounce`, `TestVersionFollowsTheItems`,
  `TestSnapshotsAreImmutable`, `TestPanickingUpdateIsDroppedAlone`, `TestPanickingIndexKeepsSnapshot`, `TestFacetsAndIndexes`, `TestPublicView`,
  `TestConcurrentPublishesKeepEveryUpdate`.
- `internal/store`: `TestRoundTrip`, `TestCatalogueReplace` (rename and cascade), `TestRetainSources`, `TestQuarantine`.
- `cmd/scp` `TestLoad1000`: cold start of 1,000 repositories, warm start, p99 per page; `TestServeEndToEnd`: warm start, outage served
  stale, readiness.
