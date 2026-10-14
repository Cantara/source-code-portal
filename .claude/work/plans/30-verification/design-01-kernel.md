# Verification report: design unit 01 (kernel: scm, freshness, job, reactor, routes, safehtml)

Verifier: independent (not the author). Date: 2026-10-14. Document: `20-design/01-kernel.md` plus companions `01-kernel.assets/` (reference module) and `01-kernel.routes.tsv`.
Scratch copies: the session scratchpad `verify-01-kernel/` (Go 1.27.1 and go1.27.2, darwin/arm64; linux/amd64 vetted only).

Result: **22 findings (V-01, V-11 and V-18 withdrawn), 21 fixed in the document and the reference module, 1 tracked as a task (V-24: sibling units hold snapshots of the first-draft kernel).** Two were high (V-02 the documented startup wiring panics; V-22 the admin route table lacked 21 of the 24 admin API operations), three medium (V-03, V-04, V-05; V-12 is low to medium). Before: `go vet` clean, `go test -race` green. After: `go vet` clean (also GOOS=linux GOARCH=amd64), `go test -race -count=3 ./...` green, also under go1.27.2, coverage scm 99.0%, freshness 100%, job 99.0%, reactor 100%, routes 90.4%, safehtml 99.6%, three fuzz targets re-run clean with a stronger oracle, `01-kernel.routes.tsv` equal to a fresh `go run ./cmd/routestsv`. The other units' documents were not touched.

## (a) Conformance with the architecture and ADRs

Import directions checked with `go list` against matrix 5.2 (not by reading alone):

| Package | Imports (internal) | Third party | Matrix | Verdict |
|---|---|---|---|---|
| `scm`, `freshness`, `routes` | none | none | none | conforms |
| `safehtml` | none | `microcosm-cc/bluemonday` (+ `x/net/html` in `_test.go` only) | none / bluemonday | conforms |
| `job` | `freshness` | none | `freshness` | conforms |
| `reactor` | `job`, `scm` | none | `job`, `scm`, `platform/logx` | D5 (documented deviation: logger arrives in `Deps`); stricter than the matrix, no cycle |

Also checked: no `log`, no `os/exec`, `net/http` only in `routes`, no `init`, no mutable package variable (G5), `time.Now` only in `reactor` (durations), no `Clock` interface (ADR-003). Signatures of architecture 8.1, 8.2, 8.3, 8.10, 8.13 compared with `go doc`: every architecture name exists with the architecture's shape; the differences are the additive ones recorded as D4-D10 (`job.Spec.Run` is the named type `RunFunc`, to which a plain func is assignable). A grep for every `scm.`, `freshness.`, `job.`, `reactor.`, `routes.`, `safehtml.` symbol in the eleven other design documents and their assets found no symbol the kernel lacks.

Two conformance defects were found against the architecture and sibling units, not against the matrix: V-02 and V-22 below.

## (b) Completeness

