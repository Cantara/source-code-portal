# ADR-003: Sync engine: build in-house (`internal/refresh`), level-triggered, tested with synctest

Status: Accepted. Date: 2026-10-14. Refines 04 D-03; renames `internal/sync` to `refresh`; drops `platform/clock` (A-03).

## Context
D-03 wants keyed coalescing, per-upstream limits, jittered backoff, rate-limit pause and readiness gating, with features registering jobs. Candidates: River (Postgres), Asynq/Machinery/Faktory (Redis/brokers), Temporal (cluster), `go-co-op/gocron` (scheduler only), `riverqueue` duplicate rules, plain `errgroup` plus tickers. `sync` collides with the standard library package name.

## Decision
Build a small engine (about 600 lines plus tests):
- Keyed table with states queued/delayed/running/parked. Enqueue while queued coalesces (promote time, raise priority, never demote); enqueue while running marks `dirty` and runs exactly one follow-up; a parked key is ignored unless forced.
- One ready heap and one delayed heap per upstream with `MaxConcurrent` and `Pace`; priority classes interactive, webhook, scheduled, backfill; github defaults 4 and 100 ms.
- Failure classification by behaviour interface (`job.RateLimited`, `job.Permanent`); rate-limit pauses the whole upstream until reset without consuming an attempt; permanent errors and recovered panics park the key; transient errors use full-jitter backoff with a floor (`Base + U[0, min(Cap, Base<<n) - Base]`, 2 s / 15 min / 8 attempts, then keep retrying at the cap).
- Level-triggered `Planner` (state of record is `next_due_at` per item; webhooks and head sweeps only accelerate) with idle back-off after consecutive unchanged outcomes, tracking both per-repository and **global** job kinds (`catalog.reconcile`, `catalog.heads`, integration sweeps, `webhook.redelivery`, `manifests.graph`), so every upstream call runs inside the engine; a fixed-delay jittered `Scheduler` for **local housekeeping only** (`planner.tick`, `store.optimize`, `pages.reload`) with skip-if-running and completion-time bookkeeping; a budget `Gate` (`Admit(upstream, prio)`) implemented by the GitHub adapter.
- No persisted job log and no persisted webhook intents: the queue is re-derived at start from persisted `next_due_at`.
- Shutdown: dispatch stops at context cancel, in-flight jobs keep a `Grace` (15 s) via `context.WithoutCancel`, then are cancelled; `Run` returns after the drain.
- Time is the process clock; tests run in `testing/synctest` bubbles with an in-memory fake store and `httptest.NewTestServer`. No `Clock` interface. Randomness takes a seed.
- Package name `refresh`; keys are strings; `refresh` imports only `job`, `freshness`, `platform/logx` (G8).

## Alternatives considered
River/Asynq (external services, violate single-binary); Temporal (a cluster); gocron (no keyed coalescing, no per-upstream pacing); a persisted outbox (adds a durable store for something GitHub and the planner already repair within the TTL).

## Consequences
We own about 600 lines of concurrency code, mitigated by the state-by-event table tests, a proposed model-based fuzz, `-race -count=30` runs and goleak. No cluster semantics; a crash between 202 and processing loses an accelerator, repaired by the planner within the TTL (risk W12).

## Evidence
`05-research/05-store-sync-engine.md` sections 5 (design, 10 synctest tests passing under `-race -count=30`, M4), 8 and 9; Go 1.27 release notes (`synctest.Sleep`, `httptest.NewTestServer`): https://go.dev/doc/go1.27; Go release dates verified 2026-10-14 at https://go.dev/doc/devel/release.

## Exit condition
Adopt a library only if it provides keyed coalescing with dirty-while-running, per-upstream pacing and in-process operation, or if multi-replica becomes a requirement.

## Amendment 2026-10-14 (review revision)

Contract corrections found by the Go review and confirmed against the prototype (F-05, F-06, F-07, F-16, F-21): (1) no hook (`OnParked`, `OnExhausted`, the planner's `Observe`) is ever invoked with the engine mutex held; the prototype's `finish()` does call `OnParked` under `e.mu` (read in `engine.go`), which makes a re-entering hook deadlock; (2) `job.Spec.Timeout` with a per-upstream default, overrun and ignored-cancel counters, and `/livez` failing when every slot of an upstream has been stuck for 2 minutes; (3) `Enqueue` returns `job.ErrClosed` after stop and `job.ErrQueueFull` at the key cap; (4) the planner copies due keys under its lock and enqueues after releasing it, marks a key `inflight` when it enqueues so a job spanning a tick boundary does not run twice, and takes a `job.Enqueuer` (not the engine), with `engine.Observe(planner.OnResult)` called once before `Run`; (5) a `Gone` error class drops a key for a vanished subject; (6) the `Scheduler` is frozen at `Run` and each task owns its `*rand.Rand`. The `Planner`, `Gate`, `Timeout` and lifecycle `Plan` are specified but not prototyped (risk W22); the M0 milestone builds them with the test list in architecture section 22.
