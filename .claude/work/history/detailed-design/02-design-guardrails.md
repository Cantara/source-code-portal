---
id: design-guardrails
kind: guardrails
title: Design guardrails for Source Code Portal (SCP)
status: accepted
date: 2026-10-14
precedence: >-
  Binding order is 04-decisions > 10-architecture > 11-adr > 20-design.
  These guardrails are not a tier of that order; they inform all of them.
  Every decision, architecture section, ADR and unit design must satisfy
  them. A document that departs from a guardrail records the departure, the
  reason and an exit condition in its Deviations section and in
  20-design/93-deviations-and-open-questions.md.
applies-to:
  - 04-decisions.md
  - 10-architecture.md
  - 11-adr/
  - 20-design/
  - 40-implementation-plan.md
  - 41-test-plan.md
refs:
  - 04-decisions.md
  - 10-architecture.md
  - 41-test-plan.md
  - 20-design/90-config-reference.md
  - 20-design/92-error-catalogue.md
ids: GR-01..GR-45
---

# 02 - Design guardrails

Purpose: the cross-cutting engineering rules the whole product is built against. Each guardrail states a principle, the general reason for it, the design units it constrains, and the automated check that proves an implementation honours it. The design phase decides the mechanics; the guardrails decide what any mechanics must achieve.

How to use this file:

- **Principle** is normative ("must", "must not").
- **Rationale** is the engineering reason, independent of any particular implementation.
- **Applies to** names the units in `20-design/` that own the mechanism. Units not listed may still be affected; the listed ones carry the test.
- **Test** names the automated check (unit, property, fuzz, benchmark, archtest, lint, integration, e2e or CI gate). Names are the ones implementers should use, so that `41-test-plan.md`, the unit designs and the code agree. Where a check already exists in a unit design under that name, the unit design is authoritative for its detail.
- `10-architecture.md` section 20 maps each guardrail to the structural mechanism (type, archtest, lint rule, test or CI gate) that makes violating it impossible or visible.

Index:

| ID | Title |
|---|---|
| GR-01 | One composition root and an explicit startup sequence |
| GR-02 | Readiness is gated on data |
| GR-03 | Dependencies are explicit and lifecycles are owned |
| GR-04 | Nothing exists without a consumer; nothing ships half-built |
| GR-05 | Code tells the truth: names, comments, TODOs and decisions |
| GR-06 | Documentation is generated from, or checked against, the code |
| GR-07 | Every entity has its own identity; relations are indexes |
| GR-08 | Behaviour is deterministic and independent of order |
| GR-09 | Repository matchers are typed and anchored |
| GR-10 | Git refs are resolved from the repository |
| GR-11 | Domain, wire and view types are distinct; one encoder |
| GR-12 | One typed, strict, validated configuration |
| GR-13 | Secrets and credentials are a type and never leave headers |
| GR-14 | Defaults are neutral; branding and integrations are configuration |
| GR-15 | One route table; links are generated, escaped and crawled |
| GR-16 | HTTP responses are truthful: status, type and charset |
| GR-17 | The management plane is separate; nothing reflects requests |
| GR-18 | Browser security policy is strict by default |
| GR-19 | One reproducible asset build; only its output is served |
| GR-20 | Webhook ingress: limit, verify, then parse |
| GR-21 | Webhook events are hints that become coalesced refresh jobs |
| GR-22 | One client, breaker and limiter per upstream, bounded by time |
| GR-23 | Upstream failures are typed and rate limits are honoured |
| GR-24 | The job engine never loses work silently |
| GR-25 | Upstream requests are budgeted |
| GR-26 | Freshness is data; reconciliation is authoritative |
| GR-27 | Test seams live in tests |
| GR-28 | The read model is an immutable snapshot with write-time indexes |
| GR-29 | Absence and unsupported input are typed states |
| GR-30 | Retention is bounded and measured |
| GR-31 | Restart is warm |
| GR-32 | Trusted HTML is a type minted only by the sanitiser |
| GR-33 | Badges are rendered locally with structural XML escaping |
| GR-34 | Untrusted documents are parsed safely and within limits |
| GR-35 | Access is an explicit policy; personal data is minimised |
| GR-36 | No embedded interpreters; render once per content hash |
| GR-37 | Document rendering is faithful to the source |
| GR-38 | Times are stored in UTC and shown with an explicit zone |
| GR-39 | The frontend is accessible and purpose-written |
| GR-40 | Live updates respect visibility and show staleness |
| GR-41 | Health derives from outcomes; probes are local and binary |
| GR-42 | Logs are structured and correlated |
| GR-43 | Tests assert, run offline and exercise the shipping code |
| GR-44 | CI is the verifier and gates every change |
| GR-45 | Artefacts are reproducible, immutable and verifiable |

---

## 1. Architecture and lifecycle

### GR-01 One composition root and an explicit startup sequence

- **Principle.** The product must be one binary with one composition root (`cmd/scp`), one router, one configuration and one startup path. There must be no runtime "modes" that select different routes, settings, status codes or serialisers. Startup must be an explicit, ordered sequence written in code (load config, validate, build clients, open and restore state, start sync, mark ready, serve), with every step logged and individually testable. Behaviour must not be switched on or off by annotations, build profiles, package `init()` side effects or naming conventions.
- **Rationale.** Two execution paths double the surface that must be tested and documented, and in practice one of them goes untested. Hidden startup wiring makes it impossible to tell, by reading the code, whether data is loaded at all; an ordered sequence makes the dependency order reviewable and makes failures attributable to one step.
- **Applies to.** `20-design/16-cmd-build-release.md`, `20-design/02-platform.md` (`lifecycle`), `20-design/01-kernel.md` (`routes`).
- **Test.** `TestRunStageOrder` and `TestRunServesAndStops` (integration, `cmd/scp`: boots the real `run()` against the fake GitHub and asserts the stage order from the log and the lifecycle plan); `TestMountedRoutesAgreeWithTheTable` (one router: the mounted mux equals `routes.Table`); lint `gochecknoinits`; config struct test asserting no `mode`-like key exists.

### GR-02 Readiness is gated on data

- **Principle.** `/readyz` must answer 503 until the first complete catalogue sync (and any configured minimum data set) has been published, and again during shutdown. Pages and API responses served before that point must show an explicit "warming up" state rather than an empty portal. After its first success, readiness must not regress because of an upstream outage; upstream health belongs in `/status` and in per-item freshness.
- **Rationale.** Serving traffic before data exists makes an empty response indistinguishable from "this organisation has no repositories". Orchestrators use readiness to route traffic; a signal that flaps on upstream errors takes a still-useful single replica offline.
- **Applies to.** `20-design/15-live-ops-overview.md` (`ops`), `20-design/08-catalog.md`, `20-design/16-cmd-build-release.md`.
- **Test.** `TestReadinessStateMachine` (unit: cold, warm, ready, upstream outage after ready, shutdown); `TestReadinessEmptyCatalogueIsCovered`; `TestSiteBeforeFirstSnapshotIsNotReady`; e2e boot test asserting 503 on `/readyz` and a warming-up page until the catalogue gate opens.

### GR-03 Dependencies are explicit and lifecycles are owned

