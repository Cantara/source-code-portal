# 41. Test plan: strategy, inventory, budgets and traceability

Status: design, revision 1, written 2026-10-14. Binding inputs in precedence order: `04-decisions.md` (including section 7 amendments), `10-architecture.md`, `11-adr/` (ADR-016 is the test strategy of record). Derived from the unit designs `20-design/01..18`, the design guardrails in `02-design-guardrails.md`, the verification reports in `30-verification/`, the research prototypes in `05-research/` and the CI seed files in `50-repo-seed/`.

This document is the single place where the whole test effort is described: how many tests of which kind, with which tools, against which fixtures, in which CI stage and in how much time, and which test proves that each design guardrail (GR-nn, `02-design-guardrails.md`) holds. It does not restate unit test plans; each unit design keeps its own detailed list and this plan points at them by name. Counts in this plan are targets derived by summing the unit plans (labelled "target"), never measurements; measured numbers are labelled "measured" and carry their source.

Conventions used below:

- "(new)" after a test name means the test does not appear in a unit plan and is introduced by this plan. Every other test name is quoted from a unit design or the seed files and must exist with that name (or the unit plan is updated in the same change).
- "Gate" means a failing run blocks merge. "Non-gating" means the result is reported (job summary, issue, nightly report) but does not block.
- All time budgets are wall-clock on the standard GitHub-hosted `ubuntu-24.04` runner unless stated.
- "Fake GitHub" is `internal/github/githubtest` (unit 05); "world" is its in-memory GitHub model; "ledger" is its request record.

## 0. Inputs and verification log

Verified on 2026-10-14 (WebFetch of the primary source or the package registry). Items marked UNVERIFIED are carried as spikes in section 16.4.

| Item | Result | Source | Consequence |
|---|---|---|---|
| GitHub webhook signature test vector | secret `It's a Secret to Everybody`, payload `Hello, World!`, `X-Hub-Signature-256: sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17` | docs.github.com, "Validating webhook deliveries" | Used verbatim in `TestSignVector` and `FuzzVerify` seeds. |
| SHA-1 header | GitHub still sends `X-Hub-Signature` (HMAC-SHA1) next to the SHA-256 header for older consumers | same page | Tests must prove it is ignored (`TestInvalidSignatureMatrix` SHA-1-only row). |
| Schemathesis | latest on PyPI 4.31.0, needs Python >= 3.11; CLI `st run [OPTIONS] SCHEMA` with `--seed`, `-n/--max-examples`, `--max-time`, `-c/--checks`, `--exclude-checks`, `--phases examples,coverage,fuzzing,stateful`, `-w/--workers`, `--warnings` | PyPI, schemathesis.readthedocs.io | Pin 4.31.0 everywhere (D-T3: `ci.yml` pins 4.29.4). |
| k6 | latest release v2.3.0 (2026-09-21); `constant-arrival-rate` executor and `thresholds` exist. Licence is AGPL-3.0 from memory, UNVERIFIED | grafana/k6 releases, docs | Used as an external tool only, never linked into or shipped with the product (D-T5). |
| Playwright | `@playwright/test` 1.64.0; `@axe-core/playwright` 4.13.0 | npm registry | Units 17/18 pin 1.63; D-T3 asks for one bump through the pinned-version check. |
| Go 1.27 | `testing/synctest.Sleep` and `httptest.NewTestServer` (in-memory network usable inside a bubble) confirmed in the release notes; nothing new for `-race` or fuzzing | go.dev/doc/go1.27 | Patterns in section 6 may use both; the architecture pins Go 1.27.2, so they are available. |
| goleak | v1.3.0 | pkg.go.dev | `goleak.VerifyTestMain` in every package with goroutines (section 6.2). |

Earlier verifications recorded in `10-architecture.md` section 0 (Go 1.27.2, modernc.org/sqlite v1.60.1, ncruces v0.35.6, and others) are not repeated; this plan adds no dependency to the product module.

## 1. Principles

These are the rules every test in the repository obeys. Most are mechanically enforced (column "Enforced by"); the rest are review rules.

| # | Rule | Enforced by |
|---|---|---|
| P1 | Offline: no test touches the network except loopback listeners it created. Tests that need GitHub use the fake GitHub; tests that need a clock use `testing/synctest` or an injected clock. | CI job `offline` runs `go test` under `unshare -n` (new, D-T4); `forbidigo` bans `http.DefaultClient` and non-loopback dials in `_test.go`. |
| P2 | Every test asserts. A test that can pass with its body deleted is a defect (GR-43). | Archtest rule "every `Test*` body reaches a `t.Error/Errorf/Fatal/Fatalf/Fail` or a helper taking `*testing.T`" (new, simple AST check in `internal/archtest`); mutation checks (3.8). |
| P3 | No dependence on the working directory or on files outside the package: `t.TempDir()`, `t.Setenv`, `embed`/`testdata` only; no `os.Chdir`. | `usetesting` and `thelper` linters (GR-43); archtest `TestRealSource` rejects `os.Chdir`/`os.Getwd` in tests. |
| P4 | `t.Skip` needs an issue link on the same line; the skipped-test count may not grow. | Archtest rule `skip-needs-issue` (GR-43); CI step prints the skip count. |
| P5 | Hand-written fakes named after behaviour (`fakeClock`, `failingStore`, `blockingSink`), consumer-defined interfaces only. No mock-generation framework. | `depguard` list in `.golangci.yml` kept in sync with the architecture table by `TestDepguardInSync`. |
| P6 | A test names the behaviour, not the function: `TestPlanEverythingVanishingIsHeld`, not `TestPlan3`. Table rows have a `name` field and run as subtests. | `revive` rule plus review. |
| P7 | Determinism: fixed seeds printed on failure, no `time.Now()` in assertions, no global `math/rand`, no map-order dependence (golden outputs sorted). Every random test accepts `SCP_TEST_SEED`. | review; `TestRealSource` bans global `math/rand` functions in tests. |
| P8 | Fixtures are data, not code: recorded or synthetic files under `testdata/`, each with provenance and licence (section 4.4). | `TestFixtureLicences` (unit 13), `SOURCES.tsv` allow-list check (unit 12), CI job `gen` drift. |
| P9 | A bug fix starts with a failing test committed in the same change as the fix. A defect found by fuzzing becomes a seed file under `testdata/fuzz/` and, if it is a behaviour, also a named unit test. | PR template checkbox; fuzz crashers are committed from the nightly job's artifact. |
| P10 | The real thing wherever it is cheap. Real SQLite file in `t.TempDir()`, real `net/http` server on loopback, real router, real startup sequence `run()` (GR-43). Fakes only for GitHub, Jenkins, Snyk, deps.dev and time. | e2e `run()` tests in `cmd/scp`; `storetest` runs one suite against memory and both SQLite drivers. |
| P11 | Timeouts are mandatory: every `go test` is invoked with `-timeout` (default 10 min in the Makefile, packages that need less declare less) so a hang is a failure with a goroutine dump. | Makefile `test-go`; `TestMain` with `goleak`. |
| P12 | Tests do not leak secrets. A canary credential (a `ghs_CANARY...`-shaped value, never a real token) flows through the whole system in the e2e run and every log line, metric, status document and error body is scanned for it. | credential-leak canary (unit 05), `TestNoSecretInLogs` (unit 13), e2e canary scan (10.9). |
| P13 | The CI summary prints, per stage, tests run, skipped, fuzz execs and benchmark deltas. | `scripts/ci-summary.sh` (new, small). |

## 2. Test pyramid with count targets

