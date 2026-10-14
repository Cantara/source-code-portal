# Sync, snapshot and store (the v1 data engine)
Status: v1 design note, 2026-10-14. Binding order: ../plan.md > this note > ../reference/.

## Scope (v1)
`internal/sync` (scheduler, keyed job queue, retry, rate budget, pauses, readiness), `internal/snapshot` (immutable serving state, shared
value types, freshness), `internal/store` (SQLite last-good cache). Single instance. Features register jobs and contribute snapshot parts;
handlers read only the snapshot. This note is the one description of the rate budget, retries and pauses.

## Decisions (bullets, each with a one-line reason)
- Own small engine in `sync` with its own retry loop: keyed coalescing with one follow-up run fits in about 400 lines, in-process.
- Level-triggered: `next_due_at` per item is the state of record; events only enqueue earlier, so a lost event is repaired by the TTL.
- `sync` is the SQLite writer: a job returns a `Result` and `sync` writes item row and schedule in one transaction, giving one write path.
- One `Publish` function per job kind serves both warm start (from rows) and live results: the snapshot is built the same way in both cases.
- Shared value types (`Repo`, `Group`, `Descriptor`, `Commit`, `Doc`, `Release`, freshness) live in `internal/snapshot`: every feature
  reads and writes them through one core package, and fresh/stale has one derivation for API, pages, badges and `llms`.
- Commit freshness: webhooks give seconds; the 5-minute `catalog.reconcile` listing sorted by `pushed_at` (REST, about 10 calls per org)
  calls `Notify(EventPush, repo)` for every repository whose `pushed_at` changed; `commits.ttl` 2 h is the safety net.
- Publish is debounced (200 ms quiet, 1 s max): one rebuild per batch keeps CPU flat on cold start and webhook latency under seconds.
- `modernc.org/sqlite`, WAL, one writer connection: pure Go; the database is a cache, so `synchronous=NORMAL` and quarantine-on-doubt fit.
- Own migration runner on `PRAGMA user_version`: forward-only, a rollback is deleting the cache file; about 50 lines.
- Content SHAs are the persisted validators; ETags stay in the GitHub client's memory (github.md): they are bound to a rotating token.
- A permanent error is recorded on the item, which is next due after 24 h: the item row carries all retry state.
- Time is `time.Now()`/`time.Timer`; tests use `testing/synctest`; randomness takes a seed: virtual time comes from the bubble.

