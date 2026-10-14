# Cross-design consistency review

Date: 2026-10-14. Reviewer: cross-design consistency reviewer. Scope: the 18 unit designs in `20-design/` with their `api/` documents, fixtures and code assets, the per-unit verification reports in `30-verification/design-*.md`, against `04-decisions.md`, `10-architecture.md` and `11-adr/`.

## 1. Summary

- Findings: **63** (interface 12, config 15, route 6, metric 9, error 5, job/store 3, slot 1, dependency matrix 7, fixture 5). Fixed or resolved by a recorded decision in the documents: **52**. Recorded as requirements on units not yet written or on later work: 8. Open: 3 (`CD-CF-10`, `CD-MT-06`, `CD-DM-03`).
- Deliverables written: `20-design/90-config-reference.md` (370 keys, 24 root sections), `91-metrics-catalogue.md` (179 instrument names), `92-error-catalogue.md` (19 problem codes plus the closed `ErrCode` vocabulary), `93-deviations-and-open-questions.md` (549 unit rows indexed, 21 consolidated decision rows, owner list).
- Documents amended: every unit that needed a fix carries an in-place edit and a section "Cross-design review amendments (2026-10-14)"; `04-decisions.md` section 7 gained A-27 to A-33; `10-architecture.md` gained a revision 3 entry in the revision log (revision 2 text kept).
- The largest real defects: (1) `PlanSink` in 16 stored refresh plans under the job kind, so plans of commits, docs and manifests would never have persisted (CD-JB-01); (2) four incompatible `ErrCode` vocabularies, two of which mapped rate limits to a catch-all (CD-ER-01); (3) two units each owning the reserve arithmetic (CD-CF-02); (4) 05 had no `ReadBlobs` although 11 requires it (CD-IF-01); (5) the architecture assigned `api.repos` to a package that cannot build it (CD-RT-01).
- Tag note: the edit log tags job-key edits `CD-JOB-02`; this document and 93 call them `CD-JB-02`.

## 2. Method and evidence

