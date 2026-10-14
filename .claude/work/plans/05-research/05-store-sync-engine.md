# 05 - Research: persistence, snapshot read model and sync engine

## Executive summary (10 lines)

1. **Read model:** a feature-agnostic `readmodel` kernel with typed slots; immutable per-repository entries shared by pointer between snapshots; derived indexes (latest-N, group lists) rebuilt from entries by registered projectors; published with one `atomic.Pointer` swap. Measured at 1,000 repos: 0.31 ms full index rebuild, 0.04 ms per single-repo copy-on-write, +0.12 MB per extra live snapshot, 52 MB for the whole synthetic corpus [M1].
2. **Handlers** call `Current()` once per request and pass the `*Snapshot` down; staleness is derived at read time (`now - fetched_at`), never by publishing snapshots because time passed. Change notification is a coalescing wake-up plus a `ChangeSet` ring keyed by snapshot version, serving SSE (`Last-Event-ID` resume) and ETag polling from the same primitive.
3. **Persistence:** embedded SQLite through **`modernc.org/sqlite` v1.60.1 (SQLite 3.53.4)** behind a narrow `Store`; **`ncruces/go-sqlite3` v0.35.6 is the conformance-tested escape hatch** (it is now wasm2go-translated Go, no longer wazero). WAL, `synchronous=NORMAL`, one writer connection with `_txlock=immediate`, a small `query_only` reader pool, `busy_timeout=5000`; refuse SQLite older than 3.51.3 (WAL-reset corruption bug) [S12,S14].
4. **Schema/ops:** STRICT rowid tables, payload column last, stale-while-error (failures never overwrite the last good payload), bounded commit retention in the same transaction, `ON DELETE CASCADE` for vanished repos; forward-only migrations with a ~60-line embedded runner on `PRAGMA user_version` (goose Provider is the acceptable alternative, Atlas is not); startup `quick_check` (22 ms at 66 MB) and **quarantine-and-rebuild** on corruption because the file is a cache of GitHub; backups only via `VACUUM INTO`.
5. **bbolt** stays the documented alternative (v1.5.0, 10 ms warm load, +0.18 MB binary, but no SQL, no shrink, per-commit fsync); **Pebble** (+15 MB binary, CockroachDB-oriented) and **Badger** (+8.5 MB) are rejected for this workload [M2,M5,S21,S23,S24].
6. **Sync engine: build, do not adopt.** River's SQLite driver is "early testing" and its unique-job rule counts `running` as a duplicate (a webhook arriving mid-run would be swallowed); asynq/machinery need Redis or brokers; gocraft/work was last released in 2018; Watermill/goqite are messaging queues without keyed coalescing [S25-S33]. A ~550-line engine (compile-checked, 10 synctest tests, 30x `-race`) covers keyed coalescing with a dirty-while-running follow-up, per-upstream queues/concurrency/pace, priorities, full-jitter retries with a floor, rate-limit pause that does not burn attempts, parking of permanent failures, panic containment, graceful drain [M4].
7. **Level-triggered, not log-based:** durable truth is the per-item row (`fetched_at`, `next_due_at`, `attempts`, `status`, `etag`); the queue is in-memory and derived; restart resumes by re-deriving due work (mostly conditional requests, free against the primary limit when authorised) [S43]. Webhooks are accelerators; GitHub does not redeliver by itself, so downtime is repaired by the scan [S45].
8. **Scheduler:** fixed-delay with jitter (runs cannot overlap by construction), `Trigger` skips if running, cadence resumed from persisted `last_success`, one due-scanner instead of 1,000 timers; gocron v2 is viable but unnecessary, robfig/cron v3.0.1 is stale (2020).
9. **Lifecycle:** ordered startup (store, migrate, check, warm-load, reconcile, coverage gate), readiness that never regresses after first success except on shutdown (single replica), and shutdown order readiness-off, HTTP drain, engine drain with `Grace`, checkpoint, close.
10. **Testing:** `testing/synctest` (GA in Go 1.25, `synctest.Sleep` in Go 1.27) over an in-memory fake `Store` and fake upstream gives deterministic time with no `Clock` interface in the engine; real SQLite works inside a bubble only when called synchronously and closed [M3]. The dependency set raises the minimum toolchain to **Go 1.26** [S2].

---

Status: research input to the design phase. Conforms to `04-decisions.md` (read first). Proposed changes to a decision are only in section 9 "Deviations".
Date of research and of every web verification below: **2026-10-13** unless a row says otherwise.
Method: primary sources first (official docs, release notes, module proxy metadata), plus **local measurements and compile-checked prototypes** run on 2026-10-13 (section 0.3). Nothing below is "from memory" unless it is listed in section 13 "Unverified".

Evidence tags: `[S12]` = web fact, resolved to (URL, date verified) in the registry in section 0.2. `[M3]` = local measurement, defined in section 0.3. `(inference)` = my reasoning from cited facts, not a quoted fact.

---

## 0. Evidence

### 0.1 Constraints taken from 04 (binding)

D-02 (immutable `Snapshot` behind `atomic.Pointer`, SQLite WAL behind a narrow `Store`, never on the request path), D-03 (one sync package: scheduling, keyed coalescing queue, per-upstream limits, jittered backoff, rate-limit pause, readiness gating; features register jobs, the engine knows no GitHub specifics), Q-09 (up to 1,000 repos; commits 5 min, README 60 min conditional, repo list 30 min, status 5-15 min), Q-10 (single replica, state is a rebuildable cache, `Store` must not preclude multi-replica), D-16 (SPA; live updates by polling+ETag or SSE, research decides), and the guardrails in `02-design-guardrails.md`: GR-02 (readiness), GR-16/GR-21 (status codes, coalesced webhook jobs), GR-23 (typed errors, backoff), GR-24 (scheduling, no lost work), GR-28 (immutability, prebuilt indexes), GR-29 (typed states), GR-41 (health from outcomes).

### 0.2 Source registry (every row verified 2026-10-13)

| Tag | Source URL | What it was used for |
|---|---|---|
| S1 | https://go.dev/doc/devel/release | Go release status |
| S2 | https://proxy.golang.org/{module}/@latest and `/@v/{version}.mod` (table in 0.2.1) | module versions, timestamps, `go` directives |
| S3 | https://pkg.go.dev/modernc.org/sqlite | modernc DSN, caveats, version, licence |
| S4 | https://gitlab.com/cznic/sqlite/-/tags | modernc release cadence |
| S5 | https://gitlab.com/cznic/sqlite/-/raw/master/CHANGELOG.md | DSN validation order, OFD locking, WAL-shm fault handling, StrictPragmas |
| S6 | https://pkg.go.dev/github.com/ncruces/go-sqlite3 | ncruces version, licence, implementation |
| S7 | https://github.com/ncruces/go-sqlite3 | ncruces README (wasm2go, features, platforms) |
| S8 | https://github.com/ncruces/go-sqlite3/releases | ncruces release notes (Windows WAL corruption fix, allocator) |
| S9 | https://pkg.go.dev/github.com/ncruces/go-sqlite3/driver | ncruces DSN, `_txlock`, default busy timeout |
| S10 | https://github.com/ncruces/wasm2go | wasm2go translator status |
| S11 | https://github.com/cvilsmeier/go-sqlite-bench | third-party driver benchmark (2026-03-23 run) |
| S12 | https://sqlite.org/wal.html | WAL semantics, checkpoints, network FS, WAL-reset bug |
| S13 | https://sqlite.org/pragma.html | pragmas |
| S14 | https://sqlite.org/changes.html | SQLite release log (latest 3.54.0, 2026-10-09) |
| S15 | https://sqlite.org/lang_vacuum.html | `VACUUM INTO` |
| S16 | https://sqlite.org/howtocorrupt.html | corruption causes |
| S17 | https://sqlite.org/lang_transaction.html | BEGIN IMMEDIATE vs DEFERRED |
| S18 | https://sqlite.org/stricttables.html | STRICT |
| S19 | https://sqlite.org/withoutrowid.html | WITHOUT ROWID guidance |
| S20 | https://sqlite.org/lang_upsert.html | UPSERT (3.24.0) |
| S21 | https://github.com/etcd-io/bbolt | bbolt limitations and durability |
| S22 | https://github.com/etcd-io/bbolt/releases | bbolt release dates |
| S23 | https://github.com/cockroachdb/pebble | Pebble status and intended use |
| S24 | https://github.com/dgraph-io/badger | Badger features and caveats |
| S25 | https://github.com/riverqueue/river | River overview, licence |
| S26 | https://riverqueue.com/docs/sqlite | River SQLite status and settings |
| S27 | https://pkg.go.dev/github.com/riverqueue/river/riverdriver/riversqlite | riversqlite status |
| S28 | https://riverqueue.com/docs/unique-jobs | River uniqueness semantics |
| S29 | https://riverqueue.com/docs/job-retries | River retry policy |
| S30 | https://github.com/hibiken/asynq | asynq (Redis) |
| S31 | https://github.com/RichardKnop/machinery | machinery (brokers) |
| S32 | https://github.com/ThreeDotsLabs/watermill | Watermill |
| S33 | https://github.com/maragudk/goqite | goqite |
| S34 | https://pkg.go.dev/github.com/go-co-op/gocron/v2 | gocron v2 features |
| S35 | https://github.com/alitto/pond | pond v2 features |
| S36 | https://pkg.go.dev/github.com/maypok86/otter/v2 | otter v2 |
| S37 | https://pkg.go.dev/github.com/pressly/goose/v3#Provider | goose Provider API |
| S38 | https://pressly.github.io/goose/documentation/provider/ | goose Provider docs |
| S39 | https://github.com/pressly/goose | goose overview, licence |
| S40 | https://atlasgo.io/getting-started | Atlas workflow, paid features |
| S41 | https://github.com/ariga/atlas/blob/master/LICENSE | Atlas licence file |
| S42 | https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api | GitHub primary/secondary limits |
| S43 | https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api | conditional requests, serial requests, retry guidance |
| S44 | https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks | 10 s response, delivery id, queue |
| S45 | https://docs.github.com/en/webhooks/using-webhooks/handling-failed-webhook-deliveries | no automatic redelivery, redelivery API |
| S46 | https://pkg.go.dev/testing/synctest | synctest API and restrictions |
| S47 | https://go.dev/blog/testing-time | synctest rationale, GA in 1.25 |
| S48 | https://go.dev/doc/go1.27 | Go 1.27 changes |
| S49 | https://go.dev/doc/go1.26 | Go 1.26 changes |
| S50 | https://go.dev/doc/go1.25 | Go 1.25 changes |
| S51 | https://pkg.go.dev/sync/atomic#Pointer | `atomic.Pointer`, sequential consistency |
| S52 | https://pkg.go.dev/golang.org/x/sync/singleflight | singleflight API |
| S53 | https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/ | jitter variants |
| S54 | https://kubernetes.io/docs/concepts/configuration/liveness-readiness-startup-probes/ | probes |
| S55 | https://html.spec.whatwg.org/multipage/server-sent-events.html | SSE semantics |
| S56 | https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events | SSE connection limits |
| S57 | https://pkg.go.dev/net/http#ResponseController | method index only (semantics not fetched) |
| S58 | https://upd.dev/ncruces/go-sqlite3 (mirror, found by web search) | commit "Switch from wazero to wasm2go (#362)" dated 2026-03-12; secondary source |
| S59 | https://sqlite.org/fileformat2.html | overflow pages |