## Shapes (minimal Go types/interfaces and table sketches)
```go
package snapshot
type State string // persisted in item.status: "ok","not_found","too_large","no_content","unsupported","error","never"; stable names
type Freshness struct { State State; FetchedAt, CheckedAt time.Time; Attempts int; ErrCode string; TTL time.Duration }
type Status uint8 // Unknown, Fresh, Stale, Error; derived at read time
func (f Freshness) Derive(now time.Time) Status
type Value[T any] struct { V T; Fresh Freshness }
type RepoID struct { Source string; GitHubID int64 } // identity; survives rename and transfer
type RepoKey string                                  // "owner/name", lower-case: the URL address

type Snapshot struct {               // immutable after publish; handlers call Current() once per request
	Version uint64; BuiltAt time.Time
	Repos   map[RepoKey]*RepoEntry   // entries shared by pointer between versions
	Aliases map[RepoKey]RepoKey      // old address -> current (308)
	Groups  []Group                  // config order, membership resolved by catalog
	Latest  []CommitRef; LatestByGroup map[string][]CommitRef // feeds, derived at build time
	Sources []SourceStatus           // last listing per source
}
type RepoEntry struct { Repo Repo; Commits Value[[]Commit]; Docs Value[[]Doc]; Release Value[*Release]; Manifests Value[[]Manifest] } // types: catalog.md, content.md, manifests.md
type Builder struct{ /* copy-on-write over the current Snapshot */ }
func (b *Builder) Repo(k RepoKey) (*RepoEntry, bool) // a private copy to mutate; false if not in the catalogue
type Publisher struct{ cur atomic.Pointer[Snapshot] /* + pending mutations, debounce timer */ }
func (p *Publisher) Current() *Snapshot
func (p *Publisher) Update(fn func(*Builder)) // queued; applied in order at the next debounced publish
```
```go
package sync
type Priority uint8 // PrioEvent=1, PrioScheduled=2, PrioBackfill=3; lower runs first
type Event uint8    // EventPush, EventTag, EventRepoChanged, EventSourceChanged (mapping: github.md behaviour 10)
type Key string     // "<kind>/<owner>/<name>" for repository jobs, "<kind>/<source>" for source jobs
type Item struct { Key Key; Fresh snapshot.Freshness; ContentSHA string; Payload []byte }
type Result struct {
	State snapshot.State; Unchanged bool; ContentSHA string; Payload []byte
	Catalogue []snapshot.Repo; Remove []snapshot.RepoID // catalog jobs only: full source catalogue; removals at once
}
type Spec struct {
	Kind    string; PerSource bool; Prio Priority; TTL time.Duration; Timeout time.Duration
	On      []Event                                                 // events that enqueue this kind for the event's repository or source
	Run     func(ctx context.Context, k Key, prev Item) (Result, error)
	Publish func(b *snapshot.Builder, k Key, it Item)               // decode Payload into the snapshot; warm start and live
}
type Budget interface{ Remaining(source, owner string) (frac float64, reset time.Time) } // satisfied by *github.Client
type Engine struct{ /* specs, keyed table, ready heaps per priority, delayed heap, pauses per source */ }
func (e *Engine) Register(s Spec) error                         // before Run only
func (e *Engine) Notify(ev Event, repo snapshot.RepoKey) error  // webhook sink and catalog; never blocks; ErrQueueFull when full
func (e *Engine) Run(ctx context.Context) error
// Errors from Run are classified by behaviour (errors.As):
type RateLimited interface{ RetryAt() time.Time }; type AuthFailed interface{ AuthFailed() bool }; type Permanent interface{ Permanent() bool }
```
```sql
-- migrations/0001_init.sql; all tables STRICT; times are UTC unix seconds written by the application
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT;            -- created_at, last_reconcile
CREATE TABLE repo (id INTEGER PRIMARY KEY, source TEXT NOT NULL, github_id INTEGER NOT NULL, owner TEXT NOT NULL, name TEXT NOT NULL,
  seen_at INTEGER NOT NULL, meta TEXT NOT NULL CHECK (json_valid(meta)),          -- catalog's tracked row (facts, aliases, misses)
  UNIQUE (source, github_id), UNIQUE (owner, name)) STRICT;
CREATE TABLE item (key TEXT PRIMARY KEY, kind TEXT NOT NULL,
  repo_id INTEGER REFERENCES repo(id) ON DELETE CASCADE,                          -- NULL for source jobs
  status TEXT NOT NULL CHECK (status IN ('ok','not_found','too_large','no_content','unsupported','error','never')),
  fetched_at INTEGER, checked_at INTEGER NOT NULL, next_due_at INTEGER, attempts INTEGER NOT NULL DEFAULT 0,
  content_sha TEXT, error TEXT,
  payload BLOB) STRICT;                                                           -- last column; feature JSON with inner "schema" field
```

## Behaviour (numbered essentials and the edge cases that matter)
1. **Planner tick** (every `sync.tick`): for each registered kind in registration order, every item with `next_due_at <= now` and not queued
   is enqueued at `Spec.Prio`; items never fetched go at `PrioBackfill`. `commits.refresh` registers before `docs.refresh` so commits fill first.
   Repository items exist for every repository in the catalogue; a new repository's items are due immediately.
2. **Enqueue** (keyed): absent -> create; queued -> raise priority, pull run time earlier, never later; running -> mark dirty, exactly one
   follow-up run after success; in backoff -> raise priority only (backoff holds during a push burst). Table full (`sync.max_keys`) -> error.