- **Principle.** Every dependency (clients, stores, loggers, meters, clocks) must be constructed once in the composition root and passed explicitly. There must be no mutable package-level state, no global singletons and no global logger or meter accessors. Every resource (connection, client, goroutine, subprocess, file) must have an owner that creates it in a constructor and releases it in `Close` or when `Run(ctx)` returns.
- **Rationale.** Global state hides coupling, makes tests order-dependent and prevents running two instances in one process. Unowned resources leak under load and on shutdown, and leaks surface far from their cause.
- **Applies to.** `20-design/16-cmd-build-release.md`, `20-design/02-platform.md`, every unit that starts goroutines (`20-design/04-refresh-webhook.md`, `20-design/15-live-ops-overview.md`, `20-design/03-readmodel-store.md`).
- **Test.** Lint `gochecknoglobals`, `gochecknoinits`, `sloglint` (no global logger), `forbidigo` (no OTel global accessors); `TestEveryGoroutinePackageHasGoleak` (archtest: every package that starts goroutines calls `goleak.VerifyTestMain`); `bodyclose` lint; `TestShutdownOrder` (integration: every component is closed exactly once, in reverse order).

### GR-04 Nothing exists without a consumer; nothing ships half-built

- **Principle.** No abstraction, interface, setting, cache layer, worker pool, scheduler or data family may be added without a consumer in the same change. An interface exists only when there is a second implementation or a test seam that needs it; the only source provider is GitHub until a second one is built. Code whose last caller is removed is removed in the same change. Unfinished features must not appear in the UI, the API or health output; they stay behind a flag that defaults to off, and unimplemented kinds are absent rather than stubbed.
- **Rationale.** Decorative infrastructure costs reading time, misleads operators ("this setting does something"), and drifts out of date because nothing exercises it. Placeholders that report success make the product look healthier than it is.
- **Applies to.** All units; enforced centrally in `20-design/16-cmd-build-release.md` (lint and archtest) and `20-design/90-config-reference.md`.
- **Test.** Lint `unused` and `deadcode` in CI; `TestEveryConfigKeyIsConsumed`; `TestEveryCommandIsReachableAndDocumented`; `TestEnrichJobsAbsentWhenNoProvider` (pattern: a disabled feature registers no jobs, routes or health rows); archtest closed-world table (a package without a consumer row fails).

### GR-05 Code tells the truth: names, comments, TODOs and decisions

- **Principle.** Names must state what a value contains (a field holding HTML is named as HTML; a count of groups is not called a count of repositories; a render timestamp is not called an update time). Comments explain why, not history; design narrative lives in the design documents, not in source. A TODO that affects behaviour must reference a tracked issue and be backed by a failing or skipped-with-issue test. Every non-obvious pin, workaround or constraint must be recorded as an ADR with its reason and an exit condition.
- **Rationale.** Misleading names cause defects that review cannot catch because the code reads correctly. Unrecorded decisions become folklore that nobody dares to change.
- **Applies to.** All units; `11-adr/`.
- **Test.** Lint `revive` (naming), `godot`, `godox` configured to reject `TODO`/`FIXME` without an issue reference; `forbidigo` pattern rejecting `t.Skip` without an issue link; CI link check that every ADR reference in code comments resolves to a file in `11-adr/`.

### GR-06 Documentation is generated from, or checked against, the code

- **Principle.** Reference documentation that describes code (configuration keys, routes, API, metrics, error codes, CLI commands) must be generated from the code or verified against it in CI. A document and the code it describes must not be able to drift silently.
- **Rationale.** Hand-maintained reference documentation decays the first time the code changes without it; operators then act on documentation that is wrong, which is worse than no documentation.
- **Applies to.** `20-design/90-config-reference.md`, `20-design/91-metrics-catalogue.md`, `20-design/92-error-catalogue.md`, `20-design/07-openapi-contract.md`, `20-design/16-cmd-build-release.md`.
- **Test.** `TestConfigDocumentedEqualsConsumed`; `TestRouteTableEqualsBundledPaths`; `TestAPIClassTableEqualsOpenAPI`; `TestMetricsReferenceDrift`; `TestEveryReferencedCodeIsDocumented`; `TestEveryCommandIsReachableAndDocumented`; CI drift check for every generated artefact (`go generate` then `git diff --exit-code`).

## 2. Domain model and identity

### GR-07 Every entity has its own identity; relations are indexes

- **Principle.** Repositories (`{source, owner, name}`), groups (`id`), commits (`{repo, sha}`) and other entities must have their own value-typed identities. Relations between them (group membership, repository to groups, dependency edges) must be explicit indexes, not composite keys that double as join rows. Map keys must be comparable value types; no hand-written equality or hash that omits identifying fields or includes non-identifying ones.
- **Rationale.** A key that encodes a relation duplicates data per relation and collapses distinct entities that share part of the key. Equality that ignores a field silently merges records; equality that includes mutable state breaks lookups.
- **Applies to.** `20-design/01-kernel.md` (`scm`), `20-design/03-readmodel-store.md`, `20-design/08-catalog.md`, `20-design/09-commits-badges.md`.
- **Test.** `TestMembershipManyToManyDefaultsAndRefs`; `TestMembershipAgainstNaiveModel` (property: the index equals a naive model over random catalogues); `TestKeyOfMatchesRepoRefKey`; property test that two commits with the same SHA in different repositories are distinct and a repository in two groups is stored once.

### GR-08 Behaviour is deterministic and independent of order

- **Principle.** The same configuration and upstream data must produce the same output regardless of map iteration, listing order, goroutine scheduling or config key order. Where several sources can supply a setting (repository, group, defaults), the precedence rule must be written down and applied explicitly; "first match wins" over an unordered collection is forbidden. Sorting is a view operation and must never define identity or deduplication.
- **Rationale.** Order-dependent behaviour produces results that change between runs and restarts, which looks like flakiness and cannot be reproduced. A comparator used as identity silently drops entries that compare equal.
- **Applies to.** `20-design/08-catalog.md`, `20-design/03-readmodel-store.md`, `20-design/11-manifests-core.md`, `20-design/09-commits-badges.md`.
- **Test.** `TestMembershipIsOrderIndependent`; `TestResolveIndependentOfMembershipOrder`; `TestPlanIsOrderIndependent`; `TestBuildGraphDeterministicUnderShuffle`; `FuzzOrderTotal`, `FuzzOrderAntisymmetric` (comparators are total orders); `TestMergeEqualsNaiveSortPaged`.

### GR-09 Repository matchers are typed and anchored

- **Principle.** Group membership patterns must use explicit matcher kinds (exact, prefix, anchored glob, anchored RE2, topic, catch-all) with documented, anchored semantics. A glob must never be evaluated as a substring regular expression, and "match everything" must be a named option, not a pattern trick. Startup and `scp config lint` must report patterns that match nothing and repositories that land in several groups.
- **Rationale.** Unanchored matching makes `Foo*` match any name that contains `Fo`, so group membership is wrong in ways no one notices until a repository appears in the wrong place. Typed kinds make intent explicit and keep matching linear-time.
- **Applies to.** `20-design/08-catalog.md`.
- **Test.** `TestMatcherTable` (golden table per kind, including anchoring cases); `TestMatcherRejects`; `FuzzMatcherCompile`, `FuzzGlobMatch`; `TestGlobNoBlowup` and `TestMatchIsPolynomial`; `TestConfigLintExitCodes` covering the unmatched-pattern and multi-group reports.

### GR-10 Git refs are resolved from the repository