#### 0.2.1 Module facts from the Go module proxy [S2]

| Module | Latest | Published (UTC) | `go` directive |
|---|---|---|---|
| modernc.org/sqlite | v1.60.1 | 2026-09-29 | 1.26.0 |
| github.com/ncruces/go-sqlite3 | v0.35.6 | 2026-09-23 | 1.26.0 (depends on `github.com/ncruces/go-sqlite3-wasm/v6 v6.3.35304` in my build) |
| go.etcd.io/bbolt | v1.5.0 | 2026-06-03 (tag time; GitHub releases page says 21 Jun 2026 [S22]) | 1.25.0 |
| github.com/cockroachdb/pebble/v2 | v2.1.7 | 2026-08-24 | not read |
| github.com/dgraph-io/badger/v4 | v4.9.6 | 2026-08-05 | not read |
| github.com/pressly/goose/v3 | v3.29.0 | 2026-10-11 | 1.26.0 |
| ariga.io/atlas | v1.3.0 | 2026-07-29 | not read |
| github.com/golang-migrate/migrate/v4 | v4.20.1 | 2026-09-09 | not read |
| github.com/riverqueue/river (+ riverdriver/riversqlite) | v0.49.0 | 2026-10-05 | 1.26.0 |
| github.com/hibiken/asynq | v0.26.0 | 2026-02-03 | not read |
| github.com/RichardKnop/machinery/v2 | v2.0.16 | 2025-08-22 | not read |
| github.com/ThreeDotsLabs/watermill | v1.5.3 | 2026-08-25 | not read |
| github.com/gocraft/work | v0.5.1 | 2018-05-22 | not read |
| github.com/maragudk/goqite | v0.4.0 | 2025-11-20 | not read |
| github.com/panjf2000/ants/v2 | v2.12.1 | 2026-05-25 | not read |
| github.com/alitto/pond/v2 | v2.7.2 | 2026-10-03 | 1.20 |
| github.com/robfig/cron/v3 | v3.0.1 | **2020-01-04** | not read |
| github.com/go-co-op/gocron/v2 | v2.22.0 | 2026-07-09 | 1.22 |
| github.com/maypok86/otter/v2 | v2.3.0 | 2025-12-22 | 1.24.0 |
| golang.org/x/sync | v0.24.0 | 2026-09-23 | 1.26.0 |
| github.com/cenkalti/backoff/v5 | v5.0.3 | 2025-07-23 | not read |
| github.com/sony/gobreaker/v2 | v2.4.0 | 2026-01-01 | not read |
| github.com/jonboulle/clockwork | v0.5.0 | 2024-11-29 | not read |
| github.com/coder/quartz | v0.3.1 | 2026-04-09 | not read |
| github.com/failsafe-go/failsafe-go | v0.9.8 | 2026-09-30 | not read |
| go.uber.org/goleak | v1.3.0 | 2023-10-24 | not read |
| github.com/mattn/go-sqlite3 (CGO) | v1.14.52 | 2026-09-05 | not read |

Go toolchain [S1]: Go 1.27 released 2026-08-19 (latest 1.27.2, 2026-10-08); Go 1.26 released 2026-02-10 (latest 1.26.9, 2026-10-08); Go 1.25 (1.25.14) is **unsupported** since 1.27 shipped. SQLite [S14]: latest 3.54.0 (2026-10-09); both drivers bundle **3.53.4** (2026-07-24) [S3,S8].

### 0.3 Local measurements and prototypes (run 2026-10-13)

Hardware/OS: Apple M4 Max (16 cores), 64 GB, macOS (Darwin 27.0.0), Go 1.27.1 darwin/arm64. **Not Linux, not a container, not a production filesystem**; macOS `fsync` does not flush the drive cache unless `F_FULLFSYNC` is used, so durability-cost numbers are not representative (see Risks). Source for everything is in `05-research/05-store-sync-engine.assets/` (README there); drivers used: modernc v1.60.1, ncruces v0.35.6, bbolt v1.5.0, pebble v2.1.7, badger v4.9.6.

| ID | Measurement | Result |
|---|---|---|
| M1 | Snapshot kernel, synthetic org: 1,000 repos, 20 KB raw + 25 KB HTML README, 30 commits, 60 groups, 1-2 groups/repo, global top-100 and per-group top-50 latest commits (`snapshot-bench`) | Full index rebuild from shared entries **308 us**, 3,049 allocs; copy-on-write of 1 repo **36.9 us**; of 50 repos **141 us**; map clone alone 2.5 us; 16 parallel readers (GOMAXPROCS) while a writer swaps every 1 ms: **0.92 ns/op, 0 allocs**. Memory: one snapshot **52.1 MB**; a second CoW snapshot **+0.12 MB**; a second fully rebuilt index sharing entries **+0.14 MB**. At 10,000 repos (200 groups): full rebuild 3.26 ms, CoW-1 0.43 ms, CoW-50 0.78 ms, clone 21 us |
| M2 | SQLite store, same corpus persisted: 34,000 rows, 57.0 MB payload, 66.3 MB file, STRICT rowid tables, 1 writer conn + `_txlock=immediate`, WAL (`sqlite-bench`) | modernc: open 3 ms, populate (1 tx) 560 ms, reopen 1 ms, **warm-load all 34,000 rows 46 ms**, `quick_check` 22 ms, `integrity_check` 70-72 ms, autocommit upsert of a 25 KB payload 172 us (NORMAL) / 186 us (FULL), `VACUUM INTO` 144 ms. ncruces: warm-load **31-32 ms**, `quick_check` 19-21 ms, `integrity_check` 51-54 ms, upsert 156-180 us, `VACUUM INTO` 84-117 ms. 3 s mixed load (1 writer + 4 `query_only` readers): modernc 24,292 writes/s and 282,765 reads/s; ncruces 36,617 writes/s and 224,547 reads/s; **0 errors** for both |
| M3 | modernc real file I/O inside a `synctest` bubble (`sqlite-in-synctest`) | Works when called synchronously and `db.Close()` runs before the bubble ends; fake time elapsed during the I/O: 0 s; bubble becomes idle normally afterwards |
| M4 | Engine prototype tests under `synctest` (`engine`, 396 + 155 lines of implementation, 353 lines of tests) | 10 tests pass; `go test -race -count=30` completes in about 1.1 s of real time covering minutes-to-hours of fake time |
| M5 | Static linux/amd64 binary, `CGO_ENABLED=0 -trimpath -ldflags="-s -w"`, minimal program that actually opens the engine | baseline (`database/sql` only) 1.68 MB; modernc 6.33 MB (**+4.65**); ncruces 7.73 MB (**+6.05**); bbolt 1.86 MB (+0.18); Pebble v2 16.63 MB (**+14.95**); Badger v4 10.15 MB (+8.47). All five build with `CGO_ENABLED=0` |
| M6 | bbolt v1.5.0, same data (34,000 keys, 57.0 MB) | populate 145 ms, reopen 3 ms, **warm-load with copy 10 ms**, upsert of 25 KB with an fsync per transaction **8.9 ms** (macOS; not comparable to SQLite NORMAL, which does not fsync on commit in WAL), `Check` 7 ms |
| M7 | Schema and statements (`ddl/0001_init.sql`, `ddl_test.go`, `migrate_test.go`) | DDL, upsert, 304 revalidation, failure-keeps-payload, commit trim with a covering index (no temp b-tree), `ON DELETE CASCADE`, `integrity_check ok` verified on modernc (SQLite 3.53.4); multi-statement DDL + `PRAGMA user_version` in one transaction and rollback of a broken migration verified on **both** drivers |
| M8 | Typed-slot kernel (`readmodel`, 263 lines) | immutability, shared unchanged slots, shared sorted name slice, projector-built global index, wake-up channel, `ChangesSince`, per-entry `Rev`; `-race` with 4 concurrent readers and 200 swaps |

---

## 2. The three roles and how they connect

```
 GitHub / Jenkins / Snyk                         webhooks (HMAC verified)
        |  conditional requests (ETag)                |  Enqueue(PrioWebhook)  (O(1), coalescing, never blocks)
        v                                             v
 +---------------------------  syncengine  -----------------------------+
 | Planner (level-triggered)  --Enqueue-->  keyed coalescing queue       |
 |   scans item state each tick              per-upstream: ready+delayed |
 | Scheduler (fixed-delay+jitter)            heaps, concurrency, pace,   |
 |   reconcile, status polls                 rate-limit pause            |
 +--------------------------+--------------------------------------------+
                            | Handler(ctx, Job)  (feature code: fetch, parse, render)
                            v
              1. store.Upsert*/Revalidated/RecordFailure   (SQLite, durable last-good)
              2. publisher.Apply(batch)                     (build -> project -> validate -> atomic swap)
                            |
                            v
        readmodel.Publisher --Current()--> HTTP handlers (one load per request)
                            --Subscribe()--> SSE / ETag (version + ChangeSet)
```

Rules that follow from 04: handlers never touch SQLite; the store is written before the swap so a restart sees what was served (a store failure does **not** block publication: serving matters more than durability, the error is counted and `/status` shows `store: degraded`); the engine knows `Job{Key,Kind,Upstream,Prio}` and nothing about GitHub.

---

## 3. Part A - Immutable snapshot read model

### 3.1 Decision

* **Granularity: per-repository immutable entries shared by pointer; derived indexes rebuilt per publish; whole-snapshot atomic swap.** Per-repo sharing is what matters for memory (a deep copy per swap would be 52 MB [M1]); rebuilding the derived indexes is so cheap at the target size (0.31 ms at 1,000 repos, 3.3 ms at 10,000 [M1]) that incremental index maintenance would add complexity for no measurable gain. Revisit only if repos exceed about 10,000 or swap rate exceeds about 100 per second.
* **Do not batch swaps for CPU reasons.** A cold start of about 3,000 item completions at 0.04-0.3 ms each is about 1 s of CPU in total [M1]. Debounce at the **notification edge** (SSE writer), not at publication.
* **The kernel is feature-agnostic** (typed slots), because principle 2 forbids `store` importing feature internals while each feature's API handler must import the read model: concrete `Snapshot{Commits, Readme, ...}` fields would create an import cycle or force domain types into a shared leaf package (inference from D-01 principle 2). Compile-checked prototype: `assets/readmodel/readmodel.go` [M8].

### 3.2 Structure (Go, compile-checked)

