# Verification report: design unit 13-manifests-ecosystems-b

Document verified: `20-design/13-manifests-ecosystems-b.md` (revision 1 -> 1.1, 2026-10-14)
Verifier: independent of the author. The full defect table, facts table and unresolved list are also in section 19 of the design document; this report is the summary with evidence.

Verdict: **ready_with_caveats**. All defects that could be fixed inside this document are fixed. The remaining caveats depend on other units or on external facts (section 4).

## 1. Method

- Read the whole document against the 15-section structure. Re-read the binding inputs: `04-decisions.md`, `10-architecture.md` (sections 4, 5, 8, 13, 15, 16, 17, 18), ADR-021/022, `05-github-adapter.md`, `04-refresh-webhook.md`, `03-readmodel-store.md`, the core OpenAPI, research 07, and `02-design-guardrails.md`.
- Compiled and ran the unit's public surface inside a scratch copy of the core unit's reference scaffold (Go 1.27.1). This included the real `Registry.Register` over all 27 parser IDs, the three provider error types against the scaffold's `job` package, and strict decode of the yaml-tagged `Config` types with `manifests.ByteSize`.
- Ran experiments on the dependencies: hcl/v2 v2.25.0 (crash shapes and the pre-scan, 16 shapes), yaml v3.0.5 (alias cycles, multi-document, deep nesting) and graphql-js (both GraphQL queries validated against GitHub's schema).
- Live-fetched external sources: proxy.golang.org module versions, the purl-spec v1.1.0 type list, the GitHub REST description 1.1.4 (SBOM operations), deps.dev v3 responses, CocoaPods, Bundler, Composer and NuGet sources, GitHub Actions docs, and the fixture file sizes.
- Downloaded material was treated as untrusted data and kept in separate scratch directories.

## 2. Findings

| # | Severity | Issue | Fixed | Evidence |
|---|---|---|---|---|
| F-01 | high | 11 of 27 parser IDs (`ruby/*`, `php/*`, `dart/*`, `elixir/*`, `actions/*`) are rejected by `Registry.Register`, which requires the `<EcosystemID>/` prefix | yes: IDs renamed (D-8) | real registration run: 11 failures before, 0 after |
| F-02 | high | Parsers emitted repo-relative paths while the parse cache key is the blob SHA alone, so a copy of the same bytes at another path is served the first copy's paths | yes: 4.1.9 path independence, J-0 rebase, `TestParserPathIndependence`, `TestPathEscape` | architecture 13.4, ADR-021, scaffold `ParseKey` |
| F-03 | high | Both enrich jobs wrote one store item (lost update) and wrote the slot directly (an older tree could overwrite a newer one) | yes: one item per job kind, `Service.Reapply` under a per-repository lock (RR-19), D-9 | `TestEnrichItemsSeparate`, `TestEnrichRefreshRace` defined |
| F-04 | high | Providers saw merged units, so a rerun considered its own earlier additions present and the next persisted item lost them (flapping) | yes: own-view rule | `TestEnrichRerunStable` |
| F-05 | high | SBOM `generate-report` specified as POST; it is GET | yes | REST description 1.1.4; `TestSBOMMethods` |
| F-06 | high | Providers told to return `job.*` errors, but `job` is outside their import row, and one error type with `RetryAt()` would misclassify every failure as rate-limited | yes: three distinct error types (RR-20) and a status table | compiled against the scaffold's `job`; behaviour-interface classification per architecture |
| F-07 | medium | Join `Ancestors=true` gave a standalone pubspec its parent's lock | yes: option removed, membership-only ancestor lock | J-4 and test |
| F-08 | medium | `terraform/lock` not covered by the HCL pre-scan; pseudo-code used nonexistent token names | yes: real `hclsyntax.Token*` names, both HCL parsers, new tests | 11 crash shapes rejected at 100,000 in 8-80 ms; depth 256 accepted, 257 rejected; unguarded bracket nesting ends in a fatal stack overflow |
| F-09 | medium | yamlsafe: self-referential anchors accepted by the library, and only the first document is decoded | yes: `yaml.alias-cycle`, decoder loop | yaml v3.0.5 experiments |
| F-10 | medium | Second ecosystem switch beside `acquisition.disabled_ecosystems`; configs without yaml tags; polymorphic `providers[]` that env cannot drive; no `deps-dev` upstream | yes: RR-21, RR-22, flat `ProviderConfig` | strict-decode test with `ByteSize` |
| F-11 | medium | `Artifact.Kind` values `tool`, `metapackage`, `replace` are outside the core API enum | yes: RR-23 | core OpenAPI |
| F-12 | medium | `purl.RepoURL` is not the core's `purl.RepoKey` (lower-case `host/owner/repo`), so channel B would not match | yes | scaffold behaviour |
| F-13 | medium | `limits` API and limit-name mismatches (`bytes` vs `size`), text gate, done-context rule; the assembler set `ID`, `Role`, `LockedCount`, which `finalize` owns | yes | scaffold compile |
| F-14 | medium | deps.dev URL path built from untrusted names without validation | yes: `invalid_coordinate` rule and test | 4.4.5 |
| F-15 | medium | Path dependencies that name their package became anonymous `_local` targets | yes | 3.2, 4.2.5, 4.2.6 |
| F-16 | low | `blobPath` split at the first `/` breaks refs such as `feature/x` | yes: longest-suffix rule | 4.4.3 |
| F-17 | low | 14 tests named in the trace table had no definition | yes: new 12.7 | |
| F-18 | low | Trace table lacked GR-22, GR-24, GR-25, GR-26 and the reconciliation and refresh-cadence rows | yes | 11.2 |
| F-19 | low | Stale or wrong facts: deps.dev cold-start 70 vs 50 min; `isDeprecated` "not verified"; OQ-5/6/13/16 already answered | yes | live deps.dev; 17 evidence table |
| F-20 | low | A Gemfile.lock with merge-conflict markers had no diagnostic; lock-only join ignored the lock's own `DEPENDENCIES` rows | yes: `ruby.merge-conflict`, J-5 | Bundler `lockfile_parser.rb` |
| F-21 | low | 4.4.2 referred to a per-status classification table that did not exist | yes: table added | |

Totals: 21 findings (6 high, 9 medium, 6 low), 21 fixed in the document. Each finding is a distinct defect; several touch more than one place in the document.

## 3. Verified facts and claims

### Guardrail traceability

Each guardrail in the unit's scope has a mechanism in the trace table: untrusted manifests are parsed safely and within limits and never logged (GR-34, GR-42); every parsed fact has a consumer (GR-04); outbound clients have timeouts (GR-22) and the breaker, bulkhead and limiter are built once per upstream in `cmd/scp` (GR-22); upstream failures are typed by status (GR-23); credentials travel in headers only (GR-13); text is decoded with an explicit charset (GR-16); read paths use write-time indexes, not scans (GR-28); every manifest kind has a refresh policy (GR-26). Missing trace rows (GR-22, GR-24, GR-25, GR-26, reconciliation, refresh cadence) were added.

### External and library facts (V-F1..V-F15 in the document, section 19.2)

- Module versions (hcl/v2 v2.25.0, yaml v3.0.5, x/text, otel/metric, packageurl-go) from proxy.golang.org.
- purl-spec v1.1.0 type list: no `terraform` type, so the document's fallback applies.
- SBOM operations in REST description 1.1.4: sync endpoint removal date 2026-11-13.
- GraphQL enum `DependencyGraphEcosystem`, and both queries valid against the schema with 0 errors.
- deps.dev v3 fields including `isDeprecated`, 404 for an unknown package, and the `GetDependencies` ecosystem limits.
- CocoaPods `Version` ordering, Bundler sections and `CHECKSUMS`, Composer platform regex, NuGet lock v3 rules, Actions `$/path` and YAML anchors.
- Fixture sizes: 5 of 5 sampled matched.
- Rails `Gemfile.lock` numbers re-counted at the end. 269 spec lines, 233 gems, 397 edges, 82 direct, 5 platforms and 256 checksums all match the document. A raw count of the `CHECKSUMS` section gives 270 entries, but 14 of those are path gems (`actioncable (8.2.0.alpha)` and similar) that carry no `sha256=`, so the document's "256 checksums" is correct and was left as is.

## 4. Unresolved (not fixable inside this document)

1. RR-16: the adapter document (`05-github-adapter.md`) specifies no `GraphQL(...)`, `GenerateSBOMReport` or `FetchSBOMReport` method (OQ-18). Spike S-A (App-token support for graph and SBOM, OQ-4) also still gates both GitHub-backed providers.
2. The core unit's design document is absent (OQ-19). This unit is bound to the reference scaffold (section 2.5). RR-10, RR-19, RR-22 and RR-23 need acceptance by the core unit.
3. deps.dev rate limits are undocumented (OQ-12).
4. NuGet lock v3 and Swift v1/v2 `Package.resolved` fixtures are synthetic (OQ-6, OQ-9).

## 5. Checks not run

The unit's own parsers are not implemented, so compile checking covered the public surface, registry, error types, config and the experiments above. It did not cover parser logic.
