# GitHub integration
Status: as built. Binding order: ../plan.md > this note.

## Scope
`internal/github` reads repositories of one or more GitHub organisations for the sync jobs: App auth or a token, REST reads through
`google/go-github` v92, a small conditional-request and rate-observer transport, error classes, and the fake server
`internal/github/githubtest`. It returns `internal/snapshot` value types. Admission and the removal rule are in `catalog.md`; the rate
budget, retries and pauses in `sync-snapshot-store.md`; webhook verification and dedupe in `http-api.md` behaviour 4. This note lists the
webhook events the portal reacts to and the `sync` event each becomes.

## Decisions (bullets, each with a one-line reason)
- go-github v92 for REST, ghinstallation v2 for App auth (plan.md 4): mature, widely used libraries keep the client small.
- REST covers every read: listings, commits, trees and blobs; the README and documents are read as blobs found in the tree.
- Commit freshness: webhooks, plus the org listing every `catalog.reconcile_interval` (default 5 m) sorted by `pushed_at`; a repository
  whose `pushed_at` changed gets `sync.EventPush`; the 2 h commits TTL is the safety net.
- One listing call for both auth modes, `GET /orgs/{org}/repos`: one code path, and every item carries `visibility`, topics, licence,
  default branch, archived, fork, stars, `pushed_at` and `open_issues_count`.
- Blob SHAs from the tree are the persisted validators: a blob is fetched only when its SHA changed.
- ETags live in memory, keyed by the hashed `Authorization` value: within one token lifetime an authorised 304 costs no rate limit.
- One go-github client per token or App installation, so each installation keeps its own rate bucket.
- API version `2022-11-28`, sent on every request (`X-GitHub-Api-Version`), on github.com and GHES alike.
- The issue count is `open_issues_count`, labelled "open issues and PRs": it includes pull requests and comes with the listing.
- `github` classifies errors and reports rate state; `sync` decides when to call again: one place owns retries and pauses.

## Shapes (minimal Go types/interfaces or table sketches)
```go
package github

type Kind uint8 // Transient | RateLimited | Auth | NotFound | Permanent
type Error struct { Kind Kind; RetryAt time.Time; Code string; Err error } // RetryAt from Retry-After or reset; zero when absent
type TreeEntry struct { Path, Type, SHA string; Size int64 }
type Client struct{ /* sources by id */ }
func (c *Client) Source(id string) *Source
func (c *Client) Remaining(source, owner string) (frac float64, reset time.Time) // sync.Budget
func (c *Client) RateRemaining() map[string]float64                               // per source, for the metrics
func (c *Client) Observe(fn func(source string, status int))                      // every response status, 304 included
// Source is one configured host + credential; methods map 1:1 to the calls below.
func (s *Source) RefreshInstallations(ctx context.Context) error                          // App mode; nothing in token mode
func (s *Source) ListRepos(ctx context.Context, owner string) ([]snapshot.Repo, error)    // complete listing, newest push first, or an error
func (s *Source) Repo(ctx context.Context, owner, name string) (snapshot.Repo, error)
func (s *Source) Tree(ctx context.Context, owner, name, ref string, recursive bool) ([]TreeEntry, error) // "HEAD" = default branch
func (s *Source) Blob(ctx context.Context, owner, name, sha string) ([]byte, error)
func (s *Source) WebURL() string // https://github.com/ or the GHES host
func (s *Source) REST(ctx context.Context, owner string) (*gh.Client, error) // owner's client, for calls only one feature makes
func Classify(resp *gh.Response, err error) error                            // a failed go-github call as an *Error
```
Consumers declare their own narrow interfaces over these methods. The calls shared by several features (listings, trees, blobs) are
methods here; a call only one feature makes lives in that feature over `REST` and `Classify`: the commits listing in `commits`
(`FromGitHub`), the latest release in `badges` (`FromGitHub`).