The pyramid is bottom-heavy but not bottom-only: ADR-016 puts most assurance in pure-logic unit tests and `synctest` time tests, then integration against real SQLite and the fake GitHub, then a small number of end-to-end tests of the real binary and browser. Counts below are targets (sum of the unit plans plus each unit's "planned" additions), to be re-counted at milestone M-Test-3 (16.6). A layer more than 15 percent below target is flagged in the CI summary.

| Layer | What it proves | Tooling | Target count | Budget |
|---|---|---|---|---|
| L0 static | types, lint, architecture, generated-source drift, workflow lint | `go vet`, `golangci-lint`, `internal/archtest`, `apicheck`, `redocly`, `actionlint`, `shellcheck`, `tsc`, `eslint`, `check-pins` | 9 archtest tests, 27 apicheck rules (each with 32 failing mutations), about 40 lint rules, 5 drift checks | inside `go`, `gen`, `api`, `web` jobs |
| L1 unit (Go) | pure logic, parsers, matchers, planners, renderers, SQL against a real file | `testing`, `go-cmp`, `testing/synctest`, `goleak` | at least 1,250 top-level test functions and 5,500 executions including subtests (kernel 95/176, platform ~89, store 46 subtests x 3 backends, refresh ~74, github ~85, httpapi ~70, catalog ~65, commits/badges ~60, docs ~70, manifests core ~97, ecosystems 484 executions, integrations ~40, ops/overview ~55, cmd 46 + bridge 11 + archtest 9) | 25 min job `go` (includes L2) |
| L1 fuzz | no panic, no hang, structural invariants on every parser and decoder | native `go test -fuzz` | about 100 fuzz targets (5.2); seeds run in every `go test` | PR: 30 s per touched target; nightly: 5-8 min each |
| L1 property | model-based and algebraic invariants | hand-written generators on seeded `math/rand/v2`; `testing/synctest` for time | about 40 property tests (5.3) | inside `go` |
| L1 bench gate | budgets of section 9 hold | `go test -bench` plus `benchstat` against `testdata/bench/baseline.txt` | about 25 gates (absolute 2-5x tolerance on PR; delta nightly) | PR: absolute gates only, 5 min |
| L2 integration | components wired to real SQLite, real HTTP server, fake GitHub, virtual time | same, fake GitHub in process | about 150 tests: store conformance 46 x 3, 10 budget-arithmetic tests, 12 webhook round trips, 20 refresh+github, 25 docs/manifests end-to-end, 20 API handler + chain, 17 ops | inside `go`; SQLite job about 3 min with `-race` |
| L3 contract | the HTTP surface equals the OpenAPI document and the SPA types | `apicheck`, `redocly lint/bundle`, `oapi-codegen` and `openapi-typescript` drift, `oasdiff`, `apitest.ValidateResponse`, Schemathesis 4.31.0 | 27 rules; `TestRouteTableEqualsBundledPaths`; 3 Schemathesis seeds on PR (contract mock) plus a fresh seed nightly | `api` job 15 min |
| L4 e2e (Go) | the real binary behaves | `cmd/scp` `run()` tests; image smoke `deploy/smoke.sh` BASIC and FULL | 46 `cmd/scp` tests (about 24 e2e), smoke BASIC + FULL | `go` + `image` 25 min |
| L4 web unit | components, hooks, URL builders, guards | Vitest, Testing Library, jsdom; Vitest browser mode for 2 real-DOM tests | at least 400 tests: 144 (unit 17) + 145 (unit 18) existing, plus 48 C18 component ids and additions; about 20 browser-mode tests | `web` job 20 min |
| L4 browser e2e | user journeys against the real binary and fake GitHub | Playwright 1.64 on Chromium (Firefox and WebKit nightly), `@axe-core/playwright` | unit 17: 31 specs x 2 viewports = 62 runs; unit 18: 70 E18 ids x 2 viewports = 140 runs | `e2e` job 30 min |
| L5 system | perf, load, soak, chaos, upgrade | `go test -bench`, k6 (external), Schemathesis, `kill -9` harness, `unshare -n` | sections 9, 11, 12 | nightly / weekly; only the smoke is in the PR gate |

Shape check (target ratios by execution count): L0-L1 about 80 percent, L2 about 12 percent, L3 about 3 percent, L4 about 5 percent. If L4 exceeds 7 percent the owner is asked which lower-layer test is missing.

Where a behaviour can be tested at two layers the lower one is mandatory and the upper one is added only when the lower one cannot see the integration (route registration, header chain, embedded SPA, real timing).

## 3. Per-layer tooling

### 3.1 Go test runner and flags

| Concern | Choice | Detail |
|---|---|---|
| Runner | `go test` | CI additionally installs `gotestsum` as a separate tool (not a module dependency) to produce JUnit XML for the summary. |
| Race | `CGO_ENABLED=1 go test -race -count=1 -timeout 10m ./...` in `make test-go` | The release binary is `CGO_ENABLED=0` (ADR-016). A second short step runs `CGO_ENABLED=0 go test -count=1 ./...` (no `-race`) to prove no test or code needs cgo. |
| Shuffle | `-shuffle=on` on PR, seed printed | Order dependence is a defect. |
| Coverage | `-coverprofile` with per-package floors: kernel 90 percent per package; github adapter 85; store `readmodel` 85, `memory` 85, `sqlite` 78; other units declare theirs in `Makefile` `COVER_FLOORS`. No global floor; no coverage in the web projects (8.6) | Floors only go up (archtest rule on the Makefile). |
| Build tags | `live` (weekly real GitHub smoke, unit 05), `spa` (single-binary build), `ncruces` (escape-hatch SQLite driver). Tests run with and without `spa` where the embed matters (`TestRealTree`, `check-binary.sh`) | |
| Parallelism | `t.Parallel()` only in tests that use neither `t.Setenv` nor a `synctest` bubble | `tparallel` linter. |
| Output | `-json` piped to the summary step; failed tests print seed and fixture path | |

### 3.2 Assertions and comparisons

Standard `testing` plus `github.com/google/go-cmp/cmp`. Golden comparison uses the repo-local helper in 4.6. No assertion library. Error checks use `errors.Is/As` against typed errors; message matching only where the message is the contract (config violation text, goldens).

### 3.3 Time

Section 6.3 has the rules and patterns. Every package that sleeps, ticks or times out in production code gets its time tests in a bubble; the real clock is never used in an assertion.

### 3.4 SQLite

Real files in `t.TempDir()`; never `:memory:` for the SQLite adapter tests (WAL and file-lock behaviour is the thing under test). `storetest` conformance runs on `memory`, `sqlite` with modernc (primary) and `sqlite` with ncruces (build tag), 46 subtests each, plus the Degraded and Durable groups. SQLite tests never run inside a `synctest` bubble (file I/O is not durably blocking); they use an injected clock.

### 3.5 HTTP

Handlers are tested with `httptest.NewRecorder` for pure handler logic and with `httptest.NewServer` (real loopback) for the chain, `Content-Length`, hijack and slow-loris cases and SSE. `apitest.ValidateResponse` (kin-openapi validator, ADR-016) runs in every API test that decodes a body, so a handler cannot drift from the OpenAPI document without a test failing.

### 3.6 Frontend

Vitest (unit, jsdom), Vitest browser mode (2 real-DOM tests: focus management and `ResizeObserver`-dependent), Testing Library for components. MSW is not used: the e2e suite talks to the real binary and the fake GitHub, and unit tests of data hooks inject a `fetch` double through the typed client. Playwright for e2e and a11y; axe through `@axe-core/playwright`. See section 8.

### 3.7 Contract and fuzz tooling

Schemathesis 4.31.0 (Python >= 3.11, run from a container image pinned by digest; not a product dependency), `redocly` for lint/bundle, `oapi-codegen` and `openapi-typescript` for generated code with drift checks, `oasdiff` for breaking-change detection against the previous release. Native Go fuzzing for parsers (section 5).

### 3.8 Mutation checking

Mutation testing is not a gate. Units with delicate invariants carry hand-made mutation checks: a documented list of source mutations and the test expected to fail for each (kernel safehtml, refresh 7 checks, catalog, commits V-xx, apicheck 27 rules x 32 mutations). The `apicheck` corpus runs in CI (`TestRuleMutations`); the others run weekly by `scripts/mutation-check.sh` (new), which applies each mutation with `git apply`, runs the named test and fails if the test still passes. An optional non-gating nightly automatic mutation job on `internal/kernel` and `internal/refresh` is proposed in D-T6.

## 4. Fixture strategy

### 4.1 Four fixture families

| Family | Origin | Location | Update path | Used by |
|---|---|---|---|---|
| Recorded GitHub fixtures | Captured once from real GitHub (REST, GraphQL, webhook deliveries) against a project-owned throwaway organisation, scrubbed, committed | `internal/github/testdata/recorded/<endpoint>/<case>.json` with sibling `.meta.yaml` (recorded date, API version, scrub script version, licence statement) | `make fixtures-record` (needs `SCP_RECORD_TOKEN`; never runs in CI), review the diff; `make fixtures-verify` checks scrub invariants | Decoder tests, `TestDecodeRecordedPush`, round trips, fidelity register F-1..F-10 |
| Synthetic fake world | Declarative builders in `githubtest` (World, repos, trees, commits, installations, faults) | in code; the demo world (`acme`: `payments-api`, `docs-site` AsciiDoc, `archived-tool` private archived) is the shared seed | Edit builder; only the demo world is compared to a golden | all integration and e2e tests |
| Corpora | Hand-curated inputs for security and parsers | `internal/kernel/safehtml/testdata/corpus`, `internal/docs/testdata/{xss,ssrf,readmes,realistic}`, `internal/manifests/**/testdata` | Append-only; each entry has an id and a reason; failing-first rule (P9) | sanitiser, renderer, SSRF, parsers |
| Golden files | Expected output of deterministic renderers and reports | `testdata/*.golden` beside the test | `-update` through the shared helper (4.6) | badges, catalogue lint reports, headers, text formats, golden trees |

### 4.2 Recorded GitHub fixtures

1. Recording happens against a project-owned throwaway organisation, never a customer organisation, so no customer data enters the repository.
2. Each recording stores the request line, the relevant request headers (`If-None-Match`, `Accept`, `X-GitHub-Api-Version`) and the full response (status, `ETag`, `Link`, `X-RateLimit-*`, `Retry-After`, body). Authorization is never stored.
3. The scrub script replaces login names, node ids, avatar URLs and e-mail addresses with deterministic synthetic ones (`user-001`), keeps every field a decoder reads, and records its own version in `.meta.yaml`. `make fixtures-verify` fails on anything that looks like a token, an e-mail, or a real avatar host.
4. The recorded set is minimal and named for behaviour: `repos_list_page1_link_next`, `tree_truncated_true`, `push_force_no_path_lists`, `contents_readme_304`, `graphql_head_sweep`, `rate_limit_primary_403`, `rate_limit_secondary_403_retry_after`.
5. A recorded delivery for each handled webhook event kind is required before milestone M0 closes (open item W20 in unit 04). Until then `TestDecodeRecordedPush` runs on hand-written payloads derived from the example payloads in GitHub's webhook documentation and its subtests carry the name suffix `_synthetic`.
6. Jenkins, Snyk and deps.dev fixtures are hand-written until a recording is possible (open items in units 13 and 14); their names carry the suffix `_handwritten`.
7. Fidelity of the fake against reality is checked by the fidelity register (unit 05, F-1..F-10) and weekly by the live smoke (4.3).

### 4.3 The fake GitHub

The fake is the single stand-in for GitHub in L2-L5. Design facts the plan relies on:

- A World (organisations, installations, repositories with branches, trees, commits, files, releases, rate-limit state) and a Ledger recording every request with a virtual-time-aware timestamp, route key, conditional headers and credential kind. All budget tests assert on the ledger.
- Fault injection by named recipes: `secondary_limit:60s`, `primary_limit:1h`, `bad_credentials`, `no_permission`, `status:403:...`, `not_found`, `mint_fails:403`, `empty_repo`, `status:503`, `latency:2m`, `reset`, `truncate:100`, `malformed_json`, `wrong_api_version`, `skew_date`, `tree_max_entries`; webhook hooks `DropNext`, `DuplicateNext`, `DelayNext`, `Reorder`, `Redeliver`; payload styles `full`, `no_path_lists`, `no_tree_id`; commit lists above 2,048 entries.
- A control API under `/_fake/*`, present only in the `fakegithub` command used for e2e and smoke runs: set faults, advance the world (push a commit, rename a repository, remove an installation), read the ledger, deliver a signed webhook to the binary under test.
- Known gaps (unit 05): CD-FX-01 no SBOM or `dependencyGraphManifests` endpoint; CD-FX-02 no `object(oid:)` batch query; CD-FX-03 the demo world has no docs, descriptor, images or non-Maven manifests; CD-FX-04 no truncated monorepo, rename or multi-group world; CD-FX-05 no installation-scoped private repositories. Rows in section 15 that depend on them are marked "needs CD-FX-nn"; closing them is milestone M-Test-2.
- Version matrix: adapter tests run against `X-GitHub-Api-Version` 2026-03-10 and 2022-11-28 (the fake switches payload variants).

Weekly non-gating jobs verify the fake against reality: a `-tags live` smoke (one organisation, read-only, rate-limit-aware, fixed query set) comparing response shapes to the recordings, and the GraphQL schema check (fail if a field or type the adapter queries disappears).

### 4.4 Manifest corpora and licences

| Corpus | Contents | Licence rule | Check |
|---|---|---|---|
| Tier 1 synthetic | manifests written for the tests, one file per behaviour; 44 golden trees (golang 4, npm 10, python 7, maven 12, gradle 7, cargo 4) plus unit 13's fixtures for 27 parser ids | project licence; header comment "synthetic" | `TestFixtureLicences`: every fixture directory has an entry in `SOURCES.tsv` |
| Tier 1 adapted | small fragments adapted from permissively licensed projects | MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause only; copyright kept in `SOURCES.tsv` and `testdata/NOTICE` | allow-list check (unit 12): unknown licence id fails |
| Tier 2 oracle | about 21 real files cross-checked against an external parser (osv-scalibr) | never committed; fetched by blob id and hash-verified; CI cache only | nightly `canary` job, non-gating |
| Public-repo canary | about 200 public repositories fetched nightly, parsed, compared with the scalibr oracle; crashes and timeouts fail the job, differences are only reported | not stored | separate Go module `tools/canary` so scalibr is not a product dependency |

### 4.5 Corpus rules

- Append-only with ids. Each entry has a stable id and a one-line reason ("mXSS via `<noscript>` inside `<svg>`", "billion laughs variant 2"). Removing an entry needs the owner.
- Failing-first rule: a new corpus entry is committed together with a demonstration that the previous code fails on it, unless it is a regression entry added together with its fix.
- Counts are floors. ADR-016 says 36 sanitiser payloads; the implemented corpora hold 67 (kernel `safehtml`) and 43 (docs render). Tests assert `len(corpus) >= N` and unique ids. D-T2 asks to change the ADR wording to "at least".
- Sources of security vectors: OWASP XSS Filter Evasion Cheat Sheet, HTML5 Security Cheatsheet (html5sec.org), DOMPurify's public test vectors (MIT, adapted with notice), and the project's own regressions.

### 4.6 Golden files and the update workflow

Helper `internal/testkit/golden` (new, about 60 lines, shared by all packages):

```
golden.Check(t, name string, got []byte)   // compares with testdata/<name>.golden
-update flag or SCP_UPDATE_GOLDEN=1        // rewrites; refused when CI=true
```

Rules: goldens are text, SVG or JSON with sorted keys; each test says why the golden is the oracle; goldens that encode a security decision (CSP strings, header sets, `TestPolicyGolden`) require a CODEOWNERS review by the owner. Update procedure: run with the flag locally, read the diff, commit; the PR template asks which golden changed and why. The helper refuses `-update` when `CI=true`, and the `gen` job runs `git diff --exit-code` after regeneration steps.

Golden inventory (target about 115 files): 19 badge SVGs (`TestGoldenSVG`), `TestWidthTableGolden`, catalogue lint and membership-report goldens (unit 08), 44 manifest golden trees (unit 12), CSP and header goldens per response class (unit 06, 10.5), `TestPolicyGolden`, docs realistic-corpus outputs (40), format text golden (unit 10), llms.txt and sitemap goldens (unit 15), OpenAPI bundle and route table (unit 07), structured-log line shapes (unit 02).

### 4.7 Fixture directory conventions

Each package's `testdata/` has a `README` stating provenance, update command and licence. No executable bits; no binaries over 100 KB except the SQLite `v1.db` upgrade fixtures (12.1). Any file over 1 MB needs owner approval and a note in `NOTICE`.

## 5. Fuzz targets and property tests

### 5.1 Fuzz policy

- Native Go fuzzing (`func FuzzX(f *testing.F)`). Seeds (`f.Add` plus `testdata/fuzz/FuzzX/*`) run in every `go test`, so every committed crasher is a permanent regression test.
- PR: `-fuzztime=30s` for each fuzz target in a package touched by the diff (10-20 s elsewhere is allowed by `make fuzz-pr` when time permits). Budget: at most 5 min in job `go`.
- Nightly: sharded (`nightly.yml` `fuzz`), 8 min per target, crashers uploaded as artifacts and committed by a human into `testdata/fuzz/`.
- Every fuzz target checks an invariant, not only "no panic": round trip, fixpoint, bounded time (`context` with a 2 s deadline), bounded output size, or equality with a simple reference model.
- Parsers that take untrusted input never recurse without a depth cap and never allocate proportionally to a declared (not actual) length; the fuzz bodies assert `testing.AllocsPerRun`-free but check `runtime.MemStats` delta under a 64 MiB cap for the ecosystem parsers (`FuzzParsers`).

### 5.2 Fuzz inventory (about 100 targets, target)

| Unit / package | Targets |
|---|---|
| 01 kernel | `FuzzSanitize`, `FuzzSlug`, `FuzzSecretRedaction` (3; safehtml fixpoint is also in unit 10) |
| 02 platform | `FuzzLoadFile`, `FuzzParseBytes`, `FuzzEnv`, `FuzzClientIP`, `FuzzRate`, `FuzzRedactURL`, `FuzzParseHeadersFile`, `FuzzParsePath` (8) + `FuzzNetguardCanonical` (new) |
| 03 store | `FuzzDecodeRow`, `FuzzMigrateStatements`, `FuzzCursorRoundTrip` (3) |
| 04 refresh | `FuzzQueueModel` (model-based, new), `FuzzPlanReconcile` (2 + 1) |
| 05 github | `FuzzVerify`, `FuzzEscapeRepoPath`, `FuzzDecodeCursor`, `FuzzNextLink`, plus planned `FuzzDecodePush`, `FuzzClassify`, `FuzzTreeDecode`, `FuzzCommitDecode`, `FuzzGraphQLResponse`, `FuzzParseConfigDuration` (10) |
| 06 httpapi | proposed `FuzzDecodeCursor`, `FuzzNotModified`, `FuzzParseAccept`, `FuzzShapeMatch`, `FuzzParseManifest` (5) |
| 07 openapi | `FuzzApicheckRules` (new, 1: random mutations of the document never crash `apicheck`) |
| 08 catalog | `FuzzConfigDecode`, `FuzzMatcherCompile`, `FuzzMakeSlug` + 3 more per unit (6) |
| 09 commits/badges | `FuzzMerge`, `FuzzRenderWellFormed` (2) |
| 10 docs | `FuzzRender`, `FuzzSanitizeFixpoint`, `FuzzCanonical`, `FuzzRewriteLinks`, `FuzzDetectType`, `FuzzDescriptor`, `FuzzRepoPath` (~7) |
| 11 manifests core | `FuzzTreePlan`, `FuzzDetect`, `FuzzGraphBuild`, `FuzzBlobCache`, `FuzzPathNorm`, `FuzzVersionRange`, `FuzzPURL`, and 4 more (~11) |
| 12-13 ecosystems | 27 parser targets (one per parser id) + `FuzzDecodeYAML`, `FuzzRubyScan`, `FuzzRepoKey`, `FuzzSBOMDecode`, `FuzzGraphQLDecode`, `FuzzDepsDevDecode`, `FuzzParsers`, `FuzzTree` (~35) |
| 14 integrations/access | `FuzzIdentify`, `FuzzJenkinsDecode`, `FuzzSnykDecode` (3) |
| 15 live/ops/overview | `FuzzOverviewFilter` (1) |
| 16 cmd | `FuzzFlagsAndEnv` (1) |

`make fuzz-list` prints the authoritative list; the count in the table is checked by `TestFuzzInventory` (new) comparing `make fuzz-list` with the names in appendix B (generated), so a new target appears here in the same PR.

Arithmetic problem recorded as D-T1: 100 targets x 8 min = 800 min, while the seed `nightly.yml` provides 4 shards x 120 min = 480 min and a limit of 14 targets per shard (its comment says to raise SHARDS when `make fuzz-list | wc -l` exceeds 56). Proposal: SHARDS=8 (13 targets per shard x 8 min = 104 min, inside the 120 min limit), or keep 4 shards and drop to 5 min per target with the first 56 targets at 8 min.

### 5.3 Property tests (about 40, target)

| Property | Where | Oracle |
|---|---|---|
| Membership computation equals a naive model over random catalogues and matchers | catalog `TestMembershipAgainstNaiveModel` | naive nested loops |
| Membership is independent of repository and group order | `TestMembershipIsOrderIndependent` | permutation |
| Many-to-many defaults and refs resolve identically for any group order | `TestMembershipManyToManyDefaultsAndRefs` | table |
| Merge of per-repository commit rings equals sort-then-page | commits `TestMergeEqualsNaiveSortPaged` | naive sort |
| Sanitiser is a fixpoint: `S(S(x)) == S(x)`; output contains only allow-listed elements and attributes (structural oracle by walking the output tree) | safehtml / docs `TestCorpusIsSafeAndAFixpoint`, `FuzzSanitizeFixpoint`, differential allow-list walk | tree walk |
| Planner: every enabled item is either queued, running, scheduled or marked held; no item scheduled twice; a vanished item is never silently dropped | refresh `TestPlan*`, `FuzzPlanReconcile` | model |
| Queue: coalescing and dirty-while-running keep at most one queued and one running job per key; every enqueue is eventually run or counted as rejected | refresh `TestCoalesceQueuedAndDirtyWhileRunning`, `FuzzQueueModel` (new) | model-based sequence generator |
| Read model immutability: values returned by readers are never mutated by later writes (run under `-race`) | readmodel `TestImmutabilitySharingAndRev` and the `-race` immutability property test (amendment) | deep-copy compare |
| Read model with random operation sequences equals a trivially correct reference | readmodel `TestModelBased` | reference map |
| Store backends are observationally equal | `storetest` over memory, modernc, ncruces | same suite |
| Cursor encode/decode round trip and rejection of foreign cursors | `FuzzDecodeCursor` (httpapi, github), `FuzzCursorRoundTrip` | identity |
| ETag follows derived state only (same state, same ETag; any visible change, new ETag) | `TestETagFollowsDerivedStatus`, `ETagCoversBody` | hash of body |
| Authorisation is monotone: adding a credential never removes access, removing never adds | access `TestAuthorizerMonotone` | lattice |
| Anonymous responses equal those of a public-only deployment byte for byte | `TestAnonymousResponsesAreByteIdenticalToAPublicOnlyDeployment` | twin deployment |
| URL builders and route table round trip (every builder output matches exactly one route and parses back to its arguments) | `TestBuildersRoundTrip`, `TestLinksResolveToRoutes` | route table |
| Badge text width equals the width table for all printable ASCII; badge is well-formed XML | `TestWidthTableGolden`, `FuzzRenderWellFormed` | table, parser |
| Parser path independence: the same file content yields the same result at any path | `TestParserPathIndependence` | permutation |
| Input scale linearity: parse cost grows linearly across 18 shapes | `TestInputsAtScaleStayLinear` | ratio of times at 1x and 8x under a bounded factor |
| Graph build is deterministic and order-independent | manifests `TestBuildGraphDeterministic` | permutation |
| Rate limiter token bucket never exceeds burst and never starves (virtual time) | platform `TestRate*` | model |
| Jittered backoff stays inside its bounds and is monotone in attempt number up to the cap | refresh/github `TestBackoffBounds` (new) | bounds |
| Config: decode then encode then decode is stable; unknown keys are always an error | `FuzzConfigDecode`, `TestStrictDecodeRejects` | identity |

Property tests print their seed; failures are reproduced with `SCP_TEST_SEED=<n> go test -run <name>`.

## 6. Race, goleak and synctest policy

### 6.1 Race

- `go test -race` is on for every package in the gating `go` job (ADR-016). Packages with heavy per-test cost declare scaled workloads under `-race` through the helper `testkit.RaceScale()` (returns 1 normally, 0.2 under race via a build-tagged constant), as unit 10's adversarial-timing tests do: the non-race job asserts absolute times, the race job scales them.
- Tests that exist to find races (`TestConcurrentReadersDuringWrites`, `-race` immutability property, `BenchmarkGetParallel`, refresh stress) run at least 100 iterations on PR and 10,000 nightly (`SCP_STRESS=1`).
- The race detector's blind spots are listed so nobody believes it covers them: it does not find lock-order deadlocks (the stuck-job and `/livez` tests cover liveness), does not check atomicity of multi-step invariants (model-based tests cover them), and misses races in code paths no test executes (coverage floors).

### 6.2 goleak

- Every package that starts goroutines has `func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }` with an explicit ignore list limited to documented runtime goroutines; the list lives in `internal/testkit/leaks` and is reviewed by the owner. Unit 11 lists goleak as "planned"; this plan makes it a rule: archtest `TestEveryGoroutinePackageHasGoleak` (new) fails for a package whose non-test code contains `go ` statements or `time.NewTicker` but whose tests lack `VerifyTestMain`.
- Tests with `httptest.NewServer` close the server in `t.Cleanup`; tests that start `run()` stop it through its context and wait for the returned `Done` channel.
- A leak found in production code is fixed in production code; the ignore list never grows to hide an application goroutine.

### 6.3 synctest usage patterns

`testing/synctest` bubbles give virtual time for tickers, timeouts, backoff and schedulers. Rules and patterns:

1. Use `synctest.Test(t, func(t *testing.T){...})` for any test that depends on `time.Sleep`, `time.After`, tickers, `context.WithTimeout` or `Retry-After`. Never combine with `t.Parallel()` or `t.Setenv` (restriction of the bubble).
2. Wait for settlement with `synctest.Wait()` before asserting, never with `time.Sleep` in the test body unless the sleep is the thing being measured; advance with `time.Sleep(d)` inside the bubble.
3. Network inside a bubble: use `httptest.NewTestServer` (Go 1.27, in-memory network) or `net.Pipe`; a loopback TCP listener created inside a bubble is not durably blocking. The fake GitHub exposes `githubtest.NewInBubble(t)` returning an `http.Client` wired to the in-memory server (as in unit 05's plan).
4. SQLite and the filesystem stay outside bubbles; time-dependent store behaviour is tested with an injected clock (`kernel.Clock`).
5. Cold start with 3,000 item completions runs in virtual time (`TestColdStartThreeThousandItems`, last first-pass start measured at 4 min 59.9 s virtual): CI runs the schedule in virtual time and the CPU/memory work with pace 0, so the 5 minute real-time cold start of production defaults never appears in CI.
6. Shutdown ordering tests (`TestWorkerTimingInSynctestBubble`, the lifecycle plan with a failing and a non-cancelling stage, SIGTERM budget 44 s worst case, ceiling 48 s, grace 45 s) assert on virtual elapsed time.
7. Pattern for "nothing happens": after `synctest.Wait()` assert that the ledger count is unchanged, then advance by the interval and assert exactly one more.
8. Pattern for races between a timer and an event: use two goroutines in the bubble with a channel handshake; the bubble makes the interleaving deterministic, so these tests run without `-race` flakiness.
9. Pattern for hangs: a stage that ignores cancellation is simulated by a goroutine blocked on `<-make(chan struct{})` inside the bubble; the test asserts the lifecycle returns an error after the grace period and the bubble is released by closing the channel in `t.Cleanup`.
10. A test that needs real time (benchmarks, the 60 s smoke, SIGKILL harness) lives outside the bubble and is tagged in its name (`..._RealTime`) so the CI summary can list them; at most 15 such tests exist and their total must stay under 60 s.

## 7. Contract tests (OpenAPI, Schemathesis, SPA types)

The OpenAPI document (unit 07) is the single source of truth for the HTTP API. Four independent checks make drift impossible to merge.

### 7.1 Document quality (job `api`, 15 min)

| Check | What it catches | Name / tool |
|---|---|---|
| `redocly lint` and `redocly bundle` drift | invalid or unbundled document; committed bundle differs from sources | `make api-check` |
| `apicheck` | 27 project rules (open enums, UTC timestamps, no `Access-Control-*`, every error uses the problem schema, cursor parameters, ETag headers on cacheable responses, no unbounded arrays, `operationId` conventions, security class of each operation, and so on). Every rule has 32 failing mutations in a corpus; the test fails if a rule does not fail on its own mutations | `TestRuleMutations`, one subtest per rule |
| `keycheck.py` | duplicate keys that YAML parsers silently merge | `make api-check` |
| `oapi-codegen` drift | generated server types differ from committed | `make gen` then `git diff --exit-code` |
| `openapi-typescript` drift and TS proof | generated TS types differ; the SPA compiles against them (`tsc`) | job `web` |
| `oasdiff` breaking vs the last release tag | removal, narrowing, required-field addition; enums must be open, so adding values is not breaking | `make api-diff` (skipped on the first release) |
| Route table equals bundled paths | a registered route missing from the document or the reverse | `TestRouteTableEqualsBundledPaths`, `TestMuxRegistration` |
| API class table equals OpenAPI | the access class of each operation (public, admin, webhook, probe) in code equals the document's | `TestAPIClassTableEqualsOpenAPI` (amendment, new test name) |
| HEAD through the validator | `HEAD` is answered for every `GET` and the validator accepts it | `TestHeadThroughValidator` |
| Repeated `If-None-Match` lines | list-valued conditional header handled as one list | `TestRepeatedIfNoneMatchLines` |
| Timestamps are UTC, enums are open | `TestTimestampsAreUTC`, `TestEnumsAreOpen` | |

### 7.2 Response validation inside handler tests

`apitest.ValidateResponse(t, req, resp)` validates status, headers and body against the OpenAPI operation. Companions `ETagCoversBody` (ETag derives from body bytes, same bytes same tag) and `NoLeak` (no field outside the schema, no e-mail-shaped string, no token-shaped string) are called by every list, detail and status handler test. Responses carrying `Access-Control-*` fail validation (GR-18).

### 7.3 Schemathesis

| Run | Where | Parameters | Gate |
|---|---|---|---|
| Contract mock, 3 fixed seeds | PR `api` job, against the Prism-free in-repo contract mock built from the document (unit 07) | `st run --seed N --phases examples,coverage,fuzzing -c all --max-examples 100 --workers 2`, N in {1, 2, 3} | Gate. Runtime budget 4 min. |
| Real binary, fixed seed, read-only operations | PR `e2e` job against the binary with the fake GitHub, `GET`/`HEAD` only; mutating operations are not executed (webhook route is excluded: it needs a signature; it has its own tests) | `--seed 7 --max-examples 50 --exclude-checks ignored_auth` for the anonymous class (ignored_auth would flag intentionally public routes) | Gate. 5 min. |
| Extended, fresh seed | nightly `api-extended` job (60 min limit; Schemathesis `--max-time 10m`), including the `stateful` phase on pagination links | `--seed $(date +%s)`; seed printed and uploaded | Non-gating; failures become issues; a failing seed is copied into the PR test as a fixed seed |
| Auth-class matrix | admin routes with and without the bearer token, public routes with a spoofed identity header | `--checks negative_data_rejection,missing_required_header,unsupported_method,use_after_free` | Gate on PR run 2 |

Pin: Schemathesis 4.31.0 (verified 2026-10-14); `ci.yml` currently has 4.29.4 (D-T3). Python 3.11+ in a pinned container image. Schemathesis is a tool only and never a product dependency.

### 7.4 Server-sent events

The SSE endpoint (v1.1) is outside Schemathesis; its contract tests are `TestStreamHelloInvalidateHeartbeatDrain` (hello, invalidate, heartbeat, drain on shutdown), `TestStreamPerIPCap` (4 per IP) and the synctest tests of heartbeat cadence, all validated against the event-schema section of the document with `apitest.ValidateEvent` (new, 30 lines).

### 7.5 Consumer contract: SPA to API

The SPA's typed client is generated from the same document, so a shape drift is a TypeScript compile error. In addition `TestSPAShapesAgainstRoutes` (unit 06) loads `routes.json` (the route table exported for the SPA) and checks that every SPA route resolves to a shell answer and every SPA API call path exists in the bundled paths.

## 8. Frontend tests

### 8.1 Component and unit (Vitest)

- Existing plan: unit 17 foundation 144 tests in 21 files plus 2 browser-mode tests; unit 18 pages 145 tests in 14 files plus 48 component ids `C18-*`. Target at M-Test-3: at least 400 Vitest tests and about 20 browser-mode tests.
- Scope of unit tests: URL builders and route guards (round trip with the Go route table through `routes.json`), data hooks (`useNow`, `refetchIntervalInBackground: false`, ETag-aware polling, stale indicator), formatting (no `null`, `undefined`, `NaN` in rendered text: `E18-TXT-01` also checked as a unit property over all formatters), Markdown/AsciiDoc rendered-document component (never uses `dangerouslySetInnerHTML` except in the one audited component; lint rule), graph layout worker messages, table virtualisation, error boundaries and the not-found shell (on the Go side the real 404 with the shell is `webapp` rule 8, `20-design/06-httpapi.md` section 4.10.1).
- Rules: Testing Library queries by role and accessible name (no test ids unless a role is impossible, documented); no snapshot tests of DOM trees; no `waitForTimeout`; fake timers only through Vitest's `vi.useFakeTimers` with a fixed system time `2026-10-14T10:00:00Z`; `TZ=Europe/Oslo`, locale `en-GB` set in the Vitest config.
- No coverage thresholds in the web projects (unit 17 decision): the coverage that matters is "every route and every state has an e2e and a component id". `check-test-ids` (new, script) fails when a state listed in the page design has no `C18-*` or `E18-*` id.
- Mutation testing for web is out of scope.

### 8.2 Browser e2e (Playwright)

- The e2e job builds the real binary with the SPA embedded (`-tags spa`), starts it with the fake GitHub's control API and the demo world, then runs Playwright against it. No page-level request mocking except the "stub mode" for cases the fake world cannot produce (`page.route` documented per test and listed in `e2e/STUBS.md`; target at most 6).
- Projects: `desktop` (1280x800) and `phone` (390x844) on Chromium on every PR; Firefox and WebKit on the nightly job, same specs, `desktop` only.
- Determinism rules (unit 18): fixed clock via the binary's `SCP_TEST_NOW` seam active only in the e2e build tag, TZ `Europe/Oslo`, locale `en-GB`, `reducedMotion: 'reduce'`, no `waitForTimeout`, every wait is on a locator or the `scp.route.ready` performance mark.
- Specs: unit 17 foundation 31 specs x 2 projects = 62 runs; unit 18 70 ids `E18-*` x 2 = 140 runs. Journeys that must exist (the owner reads this list as acceptance): dashboard load with group cards and latest commits; group page; repository page with docs, commits and manifests tabs; docs navigation with relative links resolved; commits page with pagination; search and filters; status and doctor pages; error pages with request id; dark and light theme; keyboard-only navigation; browser back/forward; offline banner and stale indicator when the API is down; polling stops in a hidden tab (`E18-STA-04`, `E18-DASH-04`).
- Crawl: `E18-CRAWL-01` follows every internal link from `/dashboard` to depth 4 against the demo world; fails on any non-2xx and on any link not generated by the builders (compares against the typed route table). `TK-5` is the Go counterpart.
- Brand and text: `E18-BRAND-01` (no foreign organisation strings; the default config carries no organisation-specific text, GR-14), `E18-TXT-01` (no "null", "undefined", "NaN", `src=""` on any page of the crawl).
- Console and CSP: every Playwright test fails on an unexpected `console.error`, a failed request, or a `securitypolicyviolation` event (listener installed by the fixture; `E18-SEC-01/02`).

### 8.3 Accessibility

- `E18-A11Y-01..04`: axe-core (`@axe-core/playwright` 4.13.0, WCAG 2.2 A and AA tags) on every route in the crawl in both themes and both viewports; 320 px width and 400 percent zoom with no horizontal scroll; forced-colors (`forcedColors: 'active'`) with focus visible; minimum target size 24 by 24 CSS px (WCAG 2.2 SC 2.5.8) measured by script.
- A route-level axe violation fails the PR. Violations on third-party-free pages have no allow-list; a temporary exclusion needs an issue link in the spec.
- Manual checks that automation cannot do are listed in `docs/a11y-manual.md` and run before each release (screen reader pass on dashboard and docs page, reduced-motion pass).
- Contrast: a unit test computes contrast ratios for every colour token pair in light and dark and in forced-colors fallbacks (`TestPaletteContrast` for badges in Go, `contrast.test.ts` for the SPA tokens); screenshots are not the oracle for colour.

### 8.4 Performance budgets enforced in CI (web)

From unit 18; measured on the CI runner in the `web` and `e2e` jobs. "Gate" rows fail the build; "info" rows are reported.

| Budget | Value | Mode |
|---|---|---|
| Dashboard entry JS, gzip | <= 200 KB | Gate (`budget` script on the build output) |
| Any route chunk, gzip | <= 60 KB | Gate |
| Graph chunk / graph worker, gzip | <= 70 KB / <= 15 KB | Gate |
| CSS, gzip | <= 25 KB | Gate |
| Cold `/dashboard` transfer (HTML + JS + CSS + API) | <= 350 KB | Gate (Playwright network accounting) |
| `scp.route.ready` | <= 1,000 ms | Info on PR; nightly Lighthouse trend |
| Render of a 500 KB sanitised HTML document | <= 100 ms | Gate (browser-mode test, generous 3x on CI) |
| Table of 1,000 rows, first 100 rows painted | <= 300 ms | Gate (3x tolerance) |
| Graph of 1,000 nodes and 2,500 edges, layout | <= 1.5 s; mount <= 600 ms with software GL | Gate (3x tolerance) |
| Lighthouse (performance, accessibility, best-practices, SEO) | informational thresholds 0.9 / 1.0 / 1.0 / 1.0 on dashboard and a docs page | Nightly `lighthouse` job, non-gating |

### 8.5 Visual regression: decision

Decision: no screenshot-diff gate in v1. Reasons: (1) screenshots are brittle across fonts and GPU stacks and the project has one maintainer; (2) the properties that matter are asserted directly (contrast gate, axe, forced-colors, target size, no horizontal scroll, text sanity); (3) the unit 18 design already decided against it and Storybook is not adopted. Revisit trigger: after the design is stable (two consecutive releases without a design token change), add Playwright `toHaveScreenshot` for about 4 key pages (dashboard, repository, docs, status) on Chromium `desktop` only, in a Linux container image pinned by digest, threshold `maxDiffPixelRatio: 0.002`, with baselines updated only by a labelled manual workflow. Until then screenshots are taken in e2e only as failure artifacts (`trace: 'retain-on-failure'`, `screenshot: 'only-on-failure'`).

### 8.6 What the web side does not do

No coverage percentages, no Storybook, no mutation testing, no MSW, no snapshot DOM tests, no cross-browser matrix on PR.

## 9. Performance and load tests

### 9.1 Budgets (the numbers tests assert)

Evidence column says where the number comes from; "proposed" means this plan proposes it and the owner confirms (D-T5).

| Area | Budget | Evidence |
|---|---|---|
| Cold start at 1,000 repositories, GitHub REST core calls | <= 8,010 | unit 05 `TestBudgetColdStartREST` |
| Cold start, GraphQL head sweep | <= 20 points per sweep (1 point per 50 repositories), sweep every 5 min | unit 05 `TestBudgetColdStartGraphQL` |
| Cold start, manifests | about 1,000 tree calls plus about 240 GraphQL points | unit 11 plan H |
| Steady state, manifests | about 90 REST calls plus 40 GraphQL points per hour at 1,000 repositories | unit 11 plan H |
| Item completions during cold start | about 3,000 (3 per repository); last first-pass start at 4 min 59.9 s virtual at the default pace 100 ms, concurrency 4 | unit 04 `TestColdStartThreeThousandItems` |
| Readiness after cold start | within the startupProbe budget of 10-15 min; about 5 min at 10 requests per second | unit 04, unit 16 |
| Warm start: load of 34,000 rows | measured 125 ms; gate 5x | unit 03 `BenchmarkWarmLoad34k` |
| Snapshot memory at 1,000 repositories | measured 52.1 MB, +0.12 MB per extra retained snapshot | unit 03/04 |
| Dependency graph resident | about 6 MB, ceiling 15 MB | unit 11 `TestResidentHeap` |
| Repository facts resident | about 30 MB estimate, ceiling 45 MB | unit 11 `TestResidentHeap` |
| Docs resident budget | cap 192 MiB (config `resident_budget`) | unit 10 `TestBudgetEndToEnd` |
| Container | limit 512 MiB, `GOMEMLIMIT=400MiB` | `deploy/smoke.sh` |
| Publish of a new snapshot at 1,000 repositories | measured 0.81 ms / 1.24 MB; gate <= 5 ms | unit 09 |
| Precomputed commits page | measured 3.8 us; gate <= 25 us | unit 09 |
| Merged commits page | measured 143 us; gate <= 1 ms and <= 1 MB | unit 09 `BenchmarkCommitsPage` |
| Badge render | gate <= 25 us | unit 09 |
| Overview index, 1,000 repositories | measured 3.8 ms; gate 20 ms | unit 15 |
| List 1,000 / page of 50 / full root / sitemap / site | gates 25 ms / 8 ms / 25 ms / 15 ms / 15 us | unit 15 |
| Store: PutItem 25 KB, AppendCommits 30, SavePlans 1000, ApplyOneRepo 1000 | measured 45.9 us, 80.7 us, 7.1 ms, about 70 us; gates 2-5x | unit 03 |
| Membership build / plan reconcile | measured 23 ms / 1.7-3.2 ms (1,000 repos) | unit 08 |
| Shutdown worst case | 44 s; plan ceiling 48 s; grace 45 s | unit 16 |
| HTTP p99 (loopback, load profile, warmed): badge | <= 10 ms | proposed |
| HTTP p99: API list / repository detail | <= 50 ms / <= 30 ms | proposed |
| HTTP p99: docs HTML | <= 30 ms | proposed |
| HTTP p99: `llms-full.txt` (4 concurrent cap) | <= 100 ms | proposed |
| HTTP p99: SPA shell and static assets | <= 10 ms | proposed |
| HTTP error rate under load | 0 percent 5xx; 429 only in the rate-limit test | proposed |

The first block is evidence-backed by the unit plans; the HTTP p99 rows are not in any design document and are proposals (the handler-level gates above them are what the design commits to). The proposed numbers are about 10x the handler-level gates, to absorb scheduling noise on a shared runner.

### 9.2 Benchmark gates in CI

- PR: `make bench-gate` runs the absolute gates only (`-benchtime=200ms -count=3`, take the median); a failing gate fails the PR with the measured and the gate value. Total budget 5 min.
- Nightly: `make bench-compare` runs `-count=5` and compares medians to `testdata/bench/baseline.txt` with `benchstat`; a regression above 25 percent on any gated benchmark opens an issue; two consecutive regressions page the owner (issue label `perf`).
- Baselines are refreshed manually (`make bench-baseline`) at each release on the same runner class and committed; the commit message states the reason.
- `BenchmarkBuildGraph200k` (unit 11), `BenchmarkRender` (unit 10, per document size), `BenchmarkGetParallel` (unit 03), `BenchmarkCommitsPage` (unit 09) are in the gated set.

### 9.3 Cold start test at 1,000 repositories

1. Budget arithmetic (gate, in virtual time, no wall-clock cost): `TestBudgetColdStartREST`, `TestBudgetColdStartGraphQL`, `TestBudgetEndToEnd`, `TestCommitsBudgetColdStart` (exactly one list-commits call per repository), manifests e2e call counts versus plan H, `TestBudgetJenkinsSweep`, W13 budget arithmetic in refresh. Each asserts on the fake's ledger, never on production counters.
2. Resource run (nightly, real time, pace 0): the real binary with the fake GitHub holding a 1,000-repository world (3 groups, mixed docs and manifests) and a pre-generated 34,000-row warm DB for the warm variant. Measures: time to ready, peak RSS (from `/proc`), heap after GC via the admin `/debug` equivalent `/status` memory fields, request counts. Assertions: ready within 120 s at pace 0 (proposed), peak RSS <= 400 MiB, steady-state RSS after 10 min <= 250 MiB (proposed), zero 5xx, ledger within the call budgets above.
3. Real-time pace run (weekly): default pace and concurrency, asserting readiness at about 5 min (+/- 20 percent) and that no request exceeds the rate-limit class; runs against the fake only.

### 9.4 Steady state and soak

- Steady state memory: after cold start, 15 min of scheduled refresh ticks, 5 pushes per minute through the webhook route and one reader at 5 requests per second: heap in use after GC must stay within 10 percent of its 5-minute value.
- Soak L7 (weekly, 24 h, container 512 MiB): same traffic as L1 at 20 percent; asserts no heap growth above 5 percent between hour 2 and hour 24, goroutine count within +/- 10 of baseline, open FDs flat, SQLite file size within the retention bound, no log line at level error except injected faults.

### 9.5 Load scenarios

Load runs against the real binary in a container, with the fake GitHub holding the 1,000-repository world, a load profile config that relaxes the public rate limits (public API 300 per minute per IP becomes 1,000,000) because one generator IP would otherwise be limited; a separate test (L9 below) verifies the real limits. Tool: k6 v2.3.0 as an external binary using the `constant-arrival-rate` executor and `thresholds` (proposed addition, D-T5); a Go fallback `cmd/loadgen` (about 150 lines, test-only, not shipped) reproduces L1-L3 for contributors without k6.

| Id | Scenario | Rate and duration | Pass criteria |
|---|---|---|---|
| L1 | Mixed browsing: 50 percent API list/detail, 20 percent docs HTML, 15 percent badges, 10 percent SPA shell and assets, 5 percent commits pages | 100 rps, 10 min | p99 within 9.1 rows, 0 5xx, RSS flat |
| L2 | Badge storm: 500 rps on 50 distinct badges, 60 percent conditional requests (expect 304) | 5 min | badge p99 <= 10 ms, 304 share within 5 percent of requested, CPU one core under 70 percent |
| L3 | Snapshot-poll fleet: 200 clients polling the overview every 30 s with `If-None-Match` | 10 min | 304 share >= 99 percent in steady state, p99 <= 10 ms |
| L4 | Webhook burst: 200 valid signed deliveries per second for 30 s, 2 percent invalid signatures, one 26 MiB oversize negative | 30 s plus drain | valid deliveries 202 or 503 with `Retry-After` (never 5xx otherwise, never 400 for a valid one), invalid 401, oversize 413 before reading the body; 4-slot body semaphore holds (concurrent body reads <= 4, measured by an admin counter); queue drains within 60 s; no duplicate work (delivery-id dedupe) |
| L5 | `llms-full.txt` concurrency: 20 clients | 2 min | at most 4 in flight (the rest 503 with `Retry-After` or queued per design), p99 <= 100 ms for served, memory flat |
| L6 | Reads during cold-start publishes: L1 at 50 rps while the cold start publishes snapshots | cold start | no read observes a partial snapshot (torn-read check: every response's `rev` is monotone and every list equals the snapshot of that `rev`), p99 within budget |
| L7 | Soak | 24 h weekly | 9.4 |
| L8 | SSE (v1.1): 4 streams per IP, 200 IPs simulated through `X-Forwarded-For` behind the trusted-proxy setting | 10 min | the fifth per IP is refused, heartbeat cadence holds, drain on SIGTERM closes all within 5 s |
| L9 | Limits are real: default config, one IP, requests above each class limit | 2 min | 429 with `Retry-After` above the limit, never before it; webhook and probe classes exempt as designed |

Regression of the load results is tracked as a nightly trend (JSON summary in the artifact); L1-L3 are the only load tests that run nightly, L4-L6 and L8-L9 run weekly and on release, L7 weekly.

## 10. Security tests

### 10.1 XSS and HTML sanitisation

- Corpora: kernel `safehtml` 67 payloads, docs render 43 XSS and mXSS payloads, ADR baseline 36 (floor, see 4.5). Entry ids are unique across the three; `TestCorpusHasNoDuplicatePayloads` (new) runs the union through all three sanitiser entry points.
- Tests: `TestCorpus` (docs), `TestCorpusIsSafeAndAFixpoint` (safehtml), `FuzzRender`, `FuzzSanitizeFixpoint`, `TestPolicyGolden` (the allow-list dump, owner-reviewed), differential allow-list walk (walk the output tree and verify every element, attribute and URL scheme is on the allow-list, independent of the sanitiser's own code), bomb tests (depth, width, attribute count, entity expansion; worst accepted case 0.95 s, race job scaled), adversarial timing (non-race job asserts absolute times).
- Defence in depth: stored HTML is sanitised on write and re-sanitised on load (`TK-9`, `TK-10`: a tampered stored row is neutralised on read); the SPA component renders it inside the one audited component under Trusted Types (`E18-DOC-05`, `C18-DOC-03`, Trusted Types e2e that injects a `<script>` into the API response through the fake and asserts it is never executed and a CSP violation is not even needed).
- Real corpora: 13 real READMEs and the 40-file realistic corpus produce goldens and are checked for link rewriting, image rewriting and anchors; they are licence-checked (4.4).

### 10.2 SSRF

- `platform/netguard` tests: every URL in the ~60-URL corpus (IPv4 and IPv6 literals in all spellings, decimal, octal and hex IPs, IPv4-mapped IPv6, `localhost` variants, link-local and cloud metadata addresses, unusual ports, userinfo tricks, trailing dots, uppercase hosts, Unicode/IDNA lookalikes, schemes `file`, `gopher`, `ftp`, redirects to private addresses) is rejected unless on the explicit allow-list; `FuzzCanonical` asserts that canonicalisation is idempotent and that a canonical URL that passes is never private.
- DNS rebinding: a test resolver returns a public address first and a private address second; the dial-time guard (the check happens in `DialContext` on the resolved IP, not on the hostname) must reject the second connection (`TestDNSRebindingBlockedAtDial`, new, uses a fake resolver).
- Media proxy and image rewrite in docs: `TestRewriteImagesNeverFetchesPrivate`, tarpit origin test (a loopback server that accepts and never answers; request must abort at the timeout and release resources).
- Jenkins and Snyk clients: `TestRedirectToOtherHostIsPinned`, redirect to a login page treated as an error (unit 14), proxy environment variables ignored (`TestClientIgnoresProxyEnvironmentAndDoesNotFollowRedirectsByDefault`).
- Descriptor and bridge fixtures: a repository descriptor that points at an internal URL is never fetched.

### 10.3 Webhook signature vectors

Table in `TestInvalidSignatureMatrix` and seeds of `FuzzVerify`:

| Vector | Expectation |
|---|---|
| Official GitHub vector (secret `It's a Secret to Everybody`, body `Hello, World!`) | valid, `TestSignVector` |
| Same with upper-case hex digest | rejected or accepted exactly as the design states (decision recorded in the test: hex is case-insensitive on decode, compare is constant-time on bytes) |
| `sha1=` prefix only, `X-Hub-Signature` present, no `-256` | `InvalidSignature` (SHA-1 ignored) |
| Missing header, empty header, `sha256=` with empty digest, short digest, non-hex digest, digest with whitespace, doubled header | `InvalidSignature` for all |
| Valid signature over a different body (one byte changed, extra trailing newline) | `InvalidSignature` |
| Secret rotation: new and previous secret configured | either verifies (`TestVerifyPushAndRotation`) |
| Empty configured secret | startup refusal; verifier constructor error; never verifies anything |
| Invalid JSON with a bad signature | `InvalidSignature`, never `Malformed` (verify first, parse last) |
| Valid signature over malformed JSON | `Malformed` (400) |
| Oversize body (26 MiB) | 413 before the body is read; with the 4-slot body semaphore full: 503 with `Retry-After` |
| Body never logged; unauthenticated headers sanitised in logs | `TestBodyIsNeverLoggedAndUnauthenticatedHeadersAreSanitised` |
| Duplicate delivery id inside the window (15 min, 10,000 ids) | accepted once; second answered 202 `duplicate`; eviction past 10,000 | `TestDeliveryDedupe` |
| Failed enqueue is not recorded, so redelivery is accepted | `TestFailedEnqueueIsNotRecordedSoRedeliveryIsAccepted` |
| Unknown repository, unknown event, `ping` | acknowledged and counted as ignored, nothing refetched (`TestEveryEventKindIsHandledOrCountedIgnored`) |

### 10.4 Content-Security-Policy, headers and browser isolation

- Golden CSP string per response class (HTML shell, API JSON, rendered docs, badges, error pages, admin, webhook). The exact document policy is in architecture section 11.3; the goldens (`TestHeaderGolden`, `TestPublicChainGoldenPerClass`) are byte-compared and need owner review to change.
- Rollout states: report-only to enforced is a config switch; both states are tested (`TestCSPReportOnlyThenEnforced`, new), including `/csp-reports` ingestion with size limit and rate limit. Trusted Types is last: tested in the e2e only when enabled.
- Every Playwright route fails on a CSP violation event (8.2).
- No `Access-Control-*` header anywhere; `OPTIONS` answers 405 (`TestPublicChainGoldenPerClass`, `ValidateResponse`); `Origin`-bearing cross-site `POST` to a mutating route is refused by `http.CrossOriginProtection`, with the documented bypass list (the two webhook paths and `/csp-reports`) tested positively and negatively.
- Other headers per class: `X-Content-Type-Options: nosniff`, `Referrer-Policy`, `Permissions-Policy`, `Cross-Origin-Opener-Policy`, `Cache-Control` per class, HSTS only when configured behind TLS.

### 10.5 Admin plane and management routes

`TestAdminBearer` (token compare constant time, missing and wrong token), `TestPublicHasNoManagementRoutes`, `TestAdminProbesOnTheAdminListenerNeedNoToken`, `TestPprofRoutesNeverOnPublicMux`, `TestAccessNotOnAdmin`; `Schemathesis` auth-class matrix (7.3); a route enumeration test walks the public mux and fails on any pattern containing `debug`, `pprof`, `metrics`, `echo`, `env`, `threads` (GR-17).

### 10.6 Identity and access

- `E2E-spoof` (new): `X-Forwarded-For`, `X-Real-IP`, `Forwarded`, `X-User`, `X-Forwarded-User`, `Remote-User` and `Authorization` headers on the anonymous class must not change any response; trusted-proxy setting honours exactly the configured hops.
- `E2E-enum-anon` (the anonymous enumeration test, TK-4, `TestAnonymousResponsesAreByteIdenticalToAPublicOnlyDeployment`): the same crawl over every route against (a) a deployment with private repositories present and anonymous access and (b) a deployment whose world contains only the public repositories; all response bodies and headers (minus request id and date) must be byte-identical, and counts (list totals, sitemap size, search results, badge answers, 404 versus 403 versus 401) identical. Also at count level over `ViewClass`.
- `E2E-enum-etag` (documented expected failure R-1): ETag and conditional-request side channels can reveal that a private item exists; the test is committed as an expected failure (`t.Skip` with the issue link for R-1 is NOT allowed, so it runs and asserts the known leak exactly, failing when the leak changes) so the residual risk stays visible.
- `E2E-authed-all`: authenticated class sees all repositories; `TestAuthorizerMonotone`; visibility uniform 404 (`TestVisibilityUniform404`, `TestHandlerHidesPrivateRepoFromAnonymous`).
- Row records carry no e-mail and only documented keys (`TestRowRecordHasNoEmailAndOnlyDocumentedKeys`, GR-35).

### 10.7 Rate-limit classes

`TestRateClassesTable` (new name for the unit 06/02 table test): each class (public API 300 per minute per IP, badges, docs, llms, admin, webhook exempt, probes exempt) rejects the next request with 429 and `Retry-After`; classes are independent; the key is the trusted client IP.

### 10.8 Supply chain

`govulncheck` on source (PR) and on the built binary (nightly `vuln-binary`), `go mod verify`, licence allow-list check for Go (`go-licenses` report against the allow-list) and for npm (`license-checker` against the allow-list), `pnpm` cooldown and pin check (`check-pins`: no dependency younger than the configured cooldown, exact versions), Renovate config validator, `actionlint`, `shellcheck`, `goreleaser check`, image scan (grype or trivy) in `image`, SBOM generation in release. A dependency update PR goes through the same gate (GR-44).

### 10.9 Secrets

Credential canary: a unique canary token is set as the GitHub App private key id, a webhook secret and an admin token in the e2e and in the fake; after the full e2e run the logs, `/metrics`, `/status`, `/doctor`, every error body and the SQLite file are scanned for the canary (`TestNoSecretInLogs`, credential-leak canary in unit 05, `E2E-canary-scan` new in `cmd/scp`). `Secret` never prints (`TestNeverPrints` for `%v`, `%+v`, `%#v`, `%s`, JSON, slog attributes); errors carry no credential (`TestErrorsCarryNoCredential`); no credential in URLs (`TestNoCredentialInQuery`, ledger `Violations`); `Reveal` has a fixed allow-list of 7 call sites (archtest).

### 10.10 Other

Path traversal: `TestPathEscape`, `FuzzEscapeRepoPath`, static file serving only by exact embedded name (`TestFileServedUnderExactNameOnly`); command execution: `os/exec` banned in product code except the bounded `git` bridge (archtest) with hardened-environment tests in unit 10 bridge fixtures; XML: `TestNuGetEntityIsError`, `TestNuGetDeepXML`, `TestNuGetExpansionBomb`, `TestDockerArgBomb`, `TestYAMLAliasCycle`, HCL crash shapes in a subprocess (`TestHCLCrashShapes`).

## 11. Chaos and fault-injection scenarios

Each scenario has an id, a fault, the expected behaviour and where it runs. "VT" means in-process in a synctest bubble with the fake GitHub; "BIN" means the real binary in the e2e harness; "NIGHT" means nightly only.

| Id | Fault | Expected behaviour | Runs |
|---|---|---|---|
| C-01 | Primary rate limit exhausted mid cold start (`primary_limit:1h`, with and without `X-RateLimit-Reset`) | All GitHub calls stop until reset; work resumes in order; no item lost; `/status` shows the limit and reset time; readiness unaffected after first sync; ledger shows zero calls during the wait (`TestPrimaryLimitRecovery`) | VT |
| C-02 | Secondary limit with and without `Retry-After` (`secondary_limit:60s`) | Delay honoured, then growth of the pause on repeated hits (`TestSecondaryLimitGrowth`), concurrency reduced, no storm after the pause | VT |
| C-03 | Token expiry mid sweep, `bad_credentials`, `mint_fails:403` | The installation token is re-minted once (singleflight), other callers wait; permanent failure marks the provider degraded and keeps serving last-good data (`TestTokenExpiryMidSweep`) | VT |
| C-04 | 5xx burst (`status:503`) | Retry with jittered backoff; breaker opens only for transient classes at the configured ratio, half-opens, closes; 404 and 403-permission never trip it (`TestBreakerOpensOnTransientOnly`, `TestBreakerRatioTrip`) | VT |
| C-05 | Timeouts (`latency:2m`) | Each request cancelled at its timeout; the worker is released; no goroutine leak (`TestRequestTimeout`, goleak) | VT |
| C-06 | Truncated tree (`tree_max_entries`, `truncated: true`) | Fall back to directory walk for the needed paths; manifests marked partial; documented limit reached is reported, not hidden (`TestTreeTruncatedFallsBackToDirectoryWalk`, new) | VT |
| C-07 | Truncated or malformed bodies (`truncate:100`, `malformed_json`, `wrong_api_version`, `skew_date`) | Decode error classified permanent-for-this-item; item stays at last-good; counter increments; clock skew surfaced by doctor (`TestClassify`, `TestProviderStatusMatrix`) | VT |
| C-08 | Webhook delivery lost (`DropNext`) | The scheduled reconcile picks the change up within one planner interval (`TestWebhookLossHealedByReconcile`, new) | VT |
| C-09 | Webhook duplicates, delays, reordering, redelivery | Idempotent: final state equals the world; one fetch per coalesced key; dedupe by delivery id (`TestWebhookRoundTrip`, `TestPushWebhookRefreshesWithinSeconds`) | VT |
| C-10 | Push payload without path lists or tree id (`payload_style`), force push, more than 2,048 commits | Fallback to a fresh fetch; ring rewritten on force push (`TestForcePushRewritesRing`); no unbounded loop | VT |
| C-11 | Installation suspended, removed, permissions reduced | Repositories of that installation go to a held state; nothing deleted until the mass-removal guard passes (30 percent rule); `TestSuspendedInstallation` | VT |
| C-12 | Repository renamed, transferred, deleted mid sync | A late write for a removed item is rejected (zombie Put returns `Gone`); redirects followed once; catalogue updated at next reconcile (`late Put after Remove` amendment test) | VT |
| C-13 | Catalogue list fails or returns nothing | Last good catalogue kept; "everything vanished" is held and flagged (`TestServiceListFailureChangesNothing`, `TestPlanEverythingVanishingIsHeld`) | VT |
| C-14 | SQLite file replaced by garbage; truncated at page boundary; page corruption; newer schema version; schema drift | Corrupt file quarantined (renamed with timestamp), fresh database created, `/status` shows quarantine, service runs cold; newer schema is quarantined and the runbook says downgrade equals cold start | VT-free, real file |
| C-15 | Disk full (`PRAGMA max_page_count` small) | Writes fail with a typed error; read model keeps serving; store reports degraded; recovery when space is freed | real file |
| C-16 | Read-only filesystem, unwritable data dir | Start refuses with a clear error in strict mode, or runs memory-only when `store.optional` is set; smoke runs the container read-only and non-root | BIN |
| C-17 | Crash mid sync: `SIGKILL` of the real binary at seeded points: inside the `PutItem` transaction, during `SavePlans`, during `VACUUM INTO` backup, during a migration | After restart: no quarantine for a `kill -9` (this tests the inference that NORMAL synchronous plus WAL survives a process crash), planner state repaired from stored plans, no duplicated or half-written rows, `PRAGMA integrity_check` ok | BIN, NIGHT |
| C-18 | GitHub unreachable with a warm DB | Ready through the warm path; data served with stale indicator; recovers when GitHub returns (`deploy/smoke.sh` FULL: warm restart with GitHub down) | BIN |
| C-19 | GitHub unreachable on a cold start | Never ready, `/readyz` 503 with reason, `/livez` 200, no crash loop, retries with backoff | BIN |
| C-20 | Queue full | Webhook answers 503 with `Retry-After`, delivery id is not recorded, redelivery works (`TestQueueFullAndUnknownKind`) | VT |
| C-21 | A job that ignores cancellation (stuck) | Worker marked stuck, `/livez` fails after the stuck threshold so the orchestrator restarts; shutdown proceeds after grace (amendment tests: stuck job and `/livez`, lifecycle plan with a non-cancelling stage) | VT |
| C-22 | Slow-loris on headers and bodies, oversize bodies, many idle keep-alives | Read-header and read timeouts close the connection; resources bounded; other clients unaffected | BIN |
| C-23 | Mass-removal guard | Removing more than 30 percent of the catalogue in one reconcile is held until confirmed by two consecutive reconciles | VT |
| C-24 | SIGTERM during sync; double SIGTERM | Graceful stop within the 44 s worst case (ceiling 48 s, grace 45 s); second signal exits promptly; readiness false at once; SQLite closed cleanly; exit code asserted | BIN + VT |
| C-25 | Jenkins or Snyk down, slow, redirecting to login, returning HTML instead of JSON | Provider state `Degraded` with reason; other pages unaffected; nothing blocks the planner (`TestErrors` jenkins, `TestRedirectToOtherHostIsPinned`) | VT |
| C-26 | deps.dev timeout and cancel | `TestDepsDevTimeoutAndCancel`; enrichment absent, base manifests data intact (`TestEnrichJobsAbsentWhenNoProvider`) | VT |
| C-27 | Manifest bombs (XML entity expansion, deep nesting, YAML alias cycle, huge lockfile, Dockerfile arg bomb), parser watchdog abandonment | Parser abandoned at its budget, circuit opens for that parser, ledger shows no GitHub amplification; process memory bounded (`TestParserBombs`, `TestExpandBomb`, `TestInputsAtScaleStayLinear`) | VT / unit |
| C-28 | Memory pressure: `GOMEMLIMIT` at the container value with a 1,000-repository world and docs at the resident cap | RSS bounded, docs evict by budget, no OOM kill (smoke FULL, nightly resource run) | BIN |
| C-29 | Clock skew between host and GitHub (`skew_date`) | Doctor warns above the threshold; rate-limit reset interpreted from header epoch, not local clock alone | VT |
| C-30 | Config reload or invalid config at start | Start refuses with all violations listed with lines and hints (`TestAllViolationsWithLinesAndHints`) | unit |
| C-31 | Fake GitHub returns inconsistent data (tree says file exists, contents 404) | Item marked per-file missing, not whole repository failed | VT |
| C-32 | Concurrency chaos: random fault injection (seeded) over a 10-minute virtual run with 200 repositories | Invariants hold: no lost enabled item, ledger within budget plus retry allowance, no goroutine leak, readiness state machine legal transitions only (`TestChaosSeeded`, new, runs 20 seeds on PR, 500 nightly) | VT |

Harness notes: C-17 and C-22 use a Go test driver (`cmd/scp/chaos_test.go`, build tag `chaos`, not in the PR `go` job) that starts the real binary, waits for a progress marker through `/status`, and kills at a seeded point; the seed list is printed. The tag keeps the multi-minute tests out of the PR gate, but a 2-seed subset runs in the `e2e` job.

## 12. Upgrade tests

### 12.1 SQLite schema migrations

- For every released schema version N, a database file `testdata/v<N>.db` created by that release is committed (small: demo world). `TestMigrateFromEveryVersion` opens each file with the current code, runs the migrations, verifies `PRAGMA integrity_check`, the full `storetest` read assertions on migrated data, and that a second open performs no migration.
- A migration that fails in the middle leaves the file untouched (transactional); tested by injecting an error at each statement index (`TestMigrationFailureIsAtomic`).
- Schema drift: a file whose schema differs from the recorded fingerprint (manual edit) is quarantined (C-14).
- Newer schema than the binary: quarantine-and-rebuild; downgrade equals a cold start and the runbook says so; the test asserts the quarantined file is preserved byte for byte.
- The `v1.db` fixture is created at the first release and every release adds one; the release workflow fails if `testdata/v<current>.db` is missing.

### 12.2 Configuration

- `version: 1` strict decode; unknown keys are errors (`TestStrictDecodeRejects`); the shipped examples all lint (`TestExamplesLint`); schema equals decoder (`TestSchemaAgreesWithDecoder`); reference documentation generated from the schema equals the committed file (drift check).
- Catalogue membership report: `scp config lint --repos <listing>` prints, for a fixed set of catalogue inputs and repository listings, which repositories each group matches and which patterns match nothing; the reports are byte-compared goldens, and the catalogue built by the service from the same input equals the report (`TestConfigLintExitCodes` and the cross-checks of unit 08 section 12.5).
- A config written for version N must load in N+1 or fail with a message naming the unsupported version and the release-notes section that describes the change (`TestOlderConfigVersionMessage`, new once a second version exists; until then the test asserts the rejection text for `version: 0` and `version: 2`).
- Environment variable names: the `SCP_*` set is listed in a golden (`TestEveryConfigKeyIsConsumed` ensures no key is decorative).

### 12.3 API and SPA

- OpenAPI is additive-only inside a major version: `oasdiff breaking` against the previous tag (7.1); enums are open (clients must tolerate unknown values: a TS test feeds unknown enum values and asserts the UI shows them verbatim, no crash).
- Descriptor schema v1 is lenient: unknown fields ignored, unsupported version reported; `FuzzDescriptor`.
- Stored link and cursor formats: cursors carry a version byte; an old cursor is rejected with 400 and `code=cursor_invalid` and the client restarts the list (`TestDecodeCursorOldVersion`, new).

### 12.4 Derived data versions

- Parser version bump: stored blobs remain; the parser re-runs from the SQLite blob cache without GitHub calls (`TestParserVersionBumpReparsesFromBlobCache`, new; asserts zero ledger calls).
- Docs pipeline version bump: rolling re-render of stored documents from stored sources, bounded per tick; readers never see a half-rendered set (`TestPipelineVersionBumpRollingRerender`, new).
- Stored sanitised HTML is re-sanitised on load, so a policy tightening applies to old rows immediately (`TK-9`).

### 12.5 Build and deployment

- Generated-source drift (`go generate`, `pnpm gen`, OpenAPI bundle, route table, schema docs) then `git diff --exit-code` in job `gen`.
- Image rollback: the smoke runs version N image against a data directory written by N+1 only where N+1 declares itself compatible; otherwise it asserts quarantine-and-cold-start (`smoke.sh` UPGRADE mode, new, nightly).
- Rolling upgrade of the config file while the binary runs is not supported (restart); a test asserts that SIGHUP is ignored and logged once.

## 13. CI stage mapping with time budgets

### 13.1 Pull request pipeline (`ci.yml`, all gating, all required through the `gate` job)

| Job | Content (tests from this plan) | Limit |
|---|---|---|
| `go` | `go vet`, `golangci-lint`, `archtest`, `TestDepguardInSync`, unit + integration with `-race -shuffle=on -timeout 10m ./...`, `CGO_ENABLED=0` build and test pass, fuzz regression seeds, fuzz PR (30 s per touched target, at most 5 min), bench gate (absolute), coverage floors, the `sqlite` sub-job (storetest x 3 backends with `-race`, about 3 min), budget-arithmetic tests, `unshare -n` offline run of the same tests (new, D-T4) | 25 min |
| `gen` | `go generate` and `pnpm gen` drift, OpenAPI bundle, route table, schema docs, `git diff --exit-code`, `check-pins`, `actionlint`, `shellcheck`, `goreleaser check`, Renovate config validation | 15 min |
| `api` | redocly lint and bundle, `apicheck` + mutations, `keycheck.py`, `oasdiff`, contract mock + Schemathesis 3 seeds, route table tests | 15 min |
| `web` | `tsc`, `eslint`, format, Vitest, browser-mode tests, build, `verify-dist`, `budget`, `check-test-ids`, contrast tests | 20 min |
| `build` | `go build -tags spa` (single binary), `check-binary.sh`, `govulncheck` source, licence checks, `goreleaser` snapshot | 20 min |
| `image` | container build, image scan, `check-final-stage.sh`, `deploy/smoke.sh` BASIC (read-only, non-root, `--memory 512m`, `GOMEMLIMIT=400MiB`, SIGTERM exit about 6 s) | 25 min |
| `e2e` | Playwright (Chromium, desktop and phone), a11y, Schemathesis against the real binary (read-only), `E2E-enum-anon`, `E2E-spoof`, credential canary scan, `chaos` 2-seed subset, smoke FULL | 30 min |
| `gate` | requires all of the above | 2 min |

Total PR wall-clock is the longest path (about 30 min) because jobs run in parallel; total compute about 160 min.

### 13.2 Nightly (`nightly.yml`)

| Job | Content | Limit | Gating |
|---|---|---|---|
| `fuzz` | sharded native fuzz, 8 min per target, SHARDS=8 (D-T1), crashers as artifacts | 120 min per shard | Failure opens an issue; blocks the next release |
| `vuln-binary` | `govulncheck` on the built binary, image scan | 30 min | Failure opens an issue |
| `api-extended` | Schemathesis fresh seed, stateful phase, 10 min (`--max-time 10m`) | 60 min | Non-gating, issue |
| `lighthouse` | Lighthouse CI on dashboard and docs page | 30 min | Non-gating, trend |
| `canary` | public-repo manifests canary (about 200 repositories) with the scalibr oracle | 30 min | Crashes gating for the job; differences reported |
| `perf` (new) | `bench-compare` (baseline delta), cold-start resource run (9.3), load L1-L3 (9.5) | 45 min | Regression above 25 percent opens an issue |
| `chaos` (new) | build tag `chaos`: C-17 crash matrix, C-22 slow-loris, C-32 with 500 seeds, upgrade smoke (12.5), full cross-browser Playwright | 60 min | Failure opens an issue; blocks release |
| `offline-mutation` (optional, D-T6) | automatic mutation run on `internal/kernel` and `internal/refresh` | 60 min | Non-gating |

### 13.3 Weekly and on demand

| Job | Content | Limit |
|---|---|---|
| `live` | `-tags live` GitHub smoke against one read-only organisation; GraphQL schema check; fidelity comparison with the recordings | 20 min |
| `soak` | L7 24 h soak; L4-L6, L8, L9 load scenarios | 26 h |
| `mutation-check` | `scripts/mutation-check.sh` hand-made mutations of the units that carry them | 40 min |
| `fixtures-verify` | `make fixtures-verify` (scrub invariants, licences) | 5 min |

### 13.4 Release (`release.yml`)

| Job | Content | Limit |
|---|---|---|
| `release` | reproducible build, SBOM, signature, full `go` + `web` + `e2e` + `chaos` (all gates must be green on the tag commit), `testdata/v<N>.db` presence check, `oasdiff` vs previous, benchmark baseline refresh commit check | 40 min |
| `promote` | pull the built image, smoke BASIC + FULL + UPGRADE, tag `latest` only if green | 30 min |

### 13.5 Flake policy

A test that fails and then passes on retry is a defect. CI never retries tests. A flaky test is quarantined within one working day by moving it to `//go:build flaky` with an issue link (the build tag is excluded from `go` and included in the nightly `chaos` run so it keeps being exercised), and fixed within a week; the number of quarantined tests may not exceed 5 (archtest counts them). Playwright `retries: 0` on PR; `retries: 1` on nightly with the retry reported as a flake.

## 14. Traceability matrix

How to read: each row maps a design guardrail (GR-nn from `02-design-guardrails.md`) to the tests and mechanisms that fail if the guardrail is violated. A name followed by (new) is introduced by this plan; other names are quoted from the unit designs or from the guardrail's own test list. "Layer" uses L0-L5 of section 2. Where the mechanism is not a test it is named as in architecture section 20: AT (architecture test in `internal/archtest`), LINT, TYPE, CI, DESIGN. The rule for completeness is `TestTraceabilityComplete` (new, in `internal/archtest`): it parses the GR ids from the copied guardrails document (`docs/design/02-design-guardrails.md`) and this table's machine-readable twin `docs/test-traceability.csv` (generated from this section, see appendix B) and fails when an id has no row or a named test does not exist (`go test -list` output compared).

### 14.1 Design guardrails

| ID | Guardrail (short) | Tests / mechanism | Layer |
|---|---|---|---|
| GR-01 | One composition root and an explicit startup sequence | single `main` package, `TestRealTree` (one entry point), AT dependency table, `TestRunServesAndStops`, `TestServeAndStop`, `TestRunStageOrder` (new: the e2e `run()` test lists every stage in order) | L0, L4 |
| GR-02 | Readiness is gated on data | `TestReadinessStateMachine`, e2e `run()` answers 503 until the catalogue gate opens | L1, L4 |
| GR-03 | Dependencies are explicit and lifecycles are owned | AT: no package-level mutable state (rule in `TestRealSource`: `var` of non-error, non-sentinel types), `forbidigo`, goleak in `TestMain`, `TestEveryGoroutinePackageHasGoleak` (new), `bodyclose` lint, `TestWarningsAreRankedAndDeterministic`, `TestJobsStaleIsMeasuredAgainstTheIdleBackoffInterval`, ledger-driven status tests | L0-L1 |
| GR-04 | Nothing exists without a consumer; nothing ships half-built | `unused`, `deadcode`, `TestCheckCatchesViolations`, `TestEveryConfigKeyIsConsumed`, `TestEnrichJobsAbsentWhenNoProvider`, `TestNewChecksWiring`, `TestToneNamesAreTheCrossUnitContract`, `E18-DASH-03` (no placeholder columns); AT: no cache library imported and the store has one implementation behind its interface; AT: `scm` is vocabulary only (no interface with a single implementer, `ireturn` and review) | L0-L1, L4 |
| GR-05 | Code tells the truth: names, comments, TODOs and decisions | `revive` naming, `godot`, doc-comment and comment-length lint, `TestRealSource` todo and skip rule (a TODO needs an issue link), `TestPinsHaveDecisionIds` (new: every `replace`/`exclude` in `go.mod` and every `// pinned:` comment cites an ADR or decision id); DESIGN review for names (an automated `TestNoAmbiguousNames` is not feasible) | L0 |
| GR-06 | Documentation is generated from, or checked against, the code | `docs-drift` checks (schema reference regenerated), `TestExamplesLint`, `TestReadmeClaims` (new: the commands and flags the README lists exist) | L0 |
| GR-07 | Every entity has its own identity; relations are indexes | `TestMembershipAgainstNaiveModel`, `TestVerifyRawCaseKeyIsRejectedByTheRealStore`, value-key equality tests, entity types in `storetest`, `go vet` copylocks, `comparable` TYPE | L1 |
| GR-08 | Behaviour is deterministic and independent of order | `TestMembershipIsOrderIndependent`, `TestMembershipManyToManyDefaultsAndRefs`, `TestMergeEqualsNaiveSortPaged`, `TestVerifyRawCaseKeyIsRejectedByTheRealStore` | L1 |
| GR-09 | Repository matchers are typed and anchored | `TestMatcherTable`, `TestMatcherRejects`, `FuzzMatcherCompile`, `TestConfigLintExitCodes` | L1 |
| GR-10 | Git refs are resolved from the repository | ref-default tests (an empty ref sends no `sha`/`ref`), `TestRealRunnerMatchesRunTree` | L1-L2 |
| GR-11 | Domain, wire and view types are distinct; one encoder | `ValidateResponse` on every handler test, `encoding/json`-only depguard, AT: domain packages do not import `httpapi`, `E18-TXT-01`, `E18-DASH-03`, `TestServiceReconcileLifecycle`, `TestBaseModelOnEveryPage` (new), view-struct tests | L0-L2, L4 |
| GR-12 | One typed, strict, validated configuration | `TestStrictDecodeRejects`, `TestSchemaAgreesWithDecoder`, `FuzzConfigDecode`, reordered-keys golden, `TestEveryConfigKeyIsConsumed`, `TestPrecedence`, `TestListFromEnvAndGap`, `TestAllViolationsWithLinesAndHints`, `FuzzParseConfigDuration`, `TestDurationParsing` (new, from the `FuzzParseConfigDuration` seeds), `TestExamplesLint`, `--config` flag test | L1 |
| GR-13 | Secrets and credentials are a type and never leave headers | `TestNeverPrints`, `FuzzSecretRedaction`, `TestErrorsCarryNoCredential`, `TestNoCredentialInQuery`, ledger `Violations`, credential canary, `TestNoSecretInLogs`, `E2E-canary-scan` (new), `TestStaticAndExternal`, `TestEmptySecretIsUnset` (new, kernel), startup refusal tests, git bridge environment test (token through the configured environment, never argv), dependency check (no browser-automation module), `TestAppTokenMint` and `doctor` credential tests | L0-L4 |
| GR-14 | Defaults are neutral; branding and integrations are configuration | `TestDefaultsHaveNoForeignHosts`, `E18-BRAND-01`, `TestDashboardCommitCountIsConfig` (new), AT: no HTML-scraping dependencies | L0-L1, L4 |
| GR-15 | One route table; links are generated, escaped and crawled | `TestBuildersRoundTrip`, `TestLinksResolveToRoutes`, `E18-CRAWL-01`, `TK-5`, `TestResolution`, `TestWiringAgainstTable`, `TestRouteTableEqualsBundledPaths`, `FuzzEscapeRepoPath`, branch-with-slash e2e spec (new if absent), lint rule against string-built hrefs (`no-restricted-syntax`) | L1-L4 |
| GR-16 | HTTP responses are truthful: status, type and charset | `E18-ERR-01`, `TestHandlerHidesPrivateRepoFromAnonymous`, `TestReadyzBodies`, `TestRequestShapeRejections` (webhook 400 only for malformed requests), `TestEncodingMatrix` (UTF-8, UTF-16 BOM, Latin-1), content-type assertions in `TestPublicChainGoldenPerClass` | L1-L2, L4 |
| GR-17 | The management plane is separate; nothing reflects requests | `TestAdminBearer`, `TestPublicHasNoManagementRoutes`, `TestAdminProbesOnTheAdminListenerNeedNoToken`, `TestPprofRoutesNeverOnPublicMux`, route enumeration (10.5) | L1-L4 |
| GR-18 | Browser security policy is strict by default | `TestHeaderGolden`, `TestPublicChainGoldenPerClass`, `E18-SEC-01`, `E18-SEC-02`, CSP violation listener, `ValidateResponse` rejects `Access-Control-*`, `OPTIONS` 405 test, `pnpm` check: no external hosts in the built HTML | L2, L4 |
| GR-19 | One reproducible asset build; only its output is served | `verify-dist`, webapp bad-dist rejection tests (source map, orphan), `TestFileServedUnderExactNameOnly`, `gen` drift check, AT: no `os.Open` in handlers, `TestWebappParsedOnce` (new: `embed.FS` parsed once), `TestSlug`, `TestFaviconAndIconsServed` (new), image-type check in `verify-dist` (extension equals sniffed type) | L0-L1, L4 |
| GR-20 | Webhook ingress: limit, verify, then parse | `TestSignVector`, `TestVerifyPushAndRotation`, `TestInvalidSignatureMatrix`, `FuzzVerify`, `TestBodyIsNeverLoggedAndUnauthenticatedHeadersAreSanitised`, verify-before-parse ordering test, startup refusal and verifier constructor tests, `TestDeliveryDedupe`, `TestRequestShapeRejections` | L1, L4 |
| GR-21 | Webhook events are hints that become coalesced refresh jobs | `TestReact` (commits), docs and manifests reactor tests, `TestAcceptedPushReachesSink`, `TestWebhookRoundTrip`, `payload_style` variants, `TK-7`, `TK-8`, `TestEveryEventKindIsHandledOrCountedIgnored`, `TestDecodeRecordedPush`, `TestUnknownRepositoryIsCountedIgnored` (new), `TestFailedEnqueueIsNotRecordedSoRedeliveryIsAccepted`, L4 webhook burst | L1-L2, L5 |
| GR-22 | One client, breaker and limiter per upstream, bounded by time | `TestBreakerOpensOnTransientOnly`, `TestBreakerRatioTrip`, `forbidigo` on per-call construction and on `http.DefaultClient`, `TestRequestTimeout`, `TestDepsDevTimeoutAndCancel`, `TestClientIgnoresProxyEnvironmentAndDoesNotFollowRedirectsByDefault`, `TestPaceAndConcurrency`, per-upstream limit tests, C-05 | L0-L2 |
| GR-23 | Upstream failures are typed and rate limits are honoured | `TestClassify`, typed-error table tests, fault recipes, `TestProviderStatusMatrix`, C-01..C-04 | L1-L2 |
| GR-24 | The job engine never loses work silently | `TestCoalesceQueuedAndDirtyWhileRunning`, `TestTransientRetryExhaustedKeepsRetrying`, `TestQueueFullAndUnknownKind`, `FuzzQueueModel` (new), `TestSchedulerJitterPanicTriggerAndFrozen`, `TestWorkerTimingInSynctestBubble` | L1 |
| GR-25 | Upstream requests are budgeted | `TestBudgetColdStartREST`, `TestBudgetColdStartGraphQL`, `TestCommitsBudgetColdStart`, `TestBudgetJenkinsSweep`, `TestBudgetEndToEnd`, `TestConditionalSweep`, `TestSharedTree` | L2 |
| GR-26 | Freshness is data; reconciliation is authoritative | `TestServiceListFailureChangesNothing`, `TestPlanEverythingVanishingIsHeld`, T-9 (pagination), C-13, C-23, `TestPlanner*`, freshness `TestDerive`, `TestStaleWhileError` | L1-L2 |
| GR-27 | Test seams live in tests | `TestRealSource` rules (route literals, skip and todo), AT: no dump-to-disk or development switches in product code and no build-tag-free test seams (`SCP_TEST_NOW` only under its tag), `TestNoTestSeamsInReleaseBuild` (new) | L0 |
| GR-28 | The read model is an immutable snapshot with write-time indexes | `TestImmutabilitySharingAndRev`, `-race` immutability property test, `TestConcurrentReadersDuringWrites`, `BenchmarkGetParallel`, `BenchmarkCommitsPage`, index bench gates, `TestMembershipAgainstNaiveModel` (indexes maintained at write), `BenchmarkApplyOneRepo1000`, AT: no cache library | L0-L1 |
| GR-29 | Absence and unsupported input are typed states | `TestNoReadmeStateRenders` (new), format text golden | L1 |
| GR-30 | Retention is bounded and measured | `TestNormalise` bounds, ring retention tests, `TestResidentHeap`, soak L7 | L1, L5 |
| GR-31 | Restart is warm | warm start tests, `BenchmarkWarmLoad34k`, smoke warm restart with GitHub down (C-18) | L1, L4 |
| GR-32 | Trusted HTML is a type minted only by the sanitiser | `TestCorpus`, `FuzzRender`, `TestCorpusIsSafeAndAFixpoint`, `FuzzSanitizeFixpoint`, `TestPolicyGolden`, `TK-9`, `TK-10`, `E18-DOC-05`, `C18-DOC-03`, Trusted Types e2e, and every test of 10.1 | L1-L4 |
| GR-33 | Badges are rendered locally with structural XML escaping | `TestGoldenSVG`, `FuzzRenderWellFormed`, `TestHandlerUnknownAndHidden`, `TestBadgeCacheHeaders` (new), AT: no badge proxy dependency | L1-L2 |
| GR-34 | Untrusted documents are parsed safely and within limits | `TestNuGetEntityIsError`, `TestNuGetDeepXML`, `TestParserBombs`, `TestExpandBomb`, XXE fixtures | L1 |
| GR-35 | Access is an explicit policy; personal data is minimised | `E2E-enum-anon`, `E2E-authed-all`, `TestAuthorizerMonotone`, `TestAnonymousResponsesAreByteIdenticalToAPublicOnlyDeployment`, startup refusal tests, `TestVisibilityUniform404`, `TestRowRecordHasNoEmailAndOnlyDocumentedKeys`, SQLite schema golden has no e-mail column | L1-L4 |
| GR-36 | No embedded interpreters; render once per content hash | archtest and `forbidigo`: no `os/exec` outside the bridge, bridge fixtures, `BenchmarkRender`, goleak | L0-L1 |
| GR-37 | Document rendering is faithful to the source | format text golden, `TestRenderBasics`, link and image rewrite tests, `TestLinksResolveToRoutes`, `TestNoReadmeStateRenders` (new), format detection table (`TestDetectCaseInsensitive`, new), `FuzzDetectType`, `E18-SEC-01` (no external hosts), server-side highlight golden | L1, L4 |
| GR-38 | Times are stored in UTC and shown with an explicit zone | `TestTimestampsAreUTC`, `TestZeroTimes`, `TestNormalise` (no date), `<time datetime>` component test | L1 |
| GR-39 | The frontend is accessible and purpose-written | axe matrix `E18-A11Y-01..04`, jsx-a11y lint rules, `TestPaletteContrast`, `contrast.test.ts`, `budget` (CSS <= 25 KB), no font assets (check in `verify-dist`) | L0, L4 |
| GR-40 | Live updates respect visibility and show staleness | `E18-STA-04`, `E18-DASH-04`, `refetchIntervalInBackground` tests, `useNow` tests, L3 load (304 share) | L4-L5 |
| GR-41 | Health derives from outcomes; probes are local and binary | `TestReadyzBodies`, `TestReadinessStateMachine`, `TestLiveness`, `TestStatusAccessorsMakeNoNetworkCall`, AT: the ops package holds no client, `TestNewChecksWiring` (disabled providers report `StateDisabled`), `TestToneNamesAreTheCrossUnitContract`, `TestJobsStaleIsMeasuredAgainstTheIdleBackoffInterval`, `TestWarningsAreRankedAndDeterministic`, metric-name golden (`TestMetricNamesGolden`, new) | L0-L2 |
| GR-42 | Logs are structured and correlated | `TestChainHeadersRequestIDAndLog`, structured-log line goldens, error page shows the request id (`E18-ERR-01`) | L1-L4 |
| GR-43 | Tests assert, run offline and exercise the shipping code | P1-P4 and their enforcement: archtest rules, `offline` job (new), `usetesting`, `thelper`, `skip-needs-issue` rule and printed skip count; goleak in `TestMain`, `-race`, no fixed ports (archtest bans literal `:8080`-style listeners in tests, new); e2e `run()` tests use the real router and startup; `TestFixtureLicences`, small recorded fixtures with goldens, `TestNoLargeFixtures` (new, 1 MB limit) | L0-L1, L4 |
| GR-44 | CI is the verifier and gates every change | the `gate` job (required for dependency PRs as well), `TestWorkflowGateRequiresAllJobs` (new, parses `ci.yml`), Renovate config validation | L0 |
| GR-45 | Artefacts are reproducible, immutable and verifiable | `go build -tags spa` single binary, `check-binary.sh`, `check-final-stage.sh`, smoke BASIC, image smoke, digest-pin check in `check-pins`, version and commit ldflags test (`TestVersionBaked`, new), `go mod verify`, vendoring policy check, `govulncheck`, licence allow-list, `TestTLSConfigDefaults` (new: no `MinVersion` below the default and no `InsecureSkipVerify`), `gosec`/`forbidigo` on `InsecureSkipVerify` | L0-L1, L4 |

### 14.2 Capability coverage

Every in-scope capability listed in architecture section 21 has a user-visible behaviour covered by a Playwright journey in 8.2 or an API test, plus the Go test named in its unit design. The compact rules:

| Class | Rule | Test form |
|---|---|---|
| User-visible capability | one E2E journey or API test per capability; named `TestCapability<Name>_<behaviour>` only where no unit test already covers it | L2-L4 |
| Internal capability | covered by the unit test named in the unit design traceability table | L1 |
| Deliberately absent surface | covered by the guardrail row that forbids it (for example GR-17 for management routes on the public listener, GR-14 for foreign hosts in defaults) | L0-L2 |

`TestCapabilitiesCovered` (new, `docs/` CSV twin) fails when a capability has no test reference.

## 15. Per-unit test inventory

For each unit: what exists in its design (counts quoted), what this plan adds, and which fixtures and CI jobs it needs. Detailed names live in the unit design.

| Unit | Existing in design | Added by this plan | Key fixtures |
|---|---|---|---|
| 01 kernel | 95 top-level tests (176 with subtests), 3 fuzz, 5 benchmarks, coverage floor 90 percent per package, safehtml corpus 67 payloads | `TestEmptySecretIsUnset`, `TestCorpusHasNoDuplicatePayloads`, golden helper `testkit/golden` | corpus files |
| 02 platform | about 73 tests (+16 planned), 8 fuzz targets | `TestDurationParsing`, netguard DNS-rebinding test, `TestRateClassesTable`, `TestTLSConfigDefaults` | netguard URL corpus (about 60) |
| 03 store | storetest 46 subtests x 3 backends, readmodel `TestModelBased`, quarantine, corruption, disk-full tests, benchmarks with gates, coverage floors | `TestMigrateFromEveryVersion`, `TestMigrationFailureIsAtomic`, chaos C-14, C-15, C-17 | `testdata/v<N>.db` per release |
| 04 refresh | 62 tests + 2 fuzz (+12 planned: budget arithmetic W13, model-based queue fuzz, `-race` stress, e2e `run()`, M0 recorded deliveries), `TestColdStartThreeThousandItems`, 7 mutation checks | `FuzzQueueModel`, `TestBackoffBounds`, `TestChaosSeeded`, chaos C-08, C-20, C-21, C-23 | recorded webhook deliveries |
| 05 github adapter | about 40 tests + 45 to write + budget tests, fuzz 4 (+6 planned), canary scan, Reveal archtest, live smoke, GraphQL check, version matrix | `TestTreeTruncatedFallsBackToDirectoryWalk`, `TestUnknownRepositoryIsCountedIgnored`, `TestWebhookLossHealedByReconcile`, close CD-FX-01..05 | recorded REST and GraphQL fixtures, fake world |
| 06 httpapi | 46 tests + about 25 to write, 6 proposed fuzz targets, CSP goldens, `apitest` helpers | `TestBaseModelOnEveryPage`, `TestCSPReportOnlyThenEnforced`, `E2E-spoof`, `TestFaviconAndIconsServed` | header goldens |
| 07 openapi | apicheck 27 rules x 32 mutations, drift checks, route table tests, SSE synctest tests | `apitest.ValidateEvent`, `TestAPIClassTableEqualsOpenAPI`, `FuzzApicheckRules` | mutation corpus |
| 08 catalog | 50 tests + 3 fuzz + T-1..T-18, lint and membership-report goldens, mutation checks | `TestOlderConfigVersionMessage` | catalogue config corpus and repository listings |
| 09 commits and badges | merge tests, restore tests, V-xx mutation checks, 19 SVG goldens, bench gates, integration budget tests | `TestBadgeCacheHeaders`, `TestBadgeWidthsMatchBrowser` (planned in unit, scheduled M-Test-3) | SVG goldens, width table |
| 10 docs | 43 XSS payloads, adversarial timing, real corpus (13 READMEs) + realistic corpus (40 files), SSRF corpus about 60, tarpit origin, bridge fixtures, `TestBudgetEndToEnd`, `TestLinksResolveToRoutes` | `TestPipelineVersionBumpRollingRerender`, `TestNoReadmeStateRenders`, `TestDetectCaseInsensitive` | READMEs with licences |
| 11 manifests core | 97 tests, `TestResidentHeap`, `BenchmarkBuildGraph200k`, canary, e2e call counts versus plan H, goleak planned | `TestParserVersionBumpReparsesFromBlobCache`, goleak rule | tree fixtures |
| 12 ecosystems A | 281 test executions in six ecosystem packages (484 in `./internal/manifests/...`), 44 golden trees, 23 fuzz seeds, `TestInputsAtScaleStayLinear` (18 shapes), tier-2 oracle, `SOURCES.tsv` | none, close acceptance criteria 1-9 | golden trees |
| 13 ecosystems B | 27 parser ids, eight invariants, `TestEncodingMatrix`, `TestHCLCrashShapes` (subprocess), provider tests, regression tests listed in 10.10 | `TestNoSecretInLogs` extended to enrichers | `_handwritten` provider fixtures |
| 14 integrations and access | `TestIdentify`, `FuzzIdentify`, `TestAuthorizerMonotone`, jenkins `TestErrors`, snyk tests, `TestRedirectToOtherHostIsPinned`; to write: `E2E-enum-anon`, `E2E-enum-etag`, `E2E-authed-all`, `E2E-spoof`, `TestAccessNotOnAdmin`, `TestBudgetJenkinsSweep` | chaos C-25 | recorded Jenkins and Snyk fixtures (M0) |
| 15 live, ops, overview | `TestStreamHelloInvalidateHeartbeatDrain`, readiness and liveness tests, `TestRetryHandler` (18 cases), `TestAliasRedirect`, llms and sitemap tests, bench gates | `TestMetricNamesGolden`, SSE per-IP cap test | llms and sitemap goldens |
| 16 cmd and seed files | cmd 46 tests, bridge 11, archtest 9, pipeline checks (actionlint, shellcheck, goreleaser check, Renovate validator) | `TestRunStageOrder`, `TestVersionBaked`, `E2E-canary-scan`, `TestWorkflowGateRequiresAllJobs`, `TestTraceabilityComplete`, `offline` job, chaos harness | e2e world |
| 17 web foundation | 144 tests in 21 files, 2 browser tests, 62 Playwright runs | `check-test-ids`, `contrast.test.ts` | design tokens |
| 18 web pages | 145 tests in 14 files, 70 E18 ids, 48 C18 ids, `E18-CRAWL-01`, `E18-BRAND-01`, `E18-TXT-01`, `E18-SEC-01/02`, `E18-A11Y-01..04` | cross-browser nightly | demo world and the generated 1,000-repository world |

## 16. Deviations proposed, discrepancies, risks, spikes and milestones

### 16.1 Deviations proposed

None of these changes the package structure, the dependency directions or an ADR decision; they add tools, jobs or wording. Each is proposed, not applied.

| Id | Proposal | Evidence | Needed from the owner |
|---|---|---|---|
| D-T1 | Raise nightly fuzz `SHARDS` from 4 to 8 (13 targets per shard x 8 min = 104 min, inside the 120 min limit), or keep 4 shards and use 5 min per target for targets beyond the first 56. | About 100 targets x 8 min = 800 min against 4 x 120 = 480 min; the seed `nightly.yml` comment itself says to raise SHARDS when `make fuzz-list | wc -l` exceeds 56. | Choose option; cost is about double the nightly runner minutes. |
| D-T2 | ADR-016 and architecture text say "36-payload sanitiser corpus"; change to "at least 36" and record that the implemented corpora hold 67 (kernel) and 43 (docs). | Unit 01 and unit 10 counts; 4.5 | Approve wording. |
| D-T3 | Align tool pins: Schemathesis 4.29.4 in `ci.yml` to 4.31.0 (units 07 and 17 and the registry say 4.31.0, verified 2026-10-14); Playwright 1.63 in units 17/18 to 1.64.0 (registry). | Section 0 | Approve both bumps (through `check-pins`). |
| D-T4 | Add the `offline` CI job (`unshare -n` around `go test`, build and tests pre-fetched). GR-43 and its row in architecture section 20 name it, but neither `ci.yml` nor the Makefile has it. | Architecture section 20; seed files | Approve (adds about 6 min of parallel compute). |
| D-T5 | Add k6 (external tool, v2.3.0) for the load scenarios of 9.5, plus the proposed HTTP-level p99 budgets, which no design document states; add `cmd/loadgen` as a no-k6 fallback. k6 is AGPL-3.0 (to be confirmed); it is never linked or shipped, so the licence only matters for the contributor tooling guidance. | 9.1 | Confirm budgets; confirm k6 is acceptable as a contributor tool. |
| D-T6 | Optional non-gating nightly automatic mutation job on `internal/kernel` and `internal/refresh` (go-mutesting or gremlins); unit 08 already suggests it. | Unit 08 | Approve or drop. |
| D-T7 | Observation, no change: unit 06 observed that `http.ServeMux` answers `//` paths with a 307 redirect, architecture amendment V9 makes no claim about it. The golden of `TestPublicChainGoldenPerClass` records the observed behaviour. | Unit 06 | None. |

### 16.2 Discrepancies found while preparing this plan

| Id | Discrepancy | Resolution in this plan |
|---|---|---|
| X-1 | Sanitiser corpus sizes differ between ADR-016 (36), unit 01 (67) and unit 10 (43). | Floors, unique ids, union test (4.5, 10.1); D-T2. |
| X-2 | Schemathesis pin: `ci.yml` 4.29.4 versus units 07/17 4.31.0. | D-T3. |
| X-3 | Playwright pin: units 17/18 1.63 versus registry 1.64.0. | D-T3. |
| X-4 | `unshare -n` offline job named as the GR-43 mechanism but absent from CI seed files. | D-T4. |
| X-5 | Nightly fuzz capacity (480 min) is below demand (about 800 min). | D-T1. |
| X-6 | Unit 11 lists goleak as "planned"; ADR-016 says goleak in `TestMain`. | Made a rule with an archtest (6.2). |
| X-7 | Unit 05 has no code asset: the adapter is only signature-checked in the prototypes, so its test names are designs, not running tests. | Counted as targets; M-Test-1 requires the prototype of the fake GitHub to run. |
| X-8 | No document gives HTTP-level latency SLOs; only handler-level gates exist. | Proposed in 9.1; D-T5. |
| X-9 | The architecture's 14-target limit per shard is computed against 56 targets; the fuzz inventory has grown to about 100. | D-T1. |
| X-10 | Fuzz target names in 5.2 for units 01, 03, 06, 07, 09, 10, 11, 14, 15 and 16 are partly proposed names (not quoted from a unit plan); the unit owner may rename, `make fuzz-list` is authoritative. | Appendix B lists generated artefacts, not hand-maintained ones. |

### 16.3 Risks

| Id | Risk | Mitigation |
|---|---|---|
| R-T1 | The fake GitHub drifts from real GitHub and the whole suite passes against fiction. | Recorded fixtures, fidelity register F-1..F-10, weekly live smoke, GraphQL schema check, version matrix. |
| R-T2 | Virtual-time tests hide real-time problems (GC pauses, scheduling, SQLite latency). | At most 15 real-time tests (6.3 item 10), nightly resource run, soak L7. |
| R-T3 | Race detector with cgo only on the test side hides cgo-free build issues. | `CGO_ENABLED=0` test pass (3.1), image smoke on the release binary. |
| R-T4 | Playwright flakiness erodes trust in the gate. | `retries: 0` on PR, deterministic clock and locators, flake quarantine policy (13.5), traces on failure. |
| R-T5 | `E2E-enum-etag` documents a known residual leak (R-1) and could be mistaken for a pass. | The test asserts the exact known behaviour and fails if it changes either way; listed in the release checklist. |
| R-T6 | Golden updates become a rubber stamp. | CODEOWNERS on security goldens, PR template question, `-update` refused in CI. |
| R-T7 | Counts in this plan decay. | `TestFuzzInventory`, `TestTraceabilityComplete`, count re-check at M-Test-3, CI summary shows layer totals. |
| R-T8 | Load tests on shared runners are noisy. | Budgets about 10x handler gates, trend not threshold for nightly, dedicated runner for weekly soak when available. |
| R-T9 | The `SCP_TEST_NOW` seam is a test hook in product code (GR-27). | Only compiled with the `e2e` build tag; archtest asserts no such symbol in the default build (`TestNoTestSeamsInReleaseBuild`, new). |

### 16.4 Open spikes

| Id | Question | Exit criterion |
|---|---|---|
| S-T1 | Does `kill -9` at the seeded points leave the modernc SQLite database consistent without quarantine (the inference behind C-17)? | 200 seeded kills, 0 integrity failures; else raise `synchronous` or document recovery. |
| S-T2 | `httptest.NewTestServer` and the fake GitHub together in a bubble at 1,000 repositories: memory and time. | `TestBudgetColdStartREST` runs under 60 s wall clock with `-race`. |
| S-T3 | k6 licence (AGPL-3.0, to confirm) and an installation recipe for contributors; agreement of the Go fallback's results with k6. | Decision recorded. |
| S-T4 | Can the Playwright e2e run the full 70 x 2 specs inside the 30 min budget with the fake GitHub on one runner? | Measured run at M-Test-3; otherwise shard the job. |
| S-T5 | Unit-level open items from the designs that block specific tests: CD-CF-10, CD-MT-06 (histogram buckets), CD-DM-03 (route-literal archtest allowance), CD-FX-01..05, W20 (recorded real push delivery), W21 (modernc `mode=ro` and `temp_store`), W22 (Planner, Gate, Timeout, reactor and lifecycle prototypes). | Each closed or accepted before its dependent test row is called done. |

### 16.5 Out of scope

Penetration testing by an external party, formal verification, browser-compatibility testing of Safari older than the supported matrix, localisation (only `en-GB` is shipped), testing against GitHub Enterprise Server (not in v1), and any test of third-party services themselves.

### 16.6 Milestones for the test effort

| Milestone | Content | Done when |
|---|---|---|
| M-Test-0 | Test kit and gates: `internal/testkit` (golden, leaks, race scale), archtest rules P2/P4, `offline` job, `gate` job, summary script | Empty repository passes all L0 checks and a sample test per layer runs in CI |
| M-Test-1 | Fake GitHub with ledger and fault recipes, demo world, `storetest` on 3 backends, synctest cold-start budget test | `TestColdStartThreeThousandItems`, `TestBudgetColdStartREST` and conformance green |
| M-Test-2 | Close CD-FX-01..05, recorded fixtures (W20), contract jobs (`api` job with mock and Schemathesis seeds), webhook vectors, sanitiser corpora, SSRF corpus | Section 10 rows 10.1-10.3 green; `api` job green |
| M-Test-3 | e2e: `run()` tests, smoke BASIC and FULL, Playwright foundation and pages, a11y, enumeration tests, canary scan; count re-check against section 2 | Section 2 layers within 15 percent of target; `E2E-enum-anon` green |
| M-Test-4 | Nightly: sharded fuzz (D-T1), perf compare, chaos, canary, Lighthouse, extended Schemathesis | Two consecutive green nightlies |
| M-Test-5 | Upgrade tests with `v1.db`, weekly live smoke and soak, release job | Two consecutive green weekly runs; first release candidate passes the release job |

## Appendix A. Commands

```
make test-go          # CGO_ENABLED=1 go test -race -count=1 -shuffle=on -timeout 10m ./...
make test-nocgo       # CGO_ENABLED=0 go test -count=1 ./...
make test-offline     # unshare -n -r make test-go-prebuilt   (D-T4)
make arch             # internal/archtest
make fuzz-list        # authoritative fuzz target list (feeds TestFuzzInventory)
make fuzz-pr          # 30 s per touched target
make fuzz             # nightly: one shard, 8 min per target
make bench-gate       # absolute gates (PR)
make bench-compare    # delta against testdata/bench/baseline.txt (nightly)
make api-check        # redocly, apicheck, keycheck, oasdiff
make api-schemathesis SEED=7
make fixtures-record  # needs SCP_RECORD_TOKEN, never in CI
make fixtures-verify
make smoke            # deploy/smoke.sh BASIC; FULL=1 for the extended run
make e2e              # build with -tags spa,e2e, start fake GitHub, run Playwright
make chaos            # go test -tags chaos ./cmd/scp -run Chaos -seeds 20
make load PROFILE=l1  # k6 or cmd/loadgen
make mutation-check   # hand-made mutations
go test ./path -run TestName -update          # golden update (refused when CI=true)
SCP_TEST_SEED=123 go test ./path -run TestName  # reproduce a property or chaos failure
```

## Appendix B. Generated artefacts that keep this plan honest

| Artefact | Generated from | Checked by |
|---|---|---|
| Fuzz target list (counts per package) | `make fuzz-list` | `TestFuzzInventory` compares with section 5.2 counts (tolerance: exact per package once M-Test-3 is reached) |
| Traceability CSV (`docs/test-traceability.csv`) | section 14.1 of this file | `TestTraceabilityComplete` verifies that each GR id has a row and that each named test exists in `go test -list` or the Vitest/Playwright listings (`vitest list`, `playwright test --list`) |
| Capability coverage CSV | capability list of architecture section 21 | `TestCapabilitiesCovered` |
| CI summary | `go test -json`, Vitest and Playwright JSON reporters, benchstat output | printed in the job summary (P13) |