- **Principle.** The ref used for every fetch must be the repository's own default branch as reported by GitHub, unless explicitly overridden per repository or group; the resolved ref is stored with the item it produced. Push events must be interpreted only for `refs/heads/<tracked branch>`; tags and other refs must not be mistaken for branches, and branch names containing `/` must be handled whole.
- **Rationale.** A configured branch literal goes wrong the moment a repository renames its default branch, and different data families end up describing different branches. Storing the resolved ref makes every item auditable.
- **Applies to.** `20-design/08-catalog.md`, `20-design/05-github-adapter.md`, `20-design/04-refresh-webhook.md`, `20-design/09-commits-badges.md`, `20-design/10-docs-pages-media.md`.
- **Test.** `TestReactCreateOfTrackedBranch`; `TestDotBranchIsNotRequested`; webhook reactor table test asserting that `refs/tags/x`, `refs/heads/feature/x` on a non-tracked branch and a renamed default branch produce the right (or no) jobs; `TestBuildersRoundTrip` for refs with `/`.

### GR-11 Domain, wire and view types are distinct; one encoder

- **Principle.** Domain types must be plain data with no presentation or URL logic. Wire types (API DTOs) must be generated from the OpenAPI contract and must not be imported across feature boundaries. Every response envelope must carry its required fields (freshness, snapshot version) as non-optional, so a handler cannot omit them. The frontend renders typed API data with explicit empty states and formats values only through shared formatters. JSON must go through one encoder with typed response structs; no second JSON library and no untyped maps crossing package boundaries.
- **Rationale.** When one type serves as domain object, cache value and view model, every change ripples through all three, and presentation concerns leak into storage. Required-field omissions should be compile errors, not rendered placeholders. Two serialisers disagree on edge cases (nulls, numbers, field names).
- **Applies to.** `20-design/07-openapi-contract.md`, `20-design/06-httpapi.md`, `20-design/17-web-foundation.md`, `20-design/18-web-pages.md`, every feature unit's `<feature>api` package.
- **Test.** Archtest rule G7 (no cross-package import of `<feature>api`); `TestBaseModelOnEveryPage` (every page and envelope carries freshness and version); `TestProblemBodyMatchesSchema`; lint `depguard` forbidding any JSON library other than `encoding/json`; frontend type check (`tsc --noEmit`) against the generated client.

## 3. Configuration and secrets

### GR-12 One typed, strict, validated configuration

- **Principle.** There must be one versioned, typed configuration struct, loaded with a fixed precedence (embedded defaults < file given by `--config` < `SCP_` environment < flags) and decoded strictly: unknown keys are errors, key order never matters, and `Validate()` collects every problem with a path-accurate message. Every key must have a type, a default and documentation, and must be read by code. Durations are `time.Duration` parsed from strings such as `30s` or `5m`. Environment variable names are `SCP_` plus the upper-snake key path, never dotted. No business code performs string-keyed configuration lookups or reads the environment directly. Every example configuration shipped with the product must load and lint cleanly.
- **Rationale.** Silent defaults for misspelt keys turn a typo into a production incident; a setting that nothing reads lies to the operator. Mixed duration units cause off-by-60 errors. Dotted environment names cannot be set from POSIX shells. Strict, typed decoding moves all of these to startup, where they are cheap.
- **Applies to.** `20-design/02-platform.md` (`config`), `20-design/16-cmd-build-release.md`, `20-design/90-config-reference.md`, every unit's `Config` type.
- **Test.** `TestStrictDecodeRejects` and `TestConfigRejects` (unknown key, wrong type, bad duration); `TestConfigValidateCollectsAll`; `TestConfigDocumentedEqualsConsumed` and `TestEveryConfigKeyIsConsumed`; `TestPrecedence`; `FuzzConfigDecode`, `FuzzParseConfigDuration`, `FuzzEnv`; `TestConfigLintStrictEnv` (dotted names rejected); `TestExamplesLint` (every shipped example); lint `forbidigo` on `os.Getenv` outside `cmd/scp`.

### GR-13 Secrets and credentials are a type and never leave headers

- **Principle.** Every secret must be a distinct `Secret` type whose string, debug, JSON, text and log forms are redacted. Empty means unset: a required secret that is empty is a startup error, and anonymous operation must be an explicit setting. Secrets may be supplied from `*_FILE` paths. Credentials travel only in request headers, never in URLs, query strings, command-line arguments or log lines; transport logs carry method, host and route pattern only. The service authenticates to GitHub with GitHub App installation tokens or a fine-grained token; interactive credential acquisition, if ever needed, uses the OAuth device flow, never browser automation.
- **Rationale.** Secrets typed as strings end up in logs, error messages and URLs that proxies record. Treating an empty string as "configured" sends empty credentials and produces confusing failures far from the cause. Browser automation of a login requires storing a password and breaks on every UI change.
- **Applies to.** `20-design/02-platform.md` (`secret`, `logx`, `config`), `20-design/05-github-adapter.md`, `20-design/04-refresh-webhook.md`, `20-design/14-integrations-access.md`.
- **Test.** `TestEmptySecretIsUnset`; `FuzzSecretRedaction`; `TestLogsNeverContainSecrets` and `TestSecretsNeverLeak` (integration: boot with sentinel secrets, scan all log output and error strings); `TestNoCredentialsEmitted` and `TestErrorsCarryNoCredential` (adapter: no request URL carries a credential); `FuzzRedactURL`; `TestSecretFileResolveAndFlagRefusal`; CI dependency check that no browser-automation module is in the module graph.

### GR-14 Defaults are neutral; branding and integrations are configuration

- **Principle.** No default may point at a specific organisation, company host, wiki, CI server or page id. The GitHub API base URL must be configurable (GitHub Enterprise Server). Third-party integrations are off until configured with an explicit base URL. Site title, description, navigation, announcement banner and display zone come from configuration; the embedded default is neutral. Numeric presentation limits (latest-N counts, poll intervals) are configuration, not literals.
- **Rationale.** Organisation-specific defaults make the product wrong for every other deployment and can send credentials or traffic to a third party's host by accident. Literals duplicated across code drift apart.
- **Applies to.** `20-design/90-config-reference.md`, `20-design/14-integrations-access.md`, `20-design/15-live-ops-overview.md` (`overview`, `api.site`), `20-design/05-github-adapter.md`, `20-design/18-web-pages.md`.
- **Test.** `TestDefaultConfigHasNoForeignHosts` and `TestDefaultsHaveNoForeignHosts`; e2e "default build contains no organisation strings" check over the built SPA bundle and API responses; `TestDashboardCommitCountIsConfig`.

## 4. HTTP, routing and URLs

### GR-15 One route table; links are generated, escaped and crawled

- **Principle.** Every route must be declared once in a named route table (`routes.Table`) with method and pattern, served by the standard `net/http` mux, which owns 404 and 405. Every URL the server emits (API `url` fields, sitemap, `llms.txt`, rewritten links in rendered documents) is built by the route builders; every URL the SPA emits comes from typed routes. Ad-hoc string concatenation of paths is forbidden. Each path segment is escaped; repository slugs are single validated segments and refs travel as query parameters, so names and branches with `/` or other reserved characters round-trip.
- **Rationale.** Links built by concatenation drift from the routes they target, and nothing notices until users click them. A hand-written path parser re-implements, badly, what the standard router already does correctly.
- **Applies to.** `20-design/01-kernel.md` (`routes`), `20-design/01-kernel.routes.tsv`, `20-design/06-httpapi.md`, `20-design/17-web-foundation.md`, `20-design/10-docs-pages-media.md`.
- **Test.** `TestBuildersRoundTrip` and `FuzzEscapeRepoPath`; `TestMountedRoutesAgreeWithTheTable`; `TestRecorderRejectsUnknownPattern`; `TestLinksResolveToRoutes` and `TestCrawlFollowsBadgeURLs` (e2e crawl following every link in the SPA, rendered docs HTML, sitemap, `llms.txt` and API `url` fields, failing on any 404); lint `forbidigo` rejecting path-literal concatenation outside `routes`.

