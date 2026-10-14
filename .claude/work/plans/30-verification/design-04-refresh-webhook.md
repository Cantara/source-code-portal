# Verification report: design unit 04-refresh-webhook

Date: 2026-10-14. Verifier: independent (not the author). Verdict: ready_with_caveats.

Scope: `20-design/04-refresh-webhook.md` and `04-refresh-webhook.assets/`. Other units' documents were read, not modified.

Method: structure and ADR conformance review (15 sections, import matrix 5.2, section 8 signatures); guardrail traceability (every relevant guardrail checked for a concrete mechanism); compile and test in a scratch Go 1.27.1 module; live web check of GitHub docs and module versions; mutation checks of seven fixes.

Final evidence: `go vet ./...` clean; `go test -race -timeout 5m -count=3 ./...` green; 62 tests plus 2 fuzz targets (was fewer before verification); all assets synced.

## Findings

| # | Issue | Severity | Fixed | Evidence |
|---|-------|----------|-------|----------|
| 1 | Priority/scope numbering did not match kernel (1-based, zero invalid) | high | yes | `TestPrioritiesAreOneBasedAndZeroIsInvalid` |
| 2 | One rate-limit event escalated the floor once per in-flight job | high | yes | `TestRateLimitOneEventManyInFlightJobsDoesNotEscalate`; mutation detected |
| 3 | Budget gate bypassed for promoted delayed entries and unasked classes | high | yes | `TestGateAppliesToPromotedDelayedEntries`; mutation detected |
| 4 | Panicking After hook / OnDone could kill the engine | high | yes | `TestHookPanicIsContained` |
| 5 | Permanent+rate-limited error parked without pausing; precedence differed from `job.Classify` | high | yes | `TestPermanentAndRateLimitedParksAndStillPauses` |
| 6 | Slow-body deliveries pinned a body slot (slowloris) | high | yes | `TestSlowBodyReleasesItsSlotAtBodyTimeout` (synctest) |
| 7 | Verified-sender delivery id stored/echoed unvalidated | high | yes | `TestHostileDeliveryIdFromAVerifiedSenderIsRefusedAndNeverStored` |
| 8 | `Retry-After` set through a nonexistent Problem field (unit 06 uses `Header`) | medium | yes | stub mirrors 06 section 4.1 |
| 9 | Webhook imported `freshness`, violating matrix 5.2 | medium | yes | `TestImportsStayInsideTheMatrix` |
| 10 | De-dup skipped for empty delivery id | medium | yes | webhook regress tests |
| 11 | Redelivery adapter truncation lost the watermark; spec timeout too short | medium | yes | `TestRedeliveryAdapterTruncationKeepsWatermark`, `TestRedeliverySpecIsValidAgainstTheKernelRules` |
| 12 | First global job through planner could run twice | medium | yes | `TestFirstGlobalJobThroughPlannerIsNotRunTwice` |
| 13 | `WithTunables` zeroed unset fields | medium | yes | `TestWithTunablesKeepsDefaultsForZeroFields` |
| 14 | Metric priority label numeric; item-age metric claimed but not emitted | medium | yes | section 9 |
| 15 | Reference decoder ignored non-push events; forced/commit-less push marked complete | medium | yes | `TestDecodeOtherEvents`, `TestForcedOrCommitlessPushHasIncompletePaths` |
| 16 | Wiring order, abandon-exit and planner-during-grace unspecified | low | yes | section 2.5 |
| 17 | Missing or wrong trace rows (GR-22 per-upstream breaker confused with GR-03 owned lifecycles) | low | yes | section 11 |
| 18 | Deviations D1/D4/D5 stale; open questions table order | low | yes | sections 14, 15 |
| 19 | Appendix A signatures out of date | low | yes | `go doc` diff |
| 20 | Appendix B lacked today's re-verified facts | low | yes | WebFetch of GitHub docs |
| 21 | Assets README stale test counts | low | yes | README |
| 22 | Fixtures only for push; no tag/release/ping/installation shapes | low | yes (partly, 3 hand-written) | testdata |
| 23 | X2: unit 05 `Verify` unclassified error mapped 400 vs 500 disagreement | medium | no (cross-unit) | section 14 X2 |
| 24 | X4/X5: unit 05 must implement `Truncated()` and the 422 mapping | medium | no (cross-unit) | X4, X5 |
| 25 | X6: duplicate reserve gate and config keys in units 04 and 05 | medium | no (cross-unit) | X6 |
| 26 | Q13: timed-out deliveries vs `status=failure` filter unknown | medium | no (needs M0 spike) | Appendix B |
| 27 | Target-Type header value for App webhooks, GHES retention unverified | low | no | Appendix B |

Found: 27. Fixed: 22 (counting item 22 as fixed with a caveat). Unresolved: 5 (items 23-27).

## Guardrail traceability

Webhook signature handling (GR-20), event-to-job coalescing (GR-21), per-upstream breaker and timeouts (GR-22), typed failures and rate-limit pauses (GR-23), the job engine (GR-24), budgets (GR-25) and freshness and reconciliation (GR-26) each appear in the section 11 trace with a mechanism and a test.

## Residual risk

The reference prototype uses stubbed neighbours; real integration depends on units 01, 02, 05 and 06 implementing the contracts listed in X1-X6. Hand-written installation and repository fixtures must be replaced by real recordings (T0.2).