```go
package readmodel

type RepoID string

// Typed handles, created once in cmd/scp from a Registry that is then frozen. No init(), no globals.
type Slot[T any] struct{ id int; name string }   // per-repository value kind: commits.View, docs.Readme, ...
type Global[T any] struct{ id int; name string } // snapshot-wide value: catalogue, derived indexes
type Registry struct{ /* names; frozen */ }
func Register[T any](r *Registry, name string) Slot[T]
func RegisterGlobal[T any](r *Registry, name string) Global[T]

type Snapshot struct { /* version, builtAt, repos map[RepoID]*entry, names []RepoID, globals []any, changed ChangeSet */ }
func (s *Snapshot) Version() uint64
func (s *Snapshot) BuiltAt() time.Time
func (s *Snapshot) Repos() []RepoID           // sorted; shared between snapshots while membership is unchanged
func (s *Snapshot) Rev(id RepoID) uint64      // snapshot version at which this repo's entry last changed
func (s *Snapshot) Changed() ChangeSet
func Get[T any](s *Snapshot, id RepoID, slot Slot[T]) (T, bool)
func GetGlobal[T any](s *Snapshot, g Global[T]) (T, bool)

type ChangeSet struct{ Version uint64; Repos []RepoID; Globals []string; Resync bool }

// Builder is a single-use copy-on-write view of the previous snapshot (single goroutine).
func Put[T any](b *Builder, id RepoID, slot Slot[T], v T)   // copies the entry (slots array), replaces one slot
func PutGlobal[T any](b *Builder, g Global[T], v T)
func (b *Builder) Remove(id RepoID)
func (b *Builder) View() *Snapshot                          // lets projectors read the candidate

type Projector func(b *Builder) error      // derive cross-repo indexes (latest N, reverse deps) from the candidate
type Validator func(*Snapshot) error       // referential integrity; failure => not published, previous kept

type Publisher struct{ /* atomic.Pointer[Snapshot]; mutex serialises builders; subscribers; change ring(256) */ }
func NewPublisher(reg *Registry, projectors []Projector, validators []Validator) *Publisher
func (p *Publisher) Current() *Snapshot                                       // wait-free
func (p *Publisher) Apply(fn func(*Builder) error) (*Snapshot, error)         // build -> project -> validate -> Store()
func (p *Publisher) Subscribe() (wake <-chan struct{}, cancel func())         // cap-1 channel: coalescing, never blocks Apply
func (p *Publisher) ChangesSince(version uint64) (changes []ChangeSet, ok bool) // ok=false => send "resync"
```

How a feature uses it (consumer-side, per D-01; illustrative, not compiled):

```go
// internal/commits/readmodel.go
type View struct{ Latest []Commit; FetchedAt time.Time; Status Status }   // immutable once Put
var latestGlobal readmodel.Global[[]CommitRef]                            // injected, not package-level in real code
func Project(slot readmodel.Slot[View], out readmodel.Global[[]CommitRef]) readmodel.Projector {
    return func(b *readmodel.Builder) error { /* k-way merge of per-repo newest-first lists, top N */ }
}
// internal/commits/api.go
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
    s := h.src.Current()                       // ONE load; src is a consumer-defined interface{ Current() *readmodel.Snapshot }
    v, ok := readmodel.Get(s, id, h.slot)
    ...
}
```

### 3.3 Memory cost at 1,000 repos

* Measured synthetic corpus (20 KB raw + 25 KB sanitised HTML README, 30 commits of about 500 B, 5 status slots per repo): **52.1 MB** for the first snapshot, **+0.12 MB** for each further snapshot that shares entries, **+0.14 MB** for an independent full index rebuild [M1]. This agrees with the earlier unmeasured planning estimate of 20-40 MB for READMEs and 12 MB for commits, and now measures it.
* Old snapshots are garbage-collected as soon as the last in-flight request drops them; only **replaced** entries stay alive that long. **Do not let long-lived goroutines (SSE handlers) hold a `*Snapshot`**; hold the `uint64` version (inference: a held snapshot pins every entry replaced afterwards).
* Controls: cap README at 1 MiB with the typed state `TooLarge` (GR-29); set `GOMEMLIMIT` to about 85% of the container limit; Go 1.26 made the Green Tea GC the default (10-40% less GC overhead for GC-heavy programs [S49]).

### 3.4 Consistency guarantees (what the design promises)

1. **Atomic visibility:** a batch becomes visible entirely or not at all (single `atomic.Pointer` store; atomics are sequentially consistent [S51]).
2. **Monotonic versions:** `Version` increases by one per publish; a goroutine never observes it going backwards.
3. **Validated before publish:** `Validator`s check referential integrity (every group member exists, default repo exists, commit refs resolve). A failing candidate is **not** published, the last good snapshot stays, the error is counted and surfaced in `/status` (fail closed to last good, GR-41).
4. **Per-request consistency:** exactly one snapshot if the handler loads once. **Across requests** (an SPA issuing several calls) consistency is eventual; every response carries `X-Snapshot-Version` and per-resource `Rev` so the client can detect skew.
5. **Time is not data:** "stale" is derived at read time from `now - fetched_at` versus the item's TTL; the engine never publishes a snapshot merely because time passed.
6. **Store/snapshot divergence is allowed** (store lags or fails); the snapshot is authoritative for serving, the store for restart.

### 3.5 How handlers obtain a snapshot, and change notification

* **Obtain:** constructor-injected consumer interface `interface{ Current() *readmodel.Snapshot }`; load once at the top of the handler and pass `s` down explicitly (no context-stashing by default, which hides the one-load rule).
* **Notification primitive (one source for SSE and polling):**
  * `Version` + `ChangeSet{Repos, Globals, Resync}` per publish; ring of the last 256 change sets for `Last-Event-ID` resume; a client further behind than the ring gets a `resync` event.
  * `Subscribe()` returns a **capacity-1 channel**: a slow client can never block `Apply`, and wake-ups coalesce (level-triggered; the subscriber re-reads `Current()`).
  * **Polling path:** `GET /api/v1/snapshot` returns `{version, builtAt, readiness}` with `ETag: W/"v<version>"`; unchanged = 304. Per-repo endpoints use `Rev` as ETag.
  * **SSE path** (`GET /api/v1/events`): `id: <version>`, `retry: 3000`, a comment line every 15 s as keep-alive [S55], debounce 250-500 ms before emitting, per-write deadline through `http.ResponseController` [S57 lists `Flush` and `SetWriteDeadline`; interplay with `Server.WriteTimeout` is not verified, section 13], handler must select on `r.Context().Done()` and the server base context so `Shutdown` is not blocked. HTTP/1.1 browsers allow only 6 SSE connections per browser and domain [S56]: one SSE connection per tab, plus a documented polling fallback (`refetchInterval` with the version check) for proxies that buffer.
  * The frontend decision (SSE vs polling) stays with the D-16 research; this contract serves both.

---

## 4. Part B - Persistence

### 4.1 Engine options with current facts

| Option | Current facts | Fit | Verdict |
|---|---|---|---|
| **SQLite via modernc.org/sqlite** | v1.60.1 (2026-09-29), bundles SQLite 3.53.4, BSD-3-Clause, `go 1.26`; a tagged release every 1-3 weeks since April (v1.50.0 2026-04-24 to v1.60.1, 13 tags in about 5 months) [S3,S4]; reports 1.3-2.0x the CPU time of C SQLite on CPU-bound work [S3]; tested by River [S26] | Items are rows with metadata, retention is a `DELETE`, operators get the `sqlite3` shell | **Choose** |
| SQLite via ncruces/go-sqlite3 | v0.35.6 (2026-09-23), MIT, `go 1.26`, **pre-1.0**; since 2026-03 it wraps a Wasm build **translated to Go by wasm2go**, only Go and `x/sys` are direct requirements [S6,S7,S10,S58]; v0.35.3 (2026-08) fixed a **Windows WAL data-corruption bug** under heavy concurrency [S8]; higher per-connection memory by its own README [S7] | Equivalent API through `database/sql`; faster warm load in my run [M2] | **Escape hatch**, run the store conformance suite against it in CI |
| bbolt | v1.5.0 (2026-06), "API fixed, file format fixed", exclusive file lock, cannot truncate the file, long read tx keep pages, fsync twice per commit [S21,S22] | Warm load 10 ms, +0.18 MB binary [M5,M6]; no SQL, retention/inspection need code and `bbolt` CLI | **Documented alternative** (D-02) |
| Pebble v2 | v2.1.7 (2026-08-24), BSD-3; "stable and production ready", built for CockroachDB; warns of silent corruption with unsupported RocksDB features; no TTL listed [S23] | LSM, overpowered; **+14.95 MB** binary [M5] | Reject |
| Badger v4 | v4.9.6 (2026-08-05); TTL, value log, SSI; platform caveats [S24]; I found no source on current sponsor/maintainer status (section 13) | LSM + value-log GC; **+8.47 MB** [M5] | Reject |
| mattn/go-sqlite3 | v1.14.52 (2026-09-05) | needs CGO | Excluded by D-11 |
| Redis/Valkey, etc. | out of scope (04) | | n/a |

SQLite facts that shape the design:

* WAL: readers do not block the writer and the writer does not block readers; **one writer at a time**; WAL does **not** work over a network filesystem; keeps `-wal` and `-shm` files next to the database [S12].
* Auto-checkpoint at 1000 pages; a reader that is always active starves checkpoints and the WAL grows without bound; read performance degrades as the WAL grows [S12].
* `synchronous=NORMAL` in WAL: always consistent, but "transactions are no longer durable and might rollback following a power failure or hard reset"; `FULL` adds a WAL sync per commit [S12,S13].
* **WAL-reset bug:** possible database corruption in all SQLite from 3.7.0 through 3.51.2 when two connections write/checkpoint at the same instant; fixed in 3.51.3 (2026-03-13) and 3.53.0; backports 3.44.6 and 3.50.7 [S12,S14]. Both drivers bundle 3.53.4, so we are clear, but a **startup guard** must refuse `sqlite_version() < 3.51.3`.
* `VACUUM INTO` produces a consistent transactional snapshot of a live database; an interrupted run can leave a corrupt output file; the target must not exist (or be empty) [S15].
* Corruption causes relevant here: deleting/renaming `-wal`/journal files, copying the database file while open, network filesystems, closing any file descriptor of the database file inside the process (strips POSIX locks), multiple names/hard links to one file, `synchronous=OFF` [S16].
* STRICT tables need SQLite 3.37.0+ [S18]; UPSERT 3.24.0+ [S20]; `WITHOUT ROWID` is for small rows and is **not** appropriate with large BLOBs (rule of thumb: average row below 1/20 of the page size) [S19].

### 4.2 Driver decision: modernc.org/sqlite, ncruces as tested escape hatch

Why modernc first: semver v1.x API, a release cadence of 1-3 weeks, River's tests use it [S26], and it exposes everything we need in the DSN: `_pragma`, `_txlock`, shorthand keys `_busy_timeout`, `_foreign_keys`, `_journal_mode`, `_synchronous` [S3]. Why ncruces stays in the CI matrix: it is the only CGO-free alternative with comparable ergonomics, and my numbers show no functional gap [M2,M7]; its newer wasm2go base and the Windows WAL corruption fix argue for letting it age before depending on it [S8,S10].

Operational gotchas (all sourced):

