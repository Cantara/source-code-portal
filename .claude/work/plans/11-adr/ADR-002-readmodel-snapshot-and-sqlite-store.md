# ADR-002: Immutable read model (`readmodel`) and SQLite store (`store`)

Status: Accepted. Date: 2026-10-14. Refines 04 D-02 (A-02).

## Context
D-02 asks for an immutable `Snapshot` behind `atomic.Pointer` plus durable last-good state in embedded SQLite behind a narrow `Store`, never on the request path. A concrete `Snapshot{Commits, Readme, ...}` would force the snapshot package to import feature types (cycle) or move domain types into a shared bucket. State held only in memory forces a full re-fetch after every restart, which wastes the upstream budget and delays readiness, so restart must be warm (GR-31).

## Decision
1. `internal/readmodel` is feature-agnostic: typed `Slot[T]`/`Global[T]` handles registered once in `cmd/scp` from a `Registry` that is then frozen; copy-on-write `Builder`; `Projector`s derive cross-repo indexes (latest-N, membership); `Validator`s gate publication (fail closed to the last good snapshot); `Publisher` holds `atomic.Pointer[Snapshot]`. `Put` takes ownership of its value and returns `ErrUnknownRepo` for a repository without a value in the designated **existence slot** (`catalog.repo`), and the registry installs the converse validator (every entry has an existence value), so a late job cannot resurrect a removed repository (F-08). The 256-entry change ring, `Changed()`, `Subscribe()` (capacity-1 coalescing channel) and `ChangesSince` ship with SSE in v1.1; v1 needs only `Current()`, `Version()` and `Rev()` (F-25).
2. Handlers call `Current()` once per request. Long-lived goroutines hold a version number, never a `*Snapshot`. Staleness is derived at read time (`freshness.Info.Derive(now)`), never published by a timer.
3. `internal/store` is the contract (`Store` interface, items, commits, tasks, parked keys, a generic bounded `cache`); `store/sqlite` implements it with `modernc.org/sqlite` v1.60.1 (SQLite 3.53.4, CGO-free): WAL, one `rw` connection with `_txlock=immediate`, a 2-connection `ro` pool with `query_only`, a dedicated read-only `bk` connection for `Backup`/`Check` (`VACUUM INTO` fails on a `query_only=1` connection, verified locally 2026-10-14), `temp_store=MEMORY`, `synchronous=NORMAL`, `busy_timeout=5000`, `foreign_keys=1`, `trusted_schema=0`, `StrictPragmas`, version guard `>= 3.51.3`, `payload` last, UTC unix seconds written by the application, own embedded migration runner on `PRAGMA user_version`, quarantine-and-rebuild on corruption, `VACUUM INTO` backup.
4. The snapshot is authoritative for serving; the store is authoritative for restart; divergence is allowed (a degraded store never blocks publishing).
5. `ncruces/go-sqlite3` v0.35.6 is the tested escape hatch (the `storetest` suite runs on both). bbolt and Postgres are documented alternatives; Redis/Valkey are out of scope; the otter TTL cache is not adopted (no profiling evidence).

## Alternatives considered
bbolt (no SQL for retention queries, bucket design per feature); mattn/go-sqlite3 (cgo, violates D-11); goose/Atlas migrations (a 60-line runner suffices because "down" is deleting the cache file; Atlas has paid features); one Snapshot struct with concrete fields (cycle); per-feature caches (no atomic cross-feature view, GR-28).

## Consequences
Warm start is about 50 ms for 34,000 rows; a 1,000-repository snapshot is about 52 MB with 0.12 MB per additional snapshot; the store is a rebuildable cache, so backups are an optimisation. Multi-replica is not precluded (another `Store` implementation) but is not built (Q-10). Features own their payload codecs and put a schema field inside the payload so payload evolution needs no SQL migration.

## Evidence
`05-research/05-store-sync-engine.md` sections 3 (read model, measurements M1), 4 (schema, pragmas, corruption policy, M2/M3/M5-M7), 9 (deviations); https://pkg.go.dev/modernc.org/sqlite (v1.60.1, SQLite 3.53.4, verified 2026-10-14).

## Exit condition
Switch driver if modernc blocks a Go upgrade for more than two weeks; revisit snapshot granularity above about 10,000 repositories or 100 swaps per second.

## Amendment 2026-10-14 (review revision)

The store no longer has two writers for the same column: the planner alone writes `next_due_at` and `unchanged_streak` (`SavePlans`, batched); jobs write payload, validators and `attempts` (`PutItem`, `Revalidated`, `RecordFailure` lose their `nextDue` parameters), so a job costs one transaction (F-06). `store.RepoKey` uses `Owner` (not `Org`), `Item.State` is `freshness.State`, and `store.KeyOf(scm.RepoRef)` is the single conversion (F-17). `LoadItems`/`LoadPlans` callbacks and transactions never call another `Store` method (single-writer deadlock rule, F-20). `GetCache` touches `last_used` in memory (approximate LRU). Rendered HTML read back from the store is untrusted bytes and is re-sanitised (ADR-005). An application rollback against a newer schema is a full cold start; migrations are written additive where possible and the runbook says so.
