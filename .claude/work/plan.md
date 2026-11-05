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
| H1 | Hardened private instance (`design/hardening.md`, issue #5): one leak test over every surface, `access.require_auth`, `github.sources[].exclude`, a private access log, a start warning, removal on a deleted delivery, `docs/security-model.md` | the leak test walks every contract GET and every other route as the public and finds no trace or existence of a private repository; with `require_auth` an anonymous request gets 401 on every route but sign-in | done |
| M13 | Search engines (`design/seo.md`): canonical addresses, sitemap, robots.txt, JSON-LD with breadcrumbs, Open Graph text | every sitemap page answers 200 with its canonical, one h1, a JSON-LD graph with its trail and a `lastmod` | done |
| L1 | Landing page of signals (`design/layout.md`): people and automation counted apart per product, sweeps discounted, exceptions-only chips, the `unclaimed` matcher (issue #8); the page built from elements: fleet map, product cards, product table, latest commits by people, telemetry | a product card has the same five rows whatever the product's size (a portal of 134 repositories in 13 products went from 11.6 screens at 1440×900 to 3.1); a product with only automated commits reads as quiet; a sweep to more than 10 repositories counts as no one's work; an unclaimed group holds only what no other group claims | done |
| L2 | Layouts and design mode (`design/layout.md`): the landing page's elements as data in a file-system layout store (`layout.dir`), edited by an admin in a design mode | an admin composes the landing page, saves it and every viewer gets it; a stale save is refused; a public viewer's page never names a group of only private repositories; without `layout.dir` the default renders | done |
| L3 | AI-assisted commits (`design/layout.md`, P-40): each commit's origin (person, assisted, agent, automation) from its trailers and author; the dashboard counts the commits with an AI within people's and names the assistants; the commit lists mark them | the cards, the table and the telemetry show the share with AI; Renovate Bot and bot-merged pull requests count as automation; coding agents count as people's work | done |
| L4 | The work that counts and Settings (`design/layout.md`, P-42): the reader's lens on the work (any of by hand, with AI and by automation; P-43) in a Settings page with the other preferences | a card ranks neither; each lens changes the counts, sparklines, people, last change, fleet map and latest commits; Settings keep every preference in a cookie and the pages follow | done |
| G1 | The guide and the operators' plugin (`design/guide.md`, P-44, P-45): a Getting started page in the header with screenshots of a made-up catalogue, kept by the skill `guide-page`; a Claude Code plugin marketplace with the skill `run-scp` for released portals | the guide explains every signal of a card with its picture numbered row by row, in both themes, axe-clean and without client JavaScript; the plugin validates and its skill works from the running release's documents | done |
| R1 | Published packages (`design/packages.md`): package providers behind one interface, the `packages` job with structured concurrency and a shared registry client (limits per host, cache, pauses, the public-only guard); `maven` (the parent chain's `distributionManagement`, `maven-metadata.xml`) and `npm`; the release chip and the Packages panel | a repository whose POM inherits its distribution from a parent shows the release from that repository; an npm package shows its `latest`; a registry that hangs holds no other lookup; the leak test passes | done |
| R2 | Packages: `pypi`, `cargo`, `go`, `swift`; Gradle coordinates; `Cargo.toml` and `pyproject.toml` in the manifests | each provider passes recorded answers of its public registry; a private repository's packages are never named to a public registry | done |
| R3 | Packages: private registries (`auth`: Basic, bearer, header; `network: private`), GitHub Packages; the operator guide and the Guide | a fake private registry answers only with its credentials; a build file naming another host never receives them | done |
| R4 | Packages: `nuget`, `rubygems`, `packagist` | as R2 | done |
| W1 | Agents (`design/agents.md`): one tool catalogue (`internal/tools`) for the assistant, MCP and WebMCP; `GET`/`POST /api/v1/tools` | the assistant's tests pass on the catalogue; the leak test calls every tool as the public | done |
| W2 | Agents: `/mcp`, the catalogue over MCP's Streamable HTTP, stateless, tokens or the public view | an MCP client lists and calls the tools, sees the private repository with a token and not without; another origin is refused | done |
| W3 | Agents: WebMCP in the web UI (`document.modelContext`): the catalogue and the page's own tools; docs, the Guide, the plugin | Playwright with a fake `document.modelContext` registers and runs the tools, page-aware | done |
| Q1 | Search (`design/search.md`): the index (`internal/search`), a segment per repository from the snapshot's facets (repositories, document sections, manifests, packages, commits; groups per view), reused by `Snapshot.Digest`, BM25F, the query language | every kind found by words, phrase, prefix, one edit and each filter; one changed repository rebuilds one segment; the public view holds no private segment; the size test within its budgets | done |
| Q2 | Search: `GET /api/v1/search` and `/search/suggest` in the contract, the `search` tool for the assistant, MCP and WebMCP, the leak test's queries, status and metrics | a private viewer finds the private repository by name, README word, commit and dependency; the public finds nothing, in hits, counts or suggestions | done |
| Q3 | Search: `/search` (server-rendered, a GET form), the header's box with `/` and ⌘K suggestions, OpenSearch and the `SearchAction`; docs, the Guide, the plugin | Playwright finds a document section and opens it at its heading with and without JavaScript, the combobox by keyboard, axe clean | done |
| K1 | Presentation mode (`design/present.md`): `/present` from the layout page `present`, filling the viewport without scrolling: the status strip with the data's age, the fleet map by health, products as tiles with the good ones folded, latest commits at full height, live; `?groups=`, `?work=` | at 1180×820, 1366×1024 and 1920×1080 the page never scrolls and every product is a tile or in the fold; a closed stream turns the age to watch and attention; the leak test walks it | done |
| K2 | Presentation mode, the wall: no chrome but a tap bar, wake lock, the manifest, reload on a new build and hourly, burn-in shift; design mode on `/present`; docs, the Guide, the plugin | an iPad added to the home screen opens it full screen and stays awake; a new release reloads it; axe clean in both themes | done |
| E1 | Embedding, the contract (`design/embed.md`): versioned HTML fragments for host sites, their parameters, parts and `--scp-*` custom properties, `embed.origins` | a contract test fails when a fragment's parameter, part or custom property changes without a new version | dequeued: the owner returns to it later |
| E2 | Embedding, the fragments: product card, fleet map, telemetry, latest commits, repository health in declarative shadow roots; the dashboard built from them | the dashboard renders through the fragments with its gates unchanged; a fragment alone renders in a blank page without script | dequeued |
| E3 | Embedding, the behaviour module `/embed/v1/scp.js`: custom elements, hints, live updates, theming | a static host page with two fragments themes them, updates both on one push and passes axe | dequeued |
| E4 | Embedding, the host side: outbound snapshot webhooks, `@scp/client`, a reference host | the reference host shows a push within one snapshot through the webhook, without polling | dequeued |

Each milestone starts with its design note and lands as small commits on `main`, CI green.

## 5. How we work

- Greenfield: code, contract, config and the store schema change in place, without compatibility layers or extra migrations.
- The agent decides and records decisions in `decisions.md`; open questions are not left in documents.
- No test touches the network: a fake GitHub server and recorded fixtures.
- Planning stays under `.claude/work/`; `docs/` is user documentation; finished work moves to `.claude/work/history/`.