* modernc's `_pragma` values are executed as SQL text (anything after `;` also runs); recommended `StrictPragmas(true)` when the DSN is not a compile-time constant [S3,S5]. Our DSN contains a configurable path: **build the `file:` URI with `net/url` escaping and enable strict pragmas** (option name per [S3]; exact signature unverified).
* modernc v1.55.0 changed DSN validation: every parameter is checked before any is applied; `_busy_timeout=5s` (duration style) now **fails** the open; `_journal_mode=wal` converts the file persistently [S5]. Use integer milliseconds.
* modernc depends on `modernc.org/libc`, which the docs call "fragile": use exactly the version pinned in modernc's `go.mod`; group `modernc.org/{sqlite,libc,mathutil,memory}` in one Renovate rule [S3].
* **Never open the database file with `os.Open` anywhere in the process** (health checks, backups): closing any descriptor of the file strips SQLite's POSIX locks; modernc added opt-in Linux OFD locking (`MODERNC_SQLITE_OFD_LOCK=1` or `OFDLocking(true)` before the first open) but WAL `-shm` coordination still uses POSIX locks [S5,S16]. Backups go through `VACUUM INTO`.
* Pooled connections keep per-connection state (pragmas, ATTACH, temp tables) across borrowers, so every needed pragma must be in the DSN or a connection hook [S3]. `busy_timeout` and `foreign_keys` are per connection; `journal_mode=WAL` persists in the file [S13].
* ncruces: with no `_pragma` given it applies a **one-minute** busy timeout; `_txlock` accepts `deferred|immediate|exclusive`, but read-only transactions are always deferred and serializable ones always immediate [S9]. Set everything explicitly.
* `database/sql` defaults to unlimited open connections; bound the pool [S3].

### 4.3 Connection layout and pragmas

Two handles inside the SQLite store package, hidden behind `Store`:

| Handle | Pool | DSN essentials | Why |
|---|---|---|---|
| `rw` | `SetMaxOpenConns(1)`, `SetMaxIdleConns(1)`, `SetConnMaxLifetime(0)` | `_txlock=immediate`, `journal_mode(WAL)`, `synchronous(NORMAL)`, `busy_timeout(5000)`, `foreign_keys(1)`, `trusted_schema(0)`, `journal_size_limit(67108864)` | Single writer; `BEGIN IMMEDIATE` avoids the deferred read-to-write upgrade failing with `SQLITE_BUSY` [S17]; River's SQLite guide recommends exactly one connection, WAL, `busy_timeout(5000)`, `foreign_keys(1)` and warns that an open transaction monopolises the only connection [S26] |
| `ro` | `SetMaxOpenConns(2)` | same minus `_txlock`, plus `query_only(1)` | Warm load and admin queries never queue behind a write; measured 4 readers + 1 writer with zero errors [M2] |

Rationale for the less obvious items:

* `synchronous=NORMAL`: the file is a rebuildable cache (Q-10); the worst case after power loss is losing the most recent commits, never an inconsistent file [S12,S13]. A process crash does not lose NORMAL commits because the data is in the OS cache (inference; the docs speak only about power failure/hard reset). Make it configurable (`store.sync: normal|full`). My FULL vs NORMAL numbers are indistinguishable [M2] but are macOS numbers; re-measure on Linux CI.
* `journal_size_limit`: default is -1 (no limit) [S13]; 64 MiB bounds the leftover `-wal` after a checkpoint.
* `trusted_schema=0`: the docs recommend turning it off on each connection; the default is ON for backward compatibility [S13].
* Lifecycle pragmas: `PRAGMA optimize=0x10002` on open and `PRAGMA optimize` periodically (daily) and before close for long-lived connections [S13]; `application_id` set to a constant and `user_version` as the schema version [S13].
* Deadlock rule from `MaxOpenConns(1)`: inside a transaction use only the `tx`, never `db` (River documents the same trap) [S26]. Running the whole test suite with the single-connection pool surfaces violations immediately.
* Because the request path never reads SQLite, read throughput is irrelevant; `ro` exists for warm load and admin only.

### 4.4 Schema (executed and verified on modernc 3.53.4 [M7]; file: `assets/sqlite-bench/ddl/0001_init.sql`)

```sql
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT;

CREATE TABLE repo (                              -- persisted catalogue for warm start
  id INTEGER PRIMARY KEY, org TEXT NOT NULL, name TEXT NOT NULL, github_id INTEGER,
  seen_at INTEGER NOT NULL, meta TEXT NOT NULL,   -- JSON: description, default_branch, topics, visibility
  UNIQUE (org, name)) STRICT;

CREATE TABLE item (                              -- one row per (repo, kind): readme, release, status.*, manifests.*
  id INTEGER PRIMARY KEY,
  repo_id INTEGER NOT NULL REFERENCES repo(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('ok','not_found','unsupported','too_large','no_content','error','never')),
  fetched_at INTEGER,        -- last successful fetch OR successful 304 validation (unix seconds, UTC)
  checked_at INTEGER NOT NULL,-- last attempt
  next_due_at INTEGER,       -- planner state restored at warm start; no index (planner scans memory)
  attempts INTEGER NOT NULL DEFAULT 0,  -- consecutive failures
  etag TEXT, content_sha TEXT, error TEXT,
  payload BLOB,              -- LAST column
  UNIQUE (repo_id, kind)) STRICT;

CREATE TABLE commit_log (
  id INTEGER PRIMARY KEY,
  repo_id INTEGER NOT NULL REFERENCES repo(id) ON DELETE CASCADE,
  sha TEXT NOT NULL, committed_at INTEGER NOT NULL, payload BLOB NOT NULL,
  UNIQUE (repo_id, sha)) STRICT;
CREATE INDEX commit_log_recent ON commit_log (repo_id, committed_at DESC, id DESC);

CREATE TABLE task_state (name TEXT PRIMARY KEY, last_attempt INTEGER, last_success INTEGER, last_error TEXT) STRICT;
CREATE TABLE parked (key TEXT PRIMARY KEY, since INTEGER NOT NULL, reason TEXT NOT NULL, retry_at INTEGER NOT NULL) STRICT;
```

Design points:

