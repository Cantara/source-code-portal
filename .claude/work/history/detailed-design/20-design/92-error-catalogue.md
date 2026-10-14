# 92 - Error vocabulary and problem-type catalogue (all units, 2026-10-14)

Written by the cross-design consistency review. The eighteen units each invented part of an error vocabulary: HTTP problem codes (06, 07, 04, 15), the stable code stored in `freshness.Info.ErrCode` (08, 09, 11, 14), typed adapter errors (05), job outcomes (04), diagnostics (10, 11, 12, 13), CLI exit codes (16). This document is the **single list**. A code that is not here is not allowed in a release; the generated reference (`docs/reference/errors.md`, produced by the same test that produces the metrics list) must equal sections 2 and 3.

## 1. The layers and who may use which

| Layer | Used on | Vocabulary | Owner |
|---|---|---|---|
| HTTP problem `code` | every non-2xx answer of an API operation and of the server routes that write problems (webhook, admin retry, `/livez`) | section 2: kebab-case, `^[a-z][a-z0-9-]*$` | `httpapi` (06) constants, contract catalogue (07 section 7.5) |
| `freshness.Info.ErrCode` and the wire `freshness.lastErrorCode` | the stable code of the last failed refresh of an item | section 3: snake_case, closed set | kernel type, each feature maps its errors with one shared table |
| Typed adapter error `github.Error{Kind, Subject}` | adapter to feature, never on the wire | section 4: eight kinds | 05 |
| Job outcome | `scp_sync_jobs_total{outcome}`, `/status` | section 5 | 04 |
| Webhook result | `scp_webhook_total{result}`, HTTP status | section 6 | 04 |
| Diagnostics (per file, per repository) | API `diagnostics`, `/status`, logs | section 7: `<namespace>.<name>` with a dot | 10, 11, 12, 13 |
| Configuration / descriptor violation | startup, `config lint`, `descriptor lint` | path-accurate message, JSON-pointer-like path | 02 |
| Process exit code | `scp` commands | section 8 | 16 |

The two code styles are deliberate: **kebab-case for what a client of the HTTP API sees as a problem, snake_case for the cause stored with data**, dotted for diagnostics that name a file or a limit. A dotted value (`github.rate_limited`) is never a `lastErrorCode` (it was in the examples of unit 07; corrected, CD-ER-01).

## 2. HTTP problem codes

A problem is `application/problem+json`, `type` is `urn:scp:problem:<code>` (an identifier, not a URL), `instance` is `urn:uuid:<request id>` (the request id is a UUIDv4, 02 D11), `status` equals the HTTP status, and `errors[]` (JSON pointers) appears for `validation-failed` and `invalid-cursor` only. Clients key on `code`; `detail` is never parsed and never contains internal text (06 rule, `StrictErrors`).

