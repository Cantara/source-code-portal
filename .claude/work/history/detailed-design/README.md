# Source Code Portal: design package

> **Reference only (2026-10-14).** The binding plan is [`../plan.md`](../plan.md): shorter, coarser and with a smaller scope. This package
> is the earlier detailed design; consult it, copy what helps, ignore what the plan dropped. It is not kept in sync.

Index and user guide for the complete design of Source Code Portal (SCP). Written 2026-10-14. This directory is the only input the owner needs to build the application from scratch in a new repository.

Contents: [1. What this is](#1-what-this-is) | [2. How to use it](#2-how-to-use-this-package-to-build-the-product) | [3. Reading order](#3-reading-order) | [4. Product and architecture on one page](#4-product-and-architecture-on-one-page) | [5. Document table](#5-document-table) | [6. Glossary](#6-glossary) | [7. Binding decisions at a glance](#7-binding-decisions-at-a-glance) | [8. Product behaviour highlights](#8-product-behaviour-highlights) | [9. Owner confirmations needed](#9-owner-confirmations-needed) | [10. Deviations proposed](#10-deviations-proposed-by-this-index) | [11. Conventions and status of this package](#11-conventions-and-status-of-this-package)

---

## 1. What this is

SCP is an organisational dashboard that aggregates GitHub repositories, groups them into systems, and shows latest commits, documentation, status badges and package-manifest and dependency intelligence. It is kept fresh by scheduled sync plus webhooks, continuously reconciles the repository list, and is held to 45 binding design guardrails (GR-01..GR-45, [02-design-guardrails.md](02-design-guardrails.md)). The implementation shape:

- **Backend:** one static Go binary (Go 1.27.2, `CGO_ENABLED=0`), package-by-feature, enforced dependency rule, immutable in-memory read model, SQLite as a rebuildable last-good cache, in-house sync engine, own thin GitHub client (GitHub App first).
- **Frontend:** React 19 + shadcn/ui (Base UI) + TypeScript SPA under `web/`, embedded in the binary; Node is a build-time tool only.
- **API-first:** a versioned OpenAPI 3.1 contract (`/api/v1`) is the source of truth for the SPA, scripts and LLM agents. `llms.txt`, raw Markdown and the JSON API are server-generated and do not depend on the SPA.

The package contains the design guardrails, the decisions, the architecture, the ADRs, 18 unit designs (most with compiled and tested reference code), the verification reports of every design, a cross-design review that reconciled them, the implementation plan, the test plan and a repository seed (section 5.6).

## 2. How to use this package to build the product

1. **Lay the seed into this repository** (`git@github.com:Cantara/source-code-portal.git`, branch `main`, module `github.com/cantara/source-code-portal`; the design package lives here at `.claude/work/plans/`, `docs/` is user documentation only). Run [`50-repo-seed/bootstrap.sh`](50-repo-seed/README.md) from the repository root: it copies the starter files of `50-repo-seed/repo/` (derived from [20-design/16-seed-files/](20-design/16-seed-files/) and the unit 17 prototype, with the deviations S-1..S-13) and refuses to overwrite anything that differs. Then follow the bootstrap order in [design 16, section 4.12](20-design/16-cmd-build-release.md): step 1 is what the seed contains (`go.mod`, `tools/` module, `Makefile`, `.golangci.yml`, `internal/archtest`, `cmd/scp` skeleton, the `web/` embed placeholder), steps 2 to 8 add units in rank order.
2. **Land units in dependency order.** Kernel (01), platform (02), read model and store (03); then engine, webhook and GitHub adapter (04, 05) with the fake GitHub; then HTTP foundation, contract and catalogue (06, 07, 08); then features and services (09 to 15); then the SPA (17, 18); then image and release (16 tail). Each unit design has a task list, a test plan and a traceability section; each lands with its `archtest` row flipped from `planned` to real.
3. **Copy, do not re-derive.** Most units ship reference code in `20-design/<unit>.assets/` (compiled, vetted and tested on 2026-10-14). The cross-design review compiled all of them as one module. Copy from `20-design/api/` (not from the stale `common.yaml` copies under some `.assets/`).
4. **Use the catalogues as the single source:** config keys [90](20-design/90-config-reference.md) (370 keys, 24 root sections), metrics [91](20-design/91-metrics-catalogue.md) (179 instrument names), errors [92](20-design/92-error-catalogue.md) (19 problem codes plus the closed `ErrCode` vocabulary), routes [`01-kernel.routes.tsv`](20-design/01-kernel.routes.tsv) and the OpenAPI roots in [20-design/api/](20-design/api/).
5. **Check every deviation against [93](20-design/93-deviations-and-open-questions.md)** (549 indexed unit rows, 21 consolidated decision rows) before changing a design. Nothing may diverge silently: a proposed change goes into a "Deviations" section with evidence.
6. **Settle the owner confirmations** in [section 9](#9-owner-confirmations-needed) before the affected milestone. Every item has a default that ships if the owner does not answer.
7. **Run the M0 spikes first** (architecture risk table W1, W3, W21, W22): oapi-codegen 3.1 fragment, `ProblemMux` for `/api` 404/405, SQLite read-only backup URI and `temp_store` on a read-only root, and a prototype of `Planner`, `Gate`, per-job `Timeout`, `reactor` and `lifecycle.Plan` inside synctest bubbles.
8. **Re-verify library versions at implementation time.** All pins in [10-architecture.md, Appendix A](10-architecture.md) and the images by digest were read on 2026-10-13/14. The design rule is the same as the owner's: do not trust memory for fast-moving libraries.

## 3. Reading order

### 3.1 For an engineer building the product

1. This file, sections 4 to 8 (30 minutes).
2. [04-decisions.md](04-decisions.md): the binding record. Read section 7 (amendments A-01 to A-33) as part of the decisions, not as an appendix.
3. [02-design-guardrails.md](02-design-guardrails.md): the binding guardrails GR-01..GR-45, each with principle, rationale, scope and the test that enforces it.
4. [10-architecture.md](10-architecture.md): sections 1 to 7 (shape and dependency rule), 8 (Go signatures), 9 to 12 (state, runtime, HTTP, frontend). Sections 13 to 23 as needed.
5. The ADR of the area you are building ([11-adr/](11-adr/)); ADR-001, 018 and 024 are mandatory for everyone.
6. Your unit design in [20-design/](20-design/) and its verification report in [30-verification/](30-verification/) (the report lists what was found wrong and what remains a caveat).
7. [93](20-design/93-deviations-and-open-questions.md) rows for your unit, and the catalogues 90 to 92 for any key, metric or error you add.
8. [40-implementation-plan.md](40-implementation-plan.md) for the milestone your unit belongs to and [41-test-plan.md](41-test-plan.md) for its gates.
9. Research ([05-research/](05-research/)) only to check the evidence behind a decision.

### 3.2 For a reviewer

1. [04](04-decisions.md) and [02](02-design-guardrails.md), then [10](10-architecture.md) section 3 (decision summary) and section 23 (open risks).
2. [30-verification/architecture-review-go.md](30-verification/architecture-review-go.md): the independent Go idiom and design review of the architecture, with the reviser responses; the requirements traceability findings and their dispositions are in [10-architecture.md](10-architecture.md) revision log R.1.
3. [30-verification/cross-design-consistency.md](30-verification/cross-design-consistency.md): 63 findings across the 18 designs, which were fixed, which remain open (section 3 findings register, section 12 remaining risks).
4. [93](20-design/93-deviations-and-open-questions.md) section 2 and 3: consolidated decisions and the owner list.
5. Traceability: [10-architecture.md section 20](10-architecture.md) (every guardrail GR-01..GR-45 to its structural enforcement mechanism) and section 21 (every in-scope feature and platform capability to a package).
6. One `30-verification/design-NN-*.md` per unit you are asked to sign off; each states a verdict (most are `ready_with_caveats`).

### 3.3 For an LLM agent doing the build

1. Fetch [llms.txt](llms.txt) first: it lists the key documents with one-line purposes.
2. Obey the precedence rule (section 11): `04-decisions.md` (including section 7) over `10-architecture.md` over `11-adr/` over the unit designs over research. The guardrails in `02-design-guardrails.md` are binding at every level. Where two documents disagree, the higher one wins and the disagreement is a finding, not a licence.
3. Work one unit at a time. For each: read the unit design fully (they are long; section 1 states scope and non-goals, sections 3 to 9 hold types, behaviour, data, config, HTTP, concurrency, observability), the unit's verification report, then its rows in 93. Build against the architecture's interfaces (section 8) and the compile-time assertions in `cmd/scp/assert.go`; never import another feature.
4. Never invent a package, config key, metric or route: add it to the catalogue/table that owns it (90, 91, `routes.Table`, the OpenAPI fragment) in the same change.
5. Never contact the network in tests; use the fake GitHub ([05 design](20-design/05-github-adapter.md), `cmd/fakegithub`) and the recorded fixtures under each unit's `testdata`.
6. When a design says "owner confirms", ship the stated default and keep the item in [section 9](#9-owner-confirmations-needed) open.
7. Never violate a guardrail in [02-design-guardrails.md](02-design-guardrails.md); [10-architecture.md section 20](10-architecture.md) names the structural prevention for each and the guardrail entry names its test.

## 4. Product and architecture on one page

**Purpose (04, section 2).** An organisational dashboard of GitHub repositories grouped into systems, showing latest commits, documentation and status badges, kept fresh by sync plus webhooks. Target scale: up to 1,000 repositories, single replica, staleness SLOs of seconds (webhook), 5 min (commits), 60 min (README, conditional), 30 min (repository list), 5 to 15 min (status).

**Scope.** In: groups with typed matchers (many-to-many), dashboard, group and repository pages, commit logs, README and `docs/` rendering (CommonMark + GFM, `scp.yaml` descriptor), local SVG badges, optional Jenkins and Snyk status providers, config-driven pages and announcement, webhooks, package-manifest intelligence for 16+ ecosystems with an org-wide internal dependency graph (N-01), LLM surface (`llms.txt` family, raw Markdown, JSON API), optional trusted-proxy access mode for private repositories. Out (v1): multi-replica, non-GitHub providers, local AsciiDoc engine, OIDC, vulnerability data, plug-in system, durable user data, SSR.

**Runtime shape.**

```
Browser SPA / LLM agent / README badge viewers --> PUBLIC listener :9090  (SPA, /api/v1, badges, llms.txt, media, webhooks, /livez /readyz)
Operator / Prometheus ------------------------------> ADMIN  listener :9091  (/status, /metrics, /debug/pprof, admin API)
GitHub (signed webhooks) ---------------------------> PUBLIC listener
scp = feature packages + immutable read model (atomic snapshot) + refresh engine (planner, scheduler, gate) + SQLite (rebuildable cache)
scp --(GitHub App token, REST + GraphQL, conditional)--> GitHub / GHES / GHEC-DR;  --(SSRF-guarded)--> optional Jenkins, Snyk, Maven Central parent POMs
```

Data direction: GitHub feeds **jobs**; jobs write SQLite (last good state) and publish **immutable snapshots**; request handlers read only the current snapshot. Nothing on a request path talks to GitHub or the database.

**Package map (10, section 4; ranks and exact allowed-import matrix in section 5).**

| Layer | Packages |
|---|---|
| Kernel (vocabulary) | `scm`, `freshness`, `job`, `routes`, `safehtml` |
| Platform | `platform/{secret,logx,config,telemetry,httpx,netguard,lifecycle}` |
| Infrastructure | `readmodel`, `store` (+ `sqlite`, `memory`, `storetest`), `refresh`, `github` (+ `auth`, `githubtest`), `gitsource` (post-v1), `reactor`, `httpapi` (+ `authz`, `validate`, `openapi`, `apitest`), `apispec`, `webapp`, `web` (embed) |
| Features and services | `catalog`, `commits`, `docs` (+ `schema`, `internal/render`), `manifests` (+ `ecosystems/*`, `providers/*`), `badges`, `integrations` (+ jenkins, snyk), `pages`, `media`, `webhook`, `access`, `live`, `ops`, `overview` |
| Edges | `cmd/scp` (composition root, CLI, `assert.go`), `cmd/scp/internal/bridge`, `cmd/fakegithub`, `internal/archtest`, `tools/` (nested module), `web/` (TypeScript SPA) |

**Four rules that make it hold together.** (1) Features never import features; they communicate through the snapshot with kernel value types, consumer-defined interfaces (direct or bridged in `cmd/scp`), job specs, and normalised `scm.Event`s fanned out by `reactor.Fan`. (2) Composition happens only in `cmd/scp`; no DI framework, no globals, no `init()` in own code. (3) Failure is data: every item carries freshness; serving continues from the last good snapshot; readiness never regresses on upstream trouble. (4) Untrusted bytes (README, descriptor, manifests, webhook bodies) pass only through pure, bounded, fuzzed parsers and the single sanitiser that produces `SafeHTML`.

**CLI.** `scp serve`, `config lint|schema|show`, `descriptor lint`, `doctor`, `github app manifest`, `store check|restore`, `healthcheck`, `version`.

**Delivery.** Multi-stage image (Node 24 build, Go build, `distroless/static-debian13:nonroot` by digest), read-only root, UID 65532, `terminationGracePeriodSeconds: 45`, GoReleaser, CycloneDX SBOM, optional cosign, Renovate. CI runs `go test -race` (needs cgo) while releases are `CGO_ENABLED=0`.

## 5. Document table

Status vocabulary: **Final** (complete and not expected to change except by amendment), **Accepted** (decision record in force), **Design, verified** (design for implementation, independently verified 2026-10-14, usually `ready_with_caveats`), **Reference** (background evidence), **Generated catalogue** (single source for a key space), **Proposed** (written and awaiting owner sign-off).

### 5.1 Guardrails (02) and decisions (04)

| Document | Purpose | Status |
|---|---|---|
| [02-design-guardrails.md](02-design-guardrails.md) | 45 design guardrails (GR-01..GR-45): principle, rationale, applies-to and enforcing test for each | Accepted, **binding** |
| [04-decisions.md](04-decisions.md) | The decision record (principles, scope, Q-01..Q-25, D-01..D-17) and section 7 amendments A-01..A-33 | Accepted, **binding, highest precedence** |

### 5.2 Research (05)

Dated 2026-10-13/14. Evidence for the ADRs; consult to check a claim, not to design.

| Document | Purpose | Status |
|---|---|---|
| [05-research/01-docs-format-llm.md](05-research/01-docs-format-llm.md) | Documentation format and LLM surface (`llms.txt`, descriptor, front matter) | Reference |
| [05-research/02-go-stack-currency.md](05-research/02-go-stack-currency.md) | Go stack and library selection, currency checks | Reference |
| [05-research/03-github-api.md](05-research/03-github-api.md) | GitHub API facts for adapter, sync and webhooks (rate limits, ETags, GraphQL) | Reference |
| [05-research/04-go-architecture-patterns.md](05-research/04-go-architecture-patterns.md) | Package-by-feature, boundaries, lifecycle, hardening, testing | Reference |
| [05-research/05-store-sync-engine.md](05-research/05-store-sync-engine.md) | Persistence, snapshot read model and sync engine; prototypes in [05-store-sync-engine.assets/](05-research/05-store-sync-engine.assets/README.md) (engine, readmodel, benches; throwaway) | Reference |
| [05-research/06-web-ui-badges-security.md](05-research/06-web-ui-badges-security.md) | SPA delivery, live updates, badges, web security; prototypes in [06-prototypes/](05-research/06-prototypes/README.txt) | Reference (nonce CSP superseded, A-16) |
| [05-research/07-manifest-parsing.md](05-research/07-manifest-parsing.md) | Package manifest intelligence research (D-15) | Reference |
| [05-research/08-frontend-stack.md](05-research/08-frontend-stack.md) | Frontend stack and API contract research; prototypes in [08-prototypes/](05-research/08-prototypes/) (CSP, router, contract, bundle, graph benches) | Reference |
| [05-research/09-github-app-integration.md](05-research/09-github-app-integration.md) | GitHub App-first integration research (D-17) | Reference |

### 5.3 Architecture (10) and ADRs (11)

| Document | Purpose | Status |
|---|---|---|
| [10-architecture.md](10-architecture.md) | The architecture (revision 3): package map, dependency matrix, Go signatures, data model, runtime, HTTP surface, frontend, manifests, docs pipeline, errors, config, security, observability, deployment, guardrail enforcement map, feature traceability, CI, risks W1..W22 | Accepted (revision 3, 2026-10-14), **binding** |
| [ADR-001](11-adr/ADR-001-package-by-feature-layout-and-dependency-rule.md) | Package-by-feature layout and the dependency rule | Accepted |
| [ADR-002](11-adr/ADR-002-readmodel-snapshot-and-sqlite-store.md) | Immutable read model and SQLite store | Accepted |
| [ADR-003](11-adr/ADR-003-sync-engine-build-in-house.md) | In-house level-triggered sync engine (`refresh`), synctest | Accepted |
| [ADR-004](11-adr/ADR-004-github-client-and-auth.md) | Own thin GitHub client, App-first auth, REST baseline plus GraphQL sweep | Accepted |
| [ADR-005](11-adr/ADR-005-markdown-rendering-and-sanitising-pipeline.md) | goldmark pipeline to `SafeHTML` via bluemonday | Accepted |
| [ADR-006](11-adr/ADR-006-documentation-format-and-repository-descriptor.md) | CommonMark + GFM profile and the `scp.yaml` descriptor | Accepted |
| [ADR-007](11-adr/ADR-007-llm-surface.md) | `llms.txt` family, raw Markdown, negotiation, JSON | Accepted |
| [ADR-008](11-adr/ADR-008-template-engine.md) | No server page templates; `text/template` for SVG/text only | Accepted |
| [ADR-009](11-adr/ADR-009-live-update-mechanism.md) | Snapshot-version polling baseline, SSE in v1.1 | Accepted |
| [ADR-010](11-adr/ADR-010-spa-hosting-embedding-and-csp.md) | SPA embed behind a build tag, history fallback, static CSP | Accepted |
| [ADR-011](11-adr/ADR-011-local-badge-rendering.md) | Local flat-square SVG badges | Accepted |
| [ADR-012](11-adr/ADR-012-config-format-and-validation.md) | Config format and validation | Accepted |
| [ADR-013](11-adr/ADR-013-observability.md) | slog JSON, OpenTelemetry, Prometheus on admin | Accepted |
| [ADR-014](11-adr/ADR-014-http-server-and-routing.md) | HTTP server, middleware, routing | Accepted |
| [ADR-015](11-adr/ADR-015-resilience.md) | Per-upstream isolation, typed failures, stale-while-error | Accepted |
| [ADR-016](11-adr/ADR-016-test-strategy.md) | Test strategy | Accepted |
| [ADR-017](11-adr/ADR-017-deployment-image-and-release.md) | Image and release | Accepted |
| [ADR-018](11-adr/ADR-018-boundary-enforcement.md) | Closed-world archtest plus generated depguard rules | Accepted |
| [ADR-019](11-adr/ADR-019-frontend-react-shadcn-typescript.md) | React + shadcn/ui + TypeScript SPA (Svelte 5 considered) | Accepted |
| [ADR-020](11-adr/ADR-020-api-contract-openapi-first.md) | OpenAPI 3.1 spec-first, generated on both sides | Accepted |
| [ADR-021](11-adr/ADR-021-manifests-parser-strategy.md) | Manifest parsers: build vs adopt, purl identity | Accepted |
| [ADR-022](11-adr/ADR-022-manifests-acquisition-graph-and-snapshot.md) | Manifest acquisition, org graph, snapshot integration | Accepted |
| [ADR-023](11-adr/ADR-023-repository-identity-sources-and-visibility.md) | Identity, sources, slugs, visibility, access | Accepted |
| [ADR-024](11-adr/ADR-024-cross-feature-communication.md) | Snapshot, consumer interfaces, job specs, events | Accepted |
| [ADR-025](11-adr/ADR-025-security-model.md) | Security model and trust zones | Accepted |

### 5.4 Unit designs (20), with their verification reports (30)

Each design has reference code, fixtures or schemas under `<unit>.assets/` (unit 05 has fake-GitHub `worlds/` only and no Go code; unit 13 shares assets with unit 12 as noted in its text). All designs are "design for implementation"; the verdict column is from the verification report.

| Unit | Scope (packages) | Design | Verification | Verdict |
|---|---|---|---|---|
| 01 | Kernel: `scm`, `freshness`, `job`, `reactor`, `routes`, `safehtml` | [01-kernel.md](20-design/01-kernel.md), [routes.tsv](20-design/01-kernel.routes.tsv) | [design-01](30-verification/design-01-kernel.md) | Design, verified |
| 02 | Platform: `secret`, `logx`, `config`, `telemetry`, `httpx`, `netguard`, `lifecycle` | [02-platform.md](20-design/02-platform.md) | [design-02](30-verification/design-02-platform.md) | Design, verified and corrected |
| 03 | `readmodel`, `store`, `store/{sqlite,memory,storetest}` | [03-readmodel-store.md](20-design/03-readmodel-store.md) | [design-03](30-verification/design-03-readmodel-store.md) | Design, verified |
| 04 | `refresh` (engine, planner, scheduler, gate), `webhook` | [04-refresh-webhook.md](20-design/04-refresh-webhook.md) | [design-04](30-verification/design-04-refresh-webhook.md) | ready_with_caveats |
| 05 | `github`, `github/auth`, `githubtest`, fake GitHub | [05-github-adapter.md](20-design/05-github-adapter.md), [reference tests](20-design/05-github-adapter.reference-tests.md) | [design-05](30-verification/design-05-github-adapter.md) | Design, verified (signature level only; no code) |
| 06 | `httpapi` (+ `authz`, `validate`, `openapi`, `apitest`) | [06-httpapi.md](20-design/06-httpapi.md) | [design-06](30-verification/design-06-httpapi.md) | Design, verified |
| 07 | The OpenAPI contract: `api/`, `apispec`, per-feature fragments, `tools/apicheck` | [07-openapi-contract.md](20-design/07-openapi-contract.md), [endpoint catalogue](20-design/07-openapi-contract-catalogue.md), [api/](20-design/api/) | [design-07](30-verification/design-07-openapi-contract.md) | Design, verified and corrected |
| 08 | `catalog` | [08-catalog.md](20-design/08-catalog.md) | [design-08](30-verification/design-08-catalog.md) | Design, verified |
| 09 | `commits`, `badges` | [09-commits-badges.md](20-design/09-commits-badges.md) | [design-09](30-verification/design-09-commits-badges.md) | Design, verified |
| 10 | `docs`, `docs/internal/render`, `docs/schema`, `pages`, `media` | [10-docs-pages-media.md](20-design/10-docs-pages-media.md) | [design-10](30-verification/design-10-docs-pages-media.md) | Design, verified |
| 11 | `manifests` core: model, registry, acquisition, graph, API, outputs | [11-manifests-core.md](20-design/11-manifests-core.md) | [design-11](30-verification/design-11-manifests-core.md) | ready_with_caveats |
| 12 | Ecosystem parsers A: golang, npm, python, maven, gradle, cargo | [12-manifests-ecosystems-a.md](20-design/12-manifests-ecosystems-a.md) | [design-12](30-verification/design-12-manifests-ecosystems-a.md) | ready_with_caveats |
| 13 | Ecosystem parsers B and optional providers | [13-manifests-ecosystems-b.md](20-design/13-manifests-ecosystems-b.md) | [design-13](30-verification/design-13-manifests-ecosystems-b.md) | ready_with_caveats |
| 14 | `integrations` (Jenkins, Snyk), `access` | [14-integrations-access.md](20-design/14-integrations-access.md) | [design-14](30-verification/design-14-integrations-access.md) | Design, verified |
| 15 | `live`, `ops`, `overview` | [15-live-ops-overview.md](20-design/15-live-ops-overview.md) | [design-15](30-verification/design-15-live-ops-overview.md) | ready_with_caveats |
| 16 | `cmd/scp`, `bridge`, `archtest`, build, CI, image, release | [16-cmd-build-release.md](20-design/16-cmd-build-release.md), [seed files](20-design/16-seed-files/) | [design-16](30-verification/design-16-cmd-build-release.md) | ready_with_caveats |
| 17 | `web/` foundation: tooling, structure, shared layers | [17-web-foundation.md](20-design/17-web-foundation.md) | [design-17](30-verification/design-17-web-foundation.md) | Design, verified |
| 18 | `web/src/features/*`: pages and components | [18-web-pages.md](20-design/18-web-pages.md) | [design-18](30-verification/design-18-web-pages.md) | Design, verified |

Consolidated references (generated by the cross-design review; one source per key space):

| Document | Purpose | Status |
|---|---|---|
| [90-config-reference.md](20-design/90-config-reference.md) | Every configuration key: owner unit, type, default, env name, range (370 keys, 24 root sections) | Generated catalogue |
| [91-metrics-catalogue.md](20-design/91-metrics-catalogue.md) | Every metric instrument (179 names), units, labels (closed sets) | Generated catalogue |
| [92-error-catalogue.md](20-design/92-error-catalogue.md) | Closed `ErrCode` vocabulary and the 19 RFC 9457 problem codes | Generated catalogue |
| [93-deviations-and-open-questions.md](20-design/93-deviations-and-open-questions.md) | Consolidated decisions CD-xx, owner list, index of all 549 unit deviation and question rows with a recommendation each | Generated, authoritative for open items |
| [api/](20-design/api/) | Bundled sources for the contract: `openapi-public.yaml`, `openapi-admin.yaml`, `common.yaml`, `trusted-members.txt` | Copy-from source |

### 5.5 Verification (30)

| Document | Purpose | Status |
|---|---|---|
| [architecture-review-go.md](30-verification/architecture-review-go.md) | Independent Go idiom and design review of 10 and the ADRs (1 blocker, 14 major, 11 minor; all valid ones fixed in revision 2) | Final |
| [cross-design-consistency.md](30-verification/cross-design-consistency.md) | The 18 designs read and compiled together: 63 findings (52 fixed or resolved, 8 recorded as requirements, 3 open) | Final (revision 3) |
| `design-01-kernel.md` to `design-18-web-pages.md` | One verification report per unit: findings, fixes, caveats (see the table in 5.4 for links) | Final |

### 5.6 Plans and seed (40, 41, 50)

| Document | Purpose | Status |
|---|---|---|
| [40-implementation-plan.md](40-implementation-plan.md) | Ordered build plan: milestones, tasks, dependencies, exit criteria, critical path, work streams, risks, Definition of Done | Proposed |
| [41-test-plan.md](41-test-plan.md) | Test strategy, inventory, budgets, fixtures, CI stages and traceability from guardrails to tests | Design |
| [50-repo-seed/](50-repo-seed/) | Starter files of this repository (`repo/`), the in-place `bootstrap.sh`, the design map, seed notes and seed checks | Proposed |

The repository-root seed files referenced by unit 16 are in [20-design/16-seed-files/](20-design/16-seed-files/) (design-time form); `50-repo-seed/repo/` is the newer form that `bootstrap.sh` lays into this repository. Reference code per unit is in `20-design/*.assets/`.

## 6. Glossary

| Term | Meaning |
|---|---|
| **SCP** | Source Code Portal, the product. |
| **A-nn** | Amendment row in 04, section 7 (A-01..A-33): where research or review changed or refined a decision. |
| **ADR-nnn** | Architecture decision record in `11-adr/`. |
| **Admin listener** | Second HTTP listener (default `:9091`) for `/status`, `/metrics`, `/debug/pprof` and the admin API; not exposed by the ingress. |
| **archtest** | Closed-world import-boundary test (`internal/archtest`); the rank table and allowed-import matrix are generated into depguard rules. |
| **Bridge** | Small type in `cmd/scp/internal/bridge` that adapts one feature's data or `refresh` to another consumer's interface; wiring code, not a feature. |
| **CD-xx** | Consolidated decision of the cross-design review, recorded in 93, section 2. |
| **Consumer-defined interface** | Interface declared by the package that uses it ("accept interfaces, return structs"); satisfied directly (kernel types only) or through a bridge. |
| **D-nn / Q-nn** | Decision and open question (04, 04 section 3). |
| **Descriptor** | Optional repository file `scp.yaml` (root or `.github/`): title, owners, lifecycle, tags, links, docs ordering, integration overrides. |
| **Engine / planner / scheduler / gate** | The `refresh` package: keyed coalescing job queue with per-upstream pacing; level-triggered planner deciding what is due; local housekeeping scheduler; rate-budget gate. |
| **F-nn** | Finding of the independent Go architecture review (`30-verification/architecture-review-go.md`), F-01..F-26. |
| **G-xx / N-01** | G-01..G-14: platform-capability ids defined in 04 section 2 and cited in the traceability sections of the unit designs (each unit table names them, e.g. G-08 visibility, G-10 route table); N-01: the new-feature id in 04 (package manifest intelligence). |
| **GR-nn** | Binding design guardrail in [02-design-guardrails.md](02-design-guardrails.md), GR-01..GR-45. |
| **Freshness** | Per item: stored state (ok, error, ...), derived status (fresh, stale, ...), age and policy; carried in every API response. |
| **GitHub App (first)** | Recommended integration: installation-scoped, short-lived tokens, event-driven discovery; PAT, OAuth, anonymous and external-command strategies are alternatives behind `TokenSource`. |
| **Group** | Logical system: a set of repositories selected by typed matchers; a repository may be in several groups. |
| **Job / job kind** | A unit of refresh work registered by a feature (`commits.refresh`, `docs.refresh`, `catalog.reconcile`, ...); keyed and coalesced; the job kind may differ from the store item kind (A-33). |
| **Kernel** | Rank-0/1 vocabulary packages (`scm`, `freshness`, `job`, `routes`, `safehtml`) with no behaviour beyond small pure helpers. |
| **llms.txt family** | Server-generated `/llms.txt`, `/llms-full.txt`, scoped variants and raw Markdown endpoints for LLM agents. |
| **M0** | First implementation milestone: spikes and prototypes (W1, W3, W21, W22) before any feature. |
| **Manifest / component / purl** | Parsed package-manager file; a dependency or artifact; Package URL, the identity of a component. |
| **Rank** | Position of a package in the dependency matrix; a package may import only lower or equal listed ranks per 10, section 5.2. |
| **Reactor** | `reactor.Func` / `reactor.Fan`: synchronous fan-out of one normalised `scm.Event` to every feature's reaction function. |
| **ReadModel / Snapshot / Slot** | Immutable serving state published through an atomic pointer; feature-agnostic typed slots hold each feature's data. |
| **RepoRef / Slug** | Repository identity `{Source, ID, Owner, Name}`; `Slug` is the unique single-segment portal id used in URLs, with rename aliases. |
| **SafeHTML** | Distinct type for sanitised HTML; only `safehtml.Sanitize` constructs it; stored HTML is re-sanitised at load. |
| **Seed files** | Repository-root files under `20-design/16-seed-files/` copied verbatim into the new repository. |
| **Source** | A GitHub organisation or user account reached through an App installation or token; one or many per deployment. |
| **Spike S-A..S-D, W-nn, V-nn** | Named open technical items: S = spike (S-A installation tokens for dependency graph, S-B GraphQL blob batching, S-C Maven Central parent-POM policy, S-D graph heap), W = architecture risk (section 23), V = verified fact in section 0. |
| **StatusProvider** | Interface for optional Jenkins and Snyk integrations returning structured status. |
| **Store** | Durable, rebuildable last-good state in embedded SQLite (`modernc.org/sqlite`), never on the request path. |
| **synctest** | Go's `testing/synctest`; virtualises time so there is no `Clock` interface. |
| **TokenSource / SourceReader** | Credential strategy and acquisition mode (api, webhook-assisted, optional git) behind the GitHub adapter. |
| **ViewClass** | Visibility class of a caller (public or all); global projections are computed per class. |
| **Unit** | One of the 18 design documents in `20-design/`, numbered 01..18. |

## 7. Binding decisions at a glance

Full text in [04-decisions.md](04-decisions.md); amendments in its section 7 override the original wording where marked "changes".

**Principles (section 1).** Package by feature; clean boundaries via consumer interfaces; SRP; composition root only in `cmd/scp`; stdlib first; correct by construction (typed ids, `Secret`, `SafeHTML`, immutable snapshots, validated config); failure is first-class; observable and testable; the design guardrails are binding (02); LLM-friendly surface.

**Decisions D-01..D-17.**

| ID | Decision (as amended) |
|---|---|
| D-01 | One Go module; `cmd/scp` plus `internal/` feature and platform packages; package set and names per A-01 (`sync` became `refresh`, `store` split into `readmodel` + `store`, kernel packages added) |
| D-02 | Immutable `Snapshot` behind an atomic pointer; SQLite (modernc, WAL) as last-good store behind a narrow `Store`; no otter cache (A-02) |
| D-03 | In-house sync engine with keyed coalescing queue, per-upstream limits, jittered backoff, rate-limit pause, readiness gating; features register jobs (A-03, A-21) |
| D-04 | `github` adapter owns client, auth, conditional requests, pagination, rate limits, breaker, typed errors, webhook types; own thin client, content-SHA validators (A-04) |
| D-05 | HTTP server, middleware chain, route table, public/admin split stand; templates and HTMX superseded by D-16 (A-05) |
| D-06 | Markdown (CommonMark + GFM) is the documentation format; `scp.yaml` descriptor; goldmark + chroma + bluemonday into `SafeHTML`; `llms.txt`, raw `.md`, JSON API (A-06, A-26) |
| D-07 | One typed, versioned YAML config, `SCP_` env, `*_FILE` secrets, strict decoding, generated JSON Schema, documented keys equal consumed keys (A-07, A-28) |
| D-08 | slog JSON, OpenTelemetry, Prometheus on the admin listener; `/livez`, `/readyz`, `/status` (A-08, A-24, A-32) |
| D-09 | Typed redacted secrets, HMAC-SHA256 webhooks with de-duplication, CSP, sanitised content, SSRF guard, non-root read-only container (A-11) |
| D-10 | Unit/table/golden/fuzz tests, fake GitHub, `-race`, goleak, import-boundary test, route and e2e smoke (A-09) |
| D-11 | `CGO_ENABLED=0` static binary, distroless by digest, GitHub Actions CI, Renovate, GoReleaser (A-10) |
| D-12 | Canonical routes; ServeMux-legal patterns per A-12 (`/badges/{repo}/{file}`, `/webhooks/github[/{source}]`, `/assets/<name>-<hash>.<ext>`) |
| D-13 / D-17 | Sources model one or many orgs; repository identity `source/org/repo`; GitHub App first; `TokenSource` and `SourceReader` pluggable; configurable hosts for GHES/GHEC-DR |
| D-14 | Hygiene: docs, ADRs, CI gate; `llms.txt` generated by the app and checked in; generated source committed and drift-checked, build output not (A-14) |
| D-15 | Package manifest intelligence (N-01): all major ecosystems, purl identity, static parsing only, org dependency graph; own pure parsers plus adopted leaf libraries (A-15) |
| D-16 | React + shadcn/ui + TypeScript SPA embedded in the binary; OpenAPI 3.1 spec-first; Zod only at route params and in dev/test; static CSP without nonce; polling baseline, SSE in v1.1 (A-16, A-17) |

**Answers to the open questions (selection).** Q-02 canonical clean routes only; Q-03 public repositories only by default, optional trusted-proxy auth for private; Q-05 Jenkins and Snyk optional, no Shields.io calls; Q-06 Markdown, AsciiDoc only as GitHub-rendered HTML; Q-07 many-to-many groups with typed anchored matchers; Q-09 sizing and staleness SLOs (App installation budget corrected to 5,000 + 50/h per repository above 20, capped at 12,500/h, A-04); Q-10 single replica; Q-11 health is `/livez` and `/readyz` only; Q-16 UTC storage and configurable display zone; Q-18 default-branch commit view; Q-19 dark mode; Q-23 OCI distroless, Kubernetes-friendly; Q-24 organisation or repository webhook with SHA-256 signature.

**Scope table.** In scope: repository groups and discovery, dashboard, group and repository pages, commit logs, documentation rendering, local badges, webhooks, freshness and readiness, configuration, observability, config-driven pages and announcement, platform capabilities G-01..G-14, and package manifest intelligence (N-01); full list in 04 section 2 and [10-architecture.md section 21](10-architecture.md). Optional: Jenkins and Snyk status providers. Out of scope: non-GitHub providers (Bitbucket), a signed knowledge manifest, and the items listed as out of scope in section 4.

**Recent amendments (cross-design review).** A-27 to A-33: completed consumer interfaces, one config key table, route ownership and admin superset, matrix additions, shutdown 44 s, one metric catalogue, closed `ErrCode` vocabulary and job-kind translation.

## 8. Product behaviour highlights

Read from 04 (Q-02, Q-04..Q-06, Q-11, Q-16..Q-20, A-19, A-25), 10 (section 11.2, 12.3, 21, W19) and unit 18. Items marked **[owner]** are also in section 9.

| # | Area | Behaviour |
|---|---|---|
| 1 | UI technology | React SPA over a JSON API; history-fallback routing; real 404 status for unknown shapes |
| 2 | URLs | Canonical `/dashboard`, `/groups/{id}`, `/groups/{id}/commits`, `/commits`, `/repos/{repo}`, `/repos/{repo}/commits`, `/repos/{repo}/docs/{path}`, `/pages/{slug}`, `/dependencies`, `/packages?purl=`; one route table (GR-15); group ids lower-cased |
| 3 | Health and diagnostics | **[owner]** `/livez` and `/readyz` on the public listener and no other health paths (Q-11); `/status`, `/metrics`, `/debug/pprof` on a separate admin listener (GR-17, GR-41) |
| 4 | Repository discovery | GitHub App installation discovery, paginated, scheduled reconcile plus installation events, mass-removal guard (GR-26) |
| 5 | Dashboard order | **[owner]** Config order (sort option available) |
| 6 | Dashboard and group commit lists | **[owner]** 10 latest commits on dashboard and group pages (SPA constant), full log one click away |
| 7 | Commit rows | **[owner]** First line of the message (capped); author initials on a deterministic colour, no third-party request; avatar proxy possible in v1.1 |
| 8 | Commit log scope | Default branch only (Q-18); no per-branch column (Q-20) |
| 9 | Badges | Rendered locally as flat-square SVG (GR-33); kinds issues, last-commit, release, licence, build, security, group repository-count at `/badges/groups/{groupId}/{file}`; unknown kind or repo is 404, known-but-unavailable is grey; no calls to third-party badge services |
| 10 | Jenkins and Snyk | Optional, off by default, structured data via `StatusProvider`; Snyk REST API with token |
| 11 | Documentation | Markdown (CommonMark + GFM) primary; AsciiDoc only as GitHub-rendered HTML, flagged as not natively rendered; `scp.yaml` descriptor and front matter; GitHub alerts; Mermaid in a sandboxed iframe from v1.1; external images through a signed `/media` proxy; **[owner]** external SVG denied by default; `docs/` directory supported **[owner]** |
| 12 | Pages and announcement | Config-driven Markdown `pages` and an optional dismissible `announcement` |
| 13 | Group membership | Many-to-many; typed matchers (exact, prefix, glob, anchored RE2 regexp, topic, catch-all) (GR-09); forks excluded by default **[owner]** |
| 14 | Visibility | Public only by default; private repositories require trusted-proxy auth mode; startup refuses otherwise; anonymous enumeration test (GR-35) |
| 15 | Webhooks | `/webhooks/github[/{source}]`; HMAC-SHA256 only, constant-time comparison; delivery de-duplication; 202/400/401/503 mapping (GR-20, GR-21) |
| 16 | Freshness | Per-item freshness with visible staleness, stale-while-error, readiness gating, header shows last full sync and repository count (Q-17) |
| 17 | Time | Stored UTC; configurable display zone (default UTC); browser-local via `<time datetime>` (GR-38) |
| 18 | Theme | Light, dark and system, persisted override, accessible contrast and no flash on navigation (Q-19, GR-39) |
| 19 | Configuration | One strict YAML, `SCP_<PATH>` env, `*_FILE` secrets; unknown key is an error; `scp config lint|schema|show` (GR-12) |
| 20 | Package manifest intelligence | 16+ ecosystems: per-repo technology and dependency panel, org-wide internal dependency graph, reverse dependencies, version spread, licences, CycloneDX export |
| 21 | LLM and API surface | `/api/v1` (OpenAPI), `/llms.txt`, `/llms-full.txt`, scoped files, raw Markdown, `Accept: text/markdown`, `sitemap.xml`, `robots.txt` |
| 22 | Providers | GitHub only (GitHub.com, GHES, GHEC-DR) |
| 23 | Deployment | Static binary and distroless non-root image, Kubernetes manifests, graceful 44 s worst-case shutdown |

## 9. Owner confirmations needed

Everything below is an open item. Every item has a **default that ships** if the owner does not answer; the owner may overrule at any time. The authoritative source is [93, section 3](20-design/93-deviations-and-open-questions.md) plus [cross-design-consistency.md section 11](30-verification/cross-design-consistency.md), [04, end of section 7](04-decisions.md) and [10, section 23](10-architecture.md).

### 9.1 Decisions for the owner

| # | Item | Source | Default that ships |
|---|---|---|---|
| OC-01 | Product behaviour choices (A-25, W19): commit authors render as initials (no avatar fetch), first line of the commit message, dashboard in config order, 10 commits on dashboard and group pages, health served only at `/livez` and `/readyz` | 04 A-25; 10 W19; 93 item 3; unit 18 D-18-05 | As designed in section 8 (all five ship). An avatar proxy (`/avatars/{login}`) is additive in v1.1 |
| OC-02 | Reading of Q-20: "no sub-folder docs" means "no monorepo sub-project docs"; a `docs/` directory (default `docs.root`) is supported | 04 A-06; 10 W18 | `docs/` supported |
| OC-03 | Descriptor lifecycle vocabulary: Backstage `experimental`, `production`, `deprecated` instead of 04's `active`; UI may label "Active" | 04 A-06; 10 W18; 93 item 3 | `production`, labelled "Active" in the UI. Confirm before the descriptor schema freezes |
| OC-04 | Admin listener also serves the 22 public operations (superset) plus `getStatus` and `retryJob` | 93 item 1; 01 Q9/D14; CD-RT-08 | Keep the superset (22 + 2 operations) |
| OC-05 | External SVG through `/media/ext`: keep `deny` or make `sandboxed` the default (40 of 53 external images in 13 real READMEs are SVG badges) | 93 item 2; 10 D-1 | Code default `deny` until approved; evidence recommends approving `sandboxed` |
| OC-06 | Camo behaviour for external images and badges (does GitHub's proxy follow redirects, honour short `max-age`, cache 404 bodies); unverified | 93 item 4; 09 Q-3; A-19 | Badge URLs are served directly (no redirect); manual check with a real README (task H4) |
| OC-07 | Rate-limit pause is global per upstream `github` in v1 (not per installation) | 93 item 5; 04 Q1; 05 Q-10 | Single upstream, global pause; per-installation pause is v1.1. Confirm before multi-source deployments |
| OC-08 | The reserve gate lives in the GitHub adapter (not in `refresh`) | 93 item 6; X6; 16 Q16-1 | Adapter owns it; `bridge.RateGate` maps `refresh.Gate` |
| OC-09 | Keep both concurrency knobs (`github.defaults.budget.*` and `refresh.upstreams.<n>.*`, validated); 03 P1 plan table not adopted | 93 items 7 and 8; CD-CF-05 | Keep both; plan table not adopted |
| OC-10 | Two view classes only (fail closed); empty groups hidden for everyone; `discovery.include_forks` stays `false` (forks can include repositories a group names as its default; an operator opts in explicitly) | 93 item 9; 06 Q1; 08 Q-1, Q-2 | Two classes; empty groups omitted and surfaced in `/status`; forks excluded |
| OC-11 | Image namespace and repository name `ghcr.io/cantara/source-code-portal` | 16 Q16-8 | `ghcr.io/cantara/source-code-portal`; must be confirmed before the first release |
| OC-12 | No signed `knowledge.yaml` in the repository (Q-22); cosign keyless signing instead | 16 Q16-7 | None; cosign keyless |
| OC-13 | Stay on Node 24 for v0.1 (Node 26 becomes LTS 2026-10-28) | 16 Q16-12 | Node 24.21.0 |
| OC-14 | Enable the merge queue on the new repository (workflows have a `merge_group` trigger) | 16 Q16-16 | Enabled |
| OC-15 | No early `/livez` listener; listeners open after the store is opened and checked; startup probes cover it | 16 Q16-19 | No early listener |
| OC-16 | SIGHUP reload scope: pages and App key only; configuration is not re-read | 16 D-15 | As stated; owner may veto |
| OC-17 | Shutdown worst case 44 s, supervisor grace 45 s (architecture 10.4 said 38 s) | 16 D-12; A-31 | 44 s / 45 s |
| OC-18 | Default admin bind `:9091` with an open-admin startup warning (loopback is safer but breaks cross-pod Prometheus); Host guard off by default | 93 item 11; 02 Q5, Q15 | `:9091` with warning; Host guard off |
| OC-19 | Admin SSE instance and related unit-15 questions (Q4, Q5, Q7, Q8) | 93 item 12 | Admin SSE instance dropped (the admin table has no `events` row); other unit defaults adopted |
| OC-20 | Optional-provider policies: Snyk regional hosts (US-01, US-02, EU-01, AU-01; tokens are region-specific by hint only); Maven Central parent-POM fetch policy S-C | 93 item 13; 04 section 7 end; 14 V1 | Providers off by default; `PomSource` off by default and never asked for internal coordinates |
| OC-21 | `cmd/fakegithub` needs a lint and archtest allowance (second `main`) | 93 item 14; 05 D-11 | Allowance granted |
| OC-22 | Matrix additions of A-30 (`httpapi/openapi` imported by `commits`, `manifests`, `overview`; `go.yaml.in/yaml/v3` in `docs/internal/render`; route-literal allowance) | 93 item 15; CD-DM-01..03 | Matrix cell added now; generated `*api` alias packages recommended later |
| OC-23 | Not adopted for v1: server-side commit filters `author` and `q` (D-18-02); config key `site.dashboard_commits` (D-18-03) | 93 item 16; CD-CF-12 | Not adopted; owner may overrule |
| OC-24 | Trusted Types: report-only first, enforced last, because React 19.3 with the shadcn components is unverified | 04 section 7 end; 10 W9 | Report-only, Playwright CSP gate |

### 9.2 Open technical items (not owner decisions; listed because they block or gate work)

| Item | What is open | Gate and default |
|---|---|---|
| W1 | oapi-codegen 3.1 support is initial; multi-type unions generate `any` | Spike in M0; restrict the spec to a small 3.1 subset; Overlay downgrade path to 3.0; Huma recorded as alternative |
| W3 | `ProblemMux` for `/api` 404/405 not prototyped | M0; make "unsupported methods" a CI gate |
| W21 | modernc driver: `file:...?mode=ro` for the backup connection and `temp_store=MEMORY` on a read-only root unverified | M0 spike; fallback is a read-only-by-discipline `bk` connection and an `emptyDir` at `/tmp` |
| W22 | `Planner`, `Gate`, `Timeout`, `reactor`, `lifecycle.Plan` specified but not prototyped | M0 against `store/memory` in synctest bubbles |
| S-A (W2) | Do App installation tokens work for `dependencyGraphManifests` and the async SBOM endpoints; the synchronous SBOM endpoint is removed 2026-11-13 | Spike needs a real App and a private repository; providers P5/P6 stay off by default |
| S-B (W5) | GraphQL blob batching cost at 25 aliases | REST raw fallback complete behind the same interface; cold start at `PrioBackfill` |
| S-D (W15) | Heap of the interned graph (about 6 MB estimated) | Measure with `runtime/metrics`; weighted semaphore bounds the risk |
| W20 | Push payload per-commit path arrays and `head_commit.tree_id` undocumented | Head-sweep tree comparison within 5 minutes is the backstop; record a real delivery in M0 |
| CD-CF-10 | `catalog.Config` Describe tags and one-key matcher `UnmarshalYAML` unowned until units 02 and 08 are edited | Edit units 02 and 08 before the real build |
| CD-MT-06 | Telemetry View override discards histogram bucket advice | Unit 02 passes explicit buckets in its View |
| CD-DM-03 | Route-literal archtest rule needs an allowance for route-table files | Add the allowance rule (same as OC-22) |
| CD-FX | Fake GitHub gaps (dependency-graph SBOM, GraphQL `dependencyGraphManifests`, `object(oid:)` blob batch, docs/`scp.yaml`/CODEOWNERS/images in the demo world) | Extend the demo world with the first consumers |
| Unit 05 | No code, signature-level check only: reserve gate, batched blob read and 5xx classification unproven | Prove during unit 05 implementation against the unit 05 reference tests |
| Overview accessors | `overview` read accessors on `docs`, `pages`, `badges`, `manifests`, `integrations` (CD-IF-07) were compiled against stand-ins | First real build may adjust signatures |
| Library facts | All pins date from 2026-10-13/14 | Re-verify at implementation time |

## 10. Deviations proposed by this index

None. This README restates the architecture and decisions as written. One observation is recorded for the owner without changing any design:

- 93's unit 16 and unit 05 rows still contain statements made before the designs were completed (for example Q16-10 says unit 12 had no design document; it now has one, with a verification report). Treat the unit documents and verification reports as newer than those row texts.

## 11. Conventions and status of this package

- **Precedence:** `04-decisions.md` (including section 7 amendments) > `10-architecture.md` > `11-adr/` > unit designs `20-design/` (each carries a "Cross-design review amendments (2026-10-14)" section where edited) > research `05-research/`. The guardrails in `02-design-guardrails.md` are binding throughout. A design may propose a change only in its "Deviations" section with evidence.
- **Dates:** every document is dated 2026-10-13 or 2026-10-14. Library, version and standard facts were verified on those dates (see the verification log in 10, section 0, and each unit's verification section). This index adds no library claims; it was produced from reading 04, 10, 93, the cross-design review and the document headers on 2026-10-14.
- **Reference code** under `20-design/*.assets/` and `05-research/*` prototypes is illustrative, tested at design time, and not part of the future repository layout except where a unit says "copy".
- **Links:** every relative link in this file and in [llms.txt](llms.txt) was checked to resolve on 2026-10-14.