| Need | go-github v92 call | Auth | Endpoint |
|---|---|---|---|
| installations | `Apps.ListInstallations` | App JWT | `GET /app/installations` |
| repos | `Repositories.ListByOrg` (`type=all`, `sort=pushed`, `direction=desc`, `per_page=100`) | either | `GET /orgs/{org}/repos` |
| one repo | `Repositories.Get` | either | `GET /repos/{o}/{r}` |
| commits | `Repositories.ListCommits` (`PerPage: n`, default branch) | either | `GET /repos/{o}/{r}/commits` |
| tree | `Git.GetTree(ref, recursive)` | either | `GET /repos/{o}/{r}/git/trees/{ref}` |
| README, docs, `scp.yaml`, manifests, images | `Git.GetBlobRaw` | either | `GET /repos/{o}/{r}/git/blobs/{sha}` |
| release | `Repositories.GetLatestRelease` | either | `GET /repos/{o}/{r}/releases/latest` |

Transport chain, outermost first: go-github, then `ghinstallation.Transport` (sets the token), then `scpTransport` (in-flight limit,
API version header, conditional cache, rate observer), then `http.Transport`. `scpTransport` sits below auth so it can read `Authorization`.

## Behaviour (numbered essentials and the edge cases that matter)
1. **Credentials.** App mode: the key file is read at config load; one `ghinstallation.NewAppsTransport` lists installations, and one
   transport per allowed installation comes from `NewFromAppsTransport`; ghinstallation caches and refreshes the token. Token mode: a static
   token from `token_file` goes in `Authorization`. Secrets are `config.Secret` and never logged.
2. **Installations.** `catalog.reconcile` refreshes the installations before each listing, filtered by `app.installations` (account
   logins; empty means all); suspended installations are skipped. An owner with no installation fails with `Permanent`, code
   `no_installation`; its repositories stay as stale data.
3. **Conditional cache.** The cache key is the sha256 of (`Authorization`, URL, `Accept`); the value is (ETag, body ≤ 1 MiB, headers); LRU
   with 5,000 entries per source. The transport sends `If-None-Match` on GETs that have an entry. go-github treats a 304 as an error, so on a
   304 the transport replays the cached body as a 200 with `X-Scp-Not-Modified: 1` and the fresh rate headers. A token rotation is a clean
   miss by construction.
4. **Rate observer.** Every response, errors included, updates the core bucket of its credential from `x-ratelimit-limit/remaining/reset`
   (other `x-ratelimit-resource` values are ignored). `Client.Remaining` reports the bucket of the owner's credential to the budget gate
   (`sync-snapshot-store.md` behaviour 4); unknown or reset windows report 1. At most 4 requests are in flight per source.
5. **Error classes.** `sync` reacts per class as described in `sync-snapshot-store.md` behaviour 5. A cancelled context is returned as is.

   | Response | Kind | Code |
   |---|---|---|
   | `*RateLimitError` (primary) | RateLimited, `RetryAt` = reset | `rate_limited` |
   | `*AbuseRateLimitError` (secondary) | RateLimited, `RetryAt` = now + `Retry-After` (zero when absent) | `secondary_rate_limit` |
   | 404 | NotFound | `not_found` |
   | 409 (empty repository) | NotFound | `empty` |
   | 401 | Auth | `unauthorized` |
   | 403, 422 | Permanent | `forbidden` |
   | 5xx, no response | Transient | `transient` |
   | other 4xx | Permanent | `http_<status>` |
   | malformed 2xx | Permanent | `malformed` |
6. **Listing.** `ListRepos` follows the next page up to 100 pages (more fails with `too_many_pages`); any failed page fails the whole
   listing, so the caller keeps the previous catalogue. Repositories are deduplicated by `id`, which catches a repository that moves to a
   later page while the listing runs. The listing is returned as GitHub sends it; `catalog` admits.
