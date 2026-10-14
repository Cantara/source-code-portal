# 05-research/03 - GitHub API facts for the adapter, sync and webhook design

Research date: **2026-10-13** (all web facts below were fetched on this date unless a row shows another date).
Scope: `internal/github` (adapter), `internal/sync` (budget and scheduling), `internal/webhook` (receiver). Conforms to `04-decisions.md` (D-02, D-03, D-04, D-09, Q-09, Q-13, Q-24); proposals that change a decision are in section 10 ("Deviations proposed").

## 0. Executive summary (recommendations)

1. Always send an explicit `X-GitHub-Api-Version`; a second version, `2026-03-10`, now exists (default for unversioned requests is still `2022-11-28`, supported until 2028-03-10). Default the adapter to `2026-03-10` for github.com, make it configurable (GHES may lag), and read rate limits from `resources.core`, never the removed `rate` object.
2. Credential: GitHub App with read-only **Metadata + Contents** is the primary path (also the webhook source); fine-grained PAT (same two permissions, single org, expiry) is the fallback. An App installation on 1,000 repositories gets **12,500 req/h**, not 5,000 (Q-09 understates it); a PAT stays at 5,000.
3. Do not build "cheap restart" on persisted REST ETags with App auth: GitHub's ETag is derived from the request headers including `Authorization` (third-party evidence; I reproduced the `Accept` part live, not the `Authorization` part) and installation tokens rotate hourly, so persisted ETags are likely invalid after a restart or hourly rotation; use content identifiers (commit/blob SHAs) instead. Within one token lifetime conditional requests work and authenticated 304s are documented as free.
4. Make **GraphQL the bulk read path** (one query returns 50 repos with metadata, topics, licence, last commits, latest release, open issue and PR counts for about 2 points; the 500,000-node and 10 s limits are far away) and keep REST for README text on change, the repository listing in PAT mode and anything GraphQL cannot express. Cold start for 1,000 repos: about 80 GraphQL points plus about 100 REST calls, against about 3,660 REST calls for a REST-only design.
5. Webhooks are a latency optimisation only, but they carry most of the payload: the `push` payload has up to 2,048 commits with `added/removed/modified` per commit and the `release` payload has the release, so steady state needs almost no API calls; correctness comes from PUSHED_AT-ordered incremental GraphQL sweeps plus a full reconcile (about 100 GraphQL points/h in total).
6. Webhook receiver: verify `X-Hub-Signature-256` (HMAC-SHA256, `sha256=` prefix, constant time) over the raw bytes before parsing, cap the body at 25 MB, answer 2xx within 10 s (aim for under 1 s, enqueue and return 202), dedupe by `X-GitHub-Delivery`, and assume nothing is ever retried: GitHub does **not** redeliver automatically (manual or API redelivery within 3 days only), so startup reconcile is mandatory.
7. Rate-limit handling: primary exhaustion is HTTP 403 (429 is also possible) with `x-ratelimit-remaining: 0` and **no** `Retry-After` (observed live); secondary limits return 403/429 with `Retry-After`, else wait at least 60 s and back off exponentially; GitHub asks for serial requests, so cap REST concurrency low (default 4) and GraphQL at 1-2.
8. `open_issues_count` counts open issues plus open pull requests (no official statement, community and client evidence only); show it as "open issues and PRs" or take the true count from GraphQL `issues(states: OPEN){totalCount}`.
9. Go: `go-github` is at **v92.0.0** (2026-09-14) with six majors in four months (new import path each time, Go 1.26 required), no conditional-request support (a 304 surfaces as an `*ErrorResponse`), and a webhook validator that accepts SHA-1/SHA-512 and skips validation when the secret is empty; prefer a thin owned client (`net/http` + `golang-jwt/jwt/v5` + own ETag transport + own HMAC verifier), keep go-github optional behind the adapter.
10. D-15 impact: the synchronous SBOM endpoint closes **2026-11-13** (use the asynchronous generate-report/fetch-report pair or avoid SBOM), and the recursive Git Trees API is capped at 100,000 entries or 7 MB (`truncated: true`).

## 1. Method and conventions

- Primary sources: docs.github.com pages; the machine-readable REST description (`github/rest-api-description`, `api.github.com.json`, info.version 1.1.4, downloaded 2026-10-13, 13.5 MB) queried with `jq`; the GraphQL public schema (`https://docs.github.com/public/fpt/schema.docs.graphql`, 2026-10-13) queried locally; the go-github, ghinstallation and related repositories read as raw source on 2026-10-13; Go module proxy (`proxy.golang.org`) for versions and release times; live unauthenticated probes of `api.github.com` (appendix A).
- Tooling caveat: docs pages were read through a summarising fetch tool. Wherever that tool said "not stated" I recorded "not stated" instead of filling in from memory; where it might have been lossy I cross-checked against the OpenAPI description or schema (marked "(OAS)" or "(schema)").
- "Verified" below means: read on the cited source on 2026-10-13. "Third-party" means a non-GitHub source (project README, issue). "Unverified" items are collected in section 12.
- Source column uses short paths; the full URL is in the source index (section 13), identical host `docs.github.com` unless stated.

## 2. REST API facts

### 2.1 Versioning

| Fact | Source (URL, date 2026-10-13) |
|---|---|
| Supported versions: `2026-03-10` (released 2026-03-10, end of support "not yet scheduled") and `2022-11-28` (end of support 2028-03-10). | https://docs.github.com/en/rest/about-the-rest-api/api-versions |
| Header `X-GitHub-Api-Version: <version>`; omitted header means `2022-11-28`. Each version is supported 24 months after a newer one is released; nearing closure responses carry `Deprecation` and `Sunset` headers; a retired version gets `410 Gone`. | same |
| "Unversioned requests move to the next oldest supported version, not the closing one" i.e. the implicit default will change; therefore never rely on the default. | same |
| Breaking changes ship only in a new version; additive changes (new fields, headers, enum values) appear in all supported versions. Therefore the client must tolerate unknown fields and unknown enum values. | same |
| Changes in `2026-03-10` that touch us: `GET /rate_limit` loses the deprecated `rate` property (use `resources.core`); `has_downloads` removed from Repository responses; `use_squash_pr_title_as_default` removed; Contents listing reports submodules as `type: "submodule"`. None for releases, webhooks, topics, commits; no README change listed. | https://docs.github.com/en/rest/about-the-rest-api/breaking-changes |
| GHES: the docs for GHES 3.22 (the `@latest` GHES docs resolved to 3.22) list the same two versions, but state versions are "decoupled" and a REST version can be used if "available for your GitHub Enterprise Server version". REST base `http(s)://HOSTNAME/api/v3`. | https://docs.github.com/en/enterprise-server@latest/rest/about-the-rest-api/api-versions ; https://docs.github.com/en/enterprise-server@latest/rest/overview/resources-in-the-rest-api |
| Other required headers: `User-Agent` mandatory (invalid or missing gives 403); recommended `Accept: application/vnd.github+json`. | https://docs.github.com/en/rest/using-the-rest-api/getting-started-with-the-rest-api |
| go-github v92 default is `2022-11-28` (constant `api20221128` in `NewClient`), `2026-03-10` constant exists, per-request override `WithVersion`. | https://raw.githubusercontent.com/google/go-github/master/github/github.go (2026-10-13) |

Recommendation: `scp` config key `github.api_version` default `2026-03-10`; on GHES leave it settable, and fail readiness with a clear message if the first call returns 400/410. Add a contract test that runs recorded fixtures for both versions.

### 2.2 Primary rate limits

| Credential | Limit (req/h) | Source |
|---|---|---|
| Unauthenticated (per IP) | 60. Observed live 2026-10-13: `x-ratelimit-limit: 60`, `x-ratelimit-resource: core`, HTTP **403** with message "API rate limit exceeded for <ip>" and no `Retry-After`. | https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api ; appendix A |
| PAT / user-to-server token | 5,000 per user, shared across all of that user's tokens and apps; 15,000 if acting through a GitHub Enterprise Cloud org-owned App/OAuth app | same; OAS `/rate_limit` description ("Requests authenticated as a user share the user's rate limit across credentials") |
| GitHub App installation token | 5,000 minimum; non-GHEC installations with more than 20 repositories get +50/h per repository and (org installs with more than 20 users) +50/h per user, **capped at 12,500**; GHEC: 15,000 | same |
| `GITHUB_TOKEN` in Actions | 1,000 per repository (15,000 GHEC) | same |
| GHES | Not covered in the github.com page. GHES docs: admin enables HTTP API rate limiting in the Management Console (authenticated and unauthenticated values are admin-set; page does not state whether it is on by default). A third-party README (Feb 2025) says primary limits are disabled by default on GHES. | https://docs.github.com/en/enterprise-server@latest/admin/configuring-settings/configuring-user-applications-for-your-enterprise/configuring-rate-limits ; https://github.com/bored-engineer/github-conditional-http-transport |

Arithmetic for the App formula: `5,000 + 50 x (repos - 20)`; reaches the 12,500 cap at 170 installed repositories, so any 1,000-repo installation is at 12,500/h (if the org also has more than 20 users the user term only helps below the cap).

`GET /rate_limit` does not count against the primary limit (can count against secondary); categories in `resources`: `core`, `search`, `code_search`, `graphql`, `integration_manifest`, `dependency_snapshots`, `dependency_sbom`, `actions_runner_registration`, `source_import` (OAS description of `/rate_limit`, 2026-10-13). REST and GraphQL are **separate buckets** (`core` vs `graphql`), which is what makes the hybrid design below cheap.

### 2.3 Secondary rate limits and headers

