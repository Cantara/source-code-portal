# GitHub integration
Status: v1 design note, 2026-10-14. Binding order: ../plan.md > this note > ../reference/.

## Scope (v1)
`internal/github` reads repositories of one or more GitHub organisations for the sync jobs: App auth with a token fallback, REST reads
through `google/go-github` v92, a small conditional-request and rate-observer transport, error classes, and the fake server
`internal/github/githubtest`. It returns `internal/snapshot` value types. Admission and the removal rule are in `catalog.md`; the rate
budget, retries and pauses in `sync-snapshot-store.md`; webhook verification and dedupe in `http-api.md` behaviour 4. This note lists the
webhook events v1 reacts to and the `sync` event each becomes.

## Decisions (bullets, each with a one-line reason)
- go-github v92 for REST, ghinstallation v2.19 for App auth (plan.md 4): mature, widely used libraries keep the client small. ghinstallation pulls go-github v88 and jwt/v4 in as indirect modules; accepted.
- REST covers every v1 read. At 1,000 repositories a cold start costs about 4,500 calls: 36% of an App installation's 12,500/h (research 03 2.2, 2026-10-13).
- Commit freshness: webhooks, plus the 5-minute org listing sorted by `pushed_at` (about 10 calls per org per sweep); a repository whose `pushed_at` changed gets `sync.EventPush`; `commits.ttl` 2 h is the safety net.
- One listing call for both auth modes, `GET /orgs/{org}/repos`: one code path, and every item carries `visibility`, topics, licence, default branch, archived, fork, `pushed_at` and `open_issues_count`.
- Content SHAs (head commit, README blob, docs blobs, descriptor blob) are the persisted validators. REST ETags are tied to the `Authorization` header and App tokens rotate hourly (research 03 2.4).
- ETags live in memory, keyed by the hashed `Authorization` value. Within one token lifetime an authorised 304 costs no rate limit (docs, 2026-10-13).
- The head probe is `GetCommitSHA1` with `If-None-Match: "<sha>"`. Its validator is the SHA itself, so it holds across tokens (research 03 2.4, A15).
- One go-github client per (source, installation), so each installation keeps its own rate bucket (research 09 4.3).
- API version: go-github's default `2022-11-28`, sent explicitly, on github.com and GHES alike: supported until 2028-03-10 and present in every GHES release.
- The issue count is `open_issues_count`, labelled "open issues and PRs": it includes pull requests and comes with the listing.
- `github` classifies errors and reports rate state; `sync` decides when to call again: one place owns retries and pauses.
- Every start runs a full listing, which repairs any webhook delivery missed while the process was down (research 03 5.1).

## Shapes (minimal Go types/interfaces or table sketches)
```go
package github

type Kind uint8 // NotFound | RateLimited | Transient | Auth | Permanent
type Error struct { Kind Kind; RetryAt time.Time; Code string; Err error } // RetryAt from Retry-After or reset; zero when absent
type TreeEntry struct { Path, SHA string; Size int64 }
type File struct { Path, SHA string; Body []byte; TooLarge bool }
type Client struct{ /* sources by id */ }
func (c *Client) Source(id string) *Source
func (c *Client) Remaining(source, owner string) (frac float64, reset time.Time) // sync.Budget
// Source is one configured host + credential; methods map 1:1 to the calls below.
func (s *Source) ListRepos(ctx context.Context, owner string) ([]snapshot.Repo, error)      // complete listing, newest push first, or an error
func (s *Source) Repo(ctx context.Context, owner, name string) (snapshot.Repo, error)
func (s *Source) Head(ctx context.Context, owner, name, branch, known string) (sha string, changed bool, err error)
func (s *Source) Commits(ctx context.Context, owner, name, branch string, n int) ([]snapshot.Commit, error)
func (s *Source) Tree(ctx context.Context, owner, name, ref string) (entries []TreeEntry, truncated bool, err error)
func (s *Source) Readme(ctx context.Context, owner, name string) (File, error)              // NotFound = no README
func (s *Source) Blob(ctx context.Context, owner, name, path, sha string) (File, error)
func (s *Source) LatestRelease(ctx context.Context, owner, name string) (*snapshot.Release, error) // nil, nil = no release
```
Consumers declare their own narrow interfaces over these methods (plan.md 3 rule 2).

| Need | go-github v92 call (re-check names on import) | Auth | Endpoint |
|---|---|---|---|
| installations | `Apps.ListInstallations` | App JWT | `GET /app/installations` |
| repos | `Repositories.ListByOrg` (`type=all`, `sort=pushed`, `direction=desc`, `per_page=100`) | either | `GET /orgs/{org}/repos` |
| one repo | `Repositories.Get` | either | `GET /repos/{o}/{r}` |
| head probe | `Repositories.GetCommitSHA1` | either | `GET /repos/{o}/{r}/commits/{branch}` (sha media type) |
| commits | `Repositories.ListCommits` (`SHA: default branch`, `PerPage: n`) | either | `GET /repos/{o}/{r}/commits` |
| tree | `Git.GetTree(ref, recursive=true)` | either | `GET /repos/{o}/{r}/git/trees/{ref}` |
| README | `Repositories.GetReadme` | either | `GET /repos/{o}/{r}/readme` |
| docs/, `scp.yaml` | `Git.GetBlobRaw` | either | `GET /repos/{o}/{r}/git/blobs/{sha}` |
| release | `Repositories.GetLatestRelease` | either | `GET /repos/{o}/{r}/releases/latest` |

