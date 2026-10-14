# Verification report: design unit 07 (the OpenAPI contract)

Verifier: independent (not the author). Date: 2026-10-14. Document: `20-design/07-openapi-contract.md` plus companions `20-design/api/{common,openapi-public,openapi-admin}.yaml`, `api/trusted-members.txt`, `07-openapi-contract-catalogue.md`, `07-openapi-contract.assets/` (tools, sketches, TypeScript proof).
Method: read the design, architecture 5.2, 8.13, 8.14, 11.1-11.4, ADR-009 and ADR-020, units 01, 03, 06, 08, 09; loaded, bundled, generated, compiled and ran everything from the committed files in a scratch tree (the session scratchpad `verify-07-openapi-contract/`, Go 1.27.1, darwin/arm64) with the reproducible script `tools/prove.sh`; checked the trace tables against `02-design-guardrails.md`; ran Schemathesis, oasdiff and the TypeScript proof again.

Result: **24 findings, 24 fixed in the document and its companions; 9 items remain open that need another unit or a real binary (none blocks implementing this contract).** Baseline before changes: the YAML loaded and bundled, but the design did not compile against the real unit 06 tree, a feature could not be written inside the import matrix, and the proof existed only as scratch scripts. After: `prove.sh` exits 0 (run `run5`): `apicheck: 0 failures` (27 rules, 32 mutations each fail their rule), `keycheck.py` 0 unknown keys, Redocly 2.59.0 lint clean (2.63.0 also clean and byte-identical bundle), oapi-codegen v2.8.0 generates 9 packages (11,298 lines) plus 8 alias files, `go build`, `go vet`, `go test` pass; stage 2 overlays them on the real unit 06 prototype and `go test -race` passes in 11 packages; the TypeScript proof passes under `tsc` 7.0.2.

## (a) Conformance with the architecture and ADRs

Import directions checked with `go list` on the generated and sketch code against matrix 5.2:

| Package | Imports (module-internal) | Matrix | Verdict |
|---|---|---|---|
| `httpapi/openapi` (0) | none | rank 0 | conforms |
| each `<feature>api` (1) | `httpapi/openapi` | rank 1: `httpapi/openapi` | conforms (all eight, `prove.sh` import check) |
| `live` (4, sketch) | `httpapi`, `live/liveapi` | K4 + own api package | conforms **after V-01**; before, the strict code needed `httpapi/openapi` types and the feature had no legal way to name them |
| `overview` (5, sketch `alias.go`) | `freshness`, `httpapi`, `httpapi/authz`, `readmodel`, `scm` | overview row | conforms |
| `apispec` (0) | `embed` | rank 0 | conforms |
| `tools/*` | kin-openapi, yaml | tools module | conforms |

Signatures against unit 06 and architecture 8.14: the document's section 2.3.3 named `IfNoneMatch`, `RequestError`, `ResponseError`, `FreshnessSource`, `FreshnessDigest(parts ...FreshnessSource)`. Unit 06 exports `WithRequest`/`RequestFrom`, `NotModified(r, etag)`, `StrictErrors`, `BindingError`, `FreshnessView`, `NewFreshness`, `FreshnessDigest(statuses ...string)`, `WithDigest`, `WeakETagOf`, `NextLink`, `Deref`/`Ptr` (V-02). The rewritten sketches compile against the real tree.

Architecture and ADR statements this design cannot meet as written are in `## Deviations proposed` below (D-2 owners, D-3 `{id...}`, D-4/D-13 ETag with the epoch, D-5/D-6 validator and header fold, D-14 the 308 writer).

## (b) Completeness

The 15 required sections plus Appendix A and B are present and concrete; section 15 ends with defaults for every question (Q-4 closed, Q-13 to Q-15 added). "Decide later" items that were hidden in prose now have an owner and default: the epoch source (Q-13), the 308 writer (Q-14), unit 09's import exception (Q-15). Section 2.3.3 was the one place that was complete but wrong (V-02).

## (c) Guardrail traceability

The trace tables 11.1 to 11.3 were compared with `02-design-guardrails.md` and with the unit's subject (API, freshness, conditional requests, health, visibility, badges, renderers). They lacked GR-03, GR-09, GR-10, GR-11, GR-12, GR-15, GR-16, GR-17, GR-18, GR-21, GR-23, GR-26, GR-33, GR-35, GR-37, GR-39, GR-41, and the rows for the dashboard redirect, syntax highlighting, catalogue configuration and reconciliation, default-repository and default-branch rules, commit retention, licence and release facts, the badge kinds, document rendering, probes, build info, telemetry, visibility policy and `llms.txt`; each was added to 11.1 to 11.3 with a concrete mechanism (V-19). Q-18 (no branch) is now cited from the default-branch row and GR-10.