| Fact | Source |
|---|---|
| Concurrency: at most 100 concurrent requests (shared REST+GraphQL). | rate-limits page |
| Points per minute: 900 for REST endpoints, 2,000 for GraphQL. Points: most GET/HEAD/OPTIONS = 1; POST/PATCH/PUT/DELETE = 5; GraphQL without mutation = 1, with = 5. Some endpoints cost more (unpublished). | same |
| CPU: at most 90 s of CPU per 60 s real time, at most 60 s of that for GraphQL. Content creation about 80/min and 500/h. Limits "can change without notice"; there is no way to query secondary status. | same |
| Headers: `x-ratelimit-limit`, `-remaining`, `-used`, `-reset` (UTC epoch seconds), `-resource`; `retry-after` on secondary limit responses. Headers can differ across regions; they are authoritative over `/rate_limit`. | same |
| Reaction rules: primary: do not retry before `x-ratelimit-reset`. Secondary: if `retry-after` present wait that many seconds; else if remaining is 0 wait until reset; else wait at least one minute; on repeated failures exponential backoff then error; continuing while limited "may result in the banning of your integration". Status can be 403 or 429. | same; https://docs.github.com/en/rest/using-the-rest-api/troubleshooting-the-rest-api |
| Best practice: "make requests serially instead of concurrently"; wait at least 1 s between mutating requests (we do none); prefer webhooks over polling; if polling, respect `x-poll-interval` and use conditional requests. | https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api |
| Timeouts: GitHub terminates an API request after 10 s and returns a timeout response with a "Server Error" message (the troubleshooting page does not name a status code). | troubleshooting page |
| 404 is used instead of 403 for private resources the token cannot see (a bad or under-scoped token looks like a missing repo). | same |

Adapter consequences: classify errors as `RateLimited{ResetAt|RetryAfter, Primary|Secondary}` using the rule order retry-after, then remaining==0 with reset, else 60 s; classify 404 as "not found or not visible" (never as "deleted") unless confirmed by the list; configure `http.Client.Timeout` of about 30 s for REST and 20 s for GraphQL.

### 2.4 Conditional requests (ETag, Last-Modified) and what 304 costs

| Fact | Source |
|---|---|
| Endpoints usually return `etag` and many `last-modified`; send them back as `If-None-Match` / `If-Modified-Since`; unchanged gives `304`. | best-practices page |
| 304 does not count against the primary limit **when the request is authorised** (`Authorization` header). The page says "Generally no". | same |
| Events API explicitly: with ETag and no new events "your current rate limit will be untouched". | https://docs.github.com/en/rest/activity/events |
| The Contents and README endpoints list `304` as a response status (OAS: readme responses 200, 304, 404, 422). | OAS `/repos/{owner}/{repo}/readme` (2026-10-13) |
| **ETag depends on the `Authorization` header.** A docs issue (opened 2024-09-22, closed "not planned" 2024-11-25 after "more-information-needed"/"stale"; a GitHub docs contributor wrote "Your analysis is correct") states that a different token gives a different ETag and an old ETag with a new token does not give a 304, "particularly a problem when using a GitHub App since the installation token refreshes (at least) every hour". A project README (observations of Feb 2025) reverse-engineered the ETag as SHA-256 of the uncompressed body prefixed with the request's `Accept`, `Authorization` and `Cookie` values joined by `:`; it says GitHub does not document this. | https://github.com/github/docs/issues/34689 ; https://github.com/bored-engineer/github-conditional-http-transport (third-party; not reproduced by me, see appendix A for what I could reproduce) |
| Last-Modified / If-Modified-Since: documented as supported and observed working unauthenticated (A4); whether `Last-Modified` is independent of the token is **not documented** and was not observable without a token. | best-practices page; appendix A4 |
| Observed 2026-10-13 without authentication: the ETag of JSON list endpoints is `W/"sha256(Accept + ":" + body)"` (A2); the README ETag (raw, html) is the blob SHA (A5, A6); the commit-SHA media type ETag is the commit SHA (A15); a 304 is **counted** when unauthenticated (A3, A15); a 404 has no ETag (A9). | appendix A |
| GraphQL: no conditional-request support is documented (queries are POST; the GraphQL limits page does not mention ETag). | https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api |