3. **Notify(ev, repo)** enqueues every spec whose `On` contains `ev` at `PrioEvent`: repository specs for `repo`, source specs for the
   source that owns `repo`'s owner (`EventSourceChanged` passes `"owner/"`). Ten pushes to one repository while queued give one run; while
   running, one run plus one follow-up. A full table returns `ErrQueueFull`.
4. **Budget gate**: at most `sync.concurrency` running jobs, starts spaced by `sync.pace`. Before each start the job's class must leave
   `Budget.Remaining(source, owner)` above its reserve (backfill 40%, scheduled 15%, event 2%); a held class waits until `reset`. Each job has `Timeout`.
5. **Result handling** (one transaction for item row + `next_due_at`, then `Publish` via `Publisher.Update`):
   - success -> fresh row, `attempts=0`, `next_due_at = now + TTL*(1±jitter)`; `Unchanged` (304 or equal SHA) updates times only.
   - transient -> `attempts++`, error code stored, payload and `fetched_at` kept (stale-while-error), retry after `base..min(max, base<<n)`
     with full jitter, forever at the cap.
   - rate limited -> pause all starts for the source until `RetryAt` + 0..1 s stagger; without a `RetryAt` (secondary limit without
     `Retry-After`) 60 s doubling to 15 min, reset by one success. The attempt is not consumed, the row is untouched.
   - auth failed (401) -> pause the source 5 min, doubling to 1 h; status shows it.
   - permanent (incl. permission errors and a recovered panic) -> error recorded, next due after `sync.permanent_retry` (24 h).
   - a job that reports `not_found` or `no_content` stores that state as a normal result.
6. **Catalogue**: `catalog.reconcile` returns `Result.Catalogue`; `sync` applies it with one `ReplaceCatalogue` for that source (upsert by
   `(source, github_id)`, then owner/name; delete the rest; items cascade). `Result.Remove` deletes those repositories at once. A result
   arriving for a removed repository writes nothing (`repo_id` lookup misses) and `Builder.Repo` returns false.
7. **Snapshot build**: `Update` mutations are queued; the publisher applies them in order to a builder over the current snapshot after 200 ms
   without new updates or 1 s after the first pending one, derives `Groups`, `Latest` and `LatestByGroup`, sets `Version+1`, and stores the
   atomic pointer. Builds run beside readers; a panic in a mutation drops that batch, keeps the old snapshot and is logged and counted.
8. **Store open**: version guard (SQLite >= 3.51.3), pragmas `journal_mode=WAL`, `synchronous=NORMAL`, `busy_timeout=5000`, `foreign_keys=1`,
   `temp_store=MEMORY` (read-only root filesystem), `quick_check`, migrate. Corrupt file, failed migration or `user_version` newer than the binary
   -> rename the file and its `-wal`/`-shm` to `scp.db.quarantine-<UTC>` (keep 2), start cold. One writer connection, a pool of 2 readers.
9. **Migrations**: embedded `NNNN_name.sql`, contiguous; each runs in one transaction together with `PRAGMA user_version = N`; additive where
   possible; payload changes use the inner `schema` field. Rows of unknown kinds and of sources no longer configured are deleted at open.
10. **Warm start**: read repo and item rows, call each kind's `Publish` into one builder, publish version 1, restore `next_due_at`. A restart
    inside the TTLs makes almost no GitHub calls. HTML read back from the store is re-sanitised (it is untrusted bytes).
11. **Degraded store**: a write error is logged once per minute and counted; the snapshot is still published (it is what serving reads).
12. **Readiness** (`/readyz` 200): the store is open and a snapshot with a catalogue is published, from the warm start or from the first
    complete listing. Once ready it stays ready until shutdown, through any GitHub outage. `/livez` fails only when the dispatcher heartbeat
    is older than 30 s.