| Code | Status | Meaning | Produced by | Retry-After | `httpapi` constant |
|---|---|---|---|---|---|
| `validation-failed` | 400 | a parameter, header or body violates the contract; also a webhook delivery that cannot be decoded | request validator, handlers, webhook | no | `CodeValidationFailed` |
| `invalid-cursor` | 400 | cursor malformed, wrong version, issued for other filters or expired | handlers via `DecodeCursor` | no | `CodeInvalidCursor` |
| `unauthorized` | 401 | identity required and absent; a webhook with an invalid signature | `access`, `webhook` | no | `CodeUnauthorized` |
| `forbidden` | 403 | cross-origin state change refused, or the identity lacks the right; **never** for an invisible repository (that is `not-found`) | platform middleware, `ops` | no | `CodeForbidden` |
| `not-found` | 404 | no such resource, or an invisible repository (byte-identical, GR-35); an unknown webhook source; a slug that is not canonical | handlers (`authz.LookupRepo`), `ProblemMux`, `webhook` | no | `CodeNotFound` |
| `feature-disabled` | 404 | the operation's feature is switched off (`manifests.enabled`, `live.sse.enabled`, `llms.enabled`, `ops.pprof.enabled`) | handlers | no | `CodeFeatureDisabled` **(added, CD-ER-02)** |
| `method-not-allowed` | 405 | wrong method on a known path (with `Allow`) | `ProblemMux` | no | `CodeMethodNotAllowed` |
| `not-acceptable` | 406 | `Accept` excludes the only representation | validator | no | `CodeNotAcceptable` |
| `request-timeout` | 408 | the request body was read too slowly (webhook `body_timeout`) | `webhook` | no | `CodeRequestTimeout` **(added, CD-ER-03)** |
| `payload-too-large` | 413 | body above the class limit (1 MiB; webhook 25 MiB; CSP reports 64 KiB; admin retry 4 KiB) | body-limit middleware, `webhook`, `ops` | no | `CodePayloadTooLarge` |
| `unsupported-media-type` | 415 | a body that is not `application/json` (`retryJob`, webhook) | validator, `webhook`, `ops` | no | `CodeUnsupportedMediaType` |
| `rate-limited` | 429 | the per-client limit of the route class; SSE per-client limit; too many invalid webhook signatures | rate-limit middleware, `webhook`, `live` | yes | `CodeRateLimited` |
| `internal` | 500 | a bug on our side: recovered panic, unclassified verifier error; no cause is rendered | recover middleware, `StrictErrors`, `webhook`, `ops` | no | `CodeInternal` |
| `upstream-unavailable` | 503 | no cached value exists and the upstream needed to build one is down | handlers | yes | `CodeUpstreamUnavailable` |
| `not-ready` | 503 | cold start with no snapshot (never for `getSnapshot`); engine closed on a retry | handlers, `ops` | yes (15 s) | `CodeNotReady` |
| `queue-full` | 503 | a bounded job queue refused work (`job.ErrQueueFull`, `job.ErrClosed` from a webhook; `retryJob`; SSE client cap) | `webhook`, `ops`, `live` | yes | `CodeQueueFull` **(added, CD-ER-02; unit 04 used `overloaded` for it, CD-ER-03)** |
| `overloaded` | 503 | an admission limit is exhausted (no webhook body slot within `acquire_wait`); **not** a job queue | `webhook` | yes | `CodeOverloaded` **(added, CD-ER-03)** |
| `unhealthy` | 503 | a liveness check failed (`/livez`) | `ops` | yes | `CodeUnhealthy` **(added, CD-ER-02; additive to 07's catalogue)** |
| `repo-renamed` | 308 | the slug is an alias; `Location` carries the current URL | `overview.AliasRedirect` | no | `CodeRepoRenamed` **(added, CD-ER-02; `WriteProblem` now accepts status 308 for this code only)** |

Rules: (1) an API operation documents only 400, 404, 429, 503 (and 304/308 where they apply) plus `default`; the other codes are answered by shared middleware and need not be repeated per operation (07 section 7.4). (2) `429` and `503` carry `Retry-After` and `retryAfterSeconds` together (`httpapi.WithRetry`, now exported). (3) The vocabulary is **open to clients** (an unknown code is read by its status class) and **closed to the servers**: a new code is a change of this document, of the `httpapi` constants and of 07's catalogue in one commit, and a test in `apitest` (to be written with the constants: it compares the `Code*` constants with the rows of 07's catalogue) fails otherwise. (4) The three codes of unit 04 D5 that the first draft did not list (`overloaded`, `request-timeout`, and `queue-full` under another name) are the reason for CD-ER-03.

## 3. `ErrCode` (the stored and wire `lastErrorCode`)

The closed set, with the one table every feature uses to map an error to a code. **The order is the order of the checks** (first match wins); a feature implements the checks that apply to its sources and returns the same code as every other feature for the same cause.

