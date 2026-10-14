# Verification report: design unit 14 (integrations and access control)

Verifier: independent (not the author). Date: 2026-10-14. Document: `20-design/14-integrations-access.md` plus companion `14-integrations-access.assets/`.
Method: read the design, architecture sections 5, 8, 11, 16, 17, ADR-023/025, sibling designs 02, 06, 07, 09; compiled and ran the reference module in a scratch copy (the session scratchpad `verify-14-integrations-access/`, Go 1.27.1); checked the trace tables against `02-design-guardrails.md`; re-fetched Snyk and oauth2-proxy facts.

Result: **22 findings, 22 fixed in the document and the companion code; 5 open items that need the owner or a spike (none blocks implementation).** Baseline before changes: `go vet` clean, `go test -race` green. After: `go vet` clean, `go test -race -count=3 ./...` green, 15 new tests plus 2 new table rows, concurrency tests 40 repetitions, `FuzzIdentify` 8 s and `FuzzParseJobPath` 5 s clean.

## (a) Conformance with the architecture and ADRs

Import directions checked with `go list` on the reference code against matrix 5.2:

| Package | Imports | Matrix | Verdict |
|---|---|---|---|
| `integrations` (4) | `freshness job platform/secret readmodel scm store` + otel API | K4 + otel API | conforms |
| `integrations/jenkins` (5) | `integrations scm` | `integrations scm freshness platform/secret` | conforms |
| `integrations/snyk` (5) | `integrations` | same | conforms |
| `access` (4) | `httpapi/authz platform/secret scm` + otel API | `scm httpapi httpapi/authz platform/logx`, third party none | needs `platform/secret` and otel API: D-1 (already proposed; sound) |
| tests of `access`/`integrations` | `otel/sdk/metric`, `metricdata` | G6 "SDK only in platform/telemetry" | not stated anywhere: **new D-9** |

Interface signatures against 8.3 (`RateLimited{RetryAt}`, `Permanent{Permanent() bool}`), 8.4 (`Apply`, `Put`, `Slot`, `Global`, `ErrUnknownRepo`), 8.5 (`ItemStore` subset of `Store`: `PutItem`, `Revalidated`, `RecordFailure` identical), 8.9 (`StatusProvider`, `Target`, `Status`), 8.14 (`Authorizer`, `Principal`, `ClassOf`): consistent. The stubs differ from the architecture only where the assets README says so (`Register(..., existence bool)`, `NewMembership`).

## (b) Completeness

The 15 required sections are present and concrete (1 purpose to 15 open questions; section 0 is the design-time verification log; a section 16 verification log was appended). Every open question has an interim default.

## (c) Guardrail traceability

The unit's guardrails were each checked for a mechanism: upstream URLs are built with path escaping from validated job paths (GR-15, GR-25); refresh cadence is per kind and configured (GR-26); provider responses are parsed as JSON with typed fields and nothing is fetched that has no reader (GR-04, GR-34); every non-success status is classified, never silently ignored (GR-23); target resolution does not depend on iteration order (GR-08); no integration base URL or organisation name is a default (GR-14); integrations are off by default and configured per instance (GR-14); outbound calls have timeouts and do not follow redirects (GR-22); "last seen" is recorded from outcomes, not from any answer (GR-41).

The trace table lacked the badge, telemetry, per-task status and secret-configuration rows and GR-03, GR-04, GR-12, GR-41 (added with mechanisms, V-16). The catalogue reconciliation, integration descriptor, provider, visibility and order-independence rows, and GR-08, GR-13, GR-21, GR-22, GR-23, GR-25, GR-26, GR-32, GR-33, GR-34, GR-35, GR-41, were present.

## (d)-(g) Findings

Severity: H high (wrong behaviour in a supported configuration or a security weakness), M medium, L low, I informational.