Transport chain, outermost first: go-github, then `ghinstallation.Transport` (sets the token), then `scpTransport` (headers, conditional cache,
rate observer), then `http.Transport`. `scpTransport` sits below auth so it can read `Authorization`.

## Behaviour (numbered essentials and the edge cases that matter)
1. **Credentials.** In App mode the key file is read once. One `ghinstallation.NewAppsTransport` lists installations, and one transport per allowed installation comes from `NewFromAppsTransport`. ghinstallation caches the token and refreshes it a minute before expiry. Tokens stay in memory and logs redact them. In token mode a static token (fine-grained PAT, single owner, or classic PAT for several organisations) goes in `Authorization`. Use a dedicated machine account, so the portal has its own 5,000/h bucket.
2. **Installations.** Installations are listed on start and on every `EventSourceChanged`, then filtered by `app.installations` (account logins; empty means all). An owner whose installation is suspended or gone has failing listings: its repositories stay as stale data and the status page names the installation.
3. **Conditional cache.** The cache key is (sha256 of `Authorization`, method, URL, `Accept`); the value is (ETag, body ≤ 1 MiB, headers); LRU with 5,000 entries. The transport sends `If-None-Match` on GETs that have an entry. go-github treats a 304 as an error, so on a 304 the transport replays the cached body as a 200 with `X-Scp-Not-Modified: 1`; without an entry it passes the 304 on. `Head` reads a go-github 304 error as `changed == false`. A token rotation is a clean miss by construction.
4. **Rate observer.** Every response, including errors, updates the bucket for (source, installation, `x-ratelimit-resource`) from `x-ratelimit-limit/remaining/reset`; response headers are the authoritative source. `Client.Remaining` reports the core bucket of the owner's installation to the budget gate (`sync-snapshot-store.md` behaviour 4). At most `github.concurrency` requests are in flight per source.
5. **Error classes.** 304s are absorbed by item 3. `sync` reacts per class as described in `sync-snapshot-store.md` behaviour 5.

   | Response | Kind | Carried |
   |---|---|---|
   | 403/429 with `Retry-After`, or `*AbuseRateLimitError` | RateLimited (secondary) | `RetryAt` = now + `Retry-After` (zero when absent) |
   | 403/429 with `x-ratelimit-remaining: 0`, or `*RateLimitError` | RateLimited (primary) | `RetryAt` = `x-ratelimit-reset` |
   | 404 | NotFound | "missing or not visible"; the job stores `not_found` |
   | 409 on commits | NotFound | an empty repository; the job stores `no_content` |
   | 401 | Auth | |
   | 403 "Resource not accessible by integration" | Permanent | `Code` names the missing permission |
   | 5xx, timeout, connection reset | Transient | |
   | malformed 2xx | Permanent | `Code` `malformed` |
6. **Listing.** `ListRepos` follows `Link rel=next` up to 100 pages; any failed page fails the whole listing, so the caller keeps the previous catalogue. Repositories are deduplicated by `id`, which also catches renames and a repository that moves to an earlier page while the listing runs. A missing `default_branch` costs one `Repositories.Get`. The listing is returned as GitHub sends it; `catalog` admits.
7. **Removal** follows `catalog.md` behaviour 4-5 (two complete listings, empty listing, private at once).
8. **Calls per job** (jobs are owned by features):
   - `commits.refresh` (content.md): `Head(known)` when a SHA is known and returns early when unchanged (this filters pushes to other branches); then `Commits(30)`. First sight skips the probe.
   - `docs.refresh` (content.md): `Tree(default branch)`, `Readme`, and `Blob` for docs entries whose SHA changed. A tree with `truncated: true` gives a partial docs list, marked partial.
   - `catalog.descriptor` (catalog.md): `Tree(default branch)` (a free 304 after the docs job within one token), `Blob` when the descriptor SHA changed.
   - `catalog.repo` (catalog.md): `Repo`. `badges.release` (content.md): `LatestRelease`; 404 means "no release".
   - Every file is capped at `github.max_file_bytes`; over the cap is `TooLarge`.