| Check (in order) | Code | Meaning for a reader | Used by |
|---|---|---|---|
| `job.IsGone(err)` | `gone` | the repository left the catalogue | 08, 09, 11 |
| `job.IsPermanent(err)` | `permanent` | retrying cannot help (undecodable content, 4xx other); **kernel precedence of `job.Classify`: Gone > Permanent > RateLimited > Canceled** | 08, 09, 11, 14 |
| `job.AsRateLimited(err)` | `rate_limited` | GitHub (or another upstream) is rate limiting us | 08, 09, 10, 11, 14 (before the review 09 returned `error` and 11 `internal` for this case: fixed, CD-ER-01) |
| `context.Canceled` | `canceled` | shutdown or caller gone; **not recorded as a failure** (the engine does not call `AfterFailure`) | 08 |
| `scm.ErrNotFound` / `github.Error{KindNotFound}` | `not_found` | the thing does not exist upstream (repository, file, project) | 09, 10, 11, 14 |
| `scm.ErrForbidden` / `KindPermissionMissing` | `forbidden` | the credential may not read it (per target) | 09, 10, 11, 14 |
| `KindAuth` | `auth` | the credential was refused after a refresh | 14 (PermanentError code), adapter pause |
| `KindSuspended` | `suspended` | the installation is suspended or removed | adapter |
| `scm.ErrUnavailable` / `KindTransient` | `unavailable` | upstream 5xx, DNS, reset, breaker open | 09, 11 |
| provider-specific permanent errors | `no_data`, `config`, `protocol`, `too_large`, `aborted` | Jenkins/Snyk answers the sweep can name | 14 only |
| `errBlobVanished` | `history_rewritten` | a content SHA disappeared (force push) | 11 |
| `context.DeadlineExceeded` | `timeout` | the job or call deadline passed | 08, 09, 11, 14 |
| anything else | `transient` | unclassified, will be retried by the engine | all (before the review: `error` in 09, `internal` in 11) |
| recovered panic | `internal` | engine outcome `panic` parks the key with this code | 04 |