The `Commit` description states the design choice directly: the subject is the first line of the message, and the commit URL opens the full message.

## (d)-(g) Findings

Severity: H high (wrong behaviour, unbuildable, or a security control that does not do what the document says), M medium, L low.

| ID | Sev | Issue | Fixed | Evidence |
|---|---|---|---|---|
| V-01 | H | Unbuildable inside the import matrix: the generated strict code of every feature names common types (`Problem`, `Freshness`, `NotModifiedResponseHeaders`, enum constants) that live in `httpapi/openapi`, while 2.1 forbade features to import it. A feature either violates 5.2 or cannot write its handler. Unit 09 had already proposed an exception (its D-12). | yes: `apicheck aliases` generates `common_aliases.go` (type and constant aliases) into each `<feature>api`; rule A18 also refuses a local schema that shadows a common name; 1.4, 2.1, Q-15 | `prove.sh` stage 1 import check (all `<feature>api` import only `httpapi/openapi`), stage 2: `internal/live imports: httpapi, live/liveapi`; `gofmt -l` clean on the generated files |
| V-02 | H | Section 2.3.3 and Appendix A specified an `httpapi` surface that differs from unit 06 (`IfNoneMatch`, `RequestError`, `ResponseError`, `FreshnessSource`, a `FreshnessDigest` over structs); the sketch used stand-ins and was never compiled against the real package, so every name was a duplicate or a conflict | yes: table rewritten to 06's names; three real additions remain (`AggregateFreshness`, `NonNil`, `FoldIfNoneMatch`); sketches compile and run inside a copy of `06-httpapi.assets` | stage 2 `go test -race` over 11 packages; `sketch/httpapi_additions.go`; doc 2.3.2 facts 4-7, 2.3.3, D-5 |
| V-03 | H | Stale `304` after a restart: tags are `W/"g<version>-<class>"` and `W/"v<version>"`, but unit 03 4.6 publishes snapshot version 1 at every warm start, so the same tag names different bodies in two process lives and an SPA that compares "greater" never sees the change | yes: a per-process epoch in every weak tag, inequality on the client, `resync` on the stream, D-13, Q-13, T-16, the examples and the mock constant changed | `live_test.go TestTagDoesNotSurviveARestart`, `TestStream`; `03-readmodel-store.md` 556-569 |
| V-04 | H | A client or proxy that splits `If-None-Match` over two header lines gets `400`: the generated binder accepts exactly one line (`TooManyValuesForParamError`); unit 06 4.17 accepts this for generated handlers | yes: `FoldIfNoneMatch` (first step of `WithRequest`), 2.3.2 fact 7, D-5, test | generated `live.gen.go` (`n != 1`); `TestRepeatedIfNoneMatchLines` (304 with the fold, 400 without); kin's validator accepts two lines (mock request, 200) |
| V-05 | H | The prompt-injection control was incomplete: 23 hand-placed `x-scp-untrusted` marks (the text said 24), none on `Commit.subject`, `Commit.authorName`, descriptor and status messages, document headings and other repository- or upstream-authored text | yes: rule A24 (every free-text member is marked or on `trusted-members.txt`, never both, stale entries fail), 64 members marked, 54 allow-listed with a reason, three negative tests | `apicheck` A24 fails 3 mutations; `grep` of the original showed 1+22+0 marks; 4.8 item 7, 3.5 |
| V-06 | H | `DocId` (`/` to `~`, paths containing `~` skipped) is not injective for `%`, spaces and non-ASCII bytes and silently dropped real files; a raw byte outside the pattern could not be addressed | yes: injective percent-encoding of every byte outside `[A-Za-z0-9._-]`, `~` for `/`, 1,024 limit, `doc.id-too-long`; D-3, T-17, Q-7 | `common.yaml` `DocId`; `TestRenamedRepositoryRedirects` keeps `docs~Release%2520Notes.md` intact |
| V-07 | H | Path-parameter schemas described the decoded value but kin-openapi (`gorillamux` with `UseEncodedPath`) validates the percent-encoded segment: a legal maximum-length id or any id with `%` was rejected (Schemathesis counterexample) | yes: wire-form schemas for `{repo}`, `{groupId}`, `{docId}` (3.3) | Schemathesis seeds 1-3 pass after the change; kin source read; catalogue shows the wire pattern |
| V-08 | M | The `{repo}` alias redirect was placed in `authz` (rank 3, cannot read feature slots), had no compile-checked signature, built `Location` from decoded text, would have used `WriteProblem` (clamps to 400..599, no `Location`), could redirect an alias that a new repository reused, and `listCommits?repo=` was said to "resolve silently" | yes: `overview.AliasRedirect` and `AliasSource` (architecture 4.4 and 9.1 already say `overview`), visibility first, current slug wins, escaped `Location`, own writer (D-14, Q-14), query aliases are `404` | `alias.go`, `alias_test.go` (`TestRenamedRepositoryRedirects`, `TestVisibilityFirst`) in stage 2; YAML `listCommits` description |
| V-09 | M | `Slug` and `GroupId` patterns differed from the kernel grammar (`.github`, `_`, leading/trailing hyphen, 100 characters) | yes: patterns equal `scm.Slug` and `scm.GroupID`; the path parameters parse at the edge | `common.yaml`; `apicheck` A1 examples validate |
| V-10 | M | Unknown query keys, empty values and repeated scalars were claimed rejected, but kin-openapi accepts all three and the mock did not implement them (`?limit=` returned 200 under Schemathesis) | yes: mock rejects all three; A23 for string parameters; requirement for unit 06's validator (empty values of every type) in D-6 and 4.12 | Schemathesis finding, mock log; `06-httpapi.md` D13 covers two of three |
| V-11 | M | Shared `Problem` response declared no headers although 405 needs `Allow`, 429/503 need `Retry-After` and every error repeats `X-Request-Id`; typed handlers and `apitest` could not assert them | yes: `Allow`, `Retry-After`, `X-Request-Id` declared (pointer fields, nil writes nothing); 7.4 rewritten | `common.gen.go` `ProblemResponseHeaders`; generated `default` types carry `Headers` |
| V-12 | M | The SSE `Last-Event-ID` was never specified against a restarted server (versions restart at 1, so "nothing newer" hid every change); a first revision of the sketch also ignored an id that overflows `uint64` (20 digits allowed by the pattern) | yes: `resync` when the id is ahead, in 7.6, the YAML and the sketch; overflow handled (`ErrRange`) | `TestStream` covers ahead, overflow and malformed |
| V-13 | M | The digest was described as a 16-bit hash (collision 1 in 65,536 hides a stale transition); unit 06's `Digest` is 64-bit (16 hex characters) | yes: text, tag shape and examples | `06-httpapi.md` 227-230 |
| V-14 | M | Owners disagree with unit 01's route table (`api.repos`, `api.group.repos` under `catalog`; `api.openapi` under `httpapi`); the route-table test compared names only, so the disagreement would have passed | yes in this unit: `getOpenApiDocument` owner `apispec` (A5 value set), the test compares owners, D-2 and T-4; **adoption by unit 01 is open** | `07-openapi-contract.md` 7.2; `01-kernel.routes.tsv`; A5 mutation |
| V-15 | M | Generated enum constants are named by bare value (`Critical`, `Dark`, `Public`) and are renamed with a type prefix when another enum declares the same word, so adding an enum breaks handler code on an additive change | yes: `always-prefix-enum-values` in every `oapi.yaml` and in `prove.sh` | `overviewapi` generated with and without the option and compared |
| V-16 | M | GR-35 (no e-mail address on the wire) was a trace item with no check; enum case (unit 06 L10) and `servers` (L11) had no rule here | yes: rules A25, A26, A27 with mutations | `main_test.go` |
| V-17 | M | T-3 claimed "every rule has a negative test"; none existed. Rules A5 (value sets), A12 (`Link`) and A18 (name clash) were weaker than described | yes: `main_test.go` (baseline plus 32 one-line mutations, one per failure mode) and the three rules tightened | `go test ./apicheck` passes; A24 needed an admin document for the stale-entry mutation |
| V-18 | M | The proof was not reproducible: scratch `build.sh`, `sketch/` inside the `tools` module (`go mod tidy` failed), no run against the real unit 06 tree | yes: `tools/prove.sh` in two stages, `go.mod`/`go.sum` tidy, sketches moved, `build.sh` removed | run `run5`, exit 0 |
| V-19 | M | Trace table lacked guardrails and features of the subject (listed in (c)) | yes: 11.1 to 11.3 with mechanisms | doc 11 |
| V-20 | L | `getRepoSbom` said "the ETag is the strong tree sha" while 4.5 defined a weak tag; the SBOM also depends on the parser version, which only a new process can change | yes: `W/"t<epoch>-<tree sha>-<class>"` in the YAML, catalogue and 4.5 | `openapi-public.yaml` |
| V-22 | L | 12.3 specified `apitest.Contract(...) *Client`; unit 06 exports functions (`ValidateResponse`, `ValidateProblem`, `ETagCoversBody`, `NoLeak`). A zero `time.Time` (`0001-01-01T00:00:00Z`) or the epoch passes the `Timestamp` pattern and response validation (GR-38) | yes: behaviour mapped onto 06's functions; `ZeroTimes`/`NoZeroTimes` added | `sketch/apitest_zerotime.go`, `TestZeroTimes` |
| V-23 | L | 4.2 described the cursor as a JSON object with `v` and a 32-bit filter hash; unit 06 encodes `0x01 || JSON` with a fingerprint | yes: cursor internals are unit 06's, examples are illustrative | `06-httpapi.md` 218-221 |
| V-24 | L | Stale numbers: 23 rules, 24 untrusted members, 11,061 generated lines, bundle and `schema.d.ts` sizes, line counts of the three documents | yes: recomputed (27 rules; 64; 11,298 plus 624; 4,854 and 5,588; 4,137; 436, 2,012, 356) | `run5` |
| V-25 | L | Mock and examples carried tags without the epoch, and the SBOM example text described a strong tag | yes | mock constant `W/"g4f7a1c2e-1841-0"` |

