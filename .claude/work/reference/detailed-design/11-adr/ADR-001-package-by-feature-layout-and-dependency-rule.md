# ADR-001: Package-by-feature layout and the dependency rule

Status: Accepted (owner-delegated, principles 1-4 of 04). Date: 2026-10-14. Amends 04 D-01 and the section 5 package table (see 04 section 7, A-01).

## Context
04 requires package by feature, consumer-defined interfaces, a composition root only in `cmd/scp`, and an enforced import boundary. Research compared three layouts: A (vertical features, tiny kernel, ports at the consumer), B (hexagonal inside every feature, 50+ packages) and C (Ben Johnson root domain package, which creates a models bucket and producer-defined interfaces). Shared "model" packages and global singletons create import cycles and hidden coupling, so the layout must make both impossible (GR-03, GR-07, GR-11).

## Decision
Layout A with a rank-based rule (architecture section 5):
- Kernel (rank 0-1): `scm` (value types and the portal identity of a repository), `freshness` (status vocabulary), `job` (feature-to-engine contract), `routes` (route table, recording mux and URL builders), `safehtml` (the `SafeHTML` type and its only constructor). No behaviour beyond small pure helpers; nothing in the kernel may import an adapter or a feature.
- Platform, infrastructure, features/services, `overview`, adapters, `cmd/scp` by rank; every allowed import edge strictly decreases rank, therefore the graph is acyclic.
- Features and services (`catalog commits docs manifests badges pages integrations media webhook access live ops`) import only the kernel, `readmodel`, `store`, `httpapi`, `httpapi/authz`, `platform/{secret,logx}`, the OpenTelemetry **API** modules and their own subpackages. They never import each other, adapters, `refresh`, `reactor`, `webapp`, `overview` or `httpapi/validate`.
- Cross-feature reads happen through the snapshot with kernel-typed slots, consumer-defined interfaces, `job.Spec` registration and `scm.Event` reactors (ADR-024). The only non-wiring package allowed to import several feature roots is `overview` (read composition only); the bridges that implement one feature's consumer interface over another's data live in `cmd/scp/internal/bridge`, which is wiring.
- Nested `internal/` packages hold feature-private code (`docs/internal/render`, `manifests/internal/*`). Package names `util common helper model models types api interfaces base` are banned.
- No DI framework, no globals, no `init()` in our own non-generated code (the SQLite driver registers itself in its own `init` and is imported blank by `store/sqlite`; third-party `init` is outside the rule).

## Alternatives considered
- Layout B (hexagonal per feature): about 25 extra packages for one-implementation ports (Q-12), the GitHub adapter would import every feature's ports. Reserved for a feature that grows several adapters; `manifests` is the only candidate and already has `ecosystems/*` and `providers/*` subpackages.
- Layout C: violates principles 1 and 2.
- Multi-module `go.work` per feature: compiler-enforced but forces versioning/replace directives for a single-binary product.

## Consequences
Positive: a feature is one folder; a mechanical test (ADR-018) enforces the rule; adapters satisfy interfaces without importing consumers; features are testable with hand-written fakes. Negative: kernel types (`scm.Repository`, `scm.RepoStats`, `scm.Membership`) are a shared vocabulary that must be curated; the `overview` aggregator and the bridge package are the price of no feature-to-feature imports; about 70 packages counting generated API packages and ecosystems (the closed-world table has 67 rows, some of them wildcards).

## Evidence
`05-research/04-go-architecture-patterns.md` sections 5-8 (layout comparison, dependency table, deviations Dv-1..Dv-11); `02-design-guardrails.md` GR-03, GR-04, GR-07, GR-11; Go layout guidance https://go.dev/doc/modules/layout (read 2026-10-13).

## Exit condition
Revisit if the kernel grows past about eight types that only one feature reads, or if a second SCM provider appears (then split `scm` and introduce provider ports). **Split point for `scm`** (Go review F-13, partly accepted): when `scm` exceeds about 30 exported types, move `Slug`, `GroupID`, `GroupMeta`, `Membership`, `ViewClass` and `Classed` to a `portal` kernel package at rank 1; the rename is not done now because 04 D-17 names the provider model `scm` and these identity types are read by at least three features. **Exit condition for `overview`**: it stays composition only (no state, no jobs, no store, no upstream, no bridges). If it gains any of those, or about 2,500 non-test lines, or a second reason to change, split it by output into an API-composites package and a text-surface package (`llms`, sitemap, robots, negotiation).

## Amendment 2026-10-14 (review revision)

Review changes: `httpapi` is split into `httpapi` (rank 2: problem details, cursor, ETag, `Static`, `Negotiate`, `Mux`), `httpapi/authz` (rank 3: `Principal`, `Authorizer`, `ClassOf`, `LookupRepo`) and `httpapi/validate` (rank 3: kin-openapi and `ProblemMux`, imported by `cmd/scp` only), so a feature that wants `WriteProblem` no longer links kin-openapi or `httpx` (F-13). Equal rank never imports equal rank, and the "feature root importing its own subpackages" exception is deleted because it was never exercised (F-24). The closed-world table gained rows for `web`, `docs/schema`, `httpapi/apitest`, `manifests/parsertest`, `safehtml`, `reactor`, `cmd/scp/internal/bridge`; `tools/` became a nested module; a `planned` row (`gitsource`) is tolerated (F-04, M6). The acyclicity proof was re-run by script on the new table: 66 packages, 0 cycles, 0 rank violations (architecture Revision log).