That is **19 codes**: `rate_limited canceled timeout gone not_found forbidden auth suspended unavailable no_data config protocol too_large aborted history_rewritten permanent transient internal` and `permission_missing` (reserved for `KindPermissionMissing` when a feature wants to tell it from `forbidden`; today it maps to `forbidden`). Maximum length 64 bytes (the store's check). The SPA (`freshness.ts`) maps every code to a sentence by exact match; a dotted form (`github.rate_limited`) is tolerated on input and never produced.

Implementation: each feature keeps its own `errCode(err)` (the packages cannot share one function because `scm`, `job` and the adapter error live at different ranks), and `cmd/scp` carries `TestErrCodeTable`, a table test that feeds the same twelve errors to every feature's mapper through its exported test seam and requires equal answers. **That test does not exist yet** (CD-ER-01 follow-up for units 08, 09, 10, 11, 14).

## 4. Typed adapter errors (unit 05)

`github.Error{Kind, Subject, Status, RequestID, ...}`; `Kind` is a closed set: `KindNotFound`, `KindAuth`, `KindPermissionMissing`, `KindRateLimited`, `KindSuspended`, `KindTransient`, `KindPermanent`, `KindCanceled`. `Subject` (`SubjectRepository`, `SubjectFile`, `SubjectInstallation`) says what a 404 refers to; `Gone()` and `Missing()` derive from it. Behaviour methods (the only way a feature may test them, because `github` may not be imported by features): `RetryAt() time.Time` on the pause error (`*PauseError`); a plain `*github.Error` has **no** `RetryAt` (05 D-12; architecture 8.7/15 text corrected, CD-IF-12), `Permanent() bool`, `Gone() bool`. Webhook verifier errors: `InvalidSignature() bool` (canonical; `Unauthenticated()` tolerated, CD-IF-08), `Malformed() bool`, `Ignored() bool`, `Reason() string`; redelivery: `Truncated() bool`.

## 5. Job outcomes (`scp_sync_jobs_total{outcome}`, `/status`)

`started success coalesced marked_dirty retried ratelimited parked parked_suppressed panic exhausted gone canceled queue_full` (unit 04). The kernel's `job.Class` (`none transient rate_limited permanent gone canceled`, the same strings as the ErrCodes of the same name) decides the label. A scheduler task outcome is `ok` or `error`.

## 6. Webhook results and answers (`scp_webhook_total{result}`)

| Result | HTTP | Problem code | Notes |
|---|---|---|---|
| `accepted` | 202 | none (`{"status":"accepted","delivery":id}`) | |
| `duplicate` | 202 | none | |
| `ignored` | 202 | none | unknown installation, unsupported event |
| `ping` | 202 | none (`"pong"`) | |
| `invalid_signature` | 401 | `unauthorized` | |
| `throttled` | 429 | `rate-limited` | per-IP invalid-signature budget exhausted |
| `malformed` | 400 | `validation-failed` | |
| `unsupported_media` | 415 | `unsupported-media-type` | |
| `too_large` | 413 | `payload-too-large` | |
| `body_timeout` | 408 | `request-timeout` | |
| `busy` | 503 | `overloaded` | no body slot |
| `queue_full` | 503 | `queue-full` | **was `overloaded` in unit 04; changed (CD-ER-03)** |
| `unknown_source` | 404 | `not-found` | |
| `error` | 500 | `internal` | an unclassified `Verify` error (unit 05 had said 400: settled 500, CD-IF-08) |

## 7. Diagnostics

Dotted `<namespace>.<name>`, namespace one of: `descriptor` (10: `syntax too_large multi_document anchor_or_alias schema_version invalid_field unknown_field bad_path`), `doc` (10: `route-collision id-too-long ...`), `limit` (`limit.files size depth nodes expand timeout tree-calls tree-entries units deps locked`, 11, plus `limit.<name>` of 12 and 13: closed per package), `tree`, `encoding`, `fetch`, `parse` (`parse.failed parse.panic parse.abandoned parse.circuit-open`), `assemble`, `lock`, and the ecosystem namespaces (`golang npm python maven gradle cargo nuget ruby php swift cocoapods dart elixir docker actions terraform helm`, 12 and 13), `provider` (13). Two spellings coexist inside the same namespace: `descriptor.*` uses underscores (`descriptor.bad_path`), the manifests namespaces use hyphens (`tree-calls`, `circuit-open`). They are kept (each is a documented, tested constant of its unit) and **never appear as `lastErrorCode`**. `parse.failed` (11, 586) is a cached-failure diagnostic of a file, not a refresh failure.

## 8. Exit codes (unit 16)

`0 exitOK`, `1 exitFail` (the thing asked for failed: I/O, bind, corrupt store), `2 exitUsage = exitInvalid` (the input was refused: bad flags, invalid configuration or descriptor), `3 exitWarn` (completed with caveats). A file that cannot be read is 1; a file that can be read and is wrong is 2.

## 9. Findings of this review about errors

| ID | Finding | Resolution |
|---|---|---|
| CD-ER-01 | Four `ErrCode` vocabularies (07 dotted examples, 09 `permanent not_found forbidden unavailable error`, 08 `rate_limited permanent transient canceled timeout gone`, 14 `rate_limited auth config no_data ...`); 09 and 11 did not map `rate_limited` (11 returned `internal`, a catch-all that hid GitHub throttling in `/status`) | section 3; code of 09 and 11 changed; 07 examples, `common.yaml`, the SPA mapping and fixtures changed |
| CD-ER-02 | 06 lacked `CodeFeatureDisabled`, `CodeQueueFull`, `CodeRepoRenamed`, `CodeUnhealthy`, `WithRetry` and refused status 308; 07's catalogue lacked `unhealthy` | added (06 code and test; 15 stand-in removed; 07 row) |
| CD-ER-03 | Unit 04 answered a full job queue with `overloaded`, 07 and 15 with `queue-full`; `overloaded` and `request-timeout` were in no catalogue | `queue-full` for the queue, `overloaded` for admission, both and `request-timeout` catalogued; 04 code changed |
| CD-ER-04 | The verifier error vocabulary differed between 04 and 05 (X1 spelling, X2 status, X4 truncation, X5 422) | settled in 04 and 05 text (CD-IF-08) |
| CD-ER-05 | Job key grammar: kernel 512 bytes with spaces allowed, contract 300 bytes without | kernel rejects white space, contract and `ops` accept 512 (CD-JOB-02) |