Every finding above is closed inside the unit's files. Findings V-02, V-03, V-04, V-10, V-12 and V-14 additionally need adoption by another unit (next section).

## (e) Testability

Do the named tests catch regressions of every listed defect? Mutation or execution evidence:

| Defect | Test that fails when it returns |
|---|---|
| any apicheck rule weakened | `TestEveryRuleHasANegativeTest` (32 cases; A1..A27) |
| incomplete untrusted marking | A24 mutations: unmarked member, marked and listed, stale entry |
| import matrix | `prove.sh` import check (and the archtest of 12.4) |
| epoch missing from a tag | `TestTagDoesNotSurviveARestart` |
| repeated `If-None-Match` lines | `TestRepeatedIfNoneMatchLines` (both directions) |
| alias leaks visibility, current slug redirected, `Location` mangled | `TestVisibilityFirst`, `TestRenamedRepositoryRedirects` |
| SSE resync after restart | `TestStream` |
| zero timestamps | `TestZeroTimes` (and `NoZeroTimes` in `ValidateResponse`, requested of unit 06) |
| contract and handler drift | `TestResponsesSatisfyTheContract` against the real admin bundle, `Lint` L1-L11 on it |
| route table versus bundle | pattern comparison in `prove.sh`; the owner comparison in `TestRouteTableEqualsBundledPaths` is specified and cannot pass until unit 01 adopts D-2 |
| empty or repeated query values | Schemathesis on the mock (three seeds); `TestEmptyQueryValue`, `TestRepeatedScalar` are specified for unit 06's validator |

