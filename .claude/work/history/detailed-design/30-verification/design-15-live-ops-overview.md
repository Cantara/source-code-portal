# Verification report: design unit 15-live-ops-overview

Date: 2026-10-14. Verifier: independent (not the author). Verdict: ready_with_caveats.

Scope: `20-design/15-live-ops-overview.md` and `15-live-ops-overview.assets/` (packages `live`, `ops`, `overview`). Other units' documents were read, not modified.

Method: structure review against the 15-section layout; conformance with `10-architecture.md` (import matrix 5.2, section 8 signatures, section 10 startup and shutdown, section 17.3 byte-identity) and the ADRs (001, 007, 009, 013, 014, 018, 024, 025); guardrail traceability (every guardrail and feature of this unit checked for a row with a concrete mechanism and a test); compile and test of the prototype in a scratch copy (Go 1.27.1) before and after the fixes; empirical probes (SSE under server timeouts, `HEAD` on the stream, pprof delta profiles); mutation testing of the prototype's tests (each named defect re-introduced as a compile-safe mutant); web and source checks of the library facts (WHATWG EventSource, RFC 9309, sitemaps.org, Go `net/http/pprof`); cross-unit contradiction check against units 01, 02, 03, 04, 06, 07, 08, 09, 10, 11, 14.

Final evidence: `gofmt -l .` empty; `go vet ./...` clean; `go test -race -count=1 ./...` green (65 top-level tests: live 9, ops 20, overview 36; goleak in each package); all six benchmarks inside their 12.3 gates; `TestLOCTripwire`: overview 2,214 of 2,500 non-test lines. Mutants killed: V15-01, 06, 11, 12, 13, 14, 16, 17, 18, 19, 20, 22. Design document and assets README refreshed; a "Verification log" section is appended to the design document.

## Findings

| # | Issue | Severity | Fixed | Evidence |
|---|-------|----------|-------|----------|
| 1 | V15-01: default SSE `ClientKey` was `RemoteAddr` (host:port), so every connection had its own bucket and the per-client cap never fired | high | yes | `TestDefaultClientKeyIsTheHostNotTheConnection`; mutant killed |
| 2 | V15-17: `HEAD /api/v1/events` opened a stream and held a client slot and a process slot until the client left | medium | yes | `TestHeadOnEventsOpensNoStream`; mutant killed |
| 3 | V15-19: `?seconds=` cap not applied to the pprof index; `/debug/pprof/heap?seconds=3600` blocked for an hour on the admin listener | medium | yes | `TestPprofIndexDeltaSecondsAreCapped` (2 s guard); mutant killed |
| 4 | V15-08: `AliasRedirect` placed outermost, so aliases resolve as an anonymous caller (visibility-first rule then wrong in private mode) | high | yes (doc) | 2.6, 2.7; `TestPrivateModeAliasIsPrivate` |
| 5 | V15-13: status text cut to 300 bytes plus an ellipsis (over the stated cap), multi-line, and task/store/parked text unbounded | medium | yes | `TestStatusTextIsBoundedOneLineAndValidUTF8`; mutant killed |
| 6 | V15-06/V15-15: idle back-off (12 h) versus TTL (2 h): `jobs.stale` and the data-age alert fired for healthy idle repositories | high | yes | `TestJobsStaleIsMeasuredAgainstTheIdleBackoffInterval`; mutant killed |
| 7 | V15-14: `store.backup-stale` silent when no backup ever succeeded | medium | yes | `TestWarningReadinessDegradedAndNeverBackedUpAndIntegrationGrace`; mutant killed |
| 8 | V15-16: `integrations.degraded` fired for a provider still `unknown` right after start | medium | yes | same test; mutant killed |
| 9 | V15-22: `llms.enabled=false` left negotiated Markdown (and the `Link` advertisement) on | medium | yes | `TestNegotiationFollowsLLMsEnabled`; mutant killed; Q11 |
| 10 | V15-11: `catalog.group_aliases` and `catalog.sync` unknown to the topics table (over-invalidation, registry guard fails) | medium | yes | `TestTopics`; mutant killed |
| 11 | V15-20: `liveness.heartbeat_max` allowed below the dispatcher's 10 s beat (flapping `/livez`, pod restarts) | medium | yes | `TestLivenessConfigFloor`; mutant killed |
| 12 | V15-12: `/status` ETag strong over bytes that pass the gzip middleware; snapshot ETag form differs from ADR-009 and unit 07 | low | yes (R-07-5 open) | `TestStatusETagIsWeakBodyHash` |
| 13 | V15-18: `readySince` overwritten when a degraded start became ready | low | yes | `TestReadySinceIsKeptWhenADegradedStartBecomesReady`; mutant killed |
| 14 | V15-07/V15-05: 2.5 claimed all overview sources are satisfied directly while rank 5 may not import feature roots; only `CommitsSource` is kernel-typed; `BadgesSource` has no owner accessor | medium | yes (doc) | per-interface mechanism table; R-DOCS-1, R-13-1, R-PAGES-1 reworded |
| 15 | V15-02: `overview` needs `httpapi/openapi`; matrix lacks the cell | low | yes (D15-11) | 2.1, 14.1 |
| 16 | V15-03: `live` needs the OTel metric API (G11); matrix lacks the cell | low | yes (D15-12) | 2.1, 14.1 |
| 17 | V15-04: architecture 10.2 `Coverage` probes versus `Kind.Infos` | medium | yes (D15-13) | 2.3, 14.1 |
| 18 | V15-10: two lifecycle stages assumed; unit 02 has one `servers` stage | low | yes (doc) | 2.7, 8.4 |
| 19 | V15-21: `q` documented over name, owners, tags; code searches slug, title, summary | low | yes (doc) | 4.8.3 |
| 20 | V15-23: package-level initialised variables (a `strings.Replacer`, three sets) against G5 | medium | yes | `go vet`, review |
| 21 | V15-24: provider status line rendered an "(observed ...)" year-1 time when the time is unknown | low | yes | review of `llms.go`; no dedicated test (the zero-time branch is untested) |
| 22 | V15-25: dead `var _` lines and unused imports in `sitemap.go`; unformatted test file | low | yes | `gofmt -l` empty |
| 23 | V15-26: test descriptions over-claimed (readySince, audit log, truncation); `TestFullConcurrencyLimit` failed by hanging | low | yes | 12.2; timeout added to the `entered` wait |
| 24 | V15-27: guardrails and features relevant to this unit had no trace row (dashboard, group page, layout, real 404s, embedded assets, build info, telemetry, CORS absence, `llms.txt`; GR-02, GR-03, GR-11, GR-14, GR-15, GR-16, GR-24, GR-33, GR-35, GR-38, GR-42) | medium | yes | section 11; rows with mechanism, section and test |
| 25 | Counts and sizes stale (LOC, test counts, tripwire 2,191, heartbeat range, Q8 default) | low | yes | header table, 12.2, Q5, 6.3 |
| 26 | V15-09: the admin `live` instance needs `snapshot`, `events`, `site` routes that unit 01's admin table does not list; unit 07 serves 22 public operations on the admin listener | medium | partly (R-01-2; fallback: no admin instance, poll `/status`) | 2.7, 14.2, Q8 |
| 27 | X15-1: `Info.Derive` marks items stale at TTL, so list rows show `stale` for most of an idle repository's life even though warnings now use the interval | medium | no (unit 01/04 decision) | 14.2 X15-1 |
| 28 | R-07-5: unit 07 and ADR-009 must gain the snapshot ETag form with phase suffix | low | no (other unit) | 14.2 |
| 29 | R-OVR-1: units 10, 11, 14 say "direct" for overview sources; they must say "bridged" | medium | no (other units) | 14.2 |
| 30 | Q4: a public SSE stream is woken by a private-only change (content-free signal) | low | no (accepted for v1.1) | 15 Q4 |
| 31 | Q12: HTTP/1.1 six-connection cap shared by all tabs when SSE is used | low | no (SPA decision, polling default) | 15 Q12 |
| 32 | Log fields of SSE and the retry audit line are not asserted (prototype logs to a discard handler) | low | no (`TestLogFieldsOnSSEAndRetry` planned) | 12.2 |
| 33 | Benchmark gates (12.3) are specified, not enforced | low | no (T6.3) | 13 |

