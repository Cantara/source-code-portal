# Design map: from design document to code

The design package lives in this repository at `.claude/work/plans/` (owner layout rule, 2026-10-14: planning and design under
`.claude/work/`, `docs/` for user documentation only). It is **binding** and changes only by a deliberate, reviewed change: do not edit a
document to make a change true; change the code, or propose the change (a new ADR in `11-adr/`, or a "Deviations" section).
Paths in this file are relative to `.claude/work/plans/`.

Precedence when documents disagree: `04-decisions.md` (section 7 amendments win over section 4) > `10-architecture.md` > `11-adr/` >
`20-design/NN-*.md` > background. `20-design/93-deviations-and-open-questions.md` and `30-verification/cross-design-consistency.md` record the
cross-unit conflicts that were found and how they were settled.

Read `README.md` first: it is the index and user guide of the design package (what it is, reading orders for engineers, reviewers and agents,
glossary, the owner confirmations with the defaults that ship). `llms.txt` is the same
index in the llms.txt format. This file is the map from each document to where it lands in the code.

## Map

| Path | What it is | Use it when |
|---|---|---|
| `README.md`, `llms.txt` | Index, user guide and glossary of the design package | first |
| `02-design-guardrails.md` | The design guardrails `GR-01`..`GR-45`: principle, rationale, scope and enforcing test of each | reviewing any change; tracing why a rule exists |
| `04-decisions.md` | Product scope and the binding decisions D-01..D-17, amendments A-nn in section 7 | any decision question |
| `05-research/` | Dated web research per area, with throw-away prototypes (`*.assets`, `*-prototypes`) | re-verifying a library or standard |
| `40-implementation-plan.md` | Ordered build plan: milestones M0 to M11, task ids per unit, owner decisions O-n, spikes, effort | planning the next milestone |
| `41-test-plan.md` | Test strategy, inventory, budgets, traceability of every guardrail to a test | writing or reviewing tests |
| `10-architecture.md` | The architecture: package map (4), dependency matrix (5), core signatures (8), data model (9), runtime (10), HTTP (11), frontend (12), manifests (13), docs pipeline (14), errors (15), config (16), security (17), observability (18), deployment (19), guardrail enforcement mapping (20), traceability (21), CI (22), risks (23) | always |
| `11-adr/ADR-001..025` | Decision records (layout and rank rule, snapshot and SQLite, sync engine, GitHub client, rendering pipeline, ... boundary enforcement, OpenAPI-first, security model) | before touching the area |
| `20-design/NN-*.md` | One design document per unit (below), each with scope, Go types, behaviour, tests, tasks, deviations and open questions | implementing the unit |
| `20-design/NN-*.assets/` | Compile-checked reference code and fixtures of the unit (throw-away prototypes; the document says what to copy) | starting an implementation |
| `20-design/90-config-reference.md` | The complete configuration key table (owner, type, default, env, range) | adding or reading a key |
| `20-design/91-metrics-catalogue.md` | Every metric and its labels | adding an instrument |
| `20-design/92-error-catalogue.md` | Problem codes and the closed `ErrCode` vocabulary | adding an error |
| `20-design/93-deviations-and-open-questions.md` | All unit deviations and open questions with decisions, plus the owner-confirmation list | before deciding anything open |
| `20-design/api/` | The contract in single-file design form (`common.yaml`, `openapi-public.yaml`, `openapi-admin.yaml`, `trusted-members.txt`) | task T-1 of unit 07 |
| `20-design/16-seed-files/` | The repository-root files as designed (this seed's source) | comparing the seed with its design |
| `30-verification/` | Independent verification reports per unit, the Go-idiom architecture review, the cross-design consistency review | judging how far to trust a statement |

## Units, packages, documents and code assets

Implementation order follows design 16 section 4.12 and 13 (bootstrap steps and tasks B-1 to B-14). "Assets" names the directory under
`20-design/` holding reference code; each unit's document states which files are real and which are stand-ins. Never copy stand-ins.

| Unit | Packages (target) | Document | Assets to start from |
|---|---|---|---|
| 01 | `scm freshness job reactor routes safehtml` | `01-kernel.md` (+ `01-kernel.routes.tsv`) | `01-kernel.assets/internal/*` (real) |
| 02 | `platform/{secret,logx,config,telemetry,httpx,netguard,lifecycle}` | `02-platform.md` | `02-platform.assets/internal/platform` (real, copy as is) |
| 03 | `readmodel store store/{sqlite,memory,storetest}` | `03-readmodel-store.md` | `03-readmodel-store.assets/internal/*` |
| 04 | `refresh webhook` | `04-refresh-webhook.md` | `04-refresh-webhook.assets/internal/{refresh,webhook}` (`ghverify` is a reference only) |
| 05 | `github github/auth github/githubtest cmd/fakegithub` | `05-github-adapter.md`, `05-github-adapter.reference-tests.md` | `05-github-adapter.assets/worlds` (fake worlds; no code) |
| 06 | `httpapi httpapi/{authz,validate,apitest} webapp web` | `06-httpapi.md` | `06-httpapi.assets` |
| 07 | `api/` `httpapi/openapi apispec <feature>api tools/apicheck` | `07-openapi-contract.md`, `07-openapi-contract-catalogue.md` | `07-openapi-contract.assets/tools` (`split.py`, `apicheck`), `20-design/api/` |
| 08 | `catalog` | `08-catalog.md` | `08-catalog.assets/mod/internal/catalog` |
| 09 | `commits badges` | `09-commits-badges.md` | `09-commits-badges.assets/internal`, `tools/badgewidths` |
| 10 | `docs docs/internal/render docs/schema pages media` | `10-docs-pages-media.md` | `10-docs-pages-media.assets/internal` (+ `safehtml-D2-D3.patch`) |
| 11 | `manifests manifests/internal/* manifests/parsertest` | `11-manifests-core.md` | `11-manifests-core.assets/internal/manifests` |
| 12, 13 | `manifests/ecosystems/*` (six, then eleven) and the optional providers | `12-manifests-ecosystems-a.md`, `13-manifests-ecosystems-b.md` | `12-...assets/overlay` (+ `apply.sh`), `13-...assets` |
| 14 | `integrations integrations/{jenkins,snyk} access` | `14-integrations-access.md` | `14-integrations-access.assets/internal` |
| 15 | `live ops overview` | `15-live-ops-overview.md` | `15-live-ops-overview.assets/internal` |
| 16 | `cmd/scp cmd/scp/internal/bridge archtest`, Makefile, CI, image, release | `16-cmd-build-release.md` | `16-cmd-build-release.assets/cmd`, `16-seed-files/` (already in this repository as the seed) |
| 17 | `web/` tooling and shared layers | `17-web-foundation.md` | `17-web-foundation.assets/web` (the seed carries its config layer and a shell) |
| 18 | `web/src/features/*` | `18-web-pages.md` | `18-web-pages.assets/web` |

## Where the seed differs from the design files

`.claude/work/plans/50-repo-seed/seed-notes.md` lists every change the seed made against `20-design/16-seed-files/` and the unit 17 prototype, each with its reason and
the open items it leaves (staged CI, `BUILD_ID`, the A-30 matrix cells, the route-table allowance, placeholders, the web shell).
