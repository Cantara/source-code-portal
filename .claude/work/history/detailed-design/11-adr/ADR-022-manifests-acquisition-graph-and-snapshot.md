# ADR-022: Manifests acquisition, org graph and snapshot integration

Status: Accepted. Date: 2026-10-14. Implements 04 D-15 "Acquisition" and "Consumers" (A-15).

## Context
D-15 wants discovery through the Git Trees API (truncation-aware) with a path allow-list, conditional fetch by blob SHA, push-driven refresh, an org-wide internal graph with a reverse index and version spread, and consumers in the UI, API and LLM outputs.

## Decision
- Jobs: `manifests.refresh` (per repo: tree discovery, fetch only unseen `(sha, parserID, parserVersion)`, verify the blob SHA, parse, assemble, persist, publish the slot, enqueue the graph job) and `manifests.graph` (global, debounced 1-5 s, coalesced). Cold start runs at `PrioBackfill` under the budget gate and never gates readiness.
- Tree handling: recursive tree; on `truncated` switch to directory-walk (pruned, `MaxTreeCalls` 200, `Truncated` surfaced); symlinks and submodules skipped; blobs under 256 KiB via GraphQL batches of 25 (when capable), larger via REST raw; raw blobs up to 256 KiB cached in SQLite (`manifests.blob`), parse results in `manifests.parse`.
- Change detection: the job first compares the HEAD tree oid with the stored tree SHA and returns `Unchanged` without an API call; `catalog.heads` (GraphQL bulk or REST fallback) emits `EventHeadChanged` with the HEAD tree oid; `manifests.React` enqueues only on a differing tree oid; push events intersect `ChangedPaths` with the allow-list; incomplete path lists (2,048 commits), forced pushes, creations and repository events trigger a full reconcile; a 30-minute planner TTL bounds drift.
- Snapshot: per-repo `Slot[freshness.Value[RepoFacts]]`; `Global[*Graph]` with interned edges, producer maps (published-name and source-URL matching), reverse index, Tarjan SCCs, version-spread histograms; rebuilt in about 9 ms per 200,000 edges; locked transitive components stay per unit in the store.
- Outputs: JSON API (`/repos/{repo}/manifests|dependencies`, `/dependencies/graph|versions|licenses`, `/components?purl=`), `dependencies.md` for LLMs (names and versions only, `trust: untrusted-data`), CycloneDX export, badges (`language`, `build-tool`, `latest-version`, `internal-deps`), metrics.
- Purls and unit ids are query parameters, never path segments (they contain `/`, `:`, `@`, `%`); this replaces `/packages/$purl` in the frontend research with `/packages?purl=`.

## Alternatives considered
Graph as a projector on every publish (27 s of CPU during a 3,000-completion cold start); incremental mutable graph index (46-95 microseconds per repo but needless at 1,000 repositories); GraphQL `dependencyGraphManifests` as the discovery source (public preview fields, App-token support unverified).

## Consequences
Budget at 1,000 repositories: about 1,000 tree calls plus 240 GraphQL points cold; about 90 REST plus 40 GraphQL points per hour steady (plan H). Manifests facts arrive late after cold start and are stale-marked until then.

## Evidence
`05-research/07-manifest-parsing.md` sections 6, 8, 9, 11 (experiments E8, E9, E11).

## Amendment 2026-10-14 (review revision)

The graph is **advisory, not a publish gate** (M1): the validator "every graph unit points to an existing repo" is removed because it blocked `catalog.reconcile` from removing a repository while the debounced graph job was pending; readers skip units whose repository no longer exists (counted), and the graph job also has a 5-minute planner TTL. The graph is built per `scm.ViewClass` (public variant only from public repositories) when non-public repositories are enabled (M2). The push-payload fields the reactor relies on are unverified against GitHub's documentation (V13); the head-sweep comparison is the backstop. Locked components live in the cache namespace `manifests.locked`; `RepoFacts` (about 30 MB estimated) is added to the memory budget (m6).