7. **Removal** follows `catalog.md` behaviour 4-5.
8. **Calls per job** (jobs are owned by features):
   - `commits` (content.md): `Repositories.ListCommits` with 30 per page, in `commits`.
   - `docs` (content.md): the `HEAD` tree, the `.github` tree and the recursive `docs` tree; `Blob` for the README and documents whose
     SHA changed; recursive trees of at most 5 other top-level directories that hold referenced images.
   - `catalog.descriptor` (catalog.md): the `HEAD` tree (and `.github` when needed), `Blob` when the descriptor SHA changed.
   - `manifests` (manifests.md): one recursive `HEAD` tree, `Blob` for manifests whose SHA changed.
   - `badges.release` (content.md): `Repositories.GetLatestRelease`, in `badges`; 404 means "no release".
9. **Webhook events handled** (verified and deduplicated first, `http-api.md` behaviour 4; `webhook.Normalise` maps them, the `cmd/scp`
   sink calls `Notify` for a repository or `NotifySource` for an owner):

    | Event / action | `sync` event |
    |---|---|
    | `push` to `refs/heads/<default_branch>` (`deleted: false`) | `EventPush` (commits, docs, descriptor, manifests) |
    | `push` to `refs/tags/*`, `release` | `EventTag` (release) |
    | `push` to other branches, or `deleted: true` | none (202) |
    | `repository` `created`, `deleted`, `publicized`, `privatized`, `archived`, `unarchived`, `renamed`, `transferred`, `edited` | `EventSourceChanged` |
    | `installation`, `installation_repositories` | `EventSourceChanged` (re-list installations and repositories) |
    | any other event with a repository | its GitHub event name (`[a-z_]{1,40}`) for the specs that subscribe to it |
    | `ping`, anything else | none (202) |
10. **Multi-org and GHES.** Each source has its own host, credential, transports and installations; sources share nothing. A GHES source
    sets `base_url` (`https://host/api/v3/`): go-github uses `WithURLs`, ghinstallation uses `BaseURL`; `WebURL` derives the web host. GHES
    without rate headers reports "no limit known" and the in-flight limit still applies.
11. **Timeouts.** Each request has a 30 s client timeout and runs under the job's context.

## Config (Viper dotted names, defaults)
| Key | Default | Meaning |
|---|---|---|
| `github.sources[].id` | none (one source required) | stable name, `[a-z0-9-]+` |
| `github.sources[].base_url` | `https://api.github.com/` | REST base; GHES `https://host/api/v3/` |
| `github.sources[].orgs` | none (required) | organisations of this source |
| `github.sources[].app.id`, `.app.private_key_file` | none | App mode: App id and PEM file (mounted secret) |
| `github.sources[].app.installations` | `[]` (all) | allowed account logins |
| `github.sources[].token_file` | none | token mode (exclusive with `app`) |
| `github.sources[].include_forks` / `.include_archived` | `false` / `true` | admission filters, applied by catalog |
| `github.sources[].include_private` | `false` | private repositories (access.md) |

## Tests (what proves it; fake GitHub, no network)
`githubtest` is an `httptest` server over an in-memory world: an App with an RSA key, installations, tokens, organisations with
repositories, commits, files (trees and blobs) and releases (`AddRepo`, `Update`, `Remove`, `SetCommits`, `SetFile`, `SetRelease`). It
checks the App JWT, mints installation tokens, sends `x-ratelimit-*` per credential, answers ETags with 304 and records every request in a
ledger (`Ledger`, `Count`). Faults: `Fail` (status, times, `Retry-After`), `Exhaust` (rate limit), `Heal`.
- `internal/github`: `TestListReposPaginatesAndConverts`, `TestConditionalRequestsAreFree`, `TestAppMode`, `TestClassify`,
  `TestCanceledIsNotClassified`, `TestETagCacheEvicts`, `TestTreesAndBlobs`, `TestWebURL`,
  `TestRateBookResetInstant`.
- `cmd/scp` `TestLoad1000`: the cold start of 1,000 repositories through an in-memory transport; `TestServeEndToEnd`: pushes, outage and
  signed webhook deliveries against the fake.