* **Rowid tables, not `WITHOUT ROWID`**: rows carry 25-50 KB payloads and the docs say rowid tables are faster for large rows [S19]. **`payload` last** so metadata scans do not touch overflow pages (SQLite spills large payloads to overflow pages [S59]; that metadata scans avoid them when the payload is the last column is an inference, not a documented guarantee).
* **Typed states instead of NULL** (GR-29): `not_found`, `unsupported`, `too_large`, `no_content` are results, not errors; `error` is a failed attempt.
* **Stale-while-error (GR-26, GR-40):** a failure statement updates only `status, error, checked_at, next_due_at, attempts`; `payload` and `fetched_at` keep the last good value. **A 304 updates `fetched_at/checked_at/next_due_at` and never rewrites the payload.**
* **UTC unix seconds written by the application**, never SQLite `CURRENT_TIMESTAMP`/`strftime('now')` (inference: inside a `synctest` bubble the application clock is fake and SQLite's is not, so mixing them breaks deterministic tests).
* **Retention** in the same transaction as the insert: `INSERT OR IGNORE` the fetched commits, then
  `DELETE FROM commit_log WHERE repo_id=? AND id NOT IN (SELECT id FROM commit_log WHERE repo_id=? ORDER BY committed_at DESC, id DESC LIMIT ?)` (K = 30 default; query plan: covering index search, no temp b-tree [M7]).
* **Repo vanished:** `DELETE FROM repo WHERE id=?` cascades items and commits (needs `foreign_keys=1` on the writer connection).
* `item.kind` is a free string owned by features; the store does not interpret `payload` (JSON by default; features own their codecs and a `schema` field inside the payload so payload evolution does not need SQL migrations).

### 4.5 Migrations: own embedded runner (decision), goose as the alternative

| Option | Facts | Verdict |
|---|---|---|
| **Own runner** (~60 lines) on `embed.FS` + `PRAGMA user_version` | Verified on both drivers: multi-statement script and `PRAGMA user_version = N` in one `BEGIN IMMEDIATE` transaction commit together, a broken migration rolls back with `user_version` unchanged [M7] | **Choose**: forward-only (the database is a cache; "down" = delete the file), zero dependencies, deterministic |
| goose v3 | v3.29.0 (2026-10-11), MIT, `go 1.26`; `NewProvider(dialect, *sql.DB, fs.FS, opts...)` has no global state, `DialectSQLite3`, `WithSlog`, `WithDisableGlobalRegistry`, `Up/UpTo/Status/HasPending`, `fs.Sub` over `embed.FS` [S37-S39]; by default it does **not** lock the database [S38] | Acceptable if the team prefers a known tool; its Down/Go-migration/multi-DB features are unused here |
| Atlas | v1.3.0; repository licence Apache-2.0 [S41]; migration linting, testing, schema visualisation and CI integration are named as **Pro** features that need a login (the page is ambiguous about whether `migrate lint` itself is Pro) [S40]; CLI-centred declarative workflow | Reject (tooling weight, partly paid) |
| golang-migrate | v4.20.1 | Not evaluated beyond version; no advantage over the two above |

Runner behaviour (pseudocode):

```go
//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(ctx context.Context, rw *sql.DB) (applied int, err error) {
    cur := userVersion(rw)                      // PRAGMA user_version
    ms := parseSorted(migrationsFS)             // NNNN_name.sql, contiguous from 0001, checked at test time
    if cur > ms.Latest() { return 0, ErrSchemaNewer }           // binary was rolled back: caller quarantines + rebuilds
    for _, m := range ms.After(cur) {
        tx, _ := rw.BeginTx(ctx, nil)                           // _txlock=immediate
        _, err = tx.ExecContext(ctx, m.SQL)
        if err == nil { _, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.Version)) }
        if err != nil { tx.Rollback(); return applied, fmt.Errorf("migration %04d: %w", m.Version, err) }
        tx.Commit(); applied++
    }
    return applied, nil
}
```

Policy: a failed migration or `ErrSchemaNewer` triggers **quarantine and rebuild** (4.7), logged at ERROR, counted (`scp_store_rebuilds_total{reason}`), never silent. A unit test asserts that the embedded migrations applied to an empty database equal the schema snapshot checked into the repo.

### 4.6 Warm start

[M2]: reopen 1-13 ms; warm-load of 34,000 rows / 57 MB in **31-47 ms** (+ decode cost in Go, which is feature code and not measured); `quick_check` 19-23 ms; `integrity_check` 51-72 ms. The whole store phase of startup is well under 0.5 s at 1,000 repos. Warm load streams rows through the `ro` handle in one read transaction (consistent view), grouped by repo, and builds the first snapshot with one `Apply` (version 1, `Resync: true`).

### 4.7 Backup, restore, corruption (cache semantics)

Because state is a rebuildable cache of GitHub (Q-10), **backups are an optimisation of cold start, not a durability requirement.**

* **Startup check:** `PRAGMA quick_check` (O(N); skips UNIQUE and index/table agreement) every start; `PRAGMA integrity_check` on demand (`scp store check`, admin endpoint) [S13, M2].
* **On failure** (`quick_check != ok`, `SQLITE_NOTADB/CORRUPT`, schema newer than binary, failed migration): close handles; **rename the database and its `-wal`/`-shm` together** to `scp.db.quarantine-<UTC>`; start empty (cold start); count and log; keep the newest 2 quarantined sets and delete older ones (bounded disk). A second failure on the fresh file (disk fault) exits non-zero rather than looping.
* **Runtime errors** (`CORRUPT`, `IOERR`, `FULL`, read-only FS): `Store` flips to `degraded`, stops writing (probe every 30 s), serving continues from the snapshot, `/status` and a metric show it; recovery is by restart (quarantine) or automatic once writes succeed again. No in-process repair.
* **Optional backup:** `VACUUM INTO 'scp.db.bak.tmp'` (hourly and at graceful shutdown), then `quick_check` of the copy and an atomic rename to `scp.db.bak` [S15: an interrupted `VACUUM INTO` can leave a corrupt file]. Restore (`scp store restore`) copies the backup in place **while the service is stopped**; the level-triggered planner then catches up. `sqlite3_rsync`, Litestream and similar replication are unnecessary for a cache (Litestream status could not be verified, section 13).
* **Volume rules:** the database needs a writable volume even with a read-only root filesystem (D-09); `emptyDir` survives container restarts, a block-storage PVC survives rescheduling; **network filesystems (NFS/EFS/SMB) are unsupported** because WAL needs shared memory on one host [S12,S16]. One name per database file (no hard links) [S16].
* **Shutdown:** `PRAGMA optimize`, `wal_checkpoint(TRUNCATE)`, close `ro` then `rw`.

### 4.8 `Store` interface (pseudocode; consumer-side narrow interfaces are carved out of it per D-01)

```go
package store

type RepoRef struct{ Org, Name string }
type ItemKey struct{ Repo RepoRef; Kind string }
type Status string // ok | not_found | unsupported | too_large | no_content | error | never

type Item struct {
    Key                            ItemKey
    Status                         Status
    FetchedAt, CheckedAt, NextDue  time.Time // UTC; zero = unset
    Attempts                       int
    ETag, ContentSHA, Error        string
    Payload                        []byte    // nil on revalidation
}

type Store interface {
    // warm start (ro handle, single read tx, streaming)
    LoadCatalogue(ctx context.Context) ([]Repo, error)
    LoadItems(ctx context.Context, fn func(Item) error) error
    LoadCommits(ctx context.Context, repo RepoRef, limit int) ([]CommitRow, error)

    // writes (rw handle; each is one BEGIN IMMEDIATE transaction)
    ReplaceCatalogue(ctx context.Context, repos []Repo) (removed []RepoRef, err error) // cascades
    PutItem(ctx context.Context, it Item) error                                        // 200: upsert, attempts=0
    Revalidated(ctx context.Context, k ItemKey, now, nextDue time.Time) error          // 304: no payload write
    RecordFailure(ctx context.Context, k ItemKey, f Failure) error                     // keeps payload and fetched_at
    AppendCommits(ctx context.Context, repo RepoRef, cs []CommitRow, keep int) error   // insert-or-ignore + trim
    SaveTask(ctx context.Context, name string, s TaskState) error
    LoadTasks(ctx context.Context) (map[string]TaskState, error)
    Park(ctx context.Context, key string, reason string, retryAt time.Time) error
    Unpark(ctx context.Context, key string) error
    LoadParked(ctx context.Context) ([]Parked, error)

    // ops
    Check(ctx context.Context, full bool) error        // quick_check / integrity_check
    Backup(ctx context.Context, path string) error     // VACUUM INTO tmp + verify + rename
    Health() Health                                    // ok | degraded(reason) for /status
    Close(ctx context.Context) error                   // optimize, checkpoint(TRUNCATE), close
}
```

Multi-replica is not precluded: a Postgres implementation of the same interface is possible; the snapshot would become a local mirror and the engine would need leader election (explicitly out of scope, Q-10).

---

## 5. Part C - Sync engine

### 5.1 Build vs adopt

| Candidate | Current facts | Fit to single-process + SQLite | Verdict |
|---|---|---|---|
| **River** | v0.49.0 (2026-10-05), MPL-2.0, Postgres or SQLite; SQLite support since 0.23.0 and "should still be considered experimental"; `riversqlite` is "in early testing" with "minimal real world use"; SQLite has no LISTEN/NOTIFY so it polls an outbox table; completion and `InsertMany` run one row at a time; SQLite needs one connection [S25-S27]. Unique jobs: default blocking states are available, completed, pending, running, retryable, scheduled; a custom list **must** include pending, scheduled, available and running; a duplicate insert is skipped and returns the existing job; uniqueness is by insert only [S28]. Retry default `attempts^4 +-10%` seconds, 25 attempts [S29] | Strong feature set, but (a) pre-1.0 SQLite driver, (b) **an event arriving while the job is running would be skipped as a duplicate** (running is mandatory in the unique states), which loses the "refresh again because something changed after this run read upstream" semantic we need, (c) a durable job log on top of a store whose truth is already per-item state duplicates state, (d) per-upstream pacing, rate-limit pause and priorities would still be ours | **Do not adopt for v1**; revisit if Q-10 turns multi-replica (Postgres) |
| asynq | v0.26.0 (2026-02-03), MIT, **requires Redis 4.0+** [S30] | external service | Reject |
| machinery | v2.0.16 (2025-08-22); brokers AMQP/Redis/SQS/Pub/Sub, MPL-2.0 [S31] | external broker | Reject |
| gocraft/work | v0.5.1, **2018-05-22**, Redis | dead | Reject |
| Watermill | v1.5.3 (2026-08-25), MIT; messaging library; SQLite Pub/Sub is "Beta" [S32] | pub/sub, no keyed coalescing, no pacing | Reject |
| goqite | v0.4.0 (2025-11-20), MIT; SQLite/Postgres queue with visibility timeout, delay, max receive; no keyed dedupe, priorities only for Postgres [S33] | closest "SQLite queue", still no coalescing/pacing | Reject (useful design reference) |
| pond v2 / ants v2 | pond v2.7.2 (2026-10-03): bounded queue, non-blocking submit, task groups, no keyed dedupe, no delayed tasks [S35]; ants v2.12.1 (2026-05-25) | worker pools only; our dispatcher already owns concurrency per upstream | Reject (no gap filled) |
| gocron v2 | v2.22.0 (2026-07-09), MIT; `WithSingletonMode`, `DurationRandomJob` (jitter), `WithClock(clockwork)`, lifecycle events, distributed lockers [S34] | fine, but brings a second time-faking mechanism (clockwork) next to `synctest` and features we do not use | Acceptable alternative; not needed |
| robfig/cron v3 | v3.0.1, **2020-01-04** | stale | Reject |
| otter v2 | v2.3.0 (2025-12-22), Apache-2.0, loaders/refresh/expiry/stats [S36] | not needed: bounded working set fits in the snapshot | Not used (D-02 stays "only if profiling justifies") |
| `x/sync/singleflight` | v0.24.0; `Do`, `DoChan`, `Forget`, no `context` parameter [S52] | the keyed queue already is a single-flight for jobs | **Adopt narrowly** (5.9) |
| `x/sync/errgroup`, `semaphore`, `time/rate`, `cenkalti/backoff` v5, `failsafe-go` | exist; backoff v5.0.3 (2025-07-23) | per-upstream pacing and attempts/jitter-with-floor are 30 lines in the engine; the breaker belongs to the github adapter (D-04) | Not needed in the engine |

**Decision: build** `internal/syncengine` (name: section 9). Scope is deliberately small (about 550 lines of implementation for queue + scheduler, compile-checked and tested [M4]); everything GitHub-specific stays in the adapter (D-03/D-04).

### 5.2 Core principle: level-triggered planner, edge-triggered accelerators

* **State of record = per-item rows** (`fetched_at`, `next_due_at`, `attempts`, `status`, `etag`) in memory and in SQLite.
* A **Planner** tick (every 10-30 s, jittered) scans the in-memory item states (about 5,000 items, microseconds) and `Enqueue`s every item whose `next_due_at <= now`. `next_due_at` is set when an item completes: `now + TTL(kind) * (1 +- 10%)`.
* **Webhooks, `release`/`create` events and "sync now" are accelerators**: they `Enqueue` a key earlier. If one is lost (restart, crash, queue loss) the planner repairs it within the TTL. GitHub does **not** redeliver failed deliveries automatically [S45], so downtime is repaired by the planner (optionally also by listing and redelivering via the deliveries API, which is a documented endpoint family [S45]; payload details unverified).
* Consequence: **no persisted job log and no persisted webhook intents.** On restart, the queue is simply re-derived (5.8).

### 5.3 Job model and keyed coalescing (compile-checked: `assets/engine`)

```go
package syncengine

type Key string        // "<kind>/<org>/<repo>"  e.g. "commits/acme/web", "readme/acme/web", "catalogue/acme"
type Upstream string   // "github", "jenkins", "snyk"
type Priority uint8
const (PrioInteractive Priority = iota; PrioWebhook; PrioScheduled; PrioBackfill)

type Job struct { Key Key; Kind string; Upstream Upstream; Prio Priority }
type Handler func(ctx context.Context, j Job) error      // idempotent, level-triggered: "bring this item up to date"

// Typed outcomes, classified with errors.As (Go 1.26 also offers errors.AsType [S49]).
type RateLimitedError struct{ ResetAt time.Time }
type PermanentError   struct{ Err error }               // anything else non-nil is transient

type UpstreamConfig struct{ MaxConcurrent int; Pace time.Duration }
type Backoff struct{ Base, Cap time.Duration; MaxAttempts int }

type Engine struct { /* byKey, per-upstream ready+delayed heaps, parked, wake chan; Grace; OnParked; OnExhausted */ }
func New(b Backoff, seed uint64) *Engine
func (e *Engine) AddUpstream(name Upstream, cfg UpstreamConfig)
func (e *Engine) Register(kind string, h Handler)
func (e *Engine) Enqueue(j Job, notBefore time.Time, force bool) error   // never blocks; O(log n); force unparks
func (e *Engine) Run(ctx context.Context)                                // returns after ctx done AND in-flight drained
func (e *Engine) Inspect() (Stats, pending, parked int)                  // for /status and metrics
```

**State x event table** (the heart of keyed coalescing; the cells are exercised by the tests listed in 5.14, [M4]; "permanent while dirty" holds by construction and is not separately tested):

| Key state | `Enqueue` arrives | Handler outcome |
|---|---|---|
| absent | create `delayed` or `ready` by `notBefore` | success: delete key |
| queued/delayed | **coalesce**: promote `runAt` to the earlier time, raise priority, never demote; counted `coalesced` | transient failure: `attempts++`, `runAt = now + backoff`, back to delayed |
| running | set `dirty` (counted `marked_dirty`), raise priority | success + `dirty`: **exactly one** follow-up run now; success: delete |
| parked (permanent failure) | ignored unless `force`; planner/scheduler never resurrect it | rate limited: pause the whole upstream until reset, **do not count an attempt**, retry at resume |
| any | | permanent or panic: park, call `OnParked`, drop `dirty` |

Why `dirty` is mandatory: a refresh that started before the event may already have read the upstream before the event happened; skipping the event (River's duplicate rule [S28]) loses it, whereas queuing N follow-ups wastes upstream budget. One follow-up is the minimum correct answer. A burst of 10 enqueues while queued yields 1 run; 10 while running yields 1 run plus exactly 1 follow-up (tested).

### 5.4 Dispatch: per-upstream queues, concurrency, pace, priority

* One **ready heap and one delayed heap per upstream**, so a paused or saturated upstream never head-of-line-blocks another (a single shared worker pool serving every upstream lets one slow host starve everything, GR-22).
* Ready order: `(priority, enqueue sequence)`; priorities `Interactive` (a user is waiting: `Enqueue` from a handler that found no data), `Webhook`, `Scheduled`, `Backfill`. Backfill phase order is simply enqueue order (catalogue, then commits for all repos, then READMEs, then the rest).
* Per upstream: `MaxConcurrent` and `Pace` (minimum spacing between starts). Proposed GitHub defaults: `MaxConcurrent 4`, `Pace 100 ms` (about 10 requests/s). GitHub documents a 100 concurrent-request ceiling and **recommends making requests serially**; it also lists 900 REST points per minute to one endpoint, 90 s CPU per 60 s, and that there is no way to check secondary-limit status [S42,S43]. Hence: start at 4, make both knobs configuration, and (phase 2) let the engine **halve `MaxConcurrent` on a secondary-limit response and restore it slowly** (AIMD). Whether "one endpoint" means one path template or one URL is unverified (section 13).
* The dispatcher is one goroutine; it sleeps until the earliest of: next delayed `runAt`, upstream `pausedTill`, next pace slot, or a wake-up. No polling loops; under `synctest` the whole timeline is virtual.

### 5.5 Failure classification, retries, rate limits (GR-23, GR-24)

Classification belongs to the **github adapter**, which returns typed errors; the engine only understands three classes. Mapping (from GitHub's documented behaviour [S42,S43]):

| Upstream signal | Adapter returns | Engine action |
|---|---|---|
| 200 / 304 | `nil` (handler already persisted via `PutItem`/`Revalidated`) | success |
| 404 on a content item (README, release) | `nil` after storing typed state `not_found`/`no_content` | success (it is a state, not an error) |
| 403/429 with `x-ratelimit-remaining: 0` | `RateLimitedError{ResetAt: x-ratelimit-reset}` | pause upstream until `ResetAt` + 0-1 s stagger; no attempt consumed |
| 403/429 with `retry-after` | `RateLimitedError{ResetAt: now + retry-after}` | same |
| 403/429 secondary without headers | `RateLimitedError{now + 60 s}` and exponential growth on repeats ("at least one minute", then exponential, stop after a set number) [S42] | same |
| 401 | adapter refreshes the installation token once; if still 401: `RateLimitedError`-style upstream pause 5 min doubling to 1 h and a critical `/status` entry | pause; do **not** park every key |
| 403 other (blocked repo, SSO), 410, 451, 422, parse error, panic | `PermanentError` | park the key (5.11) |
| 5xx, timeouts, DNS, connection reset, breaker open | transient (breaker-open may return `RateLimitedError` with its half-open time) | retry |

The rate-limit rows follow GitHub's documented behaviour [S42,S43]; the mapping of other statuses (401, other 403, 410, 451, 422) to permanent versus pause is a design choice (inference) to be confirmed against real traffic.

Backoff: **full jitter with a floor.** AWS's analysis found "equal jitter" the weakest of the jittered variants and full/decorrelated jitter best at cutting work and load [S53]; I use `delay = Base + U[0, min(Cap, Base<<attempt) - Base]` so a retry is never near zero and even the first retry is de-synchronised across keys that failed together. Defaults `Base 2 s`, `Cap 15 min`; after `MaxAttempts` (8) consecutive transient failures the engine emits `OnExhausted` (metric/log, `/status` shows the item as failing) and **keeps retrying at the cap**, because an upstream outage ends; only permanent errors park. Verified timeline in tests: retries at 0 s, 1.51 s, 3.30 s, 6.86 s with `Base 1 s` [M4].

### 5.6 Budget awareness (GitHub-specific, lives in the adapter, consumed by the engine through a gate)

* Primary limits [S42]: 5,000 req/h (PAT/user-on-behalf), GitHub App installation minimum 5,000 plus 50 per repository above 20 and 50 per user above 20, **capped at 12,500**, Enterprise Cloud 15,000. A conditional request that returns **304 does not count against the primary limit when properly authorised** [S43].
* **Arithmetic the design must respect (inference):** 1,000 repos on a 5-minute commit cadence is 200 requests/minute = 12,000/hour, more than a 5,000/h budget **unless almost all return 304**. Cold start needs about 3 calls per repo (3,000) with no validators. Therefore: (a) ETag/`If-None-Match` on every conditional-capable call is mandatory (GR-25); (b) the planner supports **adaptive idle backoff** per item (double `next_due_at` after N consecutive 304s up to a cap such as 60 min, reset on 200 or webhook) and a **budget governor**; (c) an installation token (up to 12,500/h) is materially safer than a PAT.
* Gate interface (adapter implements it from `x-ratelimit-remaining/reset/resource` headers [S42]):
  `type Gate interface{ Admit(p Priority) (wait time.Duration) }`; suggested reserves: `Backfill` needs more than 40% remaining, `Scheduled` more than 15%, `Webhook` more than 2%, `Interactive` always. The engine calls `Admit` before starting a job and re-delays by the returned duration without consuming an attempt.

### 5.7 Scheduler (compile-checked: `assets/engine/sched.go`)

```go
type Task struct {
    Name         string
    Every        time.Duration
    Jitter       float64                              // 0.1 = +-10% of Every
    InitialDelay func(rnd *rand.Rand) time.Duration   // derive from persisted last_success so a restart resumes cadence
    Run          func(ctx context.Context) error
    Timeout      time.Duration
}
type TaskStatus struct { LastAttempt, LastSuccess time.Time; LastErr string; Runs, Failures, Skipped, Overruns uint64 }

func NewScheduler(seed uint64) *Scheduler
func (s *Scheduler) Add(t Task)
func (s *Scheduler) Trigger(name string) bool           // false (counted Skipped) if already running
func (s *Scheduler) Status(name string) (TaskStatus, bool)
func (s *Scheduler) Run(ctx context.Context)            // returns when ctx done and all task goroutines returned
```

* **Fixed-delay with jitter**: the next interval starts after completion, so a task can never overlap itself and no tick backlog forms (GR-24); `Trigger` ("sync now") uses a compare-and-swap to **skip if running**; panics are recovered into `LastErr`; last attempt/success/error are recorded **on completion** ("last run" means last completed, never "last queued", GR-41); an `Overruns` counter records `duration > Every`. Verified: 10 runs within +-10% of 60 s, one injected panic survived, `Trigger` on a running task skipped [M4].
* **Which tasks exist:** `catalogue.reconcile` (30 min), `planner.tick` (10-30 s), status providers (5-15 min), housekeeping (`PRAGMA optimize` daily, backup hourly if enabled). Everything per-repo goes through the **planner and queue**, not through 1,000 timers.
* **Cold start spreading:** items with no `fetched_at` are due immediately, released at upstream pace; restart after downtime re-derives overdue items, mostly conditional (304). Per-item `next_due_at` carries jitter from the moment it is written, so cadences do not re-synchronise (no startup burst, GR-25).

### 5.8 Startup sequence, readiness gating, resumability (GR-01, GR-02, GR-41)

Explicit ordered steps in `cmd/scp serve` (each logged with duration; failure of 1-3 aborts):

1. Load/validate config (D-07); build clock-free dependencies.
2. **Open store**: check `sqlite_version() >= 3.51.3`, open `rw`/`ro`, `Migrate`, `quick_check`; on failure quarantine and recreate (4.7). Gate `store` = ok.
3. **Warm load** (if rows exist): catalogue + items + commits to the first snapshot (`Apply`, version 1); restore planner state (`next_due_at`, `attempts`), `task_state`, `parked`. Gate `warm` = loaded, record data age = now - `meta.last_full_sync`.
4. Build upstream clients and the engine; register handlers; start scheduler and planner. First planner tick enqueues overdue items; the **first `catalogue.reconcile` is `PrioWebhook`**.
5. **Open listeners early** (public and admin) so `/livez` answers; pages render an explicit "warming up" state until ready.
6. **Readiness**: `/readyz` = 200 when `store` ok AND `catalogue` (a live reconcile succeeded, **or** a warm catalogue younger than `readiness.warm_max_age`, default 72 h) AND `coverage` (at least `readiness.min_coverage`, default 0.9, of repos have a successful value for each `readiness.core_kinds`, default `commits`), OR `readiness.startup_deadline` (default 10 min cold) has passed with a catalogue present, in which case it is ready but `/status` says `degraded`. Cold start without a catalogue never becomes ready.
7. **Readiness never regresses** after first success except for shutdown: with a single replica, flipping `/readyz` on an upstream outage would take the portal offline although it can serve last-good data (readiness removes the Pod from Service endpoints [S54]); upstream health belongs in `/status` and in the UI's staleness indicators (GR-40, GR-41).
8. Probes: `/livez` is process-local (engine heartbeat goroutine alive; **no upstream dependency**) because liveness must signal only unrecoverable failure [S54]; a **startupProbe on `/readyz`** with `failureThreshold x periodSeconds` above the cold start (about 3,000 calls at 10/s is about 5 min, so set 10-15 min) because the kubelet holds liveness/readiness until the startup probe passes [S54].

**Resumability:** persisted `next_due_at/attempts/status/etag/fetched_at`, `task_state.last_success`, and `parked` fully determine what to do next; nothing else is needed. In-flight work at the moment of a crash never updated its row, so it is simply overdue again. Webhooks missed during downtime are repaired by the planner within the TTL (and mostly cost 304s).

### 5.9 Singleflight: where it belongs

`singleflight.Group` has no `context` parameter and shares one result among callers [S52], so it is the wrong tool for jobs (the keyed queue already collapses duplicates and runs under the engine's context). Use it **only inside adapters** for idempotent shared reads that different jobs or requests may trigger concurrently: minting/refreshing a GitHub App installation token, and the org repository listing used by both reconcile and a "repository created" webhook. If an HTTP handler needs data now, it calls `Enqueue(..., PrioInteractive)` and renders "loading", never a direct upstream call (stays off the request path).

### 5.10 Webhook bursts and backpressure (GR-21)

* Path: size-limited read, HMAC-SHA256 on the raw body, `X-GitHub-Delivery` de-duplication (in-memory set with 15-minute TTL; **not persisted**; redeliveries reuse the id [S44]), then `Enqueue`, then **202**. GitHub requires a 2xx within 10 seconds and recommends acknowledging first and processing asynchronously [S44]; our path is microseconds.
* **Bounded by construction:** the key space is repos x kinds (about 5,000 at 1,000 repos); 10,000 pushes to one repo in a minute are 1 running + 1 dirty follow-up. Events for repositories not in the catalogue are ignored with a counter, except `repository` events, which enqueue the single `catalogue/<org>` key (coalesced).
* Safety valve (design; **not in the prototype**, as are `Gate`, AIMD and the planner): `Engine.Enqueue` returns `ErrQueueFull` only if `len(byKey) > MaxKeys` (configuration, default 50,000); the webhook handler maps that to **503 + `Retry-After`** (4xx caller errors stay 4xx, GR-16, GR-21). Because the key space cannot exceed the catalogue, this is a bug detector, not a normal path.
* No persistence of intents: a crash between 202 and processing is repaired by the planner within the TTL; this is cheaper and simpler than a durable outbox and matches "webhooks are accelerators" (Q-15, 5.2).

### 5.11 Dead letters and permanent failures (GR-24)

* Permanent = typed `PermanentError` (including recovered panics, "no handler for kind", 403-other, 410/451, parse failures bound to a content SHA). The key is **parked** (`OnParked` persists to `parked` with `retry_at`, default now + 24 h, and logs once at ERROR with the key and cause), counted, listed in `/status`.
* Parked keys are **not** resurrected by the planner or scheduler; they are released by: `Enqueue(..., force=true)` (a webhook that proves the world changed, `repository` events, the admin "retry" action), a **content change** (new ETag/SHA observed by the next reconcile), or the `retry_at` probe (one attempt per 24 h).
* Repository disappearance is **not** a dead letter: reconcile removes it, the cascade deletes its rows, parked entries for its keys are dropped.
* A panic is contained to the job (recover in the worker), counted separately (`Panics`) and parked, so one poison item cannot kill a worker or loop (GR-24).

### 5.12 Graceful shutdown (GR-03, GR-31)

Order on SIGTERM (`signal.NotifyContext`; since Go 1.26 the context cause names the signal [S49]):

1. `readiness.SetShuttingDown()` -> `/readyz` 503; optional `shutdown.delay` (default 0 s, configurable up to a few seconds for endpoint propagation).
2. `http.Server.Shutdown(10 s)` for both listeners; SSE handlers exit on the base-context cancellation; webhooks arriving until now were accepted and enqueued.
3. Cancel the **engine/scheduler run context**: dispatch stops immediately (nothing new starts), in-flight handlers keep a context derived with `context.WithoutCancel` for **`Engine.Grace`** (default 15 s), then are cancelled; `Run` returns after they return. Tested: a 5 s job finishes inside a 10 s grace without seeing cancellation, a blocked job is cancelled exactly at the grace boundary, a late `Enqueue` does not start [M4].
4. `Store.Close`: `PRAGMA optimize`, `wal_checkpoint(TRUNCATE)`, close handles. In-flight work cut short left its row untouched; it is overdue after restart.
5. Exit 0 within `terminationGracePeriodSeconds` (set it above 10 + 15 + a margin; probe-level details of termination in the Kubernetes docs were not fully read, section 13).

### 5.13 Observability (D-08, GR-41)

Gauges: `scp_sync_pending{upstream}`, `scp_sync_running{upstream}`, `scp_sync_parked`, `scp_upstream_paused_seconds{upstream}`, `scp_item_age_seconds{kind}` (max/p95 from the snapshot), `scp_snapshot_version`, `scp_snapshot_age_seconds`, `scp_store_state`. Counters: `scp_sync_jobs_total{kind,outcome=success|coalesced|marked_dirty|retried|ratelimited|parked|panic|exhausted}`, `scp_store_errors_total{op}`, `scp_store_rebuilds_total{reason}`, `scp_webhook_total{result}`, `scp_snapshot_publish_total{result=ok|invalid}`, `scp_sse_clients`. Histograms: job duration by kind, publish duration. `/status` (admin) lists per-task `TaskStatus`, parked keys, upstream pause state, readiness gates, store health, snapshot version/age. Health is derived from these outcomes, never from "a thread exists".

### 5.14 Deterministic testing

**Primary mechanism: `testing/synctest`.** Facts [S46,S47,S48,S50]: GA in Go 1.25 (`Test`, `Wait`); `Sleep` added in Go 1.27; each bubble has its own clock starting at 2000-01-01 00:00 UTC; time advances only when every goroutine in the bubble is **durably blocked** (channel ops on bubble-created channels, `select` on them, `sync.Cond.Wait`, `WaitGroup.Wait` for a group `Add`ed in the bubble, `time.Sleep`); **mutexes, blocking I/O and syscalls are not durably blocking**; `Test` waits for all bubble goroutines to exit and panics on deadlock; `T.Run/Parallel` not allowed inside; timers firing at the same instant run in randomised order; Go 1.27 adds `httptest.NewTestServer` with an in-memory network suitable for bubbles.

What this buys: the engine needs **no `Clock` interface**; production code calls `time.Now`, `time.NewTimer`, `time.Sleep` directly and tests run it in a bubble. (Hence section 9 proposes dropping `internal/platform/clock`.) Randomness is injected as a seed (`New(backoff, seed)`), so jitter is reproducible.

Test inventory implemented in the prototype (all pass, `-race -count=30`) [M4]:

| Test | Asserts |
|---|---|
| Coalesce queued and dirty-while-running | 10 enqueues while queued = 1 run; 10 while running = 1 follow-up; pending 0 afterwards |
| Per-upstream concurrency and pace | peak concurrency = limit; start offsets `0, 100, 200 ms, 1 s, 1.1 s ...` |
| Transient retry/backoff | each delay within `[Base, Base<<n]`, success on the 4th attempt |
| Rate-limit pause | with one in-flight limited job, nothing starts before `ResetAt`; all 4 keys run after; no attempts consumed |
| Permanent failure parked | scheduler enqueue suppressed; `force` re-runs |
| Panic contained | counted, parked, no crash |
| Priority order | interactive/webhook before scheduled before backfill |
| Shutdown grace and drain | see 5.12 |
| Scheduler | +-10% jitter, panic survived, `Trigger` skipped while running |

Rules for the real suite:

1. **Engine and planner tests: in a bubble, with an in-memory fake `Store` and fake handlers/upstream** (no mutex-protected shared state across the bubble boundary without channels, no real sockets: use `net.Pipe`/`httptest.NewTestServer`).
2. **SQLite tests: outside bubbles** (conformance suite over a temp dir, both drivers). Real SQLite *can* run inside a bubble if called synchronously and closed before the bubble ends [M3] (fake time does not advance during I/O), but SQLite busy-handler sleeps use real time (inference), so keep lock-contention tests out of bubbles. Never mix SQLite's clock with the app's (4.4).
3. **Model-based fuzz** of the queue (proposed, not implemented): random `Enqueue/complete/advance-time` sequences against a 30-line reference model; invariants: at most one running per key; every `Enqueue` is followed by a run that **starts after it**; no key is lost unless parked.
4. `goleak` (v1.3.0, last release 2023-10-24 [S2]) remains useful for non-bubble tests; inside a bubble, `Test` itself fails on leaked blocked goroutines [S46].
5. Package-level `sync.WaitGroup` variables cannot be bubble-associated [S46]; keep waitgroups as fields.
6. Gotcha: with a single-connection pool, `database/sql` background goroutines are created inside the bubble if `sql.Open` runs there; always `defer db.Close()`.

---

## 6. Traceability to the design guardrails

| Guardrail (`02-design-guardrails.md`) | Where addressed |
|---|---|
| GR-28 immutable snapshot with write-time indexes | immutable shared entries, no serialisation on read (3.2, M1); projectors build latest-N and group indexes at publish time, no scans on hot paths (3.2) |
| GR-07 every entity has its own identity | separate `repo`, `item`, `commit_log`; membership as a derived index |
| GR-26 freshness is data | per-item `fetched_at/next_due_at`, planner, TTL per kind (5.2) |
| GR-23 typed upstream failures, rate limits honoured | typed classification table (a rate limit is never a 404), rate-limit pause (5.5) |
| GR-24 the job engine never loses work silently | coalescing queue, delayed full-jitter retries, parking with `OnParked` (5.3, 5.5, 5.11); fixed-delay scheduler with recover and completion-time status (5.7) |
| GR-16, GR-21 truthful status codes; webhook events become coalesced jobs | 202/4xx/503 mapping, bounded key space (5.10) |
| GR-01, GR-02 explicit startup; readiness gated on data | ordered startup and gated readiness (5.8) |
| GR-41 health derives from outcomes | metrics from outcomes, `/status` (5.13) |
| GR-29 absence is a typed state | typed item states, never a cached null (4.4) |
| GR-25, GR-31, GR-40, GR-03 budgeted requests, warm restart, visible staleness, owned lifecycles | 4.4, 5.2, 5.5, 5.8, 5.12 |

---

## 7. Open decisions for the design phase

1. Retention K (default 30), README cap (default 1 MiB), whether to keep both raw and sanitised README in memory (needed for the `/readme.md` route).
2. `readiness.warm_max_age` (72 h proposed), `min_coverage` (0.9), `core_kinds` (`commits`), cold `startup_deadline` (10 min).
3. GitHub `MaxConcurrent`/`Pace` defaults (4 / 100 ms) after observing secondary-limit behaviour; whether to implement AIMD in v1.
4. Adaptive idle backoff for polling (default fixed cadence per Q-09, adaptive as an option) and the budget gate reserves.
5. `emptyDir` (default) versus PVC for `/data`; backup default (off) and cadence.
6. `synchronous` default (NORMAL) after a Linux measurement.
7. Whether projectors declare their input slots so unaffected projectors are skipped (only if profiling shows a need).

---

## 8. Pseudocode summary of the requested signatures

All of `Job`, `Queue` (the `Engine`), `Scheduler`, and `Snapshot` are in 5.3, 5.7 and 3.2 and are **compile-checked** (`go vet`, tests, `-race`) in `assets/engine` and `assets/readmodel`. `Store` is in 4.8 (pseudocode); its SQL is executed in `assets/sqlite-bench`. A missing piece to write in the design phase: `Planner` (scans item state, applies per-kind TTL and jitter, calls `Enqueue`), `Gate` (budget), and the feature handlers.

```go
// Planner (not prototyped): level-triggered.
type PlanState struct{ NextDue time.Time; Attempts int; Status store.Status }   // in memory, restored by warm load
type KindPolicy struct{ TTL time.Duration; Jitter float64; Upstream syncengine.Upstream; Prio syncengine.Priority }
func (p *Planner) Tick(now time.Time) {            // every 10-30 s, jittered, via Scheduler
    for k, st := range p.states { if !st.NextDue.After(now) { p.eng.Enqueue(jobFor(k), time.Time{}, false) } }
}
func (p *Planner) OnResult(k store.ItemKey, res Result) { /* sets NextDue = now + TTL*(1±jitter) (idle backoff on 304 streaks) and persists via Store */ }
```

---

## 9. Deviations from 04 (proposals, each with evidence)

1. **Package name `internal/sync` -> `internal/syncengine`.** `sync` collides with the standard library package imported in nearly every concurrent file (every file would alias one of them) (inference). Responsibilities in D-03 unchanged.
2. **Split `internal/store` into `internal/readmodel` (snapshot kernel, no SQL) and `internal/store` (SQLite `Store`, migrations).** They change for different reasons (SRP) and the read model must be importable by every feature API without pulling in a database driver (principle 2, section 3.1).
3. **Drop `internal/platform/clock`** unless a concrete non-bubble need appears: `testing/synctest` is GA [S47,S50], so the engine and handlers can use `time` directly and tests virtualise it. Randomness still takes an injected seed.
4. **Clarification, not deviation, of GR-02:** a warm catalogue younger than `warm_max_age` counts as a "complete catalogue" for readiness, so a restart does not blank the portal for minutes; staleness stays visible (GR-40).
5. **Minimum Go version is 1.26** (not 1.25): modernc v1.60.x, ncruces v0.35.x, goose v3.29, and `x/sync` v0.24.0 all declare `go 1.26.0` [S2]. Go 1.25 is out of support [S1].

---

## 10. Appendix: files

* `05-research/05-store-sync-engine.assets/README.md` (how to run)
* `.../engine/{engine.go,sched.go,engine_test.go}` keyed coalescing queue, scheduler, 10 synctest tests
* `.../readmodel/{readmodel.go,readmodel_test.go}` typed-slot snapshot kernel
* `.../snapshot-bench/snap_test.go` M1
* `.../sqlite-bench/{bench_test.go,bolt_test.go,mem_test.go,ddl_test.go,migrate_test.go,ddl/0001_init.sql,schema.sql,cmd/*}` M2, M5-M7
* `.../sqlite-in-synctest/s_test.go` M3

## 11. Corrections to earlier assumptions (outdated or wrong, per the web on 2026-10-13)

| Topic | Assumption | Correction (source) |
|---|---|---|
| Toolchain | "Go 1.25 or newer ... (verify)" | Supported releases are Go 1.27 (1.27.2, 2026-10-08) and 1.26 (1.26.9); **1.25 is unsupported** since 2026-08-19 [S1]. The recommended drivers need Go 1.26 [S2]. |
| JSON | "`encoding/json/v2` is still experimental ... do not depend on it" | Go 1.27 ships `encoding/json/v2` and `jsontext` and **`encoding/json` is now backed by the v2 implementation**, v1 API still supported, unmarshal "significantly faster" [S48]. Staying on `encoding/json` remains right; the "experimental" premise is outdated. |
| `GOMAXPROCS` | "container-aware from Go 1.25 (verify; earlier versions need `automaxprocs`)" | Verified: Go 1.25 considers the cgroup CPU limit and updates it dynamically; `automaxprocs` is unnecessary [S50]. |
| SQLite driver | "`ncruces/go-sqlite3` is a wazero-based alternative" | Since 2026-03 (commit "Switch from wazero to wasm2go", [S58]) it wraps a Wasm build **translated to Go by wasm2go**; the README lists only Go and `x/sys` as required dependencies [S7,S10]. |
| SQLite driver size | "driver adds binary size (several MB)" | Measured: modernc **+4.65 MB**, ncruces **+6.05 MB**, bbolt +0.18, Pebble +14.95, Badger +8.47 [M5]. |
| SQLite driver speed | "slower than C SQLite (irrelevant here)" | Quantified: modernc reports 1.3-2.0x CPU time on CPU-bound workloads [S3]; irrelevant indeed (warm load 46 ms [M2]). |
| Conditional requests | "conditional requests that return 304 are documented not to consume the primary limit, verify at design time" | Verified, with a condition: only "if a 304 response is returned **and the request was made while correctly authorized** with an `Authorization` header" [S43]. |
| Rate budget, 04 Q-09 | "5,000 req/h budget" | Correct for PAT/user-on-behalf; a GitHub App installation gets **5,000 + 50 per repo above 20 + 50 per user above 20, capped at 12,500**; Enterprise Cloud 15,000 [S42]. The budget depends on the credential chosen in Q-13. |
| Scheduling | "`time.Ticker` loops with jitter ... `robfig/cron/v3` only if calendar schedules are needed" | robfig/cron's latest release is v3.0.1 from **2020-01-04** [S2]; if a cron library is ever needed, gocron v2 (v2.22.0, 2026-07-09) is the maintained one [S34]. |
| Leak checks | "`goleak`" as the leak check | Still valid outside bubbles; inside `synctest` bubbles `Test` fails on leaked blocked goroutines [S46]; goleak's last release is 2023-10-24 [S2]. |
| KV alternatives | Badger "maintenance cadence has varied with the sponsoring company (verify)" | Releases exist through 2026 (Badger v4.9.6 2026-08-05; Pebble v2.1.7 2026-08-24) [S2]; sponsor/maintainer status could **not** be verified (section 13). Pebble's own README says it is built for CockroachDB and warns about use outside it [S23]. |
| SQLite WAL bug | n/a | SQLite 3.7.0-3.51.2 can **corrupt WAL databases** (two connections writing/checkpointing simultaneously); fixed in 3.51.3/3.53.0 [S12,S14]; guard at startup (4.1). |

---

## 12. Risks

| # | Risk | Mitigation |
|---|---|---|
| R1 | Pure-Go SQLite drivers are young or fragile: modernc depends on a "fragile" `libc` pin [S3]; ncruces is pre-1.0 on a new wasm2go base and shipped a Windows WAL corruption fix in 2026-08 [S8,S10] | Store conformance suite on both drivers in CI; pin and group-update modernc modules; version guard; quarantine-and-rebuild makes corruption a cold start, not an outage |
| R2 | Unsupported volume (NFS/EFS) or stray file descriptor on the database file corrupts or unlocks it [S12,S16] | Document volume types; lint/test that nothing else opens the file; backups only via `VACUUM INTO` |
| R3 | GitHub budget arithmetic: 12,000 conditional requests/h at 5-min commits; cold start about 3,000 calls; 304 is free only when authorised and ETags are stable (stability of ETags on list endpoints not verified) | adaptive idle backoff, budget gate, installation token, measure real 304 ratio before fixing cadences |
| R4 | Secondary rate limits: docs recommend serial requests and 900 points/min to one endpoint [S42,S43]; our default concurrency is 4 | start at 4/100 ms, configurable, treat secondary limits as upstream pause, optional AIMD |
| R5 | Readiness semantics on a single replica: gating too strictly keeps the portal down on a bad day; never regressing hides real problems | warm-ready with visible staleness, deadline-based degraded-ready, `/status` and UI indicators carry the truth |
| R6 | Webhook loss during downtime (no automatic redelivery [S45]) leaves data stale up to its TTL after restart | level-triggered planner; optional redelivery job; startup catch-up is mostly 304s |
| R7 | A custom engine can hide concurrency bugs | 10 deterministic tests with `-race`, proposed model-based fuzz, small surface (550 lines); River is the fallback if the product ever goes multi-replica on Postgres |
| R8 | `synctest` limits: mutex/I-O are not durably blocking, so tests that block on a mutex or a real socket do not advance time or hang [S46,S47] | channel-based fakes, in-memory network (`httptest.NewTestServer`, Go 1.27 [S48]), SQLite tests outside bubbles |
| R9 | Memory: large READMEs and pinned old snapshots (SSE handlers holding a snapshot) | cap, `TooLarge` state, hold versions not snapshots, `GOMEMLIMIT` |
| R10 | My measurements ran on macOS arm64, not a Linux container; fsync cost and drivers' Linux paths differ | re-run `assets/sqlite-bench` in Linux CI before fixing `synchronous` and defaults |
| R11 | Disk full or read-only filesystem | `degraded` store state, serving continues, explicit metric; requires a writable data volume |
| R12 | SSE through buffering proxies and the HTTP/1.1 six-connection limit [S56] | one connection per tab, polling fallback, 15 s keep-alive [S55] |
| R13 | Config-derived DSN is executed as SQL text by modernc [S3] | `net/url`-escaped file URI, strict pragmas, no user input in DSN |

---

## 13. Unverified (could not be verified from a source, or only partially)

1. **ncruces per-connection memory:** the README says memory is higher [S7]; my micro-measurement (5 connections) was inconclusive because the two drivers ran in one process, so no number is claimed.
2. **ncruces dependency statement:** its README lists only Go and `x/sys` as required dependencies [S7], yet my `go.mod` resolved `github.com/ncruces/go-sqlite3-wasm/v6`, `go-strftime` and `julianday` (all by the same author) as indirect modules; I did not audit them.
3. **pkg.go.dev "not the latest version of its module" banners** appeared on modernc, ncruces, otter, gocron and x/sync pages but contradict `proxy.golang.org/@latest` and `@v/list`; I relied on the proxy. Cause not determined.
4. **bbolt v1.5.0 date:** proxy tag time 2026-06-03 versus the GitHub releases page 21 Jun 2026 [S2,S22]; bbolt's changelog contents were not available.
5. **Badger and Litestream maintenance/sponsor status:** no primary source found (searches returned nothing current); Litestream is not recommended anyway.
6. **GitHub secondary limit semantics:** whether "900 points per minute to a single REST endpoint" is per path template or per URL; what the undisclosed per-endpoint costs are [S42].
7. **ETag stability** for GitHub list endpoints (commits, repos) across identical requests, and the real 304 ratio of this org's repositories.
8. **Webhook details not in the fetched docs:** automatic-redelivery window (none stated), redelivery time limit, maximum payload size [S44,S45].
9. **`http.ResponseController.SetWriteDeadline` vs `Server.WriteTimeout`:** only the method index was fetched [S57]; the per-request override semantics are from Go 1.20 knowledge, not re-verified.
10. **River internals beyond the fetched docs:** queue/worker limits per queue, leader election on SQLite, snooze semantics (pages were summarised, not read in full) [S26,S29].
11. **modernc API names/signatures** for `StrictPragmas`, `OFDLocking`, `RegisterConnectionHook` are quoted from the doc summaries [S3,S5]; I did not compile against `StrictPragmas`.
12. **macOS fsync semantics** (why bbolt's per-commit fsync measured 8.9 ms and SQLite NORMAL 0.17 ms) is my explanation, not a measured/verified fact; Linux numbers are missing.
13. **Kubernetes termination/preStop details:** the probes page was truncated before termination guidance [S54].
14. **Go 1.27 areas not covered by the fetched notes** (sync, `net/http` SSE support, `os.Root`, `iter`): I used only what the notes quoted [S48].
15. **otter/ristretto claims from the earlier stack assessment** (admission policy, async `Set`) were not re-verified; otter's last release is 2025-12-22 [S2].
16. **`synchronous=NORMAL` crash behaviour on process kill (not power loss):** my statement that committed data survives a process crash is an inference from the docs' power-failure wording [S12].
17. **GitHub App rate-limit formula edge cases** ("for each repository" above 20; cap 12,500) were read from a summary of [S42]; the credential in use (Q-13) decides which row applies.
18. **Research write-outside note:** temporary benchmark/prototype work was run from the session scratchpad and Go module downloads went to the Go module cache (`~/go/pkg/mod`); the final copies of all code are under `05-store-sync-engine.assets/`.