Found: 33. Fixed: 25 (rows 1 to 25). Unresolved: 8 (rows 26 to 33; row 26 is partly fixed with a fallback).

## Guardrail traceability

The unit's probes and status pages follow GR-41: `/livez` and `/readyz` are local and binary, answer 200 or 503 from recorded outcomes, and "last seen" for a scheduled worker is recorded on completion, not on enqueue (GR-24). Site metadata reports the time of the last full sync and the repository count, never the render time (GR-26). The dashboard and group views read the configured `commits.latest_n`, not a hard-coded limit (GR-14). The dashboard refreshes by polling the snapshot version with an ETag and backs off when the tab is hidden (GR-40). `robots.txt` and the sitemap are generated from configuration (D15-7). No CORS header is emitted (GR-18). The GitHub health probe derives from recorded outcomes, not from an extra call (GR-41). Telemetry carries no request echo (GR-17).

Library and standard facts re-checked on 2026-10-14: EventSource drain rule (non-200 does not reconnect), RFC 9309, sitemaps.org limits, llms.txt format, Go 1.27.1 `net/http/pprof` write-deadline extension (`profile` and `trace` only, which is why the index wrapper was needed). Two hypotheses were tested and dropped: server `ReadTimeout` cancelling long SSE streams (Go clears the read deadline once the background read starts), and mixed-case slugs breaking `q` (`scm.Slug` allows only `a-z0-9._-`).

## Residual risk

- Cross-unit: the admin `live` instance (R-01-2), the `Info.Derive` basis (X15-1), the snapshot ETag form (R-07-5) and the bridge wording of units 10, 11, 14 (R-OVR-1) need those owners to act. None blocks M1 (probes) or M4 (resources); X15-1 affects the list-row freshness status, not the warnings.
- The bridges in `cmd/scp/internal/bridge` for the five overview sources are specified and not compiled; `BadgesSource` has no owner accessor today (R-09-1 asks for the kind list and path builder).
- The prototype uses hand-written stand-ins for generated OpenAPI types; conformance against `openapi-public.yaml` and `openapi-admin.yaml` is a planned test.
- `overview` sits at 2,214 of 2,500 lines (ADR-001 exit condition); the next text-output feature should trigger the split (Q5).
- Public SSE stream wake-ups are global (Q4) and HTTP/1.1 connection limits apply to multi-tab use (Q12).
- Timing-dependent behaviour (heartbeat, drain, write deadline) is tested with `synctest` for live and ops; real-network behaviour behind a proxy is not exercised.
