# CLAUDE.md

Guidance for Claude Code in this repository.

## State

This repository (`git@github.com:Cantara/source-code-portal.git`, branch `main`, Go module `github.com/cantara/source-code-portal`) is the
home of the Go rewrite of Source Code Portal (SCP). **M0 (skeleton), M1 (catalogue and sync), M2 (read API for the pages) and M3 (docs,
badges, webhooks, freshness) are done; next is M4.** Coding instructions for the repository itself are in the root `AGENTS.md`.

- **`.claude/work/plan.md` is the binding plan**: scope (v1, v1.1, later), architecture, stack, milestones M0..M6. Read it first.
- `.claude/work/README.md`: index of the development package.
- `.claude/work/design/`: short v1 design notes per area (GitHub, sync/snapshot/store, catalog, content, HTTP/API, web, build/ops,
  principles). Binding below `plan.md`.
- `.claude/work/decisions.md`: every decision taken, with where it is recorded. The M1, M2 and M3 entries list where the code is
  leaner than the notes and what was deferred.
- `.claude/work/reference/detailed-design/`: the earlier detailed design package (about 1,900 files). **Reference only**: consult it
  when building a part, copy what helps. It is not kept in sync; `plan.md` and `design/` win.

The Java product (`Cantara/SourceCodePortal`, checked out at `/Users/overanheim/Code/descoped/SourceCodePortal`) is read-only reference;
never modify it, push to it or open issues or pull requests on it.

## Working rules

- **Greenfield**: nothing is released and no data needs keeping. Change code, contract, config keys, the store schema
  (`internal/store/migrations/0001_init.sql`) and documents in place; no compatibility layers, shims, deprecations or extra migrations.
  Commit directly to `main` (small Conventional Commits, CI green) and push.
- **The agent drives the work.** It takes the milestones in order and starts the next one when the last is green, without waiting
  for a go. The specs (`plan.md`, `design/`) cover what the owner wants; the owner accepts the agent's choices. Decide, record in
  `decisions.md`, keep going; never ask the owner, and leave no open questions in documents or replies.
- Keep it small: no package, config key, metric or dependency before the code that needs it. Avoid over-engineering.
- Dependencies must be current (release within 18 months), widely used and pure Go; `bluemonday` is the one exception (`plan.md` section 4).
- Planning and design live under `.claude/work/`; `docs/` is user documentation only. Documents describe what is built, not what is not.
- Commits: Conventional Commits, no AI attribution lines. Generated files are never edited by hand (`make gen`).
- No test touches the network: loopback only, `internal/github/githubtest` and recorded fixtures.
- Keep this file current: update it in the same commit when the state, layout or rules change.

## Architecture in brief

One static Go binary (`CGO_ENABLED=0`) with the React SPA embedded. GitHub feeds sync jobs; jobs write SQLite (a rebuildable cache) and
publish an immutable snapshot behind an atomic pointer; request handlers read only the snapshot. Features never import features; wiring
only in `cmd/scp`. Untrusted bytes pass through bounded parsers and one sanitiser producing `SafeHTML`. Public port `:9090`, admin `:9091`.

## Code map (as built)

