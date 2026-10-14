# ADR-024: Cross-feature communication: snapshot, consumer interfaces, job specs, events

Status: Accepted. Date: 2026-10-14. Elaborates 04 principle 2 and D-03 (A-01).

## Context
Features must cooperate (a push should refresh commits, docs and manifests; badges show commits and manifests data; the dashboard composes everything) without importing each other, and the engine must not know GitHub or features.

## Decision
Exactly four mechanisms (architecture section 6):
1. **Snapshot**: features exchange data through `readmodel` slots; data that more than one feature reads without importing the owner uses kernel types (`scm.Repository`, `scm.RepoStats`, `scm.Membership`) wrapped in `freshness.Value[T]`; owner-typed data is read only by `overview` or a bridge.
2. **Consumer-defined interfaces**: signatures use stdlib and kernel types only; adapters satisfy them structurally; feature-to-feature bridges live in `cmd/scp/internal/bridge` (wiring, not a feature).
3. **Job registration**: `job.Spec{Kind, Upstream, Scope, Prio, Policy, Run}` exposed by `Jobs()`; `cmd/scp` registers with `refresh`; features see only `job.Enqueuer`.
4. **Events**: webhooks and head-sweep results are normalised into `scm.Event`; each feature supplies a reaction method; `cmd/scp` wires one `reactor.Fan` as the webhook `Sink` and the catalog's `EventSink`. Events are accelerators; the planner is the level-triggered backstop.
`job` stays at rank 1 (imports `freshness` only) and knows nothing about events. Reactors are plain `func(ctx, scm.Event, job.Enqueuer) error` methods of the features; the new rank-2 package `reactor` (imports `job` and `scm`) holds `reactor.Func` and `reactor.Fan`, so `refresh` and `scm` still never meet.

## Alternatives considered
A shared "models" package (the bucket the owner rejected), an in-process event bus with dynamic subscriptions (hidden coupling, ordering and error semantics unclear), feature-to-feature calls through exported services (cycles), a global registry (GR-03).

## Consequences
Adding a feature is additive: a slot, a `Jobs()`, a `React`, a `Mount`, and wiring lines in `cmd/scp`. The cost is some bridging code in `overview` and the discipline to keep kernel types small.

## Evidence
`05-research/04-go-architecture-patterns.md` sections 5.2-5.3, 5.10-5.11; `05-research/05-store-sync-engine.md` sections 3.1, 5.2, 5.10.

## Amendment 2026-10-14 (review revision)

Go review F-02: the earlier `FanOut[E](rs ...Reactor[E]) func(ctx, ev E) error` had no `Enqueuer` to pass to its reactors and returned a bare func where the consumer interface wanted a `Handle` method, and the generic existed only to avoid an import the rank rule already allowed. Now: `Reactor` returns an `error` so an enqueue refusal (`ErrQueueFull`, `ErrClosed`) reaches the webhook and becomes 503; the fan-out runs reactors synchronously on the webhook goroutine (cheap, no I/O, safe for concurrent use), recovers panics per reactor and joins errors. Bridges moved out of `overview` (F-13).
