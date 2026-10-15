# SCP plan (binding)

Source Code Portal (SCP): what it is, how it is built, and the work in progress. Finished milestones, the full decision log and the
earlier detailed design are in `history/`.

## 1. What SCP is

An organisational dashboard for GitHub: repositories grouped into products, with their health (activity, release, licence, issues,
language), latest commits, rendered README and docs, technology and dependencies, SVG badges, a JSON API and a Markdown surface for LLM
agents. One static Go binary serves the API and supervises the server-rendered Next.js web UI; webhooks and a scheduled sync keep it
current. Up to 1,000 repositories and more, on one instance or several sharing state through Hazelcast. The first release is `v2.0.0`;
later releases bump the patch version unless the owner decides otherwise.

Built: GitHub App or token sources with discovery and reconcile; groups by matchers (exact, prefix, glob, regexp, topic, catch-all);
dashboard, group, repository, document and commit-log pages; README and `docs/` rendered and sanitised; the `scp.yaml` descriptor;
`go.mod`, `package.json` and `pom.xml` manifests with internal dependency links; local SVG badges; freshness on every item; signed and
de-duplicated webhooks; `/api/v1` from an OpenAPI 3.1 contract; `/llms.txt` and Markdown answers; a sitemap, canonical addresses, JSON-LD and breadcrumbs for search engines; private repositories behind a trusted
proxy; several instances on Hazelcast; the image proxy; live updates by server-sent events; `/livez`, `/readyz`, Prometheus metrics and
a status page on the admin port; a signed multi-platform image and release archives.

## 2. Architecture

```
 ┌───────────────────────────── cmd/scp (Cobra CLI, wiring) ─────────────────────────────┐
 │  serve · config check · version            builds everything, owns no logic          │
 └───────────────────────────────────────────┬───────────────────────────────────────────┘
   features    catalog  commits  docs  badges  manifests  webhook  llms   ← never import each other
   ─────────────────────────────────────────────────────────────────────────
   core        sync   github   snapshot   store   cluster   httpapi   webui   config   safehtml   untrusted
   ─────────────────────────────────────────────────────────────────────────
   web/        Next.js web UI (server-rendered), run by scp serve as a supervised child, proxied through Go
```

```
 GitHub ──webhook──▶ webhook ──event──▶ sync: job queue (keyed, prioritised, rate-budgeted) ──▶ github client ──▶ GitHub
                                              │ job outcome
                                              ▼
                      feature: parse ─▶ sanitise ─▶ store (SQLite, last good) ─▶ new immutable snapshot (atomic swap)
                                                                                   │
 Browser / LLM / badge ──▶ Go handlers and the Next.js server (through /api/v1) ──┘ read the current snapshot only
```

Rules (checked as described in `design/principles.md`):

1. Request handlers read only the current immutable snapshot. Sync jobs are the only code that writes SQLite; they and the image
   proxy (document images, cached) are the only code that talks to GitHub.
2. Features do not import features. Each feature owns its data on the snapshot (typed facets on the repository entries, indexes over
   all of them), its jobs and its feature-only GitHub calls; a consumer declares the small function it needs and `cmd/scp` wires it. `cmd/scp/boundary_test.go` enforces the imports. The web UI keeps the same rule for `web/src/features`.
3. Failure is data: each item carries its freshness; the last good state is served.
4. Untrusted input (README, descriptor, manifests, webhook bodies) goes through bounded parsers and one sanitiser producing `SafeHTML`.
5. SQLite is a rebuildable cache: deleting it costs a resync, never data.

Packages, config keys and metrics arrive with the code that uses them.

## 3. Stack

Every dependency is the latest release of the best-in-class choice (P-17), pure Go on the server (`CGO_ENABLED=0`); `bluemonday` is
the one exception to currency, kept behind the `SafeHTML` constructor.

| Area | Library |
|---|---|
| Go | 1.27 |
| CLI and config | `spf13/cobra`, `spf13/viper` (one instance per run, unknown keys rejected) |
| HTTP | stdlib `net/http`, `log/slog` |
| API | OpenAPI 3.1, `oapi-codegen` v2 (strict server), `openapi-typescript` and `openapi-fetch` for the web |
| GitHub | `google/go-github` v92, `bradleyfalzon/ghinstallation` v2, an own ETag and rate transport |
| Storage | `modernc.org/sqlite` |
| Markdown | `yuin/goldmark` v2, `alecthomas/chroma` v2, `microcosm-cc/bluemonday` |
| Metrics | `prometheus/client_golang` |
| Distributed state | `hazelcast/hazelcast-go-client` |
| Sign-in | `coreos/go-oidc` v3, `golang.org/x/oauth2` |
| Language models | own `net/http` adapters for the Anthropic Messages and OpenAI Chat Completions wire formats (P-28) |
| Tests | `testing`, `testing/synctest`, `go-cmp`, `goleak` |
| Web | Next.js 16 (App Router, standalone), React 19, TypeScript 7 (`tsc`), Tailwind 4, TanStack Query 5, react-markdown with remark-gfm (the assistant's answers), Vitest, MSW, Playwright with axe; pnpm 12, Node.js 24 |
| Build | Makefile, golangci-lint, GitHub Actions, GoReleaser, cosign, Renovate |

## 4. Work

| M | Delivers | Done when | State |
|---|---|---|---|
| S1 | Sign-in and admin (`design/auth.md`): OIDC with PKCE, bearer tokens, stateless sealed sessions, an admin role | signed in through a fake provider a member of a private group sees a private repository, an admin reads the status, a token reads `/llms.txt` with its groups; forged state, nonce and cookies refused | done |
| A1 | Assistant (`design/assistant.md`): LLM providers (Anthropic, OpenAI, Gemini, OpenRouter, Ollama) and a read-only chat over the snapshot | the adapters pass their recorded fixtures; the loop keeps its budgets and frames every tool result; `/ask` answers with links against a fake provider; a public viewer's assistant never reads a private repository | done |
| M13 | Search engines (`design/seo.md`): canonical addresses, sitemap, robots.txt, JSON-LD with breadcrumbs, Open Graph text | every sitemap page answers 200 with its canonical, one h1, a JSON-LD graph with its trail and a `lastmod` | done |

Each milestone starts with its design note and lands as small commits on `main`, CI green.

## 5. How we work

- Greenfield: code, contract, config and the store schema change in place, without compatibility layers or extra migrations.
- The agent decides and records decisions in `decisions.md`; open questions are not left in documents.
- No test touches the network: a fake GitHub server and recorded fixtures.
- Planning stays under `.claude/work/`; `docs/` is user documentation; finished work moves to `.claude/work/history/`.
