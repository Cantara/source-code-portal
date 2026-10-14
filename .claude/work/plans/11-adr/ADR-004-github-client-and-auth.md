# ADR-004: GitHub client: own thin client, App-first auth, REST baseline plus GraphQL sweep

Status: Accepted. Date: 2026-10-14. Refines 04 D-04 and D-17 (A-04). Corrects 02 (go-github) in favour of 03 and 09.

## Context
D-04 puts the HTTP client, auth, ETag, rate limit, pagination, breaker and webhook types in one adapter; D-17 makes a GitHub App the recommended credential with pluggable `TokenSource` and `SourceReader`, GHES/GHEC-DR support and onboarding tooling. `google/go-github` ships a breaking major roughly monthly (v87 to v92 in four months), has no 304 handling (a 304 is an `*ErrorResponse`), and its `ValidatePayload` falls back to SHA-1 and skips validation without a secret, contrary to D-09. `ghinstallation` requires an older go-github major and jwt v4.

## Decision
- `internal/github` is an own thin client over `net/http` (about 1,500 lines): typed REST and GraphQL readers, conditional requests, link-header pagination, per-installation per-resource rate state, one breaker and limiter per upstream, typed `github.Error`, webhook verification (SHA-256 only, constant time, raw bytes) and decoding into `scm.Event`, installation registry, `Capabilities()`, `doctor`, App manifest flow. It owns every GitHub wire type; consumers see only `scm` types.
- Consumer-side interfaces are defined by each feature (`catalog.RepositoryLister/HeadProber`, `commits.CommitLister`, `docs.FileReader/TreeLister/RenderedReader`, `manifests.TreeReader/BlobReader`, `media.RepoImageReader`, `webhook.Verifier/Redeliverer`); `github.Client` satisfies them structurally **because their signatures use only stdlib and kernel types** (`scm.TreeOptions`, `scm.Page[T]`, `scm.Tree`). Interfaces that need consumer-owned view types (`ops.InstallationStatus`, `catalog.Tracker`) are satisfied by bridges in `cmd/scp/internal/bridge`.
- Credentials: `auth.TokenSource` with strategies `github_app` (JWT RS256 via `golang-jwt/jwt/v5`, installation tokens cached per (installation, scope), minted with singleflight, refreshed early, never persisted), `fine_grained_pat`/`pat`, `oauth_user`, `token_command` (absolute path, no shell, timeout), `anonymous` (dev only, refused on GHEC-DR). Key material behind `AppKeySource` (file/env now, KMS later) and typed `Secret`.
- Discovery prefers `GET /installation/repositories`; installation lifecycle events drive discovery; `catalog.reconcile` (30 min) is the safety net with a mass-removal guard.
- Acquisition: REST is the complete baseline and fully implements the interfaces; a GraphQL bulk sweep (head sha, tree oid, issues, licence, latest release for about 50 repositories per query; blobs under 256 KiB in batches of 25) is selected by `Capabilities()` behind the same `HeadProber`/`BlobReader`, staged after the REST path works.
- Validators are persisted content SHAs; ETags are used only inside one token lifetime because REST ETags depend on the Authorization header.
- Hosts are per source (github.com, GHES, GHEC with data residency) with proxy, CA and `pin_hosts`; the SSRF policy is explicit per host.
- `scp doctor` verifies credentials, effective permissions, webhook reachability and rate state; optional `git` acquisition is specified (`internal/gitsource`) and built post-v1.
- `ListTree` is memoised for 60 s per (repo, ref) so `docs` and `manifests` share one tree call; the shared call runs on a detached context with its own timeout, callers wait on their own contexts, and the shared `scm.Tree` is opaque and immutable.

## Alternatives considered
go-github behind the adapter (its churn and verification gaps would be hidden but still maintained); `go-githubauth`/`ghinstallation` (extra module with 34 stars or a second go-github copy); GraphQL-only (GHES feature coverage unknown, 5,000-point separate bucket).

## Consequences
We maintain the client and must track REST API version headers (`2026-03-10` on github.com, `2022-11-28` on GHES per research). Budget at 1,000 repositories: under 2% for manifests, a head sweep of about 20 GraphQL points per 5 minutes; App installation REST limit is 5,000/h minimum, +50/h per repository above 20, cap 12,500/h (GHEC 15,000/h), verified 2026-10-14.

## Evidence
`05-research/09-github-app-integration.md` sections 1-9; `05-research/03-github-api.md`; `05-research/02-go-stack-currency.md` sections 3.1 and 7; https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api (2026-10-14).

## Exit condition
Replace the client with go-github only if its release cadence stabilises and it gains 304 handling and mandatory SHA-256 verification.

## Amendment 2026-10-14 (review revision)

The Go review compiled the original interface set and found it unsatisfiable by one type: `docs.FileReader.ReadFile` and `media.RepoImageReader.ReadFile` had different signatures under one method name, `docs` and `manifests` each declared their own `TreeOptions` for the same `ListTree`, and `catalog`/`ops` result types were consumer-owned so `github` (rank 2) could not name them (F-01, blocker). Fixes: the media method is `ReadRepoImage`; `TreeOptions`, `Page[T]` and an opaque `Tree` are kernel types; `ops` and `Tracker` are bridged; `cmd/scp/assert.go` asserts every direct interface at compile time. `singleflight` hazards (first caller's context, shared slices) are handled as described above (F-09, verified: `Do`/`DoChan` take no context). Redelivery of failed App webhook deliveries is the optional `webhook.redelivery` global job through `webhook.Redeliverer`.
