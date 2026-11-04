# Changelog

Notable changes, newest first. Versions follow [Semantic Versioning](https://semver.org/): each release after 2.0.0 bumps the patch
version unless a minor or major bump is decided for it. The GitHub release of each tag lists every change from its Conventional Commit
subjects.

## 2.2.1

A release is also what a repository publishes to a package registry. No configuration change is needed to upgrade: the new
`packages.*` keys are optional, and the public registries are asked for public repositories by default (`packages.public`).

- Published packages: the portal reads what a repository publishes from its build files and asks the registry for the newest stable
  version: Maven (`pom.xml` with its parent chain's `distributionManagement`, Gradle with `maven-publish`), npm, PyPI, Cargo, Go,
  Swift, CocoaPods, NuGet, RubyGems and Packagist. The version shows on the release chip where there is no GitHub release, in the
  repository page's Packages panel and in the API's `packages` field, and it counts as a release for health.
- Private registries (`packages.registries`): Nexus, Artifactory, GitHub Packages, Verdaccio, a Cargo registry, a Go proxy and more,
  signed in with Basic, a bearer token or a header of their own; credentials go only to the registry's own URL, never on a redirect
  away from it. `network: private` reaches a registry on your network. A private repository's packages are never named to a public
  registry.
- Manifests read `Cargo.toml` and `pyproject.toml` too.
- A licence shows its short name (APL2, MIT, ...) and links its text; the open issues link to the issue tracker.
- The Guide: connecting GitHub first, access tokens before single sign-on, and a section on packages and private registries.
- More robust reads: trees and the commit list stream in bounded memory, a read that fails part-way keeps what it read, a new
  release runs failed jobs again at its first start, and a GitHub answer larger than 1 MiB is no longer cut short.

## 2.2.0

The dashboard becomes a page of signals the reader tunes, and the portal explains itself. No configuration change is needed to
upgrade: the new keys are optional and off by default.

- A landing page of signals: telemetry, a fleet map of every repository coloured by its last change, distilled product cards or a
  sortable table, and the latest commits beside the products as a side panel that folds away and resizes.
- The work that counts: commits are told apart as by hand, with AI (an AI co-author, a tool's footer or a coding agent) and by
  automation (CI, dependency and release bots, and sweeps across many repositories). The reader chooses any of them in the new
  Settings page (cookie `scp-work`); `/api/v1/dashboard?work=` and `/api/v1/commits?work=` take the same choice.
- Every colour signal (fleet tiles, legends, activity lights, health and release chips, the AI mark) names itself in a small card at
  once on hover, focus or a tap.
- A Getting started page (`/guide`, *Guide* in the header) explains the signals with screenshots and how to run a portal; the header's
  right holds the Guide, Settings and the theme as icons.
- A design mode for administrators: the dashboard's elements, their order and their groups, kept in `layout.dir` (empty: the
  built-in layout, no design mode).
- `unclaimed: true`, a group matcher for the repositories no other group selects (for an "Other" group instead of `catch_all: true`).
- A Claude Code plugin for operators: `/plugin install scp --marketplace Cantara/source-code-portal`.
- Fixes: a repository without a commit of the work that counts no longer looks active; each commit is classified once per snapshot.

## 2.1.0

The web UI moves to Next.js and the portal gains the features around it. Upgrading a binary install needs the web UI bundle and Node.js 24
(see "Web UI bundle"); the container image already holds both.

- Web UI bundle: the pages are rendered by a Next.js server that `scp serve` supervises and proxies (`web.dir`, `web.node`; a release
  ships `scp-web_<version>.tar.gz` next to the binaries, checksummed and signed with them). With `web.dir` empty every page answers 503;
  the API, badges, `llms.txt` and webhooks still work.
- A mission-control dashboard with the health badges of every repository per product; badges on the dashboard cards and in the group
  table; the portal's release shown next to its name.
- A health score for every repository, weighed into a score per product.
- Manifests: `go.mod`, `package.json` and `pom.xml` per repository, with technology, dependencies and internal links.
- Commit history: 500 default-branch commits kept per repository, a commit navigator with a git graph and numbered pages of a chosen
  size, product timelines.
- Sign-in through OIDC with PKCE or a bearer token, with administrators (`docs/production.md`, operator guide).
- An assistant at `/ask` that answers from the catalogue through Anthropic, OpenAI, Gemini, OpenRouter or Ollama. Off by default.
- Private repositories for permitted groups behind a trusted authenticating proxy.
- Several instances on Hazelcast with one leader, shared outcomes and webhook de-duplication.
- Live updates: server-sent snapshot events, a content-derived snapshot version and an image proxy for document images.
- Search-engine and link-preview support: one page address in Go and the web, JSON-LD graphs, Open Graph text, complete breadcrumbs.
- `scp config groups` previews the groups on a repository list; user accounts are listed too (an App's user installation).
- Fixes: overlapping snapshot builds no longer drop updates; the web UI server restarts when its bundle is replaced; the system theme
  follows the system preference.
- `GET /knowledge.yaml` and `GET /.well-known/kcp.json`: the catalogue as a Knowledge Context Protocol manifest, so an agent that
  knows only the portal's address finds a map of groups, repositories and documents with intents and reading order. `GET
  /groups/{group}.md` answers like the other `.md` addresses.
- The Java portal's addresses (`/dashboard`, `/group/{id}`, `/contents/...`, `/commits/...`, `/badge/...`) redirect to their
  successors; Jenkins and Snyk badges, the wiki and the old webhook answer 410.
- For portals of private repositories (`docs/security-model.md`): `access.require_auth` (no public view), `github.sources[].exclude`,
  an `audit` log line per request in the private view, a warning for wide `server.trusted_proxies`, deleted repositories gone within
  about 30 seconds, and one test that holds every public route to the same view.

## 2.0.0

The first release of the Go rewrite (2.0.0 follows the Java product's 1.x line): one static binary and container image serving an
organisational dashboard for GitHub, with a server-rendered web UI.

- Catalogue of the repositories of one or more organisations (GitHub App or token, github.com or Enterprise Server), grouped by
  configured matchers, with renames and transfers followed.
- Pages: dashboard, group, repository (descriptor, badges, README, documents, commits), documentation viewer and commit logs, in light
  and dark themes, accessible (WCAG 2.2 AA).
- README and `docs/` rendering with highlighting and GitHub alerts through one sanitiser; the optional `scp.yaml` descriptor.
- Freshness from webhooks within seconds and scheduled sync otherwise; the last good data is served through GitHub outages.
- JSON API `/api/v1` (OpenAPI 3.1), SVG badges, `/llms.txt`, `/llms-full.txt` and Markdown answers for agents.
- Operations: separate admin listener with probes, Prometheus metrics and a status page; signed images for linux/amd64 and linux/arm64.
- Package manifests: `go.mod`, `package.json` and `pom.xml` are read per repository; the repository page shows the technology and
  dependencies they declare and the catalogued repositories it uses and is used by (`/api/v1/repos/{owner}/{name}/manifests`, and in the
  repository's Markdown).
- Private and internal repositories (`include_private`), shown only to the groups in `access.private_groups` as named by a trusted
  authenticating proxy; everyone else sees the public repositories on every surface.
- Several instances serve one portal through Hazelcast (`cluster.addresses`): one leads and alone calls GitHub, every instance serves
  the same data, webhook deliveries are de-duplicated across instances and reach the leader from any of them, and another instance takes
  over when the leader stops.
- Images in documents load from the portal (`/images/...`), relative and external ones alike, also for private repositories; the web
  UI's CSP is `img-src 'self'`.
- Live updates: `/api/v1/events` streams server-sent events, and an open page shows new data within seconds without polling. The
  snapshot `version` identifies the data and is equal on every instance.
- The web UI is rendered on the server with Next.js: every page arrives as complete HTML, readable without JavaScript, with its own
  title, description and canonical URL, and `/sitemap.xml` and `/robots.txt` list the pages. `scp serve` runs the web UI bundle with
  Node.js (`web.dir`, `web.node`); releases carry it as `scp-web_<version>.tar.gz`, and the image includes Node.js and the bundle.
- Every page under a renamed or transferred repository answers 308 to the same page at its current address, and its canonical URL
  names the repository as GitHub spells it; a failed panel offers Retry.
- Internally, each feature owns its data, its GitHub calls and its pages, so a new feature is its own package or folder plus one wiring
  line.
