# SCP plan (binding baseline)

Source Code Portal (SCP), rewritten in Go. This file is the plan: short and coarse on purpose. Written 2026-10-14; it replaces the
detailed design package in `reference/detailed-design/` as the binding document (see "The reference package" at the end). Decisions taken while writing it are in
`decisions.md`. When this file and anything under `reference/detailed-design/` disagree, this file wins.

## 1. What SCP is

An organisational dashboard for GitHub: repositories grouped into systems, with their latest commits, rendered README and docs, status badges
and an API that scripts and LLM agents can read. One static Go binary with the web UI embedded. Kept fresh by webhooks plus scheduled sync.
Target: up to 1,000 repositories, one instance.

## 2. Scope

**v1 (the first release)**

- Sources: one or more GitHub organisations through a GitHub App (a token as fallback); repository discovery and periodic reconcile.
- Groups: systems defined in config by matchers (exact, prefix, glob, regexp, topic); a repository may be in several groups.
- Pages: dashboard, group, repository, commit log (default branch).
- Docs: README and `docs/` as CommonMark + GFM, sanitised; relative links and images resolved to GitHub; optional `scp.yaml` descriptor
  (title, owners, lifecycle, tags, links).
- Badges: local SVG badges (last commit, release, licence, issues, repository count per group). No third-party badge services.
- Freshness: every item shows its age; GitHub failures serve the last good data.
- Webhooks: push, repository and installation events (HMAC-SHA256, de-duplicated).
- API: JSON under `/api/v1` from an OpenAPI 3.1 contract; `/llms.txt` and raw Markdown for LLM agents.
- Operations: `/livez`, `/readyz`, Prometheus `/metrics` on a separate admin port; structured logs; one YAML config with `SCP_*` overrides;
  container image (distroless, non-root, read-only).

**v1.1**

- Package-manifest intelligence: Go, npm, Maven first (technology and dependency panel per repository, internal dependency list).
- Private repositories behind a trusted authenticating proxy.
- Multiple instances, with Hazelcast for shared state (A-35).
- Image proxy for external README images; live updates by server-sent events.

**Later (v1.2+), only on demand**

More manifest ecosystems, org-wide dependency graph and CycloneDX export, Jenkins and Snyk status, AsciiDoc, OpenTelemetry tracing.

## 3. Architecture

```
 ┌───────────────────────────── cmd/scp (Cobra CLI, wiring) ─────────────────────────────┐
 │  serve · config check · version            builds everything, owns no logic          │
 └───────────────────────────────────────────┬───────────────────────────────────────────┘
   features    catalog   commits   docs   badges   webhook   llms        ← never import each other
   ───────────────────────────────────────────────────────────────────────
   core        sync (scheduler + job queue)   github (client, App auth)   snapshot   store (SQLite)
               httpapi (server, OpenAPI handlers)                          config (Viper)
   ───────────────────────────────────────────────────────────────────────
   web/        React SPA (Vite build), embedded with go:embed
```

```
 GitHub ──webhook──▶ webhook ──event──┐
                                      ▼
 scheduler ──"what is due"──▶ sync: job queue (keyed, de-duplicated, rate-limited)
                                      │ runs job
                                      ▼
                         github client (App token, ETag) ──▶ GitHub
                                      │ data
                                      ▼
              feature: parse ─▶ sanitise ─▶ model ─▶ store (SQLite, last good)
                                      │
                                      ▼
                     new immutable snapshot ─▶ atomic pointer swap
                                      │
 Browser / LLM / badge ──▶ handlers ──┘ read the current snapshot only (never GitHub, never SQLite)
```

Rules that carry the design (detailed in `design/principles.md`):

1. Request handlers read only the current immutable snapshot. Sync jobs are the only code that talks to GitHub or writes SQLite.
2. Features do not import features; they share data through the snapshot and small interfaces declared by the consumer. Wiring happens
   only in `cmd/scp`. One test enforces "no feature imports a feature".
3. Failure is data: each item carries its freshness; the last good state is served.
4. Untrusted input (README, descriptor, webhook bodies) goes through bounded parsers and one sanitiser that produces a `SafeHTML` type.
5. SQLite is a rebuildable cache: deleting it costs a resync, never data.

