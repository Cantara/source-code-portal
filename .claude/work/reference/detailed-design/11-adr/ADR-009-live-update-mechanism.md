# ADR-009: Live updates: snapshot-version polling baseline, SSE invalidation channel in v1.1

Status: Accepted. Date: 2026-10-14. Resolves the "polling+ETag or SSE" choice left open by 04 D-16 (A-17).

## Context
Polling every 30 s from hidden tabs wastes requests and upstream budget, so live updates must respect page visibility (GR-40). Browsers allow six SSE connections per origin on HTTP/1.1; proxies may buffer; SSE handlers block `Server.Shutdown` unless they observe a shutdown signal; `WriteTimeout` interacts with long-lived responses.

## Decision
- v1: `GET /api/v1/snapshot` returns `{version, builtAt, readiness}` with `ETag: W/"v<version>"`; TanStack Query polls it visibility-aware (15-30 s) and invalidates keys on change; per-resource ETags use `Rev`. Browser HTTP cache handles revalidation; no client ETag code.
- v1.1: `GET /api/v1/events` (`text/event-stream`): `id: <version>`, `retry: 3000`, heartbeat comment every 15 s, events `hello`, `invalidate {topics, generation}`, `resync`; payloads are never streamed; debounce 250-500 ms at the notification edge; ring of 256 change sets for `Last-Event-ID`; one stream per tab; per-IP concurrency limit 4; handlers `select` on request context and the shutdown channel and extend the write deadline with `http.ResponseController`; never wrapped by compression; excluded from `http.TimeoutHandler`.
- Both paths read the same `Publisher` primitives (`Subscribe`, `ChangesSince`), and every view shows a `FreshnessBadge`.

## Alternatives considered
SSE first (operational risk before the e2e proof), WebSocket (hijacked connections escape `Shutdown`), long polling.

## Consequences
Polling costs one 304 per 15-30 s per tab; SSE removes it later without changing the UI contract. SSE stays off by default until the Playwright and shutdown tests pass.

## Evidence
`05-research/05-store-sync-engine.md` section 3.5; `05-research/06-web-ui-badges-security.md` section 6; `05-research/04-go-architecture-patterns.md` section 9.4; `05-research/08-frontend-stack.md` section 2.7.
