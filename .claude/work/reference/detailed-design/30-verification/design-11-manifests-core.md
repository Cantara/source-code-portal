# Verification report: design unit 11-manifests-core

Document verified: `20-design/11-manifests-core.md` (revision 1 -> 1.1, 2026-10-14) and its companion `20-design/11-manifests-core.assets/`.
Verifier: independent of the author. The defect table is also section 17 of the design document ("Verification log"); this report is the summary with evidence. Defect ids are `VF-nn` (the document's `V-n` ids are verified facts).

Verdict: **ready_with_caveats**. All 21 defects that could be fixed inside this unit are fixed in the document and in the reference code, each pinned by a regression test that failed on the first revision. The caveats (section 4) depend on other units (05, 07, 08, 13, 15) or on facts that need credentials (GitHub GraphQL batch cost, webhook payload path arrays).

## 1. Method

- Read the whole document (1,320 lines) against the 15 required sections and the binding inputs: `04-decisions.md`, `10-architecture.md` (sections 5, 8.7, 13, 16, 21), ADR-021 and ADR-022 with amendments, and the sibling designs 01 to 08, 13 and 15 where this unit relies on them (`02-platform.md` 2.6, `03-readmodel-store.md`, `04-refresh-webhook.md` 4.9, `05-github-adapter.md` 4.10, `07-openapi-contract.md` 4.5, `08-catalog.md` D-1).
- Compiled the unit's reference code (Go 1.27.1, scratch module `github.com/cantara/source-code-portal`) three ways: as shipped (against stubs), against the **real sibling packages** (kernel, `readmodel`, `store` with `sqlite` and `memory`, `httpapi`, `httpapi/authz`, `platform/config`, `apitest`), and against the **code oapi-codegen v2.8.0 generates** from the bundled contract (redocly bundle of unit 07; 3,488 lines for the `manifests` tag). The `go/ast` struct dump of the generated and the stand-in types was compared field by field.
- Ran every API test with response validation against the bundled OpenAPI (kin-openapi v0.150.0 through `apitest.ValidateResponse`); ran `config.Describe` and `config.Load` on `manifests.Config`; ran mutation copies against the new tests (one mutant survived, the test was rewritten, see VF-15).
- Checked library versions on the Go module proxy on 2026-10-14 (V-10 in the document).
- Downloaded and generated material was kept in separate scratch directories under the session scratchpad; nothing was installed outside it. Other units' documents were not touched.
- Final state: `go vet ./...`, `go test -race -count=1 ./...`, `gofmt -l` and `go mod tidy -diff` clean in the shipped assets (97 tests, up from 80); the same code is green in the real-sibling overlay.

## 2. Conformance, completeness and traceability

**Architecture and ADRs.**
- Package names and ranks match architecture 5 (`manifests` rank 4, `manifests/internal/{limits,purl,version}`, `manifestsapi`, `parsertest`). The import matrix deviations D-1 (`httpapi/openapi`), D-2 (`x/sync`), D-3 (`charmap`) are recorded in section 14; `go list -f '{{.Imports}}'` of the root shows only kernel and platform packages (`scm`, `freshness`, `job`, `readmodel`, `store`, `httpapi`, `httpapi/authz`, `httpapi/openapi`), `manifestsapi`, `manifests/internal/*`, `x/sync/errgroup`, `cyclonedx-go` and the OTel API: nothing outside the matrix plus those three.
- Section 8.7 signatures: `TreeReader.ListTree` and `BlobReader.ReadBlob` are identical to the architecture; the optional `BlobBatchReader` is D-4. `manifests.Store` is satisfied by `store/sqlite.Store` and `store/memory.Store`, and `Service.React` is a `reactor.Func` (compile proofs, `verification/assertchk_a_test.go.txt`).
- Architecture 13 row for `manifests.refresh` (PrioScheduled, planner raises a never-run key to PrioBackfill) was contradicted by the first revision (VF-05, fixed). Architecture 16 config model: the block now satisfies the real `config.Describe` (VF-07).
- New deviations proposed (section 14): D-13 (SBOM budget key `output.sbom_max_locked`), D-14 (a 40-hex tree id as `ListTree` ref). Existing D-6 key count updated to 45.

**Completeness.** All 15 required sections are present (1 purpose and packages, 2 position, 3 domain model, 4 behaviour, 5 data, 6 config, 7 HTTP, 8 concurrency, 9 observability, 10 security, 11 trace, 12 tests, 13 tasks, 14 deviations, 15 open questions) plus 0 (compile statement), 16 (verified facts) and 17 (this log). Every config key has a consumer and a `jsonschema_description`; every metric has a meaning; every task of section 13 has an acceptance test or a gate.

**Traceability.** Checked the trace table against architecture 21 and `02-design-guardrails.md`. Present with a concrete mechanism: startup refresh, refresh cadence, package manifest intelligence, push reaction, the badge kinds, the readiness probe, and GR-04, GR-07, GR-08, GR-10, GR-12, GR-20, GR-21, GR-22, GR-24, GR-34, together with the guardrail rows of section 11.2. The parser takes only the manifest files the discovery step selects, at the resolved default branch (GR-10), and never logs document content (GR-42). **Missing in revision 1 and added:** reaction to `create` events (assigned to `docs`/`manifests` by architecture 21; the unit had no handler: VF-10). The configuration guardrail (GR-12) was upgraded from a promise to a mechanism (`Describe` and `Load` proofs).

## 3. Findings

| # | Severity | Issue | Fixed | Evidence |
|---|---|---|---|---|
| VF-01 | high | A webhook-forced refresh stopped at check (0) when the catalogue's sweep had not yet updated `HeadTreeOID` (it still equalled the stored `TreeSHA`): the push was lost until the next sweep | yes: `recheck` mark set by `React`, consumed by one run (4.3, 4.7) | `TestPushRefreshBypassesStaleHeadTreeOID` fails on rev 1 |
| VF-02 | medium | `getRepoManifests` ETag ignored the graph build while the body carries `counts.internal`: a 304 could confirm a stale body | yes: graph build id in the digest (4.11) | `TestETagCoversGraphDerivedCounts` |
| VF-03 | medium | SBOM validator ignored the parser set that produced the components | yes: `Digest(ParserSet, repo identity)` | `TestSBOMETagCoversParserSet` |
| VF-04 | high | nil `Deps.Authz` was documented as "serve everything"; the real `authz.ClassOf` panics on nil and the intent would have exposed private repositories | yes: fail closed with `authz.PublicOnly{}` (10.2 rule 8) | `TestNilAuthzFailsClosed`; real `authz` overlay |
| VF-05 | low | job spec priority `PrioBackfill` contradicts architecture 13; the planner (unit 04 4.9) already raises never-run keys | yes: `PrioScheduled` | `TestRefreshSpecIsScheduledPriority` |
| VF-06 | medium | `limits.max_facts_bytes` accepted up to 64 MiB; `store.MaxPayload` caps item payloads at 16 MiB | yes: bound 64 KiB..16 MiB | `TestConfigFactsBytesFitTheStore`; real `config.Load` rejects 32 MiB |
| VF-07 | medium | no `jsonschema_description`, no `scp:"min,max"` and no `jsonschema:"minimum,maximum"` tags: the real `config.Describe` rejects the block | yes: all 45 keys tagged | `TestConfigTagsAreDocumented`; `assertchk_desc_test` |
| VF-08 | low | `WarmItem` of a row that only records a failed first attempt returned an opaque error the loop logged as corruption | yes: `ErrNoFacts` and metric `scp_manifests_warm_total` | `TestWarmItemWithoutPayload` |
| VF-09 | medium | stale-while-error was lost across a restart: a row with a failed attempt after a good value warmed as healthy | yes: `Attempts>0` and error code warm as `StateError` | `TestWarmItemKeepsFailureState` |
| VF-10 | medium | Reaction to `create` events had no mechanism; the trace table did not mention it | yes: `React` handles `EventCreate` of the tracked branch, new result `enqueued_create`; trace row | `TestReactCreateOfTrackedBranch` |
| VF-11 | medium | The truncated-tree walk read directories by path against a moving branch (a push between calls gives a mixed snapshot) and produced uncacheable calls; unit 05 4.10 resolves 40-hex ids and caches them immutably | yes: walk by tree id; D-14 | `TestTruncatedWalkUsesTreeIDs` |
| VF-12 | medium | the contract requires `Dependency.purl` and `Artifact.purl`; an ecosystem whose formatter declines produced an empty or missing value | yes: generic `purl.Build` fallback; DTO field is a string | `TestAPIPurlIsAlwaysPresent`; contract validation |
| VF-13 | medium | unit ids built from directories with spaces, `%` or more than 100 bytes violate `^[^#\s]{1,100}#[^\s]{1,300}$` and truncation would collide | yes: `idSegment` (escape, hash-truncate) | `TestUnitIDsSatisfyTheContractPattern` |
| VF-14 | medium | `Register` followed by `Disable` worked, the reverse order activated a disabled ecosystem | yes: `Registry.disabled` | `TestDisableThenRegister` |
| VF-15 | medium | `shrinkLargest` halved only the largest unit up to 6 times: 80 mid-sized units could not be stored (permanent failure, repository never published) | yes: cut every unit above half of the largest, 24 passes | `TestManyMidSizedUnitsAreShrunkToFit`; mutant of the old algorithm fails it |
| VF-16 | medium | the SBOM handler loaded every locked component (up to 5,000,000 per file, 500 units) into one in-memory document per anonymous request | yes: `output.sbom_max_locked` (default 200,000), `scp:lockedOmitted`, deterministic cut, D-13 | `TestSBOMLockedBudget` |
| VF-17 | low | the contract's artifact kind enum has `other`; the mapping collapsed unknown kinds to `library` | yes | `TestArtifactKindOther` |
| VF-18 | low | JSON `number` is `float32` in the generated types; the stand-in used `float64` and did not compile against the generated package | yes: stand-in and handlers | compile against the generated package |
| VF-19 | medium | the stubs diverged from the real sibling APIs (`NewPublisher(reg, proj, val, deps)`, `PutGlobal` returns an error, `scm.Membership` not constructible by this package, `store.MaxPayload` missing), so section 0's compile statement was weaker than it read | yes: stubs and call sites corrected; section 0 states what was proven against the real packages | overlay build, `go test -race` green |
| VF-20 | low | list keys documented with `SCP_..._0` environment names; the loader reads one comma-separated variable and silently ignores the indexed name | yes: section 6 | `config.Load` run: `..._EXCLUDE_SEGMENTS=a,b` applies, `..._0` is ignored |
| VF-21 | low | the strict-adapter sketch used a 304 shape that does not exist (`ETag: etag` on the response) and ignored that the generated SBOM 200 body is a typed struct with `AdditionalProperties` | yes: corrected sketch, hand-written `GetRepoSbomResponseObject`, OQ-7 closed | generated code of oapi-codegen v2.8.0 |

Cross-unit findings (not fixable here, **not fixed**; each is in the document's section 14 or 15 as a request):

| # | Severity | Issue | Owner | Status |
|---|---|---|---|---|
| X-1 | medium | unit 05 4.10 has no `ReadBlobs`; the cold-start cost plan (about 240 GraphQL points instead of 6,000 REST calls) depends on it. The REST fallback is complete behind the same interface | unit 05 | open (D-4) |
| X-2 | low | unit 15 `ManifestsSource.Technology(snap, slug)` has no `ViewClass`; an authenticated viewer sees an under-counted `internalDependencyCount` (never a leak) | unit 15 | open (OQ-3) |
| X-3 | low | unit 07 describes the ETag kinds of `getRepoManifests` (`repo-rev`) and `getRepoSbom` (`tree-sha`) without the digests VF-02 and VF-03 add | unit 07 | open; wording only, no schema change |
| X-4 | low | unit 13 (and its examples) use indexed list environment names (VF-20) | unit 13 | open |
| X-5 | medium | a group-level `ref` override makes `catalog.stats.HeadTreeOID` (the real default branch) differ from the ref this unit tracks (`catalog.repo.DefaultBranch`); check (0) then never matches, or could match a stale value | unit 08 | open (OQ-11); the reference is safe because check (0) is an optimisation and the TTL run reads the tree |

## 4. Unresolved and unverified

- X-1 to X-5 above.
- **Unverified (needs credentials or other units):** the cost and truncation threshold of GraphQL blob batches (architecture spike S-B, W5); whether GitHub's push payload carries per-commit path arrays and `head_commit.tree_id` (V13); both are recorded in OQ-12 with a safe default (REST fallback; `PathsComplete=false` routes every push to a full refresh).
- The generated strict-server adapter is not compiled inside this unit (the shipped handlers are hand-registered with the same behaviour); the sketch of 7.1 was checked by hand against the generated types, and the contract validation of every response was run against the bundled document. The SBOM response is not validated by `apitest` (kin-openapi has no decoder for `application/vnd.cyclonedx+json`); it is covered by the `cyclonedx-go` round trip in `TestAPISBOM`.
- Unit 12 (ecosystems A) has no document yet; unit 11 answers its requirements by reference to unit 13 (OQ-6).
- Performance numbers were re-run on the shipped code: facts 26.6 MB (document: 26.5), graph 9.7 MB, `BenchmarkBuildGraph200k` 56 ms and 38 MB (document: 49 to 52 ms; the task M3-1 acceptance of +50% still holds). One machine, one run.

## 5. Testability, security, races, buildability

- **Testability:** the exported surface of section 2.5 is exercised by the 97 tests (coverage: root 86.8%, `limits` 73.6%, `purl` 69.7%, `version` 78.0%, `parsertest` 76.8%); the fakes (`fakeGit`, `fakeStore`, `fakeEnq`) are small; the 17 new tests are table-free regression tests with one assertion theme each. The mutation check on the shrink algorithm (VF-15) showed that the first version of that test was too weak (the mutant survived), so it now uses heterogeneous unit sizes.
- **Security:** VF-04 (fail closed), VF-16 (response size bound), VF-13 (id pattern, no collisions), VF-02 and VF-03 (stale 304 would confirm a body that changed). No new network egress; no content, path or package name is logged; the SBOM still exports no private URL.
- **Races:** `go test -race -count=1 ./...` is clean, including `TestConcurrentRefreshGraphAndReads`; the new `recheck` map is guarded by `Service.mu` like `cleanOID` and `lockMiss` (8.2).
- **Buildability:** `go vet`, `gofmt -l`, `go mod tidy -diff` clean; the module versions of V-8 are the newest on the Go proxy (checked 2026-10-14, V-10); the real-sibling overlay builds and passes.

## 6. Files changed by the verification

- `20-design/11-manifests-core.md` (edited in place; section 17 appended; header, 0, 2.4 to 2.8, 4.3 to 4.14, 5.2, 5.4, 6, 7.1, 7.3, 8.2, 9, 10.2, 11, 12, 13, 14, 15, 16).
- `20-design/11-manifests-core.assets/` (all changed `.go` files of `internal/manifests`, the stubs, new `verify_test.go`, new `verification/` directory with the non-compiled real-sibling checks, `README.txt`).
- This report.
