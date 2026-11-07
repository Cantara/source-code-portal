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
   proxy (document images, cached) are the only code that talks to GitHub, and only to read: read permissions, GET requests and
   GraphQL queries, never a write or a mutation (P-71).
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
| GitHub | `google/go-github` v93, `bradleyfalzon/ghinstallation` v2, an own ETag and rate transport |
| Storage | `modernc.org/sqlite` |
| Markdown | `yuin/goldmark` v2, `alecthomas/chroma` v2, `microcosm-cc/bluemonday` |
| Metrics | `prometheus/client_golang` |
| Distributed state | `hazelcast/hazelcast-go-client` |
| Sign-in | `coreos/go-oidc` v3, `golang.org/x/oauth2` |
| Language models | own `net/http` adapters for the Anthropic Messages and OpenAI Chat Completions wire formats (P-28) |
| Tests | `testing`, `testing/synctest`, `go-cmp`, `goleak` |
| Web | Next.js 16 (App Router, standalone), React 19, TypeScript 7 (`tsc`), Tailwind 4, TanStack Query 5, react-markdown with remark-gfm (the assistant's answers), Vitest, MSW, Playwright with axe; pnpm 12, Node.js 26 |
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
| K3 | Presentation profiles (`design/present.md`): Handheld portrait, Handheld landscape, Desk and Wall, each with its own side column, Desk at 60%; a wall told by `?profile=wall` or the tap bar | at 2560×1440 the page is a desk at 60%, at 1180×820 a tablet at 30%, each width kept apart across a reload; `?profile=wall` and `auto` | done |
| N1 | Announcements (`design/announcements.md`, P-65): `announcements[]` in `scp.yaml` and an admin's in the layout store's directory (`internal/filestore`, shared with the layouts), served with the page layout to the viewers and pages they name between their dates, with a QR code of their link | a configured and a stored one served where and when they say, a signed-in one never to the public (the leak test), a draft never, a stale save refused | done |
| N2 | Announcements in the pages: the `announcements` element on the dashboard (dismissible) and `/present` (QR code), both themes; the admin's card in `/admin`; docs, the Guide, the plugin | Playwright sees, dismisses and keeps dismissed; an admin creates, previews, publishes and removes one; axe clean | done |
| D1 | Commit diff stats (`design/diffstat.md`, P-67): lines added and removed and files changed per recent commit, from GitHub's GraphQL API 100 at a time, kept with the commit; `+a −d` with a five-block bar in every commit list | the job asks only what it lacks within 90 days and keeps it across reads; Playwright sees the stats in the lists; axe clean | done |
| PR1 | Open pull requests, the data (`design/pulls.md`, P-68): `internal/actor`, `github.GraphQL`, the `pulls` job (a conditional REST listing, then one GraphQL query of every open pull request's state), the facet, issues counted apart from pull requests, the findings `reviews` and `updates`, `/api/v1/pulls` | an unchanged repository costs one 304 and no GraphQL; a review webhook re-reads one repository; pending checks are asked again; the leak test passes | done |
| PR2 | Open pull requests in the pages: `/pulls` in the header (whose turn, the states as filters), the telemetry tile and the strip's signal, the `pulls` element (waiting for review) in the dashboard's and `/present`'s built-in layouts, the PRs columns of the products and group tables, the group's and the repository's panels, the navigator's branch chip | Playwright filters `/pulls` by state with and without JavaScript; each page shows the fake's pull requests; axe clean in both themes | done |
| PR3 | Open pull requests, the rest: the tool, search, badges, llms; docs, the Guide, the plugin | as Q3 and W3 for the tool and search; the badges pass the leak test | done |
| IS1 | Open issues, the data and the page (`design/issues.md`, P-69): the `issues` job (a conditional REST listing, pull requests skipped), the facet, `/api/v1/issues`, `/issues` in the header (where each stands, the states as filters), the repository's panel, the group's links | an unchanged repository costs one 304; the listing's pull requests are left out; the leak test passes; Playwright filters `/issues` with and without JavaScript; axe clean in both themes | done |
| IS2 | Open issues, the rest: the tool, search, llms, the sitemap; docs, the Guide, the plugin | as PR3 | done |
| PQ1 | Prerequisites (`design/prerequisites.md`, P-71): `internal/prereq` (what each feature needs), the App's grants from its installations, a token's refusals from the jobs, `/api/v1/prerequisites`, the warning on `/pulls` and `/issues`, the Admin card, the status and the log; the docs, the Guide, the skills and the example configuration | an App without *Issues* warns on `/issues` and in Admin; a refused token warns on `/pulls`; nothing shows when all hold; the public answer names no permission; a test keeps the docs' table equal to `prereq.Needs` | done |
| FT1 | Feature switches (`design/features.md`, P-77): `features.pulls` and `features.issues` turn the open pull requests and issues off: no job, route, tool, search kind, badge, sitemap entry, layout element, prerequisite or page of theirs; health judges what it still reads, the Backlog count the best one held | each switch off and both: no GitHub request of their kind, every surface without them, the Backlog count from the issues, from the listing less the pull requests, from the listing alone | done |
| L5 | The dashboard's side panel in sections (`design/layout.md`): the `issues` element; pull requests, issues and latest commits as sections sharing the viewport-high panel equally, each with its inner scroll and its fold (`scp-side-sections`) | three sections in thirds, one folded gives halves, kept across a reload; the issues element off with `features.issues`; axe in both themes | done |
| L6 | The same sections on `/present` (`design/present.md`): its side column holds the latest commits, the pull requests and the issues, each hidden by its button (`scp-present-sections`), a rail when all are hidden; the pull requests and the issues load more at their end on both pages; the status strip compact | at 1180×820 three equal sections and every signal of the strip; one hidden gives halves, kept across a reload; all hidden a rail that brings them back; the lists read their next pages | done |
| AL1 | Archive, the data and the page (`design/archive.md`, P-81): the retired repositories (archived on GitHub or `lifecycle: deprecated`) with last active, latest release, dependants and authors counted; `GET /api/v1/archive`, `/archive` in the header, the notice on a retired repository's page | the filters, sorts and paging pass against a fixture fleet; a private archive entry is absent for the public (twin and leak tests); Playwright and axe pass | done |
| AL2 | Archive, the rest: the tool `retired_repositories`, `llms.txt`, the group's `N retired` link; docs, the Guide, the plugin | as PR3 | done |
| RL1 | Releases, the data (`design/releases.md`, P-82): the `releases` job (the 20 newest per repository as a conditional REST listing), the facet, `badges.release` replaced by the facet, the prerequisites row | the job passes against the fake GitHub (304, pre-release, draft, none, a webhook release); the badges read the same data | done |
| RL2 | Releases in the pages: `GET /api/v1/releases` (variants, since, sorts, paging), `/releases` in the header | each variant and sort passes; Playwright with and without JavaScript, axe | done |
| RL3 | Releases, the rest: the tool `recent_releases`, the search kind `release`, llms, the repository's and the group's panel; docs, the Guide, the plugin | as PR3 | done; the dashboard element is not built (P-84) |
| RL4 | Releases of published packages (`design/releases.md`, P-87): each package's newest release is a row beside the GitHub releases, `source` filters, the latest of a repository is the newer of both; the Source choice, the product page's rule, the tool | the API lists packages, the duplicates once, the counts follow source and variant; the page and the product page agree | done |
| RL5 | Release notes (`design/releases.md`, P-88): a GitHub release's body, else the repository's changelog section for the version (package releases too), rendered by the docs pipeline and shown as a disclosure on `/releases`, the repository's and the product's panels | the API carries `notes`, `notesCut`, `notesFrom`; a package row takes the changelog's section; the panels show them | done |
| SM1 | Store model for a personal edition and shared instances (`design/store-model.md`, P-89): additive migration `0002` (sources, principals, sealed credentials, `visible`, `repo.visibility`), views built per distinct entitlement set by the one function that builds `Public()`, first-claim-wins addresses, the pre-migration copy; the security model unchanged | design verified on a copy of the live database (data identical, view equals twin, cost, sealing, reference counting); build in the order SM-a to SM-c (several principals on one instance is not planned, P-90) | design |
| IN1 | Insights, the data and the page (`design/insights.md`, P-83): cadence, technology, shared libraries, single-author repositories and ageing from the snapshot; `GET /api/v1/insights`, `/insights` in the header | each insight matches a fixture fleet; no author is named; a private repository's numbers are absent for the public; Playwright and axe pass | done |
| IN2 | Insights, the rest: the tool `fleet_insights`, llms; docs, the Guide, the plugin | as PR3 | done |
| IN3 | Insights, the graphs (`design/insights.md`, owner 2026-11-06): the release rhythm heatmap, the version ladder, the dependency matrix, the concentration histogram, the ageing bars, all inline SVG | the API carries the numbers each graph draws; the graphs match a fixture; Playwright and axe pass in both themes | done |
| FP1 | Filters (`design/filters.md`, P-86): one panel at the left of every list page with every choice in view and its counts, hide and show kept in `scp-filters`, applied at once, a GET form without JavaScript; one row layout for the lists (`ListRow`) | the pages filter through the panel; the panel hides, shows and is remembered; the rows end at the same edge; axe and a phone pass | done |
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

## Complexity backlog

`.golangci.yml` holds `gocyclo` at 45 and `gocognit` at 65 (tests excepted): the worst functions today, so none gets harder to read. These are the ones past
a sensible limit (cyclomatic 25, cognitive 40), to split when their code is next changed, worst first, then the ceilings come down:
`search.(*Index).run` (cognitive 106, suppressed), `pulls.readPulls` (62), `issues.readIssues` (58), `packages.PyPI.project` (55),
`cmd/scp.newServer` (53), `commits.sides` (50), `cmd/scp.searchRead` (48), `httpapi.Options.wrap` (44), `commits.layout` (43), `docs.rewrite` (42);
cyclomatic: `search.(*Index).run` (43), `cmd/scp.newServer` (42), `config.Announcement.check` (35), `commits.API.GetHistory` (31), `packages.Client.Get` (31), `manifests.parsePOM` (30), `search.Parse` (30),
`layout.check` (28), `config.Packages.validate` (27), `config.GitHub.validate` (27), `packages.parseVersion` (26), `catalog.ParseDescriptor` (26).