1. Read all 18 unit documents, the OpenAPI documents, the route TSV, the design verification reports, and extracted all 549 section 14 and 15 rows into one index (`93`).
2. Built a single scratch Go module overlaying every unit's code assets (`mega3`). `go build ./...` and `go vet ./...` pass. Tests pass for `job`, `routes`, `readmodel`, `refresh`, `httpapi`, `live`, `ops`, `overview` and `cmd/scp/internal/bridge` (re-run 2026-10-14 at the end of the review). `webhook` and `docs` tests need unit testdata that the union does not carry; they were run per unit by their own verification.
3. `internal/crosscheck` asserts at compile time that the adapter signatures written in 05 section 2.3 satisfy the consumer interfaces of `catalog`, `commits`, `docs`, `manifests`, `media`, `webhook`, and that the store, reactor, publisher and httpx types satisfy theirs: 12 adapter-side interfaces plus 18 others. Before the fix, `manifests.BlobBatchReader` failed.
4. Ran `internal/archtest` (16's rules) against the union. Result before the matrix decisions of A-30: 21 failures of `TestRealTree` and 22 of `TestRealSource`, of which the real findings are the `httpapi/openapi` imports, the YAML import, the route literals and the test-only exporter imports (CD-DM-01..06). The rest are expected: rows for packages that exist only in the final repository (`planned`) and packages that exist only as stand-ins (`crosscheck`, `ghverify`).
5. Ran a local exporter probe to confirm the Prometheus name rewrite (gauge unit `1` becomes `_ratio`, counters get `_total`); the probe is described in 91 rule 1.
6. Compared names mechanically: OpenAPI `x-scp-route` and `x-scp-owner` against `01-kernel.routes.tsv` (22 public and 2 admin operations equal); every route-name token in each unit section 7 against the TSV (only 16's `openapi.json` and sample paths were off); config keys parsed from every unit into `cfg.json` (370 keys) and metric names into `met.json` (179 names).
7. Library and standard facts used here were verified on 2026-10-14: oapi-codegen v2.8.0 cannot emit `{id...}` wildcards (01 D1, local generation); Prometheus exporter suffix rules (local probe).

## 3. Findings register

Status: FIXED = corrected in the documents or code assets, or resolved by a decision written into the documents; RECORDED = a requirement or gap written into the owning unit and into 93; OPEN = needs a person or a later unit.

| ID | Area | Finding | Status |
|---|---|---|---|
| CD-IF-01 | Interface | `manifests.BlobBatchReader.ReadBlobs` consumed by 11 but absent from 05 section 2.3 | FIXED |
| CD-IF-02 | Interface | `live.Feed` required `Subscribe`, `ChangesSince`, `readmodel.ChangeSet`, which 03 v1 does not have (change feed deferred to v1.1) | FIXED |
| CD-IF-03 | Interface | `Snapshot.Epoch()` / `Deps.Epoch` used by 07 and 16 but missing in 03 | FIXED |
| CD-IF-04 | Interface | 16 called nonexistent `docs/schema.Descriptor/Lint` | FIXED |
| CD-IF-05 | Interface | 06 `httpapi` lacked the codes, `WithRetry` and 308 that 04, 07 and 15 use (see CD-ER-02) | FIXED |
| CD-IF-06 | Interface | stand-in `internal/logx` in 06, 09, 15 versus `internal/platform/logx` | FIXED |
| CD-IF-07 | Interface | `overview` sources assumed accessors that 09, 10, 11, 14 do not export (R-DOCS-1, R-PAGES-1, R-09-1, R-OVR-1) | RECORDED |
| CD-IF-08 | Interface | webhook verifier vocabulary X1..X6 differed between 04 and 05 | FIXED |
| CD-IF-09 | Interface | `docs.ImageSigner` needs `KeyID()`; architecture 8.7 lacked it | FIXED |
| CD-IF-10 | Interface | 04 comment "bridged over httpx" stale | FIXED |
| CD-IF-11 | Interface | 14 stand-in `readmodel.Register(..., bool)` and 12 test `catalog.repo` registration differ from the real options | FIXED |
| CD-IF-12 | Interface | 05 plain `*github.Error` has no `RetryAt` (only `PauseError`); architecture 8.7/15 text said otherwise | FIXED |
| CD-CF-01 | Config | root struct omitted `llms`, `ops`, typed `manifests.providers` as struct, no `enrich` | FIXED |
| CD-CF-02 | Config | reserve arithmetic in both 04 and 05 (X6) | FIXED |
| CD-CF-03 | Config | `webhook.redelivery.max_per_run` 200 vs 50; `rate`, `max_age` only in 04 | FIXED |
| CD-CF-04 | Config | `github.defaults.webhook` 3 keys vs 7; `webhook.enabled/default` only in 04 | FIXED |
| CD-CF-05 | Config | two concurrency knobs (budget vs upstreams); 02 Q2 suggested one | FIXED |
| CD-CF-06 | Config | 05 `SecretRef{File,Env}` and `LookupEnv` vs `secret.Secret` | FIXED |
| CD-CF-07 | Config | `discovery.*` declared in 05 and 08 with two new keys | FIXED |
| CD-CF-08 | Config | `server.public_listen`, `server.hsts`, `server.public_url` vs nested names | FIXED |
| CD-CF-09 | Config | ghost keys (`ratelimit.default`, `net.*`, `site.url`, `site.announcement.*`, `github.sources[].visibility`) | FIXED |
| CD-CF-10 | Config | `catalog.Config` fails `Describe`; one-key matcher cannot be assigned (16 U-1/U-2) | OPEN |
| CD-CF-11 | Config | shutdown budget 38 s (architecture) vs 44 s (defaults) | FIXED |
| CD-CF-12 | Config | `site.dashboard_commits` (18 D-18-03) | FIXED |
| CD-CF-13 | Config | `live.sse.max_per_ip` vs `ratelimit.sse_per_ip` | FIXED |
| CD-CF-14 | Config | `manifests.acquisition.disabled_ecosystems` appears in 11 and 13; `store.cache_caps.depsdev` in 03 and 13 (checked: single declaration each, in 11 and 03; 13 is the consumer) | FIXED |
| CD-CF-15 | Config | architecture 16 outline: `manifests.allow_list`, `reserve`, missing `ops`/`llms` | FIXED |
| CD-RT-01 | Route | owner of `api.repos` and `api.group.repos` (architecture: catalog; API draft: overview) | FIXED |
| CD-RT-02 | Route | 16 section 7 used wrong route names and openapi path | FIXED |
| CD-RT-04 | Route | `docInstall` uiUrl with extension in OpenAPI and fixtures vs extension-less in 10 | FIXED |
| CD-RT-05 | Route | `{id...}` wildcard vs `{docId}` (oapi-codegen cannot generate `{x...}`) | FIXED |
| CD-RT-07 | Route | 18 `listCommits` `author`/`q` not in the contract | FIXED |
| CD-RT-08 | Route | admin listener serves 22 public operations (architecture 11.4 ambiguous) | FIXED |
| CD-MT-01 | Metric | lifecycle metric vocabulary (02 vs 16) | FIXED |
| CD-MT-02 | Metric | owner of `scp_item_age_seconds` and `scp_store_errors_total`; 10 bumped it on `docs.put` | FIXED |
| CD-MT-03 | Metric | gauge unit `1` exports `_ratio` | FIXED |
| CD-MT-04 | Metric | architecture 18.2 outdated | FIXED |
| CD-MT-05 | Metric | four reaction counters with four names | FIXED |
| CD-MT-06 | Metric | telemetry View overrides histogram bucket advice (15 R-02-1) | OPEN |
| CD-MT-07 | Metric | 15 section 9.3 "complete" index incomplete | FIXED |
| CD-MT-08 | Metric | `scp_manifests_limit_hits_total{limit}` label must be a closed set | RECORDED |
| CD-MT-09 | Metric | `scp_client_errors_total` in 17 | FIXED |
| CD-ER-01 | Error | four `ErrCode` vocabularies | FIXED |
| CD-ER-02 | Error | 06 lacked codes, `WithRetry`, 308 | FIXED |
| CD-ER-03 | Error | 04 `overloaded` vs 07/15 `queue-full` | FIXED |
| CD-ER-04 | Error | verifier error vocabulary (see CD-IF-08) | FIXED |
| CD-ER-05 | Error | job key grammar 512 bytes with spaces vs 300 without | FIXED |
| CD-JB-01 | Job/store | job kind versus item kind: `PlanSink` stored plans under the job kind, so commits/docs/manifests plans never persisted | FIXED |
| CD-JB-02 | Job/store | job key grammar (white space, 512 bytes) across 01, 07, 15, 18 | FIXED |
| CD-JB-03 | Job/store | architecture 9.6 lacked `catalog.stats` and `manifests.enrich` | FIXED |
| CD-SL-01 | Slot | architecture 9.1 slot table versus the 14 slots in code; mixed slot value convention | FIXED |
| CD-DM-01 | Matrix | `commits`, `manifests`, `overview` import `httpapi/openapi` | FIXED |
| CD-DM-02 | Matrix | `docs/internal/render` imports `go.yaml.in/yaml/v3` | FIXED |
| CD-DM-03 | Matrix | route literals in catalog, manifests, overview, httpapi/defaults, apitest/spec | OPEN |
| CD-DM-04 | Matrix | 16 archtest rules gave `webhook` the `freshness` import that 04 withdrew | FIXED |
| CD-DM-05 | Matrix | `ghverify` and `crosscheck` have no archtest rule (stand-ins) | RECORDED |
| CD-DM-06 | Matrix | test files of live/ops/overview import Prometheus exporter libraries (G6 `TestOK`); webhook test imports `ghverify` | FIXED |
| CD-DM-07 | Matrix | other design-assumed imports (05 D-1..3, 02 D1/D2/D14, 14 D-1/D-9, 11 D-2/D-3, 12 D-4, 13 D-1/D-5, 16 D-9, 06 D2/D7/D8) | FIXED |
| CD-FX-01 | Fixture | no `dependency-graph/sbom` endpoints or GraphQL `dependencyGraphManifests` in the fake | RECORDED |
| CD-FX-02 | Fixture | batched blob read (`object(oid:)`) not covered by the fake | RECORDED |
| CD-FX-03 | Fixture | demo world lacks docs pages, `scp.yaml`, `catalog-info`, `CODEOWNERS`, images | RECORDED |
| CD-FX-04 | Fixture | demo world lacks non-Maven manifests, lockfiles, truncated monorepo tree, rename, multi-group, installation-scoped private repo | RECORDED |
| CD-FX-05 | Fixture | Jenkins, Snyk and deps.dev fixtures are hand-written; recorded fixtures pending | RECORDED |

Ids CD-RT-03, CD-RT-06 and CD-RT-09 are withdrawn and not reused.

## 4. Interface agreement

Direct consumer interfaces (kernel types only) were compared by compilation. The remaining differences were (CD-IF-01..12 above). Bridged interfaces (an adapter in `cmd/scp/internal/bridge`) were compared by reading both sides: `overview` sources (CD-IF-07), `badges` over `integrations.StatusOf`, `refresh.PlanSink` (CD-JB-01), `pages.Renderer` over `docs.PageRender`. Error types: the adapter returns `*github.Error`; behaviour interfaces `job.RateLimited`, `job.Permanent`, `job.Gone` are what the engine reads; the precedence for the string code is Gone, Permanent, RateLimited, Canceled (92).

## 5. Route, OpenAPI and page agreement

- Route table (01), the two OpenAPI documents (07) and the feature units agree on all 24 operation names, paths, parameters, status codes and owners after CD-RT-01 (the two list operations are owned by `overview`) and CD-RT-05 (`{docId}`).
- Frontend pages (18) and the foundation (17) call only operations in the documents; the exceptions were `listCommits` filters and `site.dashboard_commits` (not adopted, CD-RT-07, CD-CF-12), the doc install URL (CD-RT-04) and the job-key length (CD-JB-02).
- The admin listener serves the 22 public operations too; the page for `/status` is the same SPA bundle (CD-RT-08, owner to confirm).
- Stale copies: the `common.yaml` copies under the assets of 06, 09 and 15 are snapshots older than `api/common.yaml`; the latter is authoritative and 17's copies equal it.

## 6. Config, metrics, errors

- Config: `20-design/90-config-reference.md` is the single key table. The platform root struct now contains every section (CD-CF-01). Open: CD-CF-10.
- Metrics: `20-design/91-metrics-catalogue.md`. No name collisions remain; label cardinality is bounded (repository, user and URL values never appear as labels; `limit` and `kind` are closed sets). Open: CD-MT-06.
- Errors: `20-design/92-error-catalogue.md`. One problem-type URI scheme (`.../problems/<code>`), 19 codes, one closed `ErrCode` set.

## 7. Jobs, store tables, slots and projectors

- Job keys are `<kind>/<scope>` with no white space and at most 512 bytes in the kernel, contract, `ops`, and SPA. The job kind and the item kind differ for `commits.refresh`, `docs.refresh`, `manifests.refresh`; the store rows and the plan sink use the item kind, the engine uses the job kind (CD-JB-01).
- Job table: `catalog.reconcile`, `catalog.heads`, `catalog.stats`, `commits.refresh`, `docs.refresh`, `manifests.refresh`, `manifests.graph`, `manifests.enrich`, `integrations.jenkins`, `integrations.snyk`, `webhook.redelivery`, `github.installations` (architecture 9.6 now lists all).
- Snapshot slots registered in code: `catalog.repo` (existence), `catalog.stats`, `catalog.membership`, `catalog.aliases`, `catalog.group_aliases`, `catalog.sync`, `commits.view`, `commits.latest` (classed), `docs.repo`, `manifests.repo`, `manifests.graph` (classed global), `integrations.status`, `pages.set`, `site.meta` (classed). Projectors: `commits.latest` (owned by `commits`) and `overview.site` (owned by `overview`); `manifests.graph` is built by its own job, not a projector.
- Store tables are those of unit 03; features reach them through their own `ItemStore`/`Store` consumer interfaces (asserted for `sqlite` and `memory` in `crosscheck`).

## 8. Fixtures matrix

Fake GitHub (05 section 7.5) supports: `/user`, `/app`, installations, access tokens, hook config and deliveries (list, attempts, redeliver), `installation/repositories`, org and user repository lists, repository, README (JSON, raw, HTML), contents, commits (including 409 empty repository), `commits/{ref}` (SHA media type and ETag), releases (latest and list), git trees and blobs, GraphQL (subset: `repository`, `defaultBranchRef`, `object`, `licenseInfo`, `issues`, `pullRequests`, `latestRelease`, `rateLimit`), app-manifest conversion, and a `/_fake` control plane. Demo world: `payments-api` (README.md, `pom.xml`, 4 commits, 1 release, null authors), `docs-site` (README.adoc), `archived-tool` (private, archived).

| Feature | Upstream call | Fake GitHub | Gap |
|---|---|---|---|
| catalog | list repositories (installation, org, user) | yes | installation-scoped private repo missing (CD-FX-04) |
| catalog | head probe (GraphQL, REST fallback) | yes (GraphQL subset) | none |
| catalog | stats and latest release | yes | none |
| catalog | rename detection | partly (`repo` redirect not in world) | rename case missing (CD-FX-04) |
| commits | latest commits, conditional (ETag) | yes | none |
| commits | empty repository (409) | yes | none |
| docs | README (JSON, raw, HTML) | yes | none |
| docs | contents / read file, tree | yes | docs pages and descriptor files missing (CD-FX-03) |
| docs | rendered file (AsciiDoc bridge) | yes (HTML media type) | none |
| manifests | list tree (recursive, truncated flag) | yes | truncated monorepo missing (CD-FX-04) |
| manifests | read blob by SHA | yes | non-Maven manifests and lockfiles missing (CD-FX-04) |
| manifests | batched blob read (GraphQL aliases) | documents `object(expression:)` only | `object(oid:)` untested (CD-FX-02, spike S-B) |
| manifests providers | dependency-graph SBOM, `dependencyGraphManifests` | no | CD-FX-01 (providers off by default; own recorded JSON) |
| manifests enrich | deps.dev | not GitHub; httptest fixtures (13) | recorded fixtures pending (CD-FX-05) |
| media | repository image read | yes (contents/blob) | images missing from the world (CD-FX-03) |
| webhook | signature verify, deliveries, redelivery | yes | `GET /app/hook/deliveries/{id}` referenced but unused |
| installations / doctor | `/app`, installations, permissions, rate limit | yes | `GET /meta` referenced but unused |
| integrations | Jenkins, Snyk | not GitHub; hand-written httptest | recorded fixtures pending (CD-FX-05) |

Requirement on unit 05 section 7.5: close CD-FX-02 to CD-FX-04 in the demo world before milestone 3; CD-FX-01 only if S-A succeeds.

## 9. Dependency matrix violations introduced by the designs

Imports the designs assume that the matrix of architecture 5.2 did not allow (all now listed in the matrix additions table of 5.2, A-30):

| Package | Import | Source | Decision |
|---|---|---|---|
| commits, manifests, overview | `httpapi/openapi` | 07 Q-15, 11 D-1, 15 D15-11 | accept the cell (rank 0 generated schemas); alternative: per-feature `*api` aliases |
| docs/internal/render | `go.yaml.in/yaml/v3` | 10 D-16 (G6 owner) | accept, add owner row |
| catalog, manifests, overview, httpapi/defaults, apitest/spec | route literals | 16 U-3 | OPEN: archtest allowance for route-table files |
| github, github/auth | `x/sync/singleflight`, `x/time/rate`; own JWT | 05 D-1, D-2 | accept |
| cmd/scp/internal/bridge | `github`, `reactor`, `store` | 05 D-3, 16 D-9 | accept |
| platform/httpx, platform/access | `platform/secret`, otel API | 02 D1/D14, 14 D-1 | accept |
| manifests, manifests/internal/limits | `x/sync`; `charmap` moved to `limits` | 11 D-2, D-3 | accept |
| manifests/internal | five new packages `tomlx`, `linex`, `repopath`, `expand`, `redact` | 12 D-4 | accept |
| webhook (test) | `ghverify` | 04 | accept as test helper; rule row needed when `ghverify` becomes real |
| live, ops, overview (test) | Prometheus exporter libraries | 15 | accept with `TestOK` in G6 |
| webhook | `freshness` | 16 rules.go | removed (04 withdrew D1) |
| cmd/fakegithub | `os.Exit`, signals, `http.Server` | 05 D-11 | accept lint exemption |

## 10. Complete edit list

Every edit made by this review (the edit log keeps the exact old and new text). Paths are relative to the root of this design package. Tags are the finding ids.

| File | Edits | Findings |
|---|---|---|
| `20-design/01-kernel.md` | 6 | CD-JOB-02, CD-MT-02, CD-RT-01, CD-UNIT |
| `20-design/01-kernel.routes.tsv` | 2 | CD-RT-01 |
| `20-design/01-kernel.assets/internal/routes/public.go` | 3 | CD-RT-01 |
| `20-design/api/openapi-admin.yaml` | 1 | CD-JOB-02 |
| `20-design/17-web-foundation.assets/api/openapi-admin.yaml` | 1 | CD-JOB-02 |
| `20-design/api/openapi-public.yaml` | 2 | CD-ER-01, CD-RT-04 |
| `20-design/17-web-foundation.assets/api/openapi-public.yaml` | 2 | CD-ER-01, CD-RT-04 |
| `20-design/18-web-pages.assets/web/src/test/fixtures.ts` | 1 | CD-RT-04 |
| `20-design/api/common.yaml` | 1 | CD-ER-01 |
| `20-design/17-web-foundation.assets/api/common.yaml` | 1 | CD-ER-01 |
| `20-design/17-web-foundation.assets/web/e2e/fixtures/api/groups.json` | 1 | CD-ER-01 |
| `20-design/07-openapi-contract.md` | 8 | CD-CF-08, CD-ER-01, CD-ER-02, CD-ER-03, CD-UNIT |
| `20-design/18-web-pages.assets/web/src/shared/freshness.ts` | 1 | CD-ER-01 |
| `20-design/18-web-pages.assets/web/src/shared/shared.test.ts` | 1 | CD-ER-01 |
| `20-design/18-web-pages.md` | 4 | CD-CF-09, CD-ER-01, CD-JOB-02, CD-UNIT |
| `20-design/18-web-pages.assets/web/src/features/status/model.ts` | 1 | CD-JOB-02 |
| `20-design/16-cmd-build-release.md` | 14 | CD-CF-01, CD-DM-04, CD-IF-04, CD-JB-01, CD-MT-01, CD-RT-02, CD-UNIT |
| `20-design/06-httpapi.md` | 4 | CD-CF-08, CD-UNIT |
| `20-design/15-live-ops-overview.assets/internal/live/{live.go,topics.go,live_test.go}` | 1 | CD-IF-02 |
| `20-design/15-live-ops-overview.md` | 5 | CD-IF-02, CD-JOB-02, CD-UNIT |
| `20-design/{06,09,15}-*.assets` | 1 | CD-IF-06 |
| `20-design/03-readmodel-store.assets/internal/readmodel/{snapshot,publisher,builder,epoch_test}.go` | 1 | CD-IF-03 |
| `20-design/09-commits-badges.assets/internal/commits/service.go` | 2 | CD-ER-01 |
| `20-design/11-manifests-core.assets/internal/manifests/refresh.go` | 3 | CD-ER-01 |
| `20-design/01-kernel.assets/internal/job/key.go` | 1 | CD-JOB-02 |
| `20-design/06-httpapi.assets/internal/httpapi/{problem,regress_test}.go` | 1 | CD-ER-02 |
| `20-design/15-live-ops-overview.assets/internal/{httpapi,overview}` | 1 | CD-ER-02 |
| `20-design/09-commits-badges.assets/internal/httpapi/problem.go` | 1 | CD-ER-02 |
| `20-design/05-github-adapter.md` | 26 | CD-CF-03, CD-CF-04, CD-CF-06, CD-CF-07, CD-FX, CD-IF-01, CD-IF-08, CD-UNIT |
| `20-design/04-refresh-webhook.assets/internal/refresh/gate.go` | 1 | CD-CF-02 |
| `20-design/16-cmd-build-release.assets/internal/refresh/gate.go` | 1 | CD-CF-02 |
| `20-design/04-refresh-webhook.assets/internal/refresh/refresh_test.go` | 2 | CD-CF-02 |
| `20-design/04-refresh-webhook.assets/internal/e2e/assert_test.go` | 3 | CD-CF-02 |
| `20-design/04-refresh-webhook.md` | 25 | CD-CF-02, CD-CF-03, CD-CF-04, CD-ER-03, CD-IF-08, CD-MT-02, CD-UNIT |
| `20-design/04-refresh-webhook.assets/internal/webhook/webhook.go` | 2 | CD-ER-03, CD-IF-10 |
| `20-design/09-commits-badges.md` | 2 | CD-MT-02, CD-UNIT |
| `20-design/16-cmd-build-release.assets/cmd/scp/run.go` | 1 | CD-MT-01 |
| `20-design/16-cmd-build-release.assets/cmd/scp/cmd_config.go` | 1 | CD-IF-04 |
| `20-design/16-cmd-build-release.assets/cmd/scp/cmd_descriptor.go` | 1 | CD-IF-04 |
| `20-design/14-integrations-access.assets/internal/readmodel/readmodel.go` | 1 | CD-IF-11 |
| `20-design/14-integrations-access.assets/internal/integrations/integrations_test.go` | 2 | CD-IF-11 |
| `20-design/12-manifests-ecosystems-a.assets/overlay/internal/manifests/ecosystems/integration/integration_test.go` | 1 | CD-IF-11 |
| `20-design/10-docs-pages-media.md` | 8 | CD-CF-08, CD-CF-09, CD-MT-02, CD-UNIT |
| `20-design/11-manifests-core.md` | 2 | CD-CF-09, CD-UNIT |
| `20-design/02-platform.md` | 7 | CD-CF-01, CD-UNIT |
| `20-design/14-integrations-access.assets/internal/integrations/service.go` | 1 | CD-MT-03 |
| `20-design/13-manifests-ecosystems-b.md` | 2 | CD-MT-03, CD-UNIT |
| `20-design/91-metrics-catalogue.md` | 3 | CD-MT-02, CD-MT-04, CD-MT-08 |
| `20-design/06-httpapi.assets/internal/httpapi/problem.go` | 2 | CD-ER-03 |
| `20-design/15-live-ops-overview.assets/internal/ops/admin.go` | 1 | CD-JOB-02 |
| `20-design/92-error-catalogue.md` | 4 | CD-ER-01 |
| `20-design/16-cmd-build-release.assets/cmd/scp/internal/bridge/bridge.go` | 5 | CD-JB-01 |
| `20-design/16-cmd-build-release.assets/internal/archtest/rules.go` | 1 | CD-DM-04 |
| `10-architecture.md` | 15 | CD-CF-01, CD-CF-11, CD-CF-15, CD-DM, CD-IF-01, CD-IF-09, CD-IF-12, CD-JB-01, CD-MT-04, CD-REV, CD-RT-01, CD-RT-05, CD-SL-01 |
| `04-decisions.md` | 6 | CD-AMEND |
| `20-design/03-readmodel-store.md` | 1 | CD-UNIT |
| `20-design/08-catalog.md` | 1 | CD-UNIT |
| `20-design/12-manifests-ecosystems-a.md` | 1 | CD-UNIT |
| `20-design/14-integrations-access.md` | 1 | CD-UNIT |
| `20-design/17-web-foundation.md` | 1 | CD-UNIT |

New files: `20-design/90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md`, `93-deviations-and-open-questions.md`, this file. Removed code files: the stand-in `changes_stub.go` (15) and the stand-in descriptor schema stub (16).

## 11. Owner-confirmation items

The full list with recommendations is section 3 of `20-design/93-deviations-and-open-questions.md`. Summary:

1. Admin listener serves the 22 public operations too (01 Q9, D14).
2. External SVG in `/media/ext`: keep `deny` or approve `sandboxed` (10 D-1; recommended: approve).
3. Lifecycle `production` vs `active` (Q-20 reading) and the W19/A-25 product choices (commit authors render as initials (no avatar fetch), first-line commit subject, dashboard in configuration order, dashboard commit count 10).
4. Camo redirect behaviour for external images.
5. Global per-upstream rate pause in v1 (04 Q1, 05 Q-10).
6. Reserve gate lives in the GitHub adapter (X6).
7. Both concurrency knobs kept (CD-CF-05); 03 P1 plan table not adopted.
8. 06 Q1 two view classes; 08 Q-1 hide empty groups; 08 Q-2 `include_forks` false.
9. 16: image namespace `ghcr.io/cantara/source-code-portal` (Q16-8), no `knowledge.yaml` (Q16-7), stay on Node 24 (Q16-12), merge queue (Q16-16), no early `/livez` listener (Q16-19), SIGHUP reload scope (D-15), shutdown 44 s (D-12).
10. 02: admin bind `:9091` with warning (Q5); Host guard off by default (Q15).
11. 15 Q4, Q5, Q7, Q8 (admin SSE instance).
12. 13: spike S-A, Snyk regional URLs, Maven Central policy (S-C).
13. Matrix additions of A-30, in particular `httpapi/openapi` for feature packages and the route-literal allowance.
14. 18 D-18-02 and D-18-03 not adopted for v1.

## 12. Remaining risks

- The `overview` read accessors on `docs`, `pages`, `badges`, `manifests`, `integrations` (CD-IF-07) are requirements; `overview` was compiled against stand-ins, so the first real build may need signature adjustments.
- Unit 05 has no code: the adapter was checked at signature level (crosscheck) only; the behaviour of the reserve gate, the batched blob read and the 5xx classification is unproven.
- The union module is an overlay of scratch assets, not the real repository layout; archtest results there show rule gaps, not final results.
- Open spikes S-A (installation tokens for dependency graph), S-B (GraphQL blob batching cost), S-C (Maven Central policy), S-D (graph heap), the V13 push payload check and W21.
- A table-driven test that keeps `ErrCode` consistent across 09, 11 and 18, and an `apitest` test of the code catalogue against 06, do not exist yet (92 names them).
- `catalog.Config` Describe tags and the matcher `UnmarshalYAML` (CD-CF-10) are unowned until 02 and 08 are edited.
- Telemetry View bucket override (CD-MT-06) would silently change histogram resolution for every unit.
- Stale `common.yaml` snapshots under 06, 09 and 15 assets can mislead an implementer; copy from `api/`.
- Route-literal archtest rule (CD-DM-03) will fail the real build until an allowance is written.