### GR-16 HTTP responses are truthful: status, type and charset

- **Principle.** Status codes must mean what HTTP says: 2xx success, 4xx caller error, 5xx server error, 503 with `Retry-After` for saturation or not-ready. Unknown resources return a real 404 (problem details for the API, the SPA not-found view with status 404 for pages), never a redirect to the home page. Errors are RFC 9457 problem documents with a stable `code` and the request id. Every response declares its content type explicitly and every text type carries `charset=utf-8`; all text is encoded and decoded as UTF-8, never the platform default.
- **Rationale.** Soft 404s and always-200 endpoints break caches, crawlers, monitoring and clients that rely on status codes. Implicit charsets and sniffed types produce mojibake and open content-type confusion.
- **Applies to.** `20-design/06-httpapi.md`, `20-design/92-error-catalogue.md`, `20-design/07-openapi-contract.md`, `20-design/18-web-pages.md`.
- **Test.** `TestProblemMuxAloneRewritesServeMuxDefaults`; `TestHandlerUnknownAndHidden` and `TestVisibilityUniform404`; `TestGeneratedHandlerProblems`; Schemathesis contract run against the OpenAPI spec (status codes and content types); `TestCharsetXML` and `TestEncodingMatrix`; e2e assertion that an unknown group page returns 404 and no 3xx.

### GR-17 The management plane is separate; nothing reflects requests

- **Principle.** Metrics, pprof, detailed `/status` and operator actions must be served only on a separate admin listener (or behind authentication), never on the public port. There must be no endpoint that echoes request headers, cookies or bodies. Debugging aids live on the admin listener or in tests.
- **Rationale.** Management endpoints disclose internals and can be used to exhaust the process; echo endpoints reflect credentials to whoever can make a browser send a request. Separating listeners makes the public surface reviewable.
- **Applies to.** `20-design/15-live-ops-overview.md` (`ops`), `20-design/06-httpapi.md`, `20-design/16-cmd-build-release.md`.
- **Test.** `TestPublicHasNoManagementRoutes` (public mux has no `/debug`, `/metrics` or admin route); `TestAdminBearer`; `TestAdminHostGuardRefusesRebindingHosts`; `TestAccessNotOnAdmin`; archtest: `net/http/pprof` imported only by `ops`.

### GR-18 Browser security policy is strict by default

- **Principle.** A middleware must set a strict Content Security Policy (with Trusted Types), `X-Content-Type-Options: nosniff`, a referrer policy, `frame-ancestors`, and HSTS when served over TLS, on every response class. All scripts, styles, fonts and icons are self-hosted with hashed names; no third-party origin is loaded at runtime and no inline handlers or inline styles are emitted. No CORS headers are sent by default; cross-origin access is enabled only by an explicit Origin allow-list for a named consumer, and an Origin check is never implemented as a Host check.
- **Rationale.** Defence in depth: if sanitising ever fails, a strict CSP stops script execution. Unpinned third-party assets are a supply-chain risk and a privacy leak. Wildcard CORS exposes authenticated data to any site.
- **Applies to.** `20-design/02-platform.md` (`httpx`), `20-design/06-httpapi.md`, `20-design/17-web-foundation.md`.
- **Test.** `TestHeaderGolden` and `TestPublicChainGoldenPerClass` (golden header set per response class); `TestCSPModes`; `TestValidateResponseRejectsCORSHeaders` and an e2e assertion that no `Access-Control-*` header is present by default; Playwright check that the page loads with zero requests to non-self origins and zero CSP violations.

### GR-19 One reproducible asset build; only its output is served

- **Principle.** Frontend assets must come from one reproducible build (`pnpm build`) with content-hashed file names served with immutable caching; build output is not committed. Only that output is embedded and served, from an explicit allow-list, walked and validated once at startup; templates, source files and source maps are never served in production. Every served asset's bytes must match its declared content type, and the product ships its own favicon.
- **Rationale.** Several asset pipelines disagree about which files are current; committed build output goes stale; unhashed names serve stale code after deploys. Serving the source tree exposes files never meant to be public. Loading resources per request turns build errors into runtime errors.
- **Applies to.** `20-design/17-web-foundation.md`, `20-design/06-httpapi.md` (`webapp`), `20-design/16-cmd-build-release.md`.
- **Test.** `TestHashedAssetNamesFromViteAreAccepted`; `TestAssetsServedUnderReservedAssetsPrefix`; unit test asserting no `.map` file and no source file is reachable; asset sniff test (`http.DetectContentType` agrees with the extension for every embedded file); CI job that builds from a clean checkout and fails if `web/dist` differs from a second build or is tracked in git.

## 5. Webhook ingress

### GR-20 Webhook ingress: limit, verify, then parse

