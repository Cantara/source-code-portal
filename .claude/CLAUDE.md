# CLAUDE.md

Guidance for Claude Code in this repository.

## State

This repository (`git@github.com:Cantara/source-code-portal.git`, branch `main`, Go module `github.com/cantara/source-code-portal`) is the
home of the Go rewrite of Source Code Portal (SCP). **M0 (skeleton), M1 (catalogue and sync) and M2 (read API for the pages) are done;
next is M3.** Coding
instructions for the repository itself are in the root `AGENTS.md`.

- **`.claude/work/plan.md` is the binding plan**: scope (v1, v1.1, later), architecture, stack, milestones M0..M6. Read it first.
- `.claude/work/README.md`: index of the development package.
- `.claude/work/design/`: short v1 design notes per area (GitHub, sync/snapshot/store, catalog, content, HTTP/API, web, build/ops,
  principles). Binding below `plan.md`.
- `.claude/work/decisions.md`: every decision taken, with where it is recorded. The M1 and M2 entries list where the code is leaner
  than the notes and what was deferred.
- `.claude/work/reference/detailed-design/`: the earlier detailed design package (about 1,900 files). **Reference only**: consult it
  when building a part, copy what helps. It is not kept in sync; `plan.md` and `design/` win.

The Java product (`Cantara/SourceCodePortal`, checked out at `/Users/overanheim/Code/descoped/SourceCodePortal`) is read-only reference;
never modify it, push to it or open issues or pull requests on it.

## Working rules

- **Greenfield**: nothing is released and no data needs keeping. Change code, contract, config keys, the store schema
  (`internal/store/migrations/0001_init.sql`) and documents in place; no compatibility layers, shims, deprecations or extra migrations.
  Commit directly to `main` (small Conventional Commits, CI green) and push.
- Decide and record (`decisions.md`); do not leave open questions in documents and do not ask the owner.
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
| `cmd/scp` | Cobra CLI (`serve`, `config check`, `version`); `serve.go` wires everything; `boundary_test.go` enforces feature isolation; `run_test.go` holds the end-to-end test against the fake GitHub |
| `internal/config` | Viper loading with `UnmarshalExact`; all validation; `Secret` (redacted). Required: `github.sources`. Keys: `server.*`, `log.*`, `store.path`, `sync.concurrency`, `github.sources[]`, `catalog.reconcile_interval`, `catalog.groups[]` |
| `internal/github` | go-github v92 client per source (App via ghinstallation, or token): listings, commits, trees, blobs; `transport.go`: ETag replay of 304 as 200, rate book, in-flight limit; errors classified (`Error.Kind`; 409 is `NotFound` with code `empty`) |
| `internal/github/githubtest` | Fake GitHub: App JWT check, installations, tokens, org listings with pagination, commits, trees and blobs (`SetCommits`, `SetFile`), ETags/304, rate limits, fault injection, ledger |
| `internal/snapshot` | Shared value types (`Repo`, `Group`, `Freshness`, `Commit`, `Descriptor`), the immutable `Snapshot` with per-repository entries and merged commit feeds (`Feed`), `Builder` (`ReplaceSource`, `UpdateRepo`) and debounced `Publisher` |
| `internal/store` | SQLite (modernc) last-good cache: `repo` and `item` tables, quarantine on doubt, `Apply` in one transaction |
| `internal/sync` | Job engine: one entry per key with a due time, priorities, concurrency, pace, budget reserves, backoff, source pauses, `Notify`, warm start (a stored due time is capped at now + TTL) |
| `internal/untrusted` | Cleaning of repository-authored text: control, bidi and invisible characters stripped, whitespace collapsed, bounded length |
| `internal/catalog` | Feature: per-source listing job (`catalog.reconcile`, notifies `EventPush` when `pushed_at` changed), `Plan` (admission, aliases, two-miss removal), group matchers, the `scp.yaml` descriptor job (`catalog.descriptor`, bounded field-by-field parser), the dashboard, groups and repos API |
| `internal/commits` | Feature: latest 30 default-branch commits per repository (`commits` job, cleaned and bounded), the paged `/commits` API with filter-bound keyset cursors |
| `internal/httpapi` | Public and admin handlers, SPA serving, problems, weak ETag/304, `/readyz`, `getSnapshot`; `oapi/` is generated from `api/openapi.yaml` (plus hand-written `convert.go`) |
| `api/openapi.yaml` | The `/api/v1` contract (OpenAPI 3.1): dashboard, commits, groups, repos (detail with descriptor and commits), snapshot; `make gen` regenerates `internal/httpapi/oapi/oapi.gen.go` |
| `web/` | React SPA shell (M0); the real UI comes in M5 from the Claude Design work |
| `deploy/` | Dockerfile, `config.example.yaml` |

Test idioms: engine tests run in `testing/synctest` bubbles with a real store in a temp dir; every package with goroutines uses `goleak`.

## Next: M3

Docs, badges, webhooks, freshness (`plan.md` section 5): a push updates the page within seconds and a GitHub outage serves stale data.
Read `design/content.md`, `design/http-api.md`, `design/github.md` and the M2 entry in `decisions.md`. Deferred to M3 with the
webhook: `catalog.repo` (single-repository refresh).
