# ADR-015: Resilience: per-upstream isolation, typed failures, stale-while-error

Status: Accepted. Date: 2026-10-14. Refines 04 principle 7 and D-04/D-09 (A-04).

## Context
Breakers built per call never trip, collapsing every failure into a synthetic 500 hides rate limits and absence, one pool shared by all upstreams lets a slow upstream starve the rest, and silently dropped jobs leave data stale without a signal; hence one client, breaker and limiter per upstream, typed failures, budgeted requests and a job engine that never loses work (GR-22..GR-26).

## Decision
- One client, `gobreaker/v2` breaker and limiter per upstream, constructed once in `cmd/scp`; dial, TLS, header and total timeouts; explicit redirect policy and host pinning; context on every call.
- Typed errors with behaviour interfaces; classification table (architecture section 15); only transient errors retry; rate limits pause the upstream until reset without consuming an attempt; 401 refreshes the installation token once then pauses the upstream (5 min doubling to 1 h) and raises a critical status; permanent failures park the key with a 24 h probe.
- Backoff: full jitter with a floor (`cenkalti/backoff/v7` for adapter-level HTTP retries, engine backoff for job retries); never immediate retries.
- Budget awareness: the adapter's `RateState` feeds `refresh.Gate` with reserves per priority (backfill needs more than 40% remaining, scheduled more than 15%, webhook more than 2%, interactive always); planner idle back-off doubles `next_due_at` after consecutive unchanged outcomes up to a cap.
- Stale-while-error: failures update status and error, never the payload; readiness never regresses after first success; the UI shows staleness from `freshness`.
- Bulkhead = per-upstream `MaxConcurrent` and pace in the engine; secondary-limit AIMD (halve concurrency, restore slowly) is phase 2.

## Alternatives considered
failsafe-go (pre-1.0), thread-pool bulkheads per call (goroutines make them unnecessary), retrying inside handlers (off the request path by design).

## Consequences
A GitHub outage degrades freshness, not availability; a permanent 403 on one repository never starves others. Exhausted retries are visible, not silent.

## Evidence
`05-research/05-store-sync-engine.md` sections 5.4-5.6, 5.11; `05-research/09-github-app-integration.md` section 9; `05-research/02-go-stack-currency.md` section 3.2.

## Amendment 2026-10-14 (review revision)

A `Gone` class (the subject disappeared: drop, no retry, no parking) and a per-job `Timeout` complete the classification; hooks run outside every lock; `ErrInvalidSnapshot` is permanent (parked, loud) instead of a retry loop. Global jobs (catalogue reconcile, head sweep, integrations) now run inside the engine, so they receive the pacing, gate, rate-limit pause, parking and panic accounting that the first draft gave only to per-repository jobs (F-16).