9. **Cold start arithmetic.** Per repository: commits 1, tree 1, README 1, release 1, docs blobs about 0.5, descriptor tree 0 (304): about 4,500 calls for 1,000 repositories. A `pushed_at` sweep with no changes costs about 10 calls per org.
10. **Webhook events v1 handles** (verified and deduplicated first, `http-api.md` behaviour 4; the `cmd/scp` sink calls `sync.Notify(event, repo)`):

    | Event / action | `sync` event |
    |---|---|
    | `push` to `refs/heads/<default_branch>` (`deleted: false`) | `EventPush` (commits, docs, descriptor) |
    | `push` to `refs/tags/*` | `EventTag` (release); a push of more than 3 tags sends no tag events, so `badges.release_ttl` covers it |
    | `push` to other branches, or `deleted: true` | none (202) |
    | `repository` `privatized`, `edited` | `EventRepoChanged` (`catalog.repo` check) |
    | `repository` `created`, `deleted`, `publicized`, `archived`, `unarchived`, `renamed`, `transferred` | `EventSourceChanged` |
    | `installation` (any action), `installation_repositories` `added`, `removed` | `EventSourceChanged` (re-list installations and repositories) |
    | `ping`, anything else | none (202, counted) |
11. **Multi-org and GHES.** Each source has its own host, credential, transports and installations; sources share nothing, and each owner belongs to exactly one source. A GHES source sets `base_url` (`https://host/api/v3/`): go-github uses `WithEnterpriseURLs`, ghinstallation uses `BaseURL`. Web links come from the API's `html_url`, so they point at the right host. GHES rate limits are set by the site admin; the observer treats missing headers as "no limit known" and concurrency still applies.
12. **Timeouts.** Each request has a 30 s client timeout and runs under the job's context. GitHub itself cuts requests at 10 s.

## Config (only v1 keys, Viper dotted names, defaults)
| Key | Default | Meaning |
|---|---|---|
| `github.sources[].id` | none (one source required) | stable name, `[a-z0-9-]+` |
| `github.sources[].base_url` | `https://api.github.com/` | REST base; GHES `https://host/api/v3/` |
| `github.sources[].orgs` | none (required) | organisations of this source |
| `github.sources[].app.id`, `.app.private_key_file` | 0, none | App mode: App id and PEM file (mounted secret) |
| `github.sources[].app.installations` | `[]` (all) | allowed account logins |
| `github.sources[].token_file` | none | token mode (exclusive with `app`) |
| `github.sources[].include_forks` / `.include_archived` | `false` / `true` | admission filters, applied by catalog |
| `github.concurrency` | `4` | in-flight requests per source |
| `github.max_file_bytes` | `1MiB` | README, docs and `scp.yaml` cap |

Constants: 100 pages, 5,000 ETag entries, 30 s request timeout. Validation errors: both `app` and `token_file` set, or neither; a source without `orgs`; an org listed in two sources; a `base_url` without a trailing `/`; duplicate `id`.

## Tests (what proves it; fake GitHub, no network)
`githubtest` is an `httptest` server over an in-memory world: accounts, App with an RSA key, installations, repositories with commits, trees,
blobs and releases. It serves the Shapes endpoints and records every call, unknown paths included, in a ledger. It checks the App JWT, mints
`ghs_` tokens (1 h), sends `x-ratelimit-*` per bucket, binds ETags to `Authorization` and charges nothing for authorised 304s. Faults: status,
secondary limit (with/without `Retry-After`), primary exhaustion, latency, malformed body, truncated tree. Mutators (push, tag, release, rename,
privatize, archive, suspend, uninstall) change the world and queue an HMAC-SHA256-signed delivery. Tests run in `testing/synctest` bubbles.
- Cold start of 1,000 repositories: at most 5,000 core calls; a `pushed_at` sweep with no changes costs 10 calls per org.
- A second refresh within one token is answered by 304; after `ExpireTokens` it is a 200; `Head` with a known SHA returns `changed == false`.
- Each item 5 row maps to its kind and `RetryAt`; `Remaining` follows the headers per installation bucket.
- Listing: one failed page fails the listing; renames keep the id; a repository moved between pages appears once.
- Webhook end to end: a push to the default branch updates commits and README in the snapshot; a push to another branch costs 0 calls; a dropped delivery is repaired by the next sweep.
- Two sources (App on github.com, token on a GHES-style `base_url`) keep separate budgets and tokens.

## Reference (pointers to archive sections worth reading when implementing)
- `reference/detailed-design/05-research/03-github-api.md`: 2.2-2.4 limits and ETags, 2.6-2.11 endpoint facts, 5.2 webhook payloads, appendix A live probes (2026-10-13).
- `reference/detailed-design/05-research/09-github-app-integration.md`: 1.7 installation lifecycle, 2.2 identity and renames, 2.3 event-to-action map, 4.2-4.3 token and rate state, 5 GHES.
- `reference/detailed-design/20-design/05-github-adapter.md`: 4.3 error table, 4.4 rate book, 4.7 listing modes, 7.5-7.10 fake endpoints, faults, webhook outbox, ledger.
- `reference/detailed-design/02-design-guardrails.md` GR-25, GR-26.