## (f) Security, race and lifecycle

- Prompt injection: A24 makes the marking complete and reviewable (V-05). Personal data: A25 (V-16). Visibility: the alias redirect never reveals existence or the new name (V-08); `listCommits?repo=<alias>` is a plain 404.
- Path traversal: wire-form schemas plus `scm.ParseSlug` at the edge; dot segments, `%2e%2e`, `%2F`, upper case all fall through to the handler's 404 (`TestVisibilityFirst`).
- Stale 304 and mixed process lives: V-03. Header handling: V-04. Resource limits: every path and string parameter has a bound (the wire form allows three bytes per byte).
- Races and lifecycle: the documents are immutable, the redirect and handlers read one snapshot without locks, the epoch is set once before the publisher starts; `go test -race` passes in the 11 packages of stage 2; the SSE stream selects on the request context and the shutdown signal (stage 2 `TestStream` covers framing; drain and goroutine leaks stay with the `synctest` tests named in 12.5 and need the real `live` implementation).

## (g) Buildability

Everything in the design builds from the committed files: `sh tools/prove.sh <scratch>` (needs Go 1.27, npx, python3 with PyYAML) prints exit 0. Versions executed: kin-openapi v0.150.0, oapi-codegen v2.8.0 and `runtime` v1.7.0, Redocly CLI 2.59.0 (2.63.0 also), oasdiff v1.33.0, Schemathesis 4.31.0, openapi-typescript 7.13.0, openapi-fetch 0.17.0, TypeScript 7.0.2 (`latest` on npm today), `nethttp-middleware` v1.2.0. Library facts checked live: `npm view` gives typescript 7.0.2, openapi-typescript 7.13.0, openapi-fetch 0.17.0 (the pinned versions are the latest); `go list -m -versions` gives oapi-codegen v2.8.0 and kin-openapi v0.150.0 as the newest. oasdiff: an additive enum value is 25 errors (exit 1) without `diff/severity-levels.txt` and exit 0 with it; a removed path exits 1.