Packages are created when the first code needs them, not in advance. Config keys and metrics are added with the code that reads or writes
them and documented from the code (`scp config check` prints the effective config).

## 4. Stack

Rule: every dependency is current (a release within the last 18 months, supports the current Go), widely used, and pure Go
(`CGO_ENABLED=0`). One documented exception: `bluemonday` (last release 2024-07), the only mature Go HTML sanitiser, kept behind the
`SafeHTML` constructor so it can be swapped. Versions below were read from proxy.golang.org on 2026-10-14; re-check when first imported.

| Area | Library |
|---|---|
| Go | 1.27.x |
| CLI and config | `spf13/cobra` v1.10, `spf13/viper` v1.21 (one instance per run, unknown keys rejected) |
| HTTP | stdlib `net/http` (`ServeMux` patterns), stdlib `log/slog` |
| API | OpenAPI 3.1 contract, `oapi-codegen` v2.8 (strict server); `openapi-typescript` for the SPA client |
| GitHub | `google/go-github` v92, `bradleyfalzon/ghinstallation` v2.19 (App auth), conditional requests (ETag) in a small own transport |
| Storage | `modernc.org/sqlite` v1.60 |
| Markdown | `yuin/goldmark` v1.8, `alecthomas/chroma` v2, `microcosm-cc/bluemonday` (exception above) |
| Metrics | `prometheus/client_golang` v1.25 |
| Distributed state (v1.1) | `hazelcast/hazelcast-go-client` v1.6 |
| Tests | stdlib `testing` + `testing/synctest`, `google/go-cmp`, `go.uber.org/goleak` |
| Web | React 19, TypeScript, Vite, Tailwind 4 (shadcn-style components), TanStack Router and Query, Vitest, Playwright; pnpm |
| Build | Makefile, golangci-lint, GitHub Actions, GoReleaser, Renovate |


## 5. Milestones

Each milestone ends with working software on `main`, CI green. M1 to M4 build the backend and API, and M5 builds the
UI from `design/web.md`.

| M | Delivers | Done when |
|---|---|---|
| M0 | Skeleton: `go.mod`, Makefile, CI, Dockerfile, Cobra CLI, Viper config, `/livez`, SPA shell embedded | `make check` and the image build pass in CI |
| M1 | Catalogue and sync: GitHub App auth, discovery, groups, job queue, SQLite, snapshot | a test org (fake GitHub in tests) shows up in `/api/v1/groups` |
| M2 | Read API for the pages: dashboard, groups, repositories, commits | the API serves a real org end to end (fake GitHub in tests) |
| M3 | Docs, badges, webhooks, freshness | a push updates the page within seconds; a GitHub outage serves stale data |
| M4 | `/llms.txt`, raw Markdown, metrics, readiness, hardening (CSP, limits) | security and load checks pass for 1,000 repositories |
| M5 | Web UI: dashboard, group, repository, docs and commit pages | every page renders a real org; Playwright smoke, axe and CSP checks pass |
| M6 | Release 2.0.0: docs for operators, GoReleaser, image published | `ghcr.io/cantara/source-code-portal:2.0.0` runs from the operator guide |

v1.1 follows the same pattern, one milestone per bullet of section 2.

## 6. How we work

- Greenfield: code, contract, config and the store schema change in place, without compatibility layers or extra migrations.
- One milestone at a time; each lands as small commits with tests directly on `main`, CI green. No test touches the network: a fake GitHub server and recorded
  fixtures.
- The agent decides and records decisions in `decisions.md`; open questions are not left in documents.
- Planning stays under `.claude/work/`; `docs/` is user documentation.

## 7. The reference package

`reference/detailed-design/` (about 1,900 files) is the earlier, detailed design: decisions, architecture, ADRs, 18 unit designs with tested reference code,
reviews, a 370-key config catalogue and a repository seed. It is **reference material**: consult it when building a part (for example
`reference/detailed-design/20-design/05-github-adapter.md` for GitHub rate limits and webhook details, `reference/detailed-design/02-design-guardrails.md` for background on `design/principles.md`), copy what is useful. It is not kept in sync and not updated further. Its repository seed
(`reference/detailed-design/50-repo-seed/`) is a starting point for M0, trimmed to this plan.