| ID | Sev | Issue | Fixed | Evidence |
|---|---|---|---|---|
| V-01 | H | Jenkins HTTP 403 mapped to an instance-wide `PermanentError{auth}`. Jenkins answers 403 per job or folder (matrix/folder permissions); one such job aborted the instance and the permanent error parked the whole `integrations.jenkins/_` key for 24 h | yes: 401 = permanent, 403 = per-target `scm.ErrForbidden` (`forbidden`), `abort_after` in a row = instance `auth`; `Check` maps a root 403 to `auth` | `jenkins.go` status switch; test `TestForbiddenIsPerTargetUntilARun`, `TestErrors` rows; doc 3.2, 4.2.1, 4.4, 4.9 |
| V-02 | H | One aborted instance of several became the sweep error: permanent parked the healthy instance too; transient made the engine re-run the healthy instance's 1,000 targets after 2 s, 4 s, ... | yes: sweep error only when every probed instance is aborted, else `partial` into `Health` | `service.go` `fetchOutcome`; `TestOneAbortedInstanceDoesNotFailTheSweep`, `TestAllInstancesAbortedPrefersPermanent`; doc 4.2.1 |
| V-03 | M | A sweep where every target failed (HTML login page behind 200, wrong base_url) was "successful": `LastSuccess` advanced, `/status` healthy (health must derive from outcomes, GR-41); the per-task `consecutiveFailures` of the status page absent | yes: healthy-sweep rule, `Health.Probed/Failed/FailedCode/ConsecutiveFailures` | `TestAllTargetsFailedIsNotASuccess`; doc 4.2.1, 7, 9, 11.1 |
| V-04 | M | Snyk `clamp` returned 0 for any count above 1e6: a huge count read as "no known issues" (fail open); doc said "clamped" | yes: saturates at `MaxCount` | `snyk.go clamp`; `TestHugeCountSaturates`; doc 3.1, 4.5 |
| V-05 | M | Subject allow-list used `strings.ToLower`; Unicode folding maps U+212A KELVIN SIGN to `k` (executed), so a subject registered at the IdP with it matches another user's allow-list entry | yes: ASCII-only fold | `authorizer.go asciiLower`; `TestSubjectCaseFoldIsASCIIOnly`; doc 4.7.4 |
| V-06 | M | Warm start trusted stored payload: `detailUrl` (rendered as an anchor by the SPA, `javascript:` possible), count keys, negative counts, `state: disabled`; doc claimed only integers | yes: `safeDetailURL`, `cleanCounts`, `parseState` without `disabled` | `status.go`; `TestWarmItemRevalidatesPayload`; doc 3.1 |
| V-07 | M | Warn log on every spoofed request, justified by "the rate limiter bounds it", but the limiter is after `access.Middleware` in the chain (11.1): log flooding under attack | yes: one line per reason per minute; counter unsampled | `access.go shouldLog`; `TestSpoofLogIsSampledCounterIsNot`; doc 4.7.2, 9 |
| V-08 | L-M | Strip list lacked `X-Forwarded-Access-Token` (oauth2-proxy `--pass-access-token`, V16); subjects accepted U+2028/2029 and private-use characters | yes | `identity.go`; `TestIdentifyRejectsLineSeparatorsAndStripsAccessToken`; doc 4.7.2 |
| V-09 | M | Section 7 contradicted unit 07's validated `ProviderStatus`/`Freshness` (`detailUrl`, `counts` optional vs required; `freshness` lacked `kind`, `checkedAt`; `lastErrorCode` namespacing `github.rate_limited`) | yes (doc), plus test row `TestStatusContractShape` | `20-design/api/openapi-public.yaml:1673`, `common.yaml:145` |
| V-10 | M | Wiring snippet called `netguard.Client(netguard.Policy{Name, AllowCIDRs, ...})`, which does not exist in unit 02 (`NewPolicy`, `NewClient(ClientOptions{Policy, Hosts, AllowHTTP, Wrap})`, redirects off by default); pending `{file:}` secrets reveal `""` | yes (doc) | `02-platform.assets/internal/platform/netguard/netguard.go:42,115,139`, `secret.go:102` |
| V-11 | M | Per-instance defaults (`discover`, `multibranch`, timeout) were applied only by a helper the document never mentions; `New` and `Validate` failed with "discover must be declared or all" for an omitted key | yes: `Config.WithDefaults()` inside `Validate`/`New`, caller slice not mutated | `config.go`; `TestConfigDefaultsAreAppliedByValidateAndNew`; doc 3.3, 6.1 |
| V-12 | L | 404 carry hid a corrected descriptor/group job for up to `not_found_ttl` (1 h) | yes: in-memory instance\|job check | `service.go carryNotFound`; `TestCorrectedJobIsProbedDespiteNotFoundCarry`; doc 4.2 |
| V-13 | L | `privateWriter` consumed its one-shot `Cache-Control` rewrite on a 1xx (103 Early Hints) | yes | `TestPrivateWriterIgnoresInformationalStatus` |
| V-14 | L | Jenkins login-page redirect gave `ErrProtocol` per target (silent misconfiguration) | yes: final URL ending `/login` = `auth` (heuristic, fixture in M0, Q-2) | `jenkins.go`; `TestErrors` row; doc 4.4 |
| V-15 | L | Cluster of small defects: canonical SHA hashed numeric `State`; `New` accepted nil `Publisher`; Snyk `ports`/`max_response_bytes` unvalidated; branch `.`/`..` passes `PathEscape`; error bodies not drained (no connection reuse); `::ffff:10.0.0.0/104` trusted prefix never matches | yes | `status.go`, `service.go`, `config.go`, `jenkins.go`, `snyk.go`, access `config.go`; `TestDotBranchIsNotRequested`, `TestMappedIPv6TrustedRangeIsNormalised` |
| V-16 | L | Trace table incomplete (see (c)) | yes | doc 11.1, 11.2 |
| V-17 | L | V6 "1,620 requests/min" is not in Snyk's documentation (not found 2026-10-14; V1 API page says 2,000/min); V1 "tokens are region-specific" is not on the cited page | yes: reworded as unverified/diagnostic hint | web fetch/search 2026-10-14; doc 0 (V1, V6), 4.5 |
| V-18 | L | Two undeclared deviations: SDK imports in feature tests (G6 vs reference tests, same practice in unit 04); `DetailURL` over `http` for `allow_http` instances vs 8.9 "https only" | yes: D-9, D-10 | `go list -test`; arch 8.9 |
| V-19 | M | `access` trusts `r.RemoteAddr`: no requirement that no middleware rewrites it; SNAT in front of the public port (kube-proxy `externalTrafficPolicy: Cluster`) makes internet peers look like cluster addresses, so the proxy secret is the primary control there, not "defence in depth" | yes (requirement, test `TestRemoteAddrUntouchedByChain`, residual-risk text); structural fix is deployment (NetworkPolicy, `Local`, PROXY protocol) | doc 2.1, 10; `02-platform` `clientip.go` reads only |
| V-20 | I | Publishing every sweep advances `Snapshot.Rev` for ~1,000 repositories every 5 min (ETag/SSE churn); recorded, not changed | documented, Q-14 | doc 4.3 |
| V-21 | I | "payload absent on Revalidated/failure rows" misdescribes the store (the stored payload stays) | yes | arch 8.5 `Revalidated`, `RecordFailure` comments; doc 5.2 |
| V-22 | M | `Fact.Tone` has no names; unit 09's bridge maps by name (`success/info/warning/danger/neutral`) and maps unknown names to neutral, so every build and security badge would have been grey | yes: `Tone.String()` returns `success/warning/danger/neutral`, pinned by a test | `09-commits-badges.md` 2.4; `TestToneNamesAreTheCrossUnitContract`; doc 3.1, 4.6 |

