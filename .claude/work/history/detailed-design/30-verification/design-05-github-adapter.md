# Verification report: design unit 05-github-adapter

Verifier: independent (not the author). Date: 2026-10-14. Document: `20-design/05-github-adapter.md` and companion `05-github-adapter.reference-tests.md`. Fixes were made directly in both files; the document ends with a "Verification log" section, and fixes carry V-nn markers in the text.

Working copy: `scratchpad/verify-05-github-adapter/mod` (copy of the author's scratch module, Go 1.27.1). Every changed Go listing in the document was regenerated from it after `gofmt`, `go vet ./...` and `go test -race -count=1 ./...` passed; the four fuzz targets were re-run for 8 to 10 s each. The pre-edit state of both files is kept at `scratchpad/verify-05-github-adapter/orig-design.md` and `orig-tests.md`.

## Result

23 findings (V-01 to V-23): 20 fixed, 3 open (V-11 needs the architecture owner, V-21 is architecture-level, V-23 is a cross-unit detail). Severity: 4 high, 13 medium, 6 low. Verdict: ready with caveats. The design is complete (all 15 sections present and concrete) and, after the fixes, buildable without questions. The caveats are the three open items and the unverified facts U-1, U-2, U-4 to U-11 that the design already contains with containment.

## Checks (a) to (g)

| Check | Outcome |
|---|---|
| (a) Conformance | Production imports of the three packages match the matrix row by row (`go list` over every package). One violation: `internal/github/*_test.go` imported `job`, which is not in the `github` row (V-01). Consumer interfaces in 2.3 equal architecture 8.7 line by line and compile against `*github.Client`/`*Verifier`. `scp dev`/`devtools` contradicted 5.1.1 and GR-27 (V-02). `cmd/fakegithub` skeleton broke G4/G10/5.1.6 (V-11). Deviations D-1 (jwt) confirmed by reading module source. |
| (b) Completeness | 15 numbered sections present, plus Appendices; no "decide later" without a default. Gaps fixed: 401 retry for non-refreshable credentials, auth pause state, half-open refusal, per-installation grouping of head sweeps, key files of the demo world, ledger API (V-12, V-13, V-16, V-22). |
| (c) Guardrail traceability | One trace text was wrong (the webhook verification row described every exception as a 500, while a malformed delivery is a 400). Trace table was missing GR-03, GR-07, GR-12, GR-13, GR-14, GR-16, GR-37, GR-41, GR-43, GR-44 and the default-branch, refresh cadence, badge, probe and build-info features: added with mechanisms (V-17). |
| (d) Technical | Found and fixed by executable tests: mint 403 rate limit misread as suspension, contents bound too small, fingerprint format, Rand01 race, mid-rune cut, comma in `Link`. Library claims checked against the module proxy and module source; API claims against docs.github.com and live calls. |
| (e) Testability | Named tests did not catch regressions of client timeouts (GR-22), signature header forms and verify-before-parse ordering (GR-20), empty-ref resolution (GR-10), outcome-derived health (GR-41), document file-name matching (GR-37) and empty-is-unset secrets (GR-13); added to 12.2 (V-18). |
| (f) Security and lifecycle | Mint classification (V-03), `Rand01` race (V-07), `Reveal` allow-list (V-17), fake control API reachable by browser pages (V-20), mint bypassing the breaker (V-19). No credential-forwarding or SSRF gap found in the redirect, `Link` and cursor logic (fuzz re-run, lookalike tests pass). |
| (g) Buildability | Remaining ambiguity removed in the places listed under (b). The request pipeline remains "specified only" by design; its spec is now internally consistent. |

## Findings

| ID | Severity | Issue | Fixed | Evidence |
|---|---|---|---|---|
| V-01 | high | `internal/github/github_test.go` and `fuzz` import `internal/job`; test files may import only what the package may import (arch 5.2 last paragraph); `job` is not in the `github` row, so `archtest` would fail | yes: tests assert the behaviour interfaces locally; note in 2.1 | `go list -f '{{.TestImports}}'` before: `internal/job`; after: `platform/secret`, `scm` |
| V-02 | medium | 2.2, 7.1, 11.3 propose a `scp dev` subcommand behind a `devtools` tag importing `githubtest` from non-test code | yes: removed; `make dev` runs two processes (arch 12.4); D-13 | arch 5.1 rule 1; GR-27; arch 1641 |
| V-03 | high | `auth.mintOnce` maps every 403 to `ErrInstallationUnavailable` (suspended). A secondary rate limit on mint would stop minting for the installation until the next reconcile; 422 and 429 mapped to transient | yes: `ErrRateLimited`, `ErrInvalidScope`, `mintRateLimit`; table and mapping text; test | `TestMintClassifiesRateLimitAndScope`; docs.github.com app installation tokens (403, 422) and rate-limit page |
| V-04 | medium | 4.3 had both "404 on installation token mint: NotFound/Installation (Gone)" and "403/404 on mint: Suspended" | yes: 403 suspended, 404 NotFound/Installation | 4.3 table, 4.12 pseudo-code already said so |
| V-05 | high | Contents/README JSON response bound `file_bytes*4/3+4KiB` ignores base64 line wrapping (60 columns, +1.77 percent): files above about 97 percent of the limit fail `too_large`; a smaller `file_bytes` makes truncation an error | yes: `contentsJSONLimit`; truncation after decode; test | live call: `GET /repos/golang/go/readme`, 34 lines of 60 chars, ratio 1.0177; `TestContentsBoundFitsGitHubBase64` fails the old bound by about 20 KB |
| V-06 | high | Conditional header from the in-memory memo was sent without a caller validator; a 304 then returns `NotModified` with an empty value to a caller that holds nothing (store rebuilt, item re-added) | yes: send only when `v.SHA` equals the SHA recorded with the ETag; rule stated in 4.2.1 and 4.9; test planned in 12.2 | reasoning over 4.9 "Validators"; no executable test exists yet (pipeline is unimplemented) |
| V-07 | medium | `Rand01` is called by `App.Token` outside any lock from many goroutines; a `*rand.Rand` is not safe | yes: `auth.NewRand01(seed)` with mutex; `Doer`; wiring text | `TestNewRand01IsConcurrencySafe` under `-race` |
| V-08 | medium | `Fingerprint` printed 16 hex characters; GitHub shows base64 SHA-256 of the DER public key, so operators cannot compare | yes: base64 form, 44 characters | docs.github.com managing-private-keys (openssl pipeline); `TestFingerprintMatchesGitHubFormat` |
| V-09 | medium | Manifest `default_events` lists `installation` and `installation_repositories`, which apps cannot subscribe to | yes: removed from the manifest; note | docs.github.com webhook events: "you can't subscribe to this event" |
| V-10 | medium | Redeliver treated 422 as "already redelivered: nil" without evidence; U-3 said cursor pagination was unknown | yes: 400/422 are errors; U-3 updated (`cursor`, `status` documented) | docs.github.com/en/rest/apps/webhooks |
| V-11 | medium | `cmd/fakegithub` skeleton used `os.Exit`, `signal.NotifyContext`, `http.Server{}` (G4, 5.1.6) and two unjoined goroutines (G10), `*os.File` params, `err != http.ErrServerClosed` | partly: skeleton rewritten (joined goroutines, `errors.Is`, loopback refusal, `io.Writer`); the lint exemption is proposed as D-11 and needs the owner | compiled; arch 5.1 rule 6, 5.2 row `cmd/fakegithub` |
| V-12 | medium | `ProbeHeads` says "grouped by source"; installation tokens see only their own repositories, so chunks must be grouped per installation | yes | 4.12 owner-to-installation rule |
| V-13 | medium | Pipeline spec: semaphore/limiter order contradicted between pseudo-code and prose; no state for the 5 min doubling auth pause; 401 retry for PAT/anonymous is a wasted call; half-open `ErrTooManyRequests` unmapped; `observe` ignored responses without `x-ratelimit-resource` | yes (spec + `authPause`, `observe(..., resource)`) | `TestAuthPauseDoublesAndSuccessResets`, `TestObserveFallsBackToCallerResource` |
| V-14 | low | `excerpt` cuts at 200 bytes inside a rune; App JWTs not redacted; `Link` split on commas inside a target | yes | `TestExcerptNeverSplitsARuneAndRedactsJWT`, `TestNextLinkCommaInTarget`, fuzz re-run |
| V-15 | low | `External.Token` discards a still-valid cached token when the refresh command fails | yes | `TestExternalKeepsValidTokenWhenRefreshFails` |
| V-16 | medium | Demo world references `app.pub.pem` that is not in the assets; `keygen` "refuses to overwrite"; acceptance 8 says `validate` passes | yes: not committed, idempotent keygen, acceptance reworded | `find 05-github-adapter.assets`: no pem file |
| V-17 | medium | Traceability gaps (listed above), webhook verification trace text wrong, "`Reveal()` in exactly three places" while the code has seven, placeholder `var _ = fmt.Sprintf`, empty `ref` for trees unspecified | yes: rows added, `Token.Same`, allow-list of seven sites, `HEAD` for empty ref | grep of `Reveal()`; live `git/trees/HEAD` = 200 |
| V-18 | medium | No named test for client timeouts, signature header forms, verify-before-parse ordering, empty ref, outcome-derived health, document file names, empty-is-unset secrets, V-06, V-12 | yes: rows added to 12.2 | section 12.2 |
| V-19 | low | Mint uses a raw client: outage seen by mint does not open the breaker, mint limits not observed | yes: `auth.Doer` implemented by the pipeline minus steps C/401 | 4.2.1 |
| V-20 | low | Fake control API (loopback) callable from any web page or DNS-rebinding page | yes: JSON content type only, no `Origin`, host check | 10.2 |
| V-21 | medium | One installation's exhausted bucket or `PauseError` pauses the whole `github` upstream for all installations of the App (`Gate.Admit(upstream, prio)` has no key) | no: architecture-level; recorded as Q-10 (extends Q-1) with a fix path | arch 8.6 `Gate`; design D-10 |
| V-22 | low | Ledger API in code lacks `Unhandled` and `Violations`, used by 7.5/11.3/12.3 | yes | code listing 7.4 |
| V-23 | low | `body_too_large` is `Malformed()` (400); 413 would be the natural answer | no: cross-unit detail for `webhook`; the receiver's `MaxBytesReader` already caps at the transport | 4.11 |

## Facts confirmed or settled by the verifier

Confirmed: D-1 (`golang-jwt/jwt/v5` v5.3.1 needs `*rsa.PrivateKey`); module versions in the scratch go.mod exist on the proxy; API version default and support dates; installation rate-limit scaling and caps; secondary-limit procedure and the order used by `classify`; 2,048 commit and 25 MB payload caps; head query validates against GitHub's current public schema; sha media type ETag/304 behaviour; anonymous 304 is charged. Settled: `cursor`/`status` on app delivery listing; fingerprint format; `installation*` events not subscribable. Not settled (cannot be from here, already listed with containment): U-1, U-2, U-4 to U-10, plus new U-11 (base64 wrapping is observed, not documented).

## Unresolved

1. V-11: owner of the architecture accepts D-11 (lint exemption for `cmd/fakegithub`) or provides another way to start a dev server.
2. V-21 / Q-10: per-installation pause granularity needs a `refresh` contract change or acceptance for v1.
3. V-23: `webhook` unit to decide the status for oversize bodies.
4. V-06 and V-12 are specified and listed as tests, but cannot be exercised until the pipeline exists (M3).
