# 20-design / 03 - Immutable read model and durable store (`internal/readmodel`, `internal/store`, `internal/store/{sqlite,memory,storetest}`)

Status: design for implementation. Date: 2026-10-14. Unit key `03-readmodel-store`.
Conforms to `04-decisions.md` (D-02, D-11, A-02 and the amendments that touch them), `10-architecture.md` (sections 5, 6.1, 8.4, 8.5, 9.1-9.4, 10.1-10.4, 16, 18; rules G5, G6, G9, G10, G11), ADR-002 (with its 2026-10-14 amendment), ADR-003 and `05-research/05-store-sync-engine.md` (sections 3, 4, 9).

This document is self-sufficient: an engineer who has never seen the conversation that produced it can implement all five packages from it. A throw-away reference prototype is kept next to it in `03-readmodel-store.assets/` (a Go module with the **real kernel packages** `scm`, `freshness`, `job` copied from `01-kernel.assets` since the verification of 2026-10-14, all five packages, the conformance suite and every test named in section 12; about 6,000 lines). **Compile-check statement:** every exported signature and every SQL statement in this document was compiled and exercised in that module (`go vet ./...` clean; `go test -race ./...` green on go1.27.1 darwin/arm64 with modernc.org/sqlite v1.60.1 and, for the conformance suite, ncruces/go-sqlite3 v0.35.6). The first version of the prototype used hand-written kernel stubs and therefore missed defect V-01 (a stub `RepoRef.Key()` that did not fold case); see the Verification log (section 16). Where the document shows a Go block it is copied from compiled code; where the architecture and the prototype differ the difference is listed in section 14, never silently applied.

Conventions: "MUST/MUST NOT" are binding for the implementation; "design choice" marks a decision taken here where the architecture was silent; "Deviation Dn" points at section 14; "Question Qn" (no hyphen) at section 15, while "Q-nn" (hyphen) is an open question of `04-decisions.md`; "Tn" task ids at section 13; "Go review F-nn" is a finding of `30-verification/architecture-review-go.md`; "GR-nn" is a guardrail of `02-design-guardrails.md`.

---

## 1. Purpose, scope, non-goals, packages

### 1.1 Purpose

The portal serves every page from one immutable in-memory value and survives a restart without re-fetching the world. This unit supplies both halves:

- `internal/readmodel` is the **serving state**: a feature-agnostic, immutable `Snapshot` published through an `atomic.Pointer`, built copy-on-write, validated before publication (fail closed to the last good snapshot), read wait-free. Values are shared by pointer and never copied on access, derived indexes are built at publish time and never by scanning, and there is exactly one serving layer (GR-28, GR-04).
- `internal/store` is the **restart state**: a narrow durable contract with a SQLite implementation (CGO-free), an in-memory implementation for unit tests, and one conformance suite that both (and the escape-hatch driver) must pass. It makes a restart warm instead of a full re-fetch (GR-31, GR-26), caches typed absence states and never a `null` (GR-29), and bounds every collection (GR-30).

The snapshot is authoritative for serving; the store is authoritative for restart; the two are allowed to diverge (a degraded store never blocks publishing, architecture 9.3). The store is a **rebuildable cache of GitHub**: losing it costs a cold start (readiness in 10-15 minutes, about 3,000 API calls), never data, which is why "down" migrations do not exist and why corruption is answered by quarantine-and-rebuild.

### 1.2 What is in scope

| Area | In this unit |
|---|---|
| Read model kernel | `Registry`, `Slot[T]`, `Global[T]`, `Existence()`, `Snapshot`, `Builder`, `Put`/`PutGlobal`/`Remove`/`View`, `Projector`, `Validator`, `Publisher` (`Apply`, `Current`, `Status`), built-in existence validator, typed errors (`ErrUnknownRepo` is `job.Gone`, `ErrInvalidSnapshot` is `job.Permanent`), publish metrics and span |
| Store contract | `Store` interface and value types, `RepoKey`/`ItemKey`/`KeyOf`, shared validators, typed errors, optional capabilities `InfoProvider` and `Optimizer` |
| SQLite implementation | DSN builder with three connection roles, pragmas, version guard, own migration runner, schema fingerprint, quarantine-and-rebuild, catalogue/items/plans/commits/tasks/parked/cache SQL, degraded mode and probe, `VACUUM INTO` backup with verification, offline `CheckFile` and `Restore`, store worker (`Run`) and `Close`, metrics and spans |
| In-memory implementation | `store/memory`: same semantics without I/O, plus test hooks (`FailNext`, `SetHealth`, `CacheBytes`) |
| Conformance suite | `store/storetest.Run(t, Harness)`: the behaviour every implementation passes (section 12.1) |
| Warm-start protocol | The exact way `cmd/scp` turns store rows into snapshot version 1 (section 4.6); the decoders belong to the features |

### 1.3 Non-goals

- No snapshot change feed in v1: `Changed()`, `Subscribe()`, `ChangesSince` and the 256-entry ring arrive with SSE in v1.1 (ADR-002 point 1, ADR-009). v1 needs `Current()`, `Version()` and `Rev()` only.
- The store never interprets `Item.Payload`, `CommitRow.Payload` or `Repo.Meta` beyond `json_valid` on `repo.meta`: payload codecs and their inner `schema` field belong to the features (so payload evolution needs no SQL migration).
- No query API on the request path: nothing in an HTTP handler reads the store (D-02, architecture 9.4).
- No multi-replica semantics, no leader election, no network filesystems (WAL), no Redis/Valkey/Postgres/bbolt implementation. They are documented alternatives (ADR-002); `Store` is narrow enough that another implementation is possible (Q-10).
- No job queue persistence (ADR-003, risk W12): the store holds the planner's `next_due_at`/`unchanged_streak` and `parked` keys, not pending work.
- No change-data capture, no replication of the snapshot, no snapshot persistence as a blob (the snapshot is rebuilt from rows).
- No TTL-cache library (for example otter): no profiling evidence (ADR-002 point 5).
- No metrics SDK, no global OTel accessors (G11): the packages take a `metric.Meter` and `trace.Tracer` in their `Deps`.

### 1.4 Packages (exact)

| Package | Directory | Rank | Role |
|---|---|---|---|
| `readmodel` | `internal/readmodel` | 0 | registry, snapshot, builder, publisher |
| `store` | `internal/store` | 1 | contract, value types, keys, validators, typed errors |
| `store/sqlite` | `internal/store/sqlite` | 2 | the production implementation; owns the driver import (`modernc.org/sqlite`), the embedded `migrations/` |
| `store/memory` | `internal/store/memory` | 2 | test implementation |
| `store/storetest` | `internal/store/storetest` | 2 | conformance suite (imported from `_test.go` only) |