| Path | What it is |
|---|---|
| `cmd/scp` | Cobra CLI (`serve`, `config check`, `version`); `serve.go` wires everything; `boundary_test.go` enforces feature isolation; `run_test.go` holds the end-to-end test against the fake GitHub (warm start, `pushed_at` push, outage served stale, signed webhook push) |
| `internal/config` | Viper loading with `UnmarshalExact`; all validation; `Secret` (redacted). Required: `github.sources`. Keys: `server.*` (addresses, grace, `public_url`, `trusted_proxies`), `ops.pprof`, `log.*`, `store.path`, `sync.concurrency`, `github.sources[]`, `catalog.reconcile_interval`, `catalog.groups[]`, `webhook.secret_file`, `webhook.previous_secret_file` |
| `internal/github` | go-github v92 client per source (App via ghinstallation, or token): listings, commits, trees (optionally recursive), blobs, latest release, `WebURL`; `transport.go`: ETag replay of 304 as 200, rate book, in-flight limit; errors classified (`Error.Kind`; 409 is `NotFound` with code `empty`) |
| `internal/github/githubtest` | Fake GitHub: App JWT check, installations, tokens, org listings with pagination, commits, trees, blobs and releases (`SetCommits`, `SetFile`, `SetRelease`), ETags/304, rate limits, fault injection (`Fail`, `Heal`), ledger |
| `internal/snapshot` | Shared value types (`Repo`, `Group`, `Freshness`, `Commit`, `Descriptor`, `Doc`, `Release`), the immutable `Snapshot` with per-repository entries and merged commit feeds (`Feed`), `Builder` (`ReplaceSource`, `UpdateRepo`) and debounced `Publisher` |
| `internal/store` | SQLite (modernc) last-good cache: `repo` and `item` tables, quarantine on doubt, `Apply` in one transaction |
| `internal/sync` | Job engine: one entry per key with a due time, priorities, concurrency, pace, budget reserves, backoff, source pauses, `Notify` (per repository) and `NotifySource` (per-source specs), warm start (a stored due time is capped at now + TTL) |
| `internal/safehtml` | `SafeHTML` and the one sanitiser policy (bluemonday, `PolicyVersion`); `Sanitize` and `Text` are the only constructors |
| `internal/untrusted` | Cleaning of repository-authored text: control, bidi and invisible characters stripped, whitespace collapsed, bounded length |
| `internal/catalog` | Feature: per-source listing job (`catalog.reconcile`, refreshes App installations first, notifies `EventPush` when `pushed_at` changed), `Plan` (admission, aliases, two-miss removal), group matchers, the `scp.yaml` descriptor job (`catalog.descriptor`, bounded field-by-field parser), the dashboard, groups and repos API |
| `internal/docs` | Feature: README (`.github/`, root, `docs/`) and Markdown under `docs/` per repository (`docs` job; blobs fetched only when their SHA changed); the pipeline in `render.go` (decode, front matter, truncation, shape guard, goldmark, alerts, chroma classes, link and image rewrite on the HTML, sanitise; `pipelineVersion` pinned by `TestPipelineLock`); stored HTML is sanitised again at publish; the docs API |
| `internal/badges` | Feature: flat SVG badges (`/badges/repos/{owner}/{name}/{last-commit,release,license,issues}.svg`, `/badges/groups/{group}/repos.svg`) marshalled with `encoding/xml`, DejaVu width table, strong ETag; the latest-release job (`badges.release`, `EventTag`) |
| `internal/commits` | Feature: latest 30 default-branch commits per repository (`commits` job, cleaned and bounded), the paged `/commits` API with filter-bound keyset cursors |
| `internal/webhook` | Feature: `POST /webhooks/github` (registered when `webhook.secret_file` is set): media type, signature format, size, HMAC-SHA256 with the current then previous secret, delivery id dedupe; `Normalise` maps push, release, repository and installation events to a `sync.Event`; the `Sink` in `cmd/scp` routes to `Notify` or `NotifySource` |
| `internal/httpapi` | Public and admin handlers (features add non-contract handlers as `Route`s with their body cap), `Server` (fixed timeouts); `middleware.go`: request id, recover, access log, security headers and CSP, 30 s deadline, gzip, trusted-proxy client IP; `spa.go`: files, shell shapes and the fallback; problems (instance = request id), 406 and 405 as problems, weak ETag/304, `/livez`, `/readyz`, `getSnapshot`, `/api/v1/openapi.yaml`; `oapi/` is generated from `api/openapi.yaml` (plus hand-written `convert.go`) |
| `api/openapi.yaml` | The `/api/v1` contract (OpenAPI 3.1, embedded by `api/api.go`): dashboard, commits, groups, repos (detail with descriptor and commits), docs, snapshot; `make gen` regenerates `internal/httpapi/oapi/oapi.gen.go` |
| `web/` | React SPA shell (M0); the real UI comes in M5 from the Claude Design work |
| `deploy/` | Dockerfile, `config.example.yaml` |

Test idioms: engine tests run in `testing/synctest` bubbles with a real store in a temp dir; every package with goroutines uses `goleak`.

## Next: M4

`/llms.txt`, raw Markdown, metrics, readiness, hardening (CSP, limits) (`plan.md` section 5): security and load checks pass for 1,000
repositories. Read `design/http-api.md` (middleware chain, CSP, llms, metrics), `design/build-ops.md` and the M3 entry in `decisions.md`.
The raw `.md` document path was left for M4; request id, access log and metrics middleware were deferred here from M1.