13. **Shutdown** (SIGTERM, within `server.shutdown_grace`): `/readyz` 503; HTTP servers `Shutdown` while the engine stops starting jobs;
    in-flight jobs keep the rest of the grace on `context.WithoutCancel`, then are cancelled (row untouched, overdue after restart); pending
    `Update`s are dropped; the store closes last (`PRAGMA optimize`, `wal_checkpoint(TRUNCATE)`). A second signal exits immediately.
14. Edge cases: clock skew reads as fresh; a 304 never creates a value; `not_found` ages like a result; jobs for a repository renamed during a run
    resolve by `(source, github_id)`; a rate-limit error hitting 4 in-flight jobs pauses once, not four times.

## Config (only v1 keys, Viper dotted names, defaults)
| Key | Default | Key | Default |
|---|---|---|---|
| `sync.concurrency` | 4 | `sync.pace` | 100ms |
| `sync.tick` | 15s | `sync.max_keys` | 20000 |
| `sync.job_timeout` | 2m | `sync.jitter` | 0.1 |
| `sync.retry.base` | 2s | `sync.retry.max` | 15m |
| `sync.permanent_retry` | 24h | `store.path` | /data/scp.db |
| `sync.reserve.backfill` / `.scheduled` / `.event` | 0.40 / 0.15 / 0.02 | `snapshot.debounce` / `snapshot.max_delay` | 200ms / 1s |

TTLs are owned by the features that register the specs: `catalog.reconcile_interval` 5m (the `pushed_at` listing),
`catalog.descriptor.refresh_interval` 24h, `commits.ttl` 2h (safety net), `docs.ttl` 1h, `badges.release_ttl` 24h.

## Tests (what proves it)
- Engine in `synctest.Test` bubbles with a fake `Budget` and fake jobs: every cell of the enqueue table (2), burst coalescing and the single dirty
  follow-up (3), concurrency and pace limits, reserves per priority, backoff bounds (table and `testing/quick`), rate-limit and auth pauses
  without attempt and without escalation across in-flight jobs, permanent retry after 24 h, panic recovery, shutdown grace then cancel;
  `goleak` on every package.
- Cold start: 1,000 repositories against `githubtest`: all commits jobs before docs, calls counted against the budget, readiness turns ready.
- A changed `pushed_at` in the listing enqueues the push jobs of exactly that repository.
- Snapshot: debounce timing in a bubble (one publish per burst, max delay honoured); old pointers stay valid and unchanged (`-race`); a removed
  repository is not resurrected by a late result; `Derive` table including the 1.5 x TTL boundary.
- Store against a temp file: stale-while-error (failure never touches payload or `fetched_at`), 304 path, catalogue replace with rename and cascade,
  migration atomic (failure leaves `user_version`), quarantine on corrupt file and on newer schema, warm start round trip equals the live snapshot.
- Readiness: warm and cold paths; stays ready after a simulated GitHub outage.

## Reference (pointers to archive sections worth reading when implementing)
- `reference/detailed-design/20-design/04-refresh-webhook.md` 4.2 (enqueue table), 4.4-4.5 (result handling, backoff, rate-limit streak), 4.8 (shutdown), 4.9 (planner, cold start arithmetic).
- `reference/detailed-design/20-design/03-readmodel-store.md` 4.5 (catalogue replace: rename, transfer), 5.1 (pragmas, modernc/libc pin), 5.3 (SQL statements), 5.4 (migration runner).
- `reference/detailed-design/20-design/01-kernel.md` 4.2 FR-01..FR-03 (freshness derivation and transitions), 4.3 JB-05 (error classification precedence).
- `reference/detailed-design/10-architecture.md` 9.6 (TTL and budget table), 10.4 (shutdown order).
- `reference/detailed-design/05-research/05-store-sync-engine.md` 5.4-5.6 (dispatch, budget), 5.14 (synctest patterns); prototype in `05-store-sync-engine.assets/engine/`.
- `reference/detailed-design/11-adr/ADR-015-resilience.md` (budget reserves, stale-while-error).
