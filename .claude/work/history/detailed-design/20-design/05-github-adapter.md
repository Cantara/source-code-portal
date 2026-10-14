# 20-design / 05 - GitHub adapter, authentication and fake GitHub

Unit key `05-github-adapter`. Packages: `internal/github`, `internal/github/auth`, `internal/github/githubtest`, `internal/gitsource` (specified, post-v1), `cmd/fakegithub`.
Author: unit designer. Date: 2026-10-14. Status: design for implementation, reviewed against `10-architecture.md` (revision 2) and `04-decisions.md` section 7. Independently verified on 2026-10-14: 23 findings (V-01 to V-23), 20 fixed in this document and marked V-nn where they apply, 3 left open and listed in the Verification log at the end; the full report is `30-verification/design-05-github-adapter.md`.

This document is self-sufficient: a developer who has only the files of this design package can build the unit from it. It is binding on the unit; where it needs the architecture or an ADR to change it says so in section 14 and does not assume the change.

Precedence used: `04-decisions.md` (including section 7) over `10-architecture.md` (sections 4-8.7, 8.8, 8.15, 9-23) over `11-adr/` over the background documents. The principles this unit enforces are those of `02-design-guardrails.md` (GR-nn); section 11.2 maps each one that applies here to its mechanism.

## 0. Reading guide, evidence and what was checked

### 0.1 How to read the code in this document

Every Go block in this document is copied by a render script from a scratch module (`github.com/cantara/source-code-portal`, throwaway) in which the code was compiled with `go vet ./...` and `go test ./...` on **2026-10-14**, Go toolchain as in `10-architecture.md` appendix A. The scratch module contains the unit's packages plus three stubs copied from the architecture (`internal/scm`, `internal/job` behaviour interfaces, `internal/platform/secret`; they are not reproduced here, the kernel unit owns them).

Three levels of completeness, stated per file so nobody mistakes a signature for an implementation:

| Level | Meaning | Files |
|---|---|---|
| **Implemented and tested** | Real behaviour, unit tests pass | `github/auth/*` (App JWT, installation token cache, key parsing and reload, static and external sources), `github/errors.go` (classification), `github/ratelimit.go` (rate book and gate), `github/breaker.go` (per-source breaker over gobreaker v2.4.0), `github/webhook.go` (verifier and push decoding), `github/paging.go` (Link and cursor handling, path escaping, GraphQL head query text), `github/gitsha.go`, `github/config.go` (model and `Validate`) |
| **Signature-checked** | Types and method signatures compile and satisfy the consumer interfaces (`internal/archassert`), bodies return `errNotImplemented` or zero values; behaviour is specified in section 4 | `github/client.go`, `github/installation.go`, `github/doctor.go`, `github/appmanifest.go`, `gitsource`, `github/githubtest/{world,server}.go`, `cmd/fakegithub` |
| **Specified only** | Pseudocode and tables in section 4 and 7, no Go | request pipeline (the breaker it uses is implemented), `ListRepositories`, `ProbeHeads` execution, tree memo, redelivery, installation reconcile, doctor checks, fake GitHub route handlers |

The list of passing checks is in section 12.1. A test that "passes" in the scratch module proves the stated behaviour of the implemented files only.

### 0.2 Verification log (facts re-checked in this unit)

| Date | Fact | How checked | Result |
|---|---|---|---|
| 2026-10-14 | `golang-jwt/jwt/v5` v5.3.1 signs RS256 only with `*rsa.PrivateKey`, so it cannot sit behind the `crypto.Signer`/KMS design of `AppKeySource` | read the module source in the module cache | confirmed; own encoder of about 25 lines used (`auth.SignAppJWT`), deviation D-1 |
| 2026-10-14 | GraphQL fields used by the head sweep exist: `Repository.databaseId`, `pushedAt`, `stargazerCount`, `licenseInfo{spdxId}`, `issues(first,states){totalCount}`, `latestRelease{tagName name url isPrerelease publishedAt}`, `defaultBranchRef{target{... on Commit{oid tree{oid}}}}`, `rateLimit{cost remaining resetAt}`, enum value `INTERNAL`, root field `repository(owner,name)` | downloaded the public GraphQL schema and searched it | all present |
| 2026-10-14 | Webhook headers `X-GitHub-Event`, `X-GitHub-Delivery`, `X-GitHub-Hook-Installation-Target-ID`, `X-Hub-Signature-256`; payload cap 25 MB (above it GitHub delivers nothing); push `commits` capped at 2048 | docs.github.com webhook events and payloads page, fetched today | confirmed |
| 2026-10-14 | GraphQL cost formula (connection requests / 100, rounded, minimum 1), node cap 500,000, 10 s timeout answered 502/504 with extra point deduction, App installation limit 5,000 + 50 per repository above 20 + 50 per user above 20 capped 12,500, GHEC 10,000 | docs.github.com GraphQL rate limits page, fetched today | confirmed |
| 2026-10-14 | `POST /app/installations/{id}/access_tokens` body is optional (`repositories`, `repository_ids`, `permissions`), at most 500 repositories, token expires one hour after creation, 201, errors 401/403/404/422 | docs.github.com REST apps page, fetched today | confirmed |
| 2026-10-14 | REST API versions `2026-03-10` (no end date) and `2022-11-28` (support ends 2028-03-10); omitted header means `2022-11-28`; retired version answers 410 | docs.github.com api-versions page, fetched today | confirmed |
| 2026-10-14 | App webhook delivery endpoints `GET /app/hook/deliveries` and `POST /app/hook/deliveries/{id}/attempts` | docs.github.com redelivery article, fetched today | paths confirmed; the article states **no** maximum age and does not say whether the list uses a cursor |
| 2026-10-14 | `httptest.NewTestServer` (in-memory network for `testing/synctest` bubbles) and `testing/synctest.Test` exist in the local Go 1.27.1 toolchain; `synctest` virtual time and `NewTestServer` were exercised by `TestTokenRefreshOnVirtualTime` | `go doc` and a passing test | confirmed |
| 2026-10-14 | the fake's git blob, tree and commit ids equal real git's | golden vectors produced with the local `git` (`git hash-object`, `git write-tree`, `git commit-tree`) and compared in a test | confirmed, including the `a-b.md` versus `a/` tree sort order |
| 2026-10-14 | `sony/gobreaker/v2` v2.4.0 API: `NewCircuitBreaker[T]`, `Settings{MaxRequests, Interval, BucketPeriod, Timeout, ReadyToTrip, OnStateChange, IsSuccessful, IsExcluded}`, `State()`; counts roll by bucket; the library leaves the open state strictly after `Timeout` | read the module source in the module cache; `TestBreakerOpensOnTransientOnly` and `TestBreakerRatioTrip` pass under `synctest` | confirmed |
| carried | Installation token length about 520 characters, ETag credential binding, 3-day redelivery window, 304 free when authorised, REST secondary limits | `05-research/03` and `09` (fetched 2026-10-13) | **not re-fetched in this unit**; cited, and flagged unverified where it matters (section 15) |

Verification gaps that this design does **not** hide are listed in section 15 (U-1 ... U-10).

## 1. Purpose, scope, non-goals, packages

### 1.1 Purpose

This unit is the only place in the product that speaks to GitHub. It turns GitHub's REST, GraphQL, App and webhook wire formats into the kernel types of `scm`, and it classifies every failure into the closed error vocabulary of architecture section 15, so that no feature and no engine code ever sees a status code, a header or a JSON member of GitHub.

It consists of five deliverables:

1. **`internal/github`**: the adapter. One `*Client` for all configured sources. It satisfies the ten direct consumer interfaces of architecture 8.7 (catalog, commits, docs, manifests, media, webhook redelivery), exposes capabilities, rate state, installation registry, doctor and the App manifest flow, and owns the webhook verifier and push decoding.
2. **`internal/github/auth`**: credential strategies behind `TokenSource` (GitHub App, fine-grained/classic PAT, OAuth user token, `token_command`, anonymous) and `AppKeySource`.
3. **`internal/github/githubtest`**: the fake GitHub, a deterministic REST, GraphQL, App and webhook simulator with fault injection, a fixture format and a call ledger. It is the backbone of every end-to-end test and of the developer and Playwright environment (G-13).
4. **`cmd/fakegithub`**: the executable face of the fake (serve, keygen, validate).
5. **`internal/gitsource`**: optional git acquisition (D-17). Specified in section 4.15, built post-v1; `acquisition.mode: git` stays refused in v1 builds.

### 1.2 In scope (v1)

- Repository discovery for GitHub App installations, organisations, users and explicit lists, on github.com, GitHub Enterprise Cloud with data residency and GitHub Enterprise Server, each source with its own host, proxy, CA and pinned hosts.
- Reading: repository metadata and stats (GraphQL head sweep with REST fallback), latest commits, README and files (raw and GitHub-rendered), trees, blobs verified by SHA, repository images.
- Rate-limit bookkeeping per (source, installation, resource), the budget gate that `refresh` consults, typed errors, one breaker/limiter/semaphore per source.
- Webhook verification (SHA-256, constant time, raw bytes), normalisation into `scm.Event`, installation routing, App delivery listing and redelivery.
- Installation registry with the state machine of research 09 section 1.7, and the body of the global job `github.installations`.
- `scp doctor` GitHub checks and the App manifest onboarding flow (`scp github app manifest`).
- The fake GitHub and its fixtures.

### 1.3 Non-goals

- **No product routes.** The unit owns no HTTP route of the product (section 7 lists only the fake's endpoints). The webhook route belongs to `webhook`; the `/status` view of installations and rates belongs to `ops` through a bridge.
- **No persistence.** The adapter holds only bounded in-memory state. Persisted validators (content SHAs) are written by the features through `store`; the adapter never imports `store`.
- **No scheduling, no retry loop, no backoff.** Failures are returned typed; the engine (`refresh`) retries. The adapter sleeps only inside the fake and in the manifest flow's wait for the browser. (A bounded in-request retry exists for exactly one case: one token refresh after a 401.)
- **No rendering, sanitising or parsing of content.** It returns bytes and GitHub-rendered HTML as untrusted data; `docs` sanitises.
- **No write access to GitHub.** The permission set requested is read-only (`metadata`, `contents`, plus optional read permissions); the single mutation the adapter ever performs is the webhook redelivery POST (a write of the delivery log, not of repository data) and token minting.
- **No go-github.** ADR-004. The adapter is an own thin client over `net/http`.
- **No GitLab/Bitbucket.** `scm.SourceID` and `scm.RepoRef.Source` keep the door open; nothing else does.

### 1.4 Packages and files

| Package | Rank | Files (this design) |
|---|---|---|
| `internal/github/auth` | 1 | `source.go` (types, interfaces, errors), `keys.go` (PEM parse, fingerprint, `StaticKeys`, `FileKeys`), `jwt.go` (RS256 JWT over `crypto.Signer`), `app.go` (installation tokens), `static.go` (PAT/OAuth/external/anonymous), `auth_test.go` |
| `internal/github` | 2 | `config.go`, `client.go`, `errors.go`, `ratelimit.go`, `breaker.go`, `paging.go`, `webhook.go`, `gitsha.go`, `installation.go`, `doctor.go`, `appmanifest.go`, `github_test.go`; to be added at implementation: `transport.go` (pipeline), `repos.go`, `heads.go`, `commits.go`, `contents.go`, `trees.go`, `redeliver.go`, `capabilities.go`, `metrics.go` |
| `internal/github/githubtest` | 1 | `world.go` (state and fixtures), `server.go` (HTTP face, faults, hooks, ledger), `fixture_test.go`; to be added: `routes_*.go`, `graphql.go`, `gitobjects.go`, `webhooks.go`, `control.go` |
| `cmd/fakegithub` | 6 | `main.go` |
| `internal/gitsource` | 2 (post-v1) | `gitsource.go` and implementation files |

Everything that is not exported by a name in section 2 or 3 is unexported. No `init()` and no mutable package-level variable exists in any of these packages (rules G5, `gochecknoinits`, `gochecknoglobals`); the only package-level variables are compiled regular expressions (`regexp.MustCompile`, accepted by `gochecknoglobals`) and sentinel errors named `Err...`; lookup tables are `switch` statements.

## 2. Position in the architecture

### 2.1 Allowed imports (matrix rows, architecture 5.2)

| Package | May import (internal) | Third party (only here) | Needs matrix amendment? |
|---|---|---|---|
| `github/auth` (rank 1) | `scm`, `platform/secret` | none in the matrix row (the row lists `golang-jwt/jwt/v5`, which this design does not use, deviation D-1) | **yes**: `golang.org/x/sync/singleflight` (token mint coalescing, required by 8.8 itself) is not in the row (D-2) |
| `github` (rank 2) | `scm`, `freshness`, `platform/secret`, `platform/logx`, `platform/netguard`, `github/auth` | `sony/gobreaker/v2` v2.4.0, `cenkalti/backoff/v7` v7.0.1, otelhttp, otel API (`metric`, `trace`) | **yes**: `x/sync/singleflight` (tree memo, 8.7 requires it) and `x/time/rate` (per-source RPS limiter) are not in the row (D-2) |
| `github/githubtest` (rank 1) | `scm` only | none | no |
| `gitsource` (rank 2, post-v1) | same as `github` | none | no |
| `cmd/fakegithub` (rank 6) | `github/githubtest`, `scm` | none | no |

Consequences that the code already honours (the scratch module compiles with exactly these imports):

- `github` does **not** import `job`, `refresh`, `httpapi` or `platform/config`. Error classification therefore works by **behaviour methods** (`RetryAt() time.Time`, `Permanent() bool`, `Gone() bool`) that satisfy the `job` interfaces structurally. The budget gate is exposed as `Admit(ReserveClass) time.Duration` with an adapter-owned enum, and `cmd/scp/internal/bridge` maps `job.Priority` onto it.
- `github` does not import `platform/config`: it declares `github.Config` (section 6) and `cmd/scp/config.go` composes it under the key `github`. Secrets are referenced as `SecretRef{File, Env}` and resolved in `New` through the injected `Deps.LookupEnv` (rule G9: no `os.Getenv` in features).
- `os/exec` is not imported (G6). The only subprocess the unit needs in v1 is the `token_command` strategy; it receives an injected `auth.CommandRunner` implemented in `cmd/scp`.
- `githubtest` does not import `github` (the fake must not depend on the code under test), so it re-declares `HostKind` and speaks YAML-free JSON fixtures.
- `net/http` clients are never built in this unit: `Deps.NewHTTP(Network)` is a factory implemented in `cmd/scp` over `platform/netguard` (SSRF-safe dialing with allow-lists) and handed in. Tests pass a plain client or the fake's bubble client. The factory contract is in 2.4.
- **Test files obey the same rows** (V-01; architecture 5.2, last paragraph: a `_test.go` may import what its package may import, plus `githubtest` and test libraries). `internal/github/*_test.go` therefore must not import `job` (rank 1, not in the `github` row): the reference tests copy the three behaviour interfaces (`RateLimited`, `Permanent`, `Gone`) into the test file and assert the structural contract with them. The `archtest` that checks test imports against the matrix would otherwise fail on the first CI run.

### 2.2 Importers

| Importer | Imports | What for |
|---|---|---|
| `cmd/scp` | `github`, `github/auth`, `gitsource` (build tag), `github/githubtest` (`_test.go` files only: the e2e tests; V-02) | construction, wiring, bridges, assertions, CLI (`doctor`, `github app manifest`) |
| `cmd/scp/internal/bridge` | `github` (root) | `RateGate` (refresh.Gate), `InstallationStatus` (ops), `Tracker` bridging is catalog-owned, not here |
| `cmd/fakegithub` | `githubtest` | executable |
| test files of any package | `githubtest` | only `cmd/scp` tests and the adapter's own tests (matrix note on test files); feature tests use hand-written fakes |

No feature (`catalog`, `commits`, `docs`, `manifests`, `media`, `webhook`, `integrations`, `ops`) imports `github`. They see it only through the consumer interfaces below.

### 2.3 Consumer interfaces satisfied (exact signatures, compile-checked)

`internal/archassert/assert.go` is the scratch copy of `cmd/scp/assert.go`. It declares each consumer interface exactly as the consumer package declares it (stdlib and kernel types only) and asserts that `*github.Client`, `*github.Verifier` and `*gitsource.Client` satisfy them. This is the compile-time proof that the architecture's "direct" rows hold for the types this unit defines.

```go
// Package archassert: scratch copy of cmd/scp/assert.go, with the consumer interfaces of 10-architecture.md 8.7
// declared here exactly as in their consumer packages (signatures use stdlib and scm only).
package archassert

import (
	"context"
	"net/http"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/gitsource"
	"github.com/cantara/source-code-portal/internal/scm"
)

type catalogRepositoryLister interface {
	ListRepositories(ctx context.Context, src scm.SourceID, cursor string, v scm.Validators) (scm.Result[scm.Page[scm.Repository]], error)
}
type catalogHeadProber interface {
	ProbeHeads(ctx context.Context, repos []scm.RepoRef) (map[scm.RepoID]scm.RepoStats, error)
}
type commitsCommitLister interface {
	LatestCommits(ctx context.Context, r scm.RepoRef, ref string, n int, v scm.Validators) (scm.Result[[]scm.Commit], error)
}
type docsFileReader interface {
	Readme(ctx context.Context, r scm.RepoRef, ref string, v scm.Validators) (scm.Result[scm.File], error)
	ReadFile(ctx context.Context, r scm.RepoRef, ref, path string, v scm.Validators) (scm.Result[scm.File], error)
}
type docsTreeLister interface {
	ListTree(ctx context.Context, r scm.RepoRef, ref string, opt scm.TreeOptions) (scm.Result[scm.Tree], error)
}
type docsRenderedReader interface {
	RenderedFile(ctx context.Context, r scm.RepoRef, ref, path string, v scm.Validators) (scm.Result[string], error)
}
type manifestsTreeReader interface {
	ListTree(ctx context.Context, r scm.RepoRef, ref string, opt scm.TreeOptions) (scm.Result[scm.Tree], error)
}
type manifestsBlobReader interface {
	ReadBlob(ctx context.Context, r scm.RepoRef, sha string, maxBytes int64) (scm.Blob, error)
}
type manifestsBlobBatchReader interface { // optional batched read (11 D-4); added by the cross-design review CD-IF-01
	ReadBlobs(ctx context.Context, r scm.RepoRef, shas []string, maxBytes int64) (map[string]scm.Blob, error)
}
type mediaRepoImageReader interface {
	ReadRepoImage(ctx context.Context, r scm.RepoRef, ref, path string, maxBytes int64) (scm.File, error)
}
type webhookVerifier interface {
	Verify(src scm.SourceID, header http.Header, body []byte) (scm.Event, error)
}
type webhookRedeliverer interface {
	FailedDeliveries(ctx context.Context, src scm.SourceID, since time.Time) ([]int64, error)
	Redeliver(ctx context.Context, src scm.SourceID, deliveryID int64) error
}

var (
	_ catalogRepositoryLister = (*github.Client)(nil)
	_ catalogHeadProber       = (*github.Client)(nil)
	_ commitsCommitLister     = (*github.Client)(nil)
	_ docsFileReader          = (*github.Client)(nil)
	_ docsTreeLister          = (*github.Client)(nil)
	_ docsRenderedReader      = (*github.Client)(nil)
	_ manifestsTreeReader     = (*github.Client)(nil)
	_ manifestsBlobReader     = (*github.Client)(nil)
	_ mediaRepoImageReader    = (*github.Client)(nil)
	_ webhookVerifier         = (*github.Verifier)(nil)
	_ webhookRedeliverer      = (*github.Client)(nil)
	// gitsource satisfies the same five direct interfaces (architecture 8.15)
	_ docsFileReader       = (*gitsource.Client)(nil)
	_ docsTreeLister       = (*gitsource.Client)(nil)
	_ manifestsTreeReader  = (*gitsource.Client)(nil)
	_ manifestsBlobReader  = (*gitsource.Client)(nil)
	_ mediaRepoImageReader = (*gitsource.Client)(nil)
)
```

Notes on the table above:

- `ReadFile` has exactly one signature in the product (`docs.FileReader`); `media` uses `ReadRepoImage` (revision-2 fix F-01).
- `ListTree` is declared identically by `docs` and `manifests`; `scm.TreeOptions` and the opaque `scm.Tree` are kernel types.
- `webhook.Redeliverer` is optional: `cmd/scp` registers the job `webhook.redelivery` only when the source is an App source (`Capabilities().AppAuth`) and `redelivery.enabled` is true.
- **Bridged, not direct** (they need consumer-owned view types, so `github` cannot name them): `ops.InstallationStatus` (`Installations() []InstallationView; Rate() []RateView`) is a bridge over `Client.Installations()` and `Client.Rates()`; `refresh.Gate` (`Admit(upstream string, p job.Priority) time.Duration`) is a bridge over `Client.Admit`; `catalog.Tracker` is a bridge over `refresh.Planner`, unrelated to this unit.

The bridge code belongs to `cmd/scp/internal/bridge` (owned by the composition unit); its two github-facing pieces are small enough to specify here so the two units cannot diverge:

```go
// package bridge (cmd/scp/internal/bridge), rank 5. The matrix row says "the roots of every feature and service";
// github is a service root, but the row does not name it: deviation D-3 asks to list it explicitly.
type RateGate struct{ C *github.Client }

// Admit implements refresh.Gate. Only the upstream "github" is gated by this adapter; any other name admits at once.
func (g RateGate) Admit(upstream string, p job.Priority) time.Duration {
    if upstream != "github" { return 0 }
    switch p {
    case job.PrioBackfill:    return g.C.Admit(github.ClassBackfill)
    case job.PrioScheduled:   return g.C.Admit(github.ClassScheduled)
    case job.PrioWebhook:     return g.C.Admit(github.ClassWebhook)
    default:                      return g.C.Admit(github.ClassInteractive)
    }
}
```

(Constant names are those of architecture 8.3: `PrioInteractive < PrioWebhook < PrioScheduled < PrioBackfill`. The `default` arm maps any future priority to the interactive class, which admits whenever the bucket is not empty.)

### 2.4 Construction and the client factory contract

`cmd/scp` constructs the adapter once in startup step 4 (architecture 10.1) with this signature (compile-checked, bodies stubbed in the scratch module; section 4.1 specifies `New`):

```go
package github

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/cantara/source-code-portal/internal/github/auth"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Network is the part of a source's configuration that the HTTP client factory needs.
type Network struct {
	Source                scm.SourceID
	Hosts                 []string // pinned hosts: API, GraphQL and web hosts of the source
	ProxyURL              string
	CAFile                string
	AllowPrivateAddresses bool
	AllowInsecureHTTP     bool
	HTTP                  HTTPConfig // dial/TLS/header/idle timeouts and pool size for the factory; no overall Client.Timeout
}

// Deps is what cmd/scp injects (rule G11: Meter and Tracer come from platform/telemetry, never from globals).
type Deps struct {
	Logger     *slog.Logger
	Meter      metric.Meter
	Tracer     trace.Tracer
	Version    string                              // build version for the User-Agent
	Seed       uint64                              // seeds jitter and backoff (github.New derives auth.NewRand01 and the secondary-limit jitter from it); tests fix it
	NewHTTP    func(Network) (*http.Client, error) // bound to platform/netguard in cmd/scp; tests pass a plain client
	// LookupEnv removed (CD-CF-06): secrets are files (secret.Secret); no environment variable is read by this package
	RunCommand auth.CommandRunner                  // os/exec lives in cmd/scp (rule G6)
}

// Client is the one adapter object for all sources. It satisfies the direct consumer interfaces of architecture 8.7.
type Client struct {
	rates   *rateState
	sources map[scm.SourceID]*source
	deps    Deps
}

// source is the per-source machinery: URLs, credential, breaker, semaphore, caches, installation registry.
type source struct {
	id   scm.SourceID
	cfg  SourceConfig
	caps Capabilities
}

// New builds every source: resolves secrets, loads the App key, constructs token sources and clients, validates URLs.
// It makes NO network call; the credential check is Client.CheckCredentials (startup step 4) or `scp doctor`.
func New(cfg Config, d Deps) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Client{rates: newRateState(cfg.Defaults.Budget.Reserve), sources: map[scm.SourceID]*source{}, deps: d}, nil
}

// CheckCredentials is startup step 4: for each source, App key parsed and accepted by GET /app, or the PAT accepted by
// GET /user. It returns only definite credential failures (401, unknown key); a network failure is not an error here,
// it surfaces as readiness (the engine keeps pausing the upstream). The checks are the cheap subset of Doctor.
func (c *Client) CheckCredentials(ctx context.Context) error { return errNotImplemented }

// ReloadKeys re-reads every App key source now (SIGHUP). A key that fails to parse keeps the previous one in service.
func (c *Client) ReloadKeys() error { return nil }

// Close releases idle connections. There is no goroutine to stop: the adapter owns none.
func (c *Client) Close() {}

var errNotImplemented = errors.New("github: signature check only")

// ---- consumer interfaces satisfied directly (architecture 8.7) ------------------------------------------------

func (c *Client) ListRepositories(ctx context.Context, src scm.SourceID, cursor string, v scm.Validators) (scm.Result[scm.Page[scm.Repository]], error) {
	return scm.Result[scm.Page[scm.Repository]]{}, errNotImplemented
}

func (c *Client) ProbeHeads(ctx context.Context, repos []scm.RepoRef) (map[scm.RepoID]scm.RepoStats, error) {
	return nil, errNotImplemented
}

func (c *Client) LatestCommits(ctx context.Context, r scm.RepoRef, ref string, n int, v scm.Validators) (scm.Result[[]scm.Commit], error) {
	return scm.Result[[]scm.Commit]{}, errNotImplemented
}

func (c *Client) Readme(ctx context.Context, r scm.RepoRef, ref string, v scm.Validators) (scm.Result[scm.File], error) {
	return scm.Result[scm.File]{}, errNotImplemented
}

func (c *Client) ReadFile(ctx context.Context, r scm.RepoRef, ref, path string, v scm.Validators) (scm.Result[scm.File], error) {
	return scm.Result[scm.File]{}, errNotImplemented
}

func (c *Client) RenderedFile(ctx context.Context, r scm.RepoRef, ref, path string, v scm.Validators) (scm.Result[string], error) {
	return scm.Result[string]{}, errNotImplemented
}

func (c *Client) ListTree(ctx context.Context, r scm.RepoRef, ref string, opt scm.TreeOptions) (scm.Result[scm.Tree], error) {
	return scm.Result[scm.Tree]{}, errNotImplemented
}

func (c *Client) ReadBlob(ctx context.Context, r scm.RepoRef, sha string, maxBytes int64) (scm.Blob, error) {
	return scm.Blob{}, errNotImplemented
}

// ReadBlobs is the batched form (CD-IF-01): GraphQL aliases `object(oid:){... on Blob{oid byteSize isBinary isTruncated text}}`, 25 per query,
// at most 100 shas per call (the caller chunks), each returned blob verified with BlobSHA exactly as ReadBlob. A blob that is missing,
// binary, truncated, above maxBytes or not verifiable is ABSENT from the map (the caller falls back to ReadBlob for it); a transport,
// auth or rate error fails the whole call with the usual typed *Error. Cost: one GraphQL point per 50 aliased objects (7.8).
func (c *Client) ReadBlobs(ctx context.Context, r scm.RepoRef, shas []string, maxBytes int64) (map[string]scm.Blob, error) {
	return nil, errNotImplemented
}

func (c *Client) ReadRepoImage(ctx context.Context, r scm.RepoRef, ref, path string, maxBytes int64) (scm.File, error) {
	return scm.File{}, errNotImplemented
}

func (c *Client) FailedDeliveries(ctx context.Context, src scm.SourceID, since time.Time) ([]int64, error) {
	return nil, errNotImplemented
}

func (c *Client) Redeliver(ctx context.Context, src scm.SourceID, deliveryID int64) error {
	return errNotImplemented
}

// ---- operations surface (bridged in cmd/scp) ---------------------------------------------------------------------

// Admit is the budget gate: how long should a job of this class wait? Zero admits. cmd/scp/internal/bridge.RateGate
// maps job.Priority to ReserveClass and calls this.
func (c *Client) Admit(class ReserveClass) time.Duration { return c.rates.admit(class, time.Now()) }

// Rates returns every known bucket (for /status, metrics and the bridge).
func (c *Client) Rates() []RateSnapshot { return c.rates.snapshots() }

// HostInfo and capability probing.
type Capabilities struct {
	HostKind             HostKind
	APIVersion           string // effective X-GitHub-Api-Version
	AppAuth              bool
	GraphQL              bool // heads sweep through GraphQL is active (false when off, unsupported, or in a 1 h cool-down)
	GraphQLBlobs         bool
	AsyncSBOM            bool
	InternalVisibility   bool // ghes/ghec_dr: repositories may have visibility "internal"
	OpenIssuesIncludePRs bool // true on the REST fallback path (open_issues_count counts pull requests)
	RedeliveryRetention  time.Duration
}

// Capabilities describes the first configured source; SourceCapabilities one source.
func (c *Client) Capabilities() Capabilities { return Capabilities{} }
func (c *Client) SourceCapabilities(src scm.SourceID) (Capabilities, bool) {
	s, ok := c.sources[src]
	if !ok {
		return Capabilities{}, false
	}
	return s.caps, true
}

// TokenSource exposes a source's credential strategy (doctor, ops, the manifest flow).
func (c *Client) TokenSource(src scm.SourceID) (auth.TokenSource, bool) { return nil, false }

// Verifier returns the webhook verifier wired to every source's secrets and installation registry.
func (c *Client) Verifier(maxBody int) *Verifier { return NewVerifier(maxBody, nil) }
```

**`Deps.NewHTTP(Network) (*http.Client, error)`** is the netguard contract. For each source `cmd/scp` calls `netguard.NewClient(...)` with the source's pinned hosts (API host, GraphQL host, web host, plus the host of any `Link` next target that is on the same origin, which the adapter checks itself), the proxy and CA settings, `AllowPrivateAddresses` and `AllowInsecureHTTP`. The returned client must:

1. have dial, TLS-handshake, response-header and idle timeouts (the `Network.HTTP` member, an `HTTPConfig`), and **no overall `Client.Timeout`** (the adapter applies per-request `context.WithTimeout`, because GraphQL and REST need different values and streaming reads of bounded bodies must not be cut by a total deadline mid-read);
2. refuse redirects to any host that is not pinned (the adapter additionally refuses to forward `Authorization` across origins: it sets `CheckRedirect` via the factory result and verifies on every response that `resp.Request.URL.Host` equals the request host);
3. resolve and connect only to addresses the SSRF policy allows (GHES in a private network is therefore an explicit `allow_private_addresses: true`, never a default);
4. be safe for concurrent use and shared by all calls of the source (one pool per upstream, not per call, GR-22).

The adapter owns **no goroutine** (section 8). It reads `Deps.Seed` for jitter and `Deps.Version` for the `User-Agent`.

### 2.5 What `cmd/scp` still has to do for this unit

| Item | Where | Notes |
|---|---|---|
| Compose `github.Config` into the root config under `github:` and run `Validate()` | `cmd/scp/config.go` | all violations listed together (`ValidationError` joined) |
| Cross-check `discovery.visibility: installation_scope` with `access.mode: trusted-proxy` or `discovery.access_acknowledgement` | `cmd/scp` startup validation (architecture 16 "refuses") | `github.Validate` checks the vocabulary only (it cannot see `access`) |
| Choose `github.Client` or `gitsource.Client` per source for the five file-oriented interfaces | `cmd/scp` wiring | by `acquisition.mode`; v1 refuses `git` |
| Build `Verifier` input: `Client.Verifier(maxBody)` | wiring | returns a verifier bound to every source's secrets and installation registry |
| Register jobs `github.installations` (global, 30 min) and `webhook.redelivery` (6 h) | wiring | specs in section 4.12 and 4.11 |
| Reactor for installation events: `reactor.Fan` closure calls `Client.ObserveEvent(ev)` then `Enqueue("github.installations/_", PrioWebhook, ...)` | wiring | this closure is built in `cmd/scp` (it needs both `github` and `job`); it is the "installation reactor" of section 4.12 |
| Implement `CommandRunner` (`os/exec`, no shell, 16 KiB cap, timeout, process-group kill) | `cmd/scp` | only needed for `token_command` |
| `SIGHUP` handler calling `FileKeys.Reload` on App key sources | `cmd/scp` | rotation without restart; `Client` exposes `ReloadKeys()` (section 4.5) |
| Metrics generation test enumerating this unit's instruments | `cmd/scp` test (18.2) | names in section 9 |

## 3. Domain model and Go types

### 3.1 Principles

1. **The adapter owns every GitHub wire type and none of them leaves it.** Decoding structs (`payload`, `pushCommit`, the REST repository, commit, tree and README shapes, the GraphQL response) are unexported. Consumers see `scm.Repository`, `scm.RepoStats`, `scm.Commit`, `scm.File`, `scm.Tree`, `scm.Blob`, `scm.Event`, `scm.Validators`, `scm.Result[T]`, `scm.Page[T]` and the error types below.
2. **Unknown members and unknown enum values are tolerated** (GitHub adds both in every API version); required members are checked by the decoder of each operation, and a missing required member is a `KindPermanent` error with code `undecodable`, never a zero value that flows on. The only strict decoders in the unit are the fixture loaders of the fake (`DisallowUnknownFields`), because there a typo is a bug.
3. **Absent differs from empty.** Push path lists are `*[]string` so a payload without `added/removed/modified` (unverified on GitHub's page, research V13) is distinguishable from a push that changed nothing; `PathsComplete` carries the conclusion.
4. **Identity is the numeric repository id.** `RepoRef{Source, ID, Owner, Name}`: the ID comes from `id` (REST) or `databaseId` (GraphQL); owner and name are validated by `validOwnerName` before they ever reach a URL path. Rename and transfer are handled by the catalogue through the id (ADR-023).
5. **Secrets are `secret.Secret`** everywhere (token values, webhook secrets, the PEM). `Reveal()` is called at exactly seven listed call sites (V-17; the first draft said "three places" while its own compiled code had seven): the `Authorization` header write of the pipeline (`github/transport.go`), the `Authorization` header of the installation-token mint (`auth/app.go`, `mintOnce`), PEM parsing (`auth/keys.go`, `FileKeys.reloadLocked`), the HMAC key (`github/webhook.go`, `SignSHA256` and `Verify`), and the two token helpers `Token.Fingerprint` and `Token.Same` (`auth/source.go`) (section 10).

### 3.2 Credential types (`github/auth`)

```go
// Package auth implements the credential strategies of the GitHub adapter (architecture 8.8).
package auth

import (
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/scm"
)

// TokenKind names the credential family. It is a label for logs, metrics and /status, never a branch in feature code.
type TokenKind string

const (
	KindInstallation TokenKind = "installation"
	KindPAT          TokenKind = "pat"
	KindOAuthUser    TokenKind = "oauth_user"
	KindExternal     TokenKind = "external"
	KindAnonymous    TokenKind = "anonymous"
)

// Permissions maps a GitHub permission name to "read" or "write" ("contents" -> "read").
type Permissions map[string]string

// Scope narrows a token. The zero value means "the full grant of the credential".
type Scope struct {
	Installation  scm.InstallationID // 0 for PAT, OAuth, external and anonymous credentials
	Permissions   Permissions        // nil = everything the installation was granted
	RepositoryIDs []scm.RepoID       // nil = every repository of the installation; at most 500
}

// Key is a stable, order-independent cache key: "<installation>|<perm=lvl,...>|<id,...>".
func (s Scope) Key() string {
	var b strings.Builder
	b.WriteString(strconv.FormatInt(int64(s.Installation), 10))
	b.WriteByte('|')
	names := make([]string, 0, len(s.Permissions))
	for k := range s.Permissions {
		names = append(names, k)
	}
	sort.Strings(names)
	for i, k := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k + "=" + s.Permissions[k])
	}
	b.WriteByte('|')
	ids := append([]scm.RepoID(nil), s.RepositoryIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(int64(id), 10))
	}
	return b.String()
}

// Token is what the transport writes into the Authorization header. Value is opaque: since 2026 installation
// tokens are about 520 characters on github.com and GHEC (research 09 section 1.4); nothing validates its length.
type Token struct {
	Value       secret.Secret
	Kind        TokenKind
	ExpiresAt   time.Time // zero = does not expire
	Permissions Permissions
	Selection   string // "all" | "selected" | ""
}

// Fingerprint is a short, non-reversible identifier of the token value for cache keys and logs.
func (t Token) Fingerprint() string {
	sum := sha256.Sum256([]byte(t.Value.Reveal()))
	return hex.EncodeToString(sum[:6])
}

// Same reports whether two tokens carry the same value, in constant time. It is the only comparison of token values in the
// package, so Reveal stays confined to a short, listed set of call sites (section 10.2).
func (t Token) Same(o Token) bool {
	return subtle.ConstantTimeCompare([]byte(t.Value.Reveal()), []byte(o.Value.Reveal())) == 1
}

// CredentialInfo is safe to log and to show in /status.
type CredentialInfo struct {
	Kind           TokenKind
	Identity       string // app slug or client id, user login, "anonymous"
	KeyFingerprint string // base64 of sha256 over the PKIX public key: the format GitHub shows and `openssl ... | openssl base64` prints
	ExpiresAt      time.Time
}

// TokenSource is implemented by every strategy. Concurrent Token calls for one scope are coalesced.
type TokenSource interface {
	// Token returns a token valid for at least minValidity.
	Token(ctx context.Context, scope Scope, minValidity time.Duration) (Token, error)
	// Reject reports that the API answered 401 for exactly this token; it is dropped only if still current.
	Reject(scope Scope, rejected Token)
	Info() CredentialInfo
}

// AppKeySource supplies the signing keys of a GitHub App in preference order. *rsa.PrivateKey satisfies
// crypto.Signer, and so does a KMS or Vault client; implementations reload on Reload or on file mtime change.
type AppKeySource interface {
	Signers(ctx context.Context) ([]crypto.Signer, error)
}

// ExpiryObserver is implemented by sources that learn a credential's expiry from response headers
// (github-authentication-token-expiration on fine-grained PATs). The transport calls it when the header is present.
type ExpiryObserver interface{ ObserveExpiry(t time.Time) }

// Error kinds returned by sources. They wrap one of the sentinels below so callers use errors.Is.
var (
	ErrCredential              = errors.New("auth: credential rejected")      // bad key, deleted app, bad PAT (401)
	ErrInstallationUnavailable = errors.New("auth: installation unavailable") // suspended or removed (403/404 on mint)
	ErrClock                   = errors.New("auth: clock skew")               // GitHub rejected iat/exp
	ErrUnavailable             = errors.New("auth: token endpoint unavailable")
	ErrRateLimited             = errors.New("auth: token endpoint rate limited") // 403/429 with Retry-After, remaining 0 or a rate-limit message on mint
	ErrInvalidScope            = errors.New("auth: token scope refused")         // 422 on mint: the narrowed repositories/permissions are not grantable
	ErrCommand                 = errors.New("auth: token command failed")
	ErrNoCredential            = errors.New("auth: no credential configured") // anonymous source asked for an authenticated scope
)

// Error is the typed failure of a source.
type Error struct {
	Err        error // one of the sentinels
	Status     int   // HTTP status of the mint call, 0 if none
	Message    string
	RequestID  string
	RetryAfter time.Duration // advisory, from Retry-After on 403/429 of the mint call
}

func (e *Error) Error() string {
	s := e.Err.Error()
	if e.Status != 0 {
		s += " (status " + strconv.Itoa(e.Status) + ")"
	}
	if e.Message != "" {
		s += ": " + e.Message
	}
	return s
}
func (e *Error) Unwrap() error { return e.Err }
```

Design notes on these types:

- `Scope.Key()` is order-independent and is the cache and singleflight key. A scope with zero `RepositoryIDs` and nil `Permissions` is the common case (full installation grant); narrower scopes exist for the per-feature least-privilege option (for example a `contents:read` only token for `gitsource`).
- `Token.Fingerprint()` is a 12-hex-digit SHA-256 prefix used as part of in-memory conditional-request keys (a new token is a clean miss, never a stale 304 attempt) and in logs; it is not reversible and never leaves the process. `Token.Same(other)` is the only comparison of token values (constant time), used by `Reject`; it exists so that `Secret.Reveal` stays confined to the call sites listed in 10.2 (V-17).
- `CredentialInfo.KeyFingerprint` and `auth.Fingerprint` are **base64 of SHA-256 over the PKIX DER public key**, the value GitHub shows under the App's Credentials and that `openssl rsa -in KEY.pem -pubout -outform DER | openssl sha256 -binary | openssl base64` prints (docs.github.com, checked 2026-10-14). The first draft printed 16 hex characters, which an operator could not compare with anything GitHub shows (V-08).
- `TokenSource.Reject(scope, rejected)` implements "drop only if still current": a slow request that gets a 401 after a fresh mint must not evict the fresh token. `ExpiryObserver` is how a fine-grained PAT's `github-authentication-token-expiration` response header reaches `Info().ExpiresAt` (doctor warns under 14 days).
- The sentinel errors of the package are the only things the adapter inspects when it maps a mint failure: `ErrInstallationUnavailable` with `Status` 403 becomes `KindSuspended` (no network retry), with `Status` 404 becomes `KindNotFound` / `SubjectInstallation` (`Gone()`: the installation was removed; V-04), `ErrRateLimited` becomes `KindRateLimited` as a `*PauseError` of `RetryAfter` (V-03), `ErrCredential` and `ErrClock` become `KindAuth` (a `PauseError` through `authPause`), `ErrUnavailable` and `ErrCommand` become `KindTransient`, `ErrNoCredential` and `ErrInvalidScope` become `KindPermanent` (codes `no_credential`, `invalid_scope`: configuration).

### 3.3 Error types (`github`)

The complete classification code is in section 4.3; the types are summarised here because every other section refers to them.

| Type | Satisfies | Meaning |
|---|---|---|
| `*Error{Kind, Status, Code, Op, Source, Subject, Message, RequestID, RetryAfter, ResetAt, Err}` | `error`, `job.Permanent` (`Permanent()`), `job.Gone` (`Gone()`); wraps `scm.ErrNotFound`, `scm.ErrForbidden`, `scm.ErrUnavailable` through `Unwrap() []error` | every failure the adapter returns |
| `*PauseError{Cause *Error, At time.Time}` | `error`, `job.RateLimited` (`RetryAt()`), unwraps to `Cause` | only for rate limits, authentication failure after the single refresh, and an open breaker: the engine pauses the whole upstream until `At` without consuming an attempt |
| `*WebhookError` | `error`, `InvalidSignature() bool`, `Malformed() bool`, `Ignored() bool`, `Reason() string` | non-event outcomes of `Verifier.Verify` |
| `*ValidationError{Path, Msg}` | `error` | one violation of `Config.Validate`; joined with `errors.Join` |
| `*auth.Error` | `error`, unwraps to a sentinel | credential-source failures |

`ErrKind` values (closed set of architecture section 15): `KindNotFound`, `KindAuth`, `KindPermissionMissing`, `KindRateLimited`, `KindSuspended`, `KindTransient`, `KindPermanent`, `KindCanceled`. `Subject` says **what** a 404 refers to (`SubjectRepository`, `SubjectFile`, `SubjectInstallation`) so features can tell "the repository is gone" from "the README does not exist". `Error.Gone()` is true for a not-found whose subject is the repository or the installation; `Error.Missing()` names the subject for features that handle absent files as state.

A plain `*Error` has **no** `RetryAt` method, deliberately: `errors.As(err, &job.RateLimited)` must not succeed for a 404. Only `PauseError` has it (this fixed a defect found while compiling the first draft, section 12.1).

### 3.4 Domain-facing outputs (all kernel types)

`scm` types are specified by the kernel unit (architecture 8.1); the table fixes only **which fields this adapter fills and how**, because those rules are this unit's contract with its consumers.

| Kernel type | Field | Filled from | Rule |
|---|---|---|---|
| `scm.Repository` | `Ref.Source` | the source id | always |
| | `Ref.ID` | REST `id` / GraphQL `databaseId` | required; absent is `undecodable` |
| | `Ref.Owner`, `Ref.Name` | `owner.login`, `name` | validated by `validOwnerName`; invalid repository is skipped and counted (`scp_github_skipped_total{reason="bad_name"}`) rather than failing the page |
| | `Slug` | not filled | the catalogue derives slugs (ADR-023) |
| | `Description` | `description` | `""` when null |
| | `DefaultBranch` | `default_branch` | if absent in a minimal repository object, one `GET /repos/{o}/{r}` fills it; if still absent the repository is skipped and counted |
| | `Topics` | `topics` | sorted, lower-case as sent |
| | `Visibility` | `visibility`, else `private` boolean | `public`/`private`/`internal`; an unknown value maps to `VisPrivate` (fail closed) |
| | `Archived`, `Fork` | `archived`, `fork` | `disabled: true` repositories are dropped (counted) |
| | `HTMLURL` | `html_url` | must have the source's web host, else rebuilt from `web_url/owner/name` |
| | `CreatedAt`, `PushedAt` | `created_at`, `pushed_at` | zero when null (empty repositories have `pushed_at` null) |
| `scm.RepoStats` | `OpenIssues` | GraphQL `issues(states:OPEN){totalCount}` (issues only) or REST `open_issues_count` (issues + pull requests) | `Capabilities().OpenIssuesIncludePRs` says which |
| | `Stars` | `stargazerCount` / `stargazers_count` | |
| | `License` | `licenseInfo.spdxId` / `license.spdx_id` | `""` for none; `NOASSERTION` kept |
| | `Release` | `latestRelease{tagName name url isPrerelease publishedAt}` / `GET /releases/latest` | nil when none; drafts never; REST fallback rechecks at most every `limits.release_recheck` |
| | `HeadSHA` | `defaultBranchRef.target.oid` / commit SHA media type | `""` for an empty repository |
| | `HeadTreeOID` | `defaultBranchRef.target.tree.oid` | `""` on the REST fallback (documented as unknown, not as empty tree) |
| | `PushedAt` | `pushedAt` | |
| `scm.Commit` | `SHA` | `sha` | |
| | `Subject` | first line of `commit.message`, at most 256 runes | bounded display field (GR-30) |
| | `AuthorLogin` | top-level `author.login` | `""` when `author` is null, `{}` or lacks `login` (GR-29) |
| | `AuthorName` | `commit.author.name`, else `commit.committer.name`, else `""` | **no e-mail is ever read into a kernel type** (GR-35) |
| | `CommittedAt` | `commit.committer.date`, else `commit.author.date`, else zero | a zero date is valid output; the commits unit stores and orders by `(CommittedAt, SHA)` |
| | `URL` | `html_url` | |
| `scm.File` | `Path`, `SHA` | response `path`, `sha` (blob SHA) | `SHA` is verified: `BlobSHA(Data) == SHA` or the call fails `permanent/sha_mismatch` |
| | `Data` | decoded content | at most `limits.file_bytes`; beyond it `Truncated=true` and `Data` is the prefix |
| `scm.Tree` | `sha`, entries, truncated | `git/trees` response | `NewTree(sha, entries, truncated)`; `Truncated()` is the GitHub flag or the local `MaxEntries` cut |
| `scm.Blob` | `SHA`, `Data` | `git/blobs` raw | verified by `BlobSHA`; never returned unverified |
| `scm.Validators` | `SHA` | content validator | README/file: blob SHA; commits: head commit SHA; tree: tree SHA |
| | `ETag` | response `ETag` | **in-memory hint only** (credential-bound): the adapter sends it as `If-None-Match` only when the token fingerprint it was recorded under is the current one |
| `scm.Event` | all | `Verifier.Verify` | section 4.11; `ChangedPaths` is the sorted union over **all** commits' `added`, `removed`, `modified` (GR-21) |

### 3.5 Installation registry types (`github/installation.go`)

```go
package github

import (
	"context"
	"time"

	"github.com/cantara/source-code-portal/internal/github/auth"
	"github.com/cantara/source-code-portal/internal/scm"
)

// InstallationState is the per-installation state machine of research 09 section 1.7.
type InstallationState string

const (
	InstActive             InstallationState = "active"
	InstSuspended          InstallationState = "suspended"
	InstPermissionsPending InstallationState = "permissions_pending"
	InstRemoved            InstallationState = "removed"
	InstUnreachable        InstallationState = "unreachable" // token mint keeps failing for a reason other than 401/403/404
)

type Account struct {
	ID    int64
	Login string
	Type  string // Organization | User | Enterprise
}

// InstallationInfo is the read-only view served to /status through the ops bridge. It never carries a token.
type InstallationInfo struct {
	Source         scm.SourceID
	ID             scm.InstallationID
	Account        Account
	Selection      string // all | selected
	Permissions    auth.Permissions
	Events         []string
	State          InstallationState
	SuspendedAt    time.Time // zero when not suspended
	UpdatedAt      time.Time
	LastReconcile  time.Time
	RepoCount      int
	TokenExpiresAt time.Time // zero when no token is cached
	MintFailures   int
}

// Installations lists the registry of every App source.
func (c *Client) Installations() []InstallationInfo { return nil }

// ObserveEvent folds an installation / installation_repositories / repository event into the registry (state only, no
// network call). cmd/scp wires it as a reactor next to an Enqueue of github.installations; the job verifies through
// the API afterwards, so a forged-but-signed event cannot change what is served.
func (c *Client) ObserveEvent(ev scm.Event) {}

// ReconcileResult is what the github.installations job needs to build its job.Outcome.
type ReconcileResult struct {
	Installations int
	Repositories  int
	Changed       bool // any installation or repository count changed since the previous run => not Unchanged
}

// ReconcileInstallations is the body of the global job github.installations (30 min, accelerated by installation
// events): GET /app/installations with the JWT, a repository count per active installation, state transitions,
// the mass-removal guard of the installation set.
func (c *Client) ReconcileInstallations(ctx context.Context) (ReconcileResult, error) {
	return ReconcileResult{}, errNotImplemented
}
```

`InstallationInfo` is the read model of `/status`; it carries no token. `ReconcileResult.Changed` is what the job turns into `job.Outcome{Unchanged: !Changed}` so the planner's idle back-off applies to the installation job like to any other.

### 3.6 Doctor types (`github/doctor.go`)

```go
package github

import (
	"context"
	"time"

	"github.com/cantara/source-code-portal/internal/github/auth"
	"github.com/cantara/source-code-portal/internal/scm"
)

type Severity uint8

const (
	SevInfo Severity = iota
	SevWarn
	SevFail
)

// Finding is one doctor result (`--json` emits []Finding with these member names).
type Finding struct {
	ID          string   `json:"id"`       // "app.key", "inst.token", "rate", ...
	Severity    Severity `json:"severity"` // serialised as 0/1/2 by the CLI into info/warn/fail
	Source      string   `json:"source"`
	Title       string   `json:"title"`
	Detail      string   `json:"detail"`
	Remediation string   `json:"remediation"`
}

// Feature names the optional capabilities whose permissions doctor must see granted.
type Feature string

const (
	FeatureCore     Feature = "core" // metadata:read + contents:read
	FeaturePullReqs Feature = "pull_requests"
	FeatureIssues   Feature = "issues"
	FeatureStatuses Feature = "commit_statuses"
	FeatureChecks   Feature = "checks"
	FeatureActions  Feature = "actions"
)

// RequiredPermissions maps enabled features to the permission set the App must hold (never write, never admin).
func RequiredPermissions(fs ...Feature) auth.Permissions {
	p := auth.Permissions{"metadata": "read", "contents": "read"}
	for _, f := range fs {
		switch f {
		case FeaturePullReqs:
			p["pull_requests"] = "read"
		case FeatureIssues:
			p["issues"] = "read"
		case FeatureStatuses:
			p["statuses"] = "read"
		case FeatureChecks:
			p["checks"] = "read"
		case FeatureActions:
			p["actions"] = "read"
		}
	}
	return p
}

type DoctorOptions struct {
	Sources   []scm.SourceID // empty = all
	Offline   bool           // only config and file checks (no network)
	Features  []Feature
	PublicURL string        // server.public_url, compared with the App hook config
	Timeout   time.Duration // whole run, default 60s
}

// Doctor runs the GitHub-facing checks of research 09 section 8.4 (net.*, host.version, time.skew, app.key, app.hook,
// app.installations, inst.token, inst.read, rate, conditional, pat). cfg.*, store, webhook.self and git.* are run by
// cmd/scp. It only reads; it never changes anything on GitHub. Findings are sorted by source, then severity.
func (c *Client) Doctor(ctx context.Context, o DoctorOptions) []Finding { return nil }
```

`Finding.ID` values are stable strings (section 4.13 lists them); `scp doctor --json` serialises `[]Finding`, with `Severity` mapped to `"info"|"warn"|"fail"` by the CLI.

### 3.7 Rate book types

`RateSnapshot` (one bucket of one installation or credential: `Source`, `Installation`, `Resource`, `Limit`, `Remaining`, `Used`, `Reset`, `UpdatedAt`) is what `Client.Rates()` returns and what the `ops` bridge turns into `RateView`. `ReserveClass` (`ClassBackfill`, `ClassScheduled`, `ClassWebhook`, `ClassInteractive`) is the adapter-side image of `job.Priority`. Both are defined in `ratelimit.go`, which section 4.4 reproduces in full.

## 4. Behavioural specification

Conventions: pseudocode is Go-flavoured and normative for behaviour, not for identifiers; "returns X" means the typed error of section 3.3; `now` is `time.Now()` of the process (virtual under `testing/synctest` in tests; there is no `Clock` interface, ADR-003); every network call takes the caller's `ctx` and applies its own deadline; "pause" means a `*PauseError`.

### 4.1 Construction: `New`, sources and what happens at startup

`New(cfg Config, d Deps) (*Client, error)` makes **no network call** and starts **no goroutine**. In order, per source (all violations are collected and returned together, section 6.3):

1. `cfg.Validate()` (pure rules of section 6.3).
2. Derive URLs. `kind: github.com`: web `https://github.com`, API `https://api.github.com`, GraphQL `https://api.github.com/graphql`. `kind: ghec_dr` with subdomain `S`: web `https://S.ghe.com`, API `https://api.S.ghe.com`, GraphQL `https://api.S.ghe.com/graphql`. `kind: ghes` with `web_url W`: API `W/api/v3`, GraphQL `W/api/graphql`; `api_url`/`graphql_url` override. URLs are parsed once; the API base is stored without a trailing slash; paths are always joined with `url.JoinPath`-style escaping, never string concatenation of untrusted values (section 4.6 "path hygiene").
3. Resolve secrets: each `SecretRef` reads `file` (trailing newline trimmed, mode warning when group/world readable is a doctor finding, not an error here) (there is no `env` member: CD-CF-06). An **empty** result is "not configured" (`Secret.IsZero()`), never a credential (GR-13). A required but zero secret is a startup error naming the key path.
4. Parse App keys eagerly: PEM PKCS#1 or PKCS#8 RSA, at least 2048 bits, encrypted PEM refused, `\n` escape and base64 conveniences for single-line env values (`auth.ParsePrivateKey`). A parse error aborts startup (step 4 of 10.1 aborts on credential **format** errors, not on network errors).
5. Build the HTTP client with `Deps.NewHTTP(Network{...})`; build the token source of the strategy (4.5); build the breaker, semaphore, limiter, ETag memo, tree memo and installation registry.
6. Compute static capabilities (4.2.4).

`CheckCredentials(ctx)` is then called by `cmd/scp` (step 4): App sources `GET /app` with a JWT and load the installation list (4.12); PAT sources `GET /user`. A 401 is returned as an error carrying the exact GitHub message and the key fingerprint; `cmd/scp` decides what it means (architecture 10.1 step 4: fatal only on a cold start, otherwise the process serves the warm snapshot and reports the credential gate as failing in `/status`). A network failure is **not** an error here (readiness reports it, `/status` shows the upstream as unreachable). Nothing here retries; `scp doctor` is the thorough variant.

### 4.2 The request pipeline

Every GitHub call of the product goes through one function, `source.do`, so every property below holds for every call by construction (GR-22, GR-23).

#### 4.2.1 Order of operations

```
do(ctx, spec{Op, Resource, Method, Path|URL, Query, Accept, Body, Scope, Conditional, MaxBytes, Timeout}):
  // A. pre-flight, no network, no token
  if w := rates.hardWait(src, scope.Installation, spec.Resource, now); w > 0:
        return PauseError{Cause: rate_primary|rate_secondary, At: now+w}
  if breaker.State() == open:
        return PauseError{Cause: Error{KindTransient, Code:"breaker_open"}, At: breakerReopenAt}
  // B. admission (V-13: limiter first, then semaphore, so a slot is never held while waiting for a rate token)
  limiter.Wait(ctx)                     // x/time/rate: Budget.RPS, Budget.Burst per source
  acquire semaphore (Budget.Concurrency), select on ctx.Done(); release after the body is read (deferred)
  // C. credential
  tok, err := tokens.Token(ctx, scope, minValidity=0); map auth errors (4.5.5)
  // D. request
  req := new request: method, URL (validated, same origin), Accept, X-GitHub-Api-Version, User-Agent,
         Authorization: "Bearer " + tok.Reveal()  (omitted for anonymous), Content-Type for POST
         If-None-Match / If-Modified-Since only from the in-memory validator memo keyed by
         (tokenFingerprint, method, url, accept), only for GET, and ONLY WHEN THE CALLER SUPPLIED A VALIDATOR
         (spec.Have != "": v.SHA or v.ETag non-empty) that equals the one recorded in the memo entry (V-06, see 4.9)
  ctx2, cancel := WithTimeout(ctx, spec.Timeout or HTTP.Timeout|GraphQLTimeout)
  resp, err := breaker.Execute(func() { client.Do(req) })   // breaker counts only transient outcomes (4.2.3)
  // E. observe (always, even for errors): rate headers, Date skew, expiry header, request id, metrics, span
  rates.observe(src, scope.Installation, resp.Header, now)
  if exp := resp.Header["github-authentication-token-expiration"]; tokens implements ExpiryObserver: ObserveExpiry
  // F. outcome
  switch:
    2xx, 304                         -> rates.success(src); return resp (body bounded by spec.MaxBytes)
    401 && first attempt             -> tokens.Reject(scope, tok); redo C..F once, but only if Token() now returns a DIFFERENT
                                        value (a PAT, a command token that did not change and anonymous cannot be refreshed: V-13)
    401 && (second attempt || not refreshable)
                                     -> d := rates.authPause(src, now) (5 min, doubling to 1 h, one success resets);
                                        PauseError{Cause: KindAuth/bad_credentials, At: now+d}
    breaker refused (ErrOpenState)   -> PauseError{Cause: transient/breaker_open, At: openedAt+60 s}
    breaker refused (ErrTooManyRequests, half-open probes in flight)
                                     -> PauseError{Cause: transient/breaker_probing, At: now+1 s}  (V-13: the first draft left this case open)
    anything else                    -> classify(failure{...}) (4.3); transient kinds feed the breaker
```

`rates.observe` takes the bucket the call belongs to (`core` for REST, `graphql` for the GraphQL endpoint) as a fallback for hosts that omit `x-ratelimit-resource` (older GHES); the header wins when present (V-13).

The mint of an installation token is **not** outside this pipeline: `auth.AppConfig.HTTP` is an interface (`Do(*http.Request) (*http.Response, error)`) that `github` implements with steps A, B and E (pre-flight pause, limiter, semaphore, breaker, rate-book observation, metrics, span) but not C and not the 401 retry, so a GitHub outage opens the breaker through mint failures too and a primary or secondary limit seen on the mint call pauses the source (V-19).

The only retry in the adapter is the one-shot token refresh on 401 (an expired or revoked installation token). Everything else returns to the engine, which owns backoff with jitter (architecture 15). The adapter never sleeps in the request path.

#### 4.2.2 Headers

| Header | Value | Notes |
|---|---|---|
| `Accept` | `application/vnd.github+json` by default; per-operation media types listed in 4.6-4.10 | |
| `X-GitHub-Api-Version` | effective version (4.2.4) | never omitted: the implicit default will move (research 03 2.1) |
| `User-Agent` | `source-code-portal/<Deps.Version> (+<web host of the portal's public_url, if configured>)` | mandatory on GitHub; a missing agent is a 403 |
| `Authorization` | `Bearer <token>` | `Bearer` works for JWTs, installation tokens, PATs and OAuth tokens; header written only here; URL never carries a credential (GR-13) |
| `Content-Type` | `application/json` | POST bodies only |

#### 4.2.3 Breaker, limiter, semaphore (GR-22)

One of each **per source** (the upstream is the host, not the call), constructed once in `New` and shared by every call. `sony/gobreaker/v2` v2.4.0:

- Settings (compiled and tested against `sony/gobreaker/v2` v2.4.0): `MaxRequests: 3` (half-open probes; the breaker closes after 3 successes), a **rolling** failure window (`Interval: 60 s`, `BucketPeriod: 10 s`), open duration `Timeout: 60 s`, `ReadyToTrip`: 8 consecutive failures **or** at least 20 requests in the window with a failure ratio of at least 50 percent (evaluated by the library on each failure in the closed state). `IsSuccessful`/failure classification is done by the wrapper: only a network error, a timeout, a 5xx or a 408 counts as failure (the wrapper returns the real response to the caller and a marker error to the breaker); a 404, 403, 429, 401, rate-limit or other `permanent` response means GitHub answered, so it counts as success; `IsExcluded` ignores a caller's `context.Canceled`, which says nothing about GitHub. `OnStateChange` records the opening time (the library does not expose it), feeds `scp_github_breaker_state{upstream}` and logs once per transition. The library leaves the open state strictly after `Timeout`, so the engine's pause is `openedAt + 60 s` and the first probe may be a second later.

```go
package github

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/sony/gobreaker/v2"
)

// One breaker per source (GR-22: a breaker built per call never sees enough calls to open). It counts only outcomes that say
// "the upstream is unhealthy": network failures, timeouts, 5xx and 408. A 404, 403, 429, 401 or a rate-limit answer means
// GitHub responded and is healthy, and a canceled caller context says nothing about GitHub at all.
const (
	breakerOpenFor    = 60 * time.Second
	breakerWindow     = 60 * time.Second
	breakerBucket     = 10 * time.Second
	breakerHalfOpenN  = 3
	breakerConsec     = 8
	breakerMinSamples = 20
	breakerRatio      = 0.5
)

// errUpstream marks a response that counts against the breaker; the caller still receives the response and classifies it.
type errUpstream struct{ Status int }

func (e *errUpstream) Error() string { return "upstream unhealthy" }

type breaker struct {
	cb       *gobreaker.CircuitBreaker[*http.Response]
	mu       sync.Mutex
	openedAt time.Time
	onChange func(from, to string)
}

func newBreaker(name string, onChange func(from, to string)) *breaker {
	b := &breaker{onChange: onChange}
	b.cb = gobreaker.NewCircuitBreaker[*http.Response](gobreaker.Settings{
		Name:         name,
		MaxRequests:  breakerHalfOpenN,
		Interval:     breakerWindow,
		BucketPeriod: breakerBucket,
		Timeout:      breakerOpenFor,
		ReadyToTrip: func(c gobreaker.Counts) bool {
			if c.ConsecutiveFailures >= breakerConsec {
				return true
			}
			return c.Requests >= breakerMinSamples && float64(c.TotalFailures)/float64(c.Requests) >= breakerRatio
		},
		IsExcluded: func(err error) bool { return errors.Is(err, context.Canceled) }, // a caller's own cancellation says nothing about GitHub
		OnStateChange: func(_ string, from, to gobreaker.State) {
			b.mu.Lock()
			if to == gobreaker.StateOpen {
				b.openedAt = time.Now()
			}
			b.mu.Unlock()
			if b.onChange != nil {
				b.onChange(from.String(), to.String())
			}
		},
	})
	return b
}

// Do runs fn through the breaker. fn returns the response and a transport error; a response with status >= 500 or 408 is
// counted as a failure without hiding the response from the caller.
func (b *breaker) Do(fn func() (*http.Response, error)) (*http.Response, error) {
	var seen *http.Response
	_, err := b.cb.Execute(func() (*http.Response, error) {
		resp, err := fn()
		seen = resp
		if err != nil {
			return resp, err
		}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout {
			return resp, &errUpstream{Status: resp.StatusCode}
		}
		return resp, nil
	})
	var up *errUpstream
	if errors.As(err, &up) {
		return seen, nil // the response is real; the breaker has already counted it
	}
	return seen, err
}

// Open reports whether calls are refused right now and until when (the half-open probe time).
func (b *breaker) Open(now time.Time) (until time.Time, open bool) {
	if b.cb.State() != gobreaker.StateOpen {
		return time.Time{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.openedAt.Add(breakerOpenFor), true
}

// State is the metric value: 0 closed, 1 half-open, 2 open.
func (b *breaker) State() int {
	switch b.cb.State() {
	case gobreaker.StateHalfOpen:
		return 1
	case gobreaker.StateOpen:
		return 2
	}
	return 0
}

// refused reports whether err is the breaker refusing to call.
func refused(err error) bool {
	return errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests)
}
```

- Open breaker: no network call; `PauseError{Cause: transient/breaker_open, At: openedAt+Timeout}` so the engine pauses the upstream without consuming an attempt (architecture 15, "breaker open").
- Semaphore: capacity `budget.concurrency` (default 4, GitHub's secondary limit allows 100 and recommends serial use). Acquisition honours the caller's context.
- Limiter: `x/time/rate` token bucket, `budget.rps` (8) and `budget.burst` (16). Its `Wait` honours the context; it does not hold a semaphore slot while waiting, which is why the pipeline above acquires the limiter first and the semaphore second (the first draft's pseudocode said the opposite while the prose said this one: V-13).
- `refresh` additionally paces per upstream (`UpstreamConfig.Pace`, default 100 ms) and bounds workers; the adapter's limits are a second, independent line of defence for callers that bypass the engine (doctor, manifest flow, `scp` subcommands).

Retries inside the breaker callback: none. Backoff: none in the adapter (`cenkalti/backoff/v7` is used only by `doctor`'s bounded wait for a freshly created App's first token and by `gitsource`, not on the product path; if no such use survives implementation review the dependency is dropped and D-2 shrinks).

#### 4.2.4 API version and capabilities

`api_version: auto` resolves to `2026-03-10` on `github.com` and `ghec_dr`, and to `2022-11-28` on `ghes` (GHES documents REST versions as decoupled from the product release; the default must work on the oldest supported GHES). An explicit value is passed through. A 400 whose message names the version header or a 410 on a versioned request is `permanent/api_version_unsupported`; readiness fails with that message (research 03 2.1). Contract tests run the same fixtures for both versions (section 12.4).

`Capabilities` (compile-checked struct in 2.4) is computed per source:

| Field | Rule |
|---|---|
| `HostKind`, `APIVersion` | as above |
| `AppAuth` | `auth.kind == github_app` |
| `GraphQL` | `acquisition.graphql` is `on`, or `auto` and the credential is not anonymous and the last GraphQL probe did not fail with 404/400/401 within the 1 h cool-down; `off` always false |
| `GraphQLBlobs` | `acquisition.graphql_blobs == on` (staged, spike S-B; false by default) |
| `AsyncSBOM` | true on `github.com` and `ghec_dr`; false on `ghes` until verified (consumer: `manifests`, spike S-A) |
| `InternalVisibility` | `ghes` and `ghec_dr` |
| `OpenIssuesIncludePRs` | true when the head sweep runs on the REST fallback |
| `RedeliveryRetention` | 72 h (research 09 3.2; unverified, U-3) |

`Client.Capabilities()` describes the first source (single-source deployments are the norm); `SourceCapabilities(src)` is exact. `cmd/scp` consults it once at wiring and the planner never needs to branch on it: `HeadProber` hides REST versus GraphQL.

### 4.3 Error classification (GR-23)

One function, `classify`, turns a non-success response into a typed error. The table is architecture 15 made exact; the **fake GitHub's fault injection tests every row** (section 12.3).

| Upstream signal | `Kind` / `Code` | Returned as | Engine effect |
|---|---|---|---|
| `Retry-After` header on 403/429 | `RateLimited` / `rate_secondary`, wait = header (at least 1 s) | `*PauseError` | pause upstream, no attempt used |
| 403/429, `x-ratelimit-remaining: 0`, `x-ratelimit-reset` | `RateLimited` / `rate_primary`, wait = reset - now (at least 1 s), `ResetAt` set | `*PauseError` | same |
| 403/429 message contains "secondary rate limit" or "abuse detection", or any 429, and no other signal | `RateLimited` / `rate_secondary`, wait = per-source growing 60 s, 120 s, ... cap 15 min | `*PauseError` | same; repeats grow, reset by one success |
| 403 "Resource not accessible by integration" | `PermissionMissing` / `permission_missing` | `*Error` | `Permanent` (park, probe daily); doctor names the missing permission |
| 403 other (blocked repository, SAML/SSO, disabled) | `Permanent` / `forbidden` | `*Error` | park |
| 401 (after the single refresh) | `Auth` / `bad_credentials` | `*PauseError` (5 min, doubling to 1 h) | pause the upstream, critical `/status` entry |
| 404 on a repository-level resource | `NotFound`, `Subject=Repository` | `*Error` (`Gone()` true) | drop the key |
| 404 on a README, file, release, path | `NotFound`, `Subject=File` | `*Error` (`Gone()` false) | the **feature** turns it into the typed state `not_found`/`no_content` and returns success to the engine |
| 404 on an installation or an installation token mint | `NotFound`, `Subject=Installation` | `*Error` (`Gone()` true) | drop; registry state `removed` |
| 403 on token mint for a known installation, **not** a rate limit (no `Retry-After`, `x-ratelimit-remaining` not 0, message without "rate limit") | `Suspended` | `*Error` wrapping `auth.ErrInstallationUnavailable`; **no network call is made for it again until the registry says active** | pause that installation's keys, not the upstream |
| 404 on token mint | `NotFound`, `Subject=Installation` | `*Error` (`Gone()` true), registry state `removed` | drop; this row and the one above replace the first draft's single "403/404 -> Suspended" row, which contradicted the 404-on-installation row (V-04) |
| 403/429 on token mint that **is** a rate limit (V-03) | `RateLimited` / `rate_secondary` or `rate_primary`, wait = `Retry-After` / reset, else 60 s | `*PauseError` | pause upstream, no attempt used; the first draft classified every mint 403 as a suspension and would have stopped minting for the installation until the next reconcile |
| 422 on token mint (narrowed scope not grantable) | `Permanent` / `invalid_scope` | `*Error` | park |
| 409 on commits | not an error | empty `[]scm.Commit`, `RepoStats.HeadSHA == ""` | success (empty repository) |
| 301/307 same origin | followed (max 3) | counted `scp_github_redirect_total` | none |
| 301/307 other origin | `Permanent` / `foreign_redirect` | `*Error` | park; `Authorization` is never forwarded |
| 410, 451, 422 | `Permanent` / `status_<n>` | `*Error` | park |
| 400 naming the API version header | `Permanent` / `api_version_unsupported` | `*Error` | readiness failure |
| 5xx, 408, network error, timeout, TLS failure, undecodable 2xx body of a transient nature | `Transient` | `*Error` | full-jitter backoff; feeds the breaker |
| 2xx body that does not decode, or lacks a required member | `Permanent` / `undecodable` | `*Error` | park (a retry would read the same bytes) |
| response larger than the limit (JSON, tree, blob) | `Permanent` / `too_large` | `*Error` | park; READMEs are truncated instead (4.9) |
| caller context ended | `Canceled` | `*Error` wrapping `ctx.Err()` | not counted as a failure of the upstream |
| any other status | `Permanent` / `status_<n>` | `*Error` | park |

Rules that make this table safe: **no error text is ever parsed for control flow** except four documented GitHub phrases ("secondary rate limit", "abuse detection", "Resource not accessible by integration", and a mention of `X-GitHub-Api-Version` in a 400), and only after the structured signals (headers, status) have been tried (order in `classify`); messages are truncated to 200 bytes and token-redacted (`RedactTokens`); control characters are replaced; the `Error` carries `RequestID` (`x-github-request-id`) for support tickets.

```go
package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

// ErrKind is the closed classification of architecture section 15.
type ErrKind uint8

const (
	KindNotFound          ErrKind = iota + 1 // 404, and 409 "repository is empty" is NOT an error (see LatestCommits)
	KindAuth                                 // 401 after one token refresh; pauses the upstream
	KindPermissionMissing                    // 403 "Resource not accessible by integration"
	KindRateLimited                          // primary or secondary limit
	KindSuspended                            // installation suspended or removed (no network call is made)
	KindTransient                            // 5xx, timeouts, DNS, reset, breaker open
	KindPermanent                            // 403 other, 410, 451, 422, undecodable content
	KindCanceled                             // caller context ended
)

func (k ErrKind) String() string {
	switch k {
	case KindNotFound:
		return "not_found"
	case KindAuth:
		return "auth"
	case KindPermissionMissing:
		return "permission_missing"
	case KindRateLimited:
		return "rate_limited"
	case KindSuspended:
		return "suspended"
	case KindTransient:
		return "transient"
	case KindPermanent:
		return "permanent"
	case KindCanceled:
		return "canceled"
	}
	return "unknown"
}

// Subject says what a 404 refers to, so features can tell "the repository is gone" (Gone) from "the file is absent"
// (a typed state, not a failure).
type Subject uint8

const (
	SubjectUnknown    Subject = iota
	SubjectRepository         // repo-level resource: commits, tree, blob, repository itself
	SubjectFile               // README, contents path, rendered file, repository image
	SubjectInstallation
)

// Error is the single failure type of the adapter. It carries no secret: Message is a truncated, token-redacted
// excerpt of GitHub's "message" member, never a request header or URL query.
type Error struct {
	Kind       ErrKind
	Status     int
	Code       string // stable machine code: "rate_primary", "rate_secondary", "not_a_file", "tree_truncated", ...
	Op         string // resource label, same vocabulary as the metric label: "readme", "commits", ...
	Source     scm.SourceID
	Subject    Subject
	Message    string
	RequestID  string // x-github-request-id
	RetryAfter time.Duration
	ResetAt    time.Time
	Err        error // underlying cause (network error, auth.Error)
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("github ")
	b.WriteString(e.Op)
	b.WriteString(": ")
	b.WriteString(e.Kind.String())
	if e.Status != 0 {
		b.WriteString(" (status " + strconv.Itoa(e.Status) + ")")
	}
	if e.Code != "" {
		b.WriteString(" [" + e.Code + "]")
	}
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	return b.String()
}

// Unwrap lets errors.Is match the kernel sentinel of the kind and the underlying cause.
func (e *Error) Unwrap() []error {
	var out []error
	switch e.Kind {
	case KindNotFound:
		out = append(out, scm.ErrNotFound)
	case KindAuth, KindPermissionMissing:
		out = append(out, scm.ErrForbidden)
	case KindRateLimited, KindSuspended, KindTransient:
		out = append(out, scm.ErrUnavailable)
	case KindPermanent:
		if e.Status == http.StatusForbidden || e.Status == http.StatusUnavailableForLegalReasons {
			out = append(out, scm.ErrForbidden)
		}
	}
	if e.Err != nil {
		out = append(out, e.Err)
	}
	return out
}

// Permanent implements job.Permanent: retrying cannot help until content or permissions change.
func (e *Error) Permanent() bool { return e.Kind == KindPermanent || e.Kind == KindPermissionMissing }

// Gone implements job.Gone: the repository itself is not there (deleted, transferred away, invisible to this credential,
// or no installation covers its owner any more). A missing README or file is not Gone: it is a typed state.
func (e *Error) Gone() bool {
	return e.Kind == KindNotFound && (e.Subject == SubjectRepository || e.Subject == SubjectInstallation)
}

// Missing reports what a not-found error refers to ("repository", "file", "installation", "").
func (e *Error) Missing() string {
	switch e.Subject {
	case SubjectRepository:
		return "repository"
	case SubjectFile:
		return "file"
	case SubjectInstallation:
		return "installation"
	}
	return ""
}

// PauseError is an *Error that also implements job.RateLimited, so the engine pauses the whole upstream until RetryAt
// without consuming an attempt. It is returned for KindRateLimited, KindAuth (after one failed token refresh) and an
// open circuit breaker. A plain *Error never has RetryAt, otherwise errors.As would classify every failure as a pause.
type PauseError struct {
	Cause *Error // always non-nil; Kind is KindRateLimited, KindAuth or KindTransient (breaker open)
	At    time.Time
}

func (p *PauseError) Error() string {
	return p.Cause.Error() + " (paused until " + p.At.UTC().Format(time.RFC3339) + ")"
}
func (p *PauseError) Unwrap() []error    { return []error{p.Cause} }
func (p *PauseError) RetryAt() time.Time { return p.At }

// tokenRe matches the GitHub token families (research 09 section 8.1; ghs_ tokens are about 520 characters since 2026).
// The last alternative is an App JWT (three base64url segments, the header starts with eyJ): GitHub never echoes one, but
// a proxy in front of a GHES host might, and a JWT is a 10-minute credential.
var tokenRe = regexp.MustCompile(`\b(?:ghs_[A-Za-z0-9._-]{36,}|gh[pour]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})`)

// RedactTokens replaces anything that looks like a GitHub token. It is applied to every upstream string that reaches
// an error message or a log record.
func RedactTokens(s string) string { return tokenRe.ReplaceAllString(s, "[REDACTED]") }

// excerpt extracts GitHub's "message" member (or the raw start of the body), redacts it and caps it at 200 bytes.
func excerpt(body []byte) string {
	var m struct {
		Message string `json:"message"`
	}
	s := string(body)
	if json.Unmarshal(body, &m) == nil && m.Message != "" {
		s = m.Message
	}
	s = RedactTokens(strings.Map(func(r rune) rune {
		if r < 0x20 {
			return ' '
		}
		return r
	}, s))
	if len(s) > 200 {
		s = strings.ToValidUTF8(s[:200], "") // never cut inside a multi-byte rune
	}
	return s
}

// failure carries the response facts the classifier needs.
type failure struct {
	Op        string
	Source    scm.SourceID
	Subject   Subject
	Status    int
	Header    http.Header
	Body      []byte
	Now       time.Time
	Secondary func(now time.Time) time.Duration // per-source growing wait for secondary limits (60 s, 120 s, ... cap 15 min)
}

// classify implements the table of architecture section 15 for a non-success status. 404 on a README or release is
// still returned as an error here; the feature turns it into the typed state not_found/no_content.
func classify(f failure) error {
	base := &Error{Status: f.Status, Op: f.Op, Source: f.Source, Subject: f.Subject, RequestID: f.Header.Get("X-GitHub-Request-Id"), Message: excerpt(f.Body)}
	msg := strings.ToLower(base.Message)
	remaining, hasRemaining := atoiHeader(f.Header, "X-RateLimit-Remaining")
	reset := time.Time{}
	if v, ok := atoiHeader(f.Header, "X-RateLimit-Reset"); ok {
		reset = time.Unix(int64(v), 0).UTC()
	}
	switch {
	case f.Status == http.StatusForbidden || f.Status == http.StatusTooManyRequests:
		secondary := strings.Contains(msg, "secondary rate limit") || strings.Contains(msg, "abuse detection")
		var wait time.Duration
		switch {
		case f.Header.Get("Retry-After") != "": // rule order of GitHub's docs: retry-after first
			secs, _ := strconv.Atoi(f.Header.Get("Retry-After"))
			wait = time.Duration(max(secs, 1)) * time.Second
			base.Code, base.Kind = "rate_secondary", KindRateLimited
		case hasRemaining && remaining == 0 && !reset.IsZero():
			wait = max(reset.Sub(f.Now), time.Second)
			base.Code, base.Kind = "rate_primary", KindRateLimited
			base.ResetAt = reset
		case secondary || f.Status == http.StatusTooManyRequests:
			wait = f.Secondary(f.Now)
			base.Code, base.Kind = "rate_secondary", KindRateLimited
		case strings.Contains(msg, "resource not accessible by integration"):
			base.Kind, base.Code = KindPermissionMissing, "permission_missing"
			return base
		default:
			base.Kind, base.Code = KindPermanent, "forbidden" // blocked, SAML/SSO, disabled repository
			return base
		}
		base.RetryAfter = wait
		return &PauseError{Cause: base, At: f.Now.Add(wait)}
	case f.Status == http.StatusUnauthorized:
		base.Kind, base.Code = KindAuth, "bad_credentials"
		return base // the transport upgrades it to a PauseError after the single token refresh failed
	case f.Status == http.StatusNotFound:
		base.Kind = KindNotFound
		return base
	case f.Status == http.StatusGone, f.Status == http.StatusUnavailableForLegalReasons, f.Status == http.StatusUnprocessableEntity:
		base.Kind, base.Code = KindPermanent, fmt.Sprintf("status_%d", f.Status)
		return base
	case f.Status == http.StatusBadRequest && strings.Contains(msg, "x-github-api-version"):
		base.Kind, base.Code = KindPermanent, "api_version_unsupported"
		return base
	case f.Status >= 500 || f.Status == http.StatusRequestTimeout:
		base.Kind = KindTransient
		return base
	}
	base.Kind, base.Code = KindPermanent, fmt.Sprintf("status_%d", f.Status)
	return base
}

func atoiHeader(h http.Header, k string) (int, bool) {
	v := h.Get(k)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}
```

### 4.4 Rate book and the budget gate (G-03, ADR-015, GR-25)

Every response, success or failure, feeds `rateState.observe`. Buckets are keyed `(source, installation, resource)`, because an App's installations have independent limits and GraphQL and REST are separate buckets (research 03 2.2, 09 4.3). Headers are authoritative over `GET /rate_limit` (which is used by doctor only).

Three kinds of wait exist and all are returned, never slept:

1. **Hard wait** (`hardWait`): a pause recorded for the source (secondary limit or authentication failure), or an exhausted bucket whose reset lies in the future. Checked in step A of the pipeline: no request is sent at all.
2. **Secondary wait** (`secondaryWait`): the growing 60 s, 120 s, 240 s, 480 s, 900 s sequence, reset by one success. GitHub says to wait at least a minute and to back off when the limit persists; continuing while limited risks a ban.
3. **Gate wait** (`admit`): what `refresh` asks before starting a job of a priority. A job class may start only while `remaining/limit` of every `core` bucket exceeds the class threshold: backfill 40 percent, scheduled 15 percent, webhook 2 percent, interactive always unless the bucket is empty. The answer is the worst case over all sources and installations (the engine pauses per upstream name "github", not per source; section 15 Q-4), capped at 5 minutes so the engine asks again and notices fresh budget. A bucket whose reset has passed is ignored (it will refill on the next response).

```go
package github

import (
	"net/http"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

// ReserveClass is the adapter-side view of a job priority. The bridge in cmd/scp maps job.Priority to it, because
// github (rank 2) does not import job. Higher classes may use more of the remaining budget.
type ReserveClass uint8

const (
	ClassBackfill    ReserveClass = iota // cold start, manifests backfill: needs > Reserve.Backfill remaining
	ClassScheduled                       // planner ticks: needs > Reserve.Scheduled
	ClassWebhook                         // webhook accelerators: needs > Reserve.Webhook
	ClassInteractive                     // operator actions, doctor: always admitted unless the bucket is empty
)

// RateSnapshot is one bucket of one installation (or credential), updated from every response.
type RateSnapshot struct {
	Source       scm.SourceID
	Installation scm.InstallationID // 0 for PAT, OAuth, external, anonymous
	Resource     string             // core | graphql | search | ... (x-ratelimit-resource)
	Limit        int
	Remaining    int
	Used         int
	Reset        time.Time
	UpdatedAt    time.Time
}

type rateKey struct {
	src      scm.SourceID
	inst     scm.InstallationID
	resource string
}

// rateState is the per-client rate book. All methods are safe for concurrent use; time is passed in (synctest-friendly).
type rateState struct {
	mu      sync.Mutex
	buckets map[rateKey]RateSnapshot
	paused  map[scm.SourceID]time.Time // secondary-limit and auth pauses
	streak  map[scm.SourceID]int       // consecutive secondary limits, drives 60 s, 120 s, ... up to 15 min
	authN   map[scm.SourceID]int       // consecutive authentication pauses, drives 5 min, 10 min, ... up to 1 h
	reserve ReserveConfig
}

func newRateState(r ReserveConfig) *rateState {
	return &rateState{buckets: map[rateKey]RateSnapshot{}, paused: map[scm.SourceID]time.Time{}, streak: map[scm.SourceID]int{}, authN: map[scm.SourceID]int{}, reserve: r}
}

// observe folds the x-ratelimit-* headers of a response into the bucket. Headers are authoritative over /rate_limit.
//
// resource is the bucket the response belongs to when the host does not send x-ratelimit-resource (older GHES): "core" for
// REST calls, "graphql" for the GraphQL endpoint. The header wins when present.
func (s *rateState) observe(src scm.SourceID, inst scm.InstallationID, h http.Header, now time.Time, resource string) {
	res := h.Get("X-RateLimit-Resource")
	if res == "" {
		res = resource
	}
	limit, ok1 := atoiHeader(h, "X-RateLimit-Limit")
	rem, ok2 := atoiHeader(h, "X-RateLimit-Remaining")
	if res == "" || !ok1 || !ok2 {
		return
	}
	used, _ := atoiHeader(h, "X-RateLimit-Used")
	rst, _ := atoiHeader(h, "X-RateLimit-Reset")
	s.mu.Lock()
	s.buckets[rateKey{src, inst, res}] = RateSnapshot{Source: src, Installation: inst, Resource: res, Limit: limit, Remaining: rem, Used: used, Reset: time.Unix(int64(rst), 0).UTC(), UpdatedAt: now}
	s.mu.Unlock()
}

// secondaryWait returns the next growing wait (60 s, 120 s, ... cap 15 min) and records the pause.
func (s *rateState) secondaryWait(src scm.SourceID, now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.streak[src]
	s.streak[src] = n + 1
	w := min(time.Minute<<min(n, 4), 15*time.Minute)
	s.paused[src] = now.Add(w)
	return w
}

// authPause records the pause that follows a second 401 (the single token refresh already failed) and returns its length:
// 5 min, then 10, 20, 40, capped at 1 h while the failures continue; one success resets it (architecture 15, ADR-015).
func (s *rateState) authPause(src scm.SourceID, now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.authN[src]
	s.authN[src] = n + 1
	w := min(5*time.Minute<<min(n, 4), time.Hour)
	if t := now.Add(w); t.After(s.paused[src]) {
		s.paused[src] = t
	}
	return w
}

// pauseUntil records an externally decided pause (Retry-After, auth failure).
func (s *rateState) pauseUntil(src scm.SourceID, t time.Time) {
	s.mu.Lock()
	if t.After(s.paused[src]) {
		s.paused[src] = t
	}
	s.mu.Unlock()
}

// success resets the secondary streak after a 2xx/304.
func (s *rateState) success(src scm.SourceID) {
	s.mu.Lock()
	delete(s.streak, src)
	delete(s.authN, src)
	s.mu.Unlock()
}

// hardWait is the wait before ANY request of the (source, installation, resource) may be sent: an active pause, or an
// exhausted bucket whose reset lies ahead. Zero means send.
func (s *rateState) hardWait(src scm.SourceID, inst scm.InstallationID, resource string, now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	var w time.Duration
	if p := s.paused[src]; p.After(now) {
		w = p.Sub(now)
	}
	if b, ok := s.buckets[rateKey{src, inst, resource}]; ok && b.Remaining <= 0 && b.Reset.After(now) {
		w = max(w, b.Reset.Sub(now))
	}
	return w
}

// admit is the Gate: how long should the engine delay a job of this class? It looks at every source's `core` buckets
// and answers the worst case (the engine pauses per upstream name "github", not per source; section 4 open question).
// A wait is capped at maxGateWait so the engine re-asks regularly and notices fresh budget (webhook-free resets).
const maxGateWait = 5 * time.Minute

func (s *rateState) admit(c ReserveClass, now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	var worst time.Duration
	for src, p := range s.paused {
		if p.After(now) {
			worst = max(worst, p.Sub(now))
		}
		_ = src
	}
	threshold := map[ReserveClass]float64{ClassBackfill: float64(s.reserve.Backfill), ClassScheduled: float64(s.reserve.Scheduled), ClassWebhook: float64(s.reserve.Webhook), ClassInteractive: 0}[c]
	for k, b := range s.buckets {
		if k.resource != "core" || b.Limit <= 0 || !b.Reset.After(now) {
			continue // unknown or already reset: admit
		}
		frac := float64(b.Remaining) / float64(b.Limit)
		if b.Remaining <= 0 || frac <= threshold && c != ClassInteractive {
			worst = max(worst, b.Reset.Sub(now))
		}
	}
	return min(worst, maxGateWait)
}

func (s *rateState) snapshots() []RateSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RateSnapshot, 0, len(s.buckets))
	for _, b := range s.buckets {
		out = append(out, b)
	}
	return out
}
```

Budget arithmetic that the thresholds are meant to guarantee (research 09 section 9, 1,000 repositories, App at the 12,500/h cap): cold start 8,010 REST calls is 64 percent of an hour's budget, so the backfill threshold of 40 percent stops the backfill at roughly 60 percent consumed and lets scheduled and webhook work continue until 85 and 98 percent; steady state with healthy webhooks costs about 600 charged calls per hour. With a PAT (5,000/h) the same cold start takes 1.6 h and the gate spreads it. The fake GitHub counts calls so `TestBudgetColdStart` asserts the arithmetic (section 12.3).

Metrics fed from the same observation: `scp_github_rate_remaining`, `scp_github_rate_reset_seconds`, `scp_github_conditional_total{result}` (section 9).

### 4.5 Credentials (`github/auth`)

#### 4.5.1 Strategies

| `auth.kind` | Type | Behaviour |
|---|---|---|
| `github_app` | `auth.App` | App JWT, installation tokens per (installation, scope), cache, early refresh, single flight |
| `pat`, `fine_grained_pat` | `auth.Static` | one long-lived token from a file (re-read when the mtime changes) or env; `ObserveExpiry` from the expiry header |
| `oauth_user_token` | `auth.Static` | same, `Kind=oauth_user`; there is no interactive OAuth flow and no browser-automated token tool (GR-13) |
| `token_command` | `auth.External` | absolute argv, no shell, timeout 10 s, stdout at most 16 KiB `{"token":"...","expires_at":"RFC3339"}`, refresh 5 min before expiry (default TTL 10 min when absent) |
| `anonymous` | `auth.Anonymous` | no `Authorization` header; refused unless `dev: true`, refused on `ghec_dr` |

#### 4.5.2 App JWT

RS256 over a `crypto.Signer` (file key, env key, or a KMS handle). `iat = now - 60 s` (clock skew margin), `exp = now + 8 min` (GitHub allows at most 10), `iss` = client id (preferred) or numeric app id. A JWT is signed per use and never cached (signing costs microseconds; a cached JWT would need expiry handling). Why not `golang-jwt/jwt/v5`: v5.3.1 signs RS256 only with `*rsa.PrivateKey` (verified 2026-10-14 in the module source), which excludes `crypto.Signer` backed by a KMS; the 25-line encoder below is the entire dependency (D-1).

```go
package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// SignAppJWT builds the App JWT of research 09 section 1.3: RS256, iat = now-60s, exp = now+8m (GitHub allows at most
// 10 minutes), iss = client id (recommended) or app id. It takes a crypto.Signer so a KMS key works; golang-jwt/jwt/v5
// v5.3.1 only signs RS256 with *rsa.PrivateKey (verified in source on 2026-10-14), so a 30-line encoder replaces it.
func SignAppJWT(s crypto.Signer, iss string, now time.Time) (string, error) {
	hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(8 * time.Minute).Unix(),
		"iss": iss,
	})
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(hdr) + "." + enc.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := s.Sign(rand.Reader, digest[:], crypto.SHA256) // PKCS#1 v1.5 for RSA keys
	if err != nil {
		return "", fmt.Errorf("sign app jwt: %w", err)
	}
	return signing + "." + enc.EncodeToString(sig), nil
}
```

#### 4.5.3 Installation tokens

```go
package auth

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/cantara/source-code-portal/internal/platform/secret"
)

// Doer is the part of *http.Client the installation-token mint needs. github passes its own implementation (steps A, B and
// E of the request pipeline, section 4.2.1) so mint traffic feeds the breaker and the rate book like every other call.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// AppConfig configures the github_app strategy.
type AppConfig struct {
	AppID    int64  // numeric app id (iss fallback)
	ClientID string // preferred iss
	Slug     string // for CredentialInfo only
	APIBase  string // REST base without trailing slash, e.g. https://api.github.com or https://ghe.example/api/v3
	Keys     AppKeySource
	HTTP     Doer        // the source's pipeline without the credential step and the 401 retry (limiter, semaphore, breaker, rate book); NO Authorization injection
	Header   http.Header // constant request headers (Accept, X-GitHub-Api-Version, User-Agent)

	RefreshAt   time.Duration    // refresh when remaining validity is below this (default 10m, i.e. age 50m of 60m)
	JitterMax   time.Duration    // random extra margin up to this (default 60s), drawn from Rand01
	MintTimeout time.Duration    // detached mint deadline (default 20s)
	Rand01      func() float64   // must be safe for concurrent use: Token calls it outside any lock; github.New passes NewRand01(Deps.Seed)
	Now         func() time.Time // default time.Now; tests under synctest leave it nil
}

// App is the GitHub App strategy: App JWT, installation token minting, per-(installation, scope) cache.
type App struct {
	cfg   AppConfig
	sf    singleflight.Group
	mu    sync.Mutex
	cache map[string]Token // key = Scope.Key()
}

func NewApp(cfg AppConfig) (*App, error) {
	if cfg.Keys == nil || cfg.HTTP == nil || cfg.APIBase == "" {
		return nil, errors.New("auth: AppConfig needs Keys, HTTP and APIBase")
	}
	if cfg.ClientID == "" && cfg.AppID == 0 {
		return nil, errors.New("auth: AppConfig needs ClientID or AppID")
	}
	if cfg.RefreshAt == 0 {
		cfg.RefreshAt = 10 * time.Minute
	}
	if cfg.JitterMax == 0 {
		cfg.JitterMax = time.Minute
	}
	if cfg.MintTimeout == 0 {
		cfg.MintTimeout = 20 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand01 == nil {
		cfg.Rand01 = func() float64 { return 0.5 } // deterministic default for tests; production wiring passes NewRand01(seed)
	}
	return &App{cfg: cfg, cache: map[string]Token{}}, nil
}

func (a *App) iss() string {
	if a.cfg.ClientID != "" {
		return a.cfg.ClientID
	}
	return strconv.FormatInt(a.cfg.AppID, 10)
}

// JWT returns a fresh App JWT (one per use; signing is cheap, nothing is cached). It tries the keys in preference order
// only for signing; whether GitHub accepts the key is learned by the caller through a 401 and Rotate.
func (a *App) JWT(ctx context.Context) (secret.Secret, error) {
	signers, err := a.cfg.Keys.Signers(ctx)
	if err != nil || len(signers) == 0 {
		return secret.Secret{}, &Error{Err: ErrCredential, Message: "no usable app key"}
	}
	return a.jwtWith(signers[0])
}

func (a *App) jwtWith(s crypto.Signer) (secret.Secret, error) {
	j, err := SignAppJWT(s, a.iss(), a.cfg.Now())
	if err != nil {
		return secret.Secret{}, &Error{Err: ErrCredential, Message: err.Error()}
	}
	return secret.New(j), nil
}

// Token returns a cached installation token valid for at least max(minValidity, RefreshAt+jitter) or mints one.
// There is no background refresher (the adapter owns no goroutine): the first caller after the margin pays one mint.
func (a *App) Token(ctx context.Context, scope Scope, minValidity time.Duration) (Token, error) {
	if scope.Installation == 0 {
		return Token{}, &Error{Err: ErrNoCredential, Message: "installation scope required"}
	}
	key := scope.Key()
	margin := a.cfg.RefreshAt + time.Duration(a.cfg.Rand01()*float64(a.cfg.JitterMax))
	if minValidity > margin {
		margin = minValidity
	}
	a.mu.Lock()
	t, ok := a.cache[key]
	a.mu.Unlock()
	if ok && t.ExpiresAt.Sub(a.cfg.Now()) > margin {
		return t, nil
	}
	// Detached context with its own timeout: one caller's cancellation must not fail the others (singleflight has no ctx).
	ch := a.sf.DoChan(key, func() (any, error) {
		mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.cfg.MintTimeout)
		defer cancel()
		return a.mint(mctx, scope)
	})
	select {
	case r := <-ch:
		if r.Err != nil {
			return Token{}, r.Err
		}
		return r.Val.(Token), nil
	case <-ctx.Done():
		return Token{}, ctx.Err()
	}
}

// Reject drops the cached token only if it is still the rejected one (a fresh mint must survive a late 401).
func (a *App) Reject(scope Scope, rejected Token) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cur, ok := a.cache[scope.Key()]; ok && cur.Same(rejected) {
		delete(a.cache, scope.Key())
	}
}

func (a *App) Info() CredentialInfo {
	fp := ""
	if s, err := a.cfg.Keys.Signers(context.Background()); err == nil && len(s) > 0 {
		fp = Fingerprint(s[0].Public())
	}
	id := a.cfg.Slug
	if id == "" {
		id = a.iss()
	}
	return CredentialInfo{Kind: KindInstallation, Identity: id, KeyFingerprint: fp}
}

type mintBody struct {
	RepositoryIDs []int64     `json:"repository_ids,omitempty"`
	Permissions   Permissions `json:"permissions,omitempty"`
}
type mintResp struct {
	Token       string      `json:"token"`
	ExpiresAt   time.Time   `json:"expires_at"`
	Permissions Permissions `json:"permissions"`
	Selection   string      `json:"repository_selection"`
}

// mint tries each signer in order: a 401 means GitHub does not know that key (rotation in progress), so the next key
// is tried; 403/404 mean the installation is suspended or removed and are not retried with another key.
func (a *App) mint(ctx context.Context, scope Scope) (Token, error) {
	signers, err := a.cfg.Keys.Signers(ctx)
	if err != nil || len(signers) == 0 {
		return Token{}, &Error{Err: ErrCredential, Message: "no usable app key"}
	}
	var body mintBody
	for _, id := range scope.RepositoryIDs {
		body.RepositoryIDs = append(body.RepositoryIDs, int64(id))
	}
	body.Permissions = scope.Permissions
	payload, _ := json.Marshal(body)
	if len(body.RepositoryIDs) == 0 && len(body.Permissions) == 0 {
		payload = nil
	}
	var last error
	for _, s := range signers {
		jwt, err := a.jwtWith(s)
		if err != nil {
			return Token{}, err
		}
		tok, status, err := a.mintOnce(ctx, scope, jwt, payload)
		if err == nil {
			a.mu.Lock()
			a.cache[scope.Key()] = tok
			a.mu.Unlock()
			return tok, nil
		}
		last = err
		if status != http.StatusUnauthorized {
			break
		}
	}
	return Token{}, last
}

func (a *App) mintOnce(ctx context.Context, scope Scope, jwt secret.Secret, payload []byte) (Token, int, error) {
	u := fmt.Sprintf("%s/app/installations/%d/access_tokens", a.cfg.APIBase, scope.Installation)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return Token{}, 0, err
	}
	for k, v := range a.cfg.Header {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", "Bearer "+jwt.Reveal()) // JWTs require Bearer
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.cfg.HTTP.Do(req)
	if err != nil {
		return Token{}, 0, &Error{Err: ErrUnavailable, Message: "mint request: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	rid := resp.Header.Get("X-GitHub-Request-Id")
	switch {
	case resp.StatusCode == http.StatusCreated:
		var m mintResp
		if err := json.Unmarshal(raw, &m); err != nil || m.Token == "" {
			return Token{}, resp.StatusCode, &Error{Err: ErrUnavailable, Status: resp.StatusCode, Message: "undecodable mint response", RequestID: rid}
		}
		return Token{Value: secret.New(m.Token), Kind: KindInstallation, ExpiresAt: m.ExpiresAt, Permissions: m.Permissions, Selection: m.Selection}, resp.StatusCode, nil
	case resp.StatusCode == http.StatusUnauthorized:
		kind := ErrCredential
		msg := firstLine(raw)
		if strings.Contains(strings.ToLower(msg), "'iat'") || strings.Contains(strings.ToLower(msg), "'exp'") {
			kind = ErrClock
		}
		return Token{}, resp.StatusCode, &Error{Err: kind, Status: resp.StatusCode, Message: msg, RequestID: rid}
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		// A 403 is "suspended" only when it is not a rate limit: the structured signals come first (Retry-After,
		// x-ratelimit-remaining: 0), the phrase "rate limit" is the last resort.
		if wait, limited := mintRateLimit(resp.Header, firstLine(raw), a.cfg.Now()); limited {
			return Token{}, resp.StatusCode, &Error{Err: ErrRateLimited, Status: resp.StatusCode, Message: firstLine(raw), RequestID: rid, RetryAfter: wait}
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			return Token{}, resp.StatusCode, &Error{Err: ErrRateLimited, Status: resp.StatusCode, Message: firstLine(raw), RequestID: rid, RetryAfter: time.Minute}
		}
		return Token{}, resp.StatusCode, &Error{Err: ErrInstallationUnavailable, Status: resp.StatusCode, Message: firstLine(raw), RequestID: rid}
	case resp.StatusCode == http.StatusNotFound:
		return Token{}, resp.StatusCode, &Error{Err: ErrInstallationUnavailable, Status: resp.StatusCode, Message: firstLine(raw), RequestID: rid}
	case resp.StatusCode == http.StatusUnprocessableEntity:
		return Token{}, resp.StatusCode, &Error{Err: ErrInvalidScope, Status: resp.StatusCode, Message: firstLine(raw), RequestID: rid}
	default:
		return Token{}, resp.StatusCode, &Error{Err: ErrUnavailable, Status: resp.StatusCode, Message: firstLine(raw), RequestID: rid}
	}
}

// mintRateLimit recognises a rate-limited mint response and returns the advised wait (at least 1 s).
func mintRateLimit(h http.Header, msg string, now time.Time) (time.Duration, bool) {
	if v := h.Get("Retry-After"); v != "" {
		secs, _ := strconv.Atoi(v)
		return time.Duration(max(secs, 1)) * time.Second, true
	}
	if h.Get("X-RateLimit-Remaining") == "0" {
		if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			return max(time.Unix(reset, 0).Sub(now), time.Second), true
		}
		return time.Minute, true
	}
	if strings.Contains(strings.ToLower(msg), "rate limit") {
		return time.Minute, true
	}
	return 0, false
}

// NewRand01 returns a goroutine-safe source of floats in [0,1) seeded from Deps.Seed. App.Token calls Rand01 outside its
// mutex from many goroutines, and a bare *rand.Rand is not safe for concurrent use.
func NewRand01(seed uint64) func() float64 {
	var mu sync.Mutex
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	return func() float64 {
		mu.Lock()
		defer mu.Unlock()
		return r.Float64()
	}
}

func firstLine(raw []byte) string {
	var m struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &m) == nil && m.Message != "" {
		raw = []byte(m.Message)
	}
	s := string(raw)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = strings.ToValidUTF8(s[:200], "") // never cut inside a multi-byte rune
	}
	return s
}

var _ TokenSource = (*App)(nil)
```

Behavioural rules, each with a test in `auth_test.go` (section 12.1):

- **Cache key** `Scope.Key()`; memory only, never persisted, never logged. Tokens are about 520 characters on github.com since 2026 (research 09 1.4); nothing validates their length (a length assumption is exactly the kind of coupling that broke other clients).
- **Early refresh**: a cached token is reused while its remaining life exceeds `RefreshAt (10 min) + jitter (0..60 s)`; otherwise the first caller mints. There is **no background refresher**: the adapter owns no goroutine, and a token that is not used is not refreshed (an idle source costs nothing).
- **Single flight on a detached context**: `singleflight.DoChan` with a context derived from `context.WithoutCancel(ctx)` and its own `MintTimeout` (20 s), created inside the flight function so a non-leader cannot leak a cancel; each caller selects on its **own** `ctx.Done()`. One caller's cancellation or deadline cannot fail the others (architecture 8.7 and V15).
- **Key rotation**: `Signers` returns keys in preference order (current first). A 401 on mint means GitHub does not know that key, so the next key is tried; 403/404 mean suspended or removed and stop the loop. Both keys can be installed on the App during the overlap; after GitHub accepts the new key the old one is removed from config.
- **`Reject`** drops the cached token only if it still equals the rejected value.
- **Clock skew**: a 401 whose message mentions `iat` or `exp` becomes `ErrClock`; doctor's `time.skew` check (`Date` header against local time, fail over 60 s) names the cause.
- **Mint body**: omitted for a full-grant scope; `repository_ids` (at most 500) and `permissions` only for narrowed scopes.

Mapping of `auth.Error` to `github.Error` in step C of the pipeline: `ErrInstallationUnavailable` with status 403 -> `KindSuspended` (the registry is told, no call is made until it reports active) and with status 404 -> `KindNotFound`/`SubjectInstallation` (Gone, registry `removed`); `ErrRateLimited` -> `KindRateLimited` as a `PauseError` of `Error.RetryAfter`; `ErrCredential` and `ErrClock` -> `KindAuth` as `PauseError` (pause 5 min doubling to 1 h through `rates.authPause`); `ErrUnavailable` and `ErrCommand` -> `KindTransient`; `ErrNoCredential` -> `KindPermanent/no_credential`; `ErrInvalidScope` -> `KindPermanent/invalid_scope`.

**Mint classification (V-03).** A 403 on `POST /app/installations/{id}/access_tokens` is a suspension only when it is not a rate limit: `mintRateLimit` tries `Retry-After`, then `x-ratelimit-remaining: 0` with `x-ratelimit-reset`, then the phrase "rate limit" (last resort), exactly like `classify`; a 429 is always a rate limit; a 422 is `ErrInvalidScope`. Mint is also rate limited by GitHub (the docs list 403, 429 and 422 among the answers), and the first draft would have recorded a transient secondary limit as "installation suspended" and stopped minting for it until the next 30 min reconcile.

**Jitter source (V-07).** `Rand01` is called from `Token` outside the cache mutex by many goroutines, and a bare `*rand.Rand` is not safe for concurrent use. `auth.NewRand01(seed)` (PCG behind a mutex) is what `github.New` passes, seeded from `Deps.Seed`; the nil default of `NewApp` (a constant 0.5) exists for tests only.

#### 4.5.4 Static, external and anonymous sources

```go
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/secret"
)

// Static serves one long-lived token: a fine-grained or classic PAT, or an OAuth user token. When built with a path
// it re-reads the file when the mtime changes, so a rotated Kubernetes Secret takes effect without a restart.
type Static struct {
	kind    TokenKind
	ident   string
	path    string
	mu      sync.Mutex
	tok     secret.Secret
	stamp   time.Time
	expires time.Time
}

func NewStatic(kind TokenKind, identity string, tok secret.Secret) (*Static, error) {
	if tok.IsZero() { // empty string is "not configured" (GR-13)
		return nil, errors.New("auth: empty token")
	}
	return &Static{kind: kind, ident: identity, tok: tok}, nil
}

func NewStaticFile(kind TokenKind, identity, path string) (*Static, error) {
	s := &Static{kind: kind, ident: identity, path: path}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Static) reload() error {
	st, err := os.Stat(s.path)
	if err != nil {
		return err
	}
	if !s.stamp.IsZero() && st.ModTime().Equal(s.stamp) {
		return nil
	}
	t, err := secret.FromFile(s.path)
	if err != nil {
		return err
	}
	if t.IsZero() {
		return errors.New("auth: token file is empty")
	}
	s.tok, s.stamp = t, st.ModTime()
	return nil
}

func (s *Static) Token(_ context.Context, _ Scope, _ time.Duration) (Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path != "" {
		if err := s.reload(); err != nil {
			return Token{}, &Error{Err: ErrCredential, Message: err.Error()}
		}
	}
	return Token{Value: s.tok, Kind: s.kind, ExpiresAt: s.expires}, nil
}

// Reject does nothing: a static token cannot be refreshed; the transport reports Kind=Auth and pauses the upstream.
func (s *Static) Reject(Scope, Token) {}

func (s *Static) Info() CredentialInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return CredentialInfo{Kind: s.kind, Identity: s.ident, ExpiresAt: s.expires}
}

// ObserveExpiry implements ExpiryObserver.
func (s *Static) ObserveExpiry(t time.Time) {
	s.mu.Lock()
	s.expires = t
	s.mu.Unlock()
}

// Anonymous sends no Authorization header. Development only; the configuration refuses it on GHEC data residency.
type Anonymous struct{}

func (Anonymous) Token(context.Context, Scope, time.Duration) (Token, error) {
	return Token{Kind: KindAnonymous}, nil
}
func (Anonymous) Reject(Scope, Token) {}
func (Anonymous) Info() CredentialInfo {
	return CredentialInfo{Kind: KindAnonymous, Identity: "anonymous"}
}

// CommandRunner runs an external program without a shell and returns its stdout (at most 16 KiB). It is implemented in
// cmd/scp, the only package besides gitsource allowed to import os/exec (rule G6); the adapter receives it by injection.
type CommandRunner func(ctx context.Context, argv []string, timeout time.Duration) (stdout []byte, err error)

// ExternalConfig configures the token_command strategy: the command prints {"token":"...","expires_at":"RFC3339"}.
type ExternalConfig struct {
	Argv          []string      // absolute program path first; validated by config
	Timeout       time.Duration // default 10s
	RefreshBefore time.Duration // default 5m
	DefaultTTL    time.Duration // used when expires_at is absent, default 10m
	Run           CommandRunner
	Now           func() time.Time
}

type External struct {
	cfg ExternalConfig
	mu  sync.Mutex
	tok Token
}

func NewExternal(cfg ExternalConfig) (*External, error) {
	if cfg.Run == nil || len(cfg.Argv) == 0 {
		return nil, errors.New("auth: external source needs Argv and Run")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.RefreshBefore == 0 {
		cfg.RefreshBefore = 5 * time.Minute
	}
	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = 10 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &External{cfg: cfg}, nil
}

func (e *External) Token(ctx context.Context, _ Scope, minValidity time.Duration) (Token, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	margin := max(e.cfg.RefreshBefore, minValidity)
	if !e.tok.Value.IsZero() && e.tok.ExpiresAt.Sub(e.cfg.Now()) > margin {
		return e.tok, nil
	}
	out, err := e.cfg.Run(ctx, e.cfg.Argv, e.cfg.Timeout)
	if err != nil {
		// A failing refresh must not discard a token that is still valid for the request (the margin is a refresh
		// hint, not an expiry): serve it and let the next call retry the command.
		if !e.tok.Value.IsZero() && e.tok.ExpiresAt.Sub(e.cfg.Now()) > minValidity {
			return e.tok, nil
		}
		return Token{}, &Error{Err: ErrCommand, Message: err.Error()}
	}
	var r struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(out, &r); err != nil || r.Token == "" {
		return Token{}, &Error{Err: ErrCommand, Message: fmt.Sprintf("stdout is not {\"token\":...} JSON (%d bytes)", len(out))}
	}
	if r.ExpiresAt.IsZero() {
		r.ExpiresAt = e.cfg.Now().Add(e.cfg.DefaultTTL)
	}
	e.tok = Token{Value: secret.New(r.Token), Kind: KindExternal, ExpiresAt: r.ExpiresAt}
	return e.tok, nil
}

func (e *External) Reject(_ Scope, rejected Token) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.tok.Same(rejected) {
		e.tok = Token{}
	}
}
func (e *External) Info() CredentialInfo {
	return CredentialInfo{Kind: KindExternal, Identity: e.cfg.Argv[0]}
}

var (
	_ TokenSource    = (*Static)(nil)
	_ ExpiryObserver = (*Static)(nil)
	_ TokenSource    = Anonymous{}
	_ TokenSource    = (*External)(nil)
)
```

`External.Token` serves the cached token when a refresh fails while the token is still valid for the request (`expires - now > minValidity`); the 5 minute margin is a refresh hint, not an expiry, and the first draft turned one failed command run into an outage for a token that had minutes left (V-15). The command runs under the mutex, so concurrent callers queue behind one run (at most `Timeout`, 10 s) and do not honour their own `ctx` while queued: acceptable for a rarely refreshed credential, stated so nobody is surprised.

`NewStatic` rejects an empty token (an empty string is "not configured", GR-13); `NewStaticFile` re-stats the file on each `Token` call and reloads when the mtime changes, so a rotated Kubernetes Secret takes effect without a restart. `Static.Reject` is a no-op: a revoked PAT cannot be refreshed, so a second 401 pauses the upstream and `/status` shows a critical entry naming the credential (`Info()`), never the value.

#### 4.5.5 Keys

```go
package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/secret"
)

// ParsePrivateKey accepts PEM PKCS#1 ("RSA PRIVATE KEY", what GitHub issues) and PKCS#8 ("PRIVATE KEY", what vaults
// export), requires RSA of at least 2048 bits, and rejects encrypted PEM. Conveniences: "\n" escape sequences of a
// single-line env value are expanded; a value without a PEM header is tried as base64 of a PEM document.
func ParsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	s := strings.TrimSpace(string(data))
	if !strings.Contains(s, "-----BEGIN") {
		if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
			s = string(raw)
		}
	}
	s = strings.ReplaceAll(s, `\n`, "\n")
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	if _, enc := block.Headers["Proc-Type"]; enc || strings.Contains(block.Type, "ENCRYPTED") {
		return nil, errors.New("encrypted private keys are not supported")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS#1: %w", err)
		}
		key = k
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS#8: %w", err)
		}
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("key is %T, GitHub Apps require RSA", k)
		}
		key = rk
	default:
		return nil, fmt.Errorf("unsupported PEM type %q", block.Type)
	}
	if key.N.BitLen() < 2048 {
		return nil, fmt.Errorf("RSA key has %d bits, need at least 2048", key.N.BitLen())
	}
	return key, nil
}

// Fingerprint is base64 (standard alphabet) of sha256 over the PKIX (DER) public key, the value GitHub shows next to each
// key under "Credentials" and that `openssl rsa -in KEY.pem -pubout -outform DER | openssl sha256 -binary | openssl base64`
// prints (docs.github.com, managing private keys, checked 2026-10-14). Safe to print.
func Fingerprint(pub crypto.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(der)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// StaticKeys is an AppKeySource over keys that were parsed once (value from env, or tests).
type StaticKeys []crypto.Signer

func (k StaticKeys) Signers(context.Context) ([]crypto.Signer, error) { return k, nil }

// FileKeys loads one or more PEM files (current key first), re-stat'ing them at most every CheckEvery and reloading
// when the mtime or size changed, so a rotation by file swap needs no restart; cmd/scp also wires SIGHUP to Reload.
type FileKeys struct {
	Paths      []string
	CheckEvery time.Duration // default 30s
	mu         sync.Mutex
	stamp      []string
	keys       []crypto.Signer
	checked    time.Time
	lastErr    error
}

func (f *FileKeys) Signers(ctx context.Context) ([]crypto.Signer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	every := f.CheckEvery
	if every == 0 {
		every = 30 * time.Second
	}
	if f.keys != nil && time.Since(f.checked) < every {
		return f.keys, nil
	}
	keys, err := f.reloadLocked()
	if err != nil && f.keys != nil {
		f.lastErr = err
		return f.keys, nil // a bad rotation must not take the App down: keep serving the previous keys
	}
	f.lastErr = nil
	return keys, err
}

// LastReloadError is the most recent reload failure that was absorbed by keeping the previous keys (doctor, /status).
func (f *FileKeys) LastReloadError() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastErr
}

// Reload forces a re-read (SIGHUP).
func (f *FileKeys) Reload() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, err := f.reloadLocked()
	return err
}

func (f *FileKeys) reloadLocked() ([]crypto.Signer, error) {
	f.checked = time.Now()
	stamp := make([]string, len(f.Paths))
	for i, p := range f.Paths {
		st, err := os.Stat(p)
		if err != nil {
			return f.keys, fmt.Errorf("stat app key: %w", err)
		}
		stamp[i] = st.ModTime().String() + "/" + fmt.Sprint(st.Size())
	}
	if f.keys != nil && strings.Join(stamp, ";") == strings.Join(f.stamp, ";") {
		return f.keys, nil
	}
	var keys []crypto.Signer
	for _, p := range f.Paths {
		s, err := secret.FromFile(p)
		if err != nil {
			return f.keys, fmt.Errorf("read app key: %w", err)
		}
		k, err := ParsePrivateKey([]byte(s.Reveal()))
		if err != nil {
			return f.keys, fmt.Errorf("app key %s: %w", p, err)
		}
		keys = append(keys, k)
	}
	f.keys, f.stamp = keys, stamp
	return keys, nil
}
```

`FileKeys` re-stats at most every 30 s and reloads on mtime or size change; `Reload()` is wired to `SIGHUP`; a key file that fails to parse **keeps the previous keys in service** and returns the error (a bad rotation must not take the App down). The fingerprint printed by doctor and `/status` is the base64 SHA-256 of the PKIX DER public key, which is the value GitHub shows next to each key and that the documented `openssl ... | openssl base64` pipeline prints, so an operator can compare it directly (V-08; the first draft printed 16 hex characters that nothing on GitHub's side shows).

### 4.6 Path hygiene, pagination and cursors (shared by every operation)

- **Every value that becomes part of a URL path is validated and escaped.** `Owner` and `Name` pass `validOwnerName` (ASCII letters, digits, `-`, `_`, `.`; at most 100 bytes; not `.` or `..`) before use; repository file paths pass `escapeRepoPath`, which rejects empty and absolute paths, `.` and `..` segments, empty segments, backslash, NUL and control characters, and `url.PathEscape`s each segment; refs are escaped the same way (a branch may contain `/`, which is kept as a segment separator only in the query form `?ref=`; the path form is used for SHAs). A hostile repository file name or ref therefore cannot redirect a request (GR-21).
- **Query strings are built with `url.Values`**, never concatenated.
- **GraphQL documents contain no repository-controlled string**: owners and names travel as variables; only integers are formatted into the text (`buildHeadsQuery`).
- **`Link` pagination**: the next page is the `rel="next"` target of the `Link` header, accepted only if it is on the same origin as the API base (`nextLink` returns an error otherwise and the call fails `permanent/foreign_link`). The adapter never trusts `last` (it may be absent) and never loops without bound: `limits.max_pages` (50) caps every internal loop.
- **Cursors** are opaque to consumers: `Page.Next` is `base64url(JSON{v:1, i: installation index, p: "path?query" relative to the API base})`. It carries no credential and no host; `decodeCursor` refuses anything whose target does not start with `/`, contains `//` or `..`, so a tampered cursor cannot address another host or escape the base. A cursor is valid only for the process that issued it plus restarts of the same configuration (it contains no state beyond the next request), which is all `catalog.reconcile` needs (it never persists a cursor).

```go
package github

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// nextLink returns the rel="next" target of an RFC 8288 Link header as "path?query" relative to the API base, and only
// if it is on the same origin as base. A foreign origin is an error (never follow, never send Authorization there).
func nextLink(h http.Header, base *url.URL) (rel string, ok bool, err error) {
	for _, line := range h.Values("Link") {
		for _, part := range splitLinkValues(line) {
			seg := strings.Split(strings.TrimSpace(part), ";")
			if len(seg) < 2 {
				continue
			}
			isNext := false
			for _, p := range seg[1:] {
				p = strings.ReplaceAll(strings.TrimSpace(p), " ", "")
				if p == `rel="next"` || p == "rel=next" {
					isNext = true
				}
			}
			if !isNext {
				continue
			}
			raw := strings.TrimSpace(seg[0])
			if len(raw) < 3 || raw[0] != '<' || raw[len(raw)-1] != '>' {
				return "", false, errors.New("malformed Link header")
			}
			u, perr := url.Parse(raw[1 : len(raw)-1])
			if perr != nil {
				return "", false, errors.New("malformed Link target")
			}
			if u.Host != "" || u.Scheme != "" { // absolute or scheme-relative: must be exactly our origin
				if u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil {
					return "", false, fmt.Errorf("Link target on foreign origin %q", u.Host)
				}
			} else if !strings.HasPrefix(u.Path, "/") {
				return "", false, errors.New("relative Link target without leading slash") // never resolved against anything
			}
			prefix := strings.TrimSuffix(base.EscapedPath(), "/")
			full := u.EscapedPath()
			if prefix != "" && full != prefix && !strings.HasPrefix(full, prefix+"/") {
				return "", false, errors.New("Link target outside the API base path") // "/api/v30" is not under "/api/v3"
			}
			p := strings.TrimPrefix(full, prefix)
			if p == "" {
				p = "/"
			}
			if u.RawQuery != "" {
				p += "?" + u.RawQuery
			}
			return p, true, nil
		}
	}
	return "", false, nil
}

// cursor is the opaque Page.Next of ListRepositories: a version, the installation index (installation mode), and the
// next request target relative to the API base. It carries no credential.
type cursor struct {
	V int    `json:"v"`
	I int    `json:"i,omitempty"`
	P string `json:"p"`
}

func encodeCursor(c cursor) string {
	c.V = 1
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursor, error) {
	var c cursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, &c) != nil || c.V != 1 {
		return cursor{}, errors.New("invalid cursor")
	}
	if c.P != "" && (!strings.HasPrefix(c.P, "/") || strings.Contains(c.P, "//") || strings.Contains(c.P, "..")) {
		return cursor{}, errors.New("invalid cursor target") // a cursor can never address another host or escape the API base
	}
	return c, nil
}

// escapeRepoPath validates and escapes a repository-relative file path for a URL path. It rejects what could change the
// meaning of the request: empty or absolute paths, "." and ".." segments, backslashes, NUL and control characters.
func escapeRepoPath(p string) (string, error) {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00") {
		return "", errors.New("invalid path")
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if s == "" || s == "." || s == ".." {
			return "", errors.New("invalid path segment")
		}
		for _, r := range s {
			if r < 0x20 || r == 0x7f {
				return "", errors.New("control character in path")
			}
		}
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/"), nil
}

// buildHeadsQuery renders the head-sweep document for n repositories. Only integers are formatted into the text;
// owners and names travel as variables, so no repository-controlled string is ever concatenated into GraphQL.
func buildHeadsQuery(n int) string {
	var b strings.Builder
	b.WriteString("query ScpHeads(")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "$o%d: String!, $n%d: String!", i, i)
	}
	b.WriteString(") {\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  r%d: repository(owner: $o%d, name: $n%d) { ...ScpHead }\n", i, i, i)
	}
	b.WriteString("  rateLimit { cost remaining resetAt }\n}\n")
	b.WriteString(`fragment ScpHead on Repository {
  databaseId
  pushedAt
  stargazerCount
  licenseInfo { spdxId }
  issues(first: 1, states: OPEN) { totalCount }
  latestRelease { tagName name url isPrerelease publishedAt }
  defaultBranchRef { target { ... on Commit { oid tree { oid } } } }
}
`)
	return b.String()
}

// contentsJSONLimit is the response bound for the JSON form of the contents and readme endpoints. GitHub sends `content`
// only for files up to 1 MiB (larger files answer encoding "none" with an empty content), wraps the base64 at 60
// characters (a 1.78 percent overhead measured on 2026-10-14) and adds a few KiB of metadata, so the bound is derived from
// max(fileBytes, 1 MiB) with a factor 1.5 and 16 KiB. The earlier bound fileBytes*4/3+4 KiB rejected every file above
// about 97 percent of the limit as too large instead of returning (or truncating) it.
func contentsJSONLimit(fileBytes int64) int64 {
	return max(fileBytes, 1<<20)*3/2 + 16<<10
}

// splitLinkValues splits a Link header line into its link-values at the commas that precede the next "<": a comma
// inside a target URI (RFC 8288 allows it in a query) must not tear a value apart.
func splitLinkValues(line string) []string {
	var out []string
	start := 0
	inURI := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '<':
			inURI = true
		case '>':
			inURI = false
		case ',':
			if !inURI {
				out = append(out, line[start:i])
				start = i + 1
			}
		}
	}
	return append(out, line[start:])
}
```

### 4.7 `ListRepositories` (G-01, GR-26, GR-35)

`ListRepositories(ctx, src, cursor, v) (Result[Page[Repository]], error)` returns **one page per call**; `catalog.reconcile` loops until `Next == ""`, accumulates, and applies the set **only if every page succeeded** (fail-closed: any error means "keep the last catalogue", GR-26). The adapter therefore never returns a partial catalogue as success.

Request by `discovery.mode`:

| Mode | First request | Next | Notes |
|---|---|---|---|
| `installation` | for the `i`-th active allowed installation: `GET /installation/repositories?per_page=100` with that installation's token | `Link next`; when exhausted, `i+1` | canonical list of what the installation can see; full repository objects; `total_count` and `repository_selection` read on the first page |
| `org` | `GET /orgs/{org}/repos?type=public|all&sort=full_name&direction=asc&per_page=100` | `Link next` | `type=public` when `visibility: public_only`; sorted by `full_name` so concurrent pushes cannot duplicate or skip entries (research 03 2.5) |
| `user` | `GET /users/{login}/repos?type=owner|public&sort=full_name&direction=asc&per_page=100` | `Link next` | for personal accounts |
| `explicit` | `GET /repos/{owner}/{name}` for up to 100 entries per call | cursor index into `discovery.repos` | for tiny deployments and tests; a 404 entry is skipped and counted, not fatal |

Filtering inside the adapter (so `catalog` never sees what policy forbids): `disabled` repositories are always dropped; `archived` unless `include_archived`; `fork` unless `include_forks`; and the **visibility policy**: with `visibility: public_only` (the default) only `visibility == "public"` repositories leave the adapter; with `installation_scope` everything the credential can see is returned, and the **catalogue and `access` enforce who may see it** (architecture 17.3; the adapter's job is to return the truth, not to decide audiences). Counters: `scp_github_skipped_total{reason=disabled|archived|fork|private|bad_name|no_default_branch}`.

Minimal repository objects may lack optional members; `default_branch` missing triggers one `GET /repos/{o}/{r}` per such repository (bounded by `max_pages` x 100 and counted).

Validators: the first page may be requested conditionally (`If-None-Match` from `v.ETag` or the in-memory memo) and a 304 returns `Result{NotModified: true}` with an empty page. Lists change whenever any repository is pushed (`pushed_at` is in each element), so a 304 is rare on an active organisation; the design does not depend on it. Persisted validators for this call are not meaningful across token lifetimes and `catalog` stores none.

Errors: 404 on the organisation is `NotFound` with `Subject=Unknown` (the catalogue decides; it is not `Gone`: no repository is implicated); 401/403/rate limits as in 4.3; an installation whose token mint says suspended or removed is skipped with a counted reason and reflected in the registry, **and the call returns the remaining installations' repositories plus a typed `*Error{Kind: Suspended}` only if every installation failed** (a suspended installation is normal and its repositories simply disappear, which the mass-removal guard of `catalog.reconcile` judges).

### 4.8 `ProbeHeads` (G-03, ADR-004)

`ProbeHeads(ctx, repos) (map[RepoID]RepoStats, error)` answers "what changed?" for many repositories cheaply. The caller does not know whether GraphQL or REST ran.

**Contract**: the map holds an entry for every repository the call could read; a repository **absent from the map is "no information"** (not visible, not found, or failed), never a deletion signal (deletion is reconcile's decision). The error is non-nil when anything failed and describes the **first** failure; callers apply the map and then act on the error (a `PauseError` pauses the upstream). Repositories are grouped by **(source, installation)** and chunked inside each group; groups are processed independently; the call is bounded by `limits.head_batch` per query. The installation of a repository is the one whose account login equals its owner (4.12), because an installation token can read only its own repositories: a chunk that mixed two installations would answer `NOT_FOUND` for half of its aliases under one token (V-12; the first draft said "grouped by source" and left the token choice open). PAT, OAuth, command and anonymous sources have one group (`Installation == 0`); a repository whose owner has no active installation is absent from the map and counted.

**GraphQL path** (`Capabilities().GraphQL`): chunks of `head_batch` (50) repositories, one POST per chunk to the GraphQL endpoint, resource bucket `graphql`:

```graphql
query ScpHeads($o0: String!, $n0: String!, ... $o49: String!, $n49: String!) {
  r0: repository(owner: $o0, name: $n0) { ...ScpHead }
  ...
  rateLimit { cost remaining resetAt }
}
fragment ScpHead on Repository {
  databaseId pushedAt stargazerCount
  licenseInfo { spdxId }
  issues(first: 1, states: OPEN) { totalCount }
  latestRelease { tagName name url isPrerelease publishedAt }
  defaultBranchRef { target { ... on Commit { oid tree { oid } } } }
}
```

(The exact document is produced by `buildHeadsQuery(n)`, tested.) Cost by the documented formula: each repository contributes the `issues` connection (1 request); `latestRelease` and `defaultBranchRef` are single objects, so a page of 50 costs about 50/100, rounded, minimum 1: **about 1 point per 50 repositories**, 20 points for 1,000 repositories (the ADR-004 figure); the response `rateLimit.cost` is recorded in the call ledger and metric so the estimate is checked against reality (U-5: `rateLimit(dryRun: true)` is not used because its alias behaviour is undocumented). Fields confirmed against the public schema on 2026-10-14.

Response handling:

| Response | Action |
|---|---|
| HTTP 200, `data.rN` object | map entry: `RepoID = databaseId`, stats as in 3.4; `pushedAt`, `HeadSHA`, `HeadTreeOID` from the default branch commit; a null `defaultBranchRef` (empty repository) gives `HeadSHA == ""` |
| HTTP 200, `data.rN == null` and an `errors[]` entry with `type: NOT_FOUND` and `path[0] == "rN"` | repository absent from the map; counted (`scp_github_probe_missing_total`) |
| `errors[].type` `RATE_LIMITED` (or HTTP 403/429) | `*PauseError` until `rateLimit.resetAt` / the headers; the map keeps what earlier chunks produced |
| `errors[].type` `MAX_NODE_LIMIT_EXCEEDED`, `RESOURCE_LIMITS_EXCEEDED`, or HTTP 502/504 (GitHub's 10 s cut-off, which also deducts points from the next hour) | **halve the chunk and retry the halves once**; a second failure at size 1 marks the repository missing and records the transient error |
| `errors[].type` `FORBIDDEN`, `INSUFFICIENT_SCOPES` | `*Error{PermissionMissing}` for those aliases only; others in the chunk still count |
| HTTP 404/400/401 on the endpoint itself (GraphQL unsupported on an old GHES, or token lacks GraphQL) | GraphQL capability off for 1 h, continue **on the REST path** for this call |
| undecodable body | `permanent/undecodable`, REST path for this call |

**REST fallback** (`GraphQL == false`, or on fallback): per repository, three conditional calls charged to `core` at most: `GET /repos/{o}/{r}` (stars, licence, `open_issues_count` which **includes pull requests**, `pushed_at`, `default_branch`, `id`), `GET /repos/{o}/{r}/commits/{default}` with `Accept: application/vnd.github.sha` (head SHA only), and `GET /repos/{o}/{r}/releases/latest` at most every `limits.release_recheck` (1 h; 404 means no release). `HeadTreeOID` stays `""` (unknown). Calls run under the source semaphore, so a 1,000-repository REST sweep is paced by `budget.concurrency` and `budget.rps` and by the gate.

`Capabilities().OpenIssuesIncludePRs` is true on this path so `badges` can label the count "open issues + PRs" (research 03 2.11).

### 4.9 `LatestCommits`, `Readme`, `ReadFile`, `RenderedFile` (GR-10, GR-21, GR-29, GR-37)

**`LatestCommits(ctx, r, ref, n, v)`**

```
if v.SHA != "":   // cheap change check: the commit-SHA media type, validator is the SHA itself (token independent)
    GET /repos/{o}/{r}/commits/{ref or HEAD}   Accept: application/vnd.github.sha   If-None-Match: "<v.SHA>"
    304  -> return Result{NotModified: true, Validators{SHA: v.SHA}}
    404  -> NotFound(Subject=Repository)            // Gone
    409  -> empty repository: return Result{Value: [], Validators{SHA: ""}}
    200  -> head := body (40 hex)
GET /repos/{o}/{r}/commits?per_page=<min(n,100)>[&sha=<ref>]     // ref == "" = default branch (GR-10)
409  -> empty slice, success
decode each item with tolerant optional members (3.4, GR-29); drop an item without `sha`
return Result{Value: commits, Validators{SHA: commits[0].SHA}}
```

`n` is capped at 100 (one page); the caller keeps its own retention ring. The branch is **resolved by the caller** from `scm.Repository.DefaultBranch` or the group's `ref` override; the adapter never invents `master` (GR-10). It does not parse e-mail addresses into any result (GR-35). Whether a `304` on the SHA media type is free of the primary limit is undocumented (U-4); the design does not depend on it because `ProbeHeads` already supplies `HeadSHA` and the planner calls the commits job only when it changed.

**`Readme(ctx, r, ref, v)`** and **`ReadFile(ctx, r, ref, path, v)`**

```
GET /repos/{o}/{r}/readme[?ref=<ref>]          (Readme)
GET /repos/{o}/{r}/contents/<escaped path>?ref=<ref>   (ReadFile)
Accept: application/vnd.github+json             // JSON form: carries name, path, sha, size, encoding, content
bound the response at contentsJSONLimit(limits.file_bytes) = max(file_bytes, 1 MiB) * 3/2 + 16 KiB   // V-05, see below
404 -> NotFound(Subject=File)                    // the FEATURE turns it into state `not_found`; never a blank page (GR-37)
array body (a directory) or type != "file"       -> Permanent("not_a_file") // a directory answers with an array: never decode an array as an object
type == "symlink" or "submodule"                 -> Permanent("unsupported_kind")
if encoding == "base64": decode; verify BlobSHA(data) == sha        // mismatch -> Permanent("sha_mismatch")
if encoding == "none" (file over 1 MiB):         GET again with Accept: application/vnd.github.raw+json, read at most
                                                 limits.file_bytes+1 bytes; File{Truncated: true, Data: prefix}; SHA from the JSON
if v.SHA == sha:                                 return Result{NotModified: true, Validators{SHA: sha}} (Value empty)
return Result{Value: File{Path, SHA, Data, Truncated}, Validators{SHA: sha, ETag: <memo only>}}
```

**Response bound and truncation (V-05).** GitHub sends `content` only for files up to 1 MiB (larger ones answer `encoding: "none"` with an empty `content`, docs.github.com 2026-10-14), wraps the base64 text at 60 characters (a line feed every 60 characters, measured on `GET /repos/golang/go/readme` on 2026-10-14: ratio 1.0177 over the bare 4/3), and adds about 1 KiB of metadata. The first draft bounded the body at `file_bytes * 4/3 + 4 KiB`, which a 1 MiB file exceeds by about 20 KB, so every file above roughly 97 percent of the limit would have failed `too_large` instead of being returned or truncated, and a configured `file_bytes` below 1 MiB would have turned the documented truncation into an error for every file between the limit and 1 MiB. The JSON bound therefore does not depend on `file_bytes` below 1 MiB (`contentsJSONLimit`, tested against a 60-column-wrapped 1 MiB body); after decoding and SHA verification the adapter truncates `Data` to `limits.file_bytes` and sets `Truncated` (the blob SHA still identifies the whole file). `base64.StdEncoding` ignores the line feeds. That GitHub wraps at 60 columns is observed, not documented (U-11); the factor 1.5 tolerates any plausible wrapping.

A README at any case or extension (`README.rst`, `readme.markdown`, ...) is found by GitHub's own resolution; the adapter reports `File.Path` so `docs` can choose the renderer by the **actual** name, case-insensitively (GR-37). The conditional header is sent only when (a) the in-memory memo holds an ETag recorded under the **current** token fingerprint, so an App token rotation is a clean 200 instead of a futile conditional request, **and (b) the caller passed a validator (`v.SHA != ""`) equal to the blob SHA the memo recorded next to that ETag** (V-06). Without (b) the adapter would answer `NotModified` with an empty `Value` to a caller that holds nothing: the memo outlives the caller's stored row (the item was removed and re-added after a reconcile, the store was quarantined and rebuilt, a write failed), and the caller would then keep "no content" until the next content change. Rule of thumb: `NotModified` is only ever returned to a caller who proved, by passing the validator, that it still has the value; the persisted validator is the blob SHA from the response body (ADR-004). Refs are escaped; `ref` empty means the default branch.

**`RenderedFile(ctx, r, ref, path, v)`** (AsciiDoc bridge): `GET .../readme` (when `path` is empty or the README) or `GET .../contents/<path>` with `Accept: application/vnd.github.html+json`; the body is **GitHub-rendered HTML as an untrusted string**, limited to `limits.file_bytes * 4`; relative links are not rewritten by GitHub (research 03 2.8) and `docs` rewrites and sanitises. Whether the contents endpoint honours the HTML media type for non-README files is **unverified** (U-6); the fake implements it, the real call is exercised once by the opt-in live smoke test and the feature degrades to "unsupported format, text" on `415`/`422`. `Validators` are weak: only the in-memory ETag; `NotModified` is reported only when a conditional request under the same credential returned 304.

### 4.10 `ListTree`, `ReadBlob`, `ReadRepoImage` (D-15, ADR-004, ADR-022)

**`ListTree(ctx, r, ref, opt)`**

```
key := (source, owner/name, ref, opt.Recursive, opt.Path)
immutable := ref is a 40-hex tree or commit SHA   -> result cached in the immutable LRU (limits.immutable_max), no TTL
else: shared memo, TTL limits.tree_memo_ttl (60 s), at most limits.tree_memo_max entries (oldest evicted)
single flight: DoChan(key) with ctx = WithoutCancel(first caller) + timeout; each caller selects on ITS OWN ctx.Done()
GET /repos/{o}/{r}/git/trees/{ref}?recursive=1        (opt.Recursive)   |   without recursive (one level)
        // ref == "" is sent as HEAD (GET .../git/trees/HEAD answered 200 on 2026-10-14; V-17); a branch name, a tag or a SHA is passed escaped
body bound limits.tree_bytes (16 MiB; GitHub caps at 7 MB); entries > opt.MaxEntries (default 100000) are cut and Truncated=true
"truncated": true from GitHub -> Tree.Truncated() true; the adapter does NOT silently walk (the caller decides:
        `manifests` falls back to targeted non-recursive reads of the manifest directories via opt.Path, `docs` lists docs/)
opt.Path != "": resolve by walking one segment at a time from the root tree (non-recursive), then list that subtree
entries: type blob|tree|commit -> EntryBlob|EntryTree|EntryCommit; mode "120000" -> EntryLink
return Result{Value: scm.NewTree(sha, entries, truncated), Validators{SHA: tree sha}}
```

The memoised value is the opaque, immutable `scm.Tree` (no exported slice), so `docs` and `manifests` share one call without a race (architecture 8.7). A failed flight is not memoised (the next caller retries); `NotFound` on the repository is memoised for 5 s to absorb a burst of two features.

**`ReadBlob(ctx, r, sha, maxBytes)`**: `GET /repos/{o}/{r}/git/blobs/{sha}` with `Accept: application/vnd.github.raw+json`, body read through a `maxBytes+1` limit (`permanent/too_large` above; `maxBytes <= 0` means `limits.file_bytes`); the result is verified `BlobSHA(data) == sha` and a mismatch is `permanent/sha_mismatch` and is **never returned** (D-15: manifests are immutable by SHA, so a wrong byte must not be cached forever). Not memoised here: the store's blob cache (bounded, `store.cache_caps.blob`) owns caching. `sha` must match `^[0-9a-f]{40}$` (SHA-256 repositories, 64 hex, are accepted by the same check on length 40 or 64).

**`ReadBlobs(ctx, r, shas, maxBytes)`** (CD-IF-01, optional `manifests.BlobBatchReader`): the GraphQL batch of `ReadBlob`; see the signature comment in 2.3. It is used only when `Capabilities().GraphQL` is true and `acquisition.graphql_blobs` is on; otherwise `cmd/scp` leaves `Deps.BlobBatch` nil and `manifests` reads blobs singly. The fake GitHub (7.8) must resolve `object(oid:){... on Blob{...}}` for it (CD-FX-02).

**`ReadRepoImage(ctx, r, ref, path, maxBytes)`**: `GET /repos/{o}/{r}/contents/<path>?ref=` with the raw media type, bounded by `maxBytes` (the `media` limit, 2 MiB default); `type` must be a file; returns `File{Path, SHA: BlobSHA(data), Data}`. `media` sniffs the type and sanitises SVG; the adapter does not trust a content type from GitHub for raw responses.

### 4.10a Content verification primitive

```go
package github

import (
	"crypto/sha1" //nolint:gosec // git object ids are SHA-1; this is identification, not security
	"encoding/hex"
	"strconv"
)

// BlobSHA computes the git blob object id of data: sha1("blob <len>\x00" + data). The adapter verifies every blob it
// reads by SHA with it (D-15), and derives the content validator of a README from the bytes it received instead of
// trusting an ETag (ETags are credential-bound, ADR-004).
func BlobSHA(data []byte) string {
	h := sha1.New() //nolint:gosec
	h.Write([]byte("blob " + strconv.Itoa(len(data)) + "\x00"))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
```

Every blob and file the adapter returns is verified with `BlobSHA`, and the README validator is derived from received bytes instead of trusting an ETag (ADR-004).

### 4.11 Webhooks: verify, decode, route, redeliver (G-06, GR-20, GR-21)

The receiver (`webhook`, a different unit) enforces the order **limit -> verify -> decode -> dedupe-after-enqueue**; this unit provides `Verifier.Verify`, which performs the verification and decoding steps in a fixed order and returns only typed outcomes.

**Order inside `Verify(src, header, body)`** (each failure is a `*WebhookError` with a stable `Reason()` that becomes the `reason` label and a log field):

1. source known (`unknown_source`, treated as invalid signature: no information about configured sources leaks through the status code);
2. body size at most `max_body` (second guard behind the receiver's `MaxBytesReader`; GitHub itself delivers nothing above 25 MB) (`body_too_large`);
3. **signature before any parsing**: header `X-Hub-Signature-256` present (`missing_signature`), form `sha256=<64 hex>` (`bad_signature_format`), HMAC-SHA-256 of the **raw bytes** under **each** configured secret (current and previous during rotation), compared with `hmac.Equal`, evaluating every secret without early exit (`signature_mismatch`). The SHA-1 `X-Hub-Signature` header is never read (GR-20). An empty secret is skipped; a source with no non-empty secret cannot verify anything, and startup validation refuses a webhook route without one (GR-20);
4. `X-GitHub-Event` and `X-GitHub-Delivery` present (`missing_event`, `missing_delivery`; a request without them is rejected before anything dispatches on the event name);
5. content type empty or `application/json` (`unsupported_content_type`; the form-encoded variant wraps the JSON in `payload=` and is deliberately not supported: the hook is created with `content_type: json` by the manifest flow and checked by doctor);
6. App target: when the source is an App, `X-GitHub-Hook-Installation-Target-ID` (if present) must equal the app id (`target_mismatch`, ignored);
7. JSON decode (`bad_json`); installation routing: when an installation id is present and is neither an `installation` nor `installation_repositories` event, it must be known to the registry (`unknown_installation`, ignored);
8. repository identity validated (`validOwnerName`; `no_repository`);
9. event decoding by name.

**Outcomes** (behaviour methods consumed by the receiver through consumer-local interfaces, no import of `github`): `InvalidSignature()` -> 401, `Malformed()` -> 400, `Ignored()` -> 202 counted as ignored; any other error from `Verify` is a bug on our side and is answered **500** `internal` and logged (cross-design review CD-IF-08 / X2: 4xx is the caller's fault, GR-16; GitHub does not redeliver automatically, so the earlier reason did not hold; `Truncated()` and redelivery are unaffected). A queue-full or closed engine is the receiver's 503, not this unit's concern.

**Event decoding** to `scm.Event` (`Kind`, `Delivery`, `Source`, `Installation`, `Repo`, `Action`, `Ref`, `Before`, `After`, `Created`, `Deleted`, `Forced`, `CommitCount`, `ChangedPaths`, `PathsComplete`, `HeadTreeOID`):

| Event | Kind | Notes |
|---|---|---|
| `ping` | `EventPing` | acknowledged and counted by the receiver |
| `push` | `EventPush` | `ChangedPaths` = sorted union over **all** commits' `added`, `removed`, `modified` (GR-21); `PathsComplete=false` when any commit lacks a list (pointer-nil), when `len(commits) >= 2048`, when `commits` is empty but `head_commit` is set (GitHub omits commits above its caps), or when the union exceeds 20,000 paths; `HeadTreeOID` = `head_commit.tree_id` (enables skipping a tree fetch when equal to the stored root tree); a `deleted` push returns immediately with no paths; `Ref` is passed through verbatim: **default-branch filtering is the reactors' job** (they know the catalogue's `DefaultBranch`), the adapter never hard-codes a branch (GR-10, GR-21) |
| `release` | `EventRelease` | `Ref = refs/tags/<tag_name>`, `Action` passed through (`published`, `created`, `released`, ...); reactors only enqueue "refresh latest release" |
| `create` | `EventCreate` | `ref_type` branch or tag mapped to `refs/heads/`/`refs/tags/`; `Created=true` |
| `repository` | `EventRepository` | `Action` is `created`, `deleted`, `archived`, `unarchived`, `renamed`, `transferred`, `publicized`, `privatized`, `edited`; identity by id, so renames resolve through `catalog.reconcile`, not through the event's name fields |
| `installation`, `installation_repositories` | `EventInstallation`, `EventInstallationRepos` | no repository required; `Installation` set; consumed by the installation reactor |
| everything else (`delete`, `issues`, `workflow_run`, ...) | `*WebhookError{unsupported_event}` | counted as ignored; never an error, never a stub (GR-21: a unit test enumerates `scm.EventKind` and asserts each is produced by exactly one event name or documented as synthetic like `head_changed`) |

`EventHeadChanged` is **synthetic**: produced by `catalog.heads` when the sweep sees a new head, never by this adapter.

```go
package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Webhook failure reasons (stable; they become the `reason` label of scp_webhook_total and a log field).
const (
	ReasonMissingSignature  = "missing_signature"
	ReasonBadSignatureForm  = "bad_signature_format"
	ReasonSignatureMismatch = "signature_mismatch"
	ReasonUnknownSource     = "unknown_source"
	ReasonMissingEvent      = "missing_event"
	ReasonMissingDelivery   = "missing_delivery"
	ReasonBadJSON           = "bad_json"
	ReasonBodyTooLarge      = "body_too_large"
	ReasonContentType       = "unsupported_content_type"
	ReasonUnsupportedEvent  = "unsupported_event"
	ReasonTargetMismatch    = "target_mismatch"
	ReasonUnknownInstall    = "unknown_installation"
	ReasonNoRepository      = "no_repository"
)

// WebhookError is returned by Verify for every non-event outcome. webhook.Receiver classifies it by behaviour, with
// consumer-local interfaces: InvalidSignature() -> 401, Malformed() -> 400, Ignored() -> 202 counted as ignored.
type WebhookError struct{ reason string }

func (e *WebhookError) Error() string  { return "github webhook: " + e.reason }
func (e *WebhookError) Reason() string { return e.reason }
func (e *WebhookError) InvalidSignature() bool {
	switch e.reason {
	case ReasonMissingSignature, ReasonBadSignatureForm, ReasonSignatureMismatch, ReasonUnknownSource:
		return true
	}
	return false
}
func (e *WebhookError) Malformed() bool {
	switch e.reason {
	case ReasonMissingEvent, ReasonMissingDelivery, ReasonBadJSON, ReasonBodyTooLarge, ReasonContentType, ReasonNoRepository:
		return true
	}
	return false
}
func (e *WebhookError) Ignored() bool {
	switch e.reason {
	case ReasonUnsupportedEvent, ReasonTargetMismatch, ReasonUnknownInstall:
		return true
	}
	return false
}

// VerifierSource is the verification material of one source.
type VerifierSource struct {
	Secrets      []secret.Secret               // current first, previous second (bounded overlap during rotation); empty secrets are skipped
	AppID        int64                         // 0: not an App source, Target-ID is not enforced
	KnownInstall func(scm.InstallationID) bool // nil: do not route by installation (PAT/org hooks)
}

// Verifier implements webhook.Verifier. It is safe for concurrent use and holds no mutable state.
type Verifier struct {
	sources map[scm.SourceID]VerifierSource
	maxBody int
}

func NewVerifier(maxBody int, sources map[scm.SourceID]VerifierSource) *Verifier {
	return &Verifier{sources: sources, maxBody: maxBody}
}

// SignSHA256 returns the value of X-Hub-Signature-256 for body ("sha256=<hex>"). Used by the fake GitHub, by doctor's
// webhook self-test and by tests; GitHub's published vector is in the test plan.
func SignSHA256(sec secret.Secret, body []byte) string {
	m := hmac.New(sha256.New, []byte(sec.Reveal()))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// Verify: (1) size, (2) signature over the raw bytes BEFORE any JSON parsing, (3) required headers, (4) content type,
// (5) App target and installation routing, (6) decode into the normalised event. Only SHA-256 is accepted; the SHA-1
// X-Hub-Signature header is never read (GR-20).
func (v *Verifier) Verify(src scm.SourceID, h http.Header, body []byte) (scm.Event, error) {
	s, ok := v.sources[src]
	if !ok {
		return scm.Event{}, &WebhookError{ReasonUnknownSource}
	}
	if v.maxBody > 0 && len(body) > v.maxBody {
		return scm.Event{}, &WebhookError{ReasonBodyTooLarge}
	}
	sig := h.Get("X-Hub-Signature-256")
	if sig == "" {
		return scm.Event{}, &WebhookError{ReasonMissingSignature}
	}
	hexPart, found := strings.CutPrefix(sig, "sha256=")
	got, err := hex.DecodeString(hexPart)
	if !found || err != nil || len(got) != sha256.Size {
		return scm.Event{}, &WebhookError{ReasonBadSignatureForm}
	}
	matched := false
	for _, sec := range s.Secrets {
		if sec.IsZero() {
			continue
		}
		m := hmac.New(sha256.New, []byte(sec.Reveal()))
		m.Write(body)
		if hmac.Equal(m.Sum(nil), got) { // evaluate every secret: no early exit on position
			matched = true
		}
	}
	if !matched {
		return scm.Event{}, &WebhookError{ReasonSignatureMismatch}
	}
	name := h.Get("X-GitHub-Event")
	if name == "" {
		return scm.Event{}, &WebhookError{ReasonMissingEvent}
	}
	delivery := h.Get("X-GitHub-Delivery")
	if delivery == "" {
		return scm.Event{}, &WebhookError{ReasonMissingDelivery}
	}
	if ct := h.Get("Content-Type"); ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		return scm.Event{}, &WebhookError{ReasonContentType}
	}
	if s.AppID != 0 {
		if t := h.Get("X-GitHub-Hook-Installation-Target-ID"); t != "" && t != strconv.FormatInt(s.AppID, 10) {
			return scm.Event{}, &WebhookError{ReasonTargetMismatch}
		}
	}
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return scm.Event{}, &WebhookError{ReasonBadJSON}
	}
	ev := scm.Event{Delivery: delivery, Source: src, Action: p.Action}
	if p.Installation != nil {
		ev.Installation = scm.InstallationID(p.Installation.ID)
	}
	if s.KnownInstall != nil && ev.Installation != 0 && name != "installation" && name != "installation_repositories" && !s.KnownInstall(ev.Installation) {
		return scm.Event{}, &WebhookError{ReasonUnknownInstall}
	}
	if p.Repository != nil {
		ev.Repo = scm.RepoRef{Source: src, ID: scm.RepoID(p.Repository.ID), Owner: p.Repository.Owner.Login, Name: p.Repository.Name}
		if !validOwnerName(ev.Repo.Owner) || !validOwnerName(ev.Repo.Name) {
			return scm.Event{}, &WebhookError{ReasonNoRepository}
		}
	}
	switch name {
	case "ping":
		ev.Kind = scm.EventPing
	case "push":
		ev.Kind = scm.EventPush
		if p.Repository == nil {
			return scm.Event{}, &WebhookError{ReasonNoRepository}
		}
		decodePush(&ev, &p)
	case "release":
		ev.Kind = scm.EventRelease
		if p.Repository == nil {
			return scm.Event{}, &WebhookError{ReasonNoRepository}
		}
		if p.Release != nil {
			ev.Ref = "refs/tags/" + p.Release.TagName
		}
	case "create":
		ev.Kind = scm.EventCreate
		if p.Repository == nil {
			return scm.Event{}, &WebhookError{ReasonNoRepository}
		}
		ev.Created = true
		switch p.RefType {
		case "branch":
			ev.Ref = "refs/heads/" + p.Ref
		case "tag":
			ev.Ref = "refs/tags/" + p.Ref
		}
	case "repository":
		ev.Kind = scm.EventRepository
		if p.Repository == nil {
			return scm.Event{}, &WebhookError{ReasonNoRepository}
		}
	case "installation":
		ev.Kind = scm.EventInstallation
	case "installation_repositories":
		ev.Kind = scm.EventInstallationRepos
	default:
		return scm.Event{}, &WebhookError{ReasonUnsupportedEvent} // delete, issues, ... : counted, never an error
	}
	return ev, nil
}

type payload struct {
	Action     string       `json:"action"`
	Ref        string       `json:"ref"`
	RefType    string       `json:"ref_type"`
	Before     string       `json:"before"`
	After      string       `json:"after"`
	Created    bool         `json:"created"`
	Deleted    bool         `json:"deleted"`
	Forced     bool         `json:"forced"`
	Commits    []pushCommit `json:"commits"`
	HeadCommit *pushCommit  `json:"head_commit"`
	Repository *struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Installation *struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Release *struct {
		TagName string `json:"tag_name"`
	} `json:"release"`
}

// pushCommit uses pointers so "absent" (payload without path lists, unverified on GitHub's page, V13) differs from "empty".
type pushCommit struct {
	ID       string    `json:"id"`
	TreeID   string    `json:"tree_id"`
	Added    *[]string `json:"added"`
	Removed  *[]string `json:"removed"`
	Modified *[]string `json:"modified"`
}

// pushCommitCap is GitHub's documented limit of commits in a push payload.
const pushCommitCap = 2048

// maxChangedPaths bounds the union so a hostile or huge push cannot inflate the event.
const maxChangedPaths = 20000

func decodePush(ev *scm.Event, p *payload) {
	ev.Ref, ev.Before, ev.After = p.Ref, p.Before, p.After
	ev.Created, ev.Deleted, ev.Forced = p.Created, p.Deleted, p.Forced
	ev.CommitCount = len(p.Commits)
	if p.HeadCommit != nil {
		ev.HeadTreeOID = p.HeadCommit.TreeID
	}
	ev.PathsComplete = true
	if ev.Deleted {
		return // nothing to refresh; reactors drop deleted refs
	}
	if len(p.Commits) >= pushCommitCap || (len(p.Commits) == 0 && p.HeadCommit != nil) {
		ev.PathsComplete = false
	}
	set := map[string]struct{}{}
	for _, c := range p.Commits {
		if c.Added == nil || c.Removed == nil || c.Modified == nil {
			ev.PathsComplete = false
		}
		for _, l := range []*[]string{c.Added, c.Removed, c.Modified} {
			if l == nil {
				continue
			}
			for _, path := range *l {
				set[path] = struct{}{}
			}
		}
	}
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	if len(paths) > maxChangedPaths {
		paths, ev.PathsComplete = paths[:maxChangedPaths], false
	}
	ev.ChangedPaths = paths
}

// validOwnerName guards every value that later becomes part of a URL path.
func validOwnerName(s string) bool {
	if s == "" || len(s) > 100 || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}
```

**Recovering lost deliveries (`Redeliverer`, job `webhook.redelivery`, 6 h, only for App sources with `redelivery.enabled`)**:

```
FailedDeliveries(ctx, src, since):
    since = max(since, now - 72h)                          // GitHub keeps deliveries for a limited time (U-3)
    iterate GET /app/hook/deliveries?per_page=100 (JWT, not installation token; the endpoint takes a `cursor` parameter and
        returns the next and previous cursors in the `Link` header, documented 2026-10-14: follow `rel="next"` through nextLink; a `status=failure`
        filter exists but is NOT used because the grouping rule below needs to see later successful attempts)
        stop when an item's delivered_at < since or at limits.max_pages
    group by guid; a guid is "failed" when its newest attempt has status != "OK" (status_code outside 2xx)
    return the newest failed attempt's delivery id for each failed guid, oldest first, at most redelivery.max_per_run
    if the scan stopped at limits.max_pages before reaching `since`: return the ids found AND a non-nil error satisfying `Truncated() bool` (a behaviour interface the
    `webhook` job tests with errors.As); the job then does not advance its watermark past the oldest listed delivery (CD-IF-08 / X4, adopted from unit 04)
Redeliver(ctx, src, id):  POST /app/hook/deliveries/{id}/attempts   (202)   // JWT; 400 and 422 are documented answers (docs.github.com 2026-10-14),
                                                                         // their meaning is not: both are returned as *Error{Permanent, "status_<n>"} and logged, never
                                                                         // swallowed (V-10; the first draft treated 422 as "already redelivered: nil" without evidence)
```

The `webhook.redelivery` job treats a `Permanent` error from one `Redeliver` (400, 422) as "that delivery failed" (counted `scp_webhook_redelivery_total{result="failed"}`), continues with the next id and stops after three consecutive failures; 422 is **not** classified as a rate limit (CD-IF-08 / X5: 05's evidence-based classification wins over unit 04's default). Both calls use the JWT bucket (`integration_manifest`-class traffic is not charged to installation budgets). Redelivery keeps the original `X-GitHub-Delivery`, so the receiver's de-duplication drops events it already processed. A delivery that GitHub dropped on its side (the 2026-07-14 incident class) is never listed; that is why the planner's level-triggered sweep, not webhooks, is the source of truth (architecture 6.4). Non-App sources: both methods return `*Error{Permanent, "redelivery_unsupported"}` and `cmd/scp` never registers the job for them.

### 4.12 Installation registry and `github.installations` (research 09 sections 1.7, 2.3, 4)

State per installation: `InstallationState` of 3.5. **Source of truth is the API**; events are accelerators; both flow through the same function so a forged-but-signed event can change nothing the next verification would not confirm.

```
ReconcileInstallations(ctx):                       // body of global job github.installations (30 min, Spec.Upstream="github", PrioScheduled)
  for each App source:
      list := GET /app/installations?per_page=100 (JWT, all pages)           // account, selection, permissions, events, suspended_at
      allowed := filter list by auth.installations: allow_list (case-insensitive account logins) | pinned (ids) | all
      mass-removal guard: if more than discovery.max_removal_ratio of the previously ACTIVE allowed installations are
          missing from `list` (and at least 3 existed), keep them as `unreachable`, log WARN, do not mark `removed`
      for each allowed installation:
          suspended_at != null           -> state suspended   (no token mint, no call; repositories disappear at reconcile)
          app permissions requested but not accepted (installation.permissions lacks a required one)
                                        -> permissions_pending (doctor/`/status` say which)
          else active: count := GET /installation/repositories?per_page=1 (installation token) -> total_count
              mint failure 403 (not a rate limit) -> suspended; 404 -> removed; rate limited (403/429 with the signals of 4.3) -> pause, state unchanged (V-03);
                    401/5xx -> MintFailures++, state unreachable after 3
      account -> installation map is rebuilt (login lower-cased); owners without an installation get NotFound(Installation) -> Gone
  Changed := any state, count, or membership differs from the previous run
  return ReconcileResult{Installations, Repositories, Changed}
```

`ObserveEvent(ev)` applies the cheap, immediate part (suspend, unsuspend, deleted, created, `installation_repositories` marks the installation dirty) and `cmd/scp`'s reactor enqueues the job so the verification follows within seconds. `KnownInstall` of the verifier reads this registry (lock-free read of an immutable snapshot swapped under a mutex).

Tokens are **per installation**; a repository's installation is the one whose account login equals the repository owner. A repository whose owner has no active installation produces `NotFound(Subject=Installation)` (Gone) rather than falling back to another credential: one credential per owner, no cross-use. PAT, OAuth and `token_command` sources have no registry (`Installation == 0`).

### 4.13 `Doctor` (research 09 section 8.4)

`Client.Doctor(ctx, DoctorOptions) []Finding` runs the GitHub-facing checks; `cmd/scp` runs `cfg.*`, `store`, `webhook.self` and `git.*` and merges. Never prints a secret (fingerprints and lengths only), never writes to GitHub, total timeout 60 s, `--offline` limits it to config and file checks.

| Finding ID | Check | FAIL | WARN |
|---|---|---|---|
| `net.dns`, `net.tls`, `net.proxy` | resolution, SSRF verdict, handshake, certificate expiry, CA chain | untrusted chain, unreachable proxy | certificate expires in under 14 days |
| `host.version` | detect github.com / GHEC-DR / GHES; `X-GitHub-Enterprise-Version`; probe the configured and `2026-03-10` versions | configured version refused | GHES release past its close-down date |
| `time.skew` | `Date` header against local clock | skew over 60 s | skew over 10 s |
| `app.key` | parse, bits, fingerprint; sign a JWT and `GET /app`; print app id, slug, permissions, events; diff against `RequiredPermissions(features)` | 401 (exact message), missing required permission, **any write or admin permission** | unused permissions |
| `app.hook` | `GET /app/hook/config` (URL equals `webhook.public_url`, `content_type: json`, `insecure_ssl: "0"`); last 30 deliveries: failure ratio, oldest un-redelivered failure | | URL mismatch, failures in 24 h, 3-day reminder |
| `app.installations` | `GET /app/installations`: account, selection, suspended, pending permissions, events; allow-list coverage | a configured account has no installation | suspended, `permissions_pending`, extra installations while mode is `all` |
| `inst.token` | mint per installation; expiry, length (INFO, about 520 expected), repository count, selection, visibility mix | `installation_scope` with `selection: all` and no acknowledgement | private repositories exist under `public_only` (they are hidden; say so) |
| `inst.read` | one repository per installation: README, commits, tree, release, counts; map `Resource not accessible by integration` to the missing permission | permission missing | |
| `rate` | `GET /rate_limit` (free for primary): `core`, `graphql`; check the scaling rule (5,000 + 50 x max(0, repos-20) + 50 x max(0, users-20), cap 12,500); cold-start estimate (8 calls/repo) against the limit | | estimate over 80 percent of the limit |
| `conditional` | the same GET twice with `If-None-Match`: confirm 304 and unchanged `remaining`; optionally across two fresh tokens (answers U-1) | | validators not honoured |
| `pat` | `GET /user`: login, scopes header (classic), expiry | | classic with `repo` scope, expiry under 14 days |

`RequiredPermissions(fs ...Feature)` is the single table of what each feature needs: `core` is `metadata:read` + `contents:read`; `pull_requests`, `issues`, `commit_statuses`, `checks`, `actions` add their `read` permission. It is also what the manifest flow requests, so the doctor diff and the created App cannot disagree.

### 4.14 App manifest onboarding (`scp github app manifest`, research 09 section 1.6)

`RunAppManifest(ctx, ManifestOptions) (ManifestResult, error)`:

1. Build the manifest JSON: name, `url`, `hook_attributes{url, active}` (omitted with `NoWebhook`), `redirect_url` (loopback callback), `public: false`, `default_permissions` from `RequiredPermissions(Features)`, `default_events` `push`, `release`, `create`, `repository` (only when a webhook is configured). `installation` and `installation_repositories` are **not** listed (V-09): GitHub states that apps receive them by default and cannot subscribe to them manually (docs.github.com webhook events, 2026-10-14), so listing them in a manifest asks GitHub to subscribe to events that are not subscribable; the verifier handles them whenever they arrive. The demo world's `events` array lists them only because the fake's App settings echo what an App receives.
2. Listen on `127.0.0.1:0` (or `Listen`; `0.0.0.0` and `::` are refused). Serve an auto-submitting form that POSTs the manifest to `https://<web host>/settings/apps/new` (or `/organizations/<org>/settings/apps/new`) with a random `state` of 128 bits.
3. The browser comes back to the loopback `redirect_url` with `code` and `state`; the handler verifies `state` with `subtle.ConstantTimeCompare` and accepts one callback only.
4. `POST /app-manifests/{code}/conversions` (no authentication) returns `id`, `slug`, `client_id`, `pem`, `webhook_secret`, `html_url`; the response body is read once with a size limit.
5. Write the PEM and the webhook secret to `OutDir` with mode `0600` (refuse if the file exists, never print them), and return `ConfigSnippet` (the `github.sources[]` YAML to paste) and `InstallURL` (`<web>/apps/<slug>/installations/new`).

```go
package github

import (
	"context"
	"io"
	"net/http"
	"time"
)

// ManifestOptions drives `scp github app manifest` (research 09 section 1.6).
type ManifestOptions struct {
	WebURL     string // https://github.com, https://HOST (ghes), https://SUBDOMAIN.ghe.com
	APIURL     string // https://api.github.com, https://HOST/api/v3, https://api.SUBDOMAIN.ghe.com
	Org        string // owner organisation; empty = the signed-in personal account
	Name       string // default scp-<org>
	WebhookURL string // public URL of the SCP webhook route; empty with NoWebhook
	NoWebhook  bool
	Features   []Feature
	OutDir     string        // PEM and webhook secret files are written here with mode 0600
	Code       string        // headless: exchange this code instead of running the browser step
	Listen     string        // default 127.0.0.1:0 (loopback only; 0.0.0.0 is refused)
	Timeout    time.Duration // whole flow, default 15m (GitHub allows one hour)
	HTTP       *http.Client  // netguard-built client for the conversion call
	Out        io.Writer     // progress and the URL to open; never receives a secret
}

type ManifestResult struct {
	AppID             int64
	Slug              string
	ClientID          string
	HTMLURL           string
	PEMPath           string
	WebhookSecretPath string
	InstallURL        string // https://HOST/apps/<slug>/installations/new
	ConfigSnippet     string // the github.sources[] YAML to paste
}

// RunAppManifest serves the auto-submitting form on a loopback listener, waits for the callback, verifies state in
// constant time, exchanges the code, writes the files and returns. It owns one goroutine (the loopback server) and
// joins it before returning.
func RunAppManifest(ctx context.Context, o ManifestOptions) (ManifestResult, error) {
	return ManifestResult{}, errNotImplemented
}
```

The flow owns one goroutine (the loopback server), joined before return; the whole flow has a 15 min timeout (GitHub allows an hour for the code). Headless mode (`Code` given) skips steps 2-3. The manifest-conversion call passes through the netguard-built client.

### 4.15 `gitsource` (post-v1, architecture 8.15)

```go
// Package gitsource is the optional git acquisition mode (D-17, architecture 8.15). Specified now, built post-v1.
package gitsource

import (
	"context"
	"time"

	"github.com/cantara/source-code-portal/internal/github/auth"
	"github.com/cantara/source-code-portal/internal/scm"
)

type Config struct {
	Binary      string // absolute path to git
	CacheDir    string
	MaxTotal    int64
	MaxRepo     int64
	Timeout     time.Duration
	MinVersion  string
	Concurrency int
}

type Deps struct {
	Tokens auth.TokenSource
	Run    func(ctx context.Context, argv []string, env []string, dir string, timeout time.Duration) (stdout []byte, err error)
}

type Client struct{}

func New(cfg Config, d Deps) (*Client, error) { return &Client{}, nil }

// Check verifies the binary and its minimum version (>= 2.50.1, CVE-2025-48384) and the hardened-environment probes.
func (c *Client) Check(ctx context.Context) error { return nil }

func (c *Client) Readme(ctx context.Context, r scm.RepoRef, ref string, v scm.Validators) (scm.Result[scm.File], error) {
	return scm.Result[scm.File]{}, nil
}
func (c *Client) ReadFile(ctx context.Context, r scm.RepoRef, ref, path string, v scm.Validators) (scm.Result[scm.File], error) {
	return scm.Result[scm.File]{}, nil
}
func (c *Client) ListTree(ctx context.Context, r scm.RepoRef, ref string, opt scm.TreeOptions) (scm.Result[scm.Tree], error) {
	return scm.Result[scm.Tree]{}, nil
}
func (c *Client) ReadBlob(ctx context.Context, r scm.RepoRef, sha string, maxBytes int64) (scm.Blob, error) {
	return scm.Blob{}, nil
}
func (c *Client) ReadBlobs(ctx context.Context, r scm.RepoRef, shas []string, maxBytes int64) (map[string]scm.Blob, error) {
	return nil, nil
}
func (c *Client) ReadRepoImage(ctx context.Context, r scm.RepoRef, ref, path string, maxBytes int64) (scm.File, error) {
	return scm.File{}, nil
}
```

Specified now so the five file-oriented interfaces stay implementable by a second acquisition mode; **not built in v1** (`acquisition.mode: git` is refused by `Config.Validate`). Behaviour when built: the `git` binary (never go-git, research 09 6.4) at version at least the pinned minimum (2.50.1 per research, to be re-verified when the milestone starts, U-8); hardened environment (empty environment except `PATH` and a private `HOME`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`, `GIT_ALLOW_PROTOCOL=https`, `-c core.hooksPath=/dev/null`, `-c protocol.file.allow=never`, `--no-recurse-submodules`); `clone --filter=blob:none --depth=1 --sparse --no-checkout` plus sparse-checkout of allow-listed paths; the token passed through `GIT_CONFIG_COUNT/KEY/VALUE` (`http.extraHeader`), never in argv or a URL (GR-13); cache directory with total and per-repository byte caps and LRU eviction; per-command timeouts and process-group kill; its own concurrency limit; clone failures are `github.Error`-compatible typed failures. Only this package and `cmd/scp` may import `os/exec` (G6); the injected `Deps.Run` keeps even this package testable without a real `git`. Tests: a local bare repository through `file://` inside the test only (production policy forbids `file`), with a hook script in the repository that must never run.

## 5. Data

### 5.1 What the unit persists

**Nothing.** The adapter does not import `store` and owns no table, file or cache directory (except `gitsource`, post-v1, which owns a git cache directory under the writable volume, section 4.15). What survives a restart is owned by other units and is shaped by this unit's contracts:

| Datum | Owner | Contract set by this unit |
|---|---|---|
| Content validators (`scm.Validators.SHA`): README/file blob SHA, head commit SHA, tree SHA | `store` item rows written by the features | the **SHA** is the persisted validator because REST ETags depend on the `Authorization` header and installation tokens rotate hourly (ADR-004, research 03 2.4); `Validators.ETag` is a hint that is valid only inside one token lifetime and is never relied on |
| Plan state (`next_due_at`, unchanged streak) | `refresh` through `store.SavePlans` | `Outcome.Unchanged` from a 304 or an equal SHA drives idle back-off |
| Installation tokens | nobody | memory only, never persisted, never logged (8.8) |
| App private key, PATs, webhook secrets | operator (files, Kubernetes Secrets) | referenced by `SecretRef`, read at startup and on key reload |
| Webhook delivery ids (de-duplication) | `webhook` (bounded in-memory LRU, 15 min) | `scm.Event.Delivery` is the GitHub GUID; redelivery keeps the GUID |

### 5.2 In-memory state of the adapter (all bounded)

| Structure | Key | Bound (config) | Eviction | Concurrency |
|---|---|---|---|---|
| Rate book `rateState.buckets` | (source, installation, resource) | number of installations x about 8 resources | none needed (small); entries replaced on every response | one mutex, `observe`/`admit` hold it for map operations only |
| Pause table, secondary streak | source | number of sources | cleared by success / elapsed | same mutex |
| Installation token cache (`auth.App.cache`) | `Scope.Key()` | installations x scopes in use (normally one scope each) | replaced on mint, dropped on `Reject` | mutex plus singleflight per key |
| Validator memo (ETag/Last-Modified) | (token fingerprint, method, URL, Accept) | `limits.validator_max` (4,096 entries) | oldest first | mutex; entries hold an ETag string and a time only, no body |
| Tree memo | (source, repo, ref, recursive, path) | `limits.tree_memo_max` (256) with TTL `tree_memo_ttl` (60 s) | oldest first, expired on access | singleflight per key; value is the immutable `scm.Tree` |
| Immutable tree LRU | tree or commit SHA | `limits.immutable_max` (512) | LRU | mutex |
| Installation registry | installation id and lower-cased account login | installations | replaced by reconcile | immutable snapshot swapped under a mutex; readers never block on I/O |
| Breaker, limiter, semaphore | source | one each | n/a | thread-safe types |
| GraphQL cool-down | source | one timestamp | elapses | atomic |

Memory estimate at 1,000 repositories: the validator memo and tree memo dominate; a tree entry is about 100 bytes plus the path, a 5,000-entry monorepo tree is about 0.5 MB, 256 memo entries stay under 64 MB in the worst case and typically under 10 MB; the memo is shared by `docs` and `manifests`, which is exactly why it exists.

### 5.3 Wire-to-model mapping and fixtures as data

The mapping tables are in 3.4. The **fixtures** of the fake GitHub are data too: `world.json` plus one `repo.json` per repository (section 7.3), committed under `testdata/worlds/`. The demo world used by the developer environment and the Playwright suite is reproduced in 7.3 and was decoded with the strict loader types in a test (`TestDemoFixtureDecodes`). Recorded real responses (golden files for decode tests) live under `internal/github/testdata/recorded/` with a header comment stating the endpoint, the API version, the capture date and the redactions applied; a `scripts/record-github.sh` helper (run by a person with a read-only token, never in CI) refreshes them, and every recorded body passes the same `RedactTokens` and a login/e-mail scrub before commit (GR-35).

## 6. Configuration

### 6.1 Shape and ownership

The root config lives in `cmd/scp/config.go`; this unit owns the `github:` subtree as `github.Config` (compile-checked below). It is strict-decoded (unknown key is an error), layered embedded default < file < `SCP_` environment < flags by `platform/config`, and validated by `Config.Validate()` after `ApplyDefaults()`. Env mapping is `SCP_<PATH_IN_UPPER_SNAKE>` with list indices (`SCP_GITHUB_SOURCES_0_AUTH_KIND`), no dotted names. Secrets are never given as plain values: each is a `SecretRef{file, env}`; the file form (`..._FILE` convention, `SCP_GITHUB_SOURCES_0_AUTH_PRIVATE_KEY_FILE=/run/secrets/app.pem`) is preferred and the `env` member names an environment variable to read through `Deps.LookupEnv`.

```go
package github

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

// ByteSize is a size in bytes that decodes from "512KiB", "1MiB", "2GiB" or a plain integer.
type ByteSize int64

func (b *ByteSize) UnmarshalText(t []byte) error {
	s := strings.TrimSpace(string(t))
	mult := int64(1)
	for _, u := range []struct { // longest suffix first: "MiB" also ends with "B"
		suf string
		m   int64
	}{{"KiB", 1 << 10}, {"MiB", 1 << 20}, {"GiB", 1 << 30}, {"B", 1}} {
		if strings.HasSuffix(s, u.suf) {
			mult, s = u.m, strings.TrimSpace(strings.TrimSuffix(s, u.suf))
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return fmt.Errorf("invalid size %q", string(t))
	}
	*b = ByteSize(n * mult)
	return nil
}

// Percent is a fraction in [0,1] that decodes from "40%".
type Percent float64

func (p *Percent) UnmarshalText(t []byte) error {
	s := strings.TrimSuffix(strings.TrimSpace(string(t)), "%")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || f > 100 {
		return fmt.Errorf("invalid percentage %q", string(t))
	}
	*p = Percent(f / 100)
	return nil
}

// HostKind selects URL derivation and the default REST API version.
type HostKind string

const (
	HostGitHubCom HostKind = "github.com"
	HostGHEC      HostKind = "ghec_dr" // GitHub Enterprise Cloud with data residency (ghe.com)
	HostGHES      HostKind = "ghes"
)

// SecretRef is secret.Secret of platform/secret (cross-design review CD-CF-06, 02 Q3): the YAML shape is `{file: /path}` only. The `env` member
// and Deps.LookupEnv of the first draft are dropped (SCP_..._FILE already covers the environment, and secret.Secret gives never-both
// validation, redaction in `scp config show` and logx scrubbing). IsZero keeps its meaning: an unset secret is "not configured".
type SecretRef = secret.Secret

// Config is github.Config, composed into the root config of cmd/scp under the key "github".
type Config struct {
	Defaults Defaults       `yaml:"defaults"`
	Sources  []SourceConfig `yaml:"sources"`
}

type Defaults struct {
	APIVersion string          `yaml:"api_version"` // "auto" | "2026-03-10" | "2022-11-28"
	HTTP       HTTPConfig      `yaml:"http"`
	Limits     LimitsConfig    `yaml:"limits"`
	Budget     BudgetConfig    `yaml:"budget"`
	Webhook    WebhookDefaults `yaml:"webhook"`
}

type HTTPConfig struct {
	Timeout               time.Duration `yaml:"timeout"`         // REST, total
	GraphQLTimeout        time.Duration `yaml:"graphql_timeout"` // GitHub itself cuts GraphQL at 10 s
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"`
	MaxIdleConnsPerHost   int           `yaml:"max_idle_conns_per_host"`
}

type LimitsConfig struct {
	FileBytes      ByteSize      `yaml:"file_bytes"`      // README and contents reads
	JSONBytes      ByteSize      `yaml:"json_bytes"`      // any JSON page
	TreeBytes      ByteSize      `yaml:"tree_bytes"`      // Git Trees responses (GitHub caps them at 7 MB)
	TreeMemoTTL    time.Duration `yaml:"tree_memo_ttl"`   // shared ListTree memo
	TreeMemoMax    int           `yaml:"tree_memo_max"`   // entries
	ImmutableMax   int           `yaml:"immutable_max"`   // LRU of trees by SHA
	ValidatorMax   int           `yaml:"validator_max"`   // in-memory ETag cache entries
	MaxPages       int           `yaml:"max_pages"`       // ceiling for internal pagination loops
	HeadBatch      int           `yaml:"head_batch"`      // repositories per GraphQL head query
	ReleaseRecheck time.Duration `yaml:"release_recheck"` // REST fallback only
}

type BudgetConfig struct {
	Reserve     ReserveConfig `yaml:"reserve"`
	Concurrency int           `yaml:"concurrency"` // in-flight requests per source
	RPS         float64       `yaml:"rps"`         // token bucket per source
	Burst       int           `yaml:"burst"`
}

// ReserveConfig: a class may start work only while remaining/limit is above its threshold (ADR-015).
type ReserveConfig struct {
	Backfill  Percent `yaml:"backfill"`
	Scheduled Percent `yaml:"scheduled"`
	Webhook   Percent `yaml:"webhook"`
}

type WebhookDefaults struct {
	MaxBody   ByteSize      `yaml:"max_body"`   // consumed by webhook.Receiver and by Verifier as a second guard
	DedupeTTL time.Duration `yaml:"dedupe_ttl"` // consumed by webhook
	DedupeMax int           `yaml:"dedupe_max"` // consumed by webhook
	// Added by the cross-design review (CD-CF-04): the receiver's admission keys that unit 04 section 6.2 defined under the same path.
	MaxConcurrent int           `yaml:"max_concurrent"` // 4: body slots (architecture 11.1: the 26 MiB route behind a 4-slot semaphore)
	AcquireWait   time.Duration `yaml:"acquire_wait"`   // 2s: how long a request waits for a slot before 503
	RetryAfter    time.Duration `yaml:"retry_after"`    // 5s: the Retry-After of that 503 and of queue-full
	BodyTimeout   time.Duration `yaml:"body_timeout"`   // 8s: deadline to read the body
}

type SourceConfig struct {
	ID          scm.SourceID      `yaml:"id"`
	Host        HostConfig        `yaml:"host"`
	Network     NetworkConfig     `yaml:"network"`
	APIVersion  string            `yaml:"api_version"` // "" = defaults.api_version
	Auth        AuthConfig        `yaml:"auth"`
	Webhook     SourceWebhook     `yaml:"webhook"`
	Discovery   DiscoveryConfig   `yaml:"discovery"`
	Acquisition AcquisitionConfig `yaml:"acquisition"`
}

type HostConfig struct {
	Kind       HostKind `yaml:"kind"`
	Subdomain  string   `yaml:"subdomain"`   // ghec_dr: api.<sub>.ghe.com
	WebURL     string   `yaml:"web_url"`     // ghes (required), ghec_dr (derived)
	APIURL     string   `yaml:"api_url"`     // override; ghes default <web_url>/api/v3
	GraphQLURL string   `yaml:"graphql_url"` // override; ghes default <web_url>/api/graphql
}

type NetworkConfig struct {
	ProxyURL string `yaml:"proxy_url"`
	TLS      struct {
		CAFile string `yaml:"ca_file"`
	} `yaml:"tls"`
	AllowPrivateAddresses bool     `yaml:"allow_private_addresses"`
	AllowInsecureHTTP     bool     `yaml:"allow_insecure_http"`
	PinHosts              []string `yaml:"pin_hosts"`
}

type AuthKind string

const (
	AuthGitHubApp AuthKind = "github_app"
	AuthPAT       AuthKind = "pat"
	AuthFinePAT   AuthKind = "fine_grained_pat"
	AuthOAuthUser AuthKind = "oauth_user_token"
	AuthCommand   AuthKind = "token_command"
	AuthAnonymous AuthKind = "anonymous"
)

type AuthConfig struct {
	Kind          AuthKind      `yaml:"kind"`
	ClientID      string        `yaml:"client_id"`
	AppID         int64         `yaml:"app_id"`
	PrivateKey    SecretRef     `yaml:"private_key"`
	PrivateKeys   []SecretRef   `yaml:"private_keys"` // rotation: current first
	Installations InstallSet    `yaml:"installations"`
	Token         SecretRef     `yaml:"token"`
	Command       []string      `yaml:"command"`
	Timeout       time.Duration `yaml:"timeout"`
	RefreshBefore time.Duration `yaml:"refresh_before"`
	TTL           time.Duration `yaml:"ttl"`
	Dev           bool          `yaml:"dev"` // permits anonymous
}

type InstallSet struct {
	Mode     string   `yaml:"mode"` // allow_list (default) | pinned | all
	Accounts []string `yaml:"accounts"`
	IDs      []int64  `yaml:"ids"`
}

type SourceWebhook struct {
	Enabled        *bool     `yaml:"enabled"` // nil = true; false is the explicit "no webhooks for this source" (unit 04 6.2, CD-CF-04); consumed by cmd/scp (Sources.IDs)
	Default        bool      `yaml:"default"` // the source-less webhook routes answer for this source; at most one true (unit 04 6.2, CD-CF-04)
	Secret         SecretRef `yaml:"secret"`
	PreviousSecret SecretRef `yaml:"previous_secret"`
	PublicURL      string    `yaml:"public_url"`
	Redelivery     struct {
		Enabled   bool          `yaml:"enabled"`
		Interval  time.Duration `yaml:"interval"`
		MaxPerRun int           `yaml:"max_per_run"` // 50 (CD-CF-03: unit 04's 200 is withdrawn)
		Rate      time.Duration `yaml:"rate"`        // 1s between Redeliver calls (unit 04 6.2, adopted)
		MaxAge    time.Duration `yaml:"max_age"`     // 72h: do not redeliver older than this (GitHub keeps 3 days)
	} `yaml:"redelivery"`
}

type DiscoveryConfig struct {
	Mode                  string        `yaml:"mode"`             // installation | org | user | explicit
	Org                   string        `yaml:"org"`              // org/user modes
	Repos                 []string      `yaml:"repos"`            // explicit mode: "owner/name"
	Visibility            string        `yaml:"visibility"`       // public_only | installation_scope
	IncludeArchived       *bool         `yaml:"include_archived"` // nil = default true
	IncludeForks          bool          `yaml:"include_forks"`
	ReconcileEvery        time.Duration `yaml:"reconcile_every"`        // consumed by catalog
	MaxRemovalRatio       float64       `yaml:"max_removal_ratio"`      // consumed by catalog
	RemovalConfirmations  int           `yaml:"removal_confirmations"`  // 2 (1..10): consumed by catalog (08 R3), declared here (CD-CF-07)
	MassRemovalHold       time.Duration `yaml:"mass_removal_hold"`      // 6h (0..168h): consumed by catalog (08 R4), declared here (CD-CF-07)
	AccessAcknowledgement string        `yaml:"access_acknowledgement"` // "" | network_restricted | oidc_proxy | trusted_proxy_headers
}

type AcquisitionConfig struct {
	Mode            string `yaml:"mode"`             // api | git (git refused in v1 builds)
	GraphQL         string `yaml:"graphql"`          // auto | on | off
	GraphQLBlobs    string `yaml:"graphql_blobs"`    // off | auto | on (staged, spike S-B)
	WebhookAssisted bool   `yaml:"webhook_assisted"` // consumed by wiring: register reactors for this source
	Git             struct {
		Binary     string        `yaml:"binary"`
		CacheDir   string        `yaml:"cache_dir"`
		MaxTotal   ByteSize      `yaml:"max_total"`
		MaxRepo    ByteSize      `yaml:"max_repo"`
		Timeout    time.Duration `yaml:"timeout"`
		MinVersion string        `yaml:"min_version"`
	} `yaml:"git"`
}

// DefaultConfig returns the values documented in section 6 (the embedded default config is generated from it).
func DefaultConfig() Config {
	var c Config
	c.Defaults = Defaults{
		APIVersion: "auto",
		HTTP:       HTTPConfig{Timeout: 30 * time.Second, GraphQLTimeout: 20 * time.Second, ResponseHeaderTimeout: 20 * time.Second, MaxIdleConnsPerHost: 16},
		Limits: LimitsConfig{FileBytes: 1 << 20, JSONBytes: 8 << 20, TreeBytes: 16 << 20, TreeMemoTTL: 60 * time.Second,
			TreeMemoMax: 256, ImmutableMax: 512, ValidatorMax: 4096, MaxPages: 50, HeadBatch: 50, ReleaseRecheck: time.Hour},
		Budget:  BudgetConfig{Reserve: ReserveConfig{Backfill: 0.40, Scheduled: 0.15, Webhook: 0.02}, Concurrency: 4, RPS: 8, Burst: 16},
		Webhook: WebhookDefaults{MaxBody: 25 << 20, DedupeTTL: 15 * time.Minute, DedupeMax: 10000, MaxConcurrent: 4, AcquireWait: 2 * time.Second, RetryAfter: 5 * time.Second, BodyTimeout: 8 * time.Second},
	}
	return c
}

// ApplyDefaults fills the per-source fields whose default is not the zero value. It is called by cmd/scp after the
// strict decode and before Validate; it never overwrites a value the operator set. (A YAML list element cannot be
// pre-filled by decoding into a prefilled struct, hence a method.)
func (c *Config) ApplyDefaults() {
	d := DefaultConfig().Defaults
	if c.Defaults.APIVersion == "" {
		c.Defaults.APIVersion = d.APIVersion
	}
	fill := func(p *time.Duration, v time.Duration) {
		if *p == 0 {
			*p = v
		}
	}
	h := &c.Defaults.HTTP
	fill(&h.Timeout, d.HTTP.Timeout)
	fill(&h.GraphQLTimeout, d.HTTP.GraphQLTimeout)
	fill(&h.ResponseHeaderTimeout, d.HTTP.ResponseHeaderTimeout)
	if h.MaxIdleConnsPerHost == 0 {
		h.MaxIdleConnsPerHost = d.HTTP.MaxIdleConnsPerHost
	}
	l := &c.Defaults.Limits
	if l.FileBytes == 0 {
		l.FileBytes = d.Limits.FileBytes
	}
	if l.JSONBytes == 0 {
		l.JSONBytes = d.Limits.JSONBytes
	}
	if l.TreeBytes == 0 {
		l.TreeBytes = d.Limits.TreeBytes
	}
	fill(&l.TreeMemoTTL, d.Limits.TreeMemoTTL)
	fill(&l.ReleaseRecheck, d.Limits.ReleaseRecheck)
	for _, p := range []struct {
		got *int
		def int
	}{{&l.TreeMemoMax, d.Limits.TreeMemoMax}, {&l.ImmutableMax, d.Limits.ImmutableMax},
		{&l.ValidatorMax, d.Limits.ValidatorMax}, {&l.MaxPages, d.Limits.MaxPages}, {&l.HeadBatch, d.Limits.HeadBatch},
		{&c.Defaults.Budget.Concurrency, d.Budget.Concurrency}, {&c.Defaults.Budget.Burst, d.Budget.Burst}} {
		if *p.got == 0 {
			*p.got = p.def
		}
	}
	b := &c.Defaults.Budget
	if b.RPS == 0 {
		b.RPS = d.Budget.RPS
	}
	if b.Reserve == (ReserveConfig{}) {
		b.Reserve = d.Budget.Reserve
	}
	w := &c.Defaults.Webhook
	if w.MaxBody == 0 {
		w.MaxBody = d.Webhook.MaxBody
	}
	fill(&w.DedupeTTL, d.Webhook.DedupeTTL)
	if w.DedupeMax == 0 {
		w.DedupeMax = d.Webhook.DedupeMax
	}
	for i := range c.Sources {
		s := &c.Sources[i]
		if s.Host.Kind == "" {
			s.Host.Kind = HostGitHubCom
		}
		if s.Discovery.Visibility == "" {
			s.Discovery.Visibility = "public_only"
		}
		if s.Discovery.IncludeArchived == nil {
			t := true
			s.Discovery.IncludeArchived = &t
		}
		fill(&s.Discovery.ReconcileEvery, 30*time.Minute)
		if s.Discovery.MaxRemovalRatio == 0 {
			s.Discovery.MaxRemovalRatio = 0.3
		}
		if s.Auth.Kind == AuthGitHubApp && s.Auth.Installations.Mode == "" {
			s.Auth.Installations.Mode = "allow_list"
		}
		if s.Acquisition.Mode == "" {
			s.Acquisition.Mode = "api"
		}
		if s.Acquisition.GraphQL == "" {
			s.Acquisition.GraphQL = "auto"
		}
		if s.Acquisition.GraphQLBlobs == "" {
			s.Acquisition.GraphQLBlobs = "off"
		}
		fill(&s.Auth.Timeout, 10*time.Second)
		fill(&s.Auth.RefreshBefore, 5*time.Minute)
		fill(&s.Auth.TTL, 10*time.Minute)
		if s.Webhook.Redelivery.Interval == 0 {
			s.Webhook.Redelivery.Interval = 6 * time.Hour
		}
		if s.Webhook.Redelivery.MaxPerRun == 0 {
			s.Webhook.Redelivery.MaxPerRun = 50
		}
	}
}

// ValidationError is one path-accurate violation; Validate returns them joined (errors.Join), so platform/config can
// walk them with errors.As and list every violation, not only the first (architecture section 15, point 5).
type ValidationError struct{ Path, Msg string }

func (e *ValidationError) Error() string { return e.Path + ": " + e.Msg }

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Validate applies the rules of section 6.3. It performs no I/O: file existence, key parsing and SSRF address policy
// are checked by New (startup) and by `scp doctor`.
func (c Config) Validate() error {
	var errs []error
	add := func(path, format string, a ...any) {
		errs = append(errs, &ValidationError{Path: path, Msg: fmt.Sprintf(format, a...)})
	}
	var apiVersionRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	checkVersion := func(path, v string) {
		if v != "" && v != "auto" && !apiVersionRe.MatchString(v) {
			add(path, "must be auto or a date such as 2026-03-10")
		}
	}
	checkVersion("github.defaults.api_version", c.Defaults.APIVersion)
	d := c.Defaults
	for _, p := range []struct {
		path string
		bad  bool
	}{
		{"github.defaults.http.timeout", d.HTTP.Timeout < 0}, {"github.defaults.http.graphql_timeout", d.HTTP.GraphQLTimeout < 0},
		{"github.defaults.limits.max_pages", d.Limits.MaxPages < 0}, {"github.defaults.limits.head_batch", d.Limits.HeadBatch < 0 || d.Limits.HeadBatch > 100},
		{"github.defaults.budget.concurrency", d.Budget.Concurrency < 0 || d.Budget.Concurrency > 100}, {"github.defaults.budget.rps", d.Budget.RPS < 0},
		{"github.defaults.webhook.max_body", d.Webhook.MaxBody < 0 || d.Webhook.MaxBody > 25<<20},
	} {
		if p.bad {
			add(p.path, "out of range")
		}
	}
	if r := d.Budget.Reserve; r.Backfill < r.Scheduled || r.Scheduled < r.Webhook {
		add("github.defaults.budget.reserve", "thresholds must satisfy backfill >= scheduled >= webhook (a lower-priority class needs more headroom)")
	}
	seen := map[scm.SourceID]bool{}
	for i, s := range c.Sources {
		p := fmt.Sprintf("github.sources[%d]", i)
		if !idRe.MatchString(string(s.ID)) {
			add(p+".id", "must match %s", idRe)
		}
		if seen[s.ID] {
			add(p+".id", "duplicate source id %q", s.ID)
		}
		seen[s.ID] = true
		checkVersion(p+".api_version", s.APIVersion)
		switch s.Host.Kind {
		case HostGitHubCom, "":
			if s.Host.WebURL != "" || s.Host.APIURL != "" || s.Host.GraphQLURL != "" {
				add(p+".host", "url overrides are only valid for kind ghes")
			}
		case HostGHEC:
			if s.Host.Subdomain == "" {
				add(p+".host.subdomain", "required for kind ghec_dr")
			}
		case HostGHES:
			if s.Host.WebURL == "" && s.Host.APIURL == "" {
				add(p+".host.web_url", "required for kind ghes")
			}
		default:
			add(p+".host.kind", "unknown kind %q", s.Host.Kind)
		}
		for _, kv := range [][2]string{{"host.web_url", s.Host.WebURL}, {"host.api_url", s.Host.APIURL}, {"host.graphql_url", s.Host.GraphQLURL}, {"network.proxy_url", s.Network.ProxyURL}} {
			name, raw := kv[0], kv[1]
			if raw == "" {
				continue
			}
			u, err := url.Parse(raw)
			switch {
			case err != nil || u.Host == "":
				add(p+"."+name, "not a valid absolute URL")
			case u.User != nil || u.RawQuery != "" || u.Fragment != "":
				add(p+"."+name, "userinfo, query and fragment are not allowed")
			case u.Scheme != "https" && !(u.Scheme == "http" && s.Network.AllowInsecureHTTP) && name != "network.proxy_url":
				add(p+"."+name, "https required (set network.allow_insecure_http for a lab host)")
			}
		}
		switch s.Auth.Kind {
		case AuthGitHubApp:
			if s.Auth.ClientID == "" && s.Auth.AppID == 0 {
				add(p+".auth", "client_id or app_id required")
			}
			if s.Auth.PrivateKey.IsZero() && len(s.Auth.PrivateKeys) == 0 {
				add(p+".auth.private_key", "required for github_app")
			}
			switch s.Auth.Installations.Mode {
			case "", "allow_list":
				if len(s.Auth.Installations.Accounts) == 0 {
					add(p+".auth.installations.accounts", "allow_list needs at least one account (mode all must be explicit)")
				}
			case "pinned":
				if len(s.Auth.Installations.IDs) == 0 {
					add(p+".auth.installations.ids", "pinned needs at least one id")
				}
			case "all":
			default:
				add(p+".auth.installations.mode", "unknown mode %q", s.Auth.Installations.Mode)
			}
			if s.Webhook.Secret.IsZero() {
				add(p+".webhook.secret", "required: the webhook route is not registered without a secret")
			}
		case AuthPAT, AuthFinePAT, AuthOAuthUser:
			if s.Auth.Token.IsZero() {
				add(p+".auth.token", "required")
			}
		case AuthCommand:
			if len(s.Auth.Command) == 0 || !strings.HasPrefix(s.Auth.Command[0], "/") {
				add(p+".auth.command", "argv with an absolute program path required (no shell)")
			}
		case AuthAnonymous:
			if !s.Auth.Dev {
				add(p+".auth.dev", "anonymous requires dev: true")
			}
			if s.Host.Kind == HostGHEC {
				add(p+".auth.kind", "anonymous is refused on ghec_dr (every request must be authenticated)")
			}
		default:
			add(p+".auth.kind", "unknown kind %q", s.Auth.Kind)
		}
		switch s.Discovery.Mode {
		case "installation":
			if s.Auth.Kind != AuthGitHubApp {
				add(p+".discovery.mode", "installation discovery needs auth.kind github_app")
			}
		case "org", "user":
			if s.Discovery.Org == "" {
				add(p+".discovery.org", "required for mode %s", s.Discovery.Mode)
			}
		case "explicit":
			if len(s.Discovery.Repos) == 0 {
				add(p+".discovery.repos", "required for mode explicit")
			}
		default:
			add(p+".discovery.mode", "unknown mode %q", s.Discovery.Mode)
		}
		switch s.Discovery.Visibility {
		case "", "public_only":
		case "installation_scope":
			// Needs access.mode trusted-proxy or access_acknowledgement; the access section is not visible here,
			// so cmd/scp checks the pair (ADR-023). Only the acknowledgement vocabulary is checked locally.
			switch s.Discovery.AccessAcknowledgement {
			case "", "network_restricted", "oidc_proxy", "trusted_proxy_headers":
			default:
				add(p+".discovery.access_acknowledgement", "unknown value %q", s.Discovery.AccessAcknowledgement)
			}
		default:
			add(p+".discovery.visibility", "unknown policy %q", s.Discovery.Visibility)
		}
		if s.Webhook.Redelivery.Enabled && s.Auth.Kind != AuthGitHubApp {
			add(p+".webhook.redelivery.enabled", "redelivery lists App deliveries and needs auth.kind github_app")
		}
		if s.Acquisition.Mode == "git" {
			add(p+".acquisition.mode", "git acquisition is not available in this build")
		}
		if r := s.Discovery.MaxRemovalRatio; r < 0 || r > 1 {
			add(p+".discovery.max_removal_ratio", "must be within [0,1]")
		}
	}
	return errors.Join(errs...)
}
```

### 6.2 Every key

Column "env" shows the generic mapping; `[i]` is the source index. "Consumer" is the code that reads the value; the project test **documented keys == consumed keys** (architecture 16, GR-12) reads this table's generated twin, so a key listed here that no code reads fails CI. Keys consumed by another unit are marked; they live here because they describe GitHub sources.

**`github.defaults.*`** (applied to every source; per-source override only where noted)

| Key | Type | Default | Validation | Env | Consumer |
|---|---|---|---|---|---|
| `defaults.api_version` | string | `auto` | `auto` or `YYYY-MM-DD` | `SCP_GITHUB_DEFAULTS_API_VERSION` | github (4.2.4); per-source `sources[i].api_version` overrides |
| `defaults.http.timeout` | duration | `30s` | not negative | `SCP_GITHUB_DEFAULTS_HTTP_TIMEOUT` | github (REST per-request deadline) |
| `defaults.http.graphql_timeout` | duration | `20s` | not negative | `..._HTTP_GRAPHQL_TIMEOUT` | github (GitHub cuts GraphQL at 10 s; the margin covers queueing) |
| `defaults.http.response_header_timeout` | duration | `20s` | | `..._HTTP_RESPONSE_HEADER_TIMEOUT` | cmd/scp client factory (`Network`) |
| `defaults.http.max_idle_conns_per_host` | int | `16` | | `..._HTTP_MAX_IDLE_CONNS_PER_HOST` | cmd/scp client factory |
| `defaults.limits.file_bytes` | size | `1MiB` | | `..._LIMITS_FILE_BYTES` | github (README and contents reads; truncation point of `File.Data`; the JSON response bound is `contentsJSONLimit(file_bytes)`, 4.9) |
| `defaults.limits.json_bytes` | size | `8MiB` | | `..._LIMITS_JSON_BYTES` | github (any JSON page) |
| `defaults.limits.tree_bytes` | size | `16MiB` | | `..._LIMITS_TREE_BYTES` | github (GitHub caps trees at 7 MB) |
| `defaults.limits.tree_memo_ttl` | duration | `60s` | | `..._LIMITS_TREE_MEMO_TTL` | github (shared `ListTree` memo, architecture 8.7) |
| `defaults.limits.tree_memo_max` | int | `256` | | `..._LIMITS_TREE_MEMO_MAX` | github |
| `defaults.limits.immutable_max` | int | `512` | | `..._LIMITS_IMMUTABLE_MAX` | github (trees by SHA) |
| `defaults.limits.validator_max` | int | `4096` | | `..._LIMITS_VALIDATOR_MAX` | github (in-memory ETag memo) |
| `defaults.limits.max_pages` | int | `50` | | `..._LIMITS_MAX_PAGES` | github (ceiling for every internal pagination loop) |
| `defaults.limits.head_batch` | int | `50` | 0 to 100 | `..._LIMITS_HEAD_BATCH` | github (repositories per GraphQL head query) |
| `defaults.limits.release_recheck` | duration | `1h` | | `..._LIMITS_RELEASE_RECHECK` | github (REST fallback only) |
| `defaults.budget.reserve.backfill` | percent | `40%` | 0 to 100; backfill >= scheduled >= webhook | `..._BUDGET_RESERVE_BACKFILL` | github (`Admit`) |
| `defaults.budget.reserve.scheduled` | percent | `15%` | same | `..._BUDGET_RESERVE_SCHEDULED` | github |
| `defaults.budget.reserve.webhook` | percent | `2%` | same | `..._BUDGET_RESERVE_WEBHOOK` | github |
| `defaults.budget.concurrency` | int | `4` | 0 to 100 | `..._BUDGET_CONCURRENCY` | github (in-flight requests per source) |
| `defaults.budget.rps` | float | `8` | not negative | `..._BUDGET_RPS` | github (`x/time/rate`) |
| `defaults.budget.burst` | int | `16` | | `..._BUDGET_BURST` | github |
| `defaults.webhook.max_body` | size | `25MiB` | at most 25MiB | `..._WEBHOOK_MAX_BODY` | **webhook** (`MaxBytesReader`) and github (`NewVerifier`, second guard) |
| `defaults.webhook.dedupe_ttl` | duration | `15m` | | `..._WEBHOOK_DEDUPE_TTL` | **webhook** |
| `defaults.webhook.dedupe_max` | int | `10000` | | `..._WEBHOOK_DEDUPE_MAX` | **webhook** |
| `defaults.webhook.max_concurrent` | int | `4` | 1..32 | `..._WEBHOOK_MAX_CONCURRENT` | **webhook** (CD-CF-04: moved here from 04 section 6.2, one owner per key) |
| `defaults.webhook.acquire_wait` | duration | `2s` | 0..9s (below GitHub's 10 s) | `..._WEBHOOK_ACQUIRE_WAIT` | **webhook** |
| `defaults.webhook.retry_after` | duration | `5s` | 1s..5m | `..._WEBHOOK_RETRY_AFTER` | **webhook** |
| `defaults.webhook.body_timeout` | duration | `8s` | 1s..9s | `..._WEBHOOK_BODY_TIMEOUT` | **webhook** |

**`github.sources[i].*`**

| Key | Type | Default | Validation | Env | Consumer |
|---|---|---|---|---|---|
| `id` | string | required | `^[a-z0-9][a-z0-9-]{0,39}$`, unique | `SCP_GITHUB_SOURCES_<i>_ID` | github; becomes `scm.SourceID` and the webhook route `{source}` |
| `api_version` | string | `""` (= defaults) | `auto` or date | `..._API_VERSION` | github |
| `host.kind` | enum | `github.com` | `github.com`, `ghec_dr`, `ghes` | `..._HOST_KIND` | github (URL derivation, API version, capabilities) |
| `host.subdomain` | string | none | required for `ghec_dr` | `..._HOST_SUBDOMAIN` | github |
| `host.web_url` | URL | none | required for `ghes` (unless `api_url`); https; no userinfo, query or fragment | `..._HOST_WEB_URL` | github (HTMLURL, manifest flow) |
| `host.api_url` | URL | `<web_url>/api/v3` | same | `..._HOST_API_URL` | github |
| `host.graphql_url` | URL | `<web_url>/api/graphql` | same | `..._HOST_GRAPHQL_URL` | github |
| `network.proxy_url` | URL | none | absolute URL | `..._NETWORK_PROXY_URL` | cmd/scp client factory |
| `network.tls.ca_file` | path | none | file must parse at startup (`New`) | `..._NETWORK_TLS_CA_FILE` | cmd/scp client factory |
| `network.allow_private_addresses` | bool | `false` | SSRF policy exception for a GHES in a private network | `..._NETWORK_ALLOW_PRIVATE_ADDRESSES` | cmd/scp client factory (`netguard`) |
| `network.allow_insecure_http` | bool | `false` | permits `http://` for a lab host | `..._NETWORK_ALLOW_INSECURE_HTTP` | github (URL check), factory |
| `network.pin_hosts` | []string | derived hosts | hostnames only | `..._NETWORK_PIN_HOSTS_<j>` | cmd/scp client factory |
| `auth.kind` | enum | required | `github_app`, `pat`, `fine_grained_pat`, `oauth_user_token`, `token_command`, `anonymous` | `..._AUTH_KIND` | github (strategy) |
| `auth.client_id` | string | none | `client_id` or `app_id` required for `github_app` | `..._AUTH_CLIENT_ID` | auth (`iss`) |
| `auth.app_id` | int | none | same | `..._AUTH_APP_ID` | auth, webhook target check |
| `auth.private_key.file` | secret.Secret | required for `github_app` unless `private_keys` | file readable, PEM RSA >= 2048 bits, mode warned by doctor | `..._AUTH_PRIVATE_KEY_FILE` | auth |
| `auth.private_keys[]` | []SecretRef | none | rotation: current first | `..._AUTH_PRIVATE_KEYS_<j>_FILE` | auth |
| `auth.installations.mode` | enum | `allow_list` | `allow_list`, `pinned`, `all`; `all` must be explicit | `..._AUTH_INSTALLATIONS_MODE` | github (registry filter) |
| `auth.installations.accounts[]` | []string | none | required (non-empty) for `allow_list` | `..._AUTH_INSTALLATIONS_ACCOUNTS_<j>` | github |
| `auth.installations.ids[]` | []int | none | required for `pinned` | `..._AUTH_INSTALLATIONS_IDS_<j>` | github |
| `auth.token.file` | secret.Secret | required for PAT kinds | non-empty content | `..._AUTH_TOKEN_FILE` | auth (`Static`, re-read on mtime change) |
| `auth.command[]` | []string | required for `token_command` | argv, first element absolute path, no shell | `..._AUTH_COMMAND_<j>` | auth (`External`), runner in cmd/scp |
| `auth.timeout` | duration | `10s` | | `..._AUTH_TIMEOUT` | auth (command) |
| `auth.refresh_before` | duration | `5m` | | `..._AUTH_REFRESH_BEFORE` | auth (command) |
| `auth.ttl` | duration | `10m` | | `..._AUTH_TTL` | auth (command without `expires_at`) |
| `auth.dev` | bool | `false` | required `true` for `anonymous`; anonymous refused on `ghec_dr` | `..._AUTH_DEV` | github |
| `webhook.secret.file` | secret.Secret | required for `github_app` (and for the route to exist for any kind) | non-empty content | `..._WEBHOOK_SECRET_FILE` | github (`VerifierSource.Secrets[0]`); wiring registers the route only if set |
| `webhook.previous_secret.file` | secret.Secret | none | | `..._WEBHOOK_PREVIOUS_SECRET_FILE` | github (`Secrets[1]`, bounded overlap during rotation) |
| `webhook.enabled` | bool | `true` | a source with `enabled` and an empty secret is a startup error (GR-20) | `..._WEBHOOK_ENABLED` | **cmd/scp** (`Sources.IDs`; CD-CF-04, key defined by 04) |
| `webhook.default` | bool | implied for a single source | at most one `true` | `..._WEBHOOK_DEFAULT` | **webhook** (`Sources.Default`; CD-CF-04, key defined by 04) |
| `webhook.public_url` | URL | none | https | `..._WEBHOOK_PUBLIC_URL` | doctor (`app.hook`), manifest flow |
| `webhook.redelivery.enabled` | bool | `false` | App sources only | `..._WEBHOOK_REDELIVERY_ENABLED` | **cmd/scp** (registers `webhook.redelivery`) |
| `webhook.redelivery.interval` | duration | `6h` | | `..._WEBHOOK_REDELIVERY_INTERVAL` | **cmd/scp** (`freshness.Policy` of the job) |
| `webhook.redelivery.max_per_run` | int | `50` | 1..500 | `..._WEBHOOK_REDELIVERY_MAX_PER_RUN` | github (`FailedDeliveries` cap) |
| `webhook.redelivery.rate` | duration | `1s` | 0..1m | `..._WEBHOOK_REDELIVERY_RATE` | **cmd/scp** (pause between `Redeliver` calls inside the job; CD-CF-03) |
| `webhook.redelivery.max_age` | duration | `72h` | 1h..168h (github.com keeps 3 days, GHES documents 7) | `..._WEBHOOK_REDELIVERY_MAX_AGE` | github (`since = max(since, now - max_age)`; CD-CF-03) |
| `discovery.mode` | enum | required | `installation` (needs `github_app`), `org`, `user`, `explicit` | `..._DISCOVERY_MODE` | github (`ListRepositories`) |
| `discovery.org` | string | none | required for `org`, `user` | `..._DISCOVERY_ORG` | github |
| `discovery.repos[]` | []string | none | `owner/name`, required for `explicit` | `..._DISCOVERY_REPOS_<j>` | github |
| `discovery.visibility` | enum | `public_only` | `public_only`, `installation_scope` | `..._DISCOVERY_VISIBILITY` | github (filter); **cmd/scp** cross-check with `access.mode` |
| `discovery.include_archived` | bool | `true` | | `..._DISCOVERY_INCLUDE_ARCHIVED` | github |
| `discovery.include_forks` | bool | `false` | | `..._DISCOVERY_INCLUDE_FORKS` | github |
| `discovery.reconcile_every` | duration | `30m` | | `..._DISCOVERY_RECONCILE_EVERY` | **catalog** (`Policy` of `catalog.reconcile`) |
| `discovery.max_removal_ratio` | float | `0.3` | 0 to 1 | `..._DISCOVERY_MAX_REMOVAL_RATIO` | **catalog** (mass-removal guard) and github (installation guard) |
| `discovery.removal_confirmations` | int | `2` | 1..10 | `..._DISCOVERY_REMOVAL_CONFIRMATIONS` | **catalog** (absence rule R3; declared here because this unit owns the `discovery` struct, CD-CF-07) |
| `discovery.mass_removal_hold` | duration | `6h` | 0..168h | `..._DISCOVERY_MASS_REMOVAL_HOLD` | **catalog** (mass-removal hold R4; CD-CF-07) |
| `discovery.access_acknowledgement` | enum | `""` | `network_restricted`, `oidc_proxy`, `trusted_proxy_headers` | `..._DISCOVERY_ACCESS_ACKNOWLEDGEMENT` | **cmd/scp** (startup refusal), doctor |
| `acquisition.mode` | enum | `api` | `api`; `git` refused in v1 | `..._ACQUISITION_MODE` | **cmd/scp** (implementation choice) |
| `acquisition.graphql` | enum | `auto` | `auto`, `on`, `off` | `..._ACQUISITION_GRAPHQL` | github (`Capabilities().GraphQL`) |
| `acquisition.graphql_blobs` | enum | `off` | `off`, `auto`, `on` (staged, spike S-B) | `..._ACQUISITION_GRAPHQL_BLOBS` | github (`Capabilities().GraphQLBlobs`) |
| `acquisition.webhook_assisted` | bool | `true` | | `..._ACQUISITION_WEBHOOK_ASSISTED` | **cmd/scp** (registers reactors for the source; freshness degrades without it and `/status` says so) |
| `acquisition.git.*` | | see 4.15 | post-v1 | `..._ACQUISITION_GIT_*` | gitsource |

### 6.3 Validation rules (all violations are returned together, each with a path)

`Validate()` is pure (no I/O), implemented and tested (`TestConfigRejects`, `TestConfigDefaultsSection`). It enforces: id format and uniqueness; host kind and the URL rules (https unless `allow_insecure_http`; no userinfo, query or fragment; overrides only for `ghes`); required members per `auth.kind`; installation modes (`allow_list` needs accounts, `pinned` needs ids, `all` only when explicit); `webhook.secret` required for `github_app`; per-mode requirements of `discovery` (`installation` needs `github_app`, `org`/`user` need `org`, `explicit` needs `repos`); `webhook.redelivery.enabled` only for `github_app`; the `anonymous` rules (`dev: true`, not on `ghec_dr`); `acquisition.mode: git` refused; api version syntax; reserve ordering (`backfill >= scheduled >= webhook`, otherwise a lower-priority class could outrun a higher one); numeric ranges (`head_batch` at most 100, `webhook.max_body` at most 25 MiB, `concurrency` at most 100).

`New()` adds the I/O checks, with the same path-accurate error type: every `SecretRef` resolves to a **non-empty** value (GR-13); key files parse (RSA >= 2048, not encrypted); the CA file parses; webhook secret present wherever the route is registered (GR-20); `network.pin_hosts` contains every host the source will contact; a PEM or token file with group or world permissions is a doctor warning (not an error here, because some orchestrators cannot set modes).

Cross-section rules owned by `cmd/scp` (they need other units' config): `discovery.visibility: installation_scope` requires `access.mode: trusted-proxy` **or** a non-empty `discovery.access_acknowledgement`, and an acknowledgement alone is refused on a public listener without `trusted_proxies` (ADR-023); `acquisition.mode: git` in a build without `gitsource`; GHES and GHEC-DR require an explicit acknowledgement before the first private repository is published.

### 6.4 Example (App on github.com, and a PAT on GHES)

```yaml
github:
  defaults:
    api_version: auto
    budget: { reserve: { backfill: 40%, scheduled: 15%, webhook: 2% }, concurrency: 4, rps: 8, burst: 16 }
    webhook: { max_body: 25MiB, dedupe_ttl: 15m, dedupe_max: 10000 }
  sources:
    - id: acme
      host: { kind: github.com }
      auth:
        kind: github_app
        client_id: Iv23liExample
        private_key: { file: /run/secrets/scp-app.pem }
        installations: { mode: allow_list, accounts: [acme-org] }
      webhook:
        secret: { file: /run/secrets/scp-webhook }
        public_url: https://scp.example.org/webhooks/github/acme
        redelivery: { enabled: true, interval: 6h }
      discovery: { mode: installation, visibility: public_only, reconcile_every: 30m, max_removal_ratio: 0.3 }
      acquisition: { mode: api, graphql: auto, webhook_assisted: true }
    - id: internal
      host: { kind: ghes, web_url: https://git.corp.example }
      network: { allow_private_addresses: true, tls: { ca_file: /etc/ssl/corp-ca.pem } }
      auth: { kind: fine_grained_pat, token: { file: /run/secrets/ghes-pat } }
      discovery: { mode: org, org: platform, visibility: installation_scope, access_acknowledgement: network_restricted }
      acquisition: { mode: api, graphql: off }
```

## 7. HTTP and API surface

### 7.1 What this unit exposes

The unit owns **no route of the product** (`routes.Public()`/`Admin()` contain nothing from it). Its only HTTP surface is the **fake GitHub** (`githubtest` / `cmd/fakegithub`), which impersonates GitHub toward the adapter, and, on the outbound side, the fake's webhook deliveries toward the product's own `POST /webhooks/github/{source}`. The fake is a test and development seam: it exists in the `githubtest` package and in the separate `cmd/fakegithub` binary, never in `cmd/scp` (GR-27: no development feature in the production binary; `cmd/scp` imports `githubtest` only from `_test.go` files (architecture 5.1 rule 1: test-helper packages are imported from tests only). There is no `scp dev` subcommand and no `devtools` build tag (V-02: the first draft had both, which contradicts that rule and GR-27); a developer runs `make dev`, which starts `cmd/fakegithub` and `cmd/scp serve` as two processes, as architecture 12.4 already says).

### 7.2 Why the fake is designed as a world, not as canned responses

Three needs drive the design (G-13, GR-23, GR-25, GR-43):

1. **Adapter tests** must prove every row of the error table and the budget arithmetic against a server that behaves like GitHub, including time (rate-limit resets, token expiry, redelivery windows), so the fake runs under `testing/synctest` on an in-memory network.
2. **Product end-to-end tests** (`cmd/scp` boots `run()` against it) need a self-consistent GitHub: a push must change the commit list, the head SHA, the tree, the README **and** emit a correctly signed webhook, or the e2e test proves nothing about freshness.
3. **Developers and Playwright** need a GitHub with no network, no token and no rate limit unless asked, populated by readable fixtures, controllable at runtime (push a commit, rename a repository, suspend an installation, drop a webhook).

A table of canned JSON files satisfies none of these. The fake therefore keeps one `World` (accounts, App, installations, tokens, repositories with real git objects, releases, delivery outbox) and derives every response from it; fixtures are just a way to build a `World`.

### 7.3 The world, fixtures and git objects

State (`World`) is one struct guarded by one `RWMutex`; every mutator that GitHub would announce enqueues a webhook delivery in the same critical section, so the sequence "state changed, then event sent" is atomic and replayable. Mutator signatures (compile-checked):

```go
// Package githubtest is the fake GitHub: a deterministic, self-consistent GitHub.com / GHES / GHEC-DR REST, GraphQL,
// App and webhook simulator used by adapter tests, by cmd/scp end-to-end tests and (through cmd/fakegithub) by developers
// and Playwright. It imports scm only; fixtures are JSON (stdlib), not YAML (rank-1 matrix row allows no third party).
package githubtest

import (
	"crypto/rsa"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

// HostKind mirrors github.HostKind without importing github (the fake must not depend on the code under test).
type HostKind string

const (
	HostGitHubCom HostKind = "github.com"
	HostGHES      HostKind = "ghes"
	HostGHEC      HostKind = "ghec_dr"
)

// World is the whole simulated GitHub state. All methods are safe for concurrent use. Mutations that GitHub would
// announce (push, release, create, repository, installation*) enqueue webhook deliveries in the Hooks outbox.
type World struct { /* hosts, apps, installations, tokens, repos; guarded by one RWMutex */
}

type WorldOption func(*worldConfig)
type worldConfig struct{ host HostKind }

func NewWorld(opts ...WorldOption) *World { return &World{} }
func WithHostKind(k HostKind) WorldOption { return func(c *worldConfig) { c.host = k } }

// ---- accounts, apps, installations, credentials --------------------------------------------------------------

type AppSpec struct {
	ID          int64
	ClientID    string
	Slug        string
	Name        string
	PublicKey   *rsa.PublicKey // verifies App JWTs (RS256, exp <= 10 min, iat not in the future)
	Permissions map[string]string
	Events      []string
}
type InstallationSpec struct {
	App         int64
	Account     string
	AccountType string // Organization | User
	AccountID   int64
	Selection   string   // all | selected
	Repos       []string // "owner/name" for selection=selected
	Permissions map[string]string
}
type PATSpec struct {
	Token     string
	Login     string
	Kind      string // classic | fine_grained
	ExpiresAt time.Time
	Repos     []string // "owner/name" or "owner/*"; empty = everything the login can see
}

func (w *World) AddApp(a AppSpec) error                            { return nil }
func (w *World) AddInstallation(i InstallationSpec) (int64, error) { return 0, nil }
func (w *World) AddPAT(p PATSpec) error                            { return nil }

// ---- repositories ----------------------------------------------------------------------------------------------

type Author struct{ Name, Login, Email string }
type CommitSpec struct {
	Message string
	Author  Author
	Date    time.Time
	Files   map[string]string // path -> content ("" with Delete for removal)
	Delete  []string
}
type ReleaseSpec struct {
	Tag, Name   string
	Prerelease  bool
	Draft       bool
	PublishedAt time.Time
}
type RepoSpec struct {
	ID            int64
	Owner, Name   string
	Visibility    string // public | private | internal
	DefaultBranch string
	Description   string
	Topics        []string
	License       string // SPDX id
	Archived      bool
	Fork          bool
	Disabled      bool
	Stars         int
	OpenIssues    int // issues only; the REST open_issues_count the fake serves is OpenIssues+OpenPRs
	OpenPRs       int
	Commits       []CommitSpec // oldest first; each applies its Files to the previous tree; SHAs are real git hashes
	Releases      []ReleaseSpec
}

func (w *World) AddRepo(r RepoSpec) error { return nil }

// Mutations. Each returns after the world is updated and the webhook deliveries are queued.
func (w *World) Push(repo, branch string, c CommitSpec) (commitSHA string, err error) { return "", nil }
func (w *World) PushMany(repo, branch string, cs []CommitSpec) (headSHA string, err error) {
	return "", nil
}
func (w *World) CreateBranch(repo, branch, fromBranch string) error      { return nil }
func (w *World) DeleteBranch(repo, branch string) error                  { return nil }
func (w *World) Tag(repo, tag, branch string) error                      { return nil }
func (w *World) Publish(repo string, r ReleaseSpec) error                { return nil }
func (w *World) Rename(repo, newName string) error                       { return nil }
func (w *World) Transfer(repo, newOwner string) error                    { return nil }
func (w *World) SetVisibility(repo, vis string) error                    { return nil }
func (w *World) SetArchived(repo string, archived bool) error            { return nil }
func (w *World) SetTopics(repo string, topics []string) error            { return nil }
func (w *World) DeleteRepo(repo string) error                            { return nil }
func (w *World) Suspend(installation int64) error                        { return nil }
func (w *World) Unsuspend(installation int64) error                      { return nil }
func (w *World) RemoveInstallation(installation int64) error             { return nil }
func (w *World) GrantRepos(installation int64, repos ...string) error    { return nil }
func (w *World) RevokeRepos(installation int64, repos ...string) error   { return nil }
func (w *World) SetSelection(installation int64, selection string) error { return nil }
func (w *World) SetPermissions(installation int64, p map[string]string, accepted bool) error {
	return nil
}

// Head returns the current default-branch head commit and tree SHA of a repository (for assertions).
func (w *World) Head(repo string) (commitSHA, treeSHA string, err error) { return "", "", nil }

// ---- fixtures --------------------------------------------------------------------------------------------------

// FixtureWorld is world.json (schema 1). Unknown members are an error (DisallowUnknownFields).
type FixtureWorld struct {
	Schema        int              `json:"schema"`
	Host          FixtureHost      `json:"host"`
	Limits        *FixtureLimits   `json:"limits,omitempty"`
	Apps          []FixtureApp     `json:"apps,omitempty"`
	Installations []FixtureInstall `json:"installations,omitempty"`
	Tokens        []FixtureToken   `json:"tokens,omitempty"`
	Repos         []string         `json:"repos,omitempty"` // directories relative to world.json holding repo.json; empty = walk repos/*/*
	Faults        []FixtureFault   `json:"faults,omitempty"`
}
type FixtureHost struct {
	Kind              HostKind `json:"kind"`
	APIVersions       []string `json:"api_versions,omitempty"` // default per kind
	EnterpriseVersion string   `json:"enterprise_version,omitempty"`
	TreeMaxEntries    int      `json:"tree_max_entries,omitempty"` // default 100000; lower it to force truncated trees
	MinimalRepos      bool     `json:"minimal_repos,omitempty"`    // list endpoints omit optional repository members (exercises the GET /repos fallback)
}
type FixtureLimits struct {
	CorePerHour          int   `json:"core_per_hour,omitempty"`         // default 5000 (PAT) / scaling formula for apps
	GraphQLPerHour       int   `json:"graphql_per_hour,omitempty"`      // default 5000
	WindowSeconds        int   `json:"window_seconds,omitempty"`        // default 3600
	ConditionalFree      *bool `json:"conditional_free,omitempty"`      // 304 with Authorization is free (default true)
	SecondaryConcurrency int   `json:"secondary_concurrency,omitempty"` // more concurrent requests than this triggers a secondary limit (0 = never automatically)
	ETagAuthBound        *bool `json:"etag_auth_bound,omitempty"`       // JSON ETags depend on the credential (default true, research 03 2.4)
}
type FixtureApp struct {
	ID            int64             `json:"id"`
	ClientID      string            `json:"client_id"`
	Slug          string            `json:"slug"`
	Name          string            `json:"name"`
	PublicKeyFile string            `json:"public_key_file"` // PEM, relative to world.json; generated by `fakegithub keygen`
	Permissions   map[string]string `json:"permissions"`
	Events        []string          `json:"events,omitempty"`
	Webhook       *FixtureHook      `json:"webhook,omitempty"`
}
type FixtureHook struct {
	URL          string   `json:"url"`
	Secret       string   `json:"secret"`
	ContentType  string   `json:"content_type,omitempty"` // json (default)
	Events       []string `json:"events,omitempty"`
	PayloadStyle string   `json:"payload_style,omitempty"` // full (default) | no_path_lists | no_tree_id
	Active       *bool    `json:"active,omitempty"`
	SHA1Header   bool     `json:"sha1_header,omitempty"` // also send X-Hub-Signature (SHA-1): proves the receiver ignores it (GR-20)
}
type FixtureInstall struct {
	ID          int64             `json:"id"`
	App         int64             `json:"app"`
	Account     string            `json:"account"`
	AccountType string            `json:"account_type"` // Organization | User
	AccountID   int64             `json:"account_id"`
	Selection   string            `json:"selection"` // all | selected
	Repos       []string          `json:"repos,omitempty"`
	Permissions map[string]string `json:"permissions,omitempty"`
	Suspended   bool              `json:"suspended,omitempty"`
}
type FixtureToken struct {
	Token     string    `json:"token"`
	Login     string    `json:"login"`
	Kind      string    `json:"kind"` // classic | fine_grained
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Repos     []string  `json:"repos,omitempty"`
}
type FixtureRepo struct { // repo.json
	ID            int64            `json:"id"`
	Owner         string           `json:"owner"`
	Name          string           `json:"name"`
	Visibility    string           `json:"visibility"`
	DefaultBranch string           `json:"default_branch"`
	Description   string           `json:"description,omitempty"`
	Topics        []string         `json:"topics,omitempty"`
	License       string           `json:"license,omitempty"`
	Archived      bool             `json:"archived,omitempty"`
	Fork          bool             `json:"fork,omitempty"`
	Stars         int              `json:"stars,omitempty"`
	OpenIssues    int              `json:"open_issues,omitempty"`
	OpenPRs       int              `json:"open_prs,omitempty"`
	CreatedAt     time.Time        `json:"created_at,omitempty"`
	Commits       []FixtureCommit  `json:"commits"` // oldest first
	Releases      []FixtureRelease `json:"releases,omitempty"`
}
type FixtureCommit struct {
	Message        string            `json:"message"`
	Author         Author            `json:"author"`
	Date           time.Time         `json:"date"`
	Files          map[string]string `json:"files,omitempty"` // path -> literal content, or "@files/<name>" = file in the repo directory
	Delete         []string          `json:"delete,omitempty"`
	NoAuthorUser   bool              `json:"no_author_user,omitempty"`   // top-level "author": null (GR-29 case)
	NoCommitAuthor bool              `json:"no_commit_author,omitempty"` // commit.author missing (GR-29 case)
}
type FixtureRelease struct {
	Tag         string    `json:"tag"`
	Name        string    `json:"name"`
	Prerelease  bool      `json:"prerelease,omitempty"`
	Draft       bool      `json:"draft,omitempty"`
	PublishedAt time.Time `json:"published_at"`
}
type FixtureFault struct { // faults.json entries or the faults member of world.json: loaded into Faults at start
	Name   string  `json:"name"`
	Match  Matcher `json:"match"`
	Action string  `json:"action"` // status:503 | latency:2s | reset | truncate | primary_limit | secondary_limit:60s | bad_credentials | no_permission | suspended | not_found | empty_repo
	After  int     `json:"after,omitempty"`
	Times  int     `json:"times,omitempty"`
	Every  int     `json:"every,omitempty"`
	Prob   float64 `json:"probability,omitempty"`
}

var _ = scm.ErrNotFound
```

**Real git objects.** Blob, tree and commit ids are computed with git's own object format (`gitobjects.go`), so a client that verifies a blob by SHA, compares tree SHAs or feeds a commit SHA back as `If-None-Match` behaves exactly as against GitHub. The functions are checked against golden vectors produced with the real `git` binary (`TestObjectIDsMatchGitGoldenVectors`; `scripts/verify-git-vectors.sh` regenerates them with `git hash-object`, `git write-tree` and `git commit-tree` and diffs, which is why the Go test itself contains no `os/exec`).

```go
package githubtest

import (
	"crypto/sha1" //nolint:gosec // git object ids are SHA-1; identification, not security
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The fake computes REAL git object ids, so a client that verifies blobs by SHA (D-15), compares tree SHAs, or feeds a
// commit SHA back as a validator behaves against the fake exactly as against GitHub. Nothing here is a mock hash.

func hashObject(kind string, body []byte) string {
	h := sha1.New() //nolint:gosec
	h.Write([]byte(kind + " " + strconv.Itoa(len(body)) + "\x00"))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// GitBlobSHA is the object id of a blob.
func GitBlobSHA(data []byte) string { return hashObject("blob", data) }

// TreeEntry is one entry of a git tree object.
type TreeEntry struct {
	Mode string // 100644, 100755, 120000, 40000, 160000
	Name string
	SHA  string
}

func (e TreeEntry) isTree() bool { return e.Mode == "40000" }

// GitTreeSHA is the object id of a tree. Git sorts entries bytewise by name with a trailing "/" appended for trees.
func GitTreeSHA(entries []TreeEntry) string { return hashObject("tree", encodeTree(entries)) }

func encodeTree(entries []TreeEntry) []byte {
	es := append([]TreeEntry(nil), entries...)
	key := func(e TreeEntry) string {
		if e.isTree() {
			return e.Name + "/"
		}
		return e.Name
	}
	sort.Slice(es, func(i, j int) bool { return key(es[i]) < key(es[j]) })
	var b []byte
	for _, e := range es {
		raw, _ := hex.DecodeString(e.SHA)
		b = append(b, []byte(e.Mode+" "+e.Name)...)
		b = append(b, 0)
		b = append(b, raw...)
	}
	return b
}

// GitCommitSHA is the object id of a commit with a fixed +0000 zone (the fake never emits local zones).
func GitCommitSHA(tree string, parents []string, a Author, at time.Time, message string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "tree %s\n", tree)
	for _, p := range parents {
		fmt.Fprintf(&b, "parent %s\n", p)
	}
	ident := fmt.Sprintf("%s <%s> %d +0000", a.Name, a.Email, at.Unix())
	fmt.Fprintf(&b, "author %s\ncommitter %s\n\n%s", ident, ident, message)
	if !strings.HasSuffix(message, "\n") {
		b.WriteByte('\n')
	}
	return hashObject("commit", []byte(b.String()))
}

// BuildTrees turns a flat path->content map into git trees. It returns the root tree id, every tree by id, and every blob
// by id, which is what the git/trees and git/blobs endpoints serve.
func BuildTrees(files map[string]string) (root string, trees map[string][]TreeEntry, blobs map[string][]byte) {
	type dir struct {
		files map[string]string
		dirs  map[string]*dir
	}
	top := &dir{files: map[string]string{}, dirs: map[string]*dir{}}
	for p, c := range files {
		d := top
		parts := strings.Split(p, "/")
		for _, seg := range parts[:len(parts)-1] {
			if d.dirs[seg] == nil {
				d.dirs[seg] = &dir{files: map[string]string{}, dirs: map[string]*dir{}}
			}
			d = d.dirs[seg]
		}
		d.files[parts[len(parts)-1]] = c
	}
	trees, blobs = map[string][]TreeEntry{}, map[string][]byte{}
	var walk func(d *dir) string
	walk = func(d *dir) string {
		var es []TreeEntry
		for name, c := range d.files {
			id := GitBlobSHA([]byte(c))
			blobs[id] = []byte(c)
			es = append(es, TreeEntry{Mode: "100644", Name: name, SHA: id})
		}
		for name, sub := range d.dirs {
			es = append(es, TreeEntry{Mode: "40000", Name: name, SHA: walk(sub)})
		}
		id := GitTreeSHA(es)
		trees[id] = es
		return id
	}
	return walk(top), trees, blobs
}
```

Commit dates come from the fixture, never from the wall clock, so SHAs are stable across runs; the author and committer are the same identity with a `+0000` zone; a commit's tree is the previous tree with the commit's `files` applied and `delete` removed.

**Fixture format** (JSON, strict decode with `DisallowUnknownFields`; YAML is not used because the rank-1 row of `githubtest` allows no third-party module, and JSON needs none). A fixture directory:

```
worlds/demo/
  world.json                      host, limits, apps, installations, tokens, repos list, faults
  app.pub.pem                     public key of the demo App; NOT committed: `fakegithub keygen --out worlds/demo` writes it next to the private `app.pem` (both git-ignored), V-16
  repos/<owner>/<name>/repo.json  repository metadata, commits (oldest first), releases
  repos/<owner>/<name>/files/...  file bodies referenced as "@files/<name>" in a commit's `files`
```

`world.json` members: `schema` (1), `host{kind, api_versions, enterprise_version, tree_max_entries}`, `limits{core_per_hour, graphql_per_hour, window_seconds, conditional_free, etag_auth_bound, secondary_concurrency}`, `apps[]` (id, client id, slug, public key file, permissions, events, `webhook{url, secret, content_type, events, payload_style, active}`), `installations[]` (id, app, account, selection, repos, permissions, suspended), `tokens[]` (PATs: token, login, `classic|fine_grained`, expiry, repo scope), `repos[]` (directories), `faults[]` (name, `match`, `action`, `after`, `times`, `every`, `probability`). A commit may carry `no_author_user: true` (the top-level `author` is `null`) or `no_commit_author: true` (`commit.author` absent): the two shapes that most often break commit decoding (GR-29) exist as fixture flags so every decode test and the e2e suite can include them in a normal world.

The `Fixture*` types at the end of the `world.go` listing in 7.3 are what the loader decodes into (compile-checked; `TestDemoFixtureDecodes` decodes the demo world with them).

The demo world (decoded by that test; the repository files are under `20-design/05-github-adapter.assets/worlds/demo/`):

```json
{
  "schema": 1,
  "host": { "kind": "ghes", "enterprise_version": "3.18.0" },
  "limits": { "core_per_hour": 5000, "graphql_per_hour": 5000, "window_seconds": 3600, "conditional_free": true, "etag_auth_bound": true },
  "apps": [
    {
      "id": 4242,
      "client_id": "Iv1.demo4242",
      "slug": "scp-demo",
      "name": "SCP demo",
      "public_key_file": "app.pub.pem",
      "permissions": { "metadata": "read", "contents": "read" },
      "events": ["push", "release", "create", "repository", "installation", "installation_repositories"],
      "webhook": { "url": "http://127.0.0.1:9090/webhooks/github/acme", "secret": "dev-webhook-secret", "payload_style": "full" }
    }
  ],
  "installations": [
    { "id": 1001, "app": 4242, "account": "acme", "account_type": "Organization", "account_id": 77, "selection": "all",
      "permissions": { "metadata": "read", "contents": "read" } }
  ],
  "tokens": [
    { "token": "scp-demo-pat-classic-0001", "login": "demo-user", "kind": "classic" }
  ],
  "repos": ["repos/acme/payments-api", "repos/acme/docs-site", "repos/acme/archived-tool"],
  "faults": []
}
```

```json
{
  "id": 5001, "owner": "acme", "name": "payments-api", "visibility": "public", "default_branch": "main",
  "description": "Payments service", "topics": ["payments", "java"], "license": "Apache-2.0", "stars": 12,
  "open_issues": 3, "open_prs": 2, "created_at": "2024-01-10T09:00:00Z",
  "commits": [
    { "message": "Initial commit\n\nScaffold", "author": { "name": "Ada Lovelace", "login": "ada", "email": "ada@example.org" },
      "date": "2024-01-10T09:00:00Z",
      "files": { "README.md": "@files/README.md", "pom.xml": "@files/pom.xml", "docs/overview.md": "# Overview\n" } },
    { "message": "Add architecture notes", "author": { "name": "Grace Hopper", "login": "grace", "email": "grace@example.org" },
      "date": "2024-02-01T12:30:00Z", "files": { "docs/architecture.md": "# Architecture\n" } },
    { "message": "Dependabot bump", "author": { "name": "dependabot[bot]", "login": "", "email": "" },
      "date": "2024-03-05T08:00:00Z", "no_author_user": true, "files": { "docs/overview.md": "# Overview\n\nUpdated.\n" } },
    { "message": "Commit without author block", "author": { "name": "Unknown", "login": "", "email": "" },
      "date": "2024-03-06T08:00:00Z", "no_commit_author": true, "files": { "CHANGELOG.md": "# Changelog\n" } }
  ],
  "releases": [ { "tag": "v1.0.0", "name": "1.0.0", "published_at": "2024-02-02T10:00:00Z" } ]
}
```

`docs-site` carries an AsciiDoc README (exercises `RenderedFile`), `archived-tool` is private and archived (must be hidden under `public_only`), `payments-api` has the null-author and missing-author commits, one release, a `pom.xml` for `manifests`, and docs pages. Larger worlds for load and budget tests are generated, not committed: `fakegithub gen --repos 1000 --seed 7` writes a deterministic world (1,000 repositories, 5 percent with 3 manifests, 2 percent with an AsciiDoc README) used by `TestBudgetColdStart` and the benchmark.

### 7.4 Server, hosts and URL styles

```go
package githubtest

import (
	"context"
	"io/fs"
	"net/http"
	"testing"
	"time"
)

// LoadWorld reads world.json and the repository directories from fsys (os.DirFS in tests and in cmd/fakegithub).
func LoadWorld(fsys fs.FS) (*World, error) { return NewWorld(), nil }

type ServerOption func(*serverConfig)
type serverConfig struct{}

func WithClock(now func() time.Time) ServerOption { return func(*serverConfig) {} } // default time.Now (virtual under synctest)
func WithPathPrefix(p string) ServerOption        { return func(*serverConfig) {} } // "/api/v3" for GHES style bases
func WithWebBase(url string) ServerOption         { return func(*serverConfig) {} }
func WithSeed(seed uint64) ServerOption           { return func(*serverConfig) {} } // fault probabilities, delivery guids

// Server is the HTTP face of a World.
type Server struct { /* world, faults, hooks, ledger, handler */
}

func NewServer(w *World, opts ...ServerOption) *Server { return &Server{} }

// Handler serves REST, GraphQL, the App endpoints, the manifest endpoints and the /_fake control API.
func (s *Server) Handler() http.Handler { return nil }
func (s *Server) World() *World         { return nil }
func (s *Server) Faults() *Faults       { return nil }
func (s *Server) Hooks() *Hooks         { return nil }
func (s *Server) Ledger() *Ledger       { return nil }

// NewTestServer starts the fake behind httptest.NewServer and registers cleanup. URL helpers return what a source
// configuration needs (kind ghes, api_url = APIURL, graphql_url = GraphQLURL, web_url = WebURL).
func NewTestServer(t testing.TB, w *World, opts ...ServerOption) *TestServer { return &TestServer{} }

// NewBubbleServer is the same on httptest.NewTestServer (Go 1.27): an in-memory network usable inside a
// testing/synctest bubble, so rate-limit resets, token expiry and webhook delays run on virtual time.
func NewBubbleServer(t testing.TB, w *World, opts ...ServerOption) *TestServer { return &TestServer{} }

type TestServer struct{ *Server }

func (s *TestServer) APIURL() string       { return "" }
func (s *TestServer) GraphQLURL() string   { return "" }
func (s *TestServer) WebURL() string       { return "" }
func (s *TestServer) Client() *http.Client { return nil }

// ---- fault injection -------------------------------------------------------------------------------------------

// Matcher selects requests. Empty fields match everything. Route is the Go 1.22 ServeMux pattern of the fake's own
// route table ("GET /repos/{owner}/{repo}/commits"); Repo is "owner/name"; Credential is a token prefix or "app-jwt".
type Matcher struct {
	Route      string `json:"route,omitempty"`
	Method     string `json:"method,omitempty"`
	Repo       string `json:"repo,omitempty"`
	Credential string `json:"credential,omitempty"`
	PathPrefix string `json:"path_prefix,omitempty"`
	Query      string `json:"query,omitempty"`  // substring of the raw query
	Accept     string `json:"accept,omitempty"` // substring of the Accept header (raw, html, sha media types)
}

// Action is one injected behaviour; constructors below. Actions compose with Then.
type Action interface{ isAction() }
type action struct{}

func (action) isAction() {}

func Status(code int, message string) Action         { return action{} } // JSON error body {"message":...}
func Latency(d time.Duration) Action                 { return action{} } // sleeps (virtual in a bubble) before answering
func Reset() Action                                  { return action{} } // closes the connection without a response
func TruncateBody(after int) Action                  { return action{} } // sends a Content-Length larger than the body, then closes
func MalformedJSON() Action                          { return action{} }
func PrimaryLimit(resetIn time.Duration) Action      { return action{} } // 403 + x-ratelimit-remaining 0, no Retry-After
func SecondaryLimit(retryAfter time.Duration) Action { return action{} } // 403/429 + Retry-After (0 = omit: "wait at least a minute")
func BadCredentials() Action                         { return action{} } // 401 {"message":"Bad credentials"}
func NoPermission() Action                           { return action{} } // 403 "Resource not accessible by integration"
func MintFails(status int) Action                    { return action{} } // installation token endpoint: 403 suspended / 404 removed
func ExpireTokens() Action                           { return action{} } // invalidates every installation token now
func TreeTruncated() Action                          { return action{} } // forces "truncated": true on git/trees
func EmptyRepo() Action                              { return action{} } // 409 on commits
func WrongAPIVersion(selected string) Action         { return action{} } // x-github-api-version-selected differs (research 03 A11)
func SkewDate(by time.Duration) Action               { return action{} } // Date header offset (doctor skew check)
func Then(a, b Action) Action                        { return action{} }

type Fault struct {
	Name        string
	Match       Matcher
	Do          Action
	After       int     // skip the first N matching requests
	Times       int     // stop after N injections (0 = unlimited)
	Every       int     // inject on every Nth matching request (0 = all)
	Probability float64 // 0 = always; otherwise seeded
}
type FaultID int
type Faults struct{}

func (f *Faults) Add(x Fault) FaultID { return 0 }
func (f *Faults) Remove(id FaultID)   {}
func (f *Faults) Clear()              {}
func (f *Faults) Hits(id FaultID) int { return 0 }

// ---- webhook simulation ----------------------------------------------------------------------------------------

type PayloadStyle uint8

const (
	PayloadFull        PayloadStyle = iota // commits[] with added/removed/modified and tree_id (the documented-by-octokit shape)
	PayloadNoPathLists                     // commits[] present, path arrays absent (V13 pessimistic case)
	PayloadNoTreeID                        // no head_commit.tree_id
)

type HookTarget struct {
	URL            string
	Secret         string
	PreviousSecret string // accepted for rotation tests on the receiving side; the fake always signs with Secret
	Events         []string
	Style          PayloadStyle
	Level          string // app | org | repo (decides headers and installation member)
	SHA1Header     bool   // also send X-Hub-Signature (SHA-1); a correct receiver ignores it
	Active         bool
}

type Delivery struct {
	ID           int64
	GUID         string
	Event        string
	Action       string
	Repo         string
	Installation int64
	Redelivery   bool
	At           time.Time
	StatusCode   int // 0 = connection failure
	Status       string
	Duration     time.Duration
}

// Hooks is the outbox and delivery log. Deliveries happen only on Flush (deterministic) or in Run (dev server).
type Hooks struct{}

func (h *Hooks) Configure(t HookTarget)                        {}
func (h *Hooks) Flush(ctx context.Context) ([]Delivery, error) { return nil, nil }
func (h *Hooks) Run(ctx context.Context) error                 { return nil }
func (h *Hooks) Pending() int                                  { return 0 }
func (h *Hooks) Log() []Delivery                               { return nil }
func (h *Hooks) DropNext(n int)                                {} // GitHub-side loss: never logged, cannot be redelivered (2026-07-14 incident)
func (h *Hooks) DuplicateNext(n int)                           {}
func (h *Hooks) DelayNext(n int, d time.Duration)              {}
func (h *Hooks) Reorder()                                      {}             // reverse the pending outbox
func (h *Hooks) Redeliver(id int64) error                      { return nil } // keeps the GUID, Redelivery=true

// ---- call ledger (budget tests) --------------------------------------------------------------------------------

type Call struct {
	Seq         int
	At          time.Time
	Method      string
	Route       string // pattern
	Path        string
	Repo        string
	Credential  string // "app-jwt", "inst:<id>", "pat:<login>", "anon"
	Accept      string
	Status      int
	Resource    string // core | graphql | search | none (free)
	Charged     int    // points charged to the bucket (0 for a free 304, 1 for REST, N for GraphQL cost)
	Conditional bool
	NotModified bool
	Injected    string // fault name, "" if none
}
type Ledger struct{}

func (l *Ledger) Calls() []Call                   { return nil }
func (l *Ledger) Count(match func(Call) bool) int { return 0 }
func (l *Ledger) Charged(resource string) int     { return 0 }
func (l *Ledger) Reset()                          {}

// Unhandled lists the calls to routes the fake does not register (a test fails when it is not empty at the end).
func (l *Ledger) Unhandled() []Call { return nil }

// Violations lists protocol violations by the client under test: a credential in a query string (GR-13), an
// Authorization header sent to a foreign origin, a missing or malformed User-Agent or API-version header.
func (l *Ledger) Violations() []string { return nil }
```

- **URL styles.** `NewServer(w)` serves the github.com style (REST at `/`, GraphQL at `/graphql`); `WithPathPrefix("/api/v3")` serves the GHES style (REST under `/api/v3`, GraphQL at `/api/graphql`); a GHEC-DR world uses the github.com style on its own host. The adapter test table runs the same scenarios in all three styles with a `source` configured accordingly (`host.kind`, `api_url`, `graphql_url`).
- **Three ways to run it.** `NewTestServer(t, w)`: `httptest.NewServer` on a real loopback port, for tests that do not use `synctest`. `NewBubbleServer(t, w)`: `httptest.NewTestServer` (Go 1.27) on an in-memory network that works **inside** a `testing/synctest` bubble, so `time.Sleep`, token expiry, rate-limit reset and delivery delays run on virtual time without waiting; the adapter is configured with `ts.Client()`. `cmd/fakegithub`: a real port for developers, Playwright and shell-driven e2e.
- **Clock.** The fake reads time only through `WithClock(func() time.Time)`; the default is `time.Now`, which **is** the bubble's virtual clock inside `synctest`. `Date` headers, `x-ratelimit-reset`, token `expires_at`, delivery times and JWT validation all use it.
- **Determinism.** `WithSeed` seeds every random decision (fault probabilities, delivery GUIDs, token bodies, installation token suffixes); fixture dates give the commit SHAs; map iteration never leaks into output (all lists are sorted). Two runs of the same scenario produce byte-identical request ledgers.

### 7.5 Endpoint catalogue

The fake registers exactly the routes below on a Go 1.22 `ServeMux` and nothing else; an unregistered path answers 404 `{"message":"Not Found"}` **and is recorded in the ledger as `unhandled`**, which makes any test that reaches an endpoint the adapter design does not know about fail loudly (`ledger.Unhandled()` must be empty at test end). Auth column: `jwt` = App JWT, `inst` = installation token, `user` = PAT/OAuth user token, `any` = any valid credential or anonymous, `none`.

| Route | Auth | Behaviour and fidelity notes |
|---|---|---|
| `GET /rate_limit` | any | `resources{core, graphql, search, integration_manifest, ...}`, `rate` omitted under version `2026-03-10`; not charged |
| `GET /user` | user | login, id; header `x-oauth-scopes` for classic tokens; `github-authentication-token-expiration` for expiring fine-grained tokens |
| `GET /app` | jwt | id, slug, name, owner, permissions, events |
| `GET /app/installations` | jwt | paginated, account, `repository_selection`, permissions, events, `suspended_at`, `updated_at` |
| `GET /app/installations/{id}` | jwt | one installation; 404 if unknown |
| `POST /app/installations/{id}/access_tokens` | jwt | body optional (`repository_ids`, `repositories`, `permissions`, narrowing only, at most 500 repositories); 201 `{token: "ghs_" + deterministic suffix, expires_at: now+1h, permissions, repository_selection}`; 403 if suspended, 404 if removed or foreign, 422 if the body widens the grant; every minted token is valid until `expires_at` or until `ExpireTokens()` |
| `GET /app/hook/config` | jwt | `url`, `content_type`, `insecure_ssl` |
| `GET /app/hook/deliveries` | jwt | newest first, cursor pagination (`cursor`, `Link rel=next`), items `{id, guid, delivered_at, redelivery, duration, status, status_code, event, action, installation_id, repository_id, throttled_at}`; retention 72 h of fake time |
| `POST /app/hook/deliveries/{id}/attempts` | jwt | 202; re-sends the **same payload with the same `X-GitHub-Delivery`**, `redelivery: true` in the log; 404 for a dropped (never logged) delivery |
| `GET /installation/repositories` | inst | `total_count`, `repository_selection`, `repositories[]` full repository objects visible to the installation; `per_page` (max 100), `page`, `Link` |
| `GET /orgs/{org}/repos` | user, inst | minimal-repository objects (optional members present as in GitHub's example; a `minimal_repos: true` world option drops `default_branch` to exercise the fallback), `type`, `sort`, `direction`, pagination |
| `GET /users/{login}/repos` | user, inst | same shape |
| `GET /repos/{owner}/{repo}` | any | full repository object incl. `id`, `visibility`, `topics`, `license`, `stargazers_count`, `open_issues_count` (= issues + pull requests, as GitHub), `default_branch`, `archived`, `disabled`, `pushed_at`; **301** to the new name after `Rename` (same origin) until the old name is reused; 404 for invisible repositories (never 403) |
| `GET /repos/{owner}/{repo}/readme` | any | content negotiation: `application/vnd.github+json` (JSON object, base64 `content`, `encoding`, `sha`, `path`, `name`, `size`), `.raw+json` (bytes), `.html+json` (rendered HTML; AsciiDoc rendered by a tiny deterministic converter that produces `<div class="sect1">`-style output, Markdown to `<p>`/`<h1>` with `user-content-` anchors; the fake does **not** try to be GitHub's renderer, tests only assert pass-through and sanitising); files over 1 MiB: JSON has `encoding: "none"`, `content: ""`; `ref`; `If-None-Match` honoured |
| `GET /repos/{owner}/{repo}/readme/{dir...}` | any | README in a directory |
| `GET /repos/{owner}/{repo}/contents/{path...}` | any | file object, directory array (capped at 1,000 entries), `symlink` and `submodule` objects, raw and html media types, `ref`; 404 for a missing path |
| `GET /repos/{owner}/{repo}/commits` | any | newest first; `sha`, `per_page` (max 100), `page`, `since`, `until`; items with `sha`, `html_url`, `commit{message, author, committer}`, top-level `author` (or `null` / `{}` per fixture flags), `parents`; 409 `{"message":"Git Repository is empty."}` for an empty repository |
| `GET /repos/{owner}/{repo}/commits/{ref}` | any | `Accept: application/vnd.github.sha` returns the bare SHA with `ETag: "<sha>"` and honours `If-None-Match: "<sha>"` (304 whatever the credential); otherwise the full commit with `stats`/`files` absent in the fake |
| `GET /repos/{owner}/{repo}/releases/latest` | any | latest non-draft non-prerelease by `created_at`; 404 when none |
| `GET /repos/{owner}/{repo}/releases` | any | list, drafts hidden |
| `GET /repos/{owner}/{repo}/git/trees/{tree_sha}` | any | `recursive=1`, branch name or SHA; `truncated: true` when entries exceed `host.tree_max_entries`; entries `path`, `mode`, `type`, `sha`, `size`, `url` |
| `GET /repos/{owner}/{repo}/git/blobs/{sha}` | any | JSON `{content, encoding: base64, sha, size}` or raw media type; 404 for an unknown sha |
| `POST /graphql` (`/api/graphql`) | inst, user | the subset of section 7.8; separate `graphql` bucket |
| `GET /settings/apps/new`, `GET /organizations/{org}/settings/apps/new` | none | accepts the manifest form POST (the fake shows the manifest and immediately redirects to the `redirect_url` with `code` and the supplied `state`), for manifest-flow tests |
| `POST /app-manifests/{code}/conversions` | none | one-time `code` returns `id`, `slug`, `client_id`, `pem` (a freshly generated 2048-bit key whose public half is registered), `webhook_secret`, `html_url`; a second use is 404 |
| `/_fake/...` | none (dev server and tests only) | control API, section 7.7 |

**Authentication simulation.** `jwt`: RS256 verified against the App's registered public key; `iat` in the future, `exp` in the past, `exp - iat` over 10 minutes, wrong key, wrong `iss`, or an installation token used where a JWT is required are 401 with messages that approximate GitHub's (the adapter matches only the phrases of fidelity note F-4 below). `inst`/`user`: tokens are looked up; expired is 401 `Bad credentials`; **visibility**: a repository the credential cannot see is 404; **permissions**: an installation lacking `contents: read` gets 403 `Resource not accessible by integration` on contents, readme, commits, trees, blobs, releases. A token minted with `repository_ids` sees only those repositories. Anonymous requests are allowed on public repositories only, with the 60 per hour per-IP bucket and the real 403 body, no `Retry-After`.

**Pagination and headers.** Lists clamp `per_page` to 100 (silently, like GitHub), emit absolute `Link` URLs on the server's own origin (`rel="next"`, `last`, `first`, `prev`; `last` omitted for cursor lists), and every response carries `x-github-request-id` (deterministic), `x-github-api-version-selected`, `Date`, and, when authorised, the five `x-ratelimit-*` headers. `ETag`/`Last-Modified` are present on the endpoints that have them.

### 7.6 Conditional requests, rate limits and secondary limits

- **ETag binding.** With `etag_auth_bound` (default true, research 03 2.4) the JSON-list ETag is `W/"` + hex SHA-256 of `Accept ":" Authorization ":" body` + `"` so a different token yields a different validator and an old validator with a new token yields 200, the behaviour that makes persisted ETags useless across App tokens. README raw and html ETags are the blob SHA; the commit-SHA media type uses the commit SHA (token independent). `false` makes every ETag token independent, to test the optimistic path.
- **Free 304s.** With `conditional_free` (default true) an authorised 304 is not charged to the primary bucket; an anonymous 304 is charged (matches the observation of 2026-10-13, research 03 appendix A).
- **Primary limits.** Buckets per credential on the fake clock: PAT per login (5,000), App installation per installation (`min(12,500, 5,000 + 50 x max(0, repos-20) + 50 x max(0, users-20))`; GHEC world 15,000 REST and 10,000 GraphQL), anonymous per IP (60), GraphQL separate. A window is `window_seconds` (3,600) from the first charged request; `reset` is its end. When `remaining` hits 0 the next request is 403 with `x-ratelimit-remaining: 0`, no `Retry-After`, message "API rate limit exceeded for installation ID n."; the bucket refills at `reset`.
- **Secondary limits.** Not predictable on GitHub, so they are driven by faults (`SecondaryLimit(retryAfter)`: 403 or 429 with `Retry-After`, or without it for the "wait at least a minute" case) and, optionally, by `limits.secondary_concurrency`: when more than that many requests are in flight the extra ones receive a secondary-limit response. A successful request does not clear the fault; the fault's `times` does.
- **`GET /rate_limit`** is free and reflects the buckets exactly, so doctor's `rate` check is testable.

### 7.7 Fault injection and the control API

A **fault** is `{Name, Match, Do, After, Times, Every, Probability}`: `Match` selects requests (`route` pattern of the fake's own table, `method`, `repo`, `credential` as a token prefix or `app-jwt`, `path_prefix`, `query` substring, `accept` substring), `Do` is an `Action` (constructors in the code above; `Then(a, b)` composes), `After` skips the first N matches, `Times` stops after N injections, `Every` injects on every Nth match, `Probability` is seeded. Faults are evaluated in registration order before the real handler; the first that fires wins; the ledger records the fault name on the call.

**Every row of the classification table (4.3) has a named recipe** that the adapter tests use and that the e2e suite reuses by name from JSON:

| 4.3 row | Fault recipe (`action` string in `faults.json`) |
|---|---|
| secondary limit with `Retry-After` | `{route: "GET /repos/{owner}/{repo}/commits", action: "secondary_limit:60s", times: 1}` |
| secondary limit without header | `secondary_limit:0` |
| primary limit | `primary_limit:1h` (or exhaust the bucket with `limits.core_per_hour: 20`) |
| 401, refreshed once, then paused | `bad_credentials` on `credential: "ghs_"`, `times: 1` (refresh succeeds) versus unlimited (pause) |
| 403 permission missing | `no_permission` or an installation without `contents` |
| 403 other | `status:403:Repository access blocked` |
| 404 repository / file / installation | `not_found` on a route, `Remove`/`Suspend` mutators, a missing README |
| suspended mint | `mint_fails:403`, `Suspend(id)` |
| 409 empty repository | `empty_repo` |
| 5xx, timeout, reset, truncated body | `status:503`, `latency:2m` (virtual in a bubble), `reset`, `truncate:100` |
| undecodable 2xx | `malformed_json` |
| too large | a fixture file over `limits.file_bytes`, `tree_max_entries` lowered |
| wrong API version | `wrong_api_version:2022-11-28` |
| clock skew (doctor) | `skew_date:2m` |

**Control API** (the dev server and tests; JSON over HTTP; mirrors the `World`/`Faults`/`Hooks`/`Ledger` Go methods one to one so shell-driven demos and Go tests do the same things):

| Endpoint | Effect |
|---|---|
| `GET /_fake/state` | counts of repositories, installations, tokens, pending deliveries |
| `POST /_fake/push` | `{repo, branch, message, author, files{path: content}, delete[]}` appends a commit, advances the branch, queues a `push` delivery (real path lists, `tree_id`) |
| `POST /_fake/branch`, `/tag`, `/release`, `/rename`, `/transfer`, `/visibility`, `/archive`, `/topics`, `DELETE /_fake/repos/{owner}/{repo}` | the matching mutator and its delivery |
| `POST /_fake/installations/{id}/suspend`, `/unsuspend`, `/remove`, `/grant`, `/revoke`, `/permissions` | installation lifecycle, with `installation` / `installation_repositories` deliveries |
| `POST /_fake/tokens/expire` | invalidate all installation tokens now |
| `POST /_fake/faults`, `GET /_fake/faults`, `DELETE /_fake/faults/{id}`, `DELETE /_fake/faults` | fault management, hit counts |
| `POST /_fake/hooks/flush`, `GET /_fake/hooks/log`, `POST /_fake/hooks/drop?n=`, `/duplicate?n=`, `/delay?n=&d=`, `/reorder`, `POST /_fake/hooks/redeliver/{id}`, `POST /_fake/hooks/target` | webhook outbox control (7.9) |
| `GET /_fake/ledger`, `POST /_fake/ledger/reset`, `GET /_fake/ledger/unhandled` | the call ledger (7.10) |
| `POST /_fake/time/advance` | dev server only: offsets the fake clock (a bubble test just sleeps) |
| `POST /_fake/reload` | re-reads the fixture directory |

### 7.8 GraphQL simulation

A purpose-built executor, not a general GraphQL engine. It parses the executable subset the adapter sends (operation with variables, aliases, field arguments as variables or literals, named fragments and spreads, inline fragments `... on Commit`) and resolves it against a hand-written type table: `Query{repository(owner,name), rateLimit}`, `Repository{databaseId, nameWithOwner, isArchived, isFork, isDisabled, visibility, pushedAt, stargazerCount, licenseInfo{spdxId}, issues(first,states){totalCount}, pullRequests(first,states){totalCount}, latestRelease{...}, defaultBranchRef{name, target{... on Commit{oid, messageHeadline, committedDate, tree{oid}}}}, object(expression:){... on Blob{oid, byteSize, text, isTruncated}}}`, `Release`, `License`, `RateLimit`. Behaviours that matter for the adapter:

- **Strictness catches drift**: an unknown field answers HTTP 200 with `errors[{type:"undefinedField", message:"Field 'x' doesn't exist on type 'Repository'"}]` (what GitHub does), a connection without `first`/`last` is an error, `first` over 100 is an error; so a typo in `buildHeadsQuery` fails the adapter's test instead of returning silently empty data. The same document is also checked against GitHub's real public schema by `TestHeadsQueryMatchesPublicSchema` when `SCP_GRAPHQL_SCHEMA` points at a downloaded `schema.docs.graphql` (weekly CI job; the schema is 1.5 MB and is not committed).
- **Partial failure**: an unresolvable repository returns `data.rN: null` plus `errors[{type:"NOT_FOUND", path:["rN"], message:"Could not resolve to a Repository with the name 'o/n'."}]`, status 200.
- **Cost**: computed with the documented formula (connection requests divided by 100, rounded, minimum 1) and charged to the `graphql` bucket; `rateLimit{cost remaining resetAt}` returns it; node count over 500,000 is `MAX_NODE_LIMIT_EXCEEDED`.
- **Failure modes**: `GraphQLRateLimited()` (200 with `RATE_LIMITED`; whether real GitHub answers 200 or 403 for an exhausted GraphQL bucket is unverified, U-7, so the adapter accepts both and the fake can produce both), `Latency` beyond 10 s virtual time answers 502 and deducts 20 extra points from the next window (documented behaviour), `Status(502)`.
- **Authentication**: GraphQL needs a token; anonymous is 401 (documented), which the adapter turns into "GraphQL off for 1 h".

### 7.9 Webhook simulation

`Hooks` is an **outbox plus a delivery log**. Mutators enqueue; delivery is explicit in tests (`Flush(ctx)` sends everything pending and returns the log entries, deterministic order) and automatic in the dev server (`Run(ctx)` delivers each item after `200 ms` of fake time, retrying nothing: like GitHub, a failed delivery is **not** retried automatically; it appears in the log with the receiver's status and can be redelivered through the API).

Each delivery is an HTTP POST to the configured target with GitHub's headers: `Content-Type: application/json`, `User-Agent: GitHub-Hookshot/<fake>`, `X-GitHub-Event`, `X-GitHub-Delivery` (deterministic GUID), `X-GitHub-Hook-ID`, `X-GitHub-Hook-Installation-Target-ID` (app id for App hooks), `X-Hub-Signature-256` (`github.SignSHA256` equivalent computed inside the fake from `HookTarget.Secret`; the fake does not import `github`), and **no** `X-Hub-Signature` unless `sha1_header: true` is set, which lets the adapter and webhook tests prove the SHA-1 header is ignored (GR-20).

Payloads follow the documented shapes; fidelity is chosen per world by `payload_style`:

| Style | `push` content | Why |
|---|---|---|
| `full` (default) | `commits[]` with `id`, `tree_id`, `added`, `removed`, `modified`; `head_commit` with the same; `before`, `after`, `created`, `deleted`, `forced`, `ref`, `repository`, `sender`, `installation` | the documented-by-octokit shape |
| `no_path_lists` | `commits[]` present, the three arrays absent | the pessimistic case of research V13: verifies `PathsComplete=false` end to end |
| `no_tree_id` | no `head_commit.tree_id` | verifies the fallback to a tree fetch |

Rules the fake enforces like GitHub: more than 2,048 commits in a push means the array is truncated to 2,048 (and `commits` omitted entirely for a push that exceeds the payload cap of 25 MB: **no delivery at all**, nothing in the log); `ping` is sent when the hook is created or by `POST /_fake/hooks/ping`; the `installation` member is present for App hooks; `repository` events carry `action` and, for renames, `changes.repository.name.from`; `release` carries the release; `create` carries `ref_type`.

Loss, duplication and disorder are first-class: `DropNext(n)` loses the next n deliveries on GitHub's side (never logged, cannot be redelivered; the 2026-07-14 class of incident, which is why the product's sweep, not the webhook, is the source of truth), `DuplicateNext(n)` sends the same GUID twice, `DelayNext(n, d)` holds them, `Reorder()` reverses the pending queue, `Redeliver(id)` repeats an attempt with the original GUID and `redelivery: true`. The delivery log keeps 72 hours of fake time and powers `GET /app/hook/deliveries`.

### 7.10 The call ledger

Every request is recorded as a `Call{Seq, At, Method, Route (pattern), Path, Repo, Credential ("app-jwt", "inst:<id>", "pat:<login>", "anon"), Accept, Status, Resource, Charged, Conditional, NotModified, Injected}`. `Ledger.Count(match)`, `Charged(resource)` and `Unhandled()` are how tests assert **budget arithmetic** (GR-25: "cold start of 1,000 repositories charges at most N core points"), conditional behaviour (a second sweep with unchanged content charges 0 and shows 304s), single-flight (two features reading one tree produce one ledger entry), and that nothing outside the design is called. The ledger is also served at `/_fake/ledger` for manual inspection.

### 7.11 `cmd/fakegithub`

The compiled skeleton (serve flags only; the other subcommands below are specified, not yet written):

```go
// Command fakegithub serves the fake GitHub from a fixture directory (dev, Playwright, cmd/scp end-to-end).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/github/githubtest"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fakegithub", flag.ContinueOnError)
	fs.SetOutput(stderr)
	world := fs.String("world", "testdata/worlds/demo", "fixture directory (world.json)")
	listen := fs.String("listen", "127.0.0.1:9191", "listen address")
	prefix := fs.String("path-prefix", "/api/v3", "REST path prefix (GHES style); empty = api.github.com style")
	allowRemote := fs.Bool("allow-remote", false, "permit a non-loopback --listen address")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if host, _, err := net.SplitHostPort(*listen); err != nil || (!*allowRemote && !isLoopback(host)) {
		fmt.Fprintln(stderr, "fakegithub: --listen must be a loopback host:port (use --allow-remote to override)")
		return 2
	}
	w, err := githubtest.LoadWorld(os.DirFS(*world))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	s := githubtest.NewServer(w, githubtest.WithPathPrefix(*prefix))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Every goroutine has an owner that waits for it (rule G10): the delivery loop and the shutdown watcher are joined
	// before run returns.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = s.Hooks().Run(ctx) }()
	srv := &http.Server{Addr: *listen, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		defer wg.Done()
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	fmt.Fprintf(stdout, "fakegithub listening on http://%s (path prefix %q)\n", *listen, *prefix)
	code := 0
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(stderr, err)
		code = 1
		stop() // make the watcher and the delivery loop return
	}
	wg.Wait()
	return code
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
```

| Subcommand | Behaviour |
|---|---|
| `fakegithub [serve] --world DIR --listen 127.0.0.1:9191 --path-prefix /api/v3 [--control] [--webhook-target URL --webhook-secret S]` | serve the world; webhook delivery loop on; control API on with `--control` (default on for the dev server, off otherwise) |
| `fakegithub keygen --out DIR` | writes `app.pem` (private, 0600) and `app.pub.pem` for the demo App. Both files absent: generate and exit 0. Both present and the public key matches the private one: do nothing, exit 0 (so `make dev` can call it every time). Anything else (only one file, or a mismatch): exit 1 without writing. The committed demo world carries **no** key files, because a committed public key without its private half is useless and would make `keygen` refuse to run (V-16: the first draft had a committed `app.pub.pem` reference, a keygen that refuses to overwrite, and an acceptance test that `validate` passes on a fresh checkout; the asset directory shipped without the file) |
| `fakegithub validate --world DIR` | strict-decodes the whole fixture, checks references (installations name known apps, repos exist, `@files/` targets exist), exit 1 with all problems listed |
| `fakegithub gen --repos N --seed S --out DIR` | writes a deterministic synthetic world |

It binds loopback by default, refuses a non-loopback `--listen` unless `--allow-remote` is given, prints the URLs to paste into a `github.sources[]` block, and logs every request line. It contains no code path that `cmd/scp` can reach.

### 7.12 Fidelity register (what the fake does not prove)

| # | Aspect | Fake | Real GitHub | Consequence and mitigation |
|---|---|---|---|---|
| F-1 | Secondary limits | scripted or concurrency-triggered | opaque, can change without notice | the adapter's reaction is proven, the thresholds are not; budget model keeps 900/min as design ceiling |
| F-2 | ETag algorithm | credential-bound SHA-256 as observed | undocumented | the design does not depend on it (persisted validators are content SHAs); live `conditional` doctor check (U-1) |
| F-3 | Preferred README choice among several READMEs | `.github/`, root, `docs/`; extensions md, markdown, adoc, rst, txt, none | GitHub's resolution | tests never assert a tie-break between two READMEs |
| F-4 | Error message texts | approximate | exact texts vary | only four phrases are ever matched (secondary rate limit, abuse detection, resource not accessible by integration, the API-version header in a 400) plus the `iat`/`exp` clock hint on a failed mint; recorded real bodies are in `testdata/recorded` and replayed in decode tests |
| F-5 | HTML rendering | tiny deterministic converter | GitHub Markup | tests assert pass-through and sanitising only |
| F-6 | GraphQL | the listed subset | full schema | catches drift in documents the adapter sends; schema check test covers the rest |
| F-7 | Webhook timing | fixed delays, no retries | seconds of latency, no automatic retry, outages | order and loss are injectable |
| F-8 | Tree and blob limits | configurable, same flags | 100,000 entries and 7 MB for trees | `tree_max_entries` lowers it to test truncation |
| F-9 | GHES behaviour differences | path prefix, API version default, optional missing GraphQL | per release | doctor probes the real host; the fake only encodes the two documented differences |
| F-10 | Account/permission model | installation permission map and repository lists | richer (organisation roles, SSO, IP allow lists) | SAML/SSO 403 reproduced as the fault `status:403:...` |

To keep the register honest, `internal/github/conformance_live_test.go` (build tag `live`, never in default CI) runs a small read-only subset (list, commits, readme, tree, blob verification, headers) against a public repository with a token from the environment, weekly, and fails when a recorded invariant (field present, status, header names) changed. A failure there is a signal to update the fake and the recorded fixtures, not to loosen the adapter.

## 8. Concurrency and lifecycle

### 8.1 Goroutine ownership (architecture 10.3)

| Component | Goroutines it starts | Stops when |
|---|---|---|
| `github.Client` and every strategy in `github/auth` | **none** | n/a: all work runs on the caller's goroutine (engine worker, doctor, CLI). Token refresh is lazy (first caller after the margin pays one mint); there is no refresher, no watcher, no ticker |
| `github.RunAppManifest` | one (the loopback HTTP server) | joined before the function returns; the whole flow has a timeout and honours `ctx` |
| `FileKeys`, `Static` | none | file changes are detected by `stat` on use (at most every 30 s for keys) |
| `githubtest.Server` (tests) | the handler goroutines of `httptest`; none of its own | `t.Cleanup` closes the server and, under `synctest`, the bubble must drain before the test returns |
| `githubtest.Hooks.Run` (dev server) | one delivery loop | the context passed to `Run` (cancelled at SIGINT by `cmd/fakegithub`) |
| `gitsource` (post-v1) | none; subprocesses are owned by the call that started them and killed (process group) on timeout or shutdown | |

Consequence: shutdown has nothing to stop in this unit. The engine's `Run` returns after its in-flight jobs drained within `Grace`; every outbound call of those jobs carries the job context, so they end promptly; `Client.Close()` then closes idle connections (called by `lifecycle` after the engine stage, before the store stage, architecture 10.4).

### 8.2 Locking and ordering rules

- No lock is held across a network call, a callback or a channel operation. `rateState` and `auth.App.cache` hold their mutex only for map operations; the mint runs inside the singleflight function, outside `mu`.
- Lock order: there is no nesting. `rateState.mu`, `auth.App.mu`, `FileKeys.mu`, `Static.mu`, the validator memo mutex, the tree memo mutex and the registry mutex are leaves.
- Values shared between callers are immutable (`scm.Tree`, `[]crypto.Signer` snapshots, the registry snapshot, `Capabilities` by value) so readers never lock for long and a race is structurally impossible. Metric callbacks (observable gauges) read the rate book through `snapshots()`, a copy taken under the mutex.
- All tests run with `-race` (CI), including the concurrency tests of 12.2.

### 8.3 Contexts and cancellation

| Rule | Mechanism |
|---|---|
| Every exported operation takes a `ctx` and applies its own deadline (`http.timeout`, `graphql_timeout`, `mint_timeout`) | `context.WithTimeout` per request |
| A caller's cancellation never fails another caller | shared work (token mint, tree memo, installation reconcile) runs through `singleflight.DoChan` on `context.WithoutCancel(firstCallerCtx)` plus its own timeout; every waiter selects on **its own** `ctx.Done()` (architecture 8.7, V15). Cost: a flight whose every waiter left still completes, bounded by its own timeout |
| Engine job timeouts and shutdown reach the HTTP call | the request is built with `NewRequestWithContext(ctx2, ...)` where `ctx2` derives from the caller |
| A cancelled call is not an upstream failure | `KindCanceled`, not counted by the breaker, not logged above DEBUG |
| Retry after 401 happens once and only if the context is alive | step F of the pipeline |

### 8.4 Time

No `Clock` interface (ADR-003): code reads `time.Now()`; tests run under `testing/synctest` where it is virtual; randomness (token refresh jitter, secondary-limit jitter) comes from `Deps.Seed`. The only `time.Sleep` in the unit is inside the fake (latency faults, delivery delay) and in the manifest flow's poll of the browser callback; neither is in the adapter's request path.

### 8.5 Startup and readiness contributions

- `New` (step 4; configuration and key-format errors abort startup, they are operator mistakes) then `CheckCredentials` (step 4; it only reports definite 401s and unknown keys, and `cmd/scp` treats them as fatal on a cold start and as a failing credential gate when a warm snapshot exists).
- Readiness (10.2) is computed by `ops` from the read model, not from this unit; the unit contributes **evidence**: `/status` shows per source the credential `Info()` (kind, identity, key fingerprint, expiry), `Capabilities()`, breaker state, pause-until, rate snapshots, installation states. A source whose credential is rejected shows a critical entry and the engine's pause; the product still serves the warm snapshot (stale-while-error).
- The unit never blocks startup on GitHub reachability.

## 9. Observability

### 9.1 Metrics

Instruments are created in `github.New` from the injected `Deps.Meter` (G11) with names as constants next to the instruments; the list is generated into `docs/reference/metrics.md` by the `cmd/scp` metrics test. Labels are bounded: `source` (configured sources), `installation` (App installations, tens at most), `resource` (a closed set: `core`, `graphql`, `search`, `integration_manifest`, `other`), `status_class` (`2xx`, `3xx`, `4xx`, `5xx`, `error`). **No repository, owner, path or token appears in a label.**

| Metric | Type | Labels | Meaning | Source |
|---|---|---|---|---|
| `scp_github_requests_total` | counter | `source`, `resource`, `status_class` | every outbound call | architecture 18.2 |
| `scp_github_request_seconds` | histogram | `resource` | latency including queueing for the limiter | 18.2 |
| `scp_github_rate_remaining` | observable gauge | `source`, `installation`, `resource` | last observed `x-ratelimit-remaining` | 18.2 |
| `scp_github_rate_reset_seconds` | observable gauge | same | seconds until the bucket resets | 18.2 |
| `scp_github_token_expires_seconds` | observable gauge | `installation` | seconds until the cached installation token expires (absent when none) | 18.2 |
| `scp_github_breaker_state` | observable gauge | `upstream` | 0 closed, 1 half-open, 2 open | 18.2 |
| `scp_github_installations` | observable gauge | `state` | registry size per `InstallationState` | 18.2 |
| `scp_github_conditional_total` | counter | `result` (`200`, `304`) | conditional requests by outcome; the 304 ratio alerts the validator problem (under 70 percent, research 09 9.7) | 18.2 |
| `scp_github_errors_total` | counter | `kind` (closed set of 3.3), `op` | typed failures returned | added |
| `scp_github_skipped_total` | counter | `reason` | repositories dropped by policy or malformed (`disabled`, `archived`, `fork`, `private`, `bad_name`, `no_default_branch`) | added |
| `scp_github_probe_missing_total` | counter | | repositories absent from a `ProbeHeads` result | added |
| `scp_github_graphql_cost_total` | counter | `source` | sum of `rateLimit.cost` (checks the 1 point per 50 repositories estimate) | added |
| `scp_github_token_mint_total` | counter | `result` (`ok`, `suspended`, `credential`, `unavailable`) | installation token mints | added |
| `scp_github_tree_memo_total` | counter | `result` (`hit`, `miss`, `shared`) | `ListTree` sharing between features | added |
| `scp_github_redirect_total` | counter | | followed same-origin redirects | added |
| `scp_github_gate_wait_seconds` | observable gauge | `class` | what `Admit` would answer now | added |

Rows marked "added" are additions within this unit's own namespace and are proposed in section 14 (D-6) so the generated metrics document and `docs/reference` include them. The webhook outcome counter `scp_webhook_total{result}` belongs to `webhook`; this unit exports the `Reason*` constants so that unit can label `invalid_signature`/`malformed`/`ignored` outcomes by reason in logs without a second label dimension on the counter.

### 9.2 Logs (`platform/logx`, `slog`)

Fields: `source`, `installation`, `op`, `resource`, `status`, `request_id` (GitHub's `x-github-request-id`), `kind`, `code`, `duration`. Levels: **INFO** only for state changes (breaker transition, installation state change, capability change such as GraphQL off for 1 h, key reload, credential accepted at startup); **WARN** for a rate-limit or authentication pause (with wait and reset), a mass-removal guard trigger, a rejected redirect or `Link`; **DEBUG** for the request line (`method route-pattern status duration`, repository only at DEBUG); there is **no ERROR from this unit**: failures are returned typed and logged once by the layer that decides (the engine). Never logged: headers, URLs with query strings, bodies, tokens, PEM, webhook secrets, e-mail addresses. `Secret` values format as `[REDACTED]` in every `slog` value, `Error()` and JSON (`secret.Secret` implements `LogValue`, `MarshalJSON`, `MarshalText`, `String`, `GoString`).

### 9.3 Traces

One client span per outbound HTTP call named `github.<op>` (`github.commits`, `github.readme`, `github.tree`, `github.graphql.heads`, `github.mint`, ...: a closed set, never a URL), attributes `http.request.method`, `server.address`, `http.response.status_code`, `github.source`, `github.resource`, `github.request_id`, `github.rate.remaining`, `github.conditional` (true/false), `github.not_modified`; errors set `Error` status with the typed `kind`, never the message. The parent comes from the caller's context (engine job span, which links the webhook delivery span). Created from the injected `Deps.Tracer`; no global accessor.

### 9.4 Status and alerts

`ops /status` receives, through bridges, `[]InstallationInfo` (state, selection, repo count, token expiry, mint failures) and `[]RateSnapshot`. Suggested alerts (documented in `docs/operations`): `scp_github_rate_remaining / limit < 0.1` for 10 minutes; `scp_github_breaker_state == 2`; 304 ratio under 70 percent for an hour; any installation in `suspended`/`permissions_pending`/`unreachable`; `scp_github_token_expires_seconds < 300` while jobs are running (indicates a stuck refresh).

## 10. Security

### 10.1 Trust boundaries of the unit

Untrusted inputs: every byte from GitHub (JSON, README content, HTML, tree and blob bytes, `Link` headers, redirect targets, error messages), every webhook request, repository-controlled strings (names, paths, refs, topics, commit messages), the fixture directory of the fake (developer-controlled), and the output of `token_command`. Trusted: the operator's configuration and secret files, the process's own memory.

### 10.2 Threats and controls

| Threat | Control | Test |
|---|---|---|
| **Credential leakage** into logs, errors, metrics, `/status`, panics, URLs (GR-13) | tokens and secrets are `secret.Secret` end to end (cannot be formatted); `Reveal()` appears only at the seven sites listed in 3.1 point 5 (V-17), asserted by a `grep`-style archtest that lists them as (file, function) pairs and fails on any other; upstream text passes `RedactTokens` (ghs_ of any length, ghp_/gho_/ghu_/ghr_, github_pat_) and is capped at 200 bytes; no credential in a URL or query; `Info()` and `Fingerprint()` expose only non-reversible identifiers | `TestRedactTokensAndExcerpt`, `TestErrorsCarryNoCredential`, and the canary scan of 12.5 (distinctive tokens through a full e2e run, then scan logs, error strings, metrics output and status JSON) |
| **Empty or missing secret treated as configured** (GR-13) | `Secret.IsZero()` is "unset"; `NewStatic` rejects empty; `SecretRef` that resolves to empty is a startup error naming the key path; webhook route not registered without a secret | `TestStaticAndExternal`, `TestConfigRejects` |
| **Webhook forgery and tampering** (GR-20) | SHA-256 only, HMAC over the raw bytes **before** any parse, `hmac.Equal`, every secret evaluated without early exit, header form checked first; the SHA-1 header is never read; size cap twice; unknown source and bad signature are indistinguishable to the caller | `TestVerifyPushAndRotation`, `TestSignVector` (GitHub's published vector), fuzz of `Verify` (12.4) |
| **Timing side channel** | constant-time comparison; no early exit on secret position | covered by construction, reviewed |
| **Event content trusted as data** (GR-21: payload fields used to address content can point a refresh at the wrong repository) | events are **hints**: `scm.Event` carries identity and "something changed" only; reactors enqueue "refresh repository X" jobs that re-read from the API; no payload content is ever stored | e2e test: a signed event naming a repository with falsified commit contents leaves the stored data equal to the API's |
| **Replay** | de-duplication by delivery GUID after a successful enqueue (webhook unit); idempotent level-triggered jobs make a replay harmless; redelivery keeps the GUID | e2e with `DuplicateNext` |
| **SSRF and credential forwarding** | every client comes from the `netguard` factory (connect-time address policy, host pinning); URLs are built from validated config bases plus validated, escaped segments; `Link` and redirects must stay on the API origin or the call fails; `Authorization` is never sent to another origin; the manifest conversion and token-command paths have their own fixed targets; `allow_private_addresses` is an explicit per-source switch | `TestLinkAndCursor` (foreign origin refused), pipeline test with a fake that redirects to a second server, expecting no `Authorization` there |
| **Path/URL injection** (repo or file names with `..`, `?`, `#`, `%`, NUL, newlines) | `validOwnerName`, `escapeRepoPath`, `url.Values`; GraphQL variables only | `TestEscapeRepoPath` (table), fuzz (12.4) |
| **Header injection** | header values are constants or derived from validated config (version string `^\d{4}-\d{2}-\d{2}$`, user agent from build info); no repository-controlled value becomes a header | review, fuzz of `User-Agent` build |
| **Resource exhaustion** | every body is read through a size limit (`file_bytes`, `json_bytes`, `tree_bytes`, mint 1 MiB, command stdout 16 KiB); loops capped by `max_pages`; GraphQL chunk size capped; tree entries capped (`MaxEntries`); push path union capped at 20,000; memo sizes bounded; per-request timeouts; semaphore, limiter and breaker per source | tests with oversize bodies against the fake |
| **Private data published by mistake** (GR-35) | `public_only` default; discovery returns only public repositories under it; `installation_scope` is refused without an access mode or acknowledgement (cross-checked in `cmd/scp`); commit e-mails never leave the decoder; `scm.Commit` has no e-mail field; the fixture world includes a private repository that e2e tests assert is absent from every API response | e2e anonymous enumeration test |
| **Untrusted content executes** | the adapter returns bytes and strings only; it imports neither `html/template` nor a renderer; GitHub-rendered HTML is passed through `docs` sanitising (ADR-005) | archtest import check |
| **Over-privileged App** (research 09 8.1) | the manifest requests read-only permissions from `RequiredPermissions`; doctor fails on any write or admin permission and warns on unused ones; classic PAT with `repo` scope is a warning | doctor tests against the fake |
| **Private key theft/misuse** | key referenced by file/env, never logged, never in config dumps (`SecretRef` prints the path only); mode check by doctor; `crypto.Signer` allows keeping the key in a KMS; JWT lifetime 8 min, signed per use, never cached; rotation with overlap; tokens are never persisted | `TestJWTShapeAndSignature`, `TestFileKeysKeepsPreviousOnBadRotation` |
| **Command execution via `token_command`** | argv with an absolute program path, no shell, own timeout, stdout capped, runner implemented in `cmd/scp` (the only place with `os/exec` besides `gitsource`) | `TestStaticAndExternal`, config rule |
| **Fake GitHub as an attack surface** | separate binary and package; loopback by default; the control API exists only in the dev server and tests, accepts only `Content-Type: application/json` bodies and refuses any request that carries an `Origin` header or whose `Host` is not the listen address (a web page in the developer's browser, or a DNS-rebinding page, cannot mutate the world; V-20); fixture secrets are obviously fake strings that match no real token pattern (CI secret scanning must pass over `testdata`); the demo App's private key is generated by `fakegithub keygen` and never committed | `fakegithub validate` and the CI secret scan |
| **Dependency risk** | no go-github; dependencies of the unit are `gobreaker/v2`, `x/sync`, `x/time/rate`, the OTel API, and (if kept) `backoff/v7`; none has an `init` side effect that matters; versions pinned by `go.sum` and Renovate | CI dependency check |
| **GraphQL query injection** | no repository-controlled string enters the document text | `TestHeadsQueryShape` |

## 11. Requirement and guardrail trace

### 11.1 Features and platform requirements (G-xx)

| Requirement (architecture 21) | Where this unit satisfies it |
|---|---|
| Repository discovery with visibility filter | 4.7 `ListRepositories` (paged, fail-closed, `public_only` default), 4.12 installation registry; catalogue reconcile consumes it |
| Startup prefetch | enabled by `ProbeHeads` and the gate (4.4, 4.8): cold start is paced and budget-aware; this unit adds no startup work |
| README fetch and cache | 4.9 `Readme`/`ReadFile`: array/dir/symlink handled as typed errors, blob-SHA validator, truncation, typed 404 for the feature to store as state |
| Commit ingestion (latest 30) | 4.9 `LatestCommits`: tolerant decode (null author, missing `commit.author`), no e-mail, empty repository is an empty list, caller-chosen ref |
| Package manifest intelligence, fetch side (N-01) | 4.10 `ListTree` (memoised, truncation explicit), `ReadBlob` (SHA-verified, immutable) |
| Licence detection | `RepoStats.License` from the sweep (3.4, 4.8) |
| Release/tag data | `RepoStats.Release` (GraphQL `latestRelease` / REST `releases/latest`), events as accelerators (4.11) |
| Outbound resilience | 4.2: one breaker, limiter, semaphore, timeouts per source; netguard-built client (2.4) |
| GitHub authentication | 4.5 and `github/auth`: App, PAT, OAuth, token command, anonymous (dev) |
| Rate-limit handling | 4.3, 4.4: typed errors, headers observed on every response, growing secondary waits, `Admit` reserves |
| Webhook receiver and signature check | 4.11 `Verifier` (the receiver is `webhook`) |
| Push: commit and documentation refresh | 4.11 push decoding: union of all commits' paths, `PathsComplete`, `HeadTreeOID` |
| `create`/`release` events | 4.11 decoding; no stubs: unsupported events are counted ignored |
| `ping` | 4.11 `EventPing` |
| AsciiDoc via GitHub-rendered bridge | 4.9 `RenderedFile` (unverified for non-README paths, U-6) |
| Visibility policy | 4.7 filter as a safety net; policy ownership stays with `catalog`/`access` (Q-6) |
| Fixtures and recording tool | section 7 and `testdata/worlds`, `scripts/record-github.sh` |
| G-01 Scheduled reconcile of the repository set | 4.7 and 4.12 (`github.installations`, mass-removal guard) |
| G-02 Freshness model on every fetched item | `Result{Validators, FetchedAt, NotModified}` on every read (3.4); the freshness state itself is `freshness`'s |
| G-03 Conditional requests and rate-aware scheduling | 4.4 gate, 4.9 validators (content SHAs, in-token-lifetime ETags), 304 accounting |
| G-06 Webhook de-duplication | `Event.Delivery` is the GUID; redelivery keeps it (4.11); the LRU is `webhook`'s |
| G-13 Recorded-fixture harness, fake GitHub | section 7, `githubtest`, `cmd/fakegithub` |
| Branch configuration: default branch resolved from the repository | `Repository.DefaultBranch` from `default_branch`; the adapter never supplies a ref (empty `ref` = GitHub's default; 4.9); `HEAD` is the ref used for tree reads of the default branch (verified to answer 200 on 2026-10-14) (V-17) |
| Periodic refresh, webhooks only a latency optimisation | `ProbeHeads` (one GraphQL point per 50 repositories) and content-SHA validators make the level-triggered sweep cheap (4.8, 4.9); the cadence is `refresh`'s |
| Issue, last-commit, release and licence badges computed from fetched data | `RepoStats{OpenIssues, Stars, License, Release, HeadSHA, PushedAt}` (3.4); `OpenIssuesIncludePRs` says which meaning the number has; no Shields call exists in this unit |
| GitHub health shown as observed state | `Rates()`, `Installations()`, `Capabilities()`, `TokenSource.Info()`, breaker state and pause-until reach `/status` through the ops bridge; no probe calls GitHub (9.4; GR-41) |
| Secrets handling (token, OAuth client, webhook secret) | `SecretRef` + `secret.Secret`, `IsZero` semantics, `*_FILE`, redaction; 3.1 point 5, 6.1, 10.2 |
| Onboarding without browser automation | no browser tooling in the module graph; the App manifest flow (4.14) is the onboarding path (GR-13) |
| Recorded fixtures produced outside the binary | no flag in `cmd/scp` writes upstream responses to disk; recorded fixtures are written by `scripts/record-github.sh` with scrubbing (5.3) (GR-27) |

### 11.2 Guardrails: the mechanism that enforces each

Enforcement kinds: TYPE (the type system makes the failure unrepresentable), TEST, LINT, AT (architecture test), DESIGN (no code path exists for it in this unit).

| GR | Risk in this unit | Mechanism in this unit |
|---|---|---|
| GR-07 | validator or memo keys that ignore repository identity let forks and mirrors collide, or turn group membership into a key | identity is `RepoRef{Source, ID, Owner, Name}` with the numeric id (3.1 point 4); memos are keyed by (token fingerprint, method, URL, accept) and (source, repo, ref, ...) only; no group in any key; validators are per call and carry no cross-repository key (DESIGN, TEST) (V-17) |
| GR-10 | a configured branch literal used instead of the repository's default branch | the adapter never supplies a branch: empty `ref` means GitHub's default; `Repository.DefaultBranch` is returned for the catalogue (4.7, 4.9); `ListTree` with an empty `ref` sends `HEAD`; `Ref` of an event is passed through for reactors to filter (TEST) |
| GR-12 | two keys for one setting, dotted environment names, documented keys nobody reads | one `webhook.secret` `SecretRef` (+ `previous_secret` for rotation); env mapping `SCP_GITHUB_SOURCES_<i>_...` (6.2); the project test documented keys == consumed keys reads the generated twin of 6.2 (TEST) (V-17) |
| GR-13 | an empty string counted as a configured secret; loggable secrets; credentials in URLs, query strings or logs; browser-automated credential tooling | `Secret.IsZero()`; `NewStatic` refuses empty; `New` refuses a `SecretRef` resolving to empty (6.3); `Secret` formats `[REDACTED]` for `String`, `GoString`, JSON, text, `slog`; `RedactTokens`; credentials only in the `Authorization` header, no query string logged (9.2, 10.2); the fake records any `access_token`/`client_secret` query parameter as a ledger violation; no browser tooling in the module graph, the App manifest flow is a loopback flow with `state` verified in constant time (4.14); canary scan (TYPE, TEST, AT) |
| GR-14 | hard-coded `https://api.github.com` and organisation defaults | every URL is derived per source from `host.*` (4.1); the only literals are the documented github.com defaults of `host.kind: github.com` (DESIGN) (V-17) |
| GR-16 | verifier failures answered with a status that misstates whose fault it is | `Verify` returns typed outcomes only (`InvalidSignature` 401, `Malformed` 400, `Ignored` 202); any other error is a bug answered 500 and logged (4.11, CD-IF-08) (TEST) |
| GR-20 | weak or non-constant-time HMAC, HMAC over decoded text, unchecked prefix; an unauthenticated body parsed or logged; no size limit; missing headers; an empty webhook secret; no replay protection | `Verifier`: SHA-256 only over the raw bytes, `hmac.Equal`, prefix and length check, every secret evaluated without early exit, the SHA-1 header never read; signature before decode, no body ever logged; size cap twice; header checks with typed reasons; `Validate` requires a secret for `github_app`, the verifier skips empty secrets, the route is registered only with a secret; `Event.Delivery` is the GUID and redelivery keeps it; GitHub's published vector test (TEST) |
| GR-21 | payload fields trusted as content or used to address it; head-commit-only paths; absent `commits` crashing the decoder; release tags not recognised; stub handlers; any repository or branch accepted | events are hints: identity from the validated payload, contents always re-read from the API for the repository the job names, path escaping (4.6, 4.11, 10.2); pointer path lists, union over all commits, `PathsComplete`; event names mapped one to one, unsupported events counted `ignored`, a test enumerating `scm.EventKind`; `validOwnerName` and installation routing; behaviour methods let the receiver answer 401/400/202 (503 for a full queue is the receiver's) (TEST, with `catalog`/`webhook`) |
| GR-22 | breakers or limiters built per call that never open; clients without timeouts; one shared pool with head-of-line blocking | one breaker/limiter/semaphore per source built in `New` (4.2.3); `forbidigo` on constructing breakers outside `github`; clients only from the netguard factory with dial/TLS/header timeouts and per-request deadlines (2.4); `noctx`; per-source semaphore here, per-upstream concurrency in `refresh`; breaker-open test with the fake, `TestRequestTimeout` (LINT, TEST) |
| GR-23 | 403/429/404/5xx collapsed into one outcome; `Retry-After` ignored; every failure reported as the same error | the classification table and `PauseError` (4.3); typed `Error`/`PauseError` with behaviour interfaces; headers observed on every response (4.4); a fake fault for every row of the table (TYPE, TEST) |
| GR-24 | immediate retries, silent drops, unguarded periodic jobs | the adapter never retries (except one token refresh) and never drops: it returns typed errors; backoff and parking are the engine's; no timers or schedulers in this unit (DESIGN) |
| GR-25 | fetch storms at startup and on every sweep | gate reserves, GraphQL head sweep, conditional reads, single-flight tree sharing; budget arithmetic asserted against the ledger (TEST) |
| GR-26 | unpaged discovery, an empty list on failure, a one-shot catalogue; data that never expires; items without freshness | pagination by `Link` with a `max_pages` cap (4.6); `ListRepositories` is page-wise and fail-closed (4.7); the reconcile job and the mass-removal guard live in `catalog` (4.7, 4.12); `ProbeHeads` and validators make a level-triggered sweep cheap (4.8, 4.9), the planner is `refresh`'s; `Result` carries `Validators` and `FetchedAt` on every read (TYPE, TEST) |
| GR-27 | development features or response-dumping flags in the production binary | the fake and test seams exist only in `githubtest`/`cmd/fakegithub` (no `scp dev`, no build tag in `cmd/scp`; V-02); no version-string behaviour; recorded fixtures are produced by `scripts/record-github.sh` after `RedactTokens` and a scrub (5.3) (AT, LINT) |
| GR-29 | a null `author`, a missing `commit.author` or a null date breaking the decoder | tolerant decode with explicit fallbacks (3.4); fixture flags `no_author_user`, `no_commit_author` in the demo world (7.3); the display zone is the commits unit's, storage is UTC (GR-38) (TEST) |
| GR-35 | private READMEs or commits published; commit e-mails stored | `public_only` default filter (4.7); no e-mail in any kernel type (3.4); recorded fixtures scrubbed (TYPE, TEST) |
| GR-37 | README names other than `.md`/`.adoc` producing a silent empty result | the adapter returns the actual `Path` and bytes for any README name and a typed `NotFound(Subject=File)` instead of a blank result (4.9); rendering is `docs`' (TEST) (V-17) |
| GR-41 | health probes that call GitHub; "last seen" recorded regardless of the outcome | `Rates()`, `Installations()`, `Capabilities()`, `Info()` read in-memory state only; `Rates()` carries `UpdatedAt` per bucket and the registry `LastReconcile`, all derived from real responses (9.4); `TestStatusAccessorsMakeNoNetworkCall` (TEST) (V-17, V-18) |
| GR-43 | tests that assert nothing, tests that call live hosts, fixtures asserted by count | the fake GitHub with fault injection and a ledger asserting on behaviour; no live call in default CI; recorded fixtures per behaviour with golden decode tests (sections 7 and 12, 12.6) (TEST) (V-17) |

## 12. Test plan

Tests follow ADR-016: `testing/synctest` for time, the fake GitHub for upstream behaviour, `-race` always, fuzz for every parser of untrusted input, golden recorded fixtures for decode fidelity, no live network in default CI.

### 12.1 What exists and passes today (scratch module, 2026-10-14)

Command: `gofmt -l . && go vet ./... && go test -race -count=1 ./...` on Go 1.27.1 (the local toolchain; architecture pins 1.27.2): all packages pass; no data race. Fuzz targets were each run for 8 to 15 seconds (about 1 million executions each) after the fixes below. Reference test code is reproduced in Appendix B.

| Test | Proves |
|---|---|
| `TestJWTShapeAndSignature` | RS256 over `crypto.Signer`; `iat = now-60 s`, `exp = iat + 9 min` (now+8 min), `iss`; signature verifies |
| `TestParsePrivateKey` | PKCS#1, PKCS#8, `\n`-escaped and base64 inputs; 1024-bit key refused |
| `TestAppMintCoalescesAndCaches` | five calls, one mint; a stale `Reject` keeps the fresh token; the current `Reject` forces a re-mint |
| `TestMintTriesNextKeyOn401AndStopsOnSuspended` | rotation: a 401 tries the next key; 403 means suspended and stops |
| `TestLeaderCancellationDoesNotFailOthers` | the first caller's cancellation does not fail the waiting caller; one mint |
| `TestFileKeysKeepsPreviousOnBadRotation` | a bad key file keeps the previous keys in service and the failure is visible |
| `TestStaticAndExternal` | empty token refused; rotated token file picked up by mtime; command output cached until near expiry; `Secret` does not format |
| `TestTokenRefreshOnVirtualTime` | `synctest` bubble with `httptest.NewTestServer`: cached at 49 min, early refresh at 50.5 min, a 20-caller burst coalesces to one mint; two virtual hours run in 0.06 s |
| `TestBreakerOpensOnTransientOnly` | under `synctest`: 56 client-error and success answers and 20 cancellations never open it; 8 consecutive 5xx open it while each response still reaches the caller; an open breaker refuses without calling; half-open after 60 s (virtual), closes after 3 successes; exactly 3 transitions |
| `TestBreakerRatioTrip` | alternating failures (never 8 in a row) open it once the window holds 20 samples at a 50 percent ratio |
| `TestSignVector` | GitHub's published HMAC vector |
| `TestBlobSHA` | equals `git hash-object` |
| `TestVerifyPushAndRotation` | current and previous secret both verify; union of paths over all commits; `HeadTreeOID`; installation routing; bad secret is `InvalidSignature` |
| `TestVerifyPathListsAbsent` | absent lists give `PathsComplete == false` |
| `TestVerifyIgnoredAndMalformed` | unknown installation and unsupported event are `Ignored`; missing signature is `InvalidSignature`; target-ID mismatch |
| `TestClassify` | primary and secondary limits (wait values), permission, blocked, repository 404 is `Gone`, file 404 is not, 503 is not a pause, 410 permanent |
| `TestGate` | reserves by class; empty bucket waits (capped); hard wait; secondary growth 60 s, 120 s, reset on success |
| `TestGateMonotoneProperty` | for any bucket state, a higher class never waits longer than a lower one, never above the cap |
| `TestLinkAndCursor`, `TestNextLinkRejectsLookalikes` | same-origin `Link` only; scheme-relative, userinfo, other port, `/api/v30` and relative targets refused; cursor refuses other hosts and `..` |
| `TestEscapeRepoPath`, `TestHeadsQueryShape` | path hygiene; GraphQL document shape |
| `TestRedactTokensAndExcerpt`, `TestErrorsCarryNoCredential` | all token families including a 520-character `ghs_` token are redacted, messages capped at 200 bytes, control characters removed; a canary token never appears in `Error()`, `%v`, `%+v`, `%#v` |
| `TestConfigValidate`, `TestConfigDefaultsAndValid`, `TestConfigRejects`, `TestConfigDefaultsSection` | defaults applied and idempotent; an explicit `false` survives; each rule reports its path; all violations together; reserve ordering; `ByteSize` and `Percent` decoding |
| `TestObjectIDsMatchGitGoldenVectors` | the fake's blob, nested tree and commit ids equal real git's (golden vectors produced with `git`) |
| `TestDemoFixtureDecodes` | the demo world decodes strictly with the fixture types |
| `FuzzVerify`, `FuzzEscapeRepoPath`, `FuzzDecodeCursor`, `FuzzNextLink` | no panic; success only for correctly signed bodies; validated identity only; escaped paths keep their meaning and round-trip; cursors never leave the base; links stay on origin and base path |
| `TestHeadsQueryMatchesPublicSchema` | every field of the head query exists on its type in GitHub's public schema (skipped unless `SCP_GRAPHQL_SCHEMA` is set; run once on 2026-10-14 against the downloaded schema, and shown to fail when a bogus field is added; re-run by the verifier against a fresh download, same size, passes) |
| `TestMintClassifiesRateLimitAndScope` (verifier, V-03) | mint 403 with `Retry-After` / `x-ratelimit-remaining: 0` / "secondary rate limit" and 429 are `ErrRateLimited` with the advised wait, a plain 403 is `ErrInstallationUnavailable`, 404 too, 422 is `ErrInvalidScope` |
| `TestFingerprintMatchesGitHubFormat` (V-08) | base64 SHA-256 over the PKIX DER key, 44 characters, as `openssl ... \| openssl base64` prints |
| `TestNewRand01IsConcurrencySafe` (V-07) | eight goroutines under `-race` |
| `TestExternalKeepsValidTokenWhenRefreshFails` (V-15) | a failing command run serves the cached token while it is still valid, and fails once it is expired |
| `TestContentsBoundFitsGitHubBase64` (V-05) | a 1 MiB file wrapped at 60 columns exceeds the old bound and fits `contentsJSONLimit`, which does not shrink with a smaller `file_bytes` |
| `TestExcerptNeverSplitsARuneAndRedactsJWT` (V-14) | valid UTF-8 at the 200-byte cut; an App JWT is redacted |
| `TestAuthPauseDoublesAndSuccessResets` (V-13) | 5, 10, 20, 40 minutes, then 1 h; `hardWait` reports it; one success resets |
| `TestObserveFallsBackToCallerResource` (V-13) | a response without `x-ratelimit-resource` lands in the caller's bucket |
| `TestNextLinkCommaInTarget` (V-14) | a comma inside a `Link` target does not tear the value apart |

Defects the tests found while writing this design (all fixed, regression tests kept): an embedded `*Error` in `PauseError` shadowed `Error()` (vet); a leaked `cancel` for non-leader callers of the token mint; the first `ByteSize` decoder matched the suffix `B` before `MiB` depending on map order (`"25MiB"` failed intermittently); `FileKeys.Signers` returned an error after absorbing a bad rotation, which would have stopped minting; `nextLink` accepted a scheme-relative `//host/...` target and a lookalike path `/api/v30` under base `/api/v3`; `config.Validate` iterated a map (non-deterministic message order).

### 12.2 Unit tests still to write (with the implementation)

| Area | Tests |
|---|---|
| Pipeline | order of operations against the fake: pre-flight pause makes **zero** network calls; the breaker wrapper integrated with real HTTP statuses (the unit tests above cover the breaker itself); semaphore never exceeds `concurrency` (race test counting in-flight at the fake); limiter paces to `rps`; one 401 refreshes once and retries once; second 401 pauses 5 min then doubles to 1 h; foreign redirect fails and the second server never sees `Authorization` |
| Classification | every row of 4.3 end to end through the fake with the named faults of 7.7; PAT and App variants; `KindCanceled` on context cancel |
| `ListRepositories` | installation, org, user, explicit; `Link` walk over 3 pages; `max_pages` cap; failure on page 2 returns an error and no partial page; `public_only` hides the private fixture repository; `include_archived`, `include_forks`; `minimal_repos` fallback; 304 on the first page; suspended installation skipped |
| `ProbeHeads` | GraphQL happy path for 50 and 120 repositories (chunking); `NOT_FOUND` alias leaves a hole, others fine; node-limit and 504 halve the chunk; GraphQL 404 switches to REST for 1 h of virtual time; REST fallback produces the same `RepoStats` except `HeadTreeOID` and issue semantics; empty repository |
| Reads | README: JSON, `encoding: none` over 1 MiB (raw second call, `Truncated`), directory array, symlink, 404, SHA mismatch is permanent, `NotModified` by SHA; `ReadFile` path escaping; `LatestCommits` null author, missing `commit.author`, empty repository 409, SHA-media-type 304; `ListTree` truncation flag, `opt.Path` walk, `MaxEntries`, memo hit/miss/shared with two goroutines and one ledger entry, caller cancellation does not fail the other; `ReadBlob` verification and size limit; `ReadRepoImage` limit |
| Webhooks | table of every event name; payload styles `full`, `no_path_lists`, `no_tree_id`; more than 2,048 commits; SHA-1 header ignored; delivery GUID preserved on redelivery; `FailedDeliveries` grouping (a later OK attempt hides an earlier failure), window clamp, `max_per_run` |
| Installations | state machine transitions against the fake (suspend, unsuspend, remove, grant, permissions pending); mass-removal guard (4 of 10 disappear with ratio 0.3 keeps them `unreachable`); `ObserveEvent` then reconcile confirms or reverts; token mint is not attempted for a suspended installation |
| Doctor | each finding ID against a world arranged to trigger it; secrets never printed; `--offline` makes no call (ledger empty) |
| Manifest | loopback only, `state` mismatch rejected, one callback accepted, files `0600`, existing file refused, headless `Code` path |
| Capabilities | per host kind: API version default, GraphQL on/off, `OpenIssuesIncludePRs` on the fallback |
| Verifier additions (V-18) | signature header forms: empty, `sha256=` only, `sha1=<hex>`, wrong length, non-hex, upper-case hex; **ordering**: invalid JSON with a bad signature is `InvalidSignature`, never `Malformed` (GR-20: nothing is parsed before the HMAC); a source whose only secret is empty rejects a body "signed" with the empty key (GR-13, GR-20) |
| Defaults and refs (V-18) | `LatestCommits` and `ReadFile`/`Readme` with an empty `ref` send no `sha`/`ref` parameter (GR-10); `ListTree` with an empty `ref` sends `HEAD`; README names `README`, `readme.RST`, `README.markdown` come back with their actual `Path` (GR-37) |
| Validators (V-06) | a caller that passes no validator never receives `NotModified`, even when the memo holds an ETag for the same URL; a caller whose `v.SHA` differs from the memo's recorded SHA gets a full 200 |
| Mint through the pipeline (V-19) | an outage seen by the mint call opens the breaker; a mint 403 with `Retry-After` produces a `PauseError` and `rates.paused`, not a registry `suspended` |
| Auth pause (V-13) | a second 401 pauses 5 min, the next 10 min, ... 1 h; a PAT (not refreshable) is retried zero times, an App token once; the half-open `ErrTooManyRequests` case returns a 1 s `PauseError`, not a transient failure that burns an attempt |
| ProbeHeads grouping (V-12) | two installations in one source: two chunks, each under its own installation token (ledger credential `inst:<id>`); no chunk mixes owners of different installations |
| Status accessors (V-18, GR-41) | `TestStatusAccessorsMakeNoNetworkCall`: `Rates()`, `Installations()`, `Capabilities()`, `TokenSource().Info()` leave the ledger empty; `Credential` sentinel: `Info()` of an App source touches no network |
| No credential in a URL (V-18, GR-13) | `TestNoCredentialInQuery`: a full scenario against the fake, then `ledger.Violations()` is empty (the fake records any `access_token`/`client_secret` query parameter or `Authorization` on a foreign origin) |
| Timeouts (V-18, GR-22) | `TestRequestTimeout`: a `Latency(2m)` fault under synctest returns `KindTransient` after `http.timeout` and feeds the breaker; the caller's own cancellation returns `KindCanceled` and does not |
| Config | `ApplyDefaults` and `Validate` golden tests; the generated schema and reference; documented-keys equals consumed-keys |

### 12.3 Fake-backed behavioural and budget tests

| Test | Assertion (via the ledger) |
|---|---|
| `TestBudgetColdStartREST` | 1,000-repository generated world, REST path: at most 8,010 core calls and no call outside the catalogue of 7.5 (`Unhandled()` empty); with a PAT limit of 5,000 the backfill class waits at 60 percent consumed (gate) and scheduled work continues |
| `TestBudgetColdStartGraphQL` | head sweep for 1,000 repositories: at most 20 GraphQL points (the 1 per 50 estimate; the test fails if `cost` exceeds twice it, U-5) and zero core calls |
| `TestConditionalSweep` | a second sweep with unchanged content: 0 charged points on authorised 304s, `304` ratio 100 percent; with `etag_auth_bound` and a fresh token the ETag path misses but the **SHA** validators still yield `NotModified` for README and commits |
| `TestSharedTree` | `docs` and `manifests`-style concurrent callers: one ledger entry; the first caller's cancellation does not fail the second |
| `TestPrimaryLimitRecovery` | exhaust the bucket: calls return `PauseError` with `RetryAt == reset`; no network call while paused; after virtual reset the next call succeeds |
| `TestSecondaryLimitGrowth` | repeated secondary limits wait 60 s, 120 s, 240 s ...; one success resets |
| `TestTokenExpiryMidSweep` | `ExpireTokens()` during a sweep: one 401 per in-flight call, one refresh (single flight), sweep completes |
| `TestWebhookRoundTrip` | `Push` on the world then `Flush` to a test receiver using `github.Verifier`: event normalised, then `LatestCommits` returns the new commit; with `DropNext` the planner-style sweep still finds the change (`ProbeHeads` head differs) |
| `TestSuspendedInstallation` | `Suspend`: mint 403, repositories vanish from discovery, others unaffected, no storm of mint attempts |

### 12.4 Fuzz, property and race

Fuzz targets (Go native, seed corpora committed, regression corpus under `testdata/fuzz`, nightly run of five minutes each): `FuzzVerify`, `FuzzEscapeRepoPath`, `FuzzDecodeCursor`, `FuzzNextLink` (all written), plus to write: `FuzzDecodePush` (arbitrary JSON into the decoder), `FuzzClassify` (arbitrary status, headers, bodies: no panic, never a `Gone` without `SubjectRepository`, `PauseError` wait never negative), `FuzzTreeDecode`, `FuzzCommitDecode` (tolerance), `FuzzGraphQLResponse`, `FuzzParseConfigDuration` for `ByteSize`/`Percent`. Properties: gate monotone (written), `Scope.Key` order independence, `BlobSHA` equals the fake's `GitBlobSHA`, `classify` total over all statuses, rate book never reports negative waits, cursor round trip. `go test -race` always; `-count=20` for the concurrency tests on the nightly job; goroutine leak check with `goleak`-style `runtime.NumGoroutine` assertion at test end (the adapter owns none).

### 12.5 Credential-leak canary and archtest

A test boots the fake with distinctive canary credentials (`ghs_` plus 500 characters, a PAT, an App PEM marker, a webhook secret), runs a full scenario (discovery, sweep, reads, webhook, doctor, a failing mint, a 401 pause) with logging at DEBUG captured in memory, and fails if any canary appears in captured logs, any returned error text, the metrics exposition, the `/status` JSON of the `ops` bridge, or `doctor --json`. An archtest lists every call site of `Secret.Reveal` in `internal/github/**` and fails when a new one appears (seven expected, the allow-list of 3.1 point 5). Another asserts that no package of the unit imports `os/exec`, `html/template`, `net/http/httputil` or `go-github`.

### 12.6 Contract and live checks

- **Recorded fixtures**: `internal/github/testdata/recorded/*.json` (real responses: repository list, commits with null author, README JSON, tree, blob, GraphQL head result, `/rate_limit`, errors) replayed through the decoders; refreshed by `scripts/record-github.sh` (a person, read-only token, scrub step).
- **Schema check** (weekly CI, `scripts/fetch-github-schema.sh` then `SCP_GRAPHQL_SCHEMA=... go test -run Schema`).
- **Live smoke** (build tag `live`, weekly, token from a CI secret, read-only, public repository): list, commits, README, tree, blob verification, response headers; asserts recorded invariants; failure means update the fake and recordings (7.12).
- **Version matrix**: the same fake scenarios with `api_version` `2026-03-10` and `2022-11-28` and the three URL styles.

### 12.7 Acceptance criteria for the unit

1. `gofmt`, `go vet`, `golangci-lint` (depguard, forbidigo, noctx, bodyclose, errorlint, gochecknoinits, gochecknoglobals), archtest and `-race` clean.
2. `cmd/scp/assert.go` compiles: `*github.Client` and `*gitsource.Client` satisfy every direct interface of 2.3, `*github.Verifier` satisfies `webhook.Verifier`.
3. Every row of the 4.3 table and every branch of 4.2.1 has a fake-backed test; the ledger shows zero network calls for pre-flight pauses.
4. Budget: the tests of 12.3 pass with the stated bounds; steady-state unchanged sweep charges 0.
5. The canary scan finds nothing; no `Reveal` outside the seven allowed sites of 3.1 point 5.
6. Fuzz targets run 5 minutes each without a finding in the nightly job; seed corpora include every regression found.
7. Virtual-time scenarios (two hours of token refresh, rate-limit reset, redelivery window) run in under one second of wall time each.
8. after `fakegithub keygen`, `fakegithub validate` passes the demo world (before it, `validate` fails with exactly one finding: the missing `app.pub.pem`); `go run ./cmd/fakegithub` serves it; the Playwright suite can run against it (owned by the e2e unit) with webhook delivery on.
9. The documented-keys equals consumed-keys test passes for `github.*`.
10. Coverage of `internal/github` and `internal/github/auth` at least 85 percent of statements excluding `appmanifest.go` browser callback and `gitsource`; the fake is not held to a coverage target but to the e2e usage.

## 13. Implementation tasks

Ordered milestones; each task names its deliverable, acceptance (the tests above) and cross-unit dependencies. "Reference" means the compile-checked code in this document is copied and then extended; it is not a substitute for the tests.

| M | Task | Deliverable | Depends on (units) |
|---|---|---|---|
| **M0** | Kernel types present | `scm`, `job` behaviour interfaces, `platform/secret` as in architecture 8.1/8.3/8.11 (the scratch stubs mirror them) | kernel unit |
| | netguard client factory contract | `netguard.NewClient(...)` accepting `Network` fields (2.4) | platform unit |
| **M1** | `github/auth` | reference code (`source`, `keys`, `jwt`, `app`, `static`) + tests of 12.1; `SecretRef` resolution helper | M0 |
| | Errors and classification | `errors.go`, table tests of 4.3 (pure function level) | M0 |
| | Config | `config.go`, `ApplyDefaults`, `Validate`, schema and key-consumption tests | `platform/config` unit (composition) |
| | Paging, cursor, path hygiene, content verification | `paging.go`, `gitsha.go` with fuzz | M0 |
| | Webhook verifier and decoder | `webhook.go` with the table and fuzz tests | `webhook` unit (consumer-local interfaces for the behaviour methods) |
| **M2** | Fake GitHub core | `githubtest`: `World`, git objects (done), fixture loader, REST routes of 7.5 except GraphQL and hooks, auth simulation, pagination, conditional and rate-limit simulation, faults, ledger; `TestDemoFixtureDecodes`, `fakegithub validate/serve` | M0 |
| **M3** | Transport pipeline | `transport.go`: steps A-F of 4.2.1, breaker, limiter, semaphore, metrics, spans, rate book integration; pipeline tests | M1, M2, `platform/logx`, `platform/telemetry` (Meter/Tracer injection) |
| | REST reads | `repos.go`, `commits.go`, `contents.go`, `trees.go` (memo, single flight), blob and image reads; consumer-interface assertions | M3; consumers' typed-state expectations (`docs`, `commits`, `manifests`, `media`) |
| **M4** | GraphQL head sweep and capabilities | `heads.go` (GraphQL + REST fallback, chunk halving, cool-down), fake GraphQL executor, `capabilities.go`, budget tests | M3; `catalog` (consumer semantics of absent entries) |
| **M5** | Installations and webhooks around the fake | registry, `ReconcileInstallations`, `ObserveEvent`, `Redeliverer`; fake `Hooks`, payload builders, control API, `fakegithub` subcommands; `webhook` round-trip test | M3, M4, `refresh` (job spec), `webhook`, `cmd/scp` (reactor closure) |
| | Doctor and manifest flow | `doctor.go` checks, `appmanifest.go`, CLI wiring in `cmd/scp` | M5, `cmd/scp` |
| **M6** | Hardening and quality gates | nightly fuzz, canary scan, archtests, recorded fixtures and `record-github.sh`, schema job, live smoke, generated 1,000-repository world, benchmarks (`ProbeHeads` decode, tree decode, `Verify`) | all |
| **M7 (post-v1)** | `gitsource` | implementation of 4.15 behind `acquisition.mode: git`, with its hardened-environment tests | M6, `os/exec` allowance already in the matrix |

Critical path for the rest of the product: **M2 (fake core) and M3** unblock `catalog`, `commits`, `docs`, `manifests`, `media` and the `cmd/scp` e2e tests; **M1** unblocks `webhook`; **M4** is needed for the dashboard to be cheap at 1,000 repositories but the REST fallback in M3 keeps everything functional without it.

Cross-unit interfaces to confirm with their owners before M3: the exact `job.Priority` names for the bridge (2.3), the `Spec` of `github.installations` (global scope, upstream `github`, `PrioScheduled`, 30 min, `Outcome.Unchanged` from `ReconcileResult.Changed`), the typed-state handling of `NotFound(Subject=File)` in `docs`, empty-slice semantics of `LatestCommits` in `commits`, "absent from `ProbeHeads` means unknown" in `catalog`, and the behaviour-method mapping in the webhook receiver.

## 14. Deviations proposed

Each item states the change, the evidence, and what must be edited if accepted. Nothing in this design depends on the change being accepted except where marked.

| # | Deviation | Evidence | If accepted, edit |
|---|---|---|---|
| D-1 | **Do not use `golang-jwt/jwt/v5`.** `github/auth` signs the App JWT with a 25-line encoder over `crypto.Signer`. | v5.3.1 signs RS256 only with `*rsa.PrivateKey` (module source read 2026-10-14), which contradicts the `AppKeySource ... []crypto.Signer` design (8.8: "file, env, later KMS"); the claims are three numbers and a string; the encoder is tested against a verifier. | 10-architecture 5.2 row `github/auth` third-party column (none), ADR-004 credentials bullet, appendix A pins |
| D-2 | **Matrix additions** for `golang.org/x/sync/singleflight` (rows `github` and `github/auth`) and `golang.org/x/time/rate` (row `github`); drop `cenkalti/backoff/v7` from the `github` row unless the doctor wait survives review (the engine owns backoff). | 8.7 and 8.8 mandate singleflight in both packages; `refresh` has `x/sync` and `x/time/rate` but the `github` rows do not; the compiled code imports `singleflight` in `auth` | 5.2 rows, `depguard` rules generated from them |
| D-3 | **Bridge row** names `github`: `cmd/scp/internal/bridge` needs `*github.Client` for `RateGate` and the `ops` bridge. The row says "the roots of every feature and service"; `github` is a service root but is not listed. | 5.2 row `cmd/scp/internal/bridge` | clarify the row |
| D-4 | **Config shape**: `github.defaults.budget.reserve` is three thresholds (`backfill`, `scheduled`, `webhook`) instead of the single `reserve: 10%` of the outline; `limits`, `http` blocks and per-source `discovery`, `acquisition`, `network` members as in section 6; `include_archived` is a tri-state internally (`*bool`) so its default `true` is distinguishable from an explicit `false`. | ADR-015 states the 40/15/2 reserves; the outline's single value cannot express them | 10-architecture 16 outline (github block) |
| D-5 | **Public surface additions** to `*github.Client` beyond 8.7's list: `Admit`, `Rates`, `Capabilities`/`SourceCapabilities`, `TokenSource`, `Verifier`, `Installations`, `ObserveEvent`, `ReconcileInstallations`, `CheckCredentials`, `ReloadKeys`, `Doctor`, `Close`; `RunAppManifest` as a package function. All use stdlib, kernel or package-owned types, and are consumed only by `cmd/scp` and its bridges. | section 2.3, 4.12 | 8.7 "additionally exposes" sentence |
| D-6 | **Additional metrics** within `scp_github_*`: `errors_total`, `skipped_total`, `probe_missing_total`, `graphql_cost_total`, `token_mint_total`, `tree_memo_total`, `redirect_total`, `gate_wait_seconds`. | needed to verify the GraphQL cost estimate (U-5), the memo sharing and the gate in production | 18.2 list (generated document picks them up) |
| D-7 | **Fixtures are JSON, not YAML**, for the fake: the rank-1 `githubtest` row allows no third-party module and `scm`-only imports, and a strict JSON loader needs none. | 5.2 row `github/githubtest` | none (the architecture does not fix the format; tree lists `fixtures.go`) |
| D-8 | **File layout** differs from the tree in section 7 (`static.go` holds PAT/OAuth/external/anonymous, `keys.go` and `jwt.go` are separate, `paging.go`, `gitsha.go`, `config.go` are added; `githubtest` adds `gitobjects.go`). | section 1.4 | refresh the tree listing when the unit lands |
| D-9 | **The `webhook` receiver classifies verifier errors by behaviour methods** (`InvalidSignature`, `Malformed`, `Ignored`) declared in the `webhook` package as local interfaces. 8.7 shows `Verifier.Verify` returning `error` without saying how the receiver maps it; this is the only mapping that keeps `webhook` from importing `github`. | 4.11; GR-21 | document the three methods in the `webhook` unit |
| D-10 | **Gate granularity**: `Admit` answers the worst case over all sources and installations because `refresh` pauses per upstream name `github`. Multi-source deployments therefore pause together (see Q-1). | 8.6 `Gate.Admit(upstream string, ...)` | decision pending Q-1 |
| D-11 | **`cmd/fakegithub` needs a lint and archtest allowance** (V-11). Architecture 5.1 rule 6 and G4 reserve `os.Exit`, `signal.NotifyContext` and `http.Server{}` literals for `cmd/scp` (and `platform/httpx`), but `cmd/fakegithub` is a second `main` whose row (rank 6) allows only `githubtest` and `scm`, so it cannot use `httpx` or `lifecycle`. The skeleton in 7.11 is as small as it can be (flag parsing, `signal.NotifyContext`, one `http.Server` with `ReadHeaderTimeout`, both goroutines joined). | 5.1 rule 6, G4, 5.2 row `cmd/fakegithub` | exempt `cmd/fakegithub` from the `forbidigo` rules for `os.Exit`, `signal.NotifyContext`, `http.Server`, `ListenAndServe` and from the `os.Args` rule; one line in 5.1 rule 6 |
| D-12 | **A plain `*github.Error` has no `RetryAt`; only `*github.PauseError` does.** Architecture 8.7 and 15 describe one `github.Error{Kind, Status, RetryAt, ResetAt, RequestID, Err}` that satisfies `job.RateLimited`; the engine classifies with `errors.As(err, &job.RateLimited)`, so an `Error` with a `RetryAt` method would be treated as a pause for every failure, including a 404 (a defect found while compiling the first draft, 12.1). `Error` keeps `ResetAt` and `RetryAfter` as data fields. | 3.3, TestClassify | 8.7 sentence and 15 point 1 |
| D-13 | **No `scp dev` subcommand** (V-02) and no build tag in `cmd/scp`: the first draft of this unit proposed one; it contradicted 5.1 rule 1 and GR-27, so it was removed instead of being proposed as a deviation. Recorded here so the composition unit does not look for it. | 5.1 rule 1, 12.4 `make dev` | none |

## 15. Open questions and unverified facts

### 15.1 Unverified (not confirmed from a primary source in this unit)

| # | Fact | Why it matters | How it is contained | How to settle |
|---|---|---|---|---|
| U-1 | Whether REST validators survive a token change, and whether a 304 for an installation token is free | decides the cost of restarts and token rotation | the design persists **content SHAs**, uses ETags only inside one token fingerprint | doctor `conditional` check across two fresh tokens; recorded in `/status` |
| U-2 | `commits[].added/removed/modified` present on every push payload (research V13) | decides whether docs/manifests can skip fetches by path | pointer lists give `PathsComplete=false`, which makes reactors fall back to a tree comparison | capture a real payload; `payload_style: no_path_lists` is tested |
| U-3 | Redelivery retention (3 days in research 09). **Settled by the verifier on 2026-10-14:** `GET /app/hook/deliveries` takes `per_page` (max 100), `cursor` and `status` (`success`/`failure`) and returns the cursors in the `Link` header; the redeliver endpoint answers 202, 400 or 422 (docs.github.com/en/rest/apps/webhooks). Still unverified: the retention window and what 400/422 mean | recovery after downtime | window clamp is a constant; 400/422 are errors, not successes (V-10) | live smoke with a real App |
| U-4 | Whether a 304 on `Accept: application/vnd.github.sha` is free of the primary limit | cost of the cheap change check | `ProbeHeads` supplies head SHAs, the commits job runs only on change | ledger on a real token |
| U-5 | Real GraphQL `cost` of the 50-alias head query (formula gives about 1) | budget model | `scp_github_graphql_cost_total`; test fails when over twice the estimate; chunk size is configurable | first live run |
| U-6 | `Accept: application/vnd.github.html+json` on `contents/{path}` for non-README files | AsciiDoc bridge for arbitrary pages | feature degrades to text on 415/422 | live smoke |
| U-7 | HTTP status of an exhausted GraphQL primary bucket (200 with `RATE_LIMITED` vs 403) | classification | both handled; the fake produces both | live observation |
| U-8 | Git minimum version and hardening flags for `gitsource` | post-v1 | specified as "re-verify at the milestone" | release notes at M7 |
| U-9 | GraphQL field availability on old GHES releases (`latestRelease`, `licenseInfo`, `stargazerCount`) | GHES sources | a failing sweep switches to REST for 1 h | doctor `host.version`; GHES schema per release |
| U-10 | Header `X-GitHub-Hook-Installation-Target-ID` equals the App id for App webhooks | target check | the check only runs when the header is present | capture a real delivery |
| U-11 | GitHub wraps the base64 `content` of the contents API at 60 columns (observed on 2026-10-14, 34 lines for a 1,454-byte README, every line 60 characters; not in the documentation) | response bound of README and file reads | `contentsJSONLimit` allows a factor 1.5 and `base64.StdEncoding` ignores line feeds, so any wrapping up to about 12 percent is safe | `TestContentsBoundFitsGitHubBase64`; recorded fixture of a large README |

### 15.2 Open design questions (owner or neighbouring unit decides)

| # | Question | Default taken here |
|---|---|---|
| Q-1 | Should `refresh` register one upstream per GitHub **source** (`github:acme`, `github:internal`) so one exhausted source does not pause the others? | single upstream `github`; `Admit` returns the worst case (D-10). Needs a change in 8.6 `UpstreamConfig` registration and in `Spec.Upstream` of the github jobs |
| Q-2 | Confirm that `cmd/scp` builds the `job.Spec` of `github.installations` and `webhook.redelivery` (closures over `*github.Client`), because `github` cannot import `job` | yes (2.5) |
| Q-3 | Does `catalog` also enforce `public_only`, or only this adapter? | both: the adapter never returns a non-public repository under `public_only` (safety net), `catalog` applies its policy and `access` the audience |
| Q-4 | A suspended or unreachable installation removes its repositories from discovery; is the `catalog` mass-removal guard (30 percent) the right place to refuse that, or should `ListRepositories` return an error when any allowed installation is not `active`? | the adapter skips and counts; `catalog` judges |
| Q-5 | Where does the demo and e2e world live (`testdata/worlds/demo`) and which unit owns its content and the Playwright expectations? | `testdata/worlds/demo`, owned by the e2e/composition unit; this unit owns the format and the loader |
| Q-6 | Should `scm.Repository` carry `Description`-less minimal objects' `Topics` at all, given that topics may need `GET /repos` for GHES versions that omit them in lists? | taken from the list when present; no extra call |
| Q-7 | `ReadBlob` accepts 40- and 64-hex ids; GitHub does not host SHA-256 repositories today. Keep 64 for forward compatibility? | accepted by length check only |
| Q-8 | The Go module path used in the scratch module (`github.com/cantara/source-code-portal`) is a placeholder; the composition unit fixes it. | placeholder |
| Q-9 | Should the fake serve the unauthenticated `/meta` and `/` root documents for tools that probe them? | not needed by the adapter; omitted |
| Q-10 | (V-21) One exhausted installation bucket, or one `PauseError` from it, pauses the whole upstream `github` for every other installation of the same App, because `refresh.Gate.Admit(upstream, priority)` and the engine's pause carry no installation key. Is that acceptable for Apps with many organisations? | accepted for v1: each installation has at least 5,000 requests/hour and the worst case is a pause until that bucket resets (at most an hour); a fix needs `Admit(upstream, key, priority)` and per-key pauses in `refresh` (architecture 8.6), or one engine upstream per source and, inside a source, a per-installation `Gate` answering only for jobs of that installation. Extends Q-1 |

## Appendix A. Compile-check and test record

Scratch module `github.com/cantara/source-code-portal`, 2026-10-14, Go 1.27.1:

| Check | Result |
|---|---|
| `gofmt -l .` | no output |
| `go vet ./...` | clean |
| `go test -race -count=1 ./...` | `internal/github`, `internal/github/auth`, `internal/github/githubtest` pass; `TestHeadsQueryMatchesPublicSchema` skips unless `SCP_GRAPHQL_SCHEMA` is set (it passed on 2026-10-14 with the downloaded schema) |
| Fuzz | `FuzzVerify`, `FuzzEscapeRepoPath`, `FuzzDecodeCursor`, `FuzzNextLink`: 8-15 s each, about 1 million executions each, no findings after the fixes of 12.1 |
| Coverage (statements, `go test -cover`) | after the verification fixes: `internal/github` 78.1 percent, `internal/github/auth` 75.2 percent, `internal/github/githubtest` 44.2 percent (stubs); the request pipeline is not implemented yet, so the figures describe the implemented files only |
| Interface assertions | `internal/archassert` compiles: `*github.Client`, `*github.Verifier`, `*gitsource.Client` satisfy every consumer interface of 8.7 |

Files by package (lines in the scratch module, tests included): `internal/github/auth` 5 source files and 3 test files; `internal/github` 12 source files and 5 test files; `internal/github/githubtest` 3 source files and 2 test files; `internal/gitsource`; `cmd/fakegithub`; `internal/archassert`. The three kernel stubs (`scm`, `job`, `platform/secret`) mirror 8.1, 8.3 and 8.11 and are not part of this unit.

Reproduce: create a module, copy the listings of this document into the paths named in section 1.4 together with the stubs of the kernel unit, then run the commands above.

## Appendix B. Reference tests

The compile-checked, passing test code (about 1,200 lines: auth, synctest, classification, gate, webhook, config, fuzz targets, schema check, git golden vectors, fixture decode) is the companion file `20-design/05-github-adapter.reference-tests.md`. It is part of this design: an implementer copies it next to the code of this document and extends it with the tests listed in 12.2 to 12.4. Section 12.1 explains what each test proves.

## Verification log (independent verifier, 2026-10-14)

Scope: the document and its companion `05-github-adapter.reference-tests.md`, against `10-architecture.md` (sections 5, 8, 10, 15, 16, 17), `04-decisions.md` (D-17, A-04, A-20), ADR-004, ADR-015, ADR-023, ADR-024, `02-design-guardrails.md` and the live web. The verifier worked on a copy of the author's scratch module (`scratchpad/verify-05-github-adapter/mod`, Go 1.27.1); every changed listing in this document and in the companion file was regenerated from that copy after `gofmt`, `go vet` and `go test -race` passed. The full finding report, with evidence, is `30-verification/design-05-github-adapter.md`.

**Web facts checked on 2026-10-14** (fetched today): REST API versions and default (`2022-11-28` default, `2026-03-10` current); rate-limit page (installation scaling, 12,500 cap, GHEC 15,000, secondary-limit procedure: `retry-after`, then reset, else at least a minute); app webhooks REST page (`cursor`, `per_page`, `status`; redeliver 202/400/422); webhook events page (installation events cannot be subscribed to, 2,048 commit cap, 25 MB cap); manifest flow page (`state`, `redirect_url`, conversion within one hour); private-key page (fingerprint format); contents API page (1 MB, 1-100 MB raw only). Direct calls to `api.github.com`: README base64 wrapped at 60 columns, `git/trees/HEAD` answers 200, the `application/vnd.github.sha` media type returns a quoted-SHA `ETag` and a conditional request with it answers 304, an anonymous 304 is charged to the primary bucket (consistent with the design). Go module proxy: `gobreaker/v2` v2.4.0, `backoff/v7` v7.0.1, `x/sync` v0.24.0, `x/time` v0.16.0, otel `metric` v1.47.0, `golang-jwt/jwt/v5` v5.3.1 all exist; `jwt/v5` v5.3.1 `SigningMethodRSA.Sign` requires `*rsa.PrivateKey` (module source read), so deviation D-1 is confirmed. The head query was re-validated against a fresh download of GitHub's public GraphQL schema (identical size, test passes).

**Findings fixed in this document** (V-nn markers appear where the text changed): V-01 tests of `github` imported `job`, outside its matrix row; V-02 `scp dev`/`devtools` contradicted rule 5.1.1 and GR-27; V-03 a rate-limited mint was classified as a suspended installation (new `ErrRateLimited`, `ErrInvalidScope`); V-04 the 404-on-mint rows contradicted each other; V-05 the contents response bound rejected files above about 97 percent of the limit because GitHub wraps base64 at 60 columns; V-06 a memoised ETag could produce `NotModified` for a caller that holds no value; V-07 `Rand01` raced; V-08 key fingerprint format did not match GitHub's; V-09 manifest `default_events` listed non-subscribable events; V-10 redeliver 422 treated as success without evidence, U-3 partly settled; V-11 the `cmd/fakegithub` skeleton broke G4, G10 and 5.1.6 (code fixed, D-11 proposed); V-12 `ProbeHeads` chunks must be grouped per installation; V-13 pipeline order contradiction, missing auth-pause state, 401 retry for non-refreshable credentials, half-open refusal, resource fallback; V-14 `excerpt` cut inside a rune, JWT not redacted, comma inside a `Link` target; V-15 a failing token command discarded a still-valid token; V-16 the demo world referenced an uncommitted key file and `keygen` refused to overwrite; V-17 traceability gaps, the false "three `Reveal` sites" statement (the code has seven), the `fmt` placeholders, the description of the webhook status mapping; V-18 missing tests for request timeouts, signature handling, verification ordering, default refs, status accessors, README names and credentials in URLs; V-19 mint bypassed the breaker and rate book; V-20 the fake's control API was reachable from a browser; V-22 `Ledger.Unhandled` and `Violations` were used but not declared.

**Left open:** V-11 (the lint exemption D-11 needs the architecture owner), V-21 (one installation's limit pauses the whole `github` upstream, Q-10, architecture-level), V-23 (the webhook receiver maps `body_too_large` to 400 where 413 is the natural answer; a cross-unit detail for the `webhook` unit, no safety consequence).

**Executable checks run** (all in the verifier's copy): `gofmt -l` empty, `go vet ./...` clean, `go test -race -count=1 ./...` pass; `FuzzVerify`, `FuzzEscapeRepoPath`, `FuzzDecodeCursor`, `FuzzNextLink` 8 to 10 seconds each after the changes, no findings; import graph of every package listed with `go list` and compared with the matrix (production imports conform; the `job` test import was the only violation); `Reveal` call sites counted (six in the scratch module, seven in the finished design).


## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Changed: 2.3 `manifests.BlobBatchReader.ReadBlobs` interface, stub and spec paragraph (CD-IF-01). Open: the fake GitHub documents `object(expression:)` aliases; whether `object(oid:)` batches work must be verified with spike S-B.
- Changed: `SecretRef = secret.Secret` without an `env` form, `LookupEnv` removed (CD-CF-06); `DiscoveryConfig` gains `removal_confirmations` and `mass_removal_hold` (CD-CF-07); redelivery and webhook admission ranges per unit 04 (CD-CF-03, CD-CF-04).
- Changed: unclassified verify error answers 500, `Truncated()` on `FailedDeliveries` (CD-IF-08).
- Note: architecture 8.7 now says a plain `*github.Error` has no `RetryAt` (CD-IF-12).
- No code exists for the adapter: the review checked it at signature level against the consumers (see the verification file).
- Fixtures (CD-FX): the fake GitHub and the demo world of 7.5 must grow to cover every upstream call the features make: the fake has no `dependency-graph/sbom` endpoints and no GraphQL `dependencyGraphManifests` (providers are off by default in v1); the batched blob read needs an `object(oid:)` alias test; the demo world has no `docs/` pages, `scp.yaml`, `catalog-info`, `CODEOWNERS`, repository images, non-Maven manifests or lockfiles, truncated monorepo tree, renamed repository, multi-group membership or private repository under an installation scope. The full matrix is in the verification file.