Sections 1-15 are present and concrete (16 is the author's record, 17 the new verification log, plus two appendices). No "TBD"/"decide later". Every open question has a default (Q1-Q4, Q7-Q9; Q5 and Q6 withdrawn). Added by this verification: D11, D13, D14, Q8, Q9, T-row updates, R6, K12, TK-15.

## (c) Guardrail traceability

Every guardrail of `02-design-guardrails.md` was compared with the trace tables 11.1-11.2. Guardrails with kernel relevance that were missing are now added with a mechanism (V-16): GR-04, GR-05, GR-09, GR-12, GR-13, GR-16, GR-17, GR-18, GR-26, GR-40, GR-41. Features with no kernel content are listed as such.

## (d) Technical correctness (executed)

- Baseline and final runs listed above; `go vet`, `gofmt -l` clean; benchmarks re-run (membership build 1.4 ms, reads 74 ns, sanitise 37 MB/s): the document's numbers hold. The route-table benchmarks were measured on an intermediate table with a different row count and are not quoted here; the reference figures for the final 64-row public table are those of design 5.2 (build about 30 microseconds, `Verify` about 35 microseconds), both one-off startup costs with no budget attached.
- Live web (2026-10-14): module proxy: bluemonday v1.0.27 (2024-07-04) is still latest, x/net v0.61.0 (2026-10-09), chroma v2.27.0 (2026-06-17), oapi-codegen v2.8.0 (2026-07-17); go.dev/dl lists go1.27.2 (the document said it was unavailable: V-25). OSV query: no advisory for bluemonday 1.0.27, douceur 0.2.0, gorilla/css 1.0.1.
- oapi-codegen v2.8.0 re-generated from a two-path document: the `ServeMux` interface is `HandleFunc(string, func(w, r))` plus `http.Handler` (the Recorder satisfies it); `{id...}` becomes `{id___}` and `PathValue("id___")` only when the parameter is named `id...`; with a parameter `id` it errors (V-21, D1 clarified).
- `runtime/pprof` lists `goroutineleak` by default in 1.27.1 and `net/http/pprof` serves it (claim in 7.2 true).
- API equality: table vs OpenAPI documents compared with `$ref` resolution: public 22 = 22, admin 24 = 24 (after V-22; before: admin 3 vs 24).
- Sanitiser: about 45 vectors outside the 60-payload corpus (DOMPurify-class mXSS, `plaintext`, `listing`, `annotation-xml`, `select/template/style`, entity-encoded schemes, `srcdoc`, `usemap`, `formaction`, `base/link/meta`) neutralised; bombs re-run at the 8 MiB limit.

## (e) Testability

Every listed defect class has at least one named test: commits without a date or author `TestCompareCommits`, event classification (GR-21) `TestEvent` (tag, empty branch, deletion), visibility (GR-35) `TestVisibilityZeroValueIsNotPublic` and `TestClassed`, rename handling (GR-07, GR-21) `TestSlugForMatchesByIDAfterARename` (+ new `TestSlugForRefusesAReusedName`), refusal propagation (GR-21) `TestRefusalIsPropagatedAndDoesNotStopOthers`, trusted HTML (GR-32) corpus + oracle + golden + fuzz, generated links (GR-15) `TestResolution` + `TestBuildersRoundTrip`. Gaps found and closed (V-23): `Absolute`, `Handle` on API routes, admin superset, empty-body and header assertions, rel/target oracle for external links, variation selectors, NaN/overflow.

## (f) Security and lifecycle

No goroutine, clock decision or global in the kernel; `-race -count=3` clean; `Table` is single-use at startup and mutex-guarded; `Fan` immutable. Findings: V-04 (webhook resolves a different repository), V-05 (invisible LLM channel), V-08 (key parts), V-09 (malformed ref reaches reactors), V-12 (external link controls), V-15 (observer panic).

## (g) Buildability

The module builds, vets and tests from a clean copy with the shipped `go.mod`/`go.sum`; adoption steps of 13.1 are mechanical. Blockers for an implementer without questions were V-02 and V-22 (the documented wiring and route table would not have worked); all fixed.

## Findings

Severity: H = a documented wiring fails or a security claim is false in a supported configuration; M = wrong behaviour users or sibling units meet; L = robustness, documentation or test gap; I = informational.

| ID | Sev | Issue | Fixed | Evidence |
|---|---|---|---|---|
| V-02 | H | `Table.Handle` panicked on API routes; architecture 11.2 and unit 06 register `api.openapi` (`httpapi.Static`) with `Handle`: probe `Handle(api.openapi)` panicked ("is an API route") | yes: `Handle` accepts any route, shared double-registration guard (D11) | `TestHandleRegistersHandWrittenAPIRoutes` |
| V-03 | M | `AfterRevalidated` forced `State = ok`; probe: `no_content` -> `ok`, also after failure | yes: result state kept; `AfterRevalidatedAs` (D13) | `TestRevalidationKeepsTheResultState`; unit 09 section 3 |
| V-04 | M | `SlugFor(ID 2, name "old")` resolved to the catalogue entry with ID 1 (probe) | yes: refused when both IDs known and different | `TestSlugForRefusesAReusedName` |
| V-05 | M | `CleanLine("a︀︁︂b")` kept the selectors; Hangul filler U+3164, U+FFA0, U+034F kept (probe) | yes: all default-ignorable and all variation selectors except FE0E/FE0F (after a base only) | `TestCleanLineVariationSelectorsAndIgnorables`, `FuzzCleanLine` 40 s |
| V-06 | L | `Policy{Jitter: NaN}.Validate()` nil; `Interval(0, NaN)` = 1 s; huge TTL platform-dependent | yes | `TestIntervalNeverMisbehavesOnNaNOrOverflow` |
| V-07 | L | `job.Class(9).String()` panics (probe) | yes | `TestClassStringIsTotal` |
| V-08 | L | `ParseKey("commits.refresh/a/\xff\xfe/c")` and U+2028 accepted (probe) | yes | `TestKeyPartsMustBeValidUTF8WithoutLineSeparators`, `FuzzParseKey` 20 s |
| V-09 | L | `NewMembership` accepted `Owner: "o/x"` (probe), later `job.NewKey` panics in reactors | yes | `TestMembershipRejectsInvalidRef` |
| V-10 | L | `ValidationError.Value` length 202 for a 5 KB input (probe), doc says 64 | yes | `TestValidationErrorValueIsCutAtConstruction` |
| V-12 | L-M | `<a href="https:///evil.example">` and `https:evil.example` survived without rel/target (probe) | yes: `href` pattern; oracle now checks rel/target of external links | `TestOpaqueAbsoluteLinksAreDropped`, 7 corpus payloads, fuzz 25 s |
| V-13 | I | `<div>x` and `</div></div>` pass unbalanced (probe); not stated | documented, pinned | SH-03 (g), R6, `TestOutputIsNotBalanced` |
| V-14 | L | Reactor name grammar documented, not enforced | yes | `TestReactorNameGrammar` |
| V-15 | L | A panicking `Observe` escaped `Handle` | yes | `TestObserverPanicDoesNotStopTheFan` |
| V-16 | L | Trace gaps (list in (c)) | yes | 11.1-11.2 |
| V-17 | L | Bomb scaling unmeasured at the limit | measured: 20 shapes at 8 MiB less 1,000 bytes, max 328 ms, linear | SH-06 |
| V-19 | L | Comment names a nonexistent test | yes | `safehtml.go` |
| V-20 | L | `Event.LogValue` printed `//` for a zero repo | yes | `TestEventLogValueZeroRepo` |
| V-21 | L | D1 evidence overstated | clarified | re-generation above |
| V-22 | H | Admin table: 3 API rows; admin document (11.4, 07 D-1, ADR-010) has 24; the author's equality script did not follow `$ref` and reported a false "equal"; unit 06 logged it as V20/Q14 | yes: `Admin()` = 22 public API rows + `api.status` + `api.admin.retry` (D14), Q9 for the owner | `TestAdminAPIIsTheSupersetOfThePublicAPI`; `$ref`-resolving script 24 = 24 |
| V-23 | L | Test-plan gaps | yes (16 new top-level tests, 76 to 92) | 12.2 |
| V-24 | I | Sibling assets (03, 06, 09, 15) contain first-draft kernel snapshots (diffed) | open: task K12, 13.1 rule 7 | `diff -rq` of each `internal/{scm,freshness,job,routes}` |
| V-25 | I | go1.27.2 now exists; vet and `-race` tests green under it | verified | `GOTOOLCHAIN=go1.27.2 go test -race ./...` |

## Not fixed (unresolved)

1. V-24 / K12: re-sync and re-run the sibling modules against the corrected kernel (`AfterRevalidated` semantics; `NewMembership` ref validation).
2. Q9: owner confirms D14 (admin listener serves the whole API) or asks to shrink the admin document (one-line change here and in 07).
3. Q8: `spa.root` on the admin listener (unit 06).
4. TK-1 (kin-openapi equality) and TK-2 (`routes.json` equality) cannot be written before `apispec` and `webapp` exist; the `$ref`-resolving comparison is the evidence.
5. Appendix B amendments to `10-architecture.md` (D4-D11, D13, D14) are proposals; the architecture was not edited.
6. linux/amd64 tests were vetted, not executed.
7. The author's open questions remain open with their defaults (owner of `/repos` routes, empty docs path, planner using `Policy.Interval`, bluemonday insurance, hidden groups).