## Deviations proposed

Recorded in section 14 of the design with evidence; the design stays on the architecture as written.

| ID | Against | Change requested | Evidence |
|---|---|---|---|
| D-2 | 11.2 and `01-kernel.routes.tsv` | owners: `api.repos`, `api.group.repos` to `overview`; `api.openapi` to `apispec` | G7 (`catalog` cannot import `overviewapi`); owner comparison in the route-table test |
| D-3 | 11.2 `{id...}` | `docId` as one injectively encoded segment | OpenAPI templating and kin-openapi cannot route `{id...}` |
| D-4, D-13 | ADR-009 and 11.4 tags | epoch in every weak tag; digest; `repo-rev` only for single-repository bodies | unit 03 4.6 (version restarts at 1); `TestTagDoesNotSurviveARestart` |
| D-5 | unit 06 | `AggregateFreshness`, `NonNil`, header fold in `WithRequest` | `TestRepeatedIfNoneMatchLines`; generated binder |
| D-6 | 11.1 | validator refuses empty values of every type, repeated scalars, unknown keys; HEAD as GET | Schemathesis; nethttp-middleware v1.2.0 answers HEAD 405 |
| D-14 | unit 06 `WriteProblem` | the 308 is written by `AliasRedirect`; alternative: allow status 308 and `Location` in `WriteProblem` | clamp to 400..599 and the header allow-list |

## Open items (need another unit or a real binary)

1. Unit 01: adopt the three route owners of D-2 (the owner comparison fails until then).
2. Unit 03: `Snapshot.Epoch()` (Q-13, T-16); features build tags with it.
3. Unit 06: the `If-None-Match` fold inside `WithRequest`, the empty-value rule in `httpapi/validate`, `AggregateFreshness` and `NonNil`, `NoZeroTimes` in `ValidateResponse`, and the call on the 308 writer (Q-14).
4. Unit 09: withdraw its D-12 (import of `httpapi/openapi`) in favour of `commitsapi` aliases (Q-15).
5. Docs unit: the injective `docId` encoding and `doc.id-too-long` (T-17, Q-7).
6. Kernel/freshness: `Status.String()` returns exactly `fresh|stale|error|unknown` (Q-3).
7. Response sizes and gzip ratios (T-11) and Schemathesis against a real binary are unmeasured; no binary exists.
8. SSE drain, goroutine-leak and per-IP stream-limit tests need the real `live` implementation.
9. RFC 9457, 8288, 8594, 9110 statements are used as published and were not re-read in this pass.

## Run log (final, `run5`)

```
apicheck: 0 failures   unknown keys: 0   go test ./apicheck: ok
redocly lint: valid (both roots)   bundle: 4,854 and 5,588 lines   apicheck -bundled: 0 failures
oapi-codegen v2.8.0: catalog 886, commits 493, docs 910, common 782, live 745, ops 1,126, manifests 3,488, overview 2,154, pages 714 = 11,298 lines (+ 8 x 78 alias lines)
stage 1: go build, vet, test ok; operations in bundled admin document 24 = generated + root patterns 24 (both directions empty)
stage 2 (unit 06 tree, -race): e2e httpapi httpapi/apitest httpapi/authz httpapi/validate live overview routes webapp wiring web: ok
stage 2 import check: internal/live imports httpapi, live/liveapi only
TypeScript: schema.d.ts 4,137 lines, tsc 7.0.2 exit 0 (including the @ts-expect-error case)
```