Also corrected: the claim that the reference tests use `goleak` (they do not; the bubbles detect leaks, `goleak` is task I-2.7); doc answers unit 09's Q-8 (404 for badge kinds of a disabled provider).

## Testability

Every listed defect now has a named regression: budgeted, validated upstream targets, GR-25 (`TestFetchMapping`, `TestBadTargetsRejected`, `FuzzParseJobPath`, `TestDotBranchIsNotRequested`), order independence, GR-08 (`TestResolveIndependentOfMembershipOrder`), access policy, GR-35 (`E2E-enum-*`, `TestAuthorizerMonotone`), outcome-derived health, GR-41 (`TestAllTargetsFailedIsNotASuccess`, `TestNewChecksWiring`), typed upstream failures, GR-23 (`TestSweepErrorClassification`, `TestForbiddenIsPerTargetUntilARun`), bounded upstream calls, GR-22 (`TestSweepAbortsInstanceAfterConsecutiveFailures`, `TestDeadlineStillPublishes`), credentials in headers only, GR-13 (`TestErrors` token absence, `TestConfigValidate`), safe parsing, GR-34 (no XML, structural). Remaining gaps are the cmd/scp e2e tests, which cannot exist until the other units do.

## Open items (not defects; need owner or spike)

1. R-1 / Q-1: per-class change counter for the public ETag and `api.snapshot` (private activity visible through the global version). Interim: documented leak pinned by `E2E-enum-etag`.
2. Q-2 / Q-13 / I-0.1: Jenkins `_class` strings, `%252F` branch encoding, 403 versus 404 for unauthorised jobs, login redirect: heuristics until recorded fixtures exist.
3. Q-3 / I-0.2: Snyk `Retry-After`, whether `latest_issue_counts` excludes ignored issues, legal `origins`, `api_version` sunset.
4. Q-14: publish a status entry only when its answer changed (ETag/SSE churn).
5. Q-15: reuse `authz.PublicOnly` from unit 06 instead of the duplicate public-only authorizer.

Unit 09 and unit 06 should read: the `Tone` names (09 section 2.4) are now a tested contract of this unit; unit 07's `ProviderStatus` is authoritative for `statuses[]`; `authz.WithPrincipal` is in unit 06 as D-2 asked.