- **Principle.** The webhook receiver must, in this order: cap the body size, verify `X-Hub-Signature-256` with HMAC-SHA256 over the raw bytes using a constant-time comparison (`hmac.Equal`), and only then decode the payload. Malformed or missing signature headers are rejected with 401/403 without parsing. The route is not registered and the process refuses to start without a non-empty webhook secret. Deliveries are deduplicated by `X-GitHub-Delivery` for a bounded window. Unauthenticated bodies are never logged.
- **Rationale.** Parsing before authenticating exposes the parser to anyone on the internet; non-constant-time comparison leaks the signature byte by byte; hashing a re-encoded string instead of the raw bytes makes verification depend on charset handling. An endpoint without a secret accepts forged events.
- **Applies to.** `20-design/04-refresh-webhook.md` (`webhook`), `20-design/05-github-adapter.md` (verifier).
- **Test.** `TestSignVector` (GitHub's published test vectors); `FuzzVerify` and `FuzzRawHeader`; `TestInvalidSignatureMatrix`; `TestBodyIsNeverLoggedAndUnauthenticatedHeadersAreSanitised`; `TestMissingEventHeaderIsMalformedAfterAuth`; `TestDeliveryDedupe`; startup test asserting refusal without a secret; `TestInvalidSignatureBudgetGives429ButValidIsNeverLimited`.

### GR-21 Webhook events are hints that become coalesced refresh jobs

- **Principle.** A verified event must be resolved against the catalogue by repository full name; unknown repositories are acknowledged and counted as ignored. Event handlers do no I/O: they enqueue coalesced "refresh repository X / family Y" jobs, using the union of path lists across all commits in a push (added, modified, removed, renamed). Every event kind is either handled or acknowledged and counted as ignored by design. Outcomes are explicit: verified and enqueued gives 202, malformed gives 400, bad signature gives 401/403, a full or closed queue gives 503 with `Retry-After`; each outcome is counted. Because events are hints, a lost event is repaired by scheduled reconciliation within the item's TTL.
- **Rationale.** Fetching inside the request couples GitHub's delivery deadline to upstream latency. Using only part of a push misses changes. Mapping internal failures to 400 tells the sender not to retry, so events are lost; 503 lets the sender redeliver.
- **Applies to.** `20-design/04-refresh-webhook.md`, `20-design/01-kernel.md` (`reactor`), `20-design/09-commits-badges.md`, `20-design/10-docs-pages-media.md`, `20-design/11-manifests-core.md`.
- **Test.** `TestEveryEventKindIsHandledOrCountedIgnored`; `TestUnknownRepositoryIsCountedIgnored`; `TestQueueFullAndUnknownKind` (503 plus `Retry-After`); `TestFailedEnqueueIsNotRecordedSoRedeliveryIsAccepted`; `TestTouchesDocs` (path-union table); `TestWebhookLossHealedByReconcile` (integration); `TestWebhookToJobAndEndToEndMetric`.

## 6. Fetching and resilience

### GR-22 One client, breaker and limiter per upstream, bounded by time

- **Principle.** Each upstream (GitHub REST, GitHub GraphQL, each integration instance) must have exactly one HTTP client, one circuit breaker and one concurrency limit, created at startup and shared by every caller. Every client has dial, TLS handshake, response-header and overall timeouts, an explicit redirect policy, and ignores ambient proxy environment unless configured. Every call takes a `context.Context`; cancellation is propagated, never swallowed. Workers are goroutines bound to a context, with per-upstream concurrency, so a slow upstream cannot starve the others; shutdown is deadline-driven.
- **Rationale.** A breaker created per call never accumulates state and can never open. A client without timeouts lets one black-holed host hold every worker. A single shared pool converts one slow dependency into a full outage.
- **Applies to.** `20-design/02-platform.md` (`netguard`, `httpx`), `20-design/05-github-adapter.md`, `20-design/14-integrations-access.md`, `20-design/04-refresh-webhook.md` (`refresh`).
- **Test.** `TestBreakerRatioTrip` and `TestBreakerOpensOnTransientOnly`; `TestClientIgnoresProxyEnvironmentAndDoesNotFollowRedirectsByDefault`; `TestRequestTimeout` and `TestSlowBodyReleasesItsSlotAtBodyTimeout`; `TestFullConcurrencyLimit` (a stalled upstream does not delay another upstream's jobs); `TestJobTimeoutOverrunAndStuckAllSlots`; lint `noctx`; `forbidigo` on `http.DefaultClient` and on constructing breakers outside adapters.

### GR-23 Upstream failures are typed and rate limits are honoured

- **Principle.** Upstream errors must be classified into typed kinds (not found, gone, rate limited with reset time, transient, permanent, canceled) from status codes and headers. Only transient errors are retried; rate limits pause the upstream until `Retry-After` or `X-RateLimit-Reset` without consuming a retry attempt; 404 is a real state with its own retention; redirects are followed only within policy. No failure is ever converted into a synthetic status code.
- **Rationale.** Treating rate limits as "not found" deletes data and hammers the upstream; collapsing every failure into one code makes retries either useless or harmful. Typed errors let the engine choose the correct action.
- **Applies to.** `20-design/05-github-adapter.md`, `20-design/01-kernel.md` (`job` behaviour interfaces), `20-design/04-refresh-webhook.md`, `20-design/14-integrations-access.md`, `20-design/92-error-catalogue.md`.
- **Test.** `TestClassify` and `FuzzClassify` (classification table: 301, 304, 401, 403 with and without rate-limit headers, 404, 410, 422, 429, 5xx, network errors, cancellation); `TestProviderErrorClassification`; `TestRateLimitPausesUpstreamWithoutAttemptAndFloorGrows`; `TestSecondaryLimitGrowth`; fake GitHub fault-injection test for every row of the classification table.

### GR-24 The job engine never loses work silently

- **Principle.** Jobs must be keyed and coalesced (a key queued twice runs once; a key enqueued while running runs exactly one follow-up). Retries use jittered exponential backoff; exhausted, permanent and panicking jobs are parked with a reason, counted and visible in `/status`. Enqueue never panics or throws when full; it returns a typed error that callers map to backpressure. Periodic schedules recover from panics, skip a tick if the previous run is still going, add jitter, and record last attempt, last success and last error on completion (not when queued).
- **Rationale.** Immediate retries amplify outages; silent drops lose data with no trace; a periodic job that dies on its first exception stops forever without telling anyone. Recording "last run" at queue time reports work that never happened.
- **Applies to.** `20-design/04-refresh-webhook.md` (`refresh`), `20-design/01-kernel.md` (`job`), `20-design/15-live-ops-overview.md`.
- **Test.** `TestCoalesceQueuedAndDirtyWhileRunning`; `TestBackoffBounds` and `TestIntervalBackoffAndJitter`; `TestPanicIsContainedAndParked`; `TestPermanentParksForceUnparksProbeAfter24h`; `TestEnqueueAfterRunReturnedIsErrClosed`; `TestSchedulerJitterPanicTriggerAndFrozen`; `TestPlannerTickDuringRunningJobDoesNotRunTwice`; `FuzzQueueModel` (model-based: no accepted job disappears without a terminal state).

### GR-25 Upstream requests are budgeted

- **Principle.** Request volume to GitHub and to integrations must fit a documented budget at the target scale. Conditional requests (`ETag`, `If-None-Match`) are used wherever the upstream supports them, change detection is batched (for example one GraphQL head sweep) before per-repository fetches, startup work is not duplicated, and no third-party service is called for data the product already holds or can derive (badges are derived from fetched data).
- **Rationale.** Unbudgeted polling exhausts rate limits at modest scale, after which every feature fails at once. Duplicate startup requests and per-item third-party calls multiply cost with no benefit.
- **Applies to.** `20-design/04-refresh-webhook.md`, `20-design/05-github-adapter.md`, `20-design/09-commits-badges.md`, `20-design/14-integrations-access.md`, `20-design/11-manifests-core.md`.
- **Test.** `TestBudgetColdStart`, `TestBudgetColdStartREST`, `TestBudgetColdStartGraphQL` and `TestBudgetEndToEnd` (fake GitHub counts calls per scenario against the budget formula); `TestBudgetFormulaMatchesPlan`; `TestConditionalSweep`; `TestColdStartThreeThousandItems`; badge test asserting no outbound request is made to render a badge.

### GR-26 Freshness is data; reconciliation is authoritative

- **Principle.** Every fetched item must carry `fetchedAt`, its validator (`ETag` or content SHA), a typed status and its last error, and its staleness is derived at read time. Scheduled reconciliation, paginated and retried, adds new repositories and removes vanished, renamed or archived ones, with a guard against mass removal on a suspicious listing. A failed refresh keeps the last good value and marks it stale (stale-while-error), visibly. A failed catalogue listing keeps the last good catalogue and affects readiness only on cold start.
- **Rationale.** Data without freshness cannot be trusted or debugged; a catalogue loaded once never learns about new repositories and never forgets deleted ones. Failing open to an empty portal turns a transient upstream error into a total outage.
- **Applies to.** `20-design/01-kernel.md` (`freshness`), `20-design/03-readmodel-store.md`, `20-design/08-catalog.md`, `20-design/04-refresh-webhook.md`, `20-design/17-web-foundation.md` (`FreshnessBadge`).
- **Test.** `TestStaleWhileError` and `TestRunFailureIsStaleWhileError`; `TestServiceListFailureChangesNothing`; `TestPlanEverythingVanishingIsHeld` (mass-removal guard); `TestServiceReconcileLifecycle`; `FuzzNextLink` and `TestNextLinkNeverFollowed` (pagination); `TestItemStatusCheckMatchesFreshnessStates`; `TestETagCoversDerivedFreshness`.

### GR-27 Test seams live in tests

- **Principle.** Recording, replay, fake upstreams, response dumping and other development aids must live in test packages or in separate development binaries, never in the production code path. Behaviour must never depend on the version string or on a "development" flag inferred from it.
- **Rationale.** Development switches in production code eventually get turned on in production, write untrusted data to disk or relax a security check. Version-dependent behaviour makes a release candidate behave differently from the release.
- **Applies to.** `20-design/05-github-adapter.md` (`githubtest`, `cmd/fakegithub`), `20-design/16-cmd-build-release.md`.
- **Test.** `TestNoTestSeamsInReleaseBuild` (archtest: no production package imports `githubtest`, `storetest`, `apitest` or `parsertest`); `TestDevBuildReachesTheFakeOnly`; `TestLoopbackRefusedInProductionBuild`; lint rule rejecting comparisons against the build version string.

## 7. Read model and storage

### GR-28 The read model is an immutable snapshot with write-time indexes

- **Principle.** Readers must see an immutable snapshot published atomically (`atomic.Pointer`) and shared by pointer; writers build the next snapshot copy-on-write. Cross-entity views (latest N commits overall and per group, repository to groups, group to repositories, dependency reverse index) are maintained by projectors at publish time. Request handlers never scan whole collections, never sort the full data set per request and never read the durable store. There is one read model and one store behind one narrow interface; no second cache stack.
- **Rationale.** Serialising or deep-copying on every access, and scanning everything per request, makes the hot path cost proportional to the data set. An immutable snapshot gives readers consistency without locks and makes publication a single, testable step.
- **Applies to.** `20-design/03-readmodel-store.md`, `20-design/09-commits-badges.md`, `20-design/08-catalog.md`, `20-design/11-manifests-core.md`.
- **Test.** `TestImmutabilitySharingAndRev`; `TestConcurrentReadersDuringWrites` (race detector); `BenchmarkCommitsPage` on a 1,000-repository snapshot with time and allocation budgets as a CI gate; `BenchmarkGetParallel`; `BenchmarkBuildMembership`; CI dependency check that no cache library is in `go.mod`.

### GR-29 Absence and unsupported input are typed states

- **Principle.** "No README", "not found", "no content", "unsupported format", "too large" and "not resident" must be typed states with their own rendering, never `null`, an empty string or a blank page. Every state renders an explicit message and, where useful, a canonical link to the source on GitHub.
- **Rationale.** Caching `null` makes "we have not fetched it", "it does not exist" and "we cannot render it" indistinguishable, which produces blank pages that cannot be diagnosed.
- **Applies to.** `20-design/01-kernel.md` (`freshness.State`), `20-design/10-docs-pages-media.md`, `20-design/03-readmodel-store.md`, `20-design/18-web-pages.md`.
- **Test.** `TestNoReadmeStateRenders`; `TestStateSetMatchesTheSQLCheckConstraint`; `TestResultStatesSurviveRoundTripThroughTransitions`; golden test per README variant (`README`, `README.txt`, `README.rst`, oversized) asserting a non-null body and the canonical link; frontend component test per empty state.

### GR-30 Retention is bounded and measured

- **Principle.** Every growing collection (commits per repository, documents per repository and globally, caches, dependency graphs, delivery dedupe windows) must have a configured bound, a deterministic admission or eviction rule, and counters for what was evicted or not admitted. Memory sizing is documented against `GOMEMLIMIT`.
- **Rationale.** Unbounded growth is a slow memory leak that ends in an out-of-memory restart, typically long after the change that caused it. Counters make the bound visible before it bites.
- **Applies to.** `20-design/09-commits-badges.md`, `20-design/10-docs-pages-media.md`, `20-design/03-readmodel-store.md`, `20-design/11-manifests-core.md`, `20-design/04-refresh-webhook.md`.
- **Test.** `TestResidentHeap`; `TestAdmitFailsClosed`; `TestEdgeCapTruncates`; `TestHintMapBoundedAndExpires`; `TestDedupeEvictionAndNoRefreshOnHit`; `TestInputsAtScaleStayLinear`; metric registry test asserting an eviction or rejection counter exists for every bounded collection.

### GR-31 Restart is warm

- **Principle.** The last good state must be persisted in the embedded store and restored at startup, so a restart serves stale-marked data at once and revalidates it with conditional requests instead of refetching everything. A corrupt or foreign database is quarantined and rebuilt, never trusted blindly; restored HTML is re-sanitised.
- **Rationale.** A full refetch after every restart multiplies upstream load, can exhaust rate limits during deploys, and leaves the portal empty for minutes.
- **Applies to.** `20-design/03-readmodel-store.md`, `20-design/04-refresh-webhook.md`, every feature unit's restore path.
- **Test.** `TestWarmStartEndToEnd` (integration restart test: second boot is ready before any upstream call completes and issues only conditional requests); `TestWarmStartProtocol`; `TestCodecRoundTripResanitises`; `TestForeignDatabaseIsNeverTouched`; `TestRestartWithACorruptRingRefetchesAtOnce`; `BenchmarkWarmLoad34k`.

## 8. Security

### GR-32 Trusted HTML is a type minted only by the sanitiser

- **Principle.** Rendered documentation (README, docs pages, any HTML from a repository) is untrusted. It must be sanitised with a strict allow-list before it enters the read model, and the only way to obtain a value of the trusted-HTML type (`safehtml.SafeHTML`) is through the sanitiser. API fields carrying HTML are filled only from that type. HTML loaded from the store is re-sanitised. A strict CSP (GR-18) is defence in depth, not the primary control.
- **Rationale.** Any repository in the organisation can contain hostile Markdown; rendering it unsanitised on the portal's origin is stored cross-site scripting. Making trusted HTML a type turns "did someone remember to sanitise?" into a compile error.
- **Applies to.** `20-design/01-kernel.md` (`safehtml`), `20-design/10-docs-pages-media.md`, `20-design/03-readmodel-store.md`, `20-design/18-web-pages.md`.
- **Test.** `FuzzSanitize` and `FuzzSanitizeFixpoint` (idempotent, no script or event handler survives); `TestPolicyGolden`; adversarial XSS corpus test (`javascript:` URLs, raw HTML, SVG, `<base>`, CSS expressions); `TestMarkdownIsInert`; archtest: bluemonday imported only by `safehtml`; `TestCodecKeepsReasonAuxImagesAndRejectsTampering`.

### GR-33 Badges are rendered locally with structural XML escaping

- **Principle.** Badges must be rendered by the portal from structured data it already holds; third-party SVG must never be proxied from the portal's origin. SVG is produced by `encoding/xml` marshalling of typed structs, so escaping is structural, never by string replacement into a template. If upstream bytes ever have to be served, they are served with `Content-Security-Policy: sandbox`, `nosniff` and a content-type allow-list. Badge responses carry `Cache-Control` and an `ETag`, and an unknown or invisible repository or kind returns 404 (with a small grey SVG body so broken embeds stay visible), never a 200 placeholder; a known repository without data yet is a short-lived 200 "unknown" badge.
- **Rationale.** Third-party SVG served same-origin is script on the portal's origin. String interpolation into XML lets upstream-controlled text break out of attributes. Rendering locally also removes a per-request third-party dependency.
- **Applies to.** `20-design/09-commits-badges.md`, `20-design/10-docs-pages-media.md` (`media`).
- **Test.** `FuzzRenderWellFormed` (arbitrary label and message text always yields well-formed XML); `TestGoldenSVG` (golden files parsed with `encoding/xml`); `TestMarshalOnlyNeverUnmarshal`; `TestUntrustedTextCannotBreakOut`; `TestExternalSVGSandboxed`; `TestBadgeCacheHeaders`.

### GR-34 Untrusted documents are parsed safely and within limits

- **Principle.** Every document from a repository or upstream (manifests, lockfiles, descriptors, XML, YAML, JSON) must be parsed with a parser that performs no entity resolution and no external fetches, after byte-size and nesting-depth limits have been applied, with a per-file time budget and cancellation at token boundaries. JSON APIs are preferred over XML; where XML is unavoidable, `encoding/xml` is used. YAML anchors and aliases are rejected where they are not needed. Parse failures report a bounded excerpt, never the whole document.
- **Rationale.** Default XML parsers resolve external entities and expand entity bombs; unbounded inputs turn a single crafted file into a denial of service. Logging whole documents leaks content and floods logs.
- **Applies to.** `20-design/11-manifests-core.md`, `20-design/12-manifests-ecosystems-a.md`, `20-design/13-manifests-ecosystems-b.md`, `20-design/10-docs-pages-media.md` (descriptor loader).
- **Test.** `TestBombs`, `TestParserBombs`, `TestPropertyBomb` and `TestLexicalBombs` (XXE, billion laughs, deep nesting, oversized input fixtures); `FuzzParsers` with the shared invariants (no panic, bounded time and memory, determinism, valid UTF-8 output, diagnostics instead of silent drops); `TestNuGetEntityIsError`; `TestYAMLStrictDecodeAndByteSize`; `TestExcerptNeverSplitsARuneAndRedactsJWT`; CI dependency check that no third-party XML library is in `go.mod`.

### GR-35 Access is an explicit policy; personal data is minimised

- **Principle.** The default publishing policy is public repositories only. Serving private or internal content requires an explicit access configuration (OIDC or a trusted proxy), a credential with the proper scope, and validation at startup that the combination is coherent; anonymous responses are byte-identical to a public-only deployment. Only the personal data the UI shows is stored: commit author e-mail addresses are never persisted.
- **Rationale.** A portal that can publish private repositories without authentication is a data leak waiting for one configuration change. Data that is never stored cannot leak and needs no retention policy.
- **Applies to.** `20-design/14-integrations-access.md`, `20-design/06-httpapi.md` (`authz`), `20-design/08-catalog.md`, `20-design/09-commits-badges.md`, `20-design/03-readmodel-store.md`.
- **Test.** `TestAnonymousResponsesAreByteIdenticalToAPublicOnlyDeployment`; `TestVisibilityZeroValueIsNotPublic`; `TestAuthorizerMonotone`; `TestPrivateGuard`; startup validation test refusing private visibility without an access policy; `TestRowRecordHasNoEmailAndOnlyDocumentedKeys` and a schema test asserting no e-mail column exists.

## 9. Rendering and frontend

### GR-36 No embedded interpreters; render once per content hash

- **Principle.** Rendering must not embed a language runtime or interpreter in the process. Formats the Go pipeline does not render natively are rendered by GitHub's API, or by a bounded subprocess pool with a timeout and a hardened environment. Every rendered result is cached by content hash plus pipeline version and rendered once.
- **Rationale.** An embedded runtime per document costs seconds of start-up and hundreds of megabytes, leaks if not closed, and widens the attack surface. Rendering per request or per refresh repeats identical work.
- **Applies to.** `20-design/10-docs-pages-media.md`, `20-design/05-github-adapter.md`.
- **Test.** Lint `forbidigo` on `os/exec` outside the allowed packages; `TestCommandRunnerKillsTheWholeProcessGroupOnTimeout` and `TestCommandRunnerHidesThePortalEnvironment` (where a subprocess is allowed); render cache test asserting a second publish of an unchanged blob performs zero renders; `BenchmarkSanitizeReadme`.

### GR-37 Document rendering is faithful to the source

- **Principle.** Format detection must be case-insensitive and cover common README names and extensions; unknown formats render as escaped text with a "view on GitHub" link. Relative links and images are rewritten against the repository and resolved ref (portal routes or GitHub URLs for links, raw URLs for public images), and opaque or dangerous schemes are dropped. Syntax highlighting happens on the server with theme-aware CSS variables, never from a CDN script.
- **Rationale.** Broken relative links make documentation unusable; case-sensitive checks silently drop valid READMEs; client-side highlighting from an external origin conflicts with a strict CSP and breaks dark mode.
- **Applies to.** `20-design/10-docs-pages-media.md`, `20-design/17-web-foundation.md`.
- **Test.** `FuzzDetectType`; `FuzzRewriteLinks` and the link-rewrite table test; `TestOpaqueAbsoluteLinksAreDropped`; `TestRewriteImagesNeverFetchesPrivate`; `TestTextIsEscapedAndAFixpoint`; `TestChromaBudget`; golden render tests per format with light and dark CSS.

### GR-38 Times are stored in UTC and shown with an explicit zone

- **Principle.** All timestamps are stored and transmitted in UTC (RFC 3339 with `Z`). The UI renders them in a configured display zone with the zone shown, inside `<time datetime>`, through one shared formatter. Grouping by day uses the display zone explicitly, never the server's local zone.
- **Rationale.** Times formatted in the server's zone without a label are ambiguous and change when the host moves; day grouping silently shifts entries between days.
- **Applies to.** `20-design/07-openapi-contract.md`, `20-design/09-commits-badges.md`, `20-design/17-web-foundation.md` (`lib/format.ts`), `20-design/18-web-pages.md`.
- **Test.** `TestTimestampsAreUTC` (API contract); `TestZeroTimes`; `TestFormatCountAndRelativeDay` (frontend unit test across zones and DST boundaries); lint rule (web) forbidding `Date` formatting outside `lib/format.ts`.

### GR-39 The frontend is accessible and purpose-written

- **Principle.** Pages use semantic HTML with landmarks, one `h1` per page, labelled controls, text alternatives for images and icon-only controls, a skip link, a route announcer and `aria-live` for live regions; the palette meets WCAG 2.2 AA in light and dark themes. Icons are tree-shaken inline SVG components, not icon fonts. Styles are purpose-written and purged; no CSS ships that no component can match; markup is valid.
- **Rationale.** Accessibility defects exclude users and are cheap to prevent but expensive to retrofit. Icon fonts fail silently and leave invisible click targets; dead CSS grows the bundle and hides real styling bugs.
- **Applies to.** `20-design/17-web-foundation.md`, `20-design/18-web-pages.md`.
- **Test.** Playwright axe scan on every route in both themes (zero violations, CI gate); `TestPaletteContrast`; HTML validity check on rendered routes; bundle-size budget in CI; component test asserting every icon-only button has an accessible name.

### GR-40 Live updates respect visibility and show staleness

- **Principle.** Live refresh in the browser must pause when the tab is hidden, poll cheaply (snapshot version with `ETag`, 304 when unchanged) or use server-sent events, and fetch only the data that changed. Every view that shows fetched data shows a visible "data as of" freshness marker, and a failed refresh is shown, not ignored.
- **Rationale.** Polling from hidden tabs wastes server and upstream budget; re-fetching whole documents on every tick multiplies load; silently ignored errors leave users looking at stale data they believe is current.
- **Applies to.** `20-design/15-live-ops-overview.md` (`live`), `20-design/17-web-foundation.md`, `20-design/18-web-pages.md`.
- **Test.** Frontend unit test asserting `refetchIntervalInBackground: false` and pause on `visibilitychange`; `TestConditionalGetHeadAndSSE` and `TestSnapshotETagIncludesPhase`; `TestStreamHelloInvalidateHeartbeatDrain`; component test that `FreshnessBadge` renders stale and error states.

## 10. Observability

### GR-41 Health derives from outcomes; probes are local and binary

- **Principle.** Liveness (`/livez`) and readiness (`/readyz`) are separate endpoints with distinct gates and answer only 200 or 503. Both read in-memory state and never make outbound calls. Health is derived from real outcomes: dispatcher heartbeat, last successful sync, data age per kind, queue depth, parked jobs, rate-limit state; never from incidental facts such as "a thread exists" or "the worker was queued". Unconfigured integrations report `disabled`, never "ok". Nuance (degraded upstreams, warnings) belongs in `/status` and metrics, whose vocabulary is defined and documented. Metrics describe goroutines, queues, in-flight jobs, upstream latency and errors; pprof is available on the admin listener for deep inspection.
- **Rationale.** A health endpoint that cannot fail is worse than none, because it hides outages. Probes that call upstreams turn the health check into load and fail when the upstream does, restarting a healthy process. Custom status words that the orchestrator does not understand are ignored.
- **Applies to.** `20-design/15-live-ops-overview.md` (`ops`), `20-design/14-integrations-access.md`, `20-design/91-metrics-catalogue.md`, `20-design/02-platform.md` (`telemetry`).
- **Test.** `TestLiveness` and `TestLivenessConfigFloor`; `TestReadyzBodies`; `TestStatusAccessorsMakeNoNetworkCall` (and archtest: `ops` has no client dependency); `TestProviderStatusMatrix` (unconfigured is `disabled`); `TestStatusContractShape`; `TestMetricNamesAreExposedWithTheirUnits`; e2e test that `/livez` turns 503 when every slot of an upstream is stuck.

### GR-42 Logs are structured and correlated

- **Principle.** Logs are JSON via `slog`, written to stdout, with the level from configuration (INFO by default). Every record carries service, version and build; contextual records carry request id, trace and span ids, webhook delivery id, job key and attempt where applicable. Access logs record the route pattern, not the raw path with query string. Expensive log arguments are evaluated lazily.
- **Rationale.** Unstructured, uncorrelated text logs cannot be searched or joined across a request's lifetime; logging raw URLs leaks query parameters; eagerly built log arguments do work (and sometimes parsing of untrusted input) even when the record is dropped.
- **Applies to.** `20-design/02-platform.md` (`logx`), `20-design/06-httpapi.md`, `20-design/04-refresh-webhook.md`.
- **Test.** `TestChainHeadersRequestIDAndLog`; `TestLogFieldsOnSSEAndRetry`; `TestEventLogValueOmitsPaths`; `TestOTELErrorHandlerRoutedToLogx`; lint `sloglint` (`no-global: all`, `context: all`, key naming); access-log test asserting no query string is logged.

## 11. Build, test and delivery

### GR-43 Tests assert, run offline and exercise the shipping code

- **Principle.** Every test must assert on behaviour. Tests never touch the network: upstreams are faked with `httptest` and the fake GitHub, and CI runs the suite in a network-isolated sandbox. Tests do not depend on the working directory and use `t.TempDir`. Integration and e2e tests boot the real `run()` and the real router, not a test-only assembly. Skipped tests require an issue link. Fixtures are small, recorded per behaviour, licence-checked, with golden files and a documented regeneration script; nothing asserts on the size of a bulk dump.
- **Rationale.** A test without an assertion only proves the code does not panic; network-dependent tests are flaky and leak credentials; testing an assembly other than the shipped one gives false confidence. Count-based assertions on large dumps break on every regeneration and test nothing specific.
- **Applies to.** `41-test-plan.md`, `20-design/05-github-adapter.md` (`githubtest`, fake GitHub), `20-design/16-cmd-build-release.md`, every unit's test section.
- **Test.** CI job running `go test -race ./...` under `unshare -n` (fails on any outbound connection); lint `usetesting`, `thelper`, `testifylint` or equivalent "no assertion" check; `forbidigo` rule on `t.Skip` without an issue link; `TestRealRunnerMatchesRunTree` (the e2e harness uses the production `run()`); `TestFixtureLicences`; `TestDemoFixtureDecodes`.

### GR-44 CI is the verifier and gates every change

- **Principle.** Continuous integration is the only verifier: `go vet`, golangci-lint, `govulncheck`, `go test -race`, archtest, generated-artefact drift checks, the frontend build, type check and tests, the image build and a readiness smoke test against the built image must all pass before merge. Dependency-update pull requests go through the same required checks. No script prints success without checking.
- **Rationale.** Verification that depends on someone remembering to run it does not happen; unattended dependency bots merging unverified updates break builds and introduce vulnerabilities.
- **Applies to.** `20-design/16-cmd-build-release.md`, `41-test-plan.md`, `40-implementation-plan.md`.
- **Test.** CI configuration test asserting the required-check list on the default branch includes every job above; `TestDepguardInSync`; `TestGenArtifacts`; image job smoke test (`/readyz` reaches 200 against the fake GitHub using the built image).

### GR-45 Artefacts are reproducible, immutable and verifiable

- **Principle.** One `go build` produces the binary, with version and commit embedded from build info or `-ldflags`; the container image is built from that same binary, so the artefact that ships is the artefact CI tested. Images are pinned by digest, have no shell or package manager, and never download or update code at runtime. Dependencies come from the public module proxy and checksum database (`GOFLAGS=-mod=readonly`, `go mod verify`); no private or plain-HTTP repositories are required to build. Runtime tuning uses Go defaults (TLS) plus `GOMEMLIMIT` and container-aware `GOMAXPROCS`; no obsolete flags. Releases carry checksums and an SBOM.
- **Rationale.** If the shipped artefact can differ from the tested one, the tests prove nothing about production. Self-updating containers make the running version unknowable and turn a supply-chain compromise into remote code execution. Private build dependencies make the project unbuildable for anyone outside one network.
- **Applies to.** `20-design/16-cmd-build-release.md`.
- **Test.** CI job that builds twice from a clean checkout and compares binary digests (reproducibility); image test asserting the entrypoint binary digest equals the CI-built binary and that the image has no shell; `go mod verify` and `GOFLAGS=-mod=readonly` in CI; `TestTLSConfigDefaults`; release job verifying checksums and SBOM presence.

---

## 12. The guardrails in ten lines

1. One path: one binary, one router, one config, one ordered startup sequence (GR-01, GR-12).
2. Typed everything: config, errors, keys, states and wire types; unknown input is an error, not a default (GR-07, GR-11, GR-12, GR-23, GR-29).
3. An immutable read model, rebuilt by a sync engine and swapped atomically; readers never scan or lock (GR-28).
4. Freshness is data: `fetchedAt`, validator, status and last error on every item; stale-while-error is visible (GR-26, GR-40).
5. Upstreams are isolated: one client, breaker and limiter each; typed failures; backoff; budgets (GR-22 to GR-25).
6. Never trust input: sanitise rendered HTML, verify webhooks on raw bytes, escape XML structurally, parse within limits (GR-20, GR-32 to GR-34).
7. Secrets are a type: never in URLs, never in logs, required at startup (GR-13).
8. Health tells the truth: liveness, readiness and status derive from outcomes; the management plane is not public (GR-17, GR-41).
9. URLs are generated from one route table and crawled in tests (GR-15).
10. Tests assert, run offline and exercise the code that ships; CI gates every change; the tested artefact is the shipped artefact (GR-43 to GR-45).