**Token-independent REST validator (documented, narrow):** `GET /repos/{owner}/{repo}/commits/{ref}` with `Accept: application/vnd.github.sha` returns only the commit SHA, and the docs say you can compare a remote ref with a local SHA "by providing the local SHA-1 reference as the ETag" (`If-None-Match: "<sha>"`; OAS description of "Get a commit", 2026-10-13; go-github's `GetCommitSHA1` implements exactly this, `repos_commits.go:221`). Because the validator is the commit SHA itself, it does not depend on the token. The OAS response list for this operation does not include 304 and the docs do not say whether the 304 is free of the primary limit; see appendix A for the unauthenticated behaviour. Use: a one-request-per-repository head check for REST-only fallback (GHES without usable GraphQL, or GraphQL outage).

Design consequence (affects rate-limit-aware fetching, GR-25, D-04, Q-13): conditional requests are an in-token-lifetime optimisation only. With a PAT the token is stable so persisted ETags survive restarts; with App installation tokens (1 h lifetime, refreshed about 1 minute before expiry by ghinstallation) they do not. The ETag store key must include the credential identity (installation id or PAT fingerprint) so a token change is a clean miss and not a stale 304 attempt. An optional "ETag recompute" optimisation (store SHA-256 of each response body, recompute the expected ETag under the new `Authorization`, send it as `If-None-Match`) is possible but relies on undocumented behaviour; if used, make it a feature flag that degrades to a normal 200.

### 2.5 Pagination

| Fact | Source |
|---|---|
| `per_page` maximum 100 for most endpoints; larger values are silently reduced; default is per endpoint (30 for the endpoints below; `GET /events` 15). | https://docs.github.com/en/rest/using-the-rest-api/using-pagination-in-the-rest-api ; OAS `components.parameters.per-page` ("max 100", default 30) |
| `Link` header `rel` values `next`, `prev`, `first`, `last`; some may be absent (`last` omitted if it cannot be computed; `prev` on page 1). Use the URLs from the header; query parameters differ by endpoint (`page`, `before`/`after`, or `since`). | pagination page |
| The apps webhook deliveries list uses a cursor (`cursor` parameter) not `page` (OAS `/app/hook/deliveries`). | OAS |
| go-github: `Response.NextPage` int (page-based) and `Response.Cursor`/`NextPageToken` for cursor endpoints; `*Iter` iterator methods (`iter.Seq2`) exist (see section 7). | go-github source (2026-10-13) |

Engineering advice (not from docs): for the full org listing sort by `full_name` ascending (stable under concurrent pushes; `sort=pushed` or `updated` can duplicate or skip repositories between page fetches) and de-duplicate by repository `id`; abort the listing, and keep the previous catalogue, if any page fails (fail-closed, GR-26).

### 2.6 Listing organisation repositories

`GET /orgs/{org}/repos` (OAS and docs, 2026-10-13: https://docs.github.com/en/rest/repos/repos#list-organization-repositories):

- Parameters: `type` in `all` (default), `public`, `private`, `forks`, `sources`, `member`; `sort` in `created` (default), `updated`, `pushed`, `full_name`; `direction` (`asc` for `full_name`, else `desc`); `per_page` max 100, default 30; `page`.
- Response is an array of **Minimal Repository** objects. In the schema only URL fields, `id`, `node_id`, `name`, `full_name`, `owner`, `private`, `fork`, `description` are *required*; the fields we want (`default_branch`, `archived`, `disabled`, `visibility`, `topics`, `license`, `open_issues_count`, `pushed_at`, `size`, `language`, `html_url`, `created_at`, `updated_at`) are listed as properties, optional in the schema, and the docs example shows them present. The adapter must therefore treat them as optional and fall back to `GET /repos/{owner}/{repo}` (full repository schema) when a field is absent. (OAS `components.schemas.minimal-repository`, 2026-10-13.)
- Permission: Metadata: read (https://docs.github.com/en/rest/overview/permissions-required-for-github-apps, UAT and installation tokens both listed).
- `type` filter semantics for our policy (public only by default, Q-03): `type=public` filters server-side; `forks`/`sources` filter forks; we also filter `archived` and `disabled` client-side because there is no query parameter for them. For an App installation use `GET /installation/repositories` (OAS: returns `total_count`, `repositories[]` of the **full** `repository` schema, `repository_selection`; only `per_page`/`page` parameters, no type filter); that is the canonical list of what the installation can see and avoids guessing what `/orgs/{org}/repos` returns for an installation token (unverified).
- `pushed_at` and `updated_at` are in each list element; consequently the list response (and its ETag) changes whenever any repository in the org is pushed to, so conditional polling of the list rarely yields 304 in a busy org (cost 10 requests per sweep at 1,000 repos).
- `GET /repos/{owner}/{repo}` lists 301 among its statuses as well as 403 and 404; the current docs page does not say when 301 occurs. GitHub's 2015 changelog (https://developer.github.com/changes/2015-07-21-automatic-redirects-for-renamed-repositories, found 2026-10-13, dated source) says the API automatically redirects renamed repositories with a 301 or 307 depending on request type; the docs on renaming (https://docs.github.com/en/repositories/creating-and-managing-repositories/renaming-a-repository) say redirects stop working if the old name is reused and do not mention the API. Key repositories internally by numeric `id`/`node_id`, not by name; handle the `repository` webhook `renamed` action (payload `changes.repository.name.from`, schema below).

### 2.7 Repository topics

- Included in the list and single-repo responses as `topics` (strings). Dedicated `GET /repos/{owner}/{repo}/topics` returns `{"names":[...]}`, `per_page` default 30 max 100, permission Metadata read; no special media type is required now (docs recommend `application/vnd.github+json`). Source: https://docs.github.com/en/rest/repos/repos#list-repository-tags (the same page covers topics), permissions page.
- Webhook signal for topic changes: the `repository` event, action `edited`, has `changes.topics.from` (octokit/webhooks JSON schema, https://raw.githubusercontent.com/octokit/webhooks/main/payload-schemas/api.github.com/repository/edited.schema.json, 2026-10-13: `changes` may contain `description`, `default_branch`, `homepage`, `topics`, each with a `from`). The GitHub docs page itself documents no payload table for this event, so treat the octokit schema as authoritative-by-convention.

### 2.8 README endpoint

`GET /repos/{owner}/{repo}/readme` and `GET /repos/{owner}/{repo}/readme/{dir}` (OAS and docs 2026-10-13; https://docs.github.com/en/rest/repos/contents#get-a-repository-readme):

- "Gets the preferred README for a repository". `ref` parameter: commit, branch or tag; default is the default branch. `{dir}` is "the alternate path to look for a README file".
- Media types: `application/vnd.github.raw+json` returns raw file contents (the OAS text calls raw the default when no media type is requested; with `Accept: application/vnd.github+json` the live API returns the JSON object, appendix A7: fields `type` (always `file`), `encoding`, `size`, `name`, `path`, `content`, `sha`, `url`, `git_url`, `html_url`, `download_url`, `_links`); `application/vnd.github.html+json` returns the README rendered to HTML ("markup languages are rendered ... using GitHub's open-source Markup library"). The OAS lists only `application/json` as a response content type; the raw/HTML variants are media-type negotiated (observed live in appendix A5-A7).
- Statuses: 200, 304, 404 ("Resource not found"), 422. The page does not say which conditions yield 404 (no README, repo not visible, ref not found), so model `NoReadme` and `NotVisible` as the same "404" state and keep the repository's html_url link.
- Size behaviour: the **README endpoint page states no size limit**. The generic Contents endpoint states: 1 MB or smaller all features; 1-100 MB only raw or object media types (with the object type `content` is an empty string and `encoding` is `"none"`); over 100 MB not supported; directory listings capped at 1,000 files. Plan: always request raw (`Accept: application/vnd.github.raw+json`), cap the body we read at a configured maximum (for example 2 MiB, enforced with `http.MaxBytesReader`-style limit on the response), store a typed `TooLarge` state beyond it. If the JSON form is used handle `encoding: "none"` and `content: ""`.
- Directory case: the response `type` enum for this endpoint is `file`; a README that is a symlink/submodule is described on the contents endpoint (symlink to a normal file returns that file; otherwise a `symlink` object with `target`; submodule returns `type: submodule`). The client must decode into a single object (never assume an array) and treat any `type != "file"` as "unsupported README kind".
- HTML variant and AsciiDoc: for D-06's AsciiDoc bridge `application/vnd.github.html+json` returns server-rendered HTML for `.adoc`, `.rst` etc. It is a rendering by GitHub, not a sanitising guarantee, so still pass through bluemonday. Relative links in the returned HTML are **not** rewritten (observed: `href="docs/install_macos.md"`, appendix A6), headings get `user-content-` anchors and inline SVG, and the HTML is about 3.5x the raw size.
- Permission: Contents: read (permissions page); ETag/304 supported (304 listed) but see 2.4.
- GraphQL equivalent has no "preferred README" resolution; it needs explicit candidates (`HEAD:README.md`, `HEAD:readme.md`, `HEAD:README`, `HEAD:README.adoc`, ...). `Blob.text` is UTF-8 or null when binary; `isTruncated` and `byteSize` exist; the truncation threshold is **not documented** (section 12).

### 2.9 Commits

`GET /repos/{owner}/{repo}/commits` (docs and OAS 2026-10-13: https://docs.github.com/en/rest/commits/commits#list-commits):

- Parameters: `sha` (SHA or branch to start from; default is the default branch), `path`, `author`, `committer`, `since`, `until` (ISO 8601 `YYYY-MM-DDTHH:MM:SSZ`, valid 1970-01-01..2099-12-31), `per_page` (default 30, max 100), `page`.
- Response per item: `sha`, `html_url`, `commit.{message, author{name,email,date}, committer{...}, verification}`, top-level `author`/`committer` (user object, or an empty object when the identity is not a GitHub user), `parents[]`. `stats` and `files` appear only when fetching a single commit. Statuses: 200, 400, 404, 409 (docs do not say when; empty repositories are the usual case), 422, 429, 500.
- Permission: Contents: read.
- `since` filters by commit date; use it with a persisted high-water mark if a REST path is used. Retention rule (GR-30): bounded latest-K ring per repo, store SHA, first-line message, author name and date, committer date, html_url; drop e-mails (privacy, GR-35).
- Default branch = branch whose head we track; webhook `push` for other refs must be ignored for the commit feed (check `ref == "refs/heads/" + repository.default_branch`).

### 2.10 Releases and tags

- `GET /repos/{owner}/{repo}/releases/latest`: "the most recent non-prerelease, non-draft release, sorted by the `created_at` attribute"; `created_at` is the date of the **commit** used for the release, not when it was drafted or published (OAS description, 2026-10-13). 404 when none (docs list 404 as "Resource not found"). Permission: Contents: read. Fields: `id`, `tag_name`, `target_commitish`, `name`, `body`, `draft`, `prerelease`, `immutable`, `created_at`, `published_at`, `author`, `assets`, URLs.
- `GET /repos/{owner}/{repo}/releases`: `per_page` max 100; drafts only visible with push access; plain tags without a release are not included.
- `GET /repos/{owner}/{repo}/tags`: `per_page` max 100, items `name`, `commit.sha`, `zipball_url`, `tarball_url`, `node_id`; Metadata: read; the page states **no ordering** and offers no `sort` parameter, so "latest tag" cannot be taken from `per_page=1` safely: sort client-side (semver, else commit date) from a page of up to 100, or prefer releases. GraphQL `refs(refPrefix:"refs/tags/", orderBy:{field: TAG_COMMIT_DATE, direction: DESC}, first: 1)` gives a server-side order (enum `RefOrderField.TAG_COMMIT_DATE`, "order refs by underlying commit date if the ref prefix is refs/tags/", schema 2026-10-13).
- GraphQL: `Repository.latestRelease: Release` ("the latest release for the repository if one exists", schema 2026-10-13) with `tagName`, `publishedAt`, `isPrerelease`, `isDraft`, `isLatest`, `url`.
- Webhook `release` actions: `created`, `deleted`, `edited`, `prereleased`, `published`, `released`, `unpublished` (docs 2026-10-13). The page does not explain the draft/published/released differences; the octokit schema for `release.published` requires `published_at` (non-null). Handling rule: any `release` event for a repository only enqueues "refresh latest release" (Q-15); the payload's `release.draft`/`prerelease` can be used to skip obviously irrelevant events.

### 2.11 Issue counts (`open_issues_count`)

- Official: the issues docs say "GitHub's REST API considers every pull request an issue, but not every issue is a pull request"; pull requests are identified by the `pull_request` key in issue-list results (https://docs.github.com/en/rest/issues/issues, 2026-10-13). The repository schema documents `open_issues_count` and `open_issues` (identical) without saying what they include.
- Not officially documented, but corroborated: a community discussion (https://github.com/orgs/community/discussions/102620, 2024-01-29, unanswered, no staff reply) and a client project state the field reports open issues **plus** open pull requests.
- True issue-only count: GraphQL `repository.issues(first: 1, states: OPEN){ totalCount }` (schema: `issues(... states: [IssueState!])` 2026-10-13) and `pullRequests(first:1, states:OPEN){totalCount}`. REST alternatives: Search API `is:issue state:open repo:o/r` (authenticated search limit 30 req/min, https://docs.github.com/en/rest/search/search) or `GET /repos/.../pulls?state=open&per_page=1` and read the last page from `Link`; both cost an extra request per repository, so use GraphQL.
- Product rule: label the badge "open issues + PRs" when only REST data is present; prefer the GraphQL counts.

### 2.12 Compare API

`GET /repos/{owner}/{repo}/compare/{basehead}` (OAS description 2026-10-13): `basehead` is `BASE...HEAD`; refs or SHAs (cross-fork `USER:BASE...USER:HEAD`); response `status` (`diverged|ahead|behind|identical`), `ahead_by`, `behind_by`, `total_commits`, `commits`, `files`, `base_commit`, `merge_base_commit`. Without `per_page`/`page` the list is limited to **250 commits**; with pagination the file list is only on the first page and holds up to **300 files** for the whole comparison; commits are chronological (the API differs from `git log BASE..HEAD`, which is reverse chronological). Permission: Contents: read. Use: fallback when a `push` payload cannot be trusted (force push, `created`/`deleted`, more than 2,048 commits, payload dropped): `compare/{before}...{after}` yields the changed file list (first 300) for the "did the README or a manifest change" decision.

### 2.13 Git Trees (D-15) and SBOM

- `GET /repos/{owner}/{repo}/git/trees/{tree_sha}?recursive=1`: limit 100,000 entries and 7 MB; `truncated: true` when exceeded; then walk subtrees non-recursively (https://docs.github.com/en/rest/git/trees#get-a-tree). Permission: Contents: read.
- SBOM: the synchronous `GET /repos/{owner}/{repo}/dependency-graph/sbom` is `deprecated=true` with `deprecationDate 2026-05-12` and `removalDate 2026-11-13` (OAS x-github block, 2026-10-13; changelog https://github.blog/changelog/2026-05-12-synchronous-sbom-api-deprecated/; docs "closing down notice"). Replacement: `GET .../dependency-graph/sbom/generate-report` (returns `sbom_url`), then poll `GET .../sbom/fetch-report/{sbom_uuid}` (202 while processing, 302 to a temporary download); reports retained up to one week. Rate limit category `dependency_sbom` exists in `/rate_limit`; its numeric limit and the required permission are not stated on the page (docs: "at least read access to the repository").

### 2.14 Events API (optional change detector)

`GET /orgs/{org}/events` is "List public organization events" only; Events API latency "anywhere from 30s to 6h", "not built to serve real-time use cases", retention 30 days / 300 events, 304 leaves the rate limit untouched, honour `X-Poll-Interval` (https://docs.github.com/en/rest/activity/events). Not suitable as the primary change feed (public only, latency); not recommended.

## 3. GraphQL alternative

### 3.1 Cost model (verified)

| Fact | Source |
|---|---|
| Primary limit: 5,000 points/h per user token (10,000 via GHEC org-owned Apps); App installations 5,000 + 50 per repository above 20 (+50 per user above 20), cap 12,500; GHEC installations 10,000; `GITHUB_TOKEN` 1,000 (15,000 GHEC enterprise). Note: for GraphQL the GHEC figures are 10,000, not the 15,000 of REST. | https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api |
| Query score: add the number of requests needed to fulfil each unique connection (each assumed to hit its `first`/`last`), divide by 100, round to the nearest whole number; minimum 1. | same |
| Node limit: every connection needs `first` or `last` (1..100); a call cannot request more than **500,000 total nodes**; nested connections multiply (docs examples: 550 nodes for 50 repos x 10 issues; 22,060 for a heavier query). | same |
| Timeout: a request over **10 s** is terminated with a 502/504-style timeout, and timeouts deduct extra points from the next hour's limit. | same |
| Secondary: 100 concurrent (shared with REST), 2,000 points/min for GraphQL, CPU at most 60 s of the 90 s per 60 s. | same |
| `rateLimit { cost limit remaining used resetAt nodeCount }` and `rateLimit(dryRun: true)` exist in the schema (`dryRun`: "calculate the cost for the query without evaluating it"). | schema.docs.graphql, 2026-10-13 |
| Fields needed exist: `Repository.defaultBranchRef`, `latestRelease`, `licenseInfo`, `pushedAt`, `isArchived`, `isFork`, `isDisabled`, `visibility` (`PUBLIC|PRIVATE|INTERNAL`), `repositoryTopics(first)`, `issues(states, first)`, `pullRequests(states, first)`, `object(expression:)`; `Commit.history(first, since, until, path, author)`; `Blob{text, byteSize, isTruncated, isBinary, oid}`; `Organization.repositories(first, after, isArchived, isFork, privacy, visibility, orderBy: {field: PUSHED_AT|UPDATED_AT|CREATED_AT|NAME|STARGAZERS}, ownerAffiliations)`; `Query.nodes(ids: [ID!]!)`. | same; https://docs.github.com/en/graphql/reference/repos |
| No conditional requests documented for GraphQL. | rate-limit page |

### 3.2 Proposed batched query and cost arithmetic

```graphql
query Sweep($org: String!, $after: String) {
  organization(login: $org) {
    repositories(first: 50, after: $after, isFork: false, isArchived: false,
                 orderBy: {field: PUSHED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        databaseId id name nameWithOwner description visibility
        isArchived isFork isDisabled pushedAt url
        licenseInfo { spdxId }
        repositoryTopics(first: 20) { nodes { topic { name } } }
        defaultBranchRef { name target { ... on Commit {
          oid history(first: 10) { nodes { oid messageHeadline committedDate url
                                           author { name user { login } } } } } } }
        latestRelease { tagName publishedAt isPrerelease url }
        issues(first: 1, states: OPEN) { totalCount }
        pullRequests(first: 1, states: OPEN) { totalCount }
        readmeMd: object(expression: "HEAD:README.md") { ... on Blob { oid byteSize } }
      }
    }
  }
  rateLimit { cost remaining resetAt nodeCount }
}
```

Cost per page of 50 repositories by the documented formula: connection requests = 1 (`repositories`) + 50 x 4 (`repositoryTopics`, `history`, `issues`, `pullRequests`) = **201**, divided by 100 = **2 points**. Node count = 50 + 50x20 + 50x10 + 50 + 50 = **1,650** (limit 500,000). A full 1,000-repo sweep = 20 pages = **40 points** (60 if the real cost rounds to 3). Measure with `rateLimit(dryRun:true)` in a spike (the alias/`object` behaviour is not described in the docs, section 12). Risk: the 10 s timeout; if a page is slow, halve `first`.

Second pass for README text only for repositories whose `readmeMd.oid` changed: `nodes(ids: [...]) { ... on Repository { object(expression:"HEAD:README.md"){ ... on Blob { text isTruncated } } } }`: no connections, so about 1 point per query; ids per call limit not re-verified (assume 50). Repositories without `README.md` (or case variants, `.adoc`, `.rst`) fall back to REST `/readme` (about 10% assumed).

What GraphQL cannot or should not do: no ETag; `Blob.text` truncation unspecified; README selection differs from REST "preferred README"; installation-token behaviour for `issues`/`pullRequests` on private repos is likely to need the Issues and Pull requests read permissions (unverified; irrelevant for public-only v1).

Go client options (versions from `proxy.golang.org`, 2026-10-13): `github.com/shurcooL/githubv4` only untagged pseudo-version `v0.0.0-20260209031235-2402fdf4a9ed` (2026-02-09); `github.com/Khan/genqlient` v0.8.1 (2025-05-18, schema-driven codegen); `github.com/hasura/go-graphql-client` v0.16.0 (2026-04-11). Because we need one or two static documents, the simplest durable choice is a hand-written query string plus `encoding/json` structs behind the adapter, with a golden test that validates the document against the downloaded public schema in CI.

## 4. GitHub App authentication, tokens, permissions

| Fact | Source (2026-10-13) |
|---|---|
| App JWT: algorithm **RS256**; `iat` recommended 60 s in the past (clock drift); `exp` no more than **10 minutes** in the future; `iss` = client ID (recommended) or app ID; header `Authorization: Bearer <jwt>` (JWTs must use `Bearer`). | https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app |
| Installation token: `POST /app/installations/{id}/access_tokens` with the JWT; "will expire after 1 hour"; optional body `repositories`/`repository_ids` and `permissions` to down-scope (omitted = all permissions of the app); response has token, `expires_at`, permissions, repositories. Installation id discovery: `GET /orgs/{org}/installation`, `/users/{u}/installation`, `/repos/{o}/{r}/installation`, `GET /app/installations`, or the `installation.id` in any webhook payload. Installation tokens may use `Authorization: Bearer` or `token`. | https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app |
| Best practice: cache installation tokens (do not mint per request); store the private key in a vault, never in code; set a webhook secret and verify it; select minimum permissions and subscribe to minimum events; use webhooks instead of polling. | https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/best-practices-for-creating-a-github-app |
| Apps preferred over OAuth apps and PATs for long-lived integrations or org-wide access; not tied to a user; short-lived tokens; centralised webhook; rate limits scale. PAT "could break" if the user loses access. | https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/deciding-when-to-build-a-github-app |

**Minimal read-only App permission set (v1, public repositories)** (permissions page, 2026-10-13: https://docs.github.com/en/rest/overview/permissions-required-for-github-apps):

| Endpoint | Permission |
|---|---|
| `GET /orgs/{org}/repos`, `GET /repos/{o}/{r}`, `GET .../tags`, `GET .../topics`, `repository` webhook | Metadata: read |
| `GET .../readme`, `.../commits`, `.../releases/latest`, `.../compare/...`, `.../git/trees`, `.../contents`, webhooks `push`/`release`/`create`/`delete` | Contents: read |

So: Repository permissions **Metadata: read** (mandatory for every App) and **Contents: read**; Organization permissions: none; no write permissions, no Issues/Pull requests for public-only v1. Webhook events to subscribe: `push`, `release`, `repository` (plus `create` only if tag-without-release display is wanted, see section 10); `installation` and `installation_repositories` and `ping` arrive without subscription ("All GitHub Apps receive this event by default. You cannot manually subscribe to this event"). `GET /installation/repositories` was not found in the permissions table text; it is an installation-token endpoint (OAS). Optional later: Checks/Commit statuses read for CI badges (not needed while status badges come from Jenkins/Snyk providers), Issues/Pull requests read for private repos with GraphQL counts.

**Fine-grained PAT** (https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens, 2026-10-13): scoped to **one** resource owner (cannot span organisations); can be limited to selected repositories; default expiry 30 days unless an owner policy shortens it (infinite lifetimes allowed but may be blocked by policy); if the org requires approval the token is "pending" and reads only public data until approved; GitHub recommends fine-grained over classic and "you should use a GitHub App" for organisation-level or long-lived integrations. Use Metadata: read + Contents: read. Rate limit is the user's 5,000/h shared across all that user's tokens. Because tokens expire silently, the adapter must expose days-to-expiry (from the `github-authentication-token-expiration` response header; the name comes from go-github's constant `headerTokenExpiration` in `github/github.go`, read 2026-10-13, not from a docs page) as a gauge and a readiness warning.

**GHES:** same App model; REST base `http(s)://HOSTNAME/api/v3` and GraphQL `http(s)://HOSTNAME/api/graphql` (https://docs.github.com/en/enterprise-server@latest/graphql/guides/forming-calls-with-graphql and .../rest/overview/resources-in-the-rest-api, 2026-10-13). SSRF allow-list applies (D-09).

## 5. Webhooks

### 5.1 Delivery contract (verified)

| Fact | Source (2026-10-13) |
|---|---|
| Headers: `X-GitHub-Hook-ID`, `X-GitHub-Event`, `X-GitHub-Delivery` (GUID), `X-Hub-Signature` (SHA-1, deprecated, only with a secret), `X-Hub-Signature-256` (HMAC hex digest SHA-256, only with a secret), `User-Agent: GitHub-Hookshot/...`, `X-GitHub-Hook-Installation-Target-Type`, `X-GitHub-Hook-Installation-Target-ID`. Content types: `application/json` or form-encoded (`payload=` field). | https://docs.github.com/en/webhooks/webhook-events-and-payloads |
| Payload cap **25 MB**; above that "GitHub will not deliver a payload" (can happen with `create` when many branches or tags are pushed at once). | same |
| Signature: `sha256=` + hex HMAC-SHA256 over the payload with the secret; compare in constant time; "never use a plain `==`"; handle payload as UTF-8; proxies must not alter the payload; header absent when no secret is set. Test vector: secret `It's a Secret to Everybody`, payload `Hello, World!` gives `sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17`. | https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries |
| Respond with 2xx within **10 seconds** or the delivery is a failure; process asynchronously with a queue; subscribe only to needed events; use HTTPS; keep SSL verification on; IP allow-list from `GET /meta` (`hooks`) and refresh periodically; `X-GitHub-Delivery` for uniqueness/replay protection and a redelivery keeps the original value. | https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks |
| **No automatic redelivery**: "GitHub does not automatically redeliver failed deliveries". Manual (UI) or API redelivery of deliveries from the past **3 days**; scheduled script pattern: list deliveries, filter `status != OK`, redeliver. | https://docs.github.com/en/webhooks/using-webhooks/handling-failed-webhook-deliveries ; https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/redelivering-webhooks |
| Redelivery APIs: repo `.../hooks/{hook_id}/deliveries` (+ `/{delivery_id}/attempts`), org `/orgs/{org}/hooks/{hook_id}/deliveries`, App `/app/hook/deliveries` (cursor paginated, **requires a JWT**). | OAS paths, 2026-10-13 |
| Types: repository, organisation, App, Marketplace, Sponsors. Repository and organisation webhooks: up to **20 webhooks per individual event type**; repository needs owner/admin, organisation needs organisation owner. Organisation webhooks receive events for all repositories the organisation owns (current docs do not say explicitly that later-created or private repositories are covered; the 2014 launch post https://github.blog/news-insights/product-news/introducing-organization-webhooks/ says org hooks deliver events from all of an organisation's repositories, and the `repository` event with action `created` is how new ones are announced; dated source, 2026-10-13). An App has a **single, built-in webhook**, "not subscribed to any events" by default, cannot be deleted, only deactivated. | https://docs.github.com/en/webhooks/types-of-webhooks |

### 5.2 Event payloads we use

Common: `repository` and `sender` always; `organization` and `installation` when applicable.

| Event | Availability, permission | Fields that matter | Caveats |
|---|---|---|---|
| `push` | repository, organisation, app; Contents: read | `ref`, `before`, `after`, `created`, `deleted`, `forced`, `base_ref` (nullable), `compare`, `commits[]` (each: `id`, `tree_id`, `distinct`, `message`, `timestamp`, `url`, `author`, `committer`, `added[]`, `removed[]`, `modified[]` - all required per octokit schema), `head_commit` (nullable), `pusher`, `repository.default_branch`, `repository.full_name` | `commits` holds **at most 2,048** commits (docs; octokit's description still says 20, stale); no event if more than **5,000 branches** pushed at once; no tag events if more than **3 tags** pushed at once; branch and tag deletions are `push` events with `deleted: true` and `head_commit` null; payload cap 25 MB. Pushes to forks are not detected. |
| `release` | repository, organisation, app; Contents: read | `action` (`created`,`deleted`,`edited`,`prereleased`,`published`,`released`,`unpublished`), `release` (octokit: `published_at` required string on `published`) | Docs do not explain the action semantics beyond the list; treat all as "refresh release". |
| `create` | same; Contents: read | `ref`, `ref_type`, `master_branch` (default branch name), `description`, `pusher_type` | Not sent when more than **3 tags** are created at once. |
| `delete` | same | `ref`, `ref_type`, `pusher_type` | Not sent when more than 3 tags are deleted at once. |
| `repository` | repository, organisation, app, business; Metadata: read | actions `archived`, `created`, `deleted`, `edited`, `privatized`, `publicized`, `renamed`, `transferred`, `unarchived`; for `edited` the octokit schema allows `changes.{description,default_branch,homepage,topics}.from` | The docs page has no payload table for this event; schema from octokit/webhooks (2026-10-13). Whether `repository.created/deleted` reach a repository-level webhook is not stated (they are meaningful for org and App webhooks). |
| `ping` | repository, organisation, app, business, marketplace | `zen`, `hook_id`, `hook` | Respond 2xx, do not enqueue sync. |
| `installation_repositories` | **app only**, delivered by default | `action` (`added`/`removed`), `installation`, `repository_selection` (`all`/`selected`), `repositories_added[]`, `repositories_removed[]` (each `id`, `node_id`, `name`, `full_name`, `private`), `requester`, `sender` | When selection changes from `all` to `selected`, `repositories_removed` is empty: re-list `GET /installation/repositories`. |
| `installation` | app only, default | actions `created`, `deleted`, `suspend`, `unsuspend`, `new_permissions_accepted`; `repositories[]` | `suspend` means stop using the installation token and show degraded status. |

Source for the table: https://docs.github.com/en/webhooks/webhook-events-and-payloads (anchors push/release/create/delete/repository/ping/installation_repositories) and https://github.com/octokit/webhooks/tree/main/payload-schemas/api.github.com (push, release, repository, installation_repositories schemas, raw files fetched 2026-10-13).

### 5.3 Org webhook vs repository webhook vs App webhook

| Criterion | Organisation webhook | GitHub App webhook (recommended) | Repository webhooks |
|---|---|---|---|
| Setup | Org owner creates it, one per org (20 per event cap) | Created with the App; one URL, one secret | One per repo: does not scale to 1,000 repos |
| Coverage | All repos the org owns; whether new repos are included is not stated in docs | Repos the installation covers; changes announced by `installation_repositories` | Single repo |
| Credentials | Separate from API credential (needs a PAT or App anyway) | Same App identity serves API tokens and webhook | n/a |
| Delivery management | UI plus org deliveries API | App delivery log plus `/app/hook/deliveries` (JWT) with cursor, redelivery | Per-repo APIs |
| Lifecycle events | None | `installation`, `installation_repositories` | None |
| Rate limit | n/a | Installation tokens scale to 12,500/h | n/a |

Recommendation: prefer the App webhook, support an organisation webhook for PAT mode (documented as the PAT-mode setup), never per-repo hooks. The receiver is identical for both: it must not depend on the `installation` field being present. Secret rotation: accept two secrets (current and previous) for a bounded overlap, because a webhook has exactly one secret at a time.

### 5.4 Receiver design implications (D-09, GR-20, GR-21)

1. `http.MaxBytesReader(w, r.Body, 25<<20)`; read the raw bytes; verify HMAC before any JSON decode; reject unsigned deliveries (refuse to start without a secret); require `Content-Type: application/json` (configure the hook as JSON; do not implement form-encoded).
2. Accept only `sha256=` signatures (reject `X-Hub-Signature` SHA-1 fallback).
3. Decode into minimal local structs (only fields we use), tolerant of unknown fields.
4. Enqueue a coalescing job keyed by repository id and return **202** immediately; 503 + `Retry-After` when the queue is saturated (but note GitHub will not retry it).
5. Dedupe on `X-GitHub-Delivery` after durable enqueue (so a failed attempt can be manually redelivered and accepted); bounded window of at least 3 days to match the redelivery window, or accept that redeliveries after the window are new.
6. Reconcile on startup and on a timer, because lost deliveries are not recoverable otherwise; optionally run a "missed deliveries" job through `/app/hook/deliveries` (JWT) every N minutes and redeliver `status != OK` within 3 days (needs the App private key at runtime; keep optional).
7. Ping returns 200 without side effects; unknown events return 202 and are counted, not errors.

## 6. Go-side pitfalls (versions from `proxy.golang.org`, repos read 2026-10-13)

| Library | Version and date | Facts | Verdict |
|---|---|---|---|
| `google/go-github/v92` | v92.0.0 (2026-09-14); recent majors v87 (05-18), v88 (05-21), v89 (07-06), v90 (08-04), v91 (09-03); `go 1.26.0` in go.mod; BSD-3-Clause style licence (LICENSE read 2026-10-13) | `NewClient(opts...)` options pattern since v87 (`WithHTTPClient`, `WithTransport`, `WithAuthToken`, `WithEnterpriseURLs`, `WithDisableRateLimitCheck`, `WithMaxSecondaryRateLimitRetryAfterDuration`); default API version `2022-11-28`; `Response.NextPage`; `ListOptions{PerPage}`; iterators `ListByOrgIter`, `ListCommitsIter`, `ListReleasesIter`, `ListTagsIter` (`iter.Seq2[*T, error]`, copy `opts`, yield errors in-band, **do not expose per-page `Response`**, so no per-page ETag/rate headers); built-in local rate-limit pre-check returns `*RateLimitError` without a network call while reset is in the future; `AbuseRateLimitError` for secondary with `RetryAfter`; **`CheckResponse` treats every non-2xx as error, including 304** (no `StatusNotModified` in any non-test file of the v92.0.0 tag, which I downloaded and grepped; its own `repos_commits_test.go` asserts that a 304 returns an error), so a conditional request needs a caching transport that synthesises a 200 | Usable, but a major version every 2-6 weeks (renames such as `Edit*` to `Update*`) makes it a maintenance tax; confine behind the adapter or skip |
| go-github webhook helpers | same module | `ValidatePayload` falls back from `X-Hub-Signature-256` to SHA-1 header; `messageMAC` accepts `sha1`, `sha256`, `sha512` prefixes; **validation is skipped if the secret is empty and no signature header is present**; body limit 25 MiB (`maxPayloadSize`); `ParseWebHook` maps event names to large typed structs | Do not use for verification (GR-20/D-09); own 30-line verifier with `hmac.Equal` over raw bytes |
| `bradleyfalzon/ghinstallation/v2` | v2.19.0 (2026-06-11), Apache-2.0 | `Transport` caches the installation token and refreshes one minute before expiry (`ExpiresAt.Add(-time.Minute)`), sets `Authorization: token ...`; `AppsTransport` JWT with 2-minute lifetime, `Bearer`; pluggable `Signer` (KMS); `BaseURL` for GHES; go.mod requires **go-github v88** and **golang-jwt/jwt/v4**, so it drags an old go-github into the module graph | Maintained; acceptable, but a 100-line own implementation on `golang-jwt/jwt/v5` (v5.3.1, 2026-01-28) avoids the dependency drag |
| `jferrl/go-githubauth` | v1.9.1 (2026-09-19) | go.mod: `golang-jwt/jwt/v5`, `golang.org/x/oauth2`; client-id or app-id | Lean alternative |
| `gofri/go-github-ratelimit/v2` | v2.0.2 (2025-03-12); MIT; 69 stars | Primary limit: blocks and returns error; secondary: sleeps and retries; callbacks | Last release 19 months old, overlaps go-github's built-ins; own small transport preferred |
| `gofri/go-github-pagination` | v1.0.1 (2025-03-12) | RoundTripper that auto-paginates; README example still imports go-github v58 | Skip; superseded by `*Iter` methods and by own loop |
| `bored-engineer/github-conditional-http-transport` | v0.0.8 (2026-10-03); MIT; zero dependencies; go 1.26 | RoundTripper implementing the Authorization-aware ETag recompute; backends bbolt, memory, Pebble, Redis, S3 (separate packages); pre-1.0, single author, relies on undocumented behaviour | Reference for the optional recompute optimisation; do not adopt as a dependency without a pinned test |
| `gregjones/httpcache` | last 2019 | unmaintained | No |
| `bartventer/httpcache` | v0.14.0 (2026-08-11) | RFC 9111 cache; go-github README suggests it | Generic HTTP caching is not what we need (credential-keyed validators in our own store) |
| `sony/gobreaker/v2` v2.4.0 (2026-01-01); `golang.org/x/time` v0.16.0 (2026-08-19); `cenkalti/backoff/v5` v5.0.3 (2025-07-23) | | matches 03's list | fine |
| `go-playground/webhooks/v6` | v6.4.0 (2024-07-30) | stale | No |
| `palantir/go-githubapp` | v0.48.0 (2026-09-17) | opinionated App framework (webhook routing, client creation) | Over-scoped for us |

Pagination helper summary: go-github's page loop is `opt.Page = resp.NextPage` until `NextPage == 0`; for our own client, parse `Link` per RFC 8288 and follow `rel="next"` verbatim (do not synthesise `page=N+1`), with a hard page-count ceiling (for example 50) to protect against loops.

Own-client stack recommendation: `http.Client{Timeout}` with a `RoundTripper` chain `otelhttp -> rate/budget gate -> conditional (credential-keyed validators) -> auth (token source) -> versions/headers -> http.DefaultTransport`; a `TokenSource` interface with `AppInstallationSource` (JWT RS256 via jwt/v5, cache, refresh at expiry minus 1-5 minutes, singleflight) and `StaticTokenSource` (PAT); typed errors (`ErrNotFound`, `RateLimitedError{ResetAt,Secondary}`, `ErrTransient`); decode only the fields we use.

## 7. Request-budget model for 1,000 repositories

### 7.1 Inputs and assumptions (explicit)

- N = 1,000 tracked repositories (non-archived, non-fork); the org list therefore needs ceil(1,000/100) = **10** pages. Budget per Q-09: 5,000 REST req/h (PAT or smallest App); App at this size: 12,500/h REST plus a separate 5,000..12,500 GraphQL points/h.
- Churn per hour (assumptions, scaled from "under 2,500 commits/hour" in the product statement): D = 100 repositories pushed to; K = 10 pushes that change the README; Rel = 2 releases; 60% of repositories have no release (their `releases/latest` is 404); 5% of repositories use a non-Markdown README (HTML bridge); 10% have no `README.md` at the expected name (GraphQL fallback).
- REST design "R": per repo README raw + commits (`per_page=10`) + latest release; list refreshed every 30 min; README and release polled hourly; commits polled every 5 min; ETags used.
- GraphQL hybrid "H": GraphQL sweeps as in 3.2, push and release webhooks applied from payload, README text via REST or `nodes()` on change only.
- Serial request time assumption: 150 ms per REST call (for run-time estimates only).
- REST-only fallback for head detection (GHES without usable GraphQL, or a GraphQL outage): `GET /repos/{o}/{r}/commits/{default_branch}` with `Accept: application/vnd.github.sha` and `If-None-Match: "<stored head sha>"` (A15): 1,000 requests per full pass (about 2.5 min serial), counted only if the 304 is not free; token-independent because the validator is the SHA; README and release then need their own checks (README ETag equals the blob SHA, A5, so `If-None-Match: "<readme blob sha>"` is worth testing with authentication).

### 7.2 Cold start

| Step | R (REST only) | H (hybrid) |
|---|---|---|
| Listing | 10 | 20 GraphQL pages x 2 pts = 40 pts (includes metadata, topics, licence, last 10 commits, latest release, issue and PR counts) |
| README | 1,000 (+50 HTML bridge for non-Markdown = 1,050) | 20 `nodes()` queries x 1 pt = 20 pts for text of the 90% with `README.md`; REST `/readme` for the other 10% = 100 |
| Commits | 1,000 | 0 (in the sweep) |
| Latest release | 1,000 (+ 600 tags fallback for the 60% without release) = 1,600 | 0 (in the sweep; tags via `refs` +20 pts if wanted) |
| **Total** | 10 + 1,050 + 1,000 + 1,600 = **3,660 REST** (73% of 5,000, 29% of 12,500); about 9 minutes serial at 150 ms | 40 + 20 + 20 (optional tags) = **80 GraphQL pts + 100 REST** (1.6% of the 5,000 GraphQL bucket; 2% of the REST bucket); 60 pts without the tags option |

03 (section 2) estimated "about 3,000 calls at 1,000 repos"; the corrected REST-only figure is 3,010 without the tags fallback and 3,660 with it.

### 7.3 Steady state (per hour, stable token and healthy webhooks)

| Component | R with PAT (stable token) | R with App token (hourly rotation) | H |
|---|---|---|---|
| Listing | 2 sweeps x 10 = 20 (list ETag changes whenever any `pushed_at` changes, so counted) | 20 | 24 (12 incremental `PUSHED_AT`-ordered pages x 2 pts) + 80 (2 full sweeps x 40 pts) = 104 GraphQL pts |
| Commits | 12 polls x 1,000 = 12,000 conditional requests (about 3.3 req/s, 200 req/min, within 900 pts/min); counted only when changed: D = 100 | 100 + 1,000 (first poll after rotation is a 200) = 1,100 | 0 (push payload carries commits; sweep corrects) |
| README | 1,000 conditional; counted K = 10 | 10 + 1,000 = 1,010 | K = 10 REST (conditional when token stable) |
| Release | 1,000 conditional; counted Rel = 2 plus 404s for the 600 repos without release = 602 (a 404 carries no ETag, observed unauthenticated in appendix A9) | 602 + 400 (those with releases re-fetched at rotation) = 1,002 | 0 (release payload; sweep corrects) |
| **Counted total** | 20 + 100 + 10 + 602 = **732** (14.6% of 5,000); **232** if 404-only repos are re-polled every 6 h instead of hourly | 20 + 1,100 + 1,010 + 1,002 = **3,132** (62.6% of 5,000; 25% of 12,500) | **about 10-30 REST and about 104 GraphQL pts** |

Without webhooks (polling only): H raises the incremental sweep to once per minute (60 x 2 = 120 pts) plus the 80-point reconcile: **200 pts/h (4%)**, with push-to-visible latency of about 1 minute. R without webhooks keeps the same counted totals but needs 12,000 conditional requests/h to reach 5-minute freshness; with an App token every hour's rotation costs 3,000 more.

### 7.4 After restart with persisted state

| Strategy | Counted calls |
|---|---|
| R, PAT, ETags persisted (credential-keyed): conditional replay of 3,000 + 10 list | only changed items: about D + K + Rel per hour of downtime = 112 for 1 h, plus 10 list, plus 600 for the repos whose `releases/latest` is 404 = **about 722**; the 3,010 requests take about 7.5 min serial |
| R, App token (persisted ETags unusable: new token) | **3,010 to 3,660**, same as cold start (no benefit); with the ETag-recompute optimisation (bodies' SHA-256 persisted) about 722 |
| H (persisted oids: default-branch head, README blob oid, release id, topics, pushedAt) | 20 GraphQL pages x 2 pts = **40 pts** to reconcile everything, then REST only for changed READMEs: K x downtime hours = 10 for 1 h, 100 for a 10 h outage. No dependency on ETags or token identity. |

### 7.5 Scheduling rules that follow

1. Budget gate in the adapter: `remaining` below 10% pauses background refreshes (webhook-triggered jobs and operator actions still run) until `reset`; expose gauges for REST and GraphQL separately.
2. Priority order on cold start: dashboard-visible repositories first (most recently pushed), documentation next, release data last; readiness (`/readyz`) after the first full GraphQL sweep, not after README fetches.
3. Concurrency: REST at most 4 in flight (docs ask for serial; secondary limit 900 pts/min and 100 concurrent); GraphQL 1-2 in flight (CPU secondary limit). Jitter all periodic jobs.
4. Never poll a resource more often than it can change visibly; README hourly is generous when `push` payloads list changed paths.
5. A `304` from our own conditional cache must still update `fetchedAt` (freshness is about when we verified, not when it changed).

## 8. Adapter, sync and webhook design inputs (consolidated)

- `internal/github` interfaces (consumer-side): `RepoCatalogSource` (list repos with metadata), `ChangeSweeper` (incremental `PUSHED_AT` sweep returning per-repo head oid, README oid, release, topics, issue and PR counts, last commits), `ReadmeFetcher` (raw, size-capped, typed states), `CommitsFetcher` (fallback and since-based), `ReleaseFetcher`, `TreeFetcher` (D-15), `WebhookVerifier`/payload types, `RateLimitState`.
- All data items carry `fetchedAt`, `validators` (credential id, ETag, body hash), `status`, `lastError` (GR-26).
- Configuration keys to add: `github.api_version`, `github.auth.mode` (`app`|`pat`), `github.app.{id|client_id, installation_id, private_key_file}`, `github.token_file`, `github.base_url` (+ `graphql_url`), `github.rest_concurrency`, `github.graphql_page_size`, `github.readme_max_bytes`, `webhook.secret_file`, `webhook.previous_secret_file`.
- Health: `readyz` depends on last successful sweep age and, for PAT mode, token days-to-expiry above a threshold; `/status` shows REST and GraphQL buckets, reset times, last secondary-limit event, and oldest un-reconciled item.

## 9. Corrections to the decision record and earlier assumptions

1. GraphQL is more than a way to "cut request counts for repo discovery": it should be the **bulk read path** (listing, metadata, commits, release, counts), about 20x fewer counted calls (3,660 REST versus about 180 units, section 7); `shurcooL/githubv4` has no tagged release (latest pseudo-version 2026-02-09), so prefer a hand-written document or `genqlient` v0.8.1.
2. Do not adopt `gofri/go-github-ratelimit`: last release v2.0.2 on 2025-03-12, and go-github now has built-in primary pre-check and secondary `Retry-After` handling with a configurable cap (`WithMaxSecondaryRateLimitRetryAfterDuration`); also `bradleyfalzon/ghinstallation` pulls `go-github v88` and `jwt/v4` transitively. Prefer an own small transport and `golang-jwt/jwt/v5` (or accept ghinstallation knowingly).
3. The estimate "about 3,000 calls at 1,000 repos" is the lower bound for REST (3,010); with the tags fallback it is about 3,660 (73% of 5,000/h).
4. `04` Q-09 "5,000 req/h GitHub budget": true for PATs and the smallest Apps; an App installed on 1,000 repositories has 12,500 REST req/h (formula `min(12,500, 5,000 + 50 x (repos - 20))`) and a separate GraphQL bucket; record both budgets.
5. `04` Q-13 ("ETag/conditional request transport") and GR-25 (conditional requests): correct in principle, but with App auth the ETag is credential-bound (docs issue 34689; third-party README), so persisted ETags do not survive hourly token rotation or restarts. Cheap restarts need content identifiers (blob and commit oids via GraphQL), not ETags.
6. API version header: there are now two supported versions and the implicit default will move; send the header, default `2026-03-10`.
7. Rate-limit "pause until reset": primary exhaustion is 403 (429 also possible) with `x-ratelimit-remaining: 0` and no `Retry-After` (observed 2026-10-13); secondary limits use `Retry-After`, else wait at least 60 s. `GET /rate_limit` `rate` is documented as removed in `2026-03-10` (a live unauthenticated call with that version still returned it, A12); use `resources.core` and `resources.graphql`.
8. `04` D-15 "evaluate the GitHub dependency-graph SBOM endpoint": the synchronous endpoint is removed on 2026-11-13; only generate-report/fetch-report remains (reports kept up to a week; separate `dependency_sbom` rate-limit category). Git Trees recursive is capped at 100,000 entries or 7 MB.
9. README endpoint (GR-37): the endpoint returns a single `file` object (never a directory array); 1 MB+ behaviour is documented only for the generic Contents endpoint (`encoding: "none"` for 1-100 MB with the object media type); a "base64 only" assumption must be replaced by raw media type with a byte cap.
10. `04` Q-24 events `push, release, create, repository, ping`: add nothing mandatory, but note `installation` and `installation_repositories` are delivered automatically to an App webhook and must be handled (no subscription possible), and `create` is redundant with `push` for tags (tag creation is a `push` with `ref: refs/tags/...`); both events are not sent when more than 3 tags are pushed together, so release/tag completeness comes from the sweep.
11. Paging and signatures: confirmed that GitHub caps `per_page` at 100 (larger values are silently reduced, so every list must paginate, GR-26) and that `X-Hub-Signature` is SHA-1 while `X-Hub-Signature-256` is the recommended header (GR-20).
12. Latest tag: the unversioned `tags` endpoint has no documented ordering, so "latest tag" via REST needs client-side sort of up to 100 tags.

## 10. Deviations proposed (to be argued in the architecture, per 04 section 3 rule)

- D-04 addition: the `github` adapter owns two read paths (GraphQL sweep, REST on demand) and a credential-keyed validator store; "ETag/conditional requests" in D-04 is kept but demoted to an optimisation.
- Q-24: keep the five events; add that `installation`/`installation_repositories` are auto-delivered in App mode. Optional removal of `create` (redundant with `push` for tags) if the product shows tags only via releases.
- D-02: persist per-repo content identifiers (default-branch head oid, README blob oid, latest release id, topics hash, pushedAt) in the `Store` so restart reconcile needs no ETags.

## 11. Risks

1. **ETag/credential coupling** is third-party and undocumented (docs issue closed as not planned; README from Feb 2025). If wrong in our favour, ETags work across tokens and R becomes cheaper; if right, R+App is expensive. H is robust either way.
2. **GraphQL timeouts and node limits**: a 50-repo page with `history(first:10)` and five connections may exceed 10 s on slow days; timeouts also deduct points. Mitigation: adaptive page size, dry-run cost checks, spike before committing the page size.
3. **Undisclosed secondary limits**: "no way to check secondary limit status"; bans are possible when ignoring `Retry-After`. Mitigation: low concurrency, strict honouring of headers, circuit breaker on repeated 403/429.
4. **go-github churn** (six majors in four months, Go 1.26 floor): if adopted, Renovate noise and import-path rewrites; own client mitigates but adds code to maintain (about 600 lines estimate, not measured).
5. **Webhook loss**: no automatic redelivery; deliveries older than 3 days cannot be redelivered; 25 MB payloads silently not delivered; tag-heavy pushes produce no events. Mitigation: reconcile sweeps, `/app/hook/deliveries` job.
6. **Single webhook secret per webhook**: rotation requires dual-secret acceptance in the receiver.
7. **Fine-grained PAT fallback**: silent 30-day default expiry, single org, org approval gate; surfaces as 404/403 that look like missing repositories (docs: 404 used for private resources).
8. **API version drift**: unversioned calls will move to a newer default; GHES may support only older versions; `Deprecation`/`Sunset` headers must be logged; unauthenticated CDN-cached responses ignored the requested version in my probe (A11), so verify `x-github-api-version-selected` on responses.
9. **`open_issues_count` semantics** rest on community evidence only; GraphQL counts need the Issues/Pull requests read permission for private repositories (unverified).
10. **SBOM and dependency-graph limits** (D-15) are changing now (2026-11-13 removal); do not build on the synchronous endpoint.
11. **GHES skew**: unknown default rate limits, possible lack of `2026-03-10`, GraphQL schema differences, App webhook and IP allow-list behaviour; needs a GHES test instance or documented "best effort".
12. **`pushed_at` semantics**: it updates on any push (any branch, tags), not only the default branch; so the incremental sweep can over-trigger for branch-heavy repositories and the sweep logic must compare default-branch head oid before refetching.

## 12. Unverified (not confirmed from a primary source, or not reproduced)

- That GitHub's ETag still depends on `Authorization` in 2026-10 (only the `Accept` component of the third-party model was reproduced, A2), whether `Last-Modified`/`If-Modified-Since` is token-independent, whether authenticated 304s are free (documented, not observed), and whether authenticated responses are `private` and keyed by `Authorization` (no token was provisioned to me; only unauthenticated probes were run).
- Whether an authenticated 404 carries an ETag (unauthenticated: none, A9).
- That the 2026-03-10 removal of the `rate` object is effective (A12 shows it still present); the unauthenticated CDN version-mix (A11) on authenticated traffic.
- The GraphQL cost and latency of the proposed query (docs formula computed by hand; alias and `object` handling not described); maximum ids for `nodes(ids:)` (assumed 50 here, commonly 100); `Blob.text` truncation threshold.
- Whether `GET /orgs/{org}/repos` with an installation token returns exactly the installation's repositories; the full field set returned by `/installation/repositories` beyond the schema reference.
- README endpoint size behaviour (documented only for the generic Contents endpoint); behaviour of `/readme` on repos where the README is a symlink or lives in `.github/` or `docs/` (the docs say "preferred README" only); the `/readme/{dir}` and empty-repository (409 on commits) cases were not exercised.
- Whether organisation webhooks automatically cover repositories created later (only a 2014 GitHub blog post says all repositories; current docs silent); whether `repository.created`/`deleted` reach repository-level hooks.
- GHES: rate-limit defaults (only a third-party README says disabled by default) and which API versions GHES 3.22 actually serves; GraphQL schema equivalence on GHES.
- The `github-authentication-token-expiration` header is known from go-github source only; no GitHub docs page confirming it was read.
- Exact retention and rate of `dependency_sbom` limits; permission needed for the SBOM report endpoints.
- Required permissions for GraphQL `issues`/`pullRequests` connections on private repositories with installation tokens.

## 13. Source index (verified 2026-10-13)

GitHub docs: https://docs.github.com/en/rest/about-the-rest-api/api-versions ; https://docs.github.com/en/rest/about-the-rest-api/breaking-changes ; https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api ; https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api ; https://docs.github.com/en/rest/using-the-rest-api/using-pagination-in-the-rest-api ; https://docs.github.com/en/rest/using-the-rest-api/getting-started-with-the-rest-api ; https://docs.github.com/en/rest/using-the-rest-api/troubleshooting-the-rest-api ; https://docs.github.com/en/rest/repos/repos ; https://docs.github.com/en/rest/repos/contents ; https://docs.github.com/en/rest/commits/commits ; https://docs.github.com/en/rest/releases/releases ; https://docs.github.com/en/rest/issues/issues ; https://docs.github.com/en/rest/search/search ; https://docs.github.com/en/rest/git/trees ; https://docs.github.com/en/rest/dependency-graph/sboms ; https://docs.github.com/en/rest/activity/events ; https://docs.github.com/en/rest/apps/installations ; https://docs.github.com/en/rest/overview/permissions-required-for-github-apps ; https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api ; https://docs.github.com/en/graphql/reference/repos ; https://docs.github.com/public/fpt/schema.docs.graphql ; https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app ; https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app ; https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/best-practices-for-creating-a-github-app ; https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/deciding-when-to-build-a-github-app ; https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens ; https://docs.github.com/en/webhooks/webhook-events-and-payloads ; https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries ; https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks ; https://docs.github.com/en/webhooks/using-webhooks/handling-failed-webhook-deliveries ; https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/redelivering-webhooks ; https://docs.github.com/en/webhooks/types-of-webhooks ; https://docs.github.com/en/webhooks/about-webhooks ; https://docs.github.com/en/repositories/creating-and-managing-repositories/renaming-a-repository ; GHES: https://docs.github.com/en/enterprise-server@latest/admin/configuring-settings/configuring-user-applications-for-your-enterprise/configuring-rate-limits ; https://docs.github.com/en/enterprise-server@latest/rest/about-the-rest-api/api-versions ; https://docs.github.com/en/enterprise-server@latest/rest/overview/resources-in-the-rest-api.

Machine-readable: https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json (info.version 1.1.4); https://raw.githubusercontent.com/octokit/webhooks/main/payload-schemas/api.github.com/{push/event,release/published,repository/edited,installation_repositories/added}.schema.json and `common/commit.schema.json`.

Other: https://github.blog/changelog/2026-05-12-synchronous-sbom-api-deprecated/ ; https://github.com/github/docs/issues/34689 ; https://github.com/orgs/community/discussions/102620 ; https://github.com/bored-engineer/github-conditional-http-transport ; https://github.com/google/go-github (releases page and raw `README.md`, `github/github.go`, `github/messages.go`, `github/github-iterators.go`, `github/repos.go`, `go.mod`) ; https://github.com/bradleyfalzon/ghinstallation (raw `transport.go`, `appsTransport.go`, `go.mod`, branch `master`) ; https://github.com/gofri/go-github-ratelimit ; https://github.com/gofri/go-github-pagination ; https://github.com/jferrl/go-githubauth (raw go.mod) ; https://proxy.golang.org (module `@latest` queries, 2026-10-13).

## Appendix A. Live probes of api.github.com (unauthenticated, 2026-10-13)

Method: `curl` with `User-Agent: scp-research-probe`, `Accept: application/vnd.github+json`, no `Authorization`, from one IP, 2026-10-13 21:48-21:55 UTC, after the unauthenticated quota (60/h per IP) had been exhausted by an earlier first probe (the first probe, at 21:00 UTC, itself returned the 403 recorded in 2.2). About 24 requests in total. No credential of the user was used. Everything below is an unauthenticated observation; authenticated behaviour (ETag dependence on `Authorization`, free 304s, `private` cache-control) was **not** observed.

| # | Probe | Observation |
|---|---|---|
| A1 | `GET /orgs/cli/repos?per_page=2&sort=full_name&direction=asc`, version `2026-03-10` | 200; `x-github-api-version-selected: 2026-03-10`; `etag: W/"77f5d5..."`; `link` has `rel="next"` and `rel="last"` using `organizations/{id}/repos?...&page=N` (page-based; GitHub rewrites the org name to its numeric id); element keys include `default_branch`, `archived`, `fork`, `disabled`, `visibility`, `topics`, `license` (object with `spdx_id`), `open_issues`, `open_issues_count`, `pushed_at`, `description`, `size`, `language`, `html_url`, `has_issues`, `permissions`, `custom_properties`; **no** `has_downloads` (consistent with the 2026-03-10 breaking-change list). So the listed fields are present in practice even though the OAS marks them optional. |
| A2 | ETag algorithm check on A1 | `sha256("application/vnd.github+json" + ":" + body)` equals the ETag value exactly (weak ETag `W/"<hex>"`); `sha256(body)` alone does not. This reproduces, for the `Accept` component and without `Authorization`, the third-party reverse engineering (`Accept:Authorization:Cookie:` prefix joined to the body, only if present). The `Authorization` component remains unreproduced. |
| A3 | A1 repeated with `If-None-Match` | 304; `x-ratelimit-remaining` went 59 to 58: **an unauthenticated 304 does count** against the limit (docs: free only when authorised). |
| A4 | `GET /repos/cli/cli/commits?per_page=1` | `etag` (weak), `last-modified: Tue, 13 Oct 2026 18:15:23 GMT`; `If-Modified-Since: <that value>` gave 304 (and counted, remaining 57 to 56). `link` `rel=last` page 12264 (so commit counts are derivable from `per_page=1` plus `last`, at the cost of one request). |
| A5 | `GET /repos/cli/cli/readme` with `Accept: application/vnd.github.raw+json` | 200, `content-type: application/vnd.github.raw+json`, body 6,284 bytes (the raw README), **strong** `etag: "e2d5ae54e7d0..."` which equals the README blob SHA (`sha` field in the JSON form), `last-modified` present. |
| A6 | Same with `Accept: application/vnd.github.html+json` | 200, `application/vnd.github.html+json`, 22,028 bytes (3.5x the raw size), wrapper `<div id="readme" class="md" data-path="README.md"><article class="markdown-body entry-content container-lg" ...>`, heading anchors with `id="user-content-..."` and an inline `<svg>` octicon per heading; **relative links stay relative** (`href="docs/install_macos.md"`, `href=".github/CONTRIBUTING.md"`), absolute images point to `user-images.githubusercontent.com`; same strong ETag (the blob SHA). So link/image rewriting is still our job and the HTML must be sanitised. |
| A7 | Same with `Accept: application/vnd.github+json` | 200, weak `W/"e2d5ae54..."`; JSON `{type:"file", encoding:"base64", size:6284, name:"README.md", path:"README.md", sha, download_url:"https://raw.githubusercontent.com/cli/cli/trunk/README.md"}` (the OAS text calling raw "the default" applies only when no specific media type is requested; with `application/vnd.github+json` you get the base64 object). The README ETag being the blob SHA suggests README conditional requests may be less credential-dependent than list endpoints, but that is observed only without authentication. |
| A8 | `GET /repos/cli/cli/releases/latest` | 200, weak ETag and `last-modified` (2026-09-30), `tag_name v2.102.0`, `published_at 2026-09-30T02:40:02Z` and `created_at 2026-09-30T02:18:02Z` (created_at is the commit date, earlier than published_at). |
| A9 | `GET /repos/octocat/Hello-World/releases/latest` (no release) | **404 with no `ETag`, no `Last-Modified` and no `Cache-Control`**; it consumed a request (remaining 52 to 51). Confirms that "no release" repositories cannot be polled conditionally and cost one request per poll. Body `{"message":"Not Found","documentation_url":"https://docs.github.com/rest/releases/releases#get-the-latest-release","status":"404"}`. |
| A10 | Unsupported version `1999-01-01` | **400 Bad Request** with message "The version you specified in the "X-GitHub-API-Version" request header, "1999-01-01", is not a supported version. The following versions are currently supported: "2026-03-10" (most recent) and "2022-11-28"." Useful for GHES version negotiation (parse the list, fall back to a listed version). |
| A11 | `GET /repos/cli/cli` and `/repositories/{id}`, `/repos/cli/cli/topics`, with `Cache-Control: no-cache` and version `2026-03-10` | `x-github-api-version-selected: 2026-03-10`; `has_downloads` absent (breaking change observed). Without `Cache-Control: no-cache`, two earlier requests with version `2026-03-10` for `/repos/cli/cli` returned `x-github-api-version-selected: 2022-11-28` and `has_downloads` present, even after the 60 s max-age: **unauthenticated responses are `cache-control: public, max-age=60, s-maxage=60` and `vary: Accept, Accept-Encoding, X-Requested-With` (no `X-GitHub-Api-Version` in Vary)**, so a shared cache can serve a response generated for the other version. Likely irrelevant to authenticated (private) responses but not observed; the adapter should check `x-github-api-version-selected` on responses and log a mismatch. |
| A12 | `GET /rate_limit` with version `2026-03-10` | `cache-control: no-cache`, no `x-github-api-version-selected` header, and the body still contained the deprecated `rate` object next to `resources` (keys for an unauthenticated caller: `code_search`, `core`, `graphql` (limit 0), `integration_manifest`, `search`). The docs say `rate` is removed in `2026-03-10`; not observed here. Read `resources.core` regardless. |
| A13 | `GET /meta` | keys `actions, actions_macos, api, codespaces, commit_signing_keys, copilot, domains, git, github_enterprise_importer, hooks, importer, packages, pages, ssh_key_fingerprints, ssh_keys, verifiable_password_authentication, web`; `hooks` holds 9 entries, `api` 26 (the webhook source-IP allow-list is `hooks`); no API-versions list. |
| A14 | `GET /repos/cli/cli/commits?per_page=100` | `link` `rel="next"` and `rel="last"` (page 123) on `repositories/{id}/commits?per_page=100&page=N`. |
| A15 | `GET /repos/cli/cli/commits/trunk` with `Accept: application/vnd.github.sha`, then `If-None-Match: "<sha>"` | First: 200, body is the 40-character SHA, **strong `etag: "d6b82b12e67d..."` equal to the SHA**, `last-modified`. Second: **304** (unauthenticated: counted, remaining 45 to 44). Confirms the documented "SHA as ETag" head check works and that its validator is the commit SHA itself (token-independent by construction). Whether the 304 is free when authenticated is not observed. |
| A16 | Primary limit exhaustion (first probe, 21:00 UTC) | HTTP **403**, `x-ratelimit-limit: 60`, `x-ratelimit-remaining: 0`, `x-ratelimit-used: 60`, `x-ratelimit-resource: core`, `x-ratelimit-reset` present, **no `Retry-After`**, JSON message "API rate limit exceeded for <ip>"; response header `access-control-expose-headers` lists `Retry-After`, `Deprecation`, `Sunset`, `X-Poll-Interval`. |