Files (the prototype's layout is the intended one):

```
internal/readmodel/    errors.go registry.go snapshot.go builder.go publisher.go
internal/store/        store.go key.go errors.go validate.go
internal/store/memory/ memory.go
internal/store/sqlite/ config.go dsn.go driver.go migrate.go open.go store.go catalogue.go items.go commits.go tasks.go cache.go
                       ops.go worker.go metrics.go migrations/0001_init.sql
internal/store/storetest/ suite.go helpers.go
```

---

## 2. Architecture position

### 2.1 Allowed imports (architecture 5.2, copied; archtest enforces)

| Package | May import (internal) | Third party |
|---|---|---|
| `readmodel` | none | OTel API (`metric`, `trace`, `attribute`; `metric/noop` and `trace/noop` for defaults) |
| `store` | `freshness`, `scm` | none |
| `store/sqlite` | `store`, `freshness` | `modernc.org/sqlite`, `modernc.org/libc` (transitive), OTel API |
| `store/memory` | `store`, `freshness` | none |
| `store/storetest` | `store`, `freshness` | none |

**Test imports (architecture 5.2, last paragraph).** A `_test.go` may import what its package may import plus test-helper packages, nothing else. Therefore: tests of `readmodel` import nothing of ours (the `Gone`/`Permanent` behaviour interfaces are restated locally; `cmd/scp` `TestBehaviourInterfacesMatchRealJob` checks them against the real `job.IsGone`/`job.IsPermanent`); tests of `store/sqlite` import `store`, `freshness`, `storetest` and the third-party `ncruces/go-sqlite3` (test library) but **not** `store/memory` or `scm` (same rank, not in the row); every test that needs both implementations (`TestDifferentialAgainstMemory`, the `ReplaceCatalogue` model test) lives in `cmd/scp` (rank 6), as does every test that needs the real YAML decoder (`TestStoreConfigStrictDecode`). The first prototype violated this in two places (verification V-05).

Consequences the implementation MUST respect: `store/sqlite` does not import `scm` (the only conversion from `scm.RepoRef` is `store.KeyOf`, in `store`); `readmodel` imports nothing of ours, so it cannot name `freshness.Value` (a feature builds `freshness.Value[T]` and passes it as `T`); the behaviour interfaces `job.Gone` and `job.Permanent` are satisfied **structurally** by method sets (`Gone() bool`, `Permanent() bool`) because `readmodel` and `store` may not import `job`. The `ncruces/go-sqlite3` import lives in a `_test.go` file of `store/sqlite` in the prototype; the shipped escape-hatch switch is Q2.

### 2.2 Who imports this unit

| Importer | Imports | Why |
|---|---|---|
| every feature (`catalog`, `commits`, `docs`, `manifests`, `badges`, `pages`, `integrations`, `media`) | `readmodel` (K4), `store` (K4) | slot handles and the snapshot in handlers/services; its own carved-out store interface uses `store` value types |
| `httpapi/authz`, `live`, `ops`, `overview` | `readmodel` | read the snapshot once per request, hold a version number (`live`) |
| `cmd/scp/internal/bridge` | `store`, `readmodel` | `PlanSink` flusher over `SavePlans`, parked/task bridges, status views |
| `cmd/scp` | everything, including `store/sqlite` (`Open`, `Run`, `Close`, `Restore`, `CheckFile`) and `store/memory` (dev/test wiring) | step 2 (open), step 3 (registry and publisher), step 5 (warm load), lifecycle `store` stage; `cmd/scp/assert.go` compile-time assertions |
| tests of features | `store/memory`, `store/storetest` is imported only by the implementations' own tests | hand-written fakes named after behaviour for everything else |

### 2.3 Dependencies this unit takes

- `freshness.State` (the `item.status` column values and `Item.State`), `scm.RepoRef` (the `KeyOf` conversion), nothing else of ours.
- OTel **API** only, through `Deps{Meter, Tracer}` (G11); `slog.Logger` injected (G3). No `time.Now` for domain decisions: times in rows are values passed by callers (`Item.FetchedAt`, `Failure.At`, `now` in `Revalidated`); the store itself reads the process clock only for `meta.created_at/last_open_at`, `parked.since` (the contract has no `since` parameter), the backup-age gauge and quarantine names, none of which feed a domain decision (section 4.3, design choice).

### 2.4 Consumer-defined carve-outs (exact Go signatures; satisfied directly by `*sqlite.Store`)

Each consumer declares the narrow interface it needs in its own package; all use only stdlib and `store` value types, so `store/sqlite` and `store/memory` satisfy them structurally and `cmd/scp/assert.go` proves it (architecture 5.5, 8.7). The block below is the prototype's `cmd/scp/assert_test.go`, compiled against both implementations.

```go
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
type blobCache interface { // package manifests (namespaces manifests.blob, manifests.parse)
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
type planSaver interface { SavePlans(ctx context.Context, rows []store.PlanRow) error }                 // bridge PlanSink flusher
type engineHooks interface {                                                                          // bridge: refresh hooks and Task.OnDone
	Park(ctx context.Context, key, reason string, retryAt time.Time) error
	Unpark(ctx context.Context, key string) error
	SaveTask(ctx context.Context, name string, s store.TaskState) error
}
type opsView interface{ Health() store.Health }                                                       // ops (bridged view types)
```

Optional capabilities asserted on the concrete type by `cmd/scp` only (not part of `Store`): `store.InfoProvider` (`Info() store.Info`, for `/status`) and `store.Optimizer` (`Optimize(ctx) error`, the body of the scheduler task `store.optimize`).

### 2.5 The seam to the sync engine (who owns which column)

| Column / table | Writer | Method | Note |
|---|---|---|---|
| `item.status, fetched_at, checked_at, etag, content_sha, payload`; `attempts = 0`; `error = NULL` | the feature's job | `PutItem` | one transaction per completed job (Go review F-06) |
| `item.fetched_at, checked_at`; `attempts = 0`; `error = NULL` | the feature's job | `Revalidated` | a 304: payload never rewritten |
| `item.status` (only when no good value exists), `checked_at`, `attempts + 1`, `error` | the job outcome handler | `RecordFailure` | stale-while-error |
| `item.next_due_at, unchanged_streak` | the **planner only** | `SavePlans` | batched by the bridge, one transaction per flush |
| `commit_log` | `commits` | `AppendCommits` | insert-or-ignore plus trim in the same transaction |
| `parked` | engine hooks via the bridge | `Park`/`Unpark` | `since` survives re-parking |
| `task_state` | scheduler `Task.OnDone`, webhook redelivery watermark | `SaveTask` | `last_success` doubles as a watermark |
| `repo` | `catalog` reconcile | `ReplaceCatalogue` | the only way a repository appears or disappears |
| `cache` | `manifests` | `PutCache`/`GetCache` | namespaces registered at `Open` |

**Plan-kind rule (verification V-02, cross-unit).** `PlanRow.Key` and `LoadPlans` speak in **item kinds** (`ItemKey.Kind`), because the planner columns live on the `item` row (architecture 9.2). The engine and planner speak in **job kinds** (`job.Key` kind). Architecture 8.5 gives item kinds that are not job kinds (`catalog.stats`, `docs.readme`, `manifests.facts`) and unit 09 (`commits`) actually uses item kind `commits.ring` for job kind `commits.refresh`. `SavePlans` matches `item.kind` exactly and **silently skips** a row that matches no item (rows without an item are due anyway), so an unmapped job kind would never persist its plan and the planner would restart "due" for every repository after each restart. Binding rules: (1) every feature that registers a per-repository job kind exports the **item kind that carries its plan** (a constant, for example `commits.ItemKind`; when job kind and item kind are the same string nothing else is needed); (2) the `PlanSink` bridge in `cmd/scp/internal/bridge` translates job kind to item kind on `SavePlans` and back on `LoadPlans` from the table assembled in `cmd/scp`; (3) `Deps.KnownKinds` is the set of **item kinds** of every compiled-in feature (never the job kinds: pruning by job kinds would delete `commits.ring` at every start); (4) `cmd/scp` has a startup assertion and a wiring test `TestEveryRepoJobKindHasAPlanItemKind` (T-IN-8); (5) the store counts every dropped row in `scp_store_plan_rows_skipped_total{reason=global|no_item}` so a mapping bug shows as a counter that grows after every flush instead of as silent re-fetching. Global job kinds (zero `RepoKey`) are never persisted (Q3, `reason=global`).

---

## 3. Domain model and Go types

### 3.1 `readmodel`

```go
package readmodel

type EntryID string // the portal slug as string; never collides with scm.RepoID (int64)

type SlotOption func(*slotConfig)
func Existence() SlotOption                                  // marks THE slot whose presence means "this repository exists" (catalog.repo)

type Registry struct{ /* slots, globals, existence, frozen */ }
type Slot[T any]   struct{ /* reg, id */ }
type Global[T any] struct{ /* reg, id */ }
func Register[T any](r *Registry, name string, opts ...SlotOption) Slot[T]   // panics: empty/duplicate name, after Freeze, second Existence
func RegisterGlobal[T any](r *Registry, name string) Global[T]               // panics: empty/duplicate name, after Freeze
func (r *Registry) Freeze()                                                  // idempotent; panics when no Existence slot was registered
func (r *Registry) SlotNames() []string                                      // registration order (for /status and tests)
func (r *Registry) GlobalNames() []string
func (s Slot[T]) Name() string
func (g Global[T]) Name() string

type Snapshot struct{ /* reg, version, builtAt, repos map[EntryID]*entry, names []EntryID, globals []any */ }
func (s *Snapshot) Version() uint64
func (s *Snapshot) BuiltAt() time.Time
func (s *Snapshot) Repos() []EntryID                // sorted; SHARED between snapshots while membership is unchanged: read-only
func (s *Snapshot) Has(id EntryID) bool
func (s *Snapshot) Rev(id EntryID) uint64           // snapshot version at which this entry last changed; 0 when absent (the per-repository ETag)
func Get[T any](s *Snapshot, id EntryID, slot Slot[T]) (T, bool)
func GetGlobal[T any](s *Snapshot, g Global[T]) (T, bool)

type Builder struct{ /* single-use copy-on-write view; ONE goroutine */ }
func Put[T any](b *Builder, id EntryID, slot Slot[T], v T) error             // TAKES OWNERSHIP of v
func PutGlobal[T any](b *Builder, g Global[T], v T) error                    // TAKES OWNERSHIP of v (Deviation D1: returns error)
func (b *Builder) Remove(id EntryID)                                         // whole entry; absent id is a no-op
func (b *Builder) View() *Snapshot                                           // for projectors; NOT publishable

type Projector func(b *Builder) error      // derives cross-repository indexes; pure, deterministic
type Validator func(*Snapshot) error       // referential integrity; failure => previous snapshot kept

type Deps struct{ Meter metric.Meter; Tracer trace.Tracer; Log *slog.Logger }   // zero value valid: no-op meter/tracer, discard logger
type PublishStatus struct {
	Version uint64; BuiltAt time.Time
	Published, Rejected, Aborted uint64
	LastRejected error; LastRejectedAt time.Time
}
type Publisher struct{ /* atomic.Pointer[Snapshot]; mu serialises builders */ }
func NewPublisher(reg *Registry, projectors []Projector, validators []Validator, d Deps) *Publisher
func (p *Publisher) Current() *Snapshot                                       // wait-free
func (p *Publisher) Apply(fn func(*Builder) error) (*Snapshot, error)         // build -> project -> validate -> swap
func (p *Publisher) Status() PublishStatus                                    // never blocks behind a running Apply

const MetricPublishTotal, MetricPublishSeconds = "scp_snapshot_publish_total", "scp_snapshot_publish_seconds"
```

Errors (exact):

```go
var (
	ErrUnknownRepo     = errors.New("readmodel: unknown repository")                                   // Put of a non-existence slot for an entry with no existence value
	ErrInvalidSnapshot = errors.New("readmodel: candidate snapshot failed validation")
	ErrNilValue        = errors.New("readmodel: nil slot value")                                       // nil interface value; absence is represented by absence
)
type UnknownRepoError struct{ ID EntryID; Slot string }            // Is(ErrUnknownRepo); Gone() bool == true  (satisfies job.Gone)
type InvalidSnapshotError struct{ Stage string; Index int; Err error } // Stage "projector"|"validator"; Index -1 = built-in validator; Is(ErrInvalidSnapshot); Unwrap; Permanent() bool == true (job.Permanent)
```

Value rules: a slot value is immutable once `Put`; `Put` takes ownership (the producer clones what it keeps using and never touches the value again); readers never mutate what they read (architecture 6.1 rule 1; the `-race` property test with a mutating reader is section 12.3). `T` may be a value or a pointer; `Get` returns `(zero, false)` for an absent entry, an absent value or a type mismatch (which cannot happen with typed handles from one registry). A handle from another registry panics (`checkReg`), a wiring bug found by the first test.

### 3.2 `store`: keys and value types

```go
package store

type RepoKey struct{ Source, Owner, Name string }
func KeyOf(r scm.RepoRef) RepoKey                 // the single conversion from the kernel identity; MUST build from r.KeyParts() (ASCII-folded owner and name), never from the raw fields
func (k RepoKey) String() string                  // "source/owner/name": identical to scm.RepoRef.Key() and to the tail of every per-repository job.Key
func (k RepoKey) IsZero() bool
func (k RepoKey) Validate() error                 // three non-empty segments, each <= 200 bytes, no '/' and no NUL (job keys split on '/'), and no ASCII upper-case: the key is the FOLDED key (UNIQUE is case-sensitive)
type ItemKey struct{ Repo RepoKey; Kind string }  // Kind is the ITEM kind (feature-owned); it equals a job kind only by convention: see the plan-kind rule in 2.5
func ValidKind(kind string) error                 // [a-z0-9_-]+(\.[a-z0-9_-]+)+, 3..64 bytes: "commits.ring", "docs.readme", "manifests.facts"
func (k ItemKey) Validate() error

type Item struct {
	Key                  ItemKey
	State                freshness.State
	FetchedAt, CheckedAt time.Time // UTC, second precision, zero = unset
	Attempts             int       // consecutive failures; store-owned: RecordFailure increments, PutItem/Revalidated reset
	ETag, ContentSHA     string
	Error                string    // stable machine code of the last failed attempt; "" when the last attempt succeeded
	Payload              []byte    // nil on revalidation; feature-owned codec with an inner schema field; never trusted as HTML
}
type PlanRow struct{ Key ItemKey; NextDue time.Time; UnchangedStreak int } // planner-owned
type CommitRow struct{ SHA string; CommittedAt time.Time; Payload []byte }
type TaskState struct{ LastAttempt, LastSuccess time.Time; LastError string }
type Parked struct{ Key string; Since, RetryAt time.Time; Reason string }
type Repo struct{ Key RepoKey; GitHubID int64; Slug string; Meta []byte; SeenAt time.Time }  // Meta: JSON object (description, default branch, topics, visibility, aliases)
type Failure struct{ Code, Message string; At time.Time }                  // Code persisted (<= 64 bytes); Message is for the caller's log
type Health struct{ OK bool; Reason string }
type Info struct {
	Driver, Path string; SchemaVersion int; SQLiteVersion string
	DBBytes, WALBytes int64; CacheBytes map[string]int64
	LastBackup time.Time; LastBackupErr, Rebuilt string; Health Health
}
const MaxPayload = 16 << 20                                // sanity bound on Item.Payload, CommitRow.Payload, Repo.Meta
// (the result-state test is the kernel's freshness.State.HasResult(): ok, not_found, unsupported, too_large, no_content; there is no store copy of it)
func ValidateItem(Item) error; ValidateFailure(ItemKey, Failure) error; ValidateRepo(Repo) error   // ValidateRepo also requires Meta to be a JSON object
func ValidateCatalogue([]Repo) error; ValidateCacheArgs(ns, key string) error
func ValidatePlan(PlanRow) error; ValidateCommit(CommitRow) error                              // shared by every implementation so they reject exactly the same inputs
```

`freshness.State` values written by the store: the five **result states** `ok`, `not_found`, `unsupported`, `too_large`, `no_content` (a typed absence is a result, GR-29, so a repository without a README is `not_found` with a payload-free row, never a cached `null`), plus `error` (only through `RecordFailure` on a row that never had a good value) and `never` (reserved by the schema; the store never writes it, the planner keeps "never fetched" as the absence of a row).

### 3.3 `store`: the contract

```go
type Store interface {
	// warm start (read handle, one read transaction, streaming). A callback MUST NOT call any Store method.
	LoadCatalogue(ctx context.Context) ([]Repo, error)                                  // ORDER BY slug
	LoadItems(ctx context.Context, fn func(Item) error) error                           // ORDER BY repo.id, item.id
	LoadPlans(ctx context.Context, fn func(PlanRow) error) error                        // rows with next_due_at set
	LoadCommits(ctx context.Context, repo RepoKey, limit int) ([]CommitRow, error)      // newest first

	// writes (write handle, each call one BEGIN IMMEDIATE transaction; no method calls another method inside its transaction)
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
```

Contract rules pinned by `storetest` (every implementation):

1. A `LoadItems`/`LoadPlans` callback MUST NOT call any `Store` method (the single writer connection and the read pool are not reentrant by design; the suite proves a callback that calls the store does **not deadlock** the read path, section 12.1).
2. No method calls another `Store` method inside its transaction.
3. A write for a repository with no row returns an error that `errors.Is(err, store.ErrNoRepo)` and satisfies `job.Gone` structurally, so a job that finishes after its repository was removed is dropped, not retried (F-08).
4. Time is stored as UTC unix seconds; sub-second precision is dropped; zero `time.Time` is `NULL`.
5. `Run` blocks until `ctx` is done and returns nil; `Close` is called by lifecycle only after `Run` returned and is idempotent; after `Close` every method returns `ErrClosed`.
6. Input validation is shared code (`store.Validate*`), so SQLite never sees a value the memory store would reject.

### 3.4 Typed errors (exact)

```go
var (
	ErrNoRepo           = errors.New("store: no such repository")                 // Satisfies job.Gone
	ErrNoItem           = errors.New("store: no such item or nothing to revalidate")
	ErrDegraded         = errors.New("store: degraded, writes suspended")
	ErrClosed           = errors.New("store: closed")
	ErrInvalid          = errors.New("store: invalid argument")
	ErrUnknownNamespace = errors.New("store: unknown cache namespace")
	ErrTooLarge         = errors.New("store: value too large")
	ErrCorrupt          = errors.New("store: integrity check failed")
)
type NoRepoError   struct{ Key RepoKey }          // Is(ErrNoRepo); Gone() bool == true
type InvalidError  struct{ Field, Reason string } // Is(ErrInvalid)
type CorruptError  struct{ Messages []string }    // Is(ErrCorrupt); first (up to 5) lines SQLite reported
```

`store/sqlite` adds (exported): `ErrSchemaNewer`, `ErrForeignDatabase`, `ErrVersionGuard`, `ErrBackupIsSource`, and the rebuild reason constants `ReasonCorrupt = "corrupt"`, `ReasonNotADatabase = "not_a_database"`, `ReasonSchemaNewer = "schema_newer"`, `ReasonMigrationFailed = "migration_failed"`, `ReasonSchemaDrift = "schema_drift"` (label values of `scp_store_rebuilds_total`).

### 3.5 `store/sqlite` public surface

```go
func Open(ctx context.Context, cfg Config, d Deps) (*Store, OpenReport, error) // step 2 of the startup sequence; starts no goroutine
func DefaultConfig() Config
func (c Config) Validate(prefix string) []error     // path-accurate, collects every violation
func (c Config) BackupPath() string                 // backup.path or store.path + ".bak"
func CheckFile(ctx context.Context, path string, full bool) error                       // offline `scp store check`; never writes
func Restore(ctx context.Context, dbPath, backupPath string, keep int) error            // offline `scp store restore`
func (s *Store) Info() store.Info; Optimize(ctx) error; FlushTouches(ctx) error         // + the store.Store methods

type Deps struct {
	Log *slog.Logger; Meter metric.Meter; Tracer trace.Tracer; BuildID string
	CacheNamespaces map[string]int64   // every namespace -> byte cap; any other namespace is rejected (ErrUnknownNamespace)
	KnownKinds []string                // when non-empty, Open deletes item rows of other kinds and cache rows of unregistered namespaces
}
type OpenReport struct {
	Created bool; Migrated MigrateResult; SchemaVersion int; SQLiteVersion string
	Rebuilt, QuarantinedAs string; Pruned map[string]int64; Duration time.Duration
}
type MigrateResult struct{ From, To, Applied int }
```

(The prototype's `Deps` also carries two unexported test seams, `migrations` and `drv`; the shipped `Deps` keeps them unexported.)

---

## 4. Behavioural specification

### 4.1 `readmodel`

#### 4.1.1 Registration and freezing (wiring time, `cmd/scp` step 3)

```
reg := &Registry{}
repoSlot  := Register[freshness.Value[scm.Repository]](reg, "catalog.repo", Existence())   // exactly one Existence slot
statsSlot := Register[freshness.Value[scm.RepoStats]](reg, "catalog.stats")
...
membership := RegisterGlobal[scm.Membership](reg, "catalog.membership")
pub := NewPublisher(reg, projectors, validators, Deps{Meter, Tracer, Log})   // Freeze()s reg; publishes the empty version-0 snapshot
```

Misuse panics at the first start, never at runtime: empty or duplicate slot/global name, `Register` after `Freeze`, a second `Existence()`, `Freeze` without an `Existence()` slot, a handle used with a different registry. Slot and global names are the architecture 9.1 names (`catalog.repo`, `commits.view`, `docs.repo`, ...); registration order is the order in `/status`. A `Registry` is not safe for concurrent use before `Freeze` and read-only afterwards.

#### 4.1.2 `Publisher.Apply` (the only mutation path)

```
Apply(fn):
  t0 := now; lock p.mu                                  // serialises builders; fn runs UNDER the lock
  span := tracer.Start("snapshot.apply")
  prev := cur.Load()
  b := newBuilder(prev)                                  // clones the repos map (pointer copy) and the globals slice; entries are shared
  if err := fn(b); err != nil:
        status.Aborted++; count("aborted"); return prev, err            // error returned unchanged (may be ErrUnknownRepo)
  for i, pr := range projectors: if err := pr(b); err != nil: return reject("projector", i, err)
  next := &Snapshot{version: prev.version+1, builtAt: now, repos: b.repos, names: b.sortedNames(), globals: b.globals}
  for id in b.touched: if e := b.repos[id]; e != nil: e.rev = next.version      // entries are still private: stamping is safe
  if err := existenceValidator(next): return reject("validator", -1, err)      // every entry has a value in the existence slot
  for i, v := range validators: if err := v(next); err != nil: return reject("validator", i, err)
  cur.Store(next)                                         // the ONLY publication point
  status.Published++; count("ok"); record scp_snapshot_publish_seconds(since t0)
  return next, nil

reject(stage, i, err): ie := &InvalidSnapshotError{stage, i, err}; status.Rejected++; status.LastRejected = ie; count("invalid")
                       log ERROR "snapshot candidate rejected" {stage, index, err, snapshot_version}; return prev, ie
```

Rules and consequences:

- **`fn` MUST NOT block or do I/O** (architecture 8.4): it runs under the publish mutex, so a slow `fn` stalls every other publisher. A test to write (section 12.2, T-RM-9) runs `Apply` with a deadline-bound `fn` and a concurrent `Apply` and asserts the second is not delayed beyond the first's CPU time. Warm-start decoding (parsing payload bytes) is done **before** `Apply` into plain values; `fn` only calls `Put`.
- On any error the previous snapshot stays current and is returned with the error; versions are gapless and monotonic (a rejected candidate does not consume a version, because `version` is computed from `prev`).
- Projectors and validators run inside the lock on the builder/candidate: pure, deterministic, no I/O, no call back into the `Publisher`.
- `ErrInvalidSnapshot` is `Permanent` (the job is parked and a critical status shown, not retried, because it would rewrite the same row); `ErrUnknownRepo` is `Gone` (the job's key is dropped).
- If cold-start publish stampede (3,000 completions) ever shows in profiles, `Apply` gains an optional coalescing window (publish at most every 100 ms, batching builders) **before** the 10,000-repository exit condition (architecture 8.4). Not built in v1. Measured cost at 1,000 repositories with a one-repository change is 67-73 us, 107 KB, 1,038 allocations (section 12.6), so 3,000 sequential completions cost about 0.2 s of publish time.

#### 4.1.3 `Builder`: copy-on-write with structural sharing

`newBuilder(prev)` clones the `repos` map and the `globals` slice (pointer copies; about 1,000 entries) and tracks three sets: `fresh` (entries created or cloned by this builder, private and mutable until publish), `touched` (to be stamped with the new version) and `gtouched`.

```
Put(b, id, slot, v):
  checkReg; if any(v) == nil: return ErrNilValue
  e := b.repos[id]
  if e == nil:
      if slot != existence slot: return &UnknownRepoError{id, slot.Name()}        // F-08: a late job cannot resurrect a removed repository
      e = &entry{slots: make([]any, len(reg.slots))}; b.repos[id]=e; fresh[id]; members=true; names=nil
  else if id not in fresh: e = &entry{slots: slices.Clone(e.slots)}; b.repos[id]=e; fresh[id]   // clone ONCE per builder per entry
  e.slots[slot.id] = v; touched[id]
Remove(b, id): absent => no-op; delete(b.repos,id); delete(fresh,id); members=true; names=nil; touched[id]
sortedNames(): if !members: return prev.names (shared slice)  else slices.Sorted(keys) cached in the builder
View(): a non-publishable Snapshot over the candidate (version prev+1) for projectors
```

Edge cases (each is covered by a prototype test or by a test listed as to-write in section 12.2): `Put` into an entry created earlier by the same builder does not clone again; two `Put`s to different slots of one entry in one `Apply` produce one new entry with `Rev` equal to the new version; an untouched entry is the **same pointer** as in the previous snapshot (sharing asserted by identity); `Remove` then `Put` of the existence slot in one `Apply` recreates the entry with every other slot empty; `Remove` of the existence entry while another `Put` for a non-existence slot of that id follows in the same `Apply` returns `ErrUnknownRepo` (order matters, as designed); `Put(nil interface)` is `ErrNilValue` and does not mark the entry touched; `PutGlobal` replaces the global (the value is not cloned).

#### 4.1.4 Reads

`Get`/`GetGlobal` are two slice/map lookups plus one type assertion (0.5 ns/op parallel on the prototype, section 12.6). A handler calls `Current()` exactly once per request and passes the `*Snapshot` down; long-lived goroutines (SSE) hold a `uint64` version, never a `*Snapshot` (architecture 6.1 rules 2-3). **`Rev` is valid only for per-repository resources that depend on that entry's slots and nothing else** (verification V-07). `Rev(id)` does not move when a **global** changes (`catalog.membership`, `catalog.aliases`, `commits.latest`, `manifests.graph`, `pages.set`) or when **another** repository's entry changes. A response that reads any global or any second entry (the composite repository resource shows groups from the membership global and dependents from the graph) MUST derive its ETag from `Snapshot.Version()` instead (or from `Version` plus `Rev`); building such an ETag from `Rev` alone would answer `304 Not Modified` with a stale body. This is the contract that `httpapi` (`WeakETagOf(ETagRepo, ...)`, unit 06 section 3.3) and `overview` must respect; the property test `ETagCoversBody` of `apitest` is the executable form, and this unit's test T-RM-11 pins the facts (a global `PutGlobal` leaves every `Rev` unchanged; a one-entry change leaves every other `Rev` unchanged). Staleness is **derived at read time** (`freshness.Info.Derive(now)`), never published by a timer, so no publish happens just because time passed.

#### 4.1.5 Memory and cost

52 MB at 1,000 repositories with 20 KB README + 25 KB HTML + 30 commits + 5 status slots; +0.12 MB per additional live snapshot; derived-index rebuild 0.31 ms (`05-research/05` M1, quoted by architecture 8.4, not re-measured here). Old snapshots are garbage as soon as the last reader returns; no explicit retirement.

### 4.2 Store: opening, migrating, quarantining (`sqlite.Open`)

```
Open(ctx, cfg, d):
  d.defaults(); errs := cfg.Validate("store"); if any: return errors.Join(errs)
  drv.strict()                                       // modernc: sqlite.StrictPragmas(true), process-wide, before any sql.Open
  ms := loadMigrations(embed.FS, "migrations")       // NNNN_name.sql, contiguous from 0001, sorted
  s, rep, err := openOnce(...)
  if err is *rebuildable{reason}:                    // the file is unusable but IS ours (or is garbage)
        log ERROR "database unusable: quarantining and starting cold"; q := quarantine(cfg.Path, cfg.QuarantineKeep)
        rebuilds_total{reason}++ ; s, rep, err = openOnce(...)                       // a FRESH file
        if err != nil: return error "fresh database also failed to open (disk fault?)"   // second failure: cmd/scp exits non-zero
        rep.Rebuilt, rep.QuarantinedAs = reason, q ; s.rebuilt = reason
  return s, rep, err

openOnce:
  1. stat path; rep.Created = not exists; if Created: removeOrphanSidecars(path)      // a stray -wal next to a NEW database would be replayed into it
  2. rw := sql.Open(buildDSN(path, roleRW)); MaxOpenConns=1, MaxIdle=1, ConnMaxLifetime=0, ConnMaxIdleTime=0   // the one writer
  3. Ping. SQLITE_NOTADB (26) => rebuildable{not_a_database}; any other error => fatal (cannot open: permissions, directory missing)
  4. SELECT sqlite_version(); if < 3.51.3 => ErrVersionGuard (fatal; never quarantine for a library problem)
  5. SELECT application_id, user_version, count(*) FROM sqlite_schema in one statement
       NOTADB or CORRUPT => rebuildable{corrupt}
       application_id != 0 && != appID, or application_id == 0 && objects > 0  => ErrForeignDatabase (fatal; the file is NEVER renamed or overwritten)
       user_version > len(migrations) => rebuildable{schema_newer}
  6. if objects > 0: PRAGMA quick_check on rw  (not ok => rebuildable{corrupt})
  7. migrate(...)  (error => rebuildable{migration_failed})
  8. if objects > 0 (a valid current file): schemaFingerprint(rw) must equal expectedFingerprint (migrations applied to a private :memory: db)
       mismatch => rebuildable{schema_drift}          // someone edited the file by hand, or a migration changed after release
  9. open ro  (mode=ro, query_only(1), MaxOpenConns = ReadConns) and bk (mode=ro, no query_only, MaxOpenConns = 1)
 10. initAfterOpen: one write tx: upsert meta(last_open_at, build_id), insert-or-ignore meta(created_at);
       if KnownKinds non-empty: DELETE FROM item WHERE kind NOT IN (...); if namespaces registered: DELETE FROM cache WHERE ns NOT IN (...)
       (counts to scp_store_pruned_total{table}); load cacheBytes per namespace; seq = max(use_seq); PRAGMA optimize=0x10002
 11. register gauges; log INFO "store open"
```

`quarantine(path, keep)`: base name `<path>.quarantine-<UTC 20060102T150405Z>` (suffix `-2`, `-3`, ... if two rebuilds happen in one second); rename **`-shm`, then `-wal`, then the database last** (a crash in between never leaves a WAL next to a database it does not belong to; a missing file is ignored); then keep only the newest `QuarantineKeep` sets (names sort chronologically; `-wal`/`-shm` siblings are part of one set). The quarantine is a diagnostic aid, not a backup: it may contain a corrupt file.

**Which failures quarantine and which are fatal** (design choice, pinned by tests): corrupt, not-a-database, newer schema, failed migration and schema drift are *rebuildable* (the data is a cache); a foreign `application_id`, an old SQLite library, an unreadable path and a failure of the **fresh** file are *fatal* (`Open` returns an error and `cmd/scp` exits non-zero before serving), because renaming someone else's database or looping on a read-only volume is worse than refusing to start.

### 4.3 Store: the write and read helpers

```
write(ctx, op, fn):                          // every mutating method
  gate(op, write=true): closed => ErrClosed; !Health.OK => fmt.Errorf("%w: %s", ErrDegraded, reason)
  span "store.tx"{op}; defer record scp_store_tx_seconds{op}
  tx := rw.BeginTx(ctx)                      // _txlock=immediate in the DSN => BEGIN IMMEDIATE
  post, err := fn(tx)                        // fn uses ONLY tx; post (optional) updates in-memory counters AFTER commit
  err => Rollback; return fail(op, err)
  Commit err => return fail(op, err)         // SQLITE_FULL / IOERR on commit flips degraded here
  post()
read(ctx, op, fn):
  gate(op, write=false)                      // reads work while degraded; only Close blocks them
  tx := ro.BeginTx(ctx, ReadOnly: true); defer Rollback; fn(tx)  // one consistent read snapshot for the whole load
fail(op, err):
  pass through unchanged: context.Canceled/DeadlineExceeded, *InvalidError, *NoRepoError, ErrNoItem, ErrTooLarge, ErrUnknownNamespace
  else: scp_store_errors_total{op}++; if SQLite primary code in {IOERR 10, CORRUPT 11, FULL 13, CANTOPEN 14, READONLY 8, NOTADB 26}: markDegraded(op, err)
        return fmt.Errorf("store: %s: %w", op, err)
```

A cancelled context is **not** a store failure (a job cancelled by shutdown must not degrade the store): `fail` passes it through uncounted. SQLite's **primary** result code (`code & 0xff`) is extracted through the driver seam (`driver.code`), so extended codes (`SQLITE_IOERR_WRITE`) classify correctly on both drivers. BUSY is deliberately not degrading: `busy_timeout=5000` plus a single writer means BUSY would indicate an external process on the file, which the logs and `scp_store_errors_total{op}` surface without halting writes.

Design choice: the store reads the process clock in three places that never feed a domain decision: `parked.since` (the contract has no `since` parameter; it is first-park time and survives re-parking), `meta.created_at/last_open_at` and the backup-age gauge. Everything the planner or a job decides on arrives as a parameter (`now` in `Revalidated`, `Failure.At`, `Item.FetchedAt`).

### 4.4 Store: per-method semantics (SQL is in section 5.3)

| Method | Semantics | Typed errors |
|---|---|---|
| `LoadCatalogue` | all rows `ORDER BY slug`; `Meta` returned as the stored JSON bytes; `GitHubID` 0 when NULL | |
| `LoadItems(fn)` | one read tx, rows `ORDER BY repo.id, item.id`, one callback per row; callback error aborts and is returned (wrapped by `fail` only if it is a driver error) | callback's own error |
| `LoadPlans(fn)` | items with `next_due_at IS NOT NULL`, same order | |
| `LoadCommits(repo, limit)` | newest first by `(committed_at DESC, id DESC)`; `limit <= 0` or unknown repository returns `nil, nil` (a repository without commits and a missing one are indistinguishable to the reader by design: the snapshot decides existence) | |
| `ReplaceCatalogue` | makes the stored catalogue equal to the argument in one transaction (algorithm 4.5); returns the removed keys sorted by `String()`. **The caller guards against a failed or partial listing**: an empty or truncated argument removes (cascades) everything missing from it, by contract (`catalog` owns the `max_removal_ratio` guard, architecture 16); a matched row keeps its known GitHub id when the incoming id is 0 (`coalesce`) | `ErrInvalid` for a duplicate key, slug or non-zero GitHub id, a key with ASCII upper-case, `Meta` that is not a JSON object, zero `SeenAt`; nothing is written |
| `PutItem` | upsert on `(repo_id, kind)`; resets `attempts=0`, `error=NULL`; does NOT touch `next_due_at`/`unchanged_streak`; nil `Payload` stays `NULL` (an empty non-nil payload stays an empty blob) | `ErrInvalid` (state not a result state, zero `FetchedAt`/`CheckedAt`, bad key/kind), `ErrTooLarge` (> 16 MiB), `NoRepoError` when 0 rows were affected |
| `Revalidated(k, now)` | sets `fetched_at = checked_at = now`, `attempts=0`, `error=NULL` where `status NOT IN ('error','never')`; never writes payload, ETag or content SHA | `NoRepoError` (no repository), `ErrNoItem` (no row or no good value) |
| `RecordFailure(k, f)` | upsert: creates an `error` row when none exists, else `attempts+1`, `checked_at=f.At`, `error=f.Code`; keeps payload and `fetched_at`; the status stays as it was when `fetched_at IS NOT NULL` (stale-while-error), becomes `error` otherwise | `ErrInvalid` (code empty or > 64 bytes, zero `At`), `NoRepoError` |
| `SavePlans(rows)` | `UPDATE item SET next_due_at, unchanged_streak WHERE kind AND repo` for each row, one transaction, prepared statement; rows with a zero `RepoKey` (global kinds) and rows with no item row are skipped (design choice: global kinds are due at every start and an item without a row is due anyway; Q3) **and counted** in `scp_store_plan_rows_skipped_total{reason}` so a job-kind/item-kind mismatch (2.5) is visible | `ErrInvalid` for a non-zero but invalid key or a negative `UnchangedStreak` (validated for every row before the transaction opens) |
| `AppendCommits(repo, cs, keep)` | validate **every** row first (the call is atomic on both implementations), insert each commit with `ON CONFLICT (repo_id, sha) DO NOTHING` (commits are immutable: an existing SHA is never rewritten; only the uniqueness conflict is ignored, never a constraint violation: the first prototype's `INSERT OR IGNORE` silently dropped a commit with a NULL payload, V-03), then trim to the newest `keep` in the same transaction (5.3) | `ErrInvalid` (`keep <= 0`, empty or > 128-byte SHA, zero time, **empty payload**), `ErrTooLarge` (payload > 16 MiB), `NoRepoError` |
| `SaveTask(name, s)` | upsert; `LastError` truncated to 512 bytes at a UTF-8 boundary | `ErrInvalid` (empty or > 128-byte name) |
| `Park(key, reason, retryAt)` | upsert; `since` is stamped on first insert and kept on conflict; reason truncated to 512 bytes | `ErrInvalid` (empty or > 600-byte key, zero `retryAt`) |
| `Unpark`, `LoadParked`, `LoadTasks` | trivial | |
| `GetCache(ns,key)` | read-pool lookup; on a hit records recency **in memory** (`touched[(ns,key)] = ++seq`, bounded at 8,192 entries, beyond which a hit is simply not recorded); a read never takes the writer | `ErrInvalid`, `ErrUnknownNamespace` |
| `PutCache(ns,key,data)` | one transaction, **serialised end to end by `putMu`** (the byte accounting is updated after commit, so a second put must not read it in between, V-06): flush pending recency ticks, upsert with `use_seq = ++seq`, then evict (5.3); a nil `data` is stored and read back as an empty value on both implementations | `ErrInvalid`, `ErrUnknownNamespace`, `ErrTooLarge` (value larger than the namespace cap: it would evict itself) |
| `Check(full)` | `quick_check` (false) or `integrity_check` (true) on the `bk` connection, serialised with `Backup` by `bkMu`; a `CorruptError` flips the store to degraded | `ErrCorrupt` |
| `Backup(path)` | section 4.7 | `ErrBackupIsSource` |
| `Health()` | lock-free read of an `atomic.Pointer[store.Health]`; `{OK:true}` when never degraded | |
| `Run(ctx)` | section 8.2 | |
| `Close(ctx)` | section 8.3 | |

Zero-value rules: a zero `time.Time` is `NULL` in `fetched_at`, `next_due_at`, `last_attempt`, `last_success`; `fromUnix` maps `NULL` and 0 to the zero time. Strings that are optional (`etag`, `content_sha`, `error`, `last_error`) are `NULL` when empty. Payloads are bound as blobs, never as text.

### 4.5 `ReplaceCatalogue`: rename, transfer, swap, reuse

Goal: the stored catalogue equals the argument, **preserving items and commits across a repository rename or transfer** (GitHub's numeric id is stable; the `(source, owner, name)` triple and the slug are not) and removing what vanished (cascade deletes its items and commits). The algorithm runs in one `BEGIN IMMEDIATE` transaction:

```
validate(repos) first (outside the tx): per-row ValidateRepo, then uniqueness of key, slug and (source, github_id) within the argument
old := SELECT id, source, owner, name, coalesce(github_id,0), slug FROM repo
byKey, byGID(source, gid) indexes over old
pass 1  (match by GitHub id):  for each incoming with GitHubID != 0: if byGID has an unclaimed row: claim it
pass 2  (match by key):        for each unmatched incoming: r := byKey[in.Key]; claim r only if it is unclaimed AND not another repository
                               (r.gid == 0 || in.GitHubID == 0 || r.gid == in.GitHubID)
delete every unclaimed old row:  DELETE FROM repo WHERE id=?     -- cascades item and commit_log
                                 DELETE FROM parked WHERE <key tail == "source/owner/name" preceded by '/'>   -- see 5.3: parked keys are engine job keys
                                 removed += r.key
phase A: for every claimed row whose key or slug changes: UPDATE repo SET owner=tmp, name=tmp, slug=tmp  (tmp = "~tmp~<id>")  -- frees the UNIQUE values so a swap cannot collide
phase B: for every incoming: matched => UPDATE all columns; unmatched => INSERT
return removed sorted by key string
```

Why two phases: swapping the names of two repositories (`a<->b`) or reusing a freed name for another repository within one transaction would otherwise violate `UNIQUE(source,owner,name)` or `UNIQUE(slug)` mid-way. Cases pinned by the suite: `RoundTrip` (ordered by slug), `EmptyReplaceRemovesAll`, `RemovalCascadesItemsCommitsAndParkedKeys`, `RenameByGitHubIDKeepsRows`, `SwapNamesWithinOneTransaction`, `NameReusedByAnotherRepositoryIsANewRow` (a removed repository's name taken by a different GitHub id is a NEW row, never inheriting the old one's items), `ValidationRejectsWholeCall`. A repository whose `GitHubID` is unknown (0) on either side matches by key only.

A matched row's `parked` keys are **not** rewritten on a rename (they are engine job keys under the old name); the catalogue's `SetSubjects` re-subjects the planner under the new key and the engine's `Gone` classification drops the old parked key at its next probe (at most `ParkRetry`, 24 h). `removed` includes only keys that disappeared; a renamed repository is not "removed" (its old key is reported by the catalogue's own diff, which `catalog` computes against its previous snapshot for the `aliases` global).

### 4.6 Warm start (`cmd/scp` step 5): from rows to snapshot version 1

The ordering constraint is the existence slot: nothing but the existence slot may be `Put` for an entry that has no existence value yet (F-08), so the catalogue goes first inside one `Apply`. The prototype's `cmd/scp/warm_test.go` runs this protocol against the memory store.

```
1. decode OUTSIDE Apply (no lock held): cat := st.LoadCatalogue(ctx); items := collect via st.LoadItems(ctx, cb) -- cb only appends to a slice
      (cb MUST NOT call the store; decoding payload bytes may happen here or in step 2, but never inside fn)
2. pub.Apply(func(b *Builder) error {
        for r in cat:  Put(b, EntryID(r.Slug), repoSlot, freshness.Value{V: decodeRepo(r), Info: ...})      // existence slot
        for it in items: slot := slotForKind[it.Key.Kind]; id := slugOf[it.Key.Repo]
                         if errors.Is(err = Put(b, id, slot, decode(it)), ErrUnknownRepo): continue          // impossible with FK; tolerated
                         else if err != nil: return err
        commits: for each repo, LoadCommits happens in step 1 (outside), Put in step 2
        return nil })                                                                                          // ONE Apply: snapshot version 1
3. planner restore: st.LoadPlans(ctx, cb) -> refresh.Planner seed; st.LoadTasks, st.LoadParked -> scheduler/engine restore options
```

**Peak memory and the callback (V-12).** Collecting every `Item` in a slice first holds the raw payload bytes of the whole store at once (about 100 MB of README and rendered HTML at 1,000 repositories) on top of the decoded values that end up in the snapshot (52 MB steady state), roughly doubling the start-up peak. The feature decoders MUST therefore run **inside the `LoadItems` callback** (no lock is held there; the callback must not call the store) and the callback appends only the decoded value, so each raw payload is garbage as soon as its row is decoded; `GOMEMLIMIT` sizing (85% of the container limit) must include this peak. The prototype's `cmd/scp/warm_test.go` follows this ordering since the verification (it initially performed `LoadCatalogue`/`LoadItems` **inside** `Apply`'s `fn`, which the rules above forbid). A kind that no feature registered (feature removed or disabled) has no `slotForKind` entry: its rows are ignored here and deleted at the next `Open` if `Deps.KnownKinds` lists the compiled-in kinds (Q5). Decode errors of one row (a payload with a newer inner `schema`, a corrupt blob) skip that row, count it in the owning feature's decode metric, and leave the item "due now" (the planner sees no `fetched_at`): a bad payload degrades to a re-fetch, never to a failed start. Rendered HTML read back from the store is untrusted bytes and is re-sanitised (or re-rendered when the pipeline version differs) by `docs` (architecture 8.10, ADR-005).

Warm start time: opening the prototype database, migrate/fingerprint/quick_check and loading about 34,000 rows is **112-125 ms** on the dev machine (`BenchmarkWarmLoad34k`, includes `Open`); the research figure for the load alone was 31-47 ms (M2). Either is far inside the 10-minute `readiness.startup_deadline`.

### 4.7 Backup, check, restore

```
Backup(ctx, path):                              // also called by Run hourly and by Close when backup.on_shutdown
  abs := Abs(path); if abs == Abs(store path) or SameFile(abs, store path): return ErrBackupIsSource
  lock bkMu; tmp := abs + ".tmp"; remove stale tmp (a crashed earlier backup)
  bk.ExecContext(`VACUUM INTO ?`, tmp)           // bound parameter, on the read-only bk connection: never the writer, never query_only
  verifyFile(tmp): open mode=ro&immutable=1; application_id == appID; user_version == s.schemaV; PRAGMA quick_check == ok
  rename(tmp, abs); fsync the DIRECTORY (never the database file); lastBackup = now
  defer: backups_total{ok|error}++, backup_seconds, lastBackupErr = msg on failure, remove tmp on failure
CheckFile(path, full):  verifyFile (user_version <= known), then integrity_check when full;      // offline; opens immutable
Restore(dbPath, backupPath, keep):             // offline only: the service MUST be stopped
  verifyFile(backupPath) else error "restore: backup does not verify"
  if dbPath exists: open a connection with locking_mode(EXCLUSIVE), busy_timeout 100 ms, BEGIN IMMEDIATE; failure => error "database is in use (stop the service first)"
                    quarantine(dbPath, keep)    // the old set is renamed, never deleted
  else: removeOrphanSidecars(dbPath)
  copy backup -> dbPath + ".restore.tmp" (0600, fsync); rename onto dbPath; fsync directory
```

Facts behind these choices (reproduced locally 2026-10-14 on modernc v1.60.1 / SQLite 3.53.4 and again with ncruces v0.35.6): `VACUUM INTO` **fails** on a `query_only=1` connection ("attempt to write a readonly database", V10), which is why `bk` is a separate connection with `mode=ro` and no `query_only`; modernc accepts `file:...?mode=ro` URIs and a bound parameter for `VACUUM INTO` on them; `temp_store=2` (MEMORY) is accepted as a `_pragma`; in WAL mode `BEGIN EXCLUSIVE` only excludes other writers, so `Restore` uses `locking_mode(EXCLUSIVE)` plus `BEGIN IMMEDIATE`, which needs the shared-memory lock and therefore fails with BUSY while **any** other connection (the running service, an operator's `sqlite3`) has the file open. A backup of a database whose copy fails verification is deleted and never installed (`TestBackupOfCorruptCopyIsNotInstalled`). The backup file contains everything the database does, including private repository metadata when private visibility is enabled (section 10).

`firstBackupDelay(interval, age, exists)`: no backup file => `min(interval, 5m)`; otherwise `min(max(interval-age, 5m), interval)`: resume the cadence from the file's age, never sooner than 5 minutes after start (the cold-start storm) and never later than one interval. After a backup the timer is reset to `interval` (fixed delay, the next interval starts after completion, GR-24).

### 4.8 Degraded mode

`markDegraded(op, err)` stores `Health{OK:false, Reason:"op: err"}` and logs ERROR once per transition (with `probe_interval`); `gate` then refuses writes with `ErrDegraded` (wrapping the reason) while reads and `Check`/`Backup` keep working. Triggers: a SQLite primary code in `{IOERR, CORRUPT, FULL, CANTOPEN, READONLY, NOTADB}` on any operation, or a `CorruptError` from `Check`. The `Run` worker probes every `probe_interval` while unhealthy: one small write on `rw` that bypasses the gate (`INSERT ... meta('probe', now) ON CONFLICT DO UPDATE`); success calls `markHealthy()` (log INFO "store recovered: writes resumed"). Consumers' obligations (architecture 9.3): a job whose `PutItem` fails with `ErrDegraded` logs once, counts `scp_store_errors_total{op}`, **still publishes to the snapshot**, and the item is retried by the planner (its row is overdue). Limitation (Q4): a successful probe clears the state whatever caused it, so a corruption flagged by `Check` is re-flagged only by the next `Check`; the remedy for corruption is a restart (quick_check then quarantines), which `/status` states.

### 4.9 Cache (generic bounded LRU)

Namespaces are registered at `Open` through `Deps.CacheNamespaces` (name to byte cap; the architecture's `store.cache_caps.{blob,parse,locked}` and the key `depsdev` required by unit 13 map to the constants `manifests.blob`, `manifests.parse`, `manifests.locked` and `manifests.depsdev`, Q6). A namespace that is not registered is rejected before SQL. Recency is a **logical tick** (`use_seq`, a process counter seeded with `max(use_seq)` at open), not a time, so eviction order is deterministic and synctest-friendly (Deviation D3). `GetCache` records ticks in memory only; they are written in the same transaction as the next `PutCache`, by the 5-minute `FlushTouches` in `Run`, and by `Close`. An unflushed tick lost in a crash only makes eviction slightly less accurate (approximate LRU, architecture 8.5). Eviction (5.3) runs in the `PutCache` transaction down to **90%** of the cap (`cap - cap/10`), in batches of 256 oldest, never evicting the key just written. `cacheBytes` accounting is updated after commit and re-derived from `SUM(size)` at open (`Durable/CacheAccountingSurvivesReopen`).

### 4.10 Edge-case catalogue (all asserted)

| Situation | Behaviour |
|---|---|
| `PutItem` with state `error` or `never` | `ErrInvalid` (only `RecordFailure` creates `error`) |
| `RecordFailure` on a row with a good value in state `not_found` | status stays `not_found`, payload and `fetched_at` kept, `attempts+1`, `error` set |
| `Revalidated` on an `error` row | `ErrNoItem` (nothing to revalidate; the job should have fetched) |
| 304 after a failure | `attempts` reset to 0 and `error` cleared (`Item/RevalidatedKeepsPayloadAndClearsFailure`) |
| Job completes after `ReplaceCatalogue` removed its repository | `NoRepoError` (`Gone`): the engine drops the key (F-08) |
| `AppendCommits` with equal timestamps | order by insertion id (`id DESC` tiebreak), stable (`Commits/EqualTimestampsOrderByInsertion`) |
| `AppendCommits` re-sending known SHAs | ignored; stored payload not rewritten (`ExistingSHAIsNotRewritten`) |
| `GetCache` value of the wrong namespace | independent namespaces (`NamespacesAreIndependent`) |
| `PutCache` overwriting a key | accounting adjusted by the delta, no eviction if it fits (`OverwriteAdjustsAccounting`) |
| Context cancelled before or during a call | the context error is returned unchanged, the store stays healthy, nothing is counted |
| `Close` twice, or a call after `Close` | second `Close` is nil; calls return `ErrClosed` |
| Path that is `:memory:` or a `file:` URI | rejected by `Config.Validate` (the DSN is always built by `buildDSN`) |
| A `RepoKey` with ASCII upper-case, or `Repo.Meta` that is not a JSON object | `ErrInvalid` before any SQL, on both implementations (`Catalogue/MixedCaseKeyRejected`, `Catalogue/InvalidMetaRejectedBeforeAnyWrite`) |
| `AppendCommits` batch with one empty payload | whole call rejected with `ErrInvalid`, nothing stored (`Commits/EmptyPayloadRejectedAndCallIsAtomic`) |
| `PutCache` with a nil value | stored and read back as an empty value (`Cache/NilValueRoundTripsAsEmpty`) |
| `SavePlans` row whose item kind matches no item row | not persisted, counted in `scp_store_plan_rows_skipped_total{reason="no_item"}` (2.5) |
| Orphan `-wal` beside a path that does not exist | quarantined, never replayed into the new database (`TestOrphanWALIsNotReplayedIntoANewDatabase`) |

---

## 5. Data: SQLite schema, pragmas, migrations, retention, backup

### 5.1 Engine, files, connections, pragmas

Engine: `modernc.org/sqlite` v1.60.1 (SQLite 3.53.4, published 2026-09-29), registered as database/sql driver `"sqlite"` by the package's own `init` (outside G5, which governs our code); escape hatch `github.com/ncruces/go-sqlite3` v0.35.6 (driver `"sqlite3"`, published 2026-09-23). Both versions were the latest the Go module proxy served on 2026-10-14 (`proxy.golang.org/<module>/@latest`); pkg.go.dev confirmed v1.60.1 bundles SQLite 3.53.4 and describes `StrictPragmas` as a process-wide switch, off by default, that rejects a `_pragma` value containing more than one SQL statement. One database file in a writable volume (default `/data/scp.db`); network filesystems are unsupported (WAL). The directory must be writable by the process user because WAL creates `-wal` and `-shm` siblings.

| Role | Handle | Pool | DSN form (`file:///abs/path?...`, built by `buildDSN`, path escaped by `net/url`) | Used for |
|---|---|---|---|---|
| `rw` | `*sql.DB` | **1** connection, `ConnMaxLifetime=0`, `ConnMaxIdleTime=0` | `_pragma=busy_timeout(5000)`, `temp_store(2)`, `journal_mode(WAL)`, `synchronous(normal\|full)`, `foreign_keys(1)`, `trusted_schema(0)`, `journal_size_limit(67108864)`; `_txlock=immediate` | every write, migrations, the open-time quick_check, the probe, `PRAGMA optimize`, the final checkpoint |
| `ro` | `*sql.DB` | `read_conns` (default 2, max 8) | `mode=ro`; `_pragma=busy_timeout`, `temp_store(2)`, `query_only(1)`, `trusted_schema(0)` | warm load, admin reads, `GetCache`, `LoadCommits` |
| `bk` | `*sql.DB` | **1** connection | `mode=ro`; `_pragma=busy_timeout`, `temp_store(2)`, `trusted_schema(0)` (NO `query_only`) | `Backup` (`VACUUM INTO`), `Check`; serialised by `bkMu` |

Pragma rationale (each verified on 3.53.4 unless noted): `journal_mode=WAL` (readers never block the writer; persisted in the file header, so `ro`/`bk` need not set it); `synchronous=NORMAL` (WAL durability is "at most the last transactions lost on power failure", acceptable for a cache; `full` is selectable); `busy_timeout=5000` (a second process on the file); `foreign_keys=1` on `rw` (cascades; `PRAGMA foreign_keys` is per connection, which is why the migration runner uses one dedicated connection per migration); `trusted_schema=0` (a hand-edited database cannot run unsafe SQL functions through views or triggers); `journal_size_limit=64MiB` (the WAL file is truncated back after a checkpoint instead of staying at its high-water mark); **`temp_store=2` (MEMORY)** because the container root filesystem is read-only and distroless has no `/tmp` (sorts and `VACUUM INTO` temporaries must not touch the disk; the image smoke job asserts a `VACUUM INTO` and a large sort succeed read-only, Q1); `wal_autocheckpoint` stays at the default 1000 pages; `PRAGMA optimize=0x10002` once at open (analyse every table that needs it) and `PRAGMA optimize` at close and daily (`store.optimize`).

**Pins (verified 2026-10-14 on proxy.golang.org):** `modernc.org/sqlite` v1.60.1 requires `modernc.org/libc` v1.77.1 exactly (its own `go.mod`); libc v1.78.0 already exists, and modernc's generated code must be built with the libc version of its release, so `go get -u` MUST NOT move libc independently (Renovate groups the two; `go.mod` keeps libc as an indirect requirement at the version of the sqlite release). The architecture pins Go 1.27.2 (`go 1.27.2`); the prototype was run on 1.27.1, which changes nothing in this unit. `sqlite.StrictPragmas(true)` is set before the first `sql.Open` (the `driver.strict` seam; a no-op for ncruces). The version guard refuses `sqlite_version() < 3.51.3` (the WAL-reset corruption bug present in 3.7.0 through 3.51.2); the guard parses `major.minor.patch` and fails closed on an unparseable string.

### 5.2 DDL (`internal/store/sqlite/migrations/0001_init.sql`, executed and tested)

```sql
-- 0001_init.sql: forward-only. The database is a rebuildable cache of GitHub: "down" is deleting the file.
-- Time columns are UTC unix seconds written by the application (never CURRENT_TIMESTAMP).
PRAGMA application_id = 1396790321; -- 0x53435031 "SCP1": a file with another non-zero id is refused, never quarantined

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT;          -- created_at, last_open_at, build_id, probe

CREATE TABLE repo (                                                            -- the persisted catalogue
  id         INTEGER PRIMARY KEY,
  source     TEXT    NOT NULL,
  owner      TEXT    NOT NULL,
  name       TEXT    NOT NULL,
  github_id  INTEGER,                                                          -- stable across rename/transfer; NULL when unknown
  slug       TEXT    NOT NULL,
  seen_at    INTEGER NOT NULL,                                                 -- last reconcile that saw it
  meta       TEXT    NOT NULL CHECK (json_valid(meta)),                        -- JSON: description, default_branch, topics, visibility, aliases
  UNIQUE (source, owner, name),
  UNIQUE (slug)
) STRICT;
CREATE UNIQUE INDEX repo_github_id ON repo (source, github_id) WHERE github_id IS NOT NULL;

CREATE TABLE item (                                                            -- one row per (repo, kind)
  id               INTEGER PRIMARY KEY,
  repo_id          INTEGER NOT NULL REFERENCES repo(id) ON DELETE CASCADE,
  kind             TEXT    NOT NULL,
  status           TEXT    NOT NULL CHECK (status IN ('ok','not_found','unsupported','too_large','no_content','error','never')),
  fetched_at       INTEGER,                                                    -- last successful determination OR successful 304 validation
  checked_at       INTEGER NOT NULL,                                           -- last attempt
  next_due_at      INTEGER,                                                    -- planner-owned (SavePlans)
  attempts         INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),           -- store-owned: consecutive failures
  unchanged_streak INTEGER NOT NULL DEFAULT 0 CHECK (unchanged_streak >= 0),   -- planner-owned (SavePlans)
  etag             TEXT,
  content_sha      TEXT,
  error            TEXT,                                                       -- stable code of the last failed attempt
  payload          BLOB,                                                       -- LAST column: metadata scans stay off overflow pages
  UNIQUE (repo_id, kind),
  CHECK (status NOT IN ('ok','not_found','unsupported','too_large','no_content') OR fetched_at IS NOT NULL)
) STRICT;

CREATE TABLE commit_log (
  id           INTEGER PRIMARY KEY,
  repo_id      INTEGER NOT NULL REFERENCES repo(id) ON DELETE CASCADE,
  sha          TEXT    NOT NULL,
  committed_at INTEGER NOT NULL,
  payload      BLOB    NOT NULL,
  UNIQUE (repo_id, sha)
) STRICT;
CREATE INDEX commit_log_recent ON commit_log (repo_id, committed_at DESC, id DESC);

CREATE TABLE task_state (name TEXT PRIMARY KEY, last_attempt INTEGER, last_success INTEGER, last_error TEXT) STRICT;
CREATE TABLE parked     (key  TEXT PRIMARY KEY, since INTEGER NOT NULL, reason TEXT NOT NULL, retry_at INTEGER NOT NULL) STRICT;

CREATE TABLE cache (                                                           -- generic bounded cache, namespaces owned by features
  ns      TEXT    NOT NULL,
  key     TEXT    NOT NULL,
  data    BLOB    NOT NULL,
  size    INTEGER NOT NULL,
  use_seq INTEGER NOT NULL,                                                    -- logical recency tick, not a time (deterministic LRU)
  PRIMARY KEY (ns, key)
) STRICT;
CREATE INDEX cache_lru ON cache (ns, use_seq);
```

Notes: `STRICT` on every table (type affinity cannot silently coerce a time into text); the CHECK on `item` makes "a result state without a `fetched_at`" unrepresentable even for a buggy writer; `repo.meta` is validated as JSON by SQLite itself (a feature decoder still validates its fields); the partial unique index lets many repositories have an unknown GitHub id while forbidding two rows for one GitHub id; `payload` is the **last** column of `item` so that scans over metadata columns stay on the b-tree leaf page and do not walk overflow chains of 25-250 KB payloads; no `AUTOINCREMENT` (ids may be reused after deletes; nothing refers to an id outside the database). `item.next_due_at`, `attempts` and `unchanged_streak` are separate columns because three different writers touch the row and each statement names exactly the columns it owns (section 2.5).

### 5.3 The statements (as executed; `?N` are bound parameters, no value is ever concatenated into SQL)

```sql
-- PutItem (0 rows affected => NoRepoError; the SELECT yields no row when the repository is gone)
INSERT INTO item(repo_id, kind, status, fetched_at, checked_at, attempts, etag, content_sha, error, payload)
SELECT id, ?4, ?5, ?6, ?7, 0, ?8, ?9, NULL, ?10 FROM repo WHERE source=?1 AND owner=?2 AND name=?3
ON CONFLICT(repo_id, kind) DO UPDATE SET status=excluded.status, fetched_at=excluded.fetched_at, checked_at=excluded.checked_at,
  attempts=0, etag=excluded.etag, content_sha=excluded.content_sha, error=NULL, payload=excluded.payload;

-- Revalidated (0 rows => NoRepoError if the repository row is missing, else ErrNoItem)
UPDATE item SET fetched_at=?5, checked_at=?5, attempts=0, error=NULL
WHERE repo_id=(SELECT id FROM repo WHERE source=?1 AND owner=?2 AND name=?3) AND kind=?4 AND status NOT IN ('error','never');

-- RecordFailure: stale-while-error; payload, fetched_at, etag and content_sha are not in the SET list
INSERT INTO item(repo_id, kind, status, fetched_at, checked_at, attempts, error)
SELECT id, ?4, 'error', NULL, ?5, 1, ?6 FROM repo WHERE source=?1 AND owner=?2 AND name=?3
ON CONFLICT(repo_id, kind) DO UPDATE SET
  status = CASE WHEN item.fetched_at IS NULL THEN 'error' ELSE item.status END,
  checked_at = excluded.checked_at, attempts = item.attempts + 1, error = excluded.error;

-- ReplaceCatalogue phase B (matched row): github_id=coalesce(?5, github_id): an unknown incoming id never erases a known one
-- SavePlans: one prepared statement, one transaction; the planner is the only caller (RowsAffected 0 is counted, not an error)
UPDATE item SET next_due_at=?5, unchanged_streak=?6
WHERE kind=?4 AND repo_id=(SELECT id FROM repo WHERE source=?1 AND owner=?2 AND name=?3);

-- warm load
SELECT source, owner, name, github_id, slug, seen_at, meta FROM repo ORDER BY slug;
SELECT r.source, r.owner, r.name, i.kind, i.status, i.fetched_at, i.checked_at, i.attempts,
       coalesce(i.etag,''), coalesce(i.content_sha,''), coalesce(i.error,''), i.payload
  FROM item i JOIN repo r ON r.id = i.repo_id ORDER BY r.id, i.id;
SELECT r.source, r.owner, r.name, i.kind, i.next_due_at, i.unchanged_streak
  FROM item i JOIN repo r ON r.id = i.repo_id WHERE i.next_due_at IS NOT NULL ORDER BY r.id, i.id;
SELECT sha, committed_at, payload FROM commit_log WHERE repo_id=(SELECT id FROM repo WHERE source=?1 AND owner=?2 AND name=?3)
  ORDER BY committed_at DESC, id DESC LIMIT ?4;
SELECT name, last_attempt, last_success, coalesce(last_error,'') FROM task_state;
SELECT key, since, reason, retry_at FROM parked ORDER BY key;

-- AppendCommits: insert (conflict on (repo_id, sha) ignored), then retention in the SAME transaction (keep = ?2, repo id = ?1)
INSERT INTO commit_log(repo_id, sha, committed_at, payload) VALUES(?1,?2,?3,?4) ON CONFLICT(repo_id, sha) DO NOTHING;   -- never OR IGNORE: it would swallow NOT NULL/CHECK violations
DELETE FROM commit_log WHERE repo_id=?1 AND (committed_at, id) <
  (SELECT committed_at, id FROM commit_log WHERE repo_id=?1 ORDER BY committed_at DESC, id DESC LIMIT 1 OFFSET ?2-1);

-- Parked cleanup when ReplaceCatalogue removes a repository: ?1 = "source/owner/name"; a parked key is an engine job key
-- "<kind>/<source>/<owner>/<name>", so the repository key is its tail preceded by '/'
DELETE FROM parked WHERE substr(key, -length(?1)) = ?1 AND substr(key, -length(?1)-1, 1) = '/';

-- Cache: upsert, then (only when the namespace total exceeds its cap) evict down to 90% in batches of 256 oldest,
-- never the key just written; accounting stays in memory and is re-derived at open
INSERT INTO cache(ns,key,data,size,use_seq) VALUES(?1,?2,?3,?4,?5)
  ON CONFLICT(ns,key) DO UPDATE SET data=excluded.data, size=excluded.size, use_seq=excluded.use_seq;
SELECT key, size FROM cache WHERE ns=?1 AND key<>?2 ORDER BY use_seq, rowid LIMIT 256;
DELETE FROM cache WHERE ns=?1 AND key=?2;
UPDATE cache SET use_seq=?3 WHERE ns=?1 AND key=?2 AND use_seq<?3;          -- recency flush (never moves a tick backwards)
SELECT ns, coalesce(sum(size),0) FROM cache GROUP BY ns;                     -- open: per-namespace bytes
SELECT max(use_seq) FROM cache;                                              -- open: seed of the tick counter

-- Open-time pruning (placeholder lists are generated from the registered kinds / namespaces, never from data)
DELETE FROM item  WHERE kind NOT IN (?,?,...);
DELETE FROM cache WHERE ns   NOT IN (?,?,...);
```

The only dynamically built SQL strings are the `IN (?,?,...)` placeholder lists (the number of `?` is generated; values are bound), `"PRAGMA " + pragma` with `pragma` one of the two constants `quick_check`/`integrity_check`, and `PRAGMA user_version = %d` with an integer taken from a validated migration file name. `VACUUM INTO ?` binds the target path.

The `CHECK (status IN (...))` list of 5.2 is exactly `freshness.States()` (kernel unit: "the SQL CHECK constraint is generated from and tested against `States()`"); `TestItemStatusCheckMatchesFreshnessStates` compares them, so a new kernel state fails the build until a migration widens the check.

**Retention summary** (what is deleted, when, by whom):

| Data | Rule | Mechanism |
|---|---|---|
| `commit_log` | newest `keep` (commits unit: 30) per repository, ordered by `(committed_at, id)` | trim statement above, same transaction as the insert; `scp_store_commits_trimmed_total` |
| `item`, `commit_log` of a removed repository | deleted with the repository | `ON DELETE CASCADE` from `ReplaceCatalogue` |
| `parked` keys of a removed repository | deleted with the repository | tail-match statement above |
| `item` rows of a kind no compiled-in feature registers | deleted at the next `Open` | `Deps.KnownKinds` (Q5); `scp_store_pruned_total{table="item"}` |
| `cache` | per-namespace byte cap, LRU, evict to 90% | `PutCache`; `scp_store_cache_evictions_total{ns}`; unregistered namespaces pruned at `Open` |
| quarantined database sets | newest `quarantine_keep` (default 2) | `pruneQuarantine` |
| backup | exactly one file (`<path>.bak`), replaced atomically | `Backup` |
| `item` payload of a live repository | never expires by age; staleness is derived from `fetched_at` at read time (GR-30 concerns unbounded growth, which per-repository bounds already prevent: 30 commits, one row per kind) | n/a |

**Query plans** (asserted by `TestHotQueryPlansUseIndexes`, no `TEMP B-TREE` and the named index): commits page uses `commit_log_recent`; item by key uses `sqlite_autoindex_item_1`; cache LRU scan uses `cache_lru`; repository by key uses `sqlite_autoindex_repo_1`.

### 5.4 Migration runner (own, about 60 lines of logic; goose and Atlas rejected in ADR-002)

- Files: `migrations/NNNN_snake_name.sql` embedded with `//go:embed migrations/*.sql`. The loader requires the pattern `^\d{4}_[a-z0-9_]+\.sql$` and a **contiguous** sequence from `0001` (a gap or a stray file is a build-time test failure and a start-up error).
- State: `PRAGMA user_version` is the applied count. `migrate()` reads it and applies `ms[cur:]` in order. If `user_version > len(ms)` it returns `ErrSchemaNewer` (the caller quarantines: an application rollback is a full cold start; the runbook says so, and migrations are written additive and read-compatible wherever possible so a one-version rollback does not meet this path).
- Atomicity: each migration runs on **one dedicated connection** (`db.Conn`) in **one transaction** that executes the script and then `PRAGMA user_version = N`; a failure rolls both back, so the file is never left half-migrated (`TestFailedMigrationLeavesVersionUntouched`, `TestMigrationRunnerIsAtomicAndAdditive`). The `rw` DSN's `_txlock=immediate` makes the transaction `BEGIN IMMEDIATE`.
- Table-rebuild migrations (SQLite's 12-step procedure): the first line `-- scp:foreign_keys=off` makes the runner execute `PRAGMA foreign_keys=OFF` on that connection before the transaction, run `PRAGMA foreign_key_check` before commit (any row fails the migration), and restore `foreign_keys=ON` afterwards (`TestForeignKeyOffMigration`).
- Forward-only; there is no down. Rules for future migrations: additive where possible (new nullable column, new table, new index); never repurpose a column; payload evolution never needs SQL (inner `schema` field); a migration that cannot be additive documents its rollback effect (cold start) in its header comment; every migration ships with a test that applies the whole chain to an empty database and to the previous release's fixture file (`testdata/v<N>.db`, generated by the previous release's migrations and committed).
- Fingerprint: after migration of a **valid existing** file, `schemaFingerprint` (SHA-256 over `type, name, tbl_name, whitespace-normalised sql` of every non-`sqlite_%` object, sorted) must equal the fingerprint of the same migrations applied to a private `:memory:` database; a mismatch (hand-edited file, or a released migration whose text was changed) quarantines with reason `schema_drift`. A freshly created file skips the check (it is the reference by construction).

### 5.5 Files on disk

```
/data/scp.db                          the database
/data/scp.db-wal, /data/scp.db-shm    WAL siblings (always renamed together with the database)
/data/scp.db.bak                      backup (store.backup.path overrides), written as .bak.tmp then renamed
/data/scp.db.quarantine-20261014T120000Z[-wal|-shm][-2]   newest store.quarantine_keep sets
/data/scp.db.restore.tmp              transient, during an offline restore
```

Disk budget: a backup needs free space about equal to the database size (`VACUUM INTO` writes a compacted copy next to it) and the WAL is capped near `journal_size_limit` after checkpoints; the runbook sizes the volume at 3x the database plus the 192 MiB blob and parse caps (Q7). The database size at 1,000 repositories was not measured in this unit; the prototype benchmark database (about 34,000 rows of 25 KB items and 300-byte commits) is the basis, and the image smoke job logs `Info.DBBytes` so the number is recorded at the first integration run.

### 5.6 Backup, quarantine and restore procedures (operator view)

| Situation | What happens | Operator action |
|---|---|---|
| Normal | hourly (when `store.backup.enabled`) and at shutdown (`on_shutdown`) `VACUUM INTO` + verify + rename | none; `/status` shows `last_backup` and `last_backup_error`; alert on `scp_store_backup_age_seconds` > 3 intervals |
| Start finds corruption, a newer schema, a failed migration or drift | file set quarantined, cold start, `scp_store_rebuilds_total{reason}` ++, `/status` shows `rebuilt: <reason>` and `QuarantinedAs` | inspect the quarantine set if curious; restoring is optional because a cold start repairs itself in 10-15 minutes |
| Start finds a foreign SQLite file | refuses to start (`ErrForeignDatabase`), file untouched | fix `store.path` |
| Runtime IOERR / FULL / CORRUPT | `degraded`: writes refused, reads and serving continue, probe every `probe_interval` | free disk space or restart; a restart quarantines a corrupt file |
| Operator wants the previous good state | stop the service; `scp store check --file scp.db.bak`; `scp store restore --from scp.db.bak` (calls `CheckFile`, then `Restore`) | start the service; warm start proceeds from the restored rows |
| Operator wants to verify without the service | `scp store check --file <path> [--full]` (`CheckFile`, immutable read-only open) | none |

The CLI verbs belong to `cmd/scp` (another unit); this unit supplies `CheckFile` and `Restore` as package functions that never need the running service and never write unless restoring.

---

## 6. Configuration keys

`readmodel` has no configuration. `store/sqlite` owns the `store:` block (root struct in `cmd/scp/config.go` embeds `sqlite.Config` as `Store sqlite.Config \`yaml:"store"\``; the root `Validate()` calls `Store.Validate("store")`, which returns path-accurate errors for **every** violation, `errors.Join`ed). Strict decode (unknown key is an error), env mapping `SCP_<PATH_IN_UPPER_SNAKE>` (architecture 16), durations as `time.Duration` strings, sizes as `ByteSize` (`"64MiB"`, `"512KiB"`, `"1GiB"`, or a plain byte count; decimal `MB` is rejected on purpose; a value that would overflow is rejected, not wrapped: the first prototype decoded `9999999999GiB` into a negative number). `ByteSize` implements `encoding.TextUnmarshaler`; `TestStoreConfigStrictDecode` (in `cmd/scp`) decodes the block through `go.yaml.in/yaml/v3` v3.0.5 with `KnownFields(true)` and pins durations, sizes, the rejection of unknown keys and of `64MB`. Cross-unit note: if `platform/config` exports its own byte-size type, `sqlite.ByteSize` is replaced by it in one commit (the store package may not import `platform/config`, rank 1, so the root struct in `cmd/scp` converts).

| Key | Type | Default | Validation | Env | Consumer |
|---|---|---|---|---|---|
| `store.path` | string | `/data/scp.db` | non-empty; no NUL; not `:memory:`; not a `file:` URI | `SCP_STORE_PATH` | `sqlite.Open` (`buildDSN`); also the base of the backup, quarantine and restore names |
| `store.sync` | enum | `normal` | `normal` or `full` | `SCP_STORE_SYNC` | `rw` DSN pragma `synchronous` |
| `store.busy_timeout` | duration | `5s` | 100ms..1m | `SCP_STORE_BUSY_TIMEOUT` | all three DSNs (`busy_timeout`) |
| `store.read_conns` | int | `2` | 1..8 | `SCP_STORE_READ_CONNS` | `ro` pool size (`SetMaxOpenConns`) |
| `store.journal_size_limit` | ByteSize | `64MiB` | 1MiB..1GiB | `SCP_STORE_JOURNAL_SIZE_LIMIT` | `rw` DSN pragma |
| `store.quarantine_keep` | int | `2` | 1..10 | `SCP_STORE_QUARANTINE_KEEP` | `quarantine`/`pruneQuarantine`, `Restore` |
| `store.probe_interval` | duration | `30s` | 5s..10m | `SCP_STORE_PROBE_INTERVAL` | `Run` worker (degraded-state write probe) |
| `store.backup.enabled` | bool | `false` | none | `SCP_STORE_BACKUP_ENABLED` | `Run`, `Close` |
| `store.backup.interval` | duration | `1h` | at least `1m` when enabled | `SCP_STORE_BACKUP_INTERVAL` | `Run` |
| `store.backup.path` | string | `""` = `<store.path>.bak` | must differ from `store.path` (cleaned, absolute) when enabled | `SCP_STORE_BACKUP_PATH` | `Backup`, `Close`, `Info` |
| `store.backup.on_shutdown` | bool | `true` | none | `SCP_STORE_BACKUP_ON_SHUTDOWN` | `Close` (only when `backup.enabled`) |
| `store.cache_caps.blob` | ByteSize | `64MiB` | 1MiB..4GiB | `SCP_STORE_CACHE_CAPS_BLOB` | `cmd/scp` maps it to the namespace constant exported by `manifests` (`manifests.blob`) in `Deps.CacheNamespaces` |
| `store.cache_caps.parse` | ByteSize | `128MiB` | 1MiB..4GiB | `SCP_STORE_CACHE_CAPS_PARSE` | maps to `manifests.parse` |
| `store.cache_caps.locked` | ByteSize | `64MiB` | 1MiB..4GiB | `SCP_STORE_CACHE_CAPS_LOCKED` | maps to `manifests.locked` (unit 13 section on locked components; resolves Q6) |
| `store.cache_caps.depsdev` | ByteSize | `16MiB` | 1MiB..256MiB | `SCP_STORE_CACHE_CAPS_DEPSDEV` | maps to `manifests.depsdev` (unit 13 RR-15, which states that this key is "defined here"; added by the verification, V-08) |

Validation messages are of the form `store.read_conns: must be 1..8, got 12`. The "documented keys == consumed keys" CI test (GR-12) finds every key above read by non-test code: the first twelve in `store/sqlite`, the four `cache_caps` in `cmd/scp`'s namespace mapping. Cache keys are at most 512 bytes (`ValidateCacheArgs`): a feature whose natural key can be longer (`manifests.locked`: `repoKey|unit|treeSHA`, with a deep monorepo path) hashes the tail (SHA-256 hex) before calling the store. `store.backup.enabled` defaults to `false` in the shipped example config because a backup doubles the disk need; the deployment guide recommends enabling it on any volume with a snapshot-less filesystem.

Config keys that other units own but this unit depends on: `commits.keep` (the `keep` argument of `AppendCommits`), the kinds registered by features (`Deps.KnownKinds`), and `readiness.warm_max_age`/`min_coverage` (computed from `Item.FetchedAt` by `ops`).

---

## 7. HTTP surface

Not applicable: this unit registers no routes and no middleware. It contributes **data** to routes owned elsewhere:

- `/readyz` gates (architecture 10.2): gate `store` is ok once `sqlite.Open` returned without error; gate `warm` is "loaded" when the step-5 `Apply` published version 1 (or the store was empty). `Health().OK == false` does **not** make the process unready (serving continues from the snapshot); it appears in `/status` as degraded with the reason.
- `/status` (admin): `store.Info()` (driver, path, schema version, SQLite version, database and WAL bytes, bytes per cache namespace, last backup time and error, `rebuilt` reason of this process, health) and `Publisher.Status()` (version, built-at, published/rejected/aborted counters, last rejection message and time) are read by `ops` through bridges (ops-owned view types); the registry's `SlotNames()`/`GlobalNames()` list what is registered.
- `ETag` for per-repository API resources is derived by `httpapi` from `Snapshot.Rev(id)` (the snapshot version at which that repository's entry last changed), so an unrelated publish does not invalidate a repository's cached response.

---

## 8. Concurrency and lifecycle

### 8.1 Goroutines and locks

| Goroutine | Owner | Started by | Stopped by |
|---|---|---|---|
| store worker (1): degraded probe, 5-minute recency flush, backup timer | `store/sqlite` (`Run`) | lifecycle `store` stage | stage context cancel; `Run` returns nil; joined before `Close` |
| none for `readmodel` | | | `Publisher` starts no goroutine; `Apply` runs on the caller's goroutine |
| none for `store/memory` | | | `Run` blocks on `ctx.Done()` only |

Every goroutine has one owner that waits for it (G10); `store/sqlite` and `readmodel` use `goleak.VerifyTestMain`. The prototype tests ran leak-free with `-race`; `goleak` itself is added in M5 (section 13).

| Lock | Protects | Held across I/O? | Order |
|---|---|---|---|
| `Publisher.mu` | serialises builders (`Apply`) | no I/O allowed by contract (`fn` MUST NOT block) | outermost |
| `Publisher.stMu` | `PublishStatus` counters | no | taken inside `mu` and alone by `Status()`; never the reverse |
| `sqlite.Store.bkMu` | the single `bk` connection (`Backup`, `Check`) | **yes**: held during `VACUUM INTO` (0.5-1 s at 192 MiB of blobs) | independent of the others; `Check` waits behind a running backup, which is acceptable (both are background operations) |
| `sqlite.Store.cacheMu` | `cacheBytes` map | no | leaf |
| `sqlite.Store.touchMu` | `touched` recency map | no | leaf; held only to swap or update the map, never across a SQL call |
| the `rw` connection itself | all writes (pool of 1) | yes, by design | a write transaction uses only `tx`; **no store method may call another store method while it holds the transaction**, because the second call would wait for the one connection the first already holds |
| atomics | `closed`, `health` (`atomic.Pointer[Health]`), `seq`, `lastBackup`, `lastBackupErr` | n/a | n/a |
| `memory.Store.mu` | everything | no | leaf |

The single-connection write pool is deliberate (ADR-002): SQLite has one writer anyway, `_txlock=immediate` takes the write lock at `BEGIN` so a transaction never upgrades mid-way (no `SQLITE_BUSY` deadlock between two upgrading readers), and a pool of one removes `busy_timeout` contention between our own goroutines. The cost is the reentrancy rule above, pinned by `Reentrancy/CallbackThatCallsTheStoreDoesNotDeadlock` for the read path and by code structure (`write` hands `fn` only `tx`) for the write path.

### 8.2 `Run(ctx)` (the store worker)

```
Run(ctx):
  w := worker{probeEvery, backupEvery, backupEnabled, firstBackup, healthy, probe, backup, flush, onErr}   // effects injected: testable without SQLite
  if backup enabled: firstBackup = firstBackupDelay(interval, age of backup file, exists)
  w.run(ctx):  select { ctx.Done => return;
                        probeTicker  => if !healthy: probe(ctx)            (error => onErr, log WARN)
                        flushTicker (5 min) => if healthy: FlushTouches
                        backupTimer => if healthy: Backup(ctx, BackupPath()); backupTimer.Reset(interval) }
  return nil
```

All time here is `time.Ticker`/`time.Timer` on the process clock, so the entire worker runs deterministically inside a `testing/synctest` bubble (`TestWorkerTimingInSynctestBubble`); there is no `Clock` interface (ADR-003). A failed backup does not stop the loop, is logged WARN once per failure, counted (`scp_store_backup_total{result="error"}`) and shown in `/status`.

### 8.3 Lifecycle and shutdown order

`cmd/scp` step 2 calls `sqlite.Open` (no goroutines). The lifecycle `store` stage's `Run` is `store.Run`; its `Close` is `store.Close`, called **after** `Run` has been joined and after the refresh stage has stopped (so no job writes during Close), per architecture 10.4: servers stop, refresh stage drains (planner's last `SavePlans` flush), then the store stage, then telemetry (last, so shutdown is observable).

```
Close(ctx):                                        // idempotent: CompareAndSwap(closed false->true); later calls return nil
  closed = true                                    // from now on every Store method returns ErrClosed; Close uses the raw handles below
  if healthy:  finalFlush (pending recency ticks in one tx); PRAGMA optimize
  if healthy && backup.enabled && backup.on_shutdown: backup(ctx, BackupPath())      // bounded by ctx: the stage deadline cancels it, tmp removed
  if healthy:  PRAGMA wal_checkpoint(TRUNCATE) -> (busy, log, ckpt); busy != 0 => log WARN "could not truncate: a reader was active"
  close ro, bk, rw in that order; errors.Join of everything that failed
```

A degraded store skips every write step and only closes the handles (an I/O-failing disk is not asked for a backup). The final backup shares the shutdown budget with the other stages (architecture 10.4: 5 + 15 + 15 + 1 + 2 s); the `Close(ctx)` context carries the stage's share, and a backup that does not fit is abandoned (its `.tmp` removed) rather than extending termination. Failure of `Run` (never in practice: it returns nil on cancel) would start the same ordered shutdown.

### 8.4 Context and cancellation rules

Every method takes a `ctx` and passes it to `database/sql` (`BeginTx`, `ExecContext`, `QueryContext`), so an in-flight statement is interrupted at cancel. Rules: a cancelled or expired context returns `context.Canceled`/`DeadlineExceeded` unchanged and uncounted (shutdown must not degrade the store); `Close` and the probe use their own contexts (`Close(ctx)` from the stage; the probe from `Run`'s ctx); the engine passes a derived job context, and after `Grace` uses `context.WithoutCancel` for in-flight work, so a `PutItem` that is already running completes (one short transaction) while new work stops.

### 8.5 Multi-process access

The database is single-process. A second process on the same file (an operator's `sqlite3` shell, a second replica) is tolerated for **reads** (WAL) and produces `busy_timeout` waits for writes; two replicas writing one file are unsupported (Q-10), and `Restore` refuses to run while any other process holds the file.

---

## 9. Observability

Instruments are created from the injected `metric.Meter` (G11); names are constants next to their instruments (`sqlite/metrics.go`, `readmodel/publisher.go`); the `cmd/scp` metrics test boots both constructors against an in-memory reader and compares instrument names with the generated `docs/reference/metrics.md` (architecture 18.2). Units follow OTel conventions; labels are bounded.

### 9.1 Metrics owned

| Name | Type | Labels | Meaning |
|---|---|---|---|
| `scp_snapshot_publish_total` | counter | `result` = `ok` \| `invalid` \| `aborted` | publish attempts; `invalid` = projector or validator rejected (Permanent); `aborted` = `fn` returned an error such as `ErrUnknownRepo` (Deviation D8) |
| `scp_snapshot_publish_seconds` | histogram (s) | none | duration of one successful `Apply` (build, project, validate, swap); buckets 50us, 100us, 250us, 500us, 1ms, 2.5ms, 5ms, 10ms, 50ms, 250ms |
| `scp_snapshot_version` | observable gauge | none | current version |
| `scp_snapshot_age_seconds` | observable gauge (s) | none | seconds since the current snapshot was built (data age is the freshness model's, not this) |
| `scp_snapshot_repos` | observable gauge | none | entries in the current snapshot |
| `scp_store_state` | observable gauge | none | 0 ok, 1 degraded (writes suspended) |
| `scp_store_errors_total` | counter | `op` | store operations that failed with a SQLite error (context errors and typed store errors excluded) |
| `scp_store_rebuilds_total` | counter | `reason` = `corrupt` \| `not_a_database` \| `schema_newer` \| `migration_failed` \| `schema_drift` | starts from a quarantined database |
| `scp_store_tx_seconds` | histogram (s) | `op` | duration of one store call (one transaction); buckets 0.5ms to 5s |
| `scp_store_cache_bytes` | observable gauge (By) | `ns` | bytes held per cache namespace |
| `scp_store_cache_ops_total` | counter | `ns`, `result` = `hit` \| `miss` | `GetCache` lookups |
| `scp_store_cache_evictions_total` | counter | `ns` | entries evicted by the LRU cap |
| `scp_store_commits_trimmed_total` | counter | none | commit rows deleted by retention |
| `scp_store_pruned_total` | counter | `table` = `item` \| `cache` | rows deleted at open (kind or namespace no longer registered) |
| `scp_store_plan_rows_skipped_total` | counter | `reason` = `global` \| `no_item` | `SavePlans` rows not persisted: a global job kind (expected) or a row that matched no item (a growing `no_item` after every flush means a job-kind/item-kind mismatch, 2.5; alert on it) |
| `scp_store_db_bytes`, `scp_store_wal_bytes` | observable gauges (By) | none | file sizes (stat at scrape) |
| `scp_store_backup_total` | counter | `result` = `ok` \| `error` | backups |
| `scp_store_backup_seconds` | histogram (s) | none | duration of one backup |
| `scp_store_backup_age_seconds` | observable gauge (s) | none | seconds since the last successful backup; **absent** until the first one |

`op` values (about 25, bounded by code): `open`, `LoadCatalogue`, `LoadItems`, `LoadPlans`, `LoadCommits`, `ReplaceCatalogue`, `PutItem`, `Revalidated`, `RecordFailure`, `SavePlans`, `AppendCommits`, `SaveTask`, `LoadTasks`, `Park`, `Unpark`, `LoadParked`, `GetCache`, `PutCache`, `FlushTouches`, `Optimize`, `Check`, `Backup`, `probe`.

Suggested alerts (runbook, not code): `scp_store_state == 1` for 5 min; `increase(scp_store_rebuilds_total[1h]) > 0`; `scp_store_backup_age_seconds > 3 * interval` when backups are enabled; `histogram_quantile(0.99, scp_store_tx_seconds{op="PutItem"}) > 0.25`; `scp_snapshot_publish_total{result="invalid"}` rate > 0.

### 9.2 Logs (`slog`, injected logger, `component=store` or `component=readmodel`)

| Level | Message | Fields | When |
|---|---|---|---|
| INFO | `store open` | `path` (base name), `sqlite`, `schema`, `migrated`, `created` | end of a successful open |
| ERROR | `database unusable: quarantining and starting cold` | `reason`, `err`, `path` (the configured `store.path`: operator configuration, not row data) | before quarantine |
| ERROR | `store degraded: writes suspended` | `op`, `err`, `probe_interval` | first failure of a degrading kind (once per transition) |
| INFO | `store recovered: writes resumed` | none | probe succeeded |
| WARN | `store worker task failed` | `task` = `probe` \| `flush` \| `backup`, `err` | a worker task failed |
| WARN | `wal checkpoint could not truncate: a reader was active` | `log_frames`, `checkpointed` | at `Close` |
| ERROR | `snapshot candidate rejected` | `stage`, `index`, `err`, `snapshot_version` | projector or validator rejected |

Never logged: payloads, `Repo.Meta`, cache values, backup paths other than the configured one, anything from a row. Every call site uses the `...Context` slog variants (sloglint `context: all`); the prototype's calls do not pass a context yet and must be converted (known gap, section 13 task T-SQ-14).

### 9.3 Traces

| Span | Attributes | Notes |
|---|---|---|
| `store.tx` | `op` | one per store call (read or write); status `Error` on begin/fn/commit failure |
| `snapshot.apply` | `snapshot.version` (new), `snapshot.changed` (count of touched entries), `result` | architecture 18.3 asks for "version, changed repos"; the prototype starts the span without attributes (known gap, T-RM-8) |
| `store.backup` | `result`, `bytes` | proposed addition: backups are the one operation longer than a second |

Trace context propagates from the job context into the store call, so a `job.run` span shows its `store.tx` child.

---

## 10. Security

Trust boundaries: the database file and everything read back from it is **our own earlier output**, but the file lives on a volume an operator can touch, so rows are treated as untrusted bytes at the boundary where they could cause harm.

| Threat | Control |
|---|---|
| SQL injection | every value is a bound parameter; the only generated SQL is placeholder lists, two constant `PRAGMA` names and an integer (5.3); `VACUUM INTO ?` binds its path. `store.path` and the backup path come from validated configuration or the CLI, never from an HTTP request |
| DSN/pragma injection through a configurable path | `buildDSN` assembles a `file:` URI with `net/url`, so `?`, `#`, `%`, spaces and `&` in a path cannot add a parameter or a pragma; `sqlite.StrictPragmas(true)` additionally rejects a `_pragma` value that contains a second statement; `FuzzDSN` (1.06 million executions, 8 s, zero findings) asserts the parsed URL always has the original path and only the intended parameters |
| Hostile or hand-edited database (unsafe views/triggers, UDF abuse) | `trusted_schema=0` on every connection; `application_id` check refuses foreign databases; the schema fingerprint check quarantines drift; `quick_check` at start |
| Untrusted payload content (HTML, JSON) | the store never interprets payloads; **rendered HTML read back is re-sanitised** by `docs` through `safehtml` (ADR-005); payload decoders are the features' and bound by their own size limits; the store bounds every payload at 16 MiB (`MaxPayload`) and cache values at their namespace cap |
| Data exposure of private repositories | when private visibility is enabled the database holds private metadata and rendered documents. The file, its `-wal`, the backup and the quarantine sets have the same sensitivity; the deployment runs as a dedicated non-root user and mounts the volume `0700`; `copyFile` (restore) creates `0600`; SQLite creates the database and `VACUUM INTO` output with the process umask (the image sets `umask 077`, Q9). **View-class filtering (public versus all) is a snapshot-level concern** (`scm.Classed`, `authz.ClassOf`): the store holds the union and is never reachable from a request |
| Path traversal / overwrite | `Backup` refuses the live database as a target (`ErrBackupIsSource`, by cleaned path and by `os.SameFile`); `Restore` quarantines before overwriting and never deletes the old set; `Config.Validate` requires `backup.path != store.path` |
| Denial of service by disk | bounded retention everywhere (30 commits, one row per kind, per-namespace cache caps, `journal_size_limit`, 2 quarantine sets); a full disk degrades instead of crashing; `ErrTooLarge` before SQL |
| Poisoned restore | `Restore` verifies the backup (application id, schema version known, `quick_check`) before touching the live file |
| Resource exhaustion through the recency map | bounded at 8,192 entries (`touchMax`) |
| Secrets | none stored; nothing from tokens, webhook secrets or credentials ever reaches the store (features pass domain data only); logs never include row content |
| Cross-repository confusion by case | `RepoKey` is the folded key (`store.KeyOf` uses `RepoRef.KeyParts()`; `Validate` rejects ASCII upper-case), so `Acme/X` and `acme/x` can never become two rows or let a job write into the wrong repository (V-01) |
| Silent loss | `ON CONFLICT DO NOTHING` instead of `INSERT OR IGNORE`; every input is validated by shared code before SQL, so a constraint violation can never be swallowed or surface as a raw SQLite error that the memory store would not produce (V-03, V-04) |
| Supply chain | `modernc.org/sqlite` is imported only in `store/sqlite` (G6); versions pinned and verified 2026-10-14; the version guard fails closed on an old library; `govulncheck` and a dependency review run in CI (architecture 22) |

Open security point: file permissions of the database created by SQLite follow the umask (Q9); the design asks the image to set it and a start-up warning when the mode is wider than `0600`.

---

## 11. Guardrail and requirement trace

Legend: "GR-nn" is a guardrail of `02-design-guardrails.md`; "G11" without a hyphen is architecture rule G11 (telemetry injection); "Go review F-nn" is a finding of `30-verification/architecture-review-go.md`. Each row names the requirement, the mechanism in this unit that satisfies it, and the test that proves it.

| Item | Requirement | Where addressed here | Verified by |
|---|---|---|---|
| GR-07 | every entity has its own identity; keys are whole values in which every field takes part in equality, so forks and multi-group repositories can never collide; relations are indexes, never join rows built from cache keys | typed keys are whole values: `RepoKey` (comparable struct of three strings), `ItemKey`, entry ids are the unique slug; `UNIQUE(source,owner,name)`, `UNIQUE(slug)`, `UNIQUE(repo_id,sha)`; a commit is addressed by `(repo, sha)`, never by SHA alone; `Repo` is one row, items and commits are separate tables joined by `repo_id` (3.2, 5.2) | `Catalogue/*`, `Commits/ExistingSHAIsNotRewritten`, schema constraints exercised by `TestDifferentialAgainstMemory` |
| GR-08 | multi-group membership is modelled in full and is order-independent; no arbitrary "first group" | membership is not stored per group: one row per repository, groups are a snapshot global (`catalog.membership`) built by a projector from the existence slot (architecture 9.1); no "first group" notion exists in the store or kernel | `TestImmutabilitySharingAndRev`, projector/validator tests (12.2); the membership global itself is the catalog unit's |
| GR-26 | freshness is data; reconciliation is authoritative: every item carries `fetched_at`, ETag, status and last error; renamed and deleted repositories are removed; a failed fetch never silently keeps an old value | per-item `fetched_at`/`checked_at`/`attempts`/`error`, `RecordFailure` stale-while-error with a visible indicator (the snapshot keeps serving the last good value; `freshness.Info` exposes state, age and `ErrCode`), `ReplaceCatalogue` removes and renames, persisted `next_due_at`/`unchanged_streak`, `task_state`, `parked` so the planner resumes after a restart (4.4, 5.2); derivation of stale/error is in `freshness` at read time | `Item/*`, `Item/StaleWhileError`, `Plans/SaveLoadAndOwnership`, `Tasks/RoundTrip`, `Parked/*`, `Durable/EverythingSurvivesReopen` |
| GR-23 | upstream failures are typed and never collapsed into "not found" | out of scope for storage; the store records a **stable machine code** of the last failure in `item.error` and counts `attempts`, so classification (by the engine) survives restarts; `Failure.Code` is bounded and persisted, `Message` is not | `Item/StaleWhileError`, `Item/FailureWithoutGoodValueIsErrorState` |
| GR-24 | the job engine never loses work silently; permanent failures are not retried identically; scheduling is fixed-delay and "last run" means completed | `ErrInvalidSnapshot` is `Permanent`, `ErrUnknownRepo`/`ErrNoRepo` are `Gone` (structural behaviour interfaces) so the engine parks or drops instead of retrying; `parked` persists across restarts with `since`; the store worker uses fixed-delay timers that restart after completion; `task_state` records `last_attempt`/`last_success` on completion (written by the scheduler's `OnDone`) | `TestZombiePutIsRejectedAndIsGone`, `TestValidatorFailsClosedAndIsPermanent`, `Catalogue/RemovalCascadesItemsCommitsAndParkedKeys`, `TestWorkerTimingInSynctestBubble`, `Tasks/RoundTrip` |
| GR-28 | the read model is an immutable snapshot with write-time indexes: values are shared by pointer, never deep-copied on access; no whole-collection scans, parallel scans or per-request sorting | immutable values shared by pointer between snapshots; `Get` is a lookup plus type assertion; `Put` takes ownership (Go review F-22); derived indexes (latest N, membership) built by **projectors at publish time**; reverse lookups are map lookups; the store has only indexed statements | `TestImmutabilitySharingAndRev`, `TestConcurrentReadersDuringWrites`, `BenchmarkGetParallel`, `TestHotQueryPlansUseIndexes`, `BenchmarkApplyOneRepo1000`, model-based test |
| GR-29 | absence and unsupported input are typed states, never a cached `null` rendered as a blank 200 | typed result states `not_found`, `unsupported`, `too_large`, `no_content` are first-class `item.status` values with a CHECK that they carry `fetched_at`; `Revalidated`/`RecordFailure` preserve them; one `item` row per (repository, kind) with a typed state covers the README and every other document kind; releases arrive in the `catalog.stats` item | `Item/TypedStatesAreResults`, `Item/NonOKResultStateSurvivesFailureAndRevalidation`, `Item/*` |
| GR-30 | retention is bounded and measured: commits are evicted, caches are capped and counted | `CommitRow.CommittedAt` required (validated, never null); per-repository retention `keep` (latest 30) in the same transaction as the insert; deterministic order `(committed_at DESC, id DESC)`; `LoadCommits` returns plain slices (no stateful iterator); per-namespace cache caps with counters (`scp_store_commits_trimmed_total`, `scp_store_cache_evictions_total`), WAL and quarantine bounds | `Commits/*`, `Commits/AppendTrimOrderIdempotent`, `Cache/LRUEvictsLeastRecentlyUsedToNinetyPercent`, `TestHotQueryPlansUseIndexes` |
| GR-31 | restart is warm: no full re-fetch after every restart | the store persists last-good items, catalogue, commits, plans, tasks and parked keys; warm start publishes snapshot version 1 in one `Apply` (112-125 ms at 34,000 rows); `SavePlans` persists `next_due_at`/`unchanged_streak`, `LoadPlans` restores, `task_state` restores scheduler watermarks | `Durable/EverythingSurvivesReopen`, `TestWarmStartProtocol`, `BenchmarkWarmLoad34k`, `Plans/SaveLoadAndOwnership` |
| GR-02 | readiness is gated on data; the first sync is the sync engine's first run | warm load (4.6) gives a ready snapshot before any fetch when rows exist; cold start has `fetched_at` absent, so every item is due immediately and the planner releases them at upstream pace; `Open` quarantine path yields exactly that cold start | `TestQuarantineOn*`, `TestWarmStartProtocol` |
| GR-04 | nothing exists without a consumer; one abstraction per concern | one serving layer (the snapshot) and one restart layer (the store), no second cache layer; the only generic cache is `cache` with namespaces registered by `manifests` (a consumer exists), bounded and counted; no per-feature cache table; no TTL-cache dependency (ADR-002 point 5) | architecture 9; `Cache/*` |
| GR-12 | one typed, strict, validated configuration; every documented key has a consumer | every `store.*` key in section 6 has a consumer (the documented == consumed CI test); `cache_caps.locked` and `depsdev` are consumed by the `manifests` namespaces of unit 13 | the CI key test, `TestStoreConfigStrictDecode` |
| GR-41 | health derives from outcomes and exposes data age | store health is `scp_store_state` and `Info`; snapshot age is `scp_snapshot_age_seconds`; the header's tracked repository count is `Snapshot.Repos()` length (`scp_snapshot_repos`) with version/built-at, and the "last full sync" time is a `site.meta` global built from `task_state` / item times by `overview` (architecture 9.1) | `TestInfoAndPrune`, `readmodel` getters tested; the global is `overview`'s |
| GR-32 | trusted HTML is minted only by the sanitiser | the store never interprets or trusts a payload; rendered HTML read back is re-sanitised through `safehtml` (or re-rendered when the pipeline version differs) by `docs` before it enters the snapshot (4.6, section 10; architecture 9.3 chose re-sanitise-on-read over sanitise-on-write because the store holds bytes of an older pipeline version) | `docs` unit's warm-start tests; this unit: no payload parsing anywhere, 4.6 contract |
| GR-35 | personal data is minimised; access is an explicit policy | the store holds a payload the feature chose (`CommitRow.Payload` is the commits DTO without e-mails, unit 09; key `(repository, sha)`, UTC time, minimal fields); private-repository data in the file is protected by file mode and the `0700` volume (section 10, Q9); view-class filtering is the snapshot's (`scm.Classed`) | section 10, `Commits/*` |
| GR-38 | times are stored in UTC | all stored times are UTC unix seconds | `Item/PutAndLoadRoundTrip` |
| GR-03 | lifecycles are owned; graceful shutdown with a deadline | `Close(ctx)` order (flush, optimize, optional backup, checkpoint, close); persisted plans/tasks/parked; `Run` returns on cancel | `Lifecycle/*`, `TestWorkerTimingInSynctestBubble` |
| G11 (architecture rule) | telemetry through injected `Meter`/`Tracer`, API modules only, names as constants | `readmodel.Deps`, `sqlite.Deps`; no `otel` root import; no global accessors | archtest/depguard (M6); name test (T-IN-3) |
| Go review F-06 | one owner per column; a job costs one transaction | `PutItem`/`Revalidated`/`RecordFailure` carry no `nextDue`; `SavePlans` is the planner's only path (2.5) | `Plans/SaveLoadAndOwnership` |
| Go review F-08 | `Put` resurrects a removed repository | existence slot + `ErrUnknownRepo` (`Gone`) + built-in converse validator; `NoRepoError` in the store | `TestZombiePutIsRejectedAndIsGone`, `Item/UnknownRepoAndItem` |
| Go review F-17 | name collisions and identity sprawl | `readmodel.EntryID` (slug string); `store.RepoKey{Source, Owner, Name}`; `Item.State` is `freshness.State`; one conversion `store.KeyOf` with a test that `KeyOf(r).String() == r.Key()` | `TestKeyOfMatchesRepoRefKey`, rename tests |
| Go review F-20 | SQLite traps (which handle backs up, `GetCache` writes, read-only root, reentrancy, downgrade, store size) | dedicated `bk` connection; in-memory recency; `temp_store=MEMORY` (smoke job pending, Q1); reentrancy test; downgrade is quarantine and documented; carve-out interfaces | `Check/BackupSmoke`, `Reentrancy/*`, `TestSchemaNewerQuarantines` |
| Go review F-22 | immutability is a convention | rule stated (3.1) and tested with a model-based property test, a race test and (to write, T-RM-10) a mutating-reader test | 12.2, 12.3 |
| Go review F-25 | SSE machinery in v1 kernel | not built: only `Rev` (ETags) ships; ring, `Changed`, `Subscribe`, `ChangesSince` wait for v1.1 | n/a |
| Go review F-26 | publish storm back-pressure | measured 67-73 us per publish at 1,000 repositories; coalescing window documented and deferred (4.1.2) | `BenchmarkApplyOneRepo1000` |

---

## 12. Test plan

### 12.1 Conformance suite (`store/storetest.Run(t, Harness)`)

```go
type Config struct{ CacheNamespaces map[string]int64 }          // NSSmall "t.small" cap 1000 B; NSBig "t.big" cap 100000 B
type Harness struct {
	New     func(t *testing.T, cfg Config) store.Store           // a fresh, empty store per subtest
	Reopen  func(t *testing.T, s store.Store) store.Store        // close (Run already stopped) and open the same durable state; nil for non-durable
	Degrade func(t *testing.T, s store.Store)                    // force the degraded state; nil to skip
}
func Run(t *testing.T, h Harness)
```

It is imported only from `_test.go` files (its own tests may import nothing else of ours, 2.1) and run by `store/memory` (`Degrade` through `SetHealth`; no `Reopen`, so the `Durable` group is skipped), `store/sqlite` on modernc (all three hooks) and `store/sqlite` on ncruces (all three hooks, driver injected through the unexported seam). Subtests (every name is a requirement; **the suite is the specification**; 46 subtests in the prototype, the seven in bold being the verification's additions):

| Group | Subtests |
|---|---|
| Catalogue | `RoundTripOrderedBySlug`, `EmptyReplaceRemovesAll`, `RemovalCascadesItemsCommitsAndParkedKeys`, `RenameByGitHubIDKeepsRows`, `SwapNamesWithinOneTransaction`, `NameReusedByAnotherRepositoryIsANewRow`, `ValidationRejectsWholeCall`, **`MixedCaseKeyRejected`**, **`InvalidMetaRejectedBeforeAnyWrite`**, **`UnknownIncomingGitHubIDKeepsKnownOne`** |
| Item | `PutAndLoadRoundTrip`, `NilPayloadStaysNil`, `TypedStatesAreResults`, `InvalidInputsNeverReachTheStore`, `PutOverwritesAndResetsAttempts`, `StaleWhileError`, `FailureWithoutGoodValueIsErrorState`, `RevalidatedKeepsPayloadAndClearsFailure`, `NonOKResultStateSurvivesFailureAndRevalidation`, `UnknownRepoAndItem` |
| Plans | `SaveLoadAndOwnership` (SavePlans never touches payload; PutItem never touches `next_due_at`/`unchanged_streak`; global-kind rows and rows without items are skipped), **`NegativeStreakRejected`** |
| Commits | `AppendTrimOrderIdempotent`, `EqualTimestampsOrderByInsertion`, `ExistingSHAIsNotRewritten`, `InvalidKeep`, `UnknownRepoLoadsEmpty`, **`EmptyPayloadRejectedAndCallIsAtomic`** |
| Tasks, Parked | `Tasks/RoundTrip`, `Parked/ParkKeepsSinceUnparkDeletes` |
| Cache | `PutGetMissOverwrite`, `BoundsAndNamespaces`, `LRUEvictsLeastRecentlyUsedToNinetyPercent`, `NamespacesAreIndependent`, `OverwriteAdjustsAccounting`, **`NilValueRoundTripsAsEmpty`**, **`ConcurrentPutsStayWithinTheCap`** |
| Lifecycle | `RunReturnsOnCancelAndCloseIsIdempotent`, `CancelledContextIsNotAStoreFailure` |
| Operations | `Check/BackupSmoke` |
| Reentrancy and callbacks | `Reentrancy/CallbackThatCallsTheStoreDoesNotDeadlock`, `Callbacks/ErrorAbortsLoad` |
| Concurrency | `Concurrency/ParallelWritersAndReaders` (run under `-race`) |
| Degraded (when `Degrade` is set) | `Degraded/WritesRefusedReadsServed` |
| Durable (when `Reopen` is set) | `Durable/EverythingSurvivesReopen`, `Durable/CacheAccountingSurvivesReopen` |

### 12.2 Package tests

`readmodel` (prototype names; `T-RM-n` = still to write): `TestImmutabilitySharingAndRev` (unchanged entries are the same pointer across snapshots; `Rev` advances only for touched entries; the shared `Repos()` slice is identical while membership is unchanged); `TestZombiePutIsRejectedAndIsGone`; `TestValidatorFailsClosedAndIsPermanent` (rejected candidate leaves `Current()` unchanged and consumes no version); `TestRegistryMisuse` (each panic); `TestConcurrentReadersDuringWrites`; `TestModelBased` (random operation sequences against a plain-map model, comparing every observable after every `Apply`); `BenchmarkApplyOneRepo1000`, `BenchmarkGetParallel`; T-RM-8: `snapshot.apply` span attributes and ctx logging; **T-RM-9**: `Apply` with a blocking `fn` stalls a concurrent `Apply` only for the `fn`'s duration, and a CI lint (forbidigo on `database/sql`, `net/http`, `os` file I/O inside `readmodel`); **T-RM-10**: a `-race` test where readers iterate `Repos()` and read slot values of held snapshots while the publisher repeatedly adds and removes entries (the builder must allocate a new `names` slice whenever membership changes and must never write into a published entry), plus a `slices.Clone`-at-producer checklist item in every feature design (a producer that reuses a buffer after `Put` cannot be caught by a test of this unit, only by that feature's own `-race` test and review, F-22).

`store`: `TestKeyOfMatchesRepoRefKey`, **`TestKeyOfFoldsLikeRepoRefKey`** (mixed-case `Acme/Widget-Core` must give `src/acme/widget-core`, and a non-folded `RepoKey` must fail `Validate`; run against the **real** kernel `scm`), `TestValidKind`, `TestRepoKeyValidate`, `FuzzValidKind` (5 s: 4.37 million executions, no findings).

`store/memory`: `TestConformance`, `TestFailNext` (fault injection hook used by feature tests).

`store/sqlite` (modernc unless noted): `TestConformance`; `TestPragmasPerRole` (every pragma of 5.1 per role, read back through `PRAGMA`); `TestDSNEscapingFuzzSeeds` and `FuzzDSN`; `TestQuarantineOnGarbageFile`, `TestQuarantineOnPageCorruption` (flip bytes in a copy), `TestSchemaNewerQuarantines`, `TestSchemaDriftQuarantines`, `TestForeignDatabaseIsNeverTouched`, `TestMigrationFailureQuarantinesThenFatalOnFreshFailure`, `TestMigrationRunnerIsAtomicAndAdditive`, `TestFailedMigrationLeavesVersionUntouched`, `TestForeignKeyOffMigration`, `TestVersionGuardParse`, `TestOrphanWALIsNotReplayedIntoANewDatabase`; `TestBackupVerifyAndRestore`, `TestBackupOfCorruptCopyIsNotInstalled`, `TestCheckDetectsCorruptionAndDegrades`, `TestDiskFullDegradesAndProbeRecovers` (a real `SQLITE_FULL`: `PRAGMA max_page_count` is set to the current page count so the next growing write fails; lifting the cap and running the probe restores writes), `TestHotQueryPlansUseIndexes`, `TestWorkerTimingInSynctestBubble`, `TestInfoAndPrune`, **`TestCacheAccountingUnderConcurrentPuts`** (accounted bytes equal `SUM(size)` and never exceed the cap after 16 concurrent writers), **`TestItemStatusCheckMatchesFreshnessStates`**; `TestConformanceNcruces` and `TestNcrucesBackupAndCheck` (escape hatch); `TestOpenRejectsBadConfig`, `TestSchemaFilesAreContiguous`.

`cmd/scp` (prototype stand-ins for the real wiring tests; the only place where both implementations, the real `job` package and the YAML decoder may meet): `TestAssertionsCompile` (section 2.4, both implementations), `TestWarmStartProtocol` (4.6, store I/O before `Apply`), **`TestDifferentialAgainstMemory`** (8 seeds, 500 operations each, now including repository replacement/renames, tasks, parked keys, empty payloads, negative streaks and unknown repositories; errors compared by class, every load compared) moved here from `store/sqlite` (V-05), **`TestBehaviourInterfacesMatchRealJob`**, **`TestStoreConfigStrictDecode`**, and to write in `cmd/scp` (T-IN-8): `TestEveryRepoJobKindHasAPlanItemKind`, `TestKnownKindsAreItemKinds`. T-RM-11: `Rev` facts of 4.1.4.

### 12.3 Property, fuzz, race, synctest

- **Property:** `TestModelBased` (readmodel) and `TestDifferentialAgainstMemory` (store, in `cmd/scp`). Both draw their random sequence once from a seeded `math/rand/v2` PCG so a failure prints a reproducing seed.
- **Fuzz:** `FuzzDSN` (path to DSN; 8 s run: about 1.06 million executions, no findings), `FuzzValidKind` (5 s: 4.37 million), plus planned `FuzzConfigDecode` (strict YAML to `Config.Validate` never panics). Corpus seeds are committed under `testdata/fuzz`.
- **Race:** the whole module is tested with `-race` in CI; `Concurrency/ParallelWritersAndReaders`, `TestConcurrentReadersDuringWrites` and the worker test are written to fail under the race detector if a lock is missing.
- **synctest:** `TestWorkerTimingInSynctestBubble` drives `worker.run` with fake effects: the probe fires only while unhealthy, the flush every 5 minutes, the first backup after `firstBackupDelay` and the next after `interval` (completion-based). Store behaviour that needs time (`Revalidated(now)`, `Failure.At`, `Item.FetchedAt`) takes the time as a parameter, so no store test needs a fake clock.
- **goleak:** `goleak.VerifyTestMain` in `readmodel`, `store/sqlite` and `store/memory` (task T-SQ-13).

### 12.4 Fixtures

| Fixture | Source | Use |
|---|---|---|
| seed catalogue of N repositories (`repo-00`...), items, commits | generated in code (`storetest.seed`, `mkRepo`, `okItem`) with fixed UTC times (`2026-10-14T12:00:00Z`) | conformance and differential tests |
| corrupt database | a valid file with page bytes overwritten, produced in the test | quarantine path |
| garbage file | text bytes written to the path | `not_a_database` |
| newer-schema file | `PRAGMA user_version = len(ms)+1` on a valid file | `schema_newer` |
| drifted file | extra column or index added by hand | `schema_drift` |
| `testdata/v1.db` (to commit) | produced by the release's own migrations once per schema version, read-only | every future migration is tested against the previous release's real file (5.4) |
| large database | `BenchmarkWarmLoad34k`: 1,000 repositories x (30 commits of 300 B + 4 items of 25 KB) | warm-start timing |
| size-capped database | `PRAGMA max_page_count = <current page_count>` on the writer connection | a real `SQLITE_FULL` for the degraded and probe tests |

### 12.5 CI jobs and gates

| Job | Content | Gate |
|---|---|---|
| unit | `go vet`, `go test -race -count=1 ./internal/readmodel/... ./internal/store/...` | green |
| ncruces | same conformance suite on the escape-hatch driver | green (the escape hatch must stay usable) |
| coverage | `-cover` | readmodel >= 85%, memory >= 85%, sqlite >= 78% (measured 87.8% / 90.5% / 78.3% before the verification's additions; the uncovered sqlite code is mostly `fail` branches for driver errors that need fault injection) |
| fuzz smoke | each `Fuzz*` for 10 s | no new crasher |
| archtest | import matrix of 2.1, G5/G6/G11 | green |
| image smoke | run the container with `readOnlyRootFilesystem`, a writable `/data`, then assert: `Open` succeeds, a `VACUUM INTO` backup verifies, a query that sorts 50 MB succeeds (Q1) | green |
| bench | benchmarks of 12.6 | regression gate below |

### 12.6 Benchmarks and measured numbers

All measured 2026-10-14 on one developer machine (darwin/arm64, 16 logical CPUs, go1.27.1, SQLite 3.53.4 via modernc v1.60.1, database in a temp directory on local SSD, `-benchtime 500x` unless noted; **not** production hardware; the CI gate uses ratios, not absolutes).

| Benchmark | Measured | Gate (fail when worse than) |
|---|---|---|
| `BenchmarkPutItem25KB` | 45.9 us/op, 1,675 B, 48 allocs | 3x baseline |
| `BenchmarkAppendCommits30` | 80.7 us/op, 2,924 B, 75 allocs (30 inserts + trim) | 3x |
| `BenchmarkSavePlans1000` | 7.1 ms for 1,000 rows in one transaction (about 7 us/row) | 3x |
| `BenchmarkWarmLoad34k` | 125 ms per `Open` + full load of about 34,000 rows (112-125 ms over runs) | 2x |
| `BenchmarkApplyOneRepo1000` | 67-73 us/op, 107,336 B, 1,038 allocs (clone of a 1,000-entry map, one entry changed) | 3x time, 1.5x allocs |
| `BenchmarkGetParallel` | 0.48-0.93 ns/op | 5x |

Coverage (`go test -cover`, 2026-10-14): `readmodel` 87.8%, `store/memory` 90.5%, `store/sqlite` 78.3%. Suite wall time with `-race` after the additions (46 subtests, two drivers, the four-hundred-operation differential in `cmd/scp`): `store/sqlite` 35-80 s, `cmd/scp` about 6 s, `readmodel` about 1 s. Budget the `store/sqlite` job at 3 minutes in CI.

### 12.7 Known gaps of the prototype that the implementation closes

(1) `goleak` not yet in `TestMain`; (2) slog calls lack contexts; (3) `snapshot.apply` span has no attributes; (4) no `testdata/v1.db`; (5) *(closed by the verification: `TestStoreConfigStrictDecode`; the real `platform/config` decoder still has to be pointed at it in T-SQ-1)*; (6) the `Apply` non-blocking test and the mutating-reader test; (7) Linux read-only-root temp-file behaviour unverified (Q1); (8) the production switch to ncruces (Q2). Each is a task in section 13.

---

## 13. Implementation tasks (ordered, in milestones, with dependencies)

Size: S under half a day, M about one day, L two to three days. "Prototype" means the file of the same name in `03-readmodel-store.assets/`, which is the starting point (copy, rename imports to the real module path, replace the three kernel stubs with the real `scm`, `freshness`, `job`). Every task ends green on `go vet` and the tests named in it, with `-race`.

**M0: foundations (depends on the kernel unit delivering `scm`, `freshness`, `job` with the signatures of architecture 8.1-8.3)**

| Id | Task | Size | Depends |
|---|---|---|---|
| T-00 | Create the package directories of 1.4 with doc comments; add `modernc.org/sqlite` v1.60.1, OTel API v1.47.0 (`metric`, `trace`, `attribute`, `codes`, `metric/noop`, `trace/noop`) to `go.mod`; add the allowed-imports rows of 2.1 to archtest; ncruces v0.35.6 as a test-only dependency | S | kernel unit |

**M1: `readmodel` (no dependency but M0)**

| Id | Task | Size | Depends |
|---|---|---|---|
| T-RM-1 | `errors.go`, `registry.go` (+ `TestRegistryMisuse`) | S | T-00 |
| T-RM-2 | `snapshot.go`: `Get`, `GetGlobal`, `Has`, `Rev`, `Repos` | S | T-RM-1 |
| T-RM-3 | `builder.go`: `Put`, `PutGlobal`, `Remove`, `View`, copy-on-write bookkeeping | M | T-RM-2 |
| T-RM-4 | `publisher.go`: `Apply`, `Current`, `Status`, built-in existence validator, version stamping | M | T-RM-3 |
| T-RM-5 | publish metrics (counter, histogram, three observable gauges) | S | T-RM-4 |
| T-RM-6 | tests: immutability/sharing/rev, zombie `Put`, fail-closed validators | M | T-RM-4 |
| T-RM-7 | `TestModelBased`, `TestConcurrentReadersDuringWrites`, benchmarks, CI ratio gate | M | T-RM-4 |
| T-RM-8 | `snapshot.apply` span attributes (`snapshot.version`, `snapshot.changed`, `result`); slog calls with context | S | T-RM-4 |
| T-RM-9 | non-blocking `Apply` test and the forbidigo rule for I/O packages in `readmodel` | S | T-RM-4 |
| T-RM-10 | membership-change race test of 12.2 and the producer clone checklist for feature designs | S | T-RM-7 |
| T-RM-11 | `Rev` facts (4.1.4): a global change moves no `Rev`; a one-entry change moves no other `Rev`; plus a note in the `httpapi`/`overview` designs that composite resources use `Version()` | S | T-RM-4 |

**M2: store contract, memory implementation, conformance suite**

| Id | Task | Size | Depends |
|---|---|---|---|
| T-ST-1 | `store`: `store.go`, `key.go`, `errors.go`, `validate.go` (+ `TestKeyOfMatchesRepoRefKey`, `TestValidKind`, `TestRepoKeyValidate`, `FuzzValidKind`) | M | T-00 |
| T-ST-2 | `store/memory`: all methods, `FailNext`, `SetHealth`, `CacheBytes`, `Optimize` | M | T-ST-1 |
| T-ST-3 | `store/storetest`: the 46 subtests of 12.1 and the `Harness` | L | T-ST-1 |
| T-ST-4 | memory conformance + `TestFailNext`; fix the suite until the memory store passes | S | T-ST-2, T-ST-3 |

**M3: SQLite core: configuration, DSN, migrations, open and quarantine**

| Id | Task | Size | Depends |
|---|---|---|---|
| T-SQ-1 | `config.go`: `Config`, `ByteSize`, `BackupConfig`, `CacheCaps`, `DefaultConfig`, `Validate`; yaml decode test with the real `platform/config` strict decoder (closes gap 5 of 12.7); `FuzzConfigDecode` | M | T-00, config unit |
| T-SQ-2 | `dsn.go` with roles and `FuzzDSN`; `TestPragmasPerRole` | S | T-SQ-1 |
| T-SQ-3 | `driver.go` seam; Q2 decision (build-tag file `driver_ncruces.go` or documented switch) | S | T-SQ-2 |
| T-SQ-4 | `migrate.go` + `migrations/0001_init.sql` + loader/atomicity/FK-off tests | M | T-SQ-3 |
| T-SQ-5 | `open.go`: guards, `openOnce`, quarantine, orphan sidecars, fingerprint, `initAfterOpen`, `OpenReport` | L | T-SQ-4 |
| T-SQ-6 | `store.go`: `write`/`read` helpers, `fail` classification, degraded state, `unix`/`fromUnix` | M | T-SQ-5 |

**M4: SQLite data operations (each task runs the conformance subtests of its group on modernc, then adds its SQL tests)**

| Id | Task | Size | Depends |
|---|---|---|---|
| T-SQ-7 | `catalogue.go` (`LoadCatalogue`, `ReplaceCatalogue`) | M | T-SQ-6, T-ST-4 |
| T-SQ-8 | `items.go` (`LoadItems`, `LoadPlans`, `PutItem`, `Revalidated`, `RecordFailure`, `SavePlans`) | M | T-SQ-7 |
| T-SQ-9 | `commits.go` (`LoadCommits`, `AppendCommits`) + `TestHotQueryPlansUseIndexes` | S | T-SQ-7 |
| T-SQ-10 | `tasks.go` (`SaveTask`, `LoadTasks`, `Park`, `Unpark`, `LoadParked`) | S | T-SQ-7 |
| T-SQ-11 | `cache.go` (`GetCache`, `PutCache`, recency map, `FlushTouches`) | M | T-SQ-6 |
| T-SQ-12 | `TestDifferentialAgainstMemory` **in `cmd/scp`** (not in `store/sqlite`: same-rank import, 2.1) over every operation including `ReplaceCatalogue` renames, tasks, parked keys and invalid inputs; `TestCacheAccountingUnderConcurrentPuts`, `TestItemStatusCheckMatchesFreshnessStates` | M | T-SQ-7..11 |

**M5: operations, lifecycle, observability**

| Id | Task | Size | Depends |
|---|---|---|---|
| T-SQ-13 | `worker.go` (`Run`, `Close`, `probe`, `firstBackupDelay`), synctest test, `goleak` in `TestMain` | M | T-SQ-11 |
| T-SQ-14 | `metrics.go` (all instruments of 9.1, `Deps`), slog calls with context, `store.tx` and `store.backup` spans | M | T-SQ-6 |
| T-SQ-15 | `ops.go`: `Check`, `Backup`, `verifyFile`, `CheckFile`, `Restore`, `Optimize`, `Info`; backup/restore/corruption/disk-full tests | L | T-SQ-13 |
| T-SQ-16 | ncruces conformance and backup tests (`TestConformanceNcruces`, `TestNcrucesBackupAndCheck`); benchmarks and ratio gates | M | T-SQ-15 |

**M6: integration (mostly in `cmd/scp` and CI, listed here because this unit's acceptance depends on them)**

| Id | Task | Size | Depends |
|---|---|---|---|
| T-IN-1 | `cmd/scp/assert.go`: the carve-out assertions of 2.4 against the real consumer interfaces | S | features exist |
| T-IN-2 | warm-start wiring per 4.6 with the features' decoders; `TestWarmStart` on the real registry; readiness gate `warm` | M | T-RM-4, T-SQ-8, features |
| T-IN-3 | metrics-name test comparing instruments with `docs/reference/metrics.md` and checking the Prometheus-exposed names (Q8) | S | T-RM-5, T-SQ-14 |
| T-IN-4 | image smoke job (read-only root, `/data` writable, `VACUUM INTO` verify, 50 MB sort) (Q1) | M | T-SQ-15, image build |
| T-IN-5 | CI jobs of 12.5 (race, ncruces, coverage gates, fuzz smoke, archtest, bench ratios) | M | T-SQ-16 |
| T-IN-6 | scheduler tasks `store.optimize` (daily) and `store.check` (weekly quick_check) and the `scp store check/restore` verbs call this unit (Q10) | S | refresh unit, T-SQ-15 |
| T-IN-7 | `testdata/v1.db` generation tool and the migration-chain test (5.4); runbook text of 5.6 | S | T-SQ-4 |
| T-IN-8 | plan-kind wiring (2.5): features export their plan item kind; the `PlanSink` bridge maps job kind to item kind and back; `Deps.KnownKinds` = item kinds; `TestEveryRepoJobKindHasAPlanItemKind`, `TestKnownKindsAreItemKinds`; alert on `scp_store_plan_rows_skipped_total{reason="no_item"}`. **Blocks readiness of the sync engine integration (04 + 09)** | S | features, T-SQ-8 |

Critical path: T-00 -> T-ST-1 -> T-ST-3 -> T-SQ-5 -> T-SQ-6 -> T-SQ-7/8 -> T-SQ-12 -> T-SQ-15 -> T-IN-4. `readmodel` (M1) is independent of the store track and can be built in parallel; features need only M1 and M2 (memory store) to start.

Acceptance of the unit: the 46 conformance subtests green on memory, modernc and ncruces; all tests of 12.2 green with `-race`; coverage gates of 12.5; `archtest` green; the two benchmark gates; the image smoke job green; `docs/reference/metrics.md` generated and matching.

---

## 14. Deviations proposed

Each item diverges from, or adds to, `10-architecture.md`, ADR-002 or `04-decisions.md`. None is applied silently; all were implemented and tested in the prototype. Resolution requested from the architecture owner.

| Id | Where | Architecture says | This design does | Why |
|---|---|---|---|---|
| D1 | 8.4 `PutGlobal` | `func PutGlobal[T any](b *Builder, g Global[T], v T)` (no result) | returns `error` (`ErrNilValue` for a nil interface value) | symmetry with `Put`; a nil global would otherwise be stored as "present" and a later `GetGlobal` type assertion would report absence silently |
| D2 | 9.2 DDL `repo` | column `org` | column `owner` | `store.RepoKey` uses `Owner` since the ADR-002 amendment and Go review F-17; one name end to end (job keys `kind/source/owner/name`) |
| D3 | 9.2 DDL `cache` | `last_used INTEGER` (time) and index `cache_lru (ns, last_used)` | `use_seq INTEGER` (a logical recency tick, process counter seeded from `max(use_seq)`) and `cache_lru (ns, use_seq)` | deterministic LRU independent of wall time (G9, synctest), and two touches in one second still order |
| D4 | 9.2 DDL (additions) | no `application_id`, no CHECKs beyond `status`, no partial index | `PRAGMA application_id = 0x53435031`; `repo.meta CHECK json_valid`; `UNIQUE (source, github_id) WHERE github_id IS NOT NULL`; `item` CHECK "result states need `fetched_at`"; `attempts >= 0`, `unchanged_streak >= 0` | refuse foreign databases without touching them; make illegal rows unrepresentable; support rename-by-GitHub-id |
| D5 | 9.2 prose, failure semantics | "a failure updates only `status, error, checked_at, next_due_at, attempts`" | `RecordFailure` does not write `next_due_at` (planner-owned, per 8.5 and the ADR amendment) and changes `status` only when the row never had a good value | the 9.2 sentence predates the F-06 ownership split; 8.5 and the amendment are authoritative, this makes the prose consistent |
| D6 | 9.3 warm start | catalogue, items and commits loaded "in one read transaction" | each `Load*` call is its own read transaction | the contract has per-method calls; at startup no writer runs yet (the refresh stage starts after warm load), so the results are mutually consistent in practice; a single-transaction `Snapshot` read would need a different interface |
| D7 | 16 `store:` block | `path`, `backup.{enabled,interval}`, `cache_caps.{blob,parse,locked}` | adds `sync`, `busy_timeout`, `read_conns`, `journal_size_limit`, `quarantine_keep`, `probe_interval`, `backup.path`, `backup.on_shutdown` (all with the architecture's behaviour as the default) | every value that was a hard-coded constant in the architecture text (30 s probe, 2 read conns, 2 quarantine sets, 5000 ms busy timeout, shutdown backup) is an operator-visible knob with validation; documented == consumed test applies |
| D8 | 18.2 metrics | `scp_snapshot_publish_total{result=ok\|invalid}`; store list of five | adds `result="aborted"`; adds `scp_snapshot_repos`, `scp_store_cache_ops_total`, `scp_store_cache_evictions_total`, `scp_store_commits_trimmed_total`, `scp_store_pruned_total`, `scp_store_db_bytes`, `scp_store_wal_bytes`, `scp_store_backup_total`, `scp_store_backup_seconds`, `scp_store_backup_age_seconds` | an `fn` error (for example `ErrUnknownRepo`) is neither a success nor an invalid candidate and operators need to see zombie writes; the others are the eviction counters GR-30 requires and the backup alerting inputs |
| D9 | 8.4/8.5 API additions | listed API | adds `Snapshot.Has`, `Registry.SlotNames/GlobalNames`, `Publisher.Status`/`PublishStatus`, `ErrNilValue`, typed `UnknownRepoError`/`InvalidSnapshotError`; `store.ErrNoItem/ErrDegraded/ErrClosed/ErrInvalid/ErrUnknownNamespace/ErrTooLarge/ErrCorrupt`, `NoRepoError/InvalidError/CorruptError`, `ValidKind`, `Info`/`InfoProvider`/`Optimizer`, `sqlite.CheckFile/Restore/Open/OpenReport` | the architecture lists `ErrNoRepo` only; every typed outcome the carve-outs need is named, and `/status` needs `Status`/`Info` |
| D10 | 9.2 corruption policy | quarantine on failed quick_check, newer schema or failed migration | also quarantines on `schema_drift` and `not_a_database`; refuses (fatal, file untouched) a foreign `application_id` and an old SQLite library | a hand-edited or wrongly migrated file is as unusable as a corrupt one; renaming someone else's database or "fixing" a library problem by discarding data is wrong |
| D11 | 8.5 semantics (clarifications that bind implementers) | silent | `SavePlans` skips global-kind rows and rows with no item; `Revalidated` on an item with no good value is `ErrNoItem`; `RecordFailure` on a missing item creates an `error` row; `PutItem` refuses `error`/`never`; `LoadCommits` of an unknown repository is empty | each had to be decided to make the contract testable; see Q3 for the first |
| D12 | 9.2 `bk` spike item | "whether modernc accepts `mode=ro` URIs is an M0 spike item (fallback: a connection with neither `query_only` nor writes)" | resolved: modernc accepts `file:...?mode=ro`, `VACUUM INTO ?` with a bound parameter works on it, `temp_store=2` is accepted, `VACUUM INTO` on a `query_only=1` connection fails (V10 re-confirmed); no fallback needed | closes the spike; not a deviation but recorded here for the owner |
| D13 | 8.5 `Open` | constructor not specified | `sqlite.Open(ctx, cfg, Deps) (*Store, OpenReport, error)`; `Deps.CacheNamespaces` (names and caps registered at open) and `Deps.KnownKinds` (prune rows of kinds no compiled-in feature registers) | the cache needs its namespaces at construction (bounded, no unknown namespace); pruning removes the rows of a removed feature instead of carrying them forever (Q5) |
| D15 | 8.5 `Store` semantics (verification V-01, V-03, V-04) | silent on key case, empty commit payloads, JSON validity of `Meta`, negative streaks | `RepoKey` is the folded key (`KeyOf` from `RepoRef.KeyParts()`, `Validate` rejects ASCII upper-case); `AppendCommits` requires a non-empty payload and uses `ON CONFLICT DO NOTHING`; `ValidateRepo` requires `Meta` to be a JSON object; `ValidatePlan` rejects a negative `UnchangedStreak`; `PutCache(nil)` is an empty value | the kernel folds case (`RepoRef.Key`), and the first prototype let a mixed-case catalogue row and a folded job key address different repositories, dropped a NULL-payload commit silently and surfaced raw constraint errors on SQLite that the memory store accepted |
| D16 | 8.5/9.2 `item.kind` | `Kind` "owned by features" with examples that are not job kinds | the plan-kind rule of 2.5 (item kind vs job kind, bridge translation, `KnownKinds` = item kinds) and the counter `scp_store_plan_rows_skipped_total{reason}` | unit 09 uses item kind `commits.ring` for job kind `commits.refresh`; without the rule `SavePlans` persists nothing for commits and `KnownKinds` built from job kinds would prune `commits.ring` at every start |
| D17 | 16 `store:` block | `cache_caps.{blob,parse,locked}` | adds `cache_caps.depsdev` (16MiB, range 1MiB..256MiB) and fixes `locked` to `manifests.locked` | unit 13 RR-15 requires the key and says it is defined by this unit |
| D18 | 8.5 `IsResultState` (this document's first draft) | a store-local helper | removed: the kernel's `freshness.State.HasResult()` is the single definition, and the CHECK list is tested against `freshness.States()` | one definition of "result state"; a mutable package-level map also violated G5 |
| **P1** | 9.2 DDL (**proposal for the architecture owner, not applied**) | `next_due_at`/`unchanged_streak` columns on `item` | a separate `plan(repo_id, job_kind, next_due_at, unchanged_streak)` table keyed by the **job** kind, `ON DELETE CASCADE` from `repo`, written by `SavePlans` as an upsert | removes the whole job-kind/item-kind translation of 2.5, lets a job without an item row (or with several) persist its plan, and makes the planner's columns physically separate from the job's (one owner per table). Cost: one more table, one more write per flush (same transaction), a migration-free change now and a migration later. The design above is complete without it (rules 1-5 of 2.5); adopt P1 only if T-IN-8 proves unwieldy |
| D14 | 9.2 `Park` | signature `Park(ctx, key, reason, retryAt)` | `since` is stamped by the store on first park and kept on re-park (process clock) | the signature has no `since`; see 4.3 for why a clock read here is harmless to G9 |

---

## 15. Open questions

| Id | Question | Owner / needed by | Provisional answer in this design |
|---|---|---|---|
| Q1 | Does `temp_store=MEMORY` plus a read-only root filesystem (distroless, no `/tmp`) really keep `VACUUM INTO`, sorts and the WAL index off the root filesystem on Linux? macOS passed; the image has not been built | image/CI owner, before M6 acceptance (T-IN-4) | assume yes; the smoke job is the proof and gates acceptance. The WAL `-shm` file lives next to the database, so `/data` must be writable and local |
| Q2 | How is the escape-hatch driver selected in production? The prototype injects it through an unexported test seam (`Deps.drv`) so the ncruces dependency is test-only | architecture owner | a build tag `scp_ncruces` selecting a second `driver_ncruces.go` (and `go.mod` carrying both); default build links modernc only. Switching costs a rebuild, which matches ADR-002's exit condition (switch only if modernc blocks a Go upgrade for two weeks) |
| Q3 | Should `SavePlans` persist global-kind plan state (zero `RepoKey`)? Global jobs (`catalog.reconcile`, `catalog.heads`) have no repository row to hang an `item` on | refresh unit (04) | no: they are due at every start and `task_state` already carries their last attempt/success; the bridge filters or the store skips them silently (current behaviour) |
| Q4 | A successful probe clears `degraded` even when the cause was corruption reported by `Check`. Should corruption latch until restart? | architecture owner | not latched in v1: the next `Check` re-flags, and a restart quarantines. Latching would turn a transient `quick_check` read error into a permanent write outage |
| Q5 | `Deps.KnownKinds` deletes rows of kinds not registered. Should kinds of features that are merely **disabled by configuration** be kept? | catalog/manifests/integrations owners; `cmd/scp` | `cmd/scp` passes every **compiled-in** kind, not only enabled ones, so toggling `manifests.enabled` does not erase its data; only code removal prunes |
| Q6 | What is `store.cache_caps.locked` (default 64MiB) for? | architecture owner / manifests unit | **resolved by unit 13**: `manifests.locked` (locked components, key `repoKey\|unit\|treeSHA`, hashed when long) and `manifests.depsdev` (`store.cache_caps.depsdev`); the store still accepts any number of namespaces registered through `Deps.CacheNamespaces` |
| Q7 | Disk sizing: the database size at 1,000 repositories is unmeasured; backups need about one extra database size | operations | the runbook recommends a volume of 3x the database plus 192 MiB; `Info.DBBytes` is logged by the image smoke job to record the first real number |
| Q8 | Do the OTel Prometheus exporter's unit/suffix rules produce exactly the metric names of 9.1 (for example `scp_store_cache_bytes` with unit `By`, `scp_store_tx_seconds` with unit `s`)? | `platform/telemetry`, T-IN-3 | assumed yes (no double suffix when the name already ends with the unit); the metrics-name test checks the **scraped** names, not just the instrument names |
| Q9 | File permissions: SQLite creates the database with the process umask; private-repository data may be present | image owner | the image sets `umask 077` and mounts `/data` at `0700`; the store logs a WARN at open when the database mode is wider than `0600` (not in the prototype) |
| Q10 | Who schedules `store.optimize` (daily) and a periodic `store.check` (weekly `quick_check`), and what is the operator-visible effect of a failing check? | refresh/scheduler unit and `cmd/scp` | `cmd/scp` registers both as local scheduler tasks; a failing check flips the store to `degraded` (writes stop) and `/status` shows the corruption message; the operator restarts to quarantine and rebuild |
| Q12 | Which side owns the job-kind to item-kind table of 2.5: each feature (an exported `ItemKind` next to its `Jobs()`), or `cmd/scp` by hand? And is proposal P1 (a `plan` table) preferred? | architecture owner, units 04, 09, 14 and the bridge unit | features export an item kind per per-repository job (unit 09 already does: `commits.ItemKind = "commits.ring"`); the `PlanSink` bridge translates; P1 stays a proposal. **Unresolved across units until T-IN-8 lands** |
| Q13 | Should the readmodel offer a per-global revision (`GlobalRev(g)`) so composite resources can build precise ETags instead of falling back to `Version()`? | `httpapi` / `overview` owners | no: composite resources use `Version()` (4.1.4); revisit if the 304 hit rate of the composite resource is measured to matter |
| Q11 | Should `Apply` coalesce publishes during a cold-start storm now? | architecture owner | no: 67-73 us per publish at 1,000 repositories; revisit with the 10,000-repository exit condition or if a profile shows the publish mutex hot |


---

## 16. Verification log (independent verifier, 2026-10-14)

Scope: conformance with `10-architecture.md` and the ADRs, completeness of the 15 sections, guardrail traceability, technical correctness (compiled and run), testability, security and lifecycle, buildability. The full findings report is `30-verification/design-03-readmodel-store.md`; this section is the log of what was changed in this document and in `03-readmodel-store.assets/`.

**Method.** (1) Copied the prototype to a scratch module and ran `go vet` and `go test -race` (green before any change). (2) Replaced the three hand-written kernel stubs by the real `scm`, `freshness`, `job` packages of `01-kernel.assets` and re-ran: compiles, existing tests green, which proved that the signatures of this unit fit the real kernel, and exposed V-01. (3) `go list` of every package and test import against the matrix of architecture 5.2 (found V-05). (4) Wrote probing tests for every hypothesis found by reading the code and kept them as permanent tests. (5) Verified library and platform facts on the live web / module proxy (below). (6) Ported all fixes into the assets and re-ran `go vet ./...` and `go test -count=1 -race ./...`: green on go1.27.1 darwin/arm64 (46 conformance subtests on memory, modernc and ncruces; `store/sqlite` 80 s under `-race`).

| Id | Severity | Finding | Evidence | Resolution |
|---|---|---|---|---|
| V-01 | high | `store.KeyOf` copied `Owner`/`Name` unfolded, while the kernel's `RepoRef.Key()` and every job-key tail are ASCII-lower-cased: `KeyOf(Acme/Widget-Core)` = `gh/Acme/Widget-Core` vs `gh/acme/widget-core`; every job write for a mixed-case repository would have returned `NoRepoError` (`Gone`) and been dropped. The prototype stub did not fold, so `TestKeyOfMatchesRepoRefKey` passed vacuously | scratch test against the real kernel failed with `KeyOf "gh/Acme/Widget-Core" != RepoRef.Key "gh/acme/widget-core"` | `KeyOf` uses `KeyParts()`; `RepoKey.Validate` rejects ASCII upper-case; new `TestKeyOfFoldsLikeRepoRefKey`, `Catalogue/MixedCaseKeyRejected`; D15 |
| V-02 | high (cross-unit) | `ItemKey.Kind` was documented as "the `job.Spec.Kind` whose plan the row carries", but unit 09 stores commits under item kind `commits.ring` for job kind `commits.refresh`, and architecture 8.5 itself lists item kinds that are not job kinds. `SavePlans` matches `item.kind` and skips silently, so commit plans were never persisted (every restart would re-fetch), and `KnownKinds` built from job kinds would delete `commits.ring` at every start | `grep` of `09-commits-badges.md` (`ItemKind = "commits.ring"`, `JobKind = "commits.refresh"`); `SavePlans` code (`RowsAffected` ignored) | plan-kind rule (2.5), `scp_store_plan_rows_skipped_total`, T-IN-8, D16, proposal P1, Q12. **Not closed**: needs units 04/09/bridge to confirm |
| V-03 | medium | `INSERT OR IGNORE INTO commit_log` swallowed a NOT NULL violation: a commit with a nil payload returned `nil` and was not stored on SQLite but was stored by the memory store | scratch test: `append err=<nil> loaded=0` | `ValidateCommit` (payload required), `ON CONFLICT DO NOTHING`, memory validates before mutating; `Commits/EmptyPayloadRejectedAndCallIsAtomic` |
| V-04 | medium | The memory store accepted inputs SQLite rejected with raw constraint errors, breaking contract rule 6: `PutCache(nil)` (NOT NULL), `SavePlans` with a negative streak (CHECK), `Repo.Meta` that is not JSON (CHECK) | scratch test printed the SQLite constraint errors and `<nil>` for memory | `ValidatePlan`, `ValidateRepo` JSON-object check, nil cache value normalised; three new conformance subtests |
| V-05 | medium | Test files broke architecture 5.2: `store/sqlite` tests imported `store/memory` (same rank 2), `readmodel` tests imported `job`; the archtest would have failed on first run | `go list -test` import dump | differential test moved to `cmd/scp`; readmodel test restates the behaviour interfaces; rule added to 2.1 |
| V-06 | low | `PutCache` updates the byte accounting in a post-commit callback after the single connection is released, so a second put could read stale accounting (lost update, cap enforcement drifts until restart). Not reproduced (16 writers x 300 puts x 3 rounds) but reachable under scheduling delay (`manifests` parses with 4 goroutines) | code reading, stress test | `putMu` serialises `PutCache`; `TestCacheAccountingUnderConcurrentPuts`, `Cache/ConcurrentPutsStayWithinTheCap` |
| V-07 | medium | `Snapshot.Rev(id)` as the per-repository ETag is stale-prone for composite resources that read a global or another entry (membership, graph, aliases): a `304` with an old body | reasoning over architecture 6.1/9.1 and unit 06 section 3.3 | rule 4.1.4, T-RM-11, Q13. Enforcement belongs to `httpapi`/`overview` (their `ETagCoversBody` test) |
| V-08 | medium (cross-unit) | `store.cache_caps.depsdev` is required by unit 13 (RR-15 "defined here") and `locked` had no owner (Q6, would fail the documented == consumed test) | `grep` unit 13 | `depsdev` added (16MiB, 1MiB..256MiB), `locked` = `manifests.locked`, Q6 resolved, D17 |
| V-09 | low | The trace row for GR-07 (keys are whole values) described the composite-key hazard imprecisely: it must say that every field of a key takes part in equality, not single out which field a key might omit | review of section 11 against `02-design-guardrails.md` | row corrected |
| V-10 | low | Trace table lacked guardrails that concern this unit: GR-04, GR-07, GR-12, GR-24, GR-32, GR-35, GR-38, GR-41 | review against `02-design-guardrails.md` | rows added with mechanism and test |
| V-11 | low | `store.IsResultState` duplicated the kernel's `State.HasResult()` using a mutable package-level map (G5) and the CHECK list was not tied to `freshness.States()` | kernel `freshness.go` | helper removed (D18), `TestItemStatusCheckMatchesFreshnessStates` |
| V-12 | medium | The prototype warm-start test (the "executable form" of 4.6) called `LoadCatalogue`/`LoadItems` inside `Apply`'s `fn`, contradicting "`fn` MUST NOT do I/O"; and collecting raw payloads before decoding doubles the start-up memory peak | `warm_test.go` | test reordered; 4.6 requires decoding inside the `LoadItems` callback and sizing `GOMEMLIMIT` for the peak |
| V-13 | low | `ReplaceCatalogue` erased a known GitHub id when the incoming row had id 0 (rename tracking lost afterwards) | `UPDATE ... github_id=?5` | `coalesce(?5, github_id)` on both implementations; `Catalogue/UnknownIncomingGitHubIDKeepsKnownOne` |
| V-14 | low | `ByteSize` overflow wrapped (`9999999999GiB` decoded to a negative number, other values could wrap into the valid range) | scratch YAML test | overflow rejected; `TestStoreConfigStrictDecode` |
| V-15 | low | Missing pin notes: `modernc.org/libc` must stay at the version in modernc's own `go.mod` (v1.77.1; v1.78.0 already published); architecture pins Go 1.27.2, prototype ran 1.27.1 | `proxy.golang.org` | note in 5.1 |
| V-16 | low | `TestDifferentialAgainstMemory` never exercised `ReplaceCatalogue` after the first call, tasks, parked keys, empty payloads, invalid inputs; its error comparison contained dead code (`&& false`) | code reading | rewritten (8 seeds x 500 operations) |
| V-17 | low | Wording and notes: the ERROR log carries the configured full path (9.2 said paths are never logged); parked keys of a renamed repository are not rewritten; an empty `ReplaceCatalogue` argument cascades by contract and needs the caller's guard; cache keys longer than 512 bytes | code reading | notes added to 9.2, 4.4, 4.5, 6 |
| V-18 | info | The prototype used kernel stubs and therefore could not detect V-01 and could not prove signature compatibility | stub `scm.go` | prototype now carries the real kernel packages; README updated |

**External facts re-verified 2026-10-14.** `proxy.golang.org`: `modernc.org/sqlite` v1.60.1 (2026-09-29, requires libc v1.77.1), `github.com/ncruces/go-sqlite3` v0.35.6 (2026-09-23), `go.opentelemetry.io/otel`, `/metric`, `/trace` v1.47.0 (2026-10-02), `modernc.org/libc` latest v1.78.0, `go.uber.org/goleak` v1.3.0 (2023-10-24, still latest). `pkg.go.dev` lists `StrictPragmas(on)` and `StrictPragmasEnabled()` in v1.60.1. `sqlite.org/wal.html`: the WAL-reset bug is "likely present in all version of SQLite from 3.7.0 (2010-07-21) through 3.51.2 (2026-01-09)", fixed in 3.51.3 (2026-03-13) with backports 3.44.6 and 3.50.7: the version guard `>= 3.51.3` is right (a binary built from a 3.44 or 3.50 line is refused although patched; acceptable, modernc ships 3.53.4). `go.yaml.in/yaml/v3` v3.0.5 decodes `time.Duration` and `encoding.TextUnmarshaler` values under `KnownFields(true)`.

**Not verifiable here (still open):** Q1 (Linux read-only root with `temp_store=MEMORY`: only macOS was run), the measured numbers of 12.6 on production hardware, the real `platform/config` decoder (unit 02 does not exist yet), and the behaviour of the Prometheus exporter name mapping (Q8). The numbers quoted from `05-research/05` (52 MB, 0.31 ms) were not re-measured.

## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Changed: `Snapshot.Epoch()` and `Deps.Epoch` added to the code and a test (CD-IF-03; used by 07 D-13, 16 Q16-5).
- Closed: Q12 job kind versus item kind (CD-JB-01); the bridge `PlanSink` translates and `item.next_due_at` stays on `item`.
- Not adopted: P1 plan table (CD-JB-01, 93).
- Requirement from 15: the change feed (`Subscribe`, `ChangesSince`, `ChangeSet`) stays deferred to v1.1; `live` defines its own `ChangeFeed` and `live.ChangeSet` until then (CD-IF-02).
- Slot names registered by the features are listed in architecture 9.1 (CD-SL-01).
