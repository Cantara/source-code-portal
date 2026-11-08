# Changelog

Notable changes, newest first. Versions follow [Semantic Versioning](https://semver.org/): each release after 2.0.0 bumps the patch
version unless a minor or major bump is decided for it. The GitHub release of each tag lists every change from its Conventional Commit
subjects.

## Unreleased

- **The store's schema is flat**: the four migrations became one baseline (`0001_init.sql`, marked with the application id `SCP1`). A store made by an earlier release is moved aside on the first start (`<store.path>.quarantine-<time>`, nothing to do, the newest two are kept) and the catalogue is read again from GitHub, a minute or two; it held only what GitHub gives again. An older release can still open the new store, and renaming the moved-aside file back restores the old one. From here on the store changes only by additive migrations again, with the `.pre-NNNN` copy first. `make rehearse` proves both generations.

- **Branch pushes and deleted refs reach the portal**: a push to a branch other than the default, and a deleted branch, make the history read that repository again at once (it waited for the 2-hour poll); a deleted tag reads the tags and releases again. A source with a webhook receiver but no GitHub App, whose missed deliveries nothing can recover, reads its releases every 30 minutes instead of every 6 hours.
- **Events report themselves**: the App's webhook delivery log is read up to 10,000 deliveries a round (it was 500, and a longer log was taken as complete and the older deliveries were lost); a log longer than that is passed on as far as it reaches and said so. `/api/v1/status` lists each log under `webhook.catchUps` (last read, last error) and warns when one has not been read for 10 minutes; a failing read is a warning in the log every time. The audit line carries `component=audit`.
- **The modes are kept apart**: `scp serve` (the shared site) and `scp personal` are chosen by the command and nothing in a configuration. A store now belongs to one of them from its first use and each refuses the other's before touching it (an existing site's store is claimed as shared on its next start; nothing else changes for it). The admin status and the start-up log say the mode. A new server build (`go build -tags server`, `make server`) leaves `scp personal` out of the binary altogether; the container image is that build, and the release adds `scp-server_<version>_linux_<arch>` archives next to the unchanged `scp_<version>_<os>_<arch>` ones.

- **The personal edition's GitHub calls use the libraries the portal already has**: creating a GitHub App from a manifest, listing where it is installed and the sign-in's reads (who you are, your organisations and repositories) go through go-github, the App's token is signed by `ghinstallation` and the sign-in by device code uses `golang.org/x/oauth2`, instead of code of our own. Nothing changes for `scp serve`.
- **Hardened profile, the last two gaps**: the private view's audit line now has a counter, `scp_private_view_requests_total` (by route pattern and status, never a path, viewer or repository). A new `auth.oidc.require_verified_email` (default off, so nothing changes) treats an `email_verified` claim the provider does not send as not verified, and `scp serve` and `scp config check` warn when `auth.admin_emails` is trusted on a provider's silence while private repositories are served. See `docs/security-model.md`. A test now fails when a registered route is neither walked by the leak and twin tests as the public nor named with the test that holds it; the legacy addresses, `/index`, `/repos/{owner}/{name}` and `/robots.txt` joined the walk.

- **Everything in SQLite, and an instance that dies survives**: the layouts and announcements administrators save now live in the store
  (the new `document` table, a copy `scp.db.pre-0004` is taken first) instead of files, and every acknowledged write (a saved
  document, a sealed credential, a parked webhook event) is synced to disk before it returns. `layout.dir` is now only read once, at
  the start, to import the files an earlier version kept; after that remove it. `make rehearse` kills a real instance with SIGKILL
  after the saves and proves the restart serves everything.

- **Upgrade and rollback rehearsal**: `make rehearse` (part of `make check`) runs the newest release and the current tree on one store against a fake GitHub and proves the migration keeps the data, takes the `.pre-NNNN` copy, that the older build quarantines a migrated store, and that the copy brings the data back.

- **Personal edition: events with a GitHub App of your own**: a new "Events" step in the setup page makes a read-only GitHub App per
  account with GitHub's one-click manifest flow. Accounts with the App installed get events within a couple of minutes (the portal
  reads the App's delivery log; nothing needs to reach your computer), the others are polled with your token. `personal.yaml` then
  has a source per mode and `webhook.catch_up`; the App's key is sealed in the vault (`app.credential`). Nothing changes for
  `scp serve`.

- **A mode per source, a cadence per subject**: a source the portal hears nothing from (no webhook receiver, no `webhook.catch_up`) now
  asks GitHub for new releases every 30 minutes instead of every 6 hours (conditional requests: an unchanged answer is free); sources
  with events keep the 6-hour net. The Admin status lists each source's mode (events or polling) and how often each subject is
  refreshed.
- **Events without a receiver**: the new `webhook.catch_up` (default off) reads each GitHub App's webhook delivery log every two minutes and
  acts on what was not answered with a success, as the receiver's net already did, without needing `webhook.secret_file`. For a portal
  GitHub cannot reach. `GET /api/v1/admin/status` says whether the receiver and the catch-up run. Nothing changes for a configuration
  without the key.

Release notes on the Releases page, the repository's and the product's pages, and a store that migrates without losing data, plus a
personal edition (`scp personal`). No configuration change is needed to upgrade.

- **Release notes**: a release's notes show under its row as a disclosure ("Release notes"), on `/releases` and in the Releases panels of
  a repository and a product. They are the GitHub release's own text; a package release, and a GitHub release without a body, shows the
  section of the repository's `CHANGELOG.md` (or `CHANGES`, `HISTORY`, `NEWS`, `RELEASES`) for its version, marked "from the changelog".
  Long notes are cut with a link to the rest. `GET /api/v1/releases` rows carry `notes`, `notesCut` and `notesFrom`. The portal makes
  two more conditional requests per repository to read the changelog; no new permission.
- **The database is kept across versions**: a store that holds data is copied to `<store.path>.pre-0002` (mode 0600, once) before its
  schema changes, and the change only adds a column, `repo.visibility`, which each source's listing fills. Put the copy back to go back
  to the previous version; without it an older version moves a newer database aside and resyncs from GitHub.
- **Two sources that list one `owner/name` no longer take it from each other**: the source whose id sorts first owns it and the other's
  repository is left out and named in the log (before, the address changed hands at every listing and its data was deleted each time).
  Not reachable with today's configuration, which keeps an account in one source.

- **What is yours**: the Pull requests and Issues pages have a "Mine" group in their filter panel (opened by me, asking my review,
  assigned to me) for a session whose credential knows its GitHub login: a token configured with the new `auth.tokens[].login`
  (`scp personal` sets it). New query parameters `author`, `reviewer` (pull requests) and `author`, `assignee` (issues); `GET
  /api/v1/me` carries `login`. The pull requests' people asked to review are now kept by login (up to 10), read from the same
  listing as before; no new request or permission.
- **A personal edition**: `scp personal` signs you in to GitHub (github.com, a GitHub Enterprise Server or a GHE.com tenant) with a code
  on GitHub or a pasted token, keeps the credential sealed in the store (the key in the operating system's keychain, else a 0600 file),
  writes an editable `personal.yaml` and serves a portal on this computer only that refuses every request without a session; a link
  that works once signs the owner in. See [the personal edition](docs/personal-edition.md). New key `github.sources[].credential`
  (`scp serve` refuses it); new store table `credential` (migration 0003, a copy first); new dependencies `go-keyring` and `x/term`.
- **Products by drag and drop, with rules that keep fitting**: the first run's product step lists your repositories per account and
  lets you drag them into products; the rules a product's repositories share (a name that starts the same, a topic) are derived
  and written to `personal.yaml`, so a repository you create later joins by itself, with a hint when unassigned repositories start
  like a product's. The repositories are read as a background job with a progress bar and a Cancel button (no more 500-repository
  cap), and the start has a staged bar.
- **A first run in the browser**: `scp personal` opens a page on this computer that signs you in (a code on GitHub with `--client-id`,
  or a pasted token), lists your accounts and repositories and lets you sort them into products (suggested from the names), then
  starts the portal and signs you in (a button takes the token the GitHub CLI already holds). `--terminal` keeps the questions in the terminal, `--no-browser` only prints the link.

## 2.2.9

One filter panel for every list page, published packages in the Releases page, and a tidier logo. No configuration change is needed to
upgrade; nothing new is asked of GitHub.

- **Filters at the left**: Commits, Pull requests, Issues, Releases, Archive, Insights, Search and a repository's history have their
  selectors in a panel at the left, every choice in view with its count (no menus, no Apply button), hidden and shown with a button and
  kept for every list page. On a phone it starts folded. Without JavaScript it is a form with an Apply button. A *Clear* link undoes
  the filters.
- **The same row everywhere**: commits, pull requests, issues, releases and the archive draw their rows alike (avatar, title, what
  weighs the row at the right, then the details), each list in one card.
- **Releases include published packages**: each package a repository publishes to a registry (Maven, npm, PyPI, ...) is a row with its
  newest release, beside the GitHub releases; the latest of a repository is the newer of both, so a repository that only publishes to a
  registry is listed. A *Source* choice limits the page to GitHub's or the packages'. `GET /api/v1/releases` gains `source` and each
  row `source` and `package`; the tool `recent_releases` takes `source`. The product page's Release column follows the same rule. A
  package version equal to a GitHub release's tag is listed once; a package whose registry gives no date is not listed.
- The header's logo no longer says "Mission control".

## 2.2.8

Insights are drawn as graphs, and Alumni is now called Archive. No configuration change is needed to upgrade; nothing new is asked of GitHub.

- **Insights as graphs** (`/insights`): a heatmap of releases per product over the last 52 weeks, ranked bars for languages, build tools
  and dependants, a version ladder per runtime with the oldest version marked, a matrix of the internal libraries against the repositories
  that use them, a histogram of how much the busiest author wrote (never naming anyone), and activity bars per product. Each graph has a
  text alternative and its table under *Show as a table*; the charts are inline SVG, so the JavaScript budget is unchanged.
  `GET /api/v1/insights` gains the numbers they draw.
- **Alumni is now Archive**: `/alumni` is `/archive` and `GET /api/v1/alumni` is `GET /api/v1/archive` (the list is `archive`, a row an
  `ArchiveEntry`). The old addresses are gone; the `retired_repositories` tool keeps its name. The 2.2.7 entry below uses the new name.

## 2.2.7

Three new pages and their tools. No configuration change is needed to upgrade; nothing new is asked of GitHub (the App's *Contents*
permission and the *Release* event are already required).

- **Releases** (`/releases`, `GET /api/v1/releases`): the 20 newest published releases of each repository, newest first, as the latest
  stable release of each repository (the default), every release, the stable ones or the pre-releases, with a time window and four
  sorts. A repository's page and a product's page show their own. One read per repository now serves both the badges' latest release
  and this list.
- **Archive** (`/archive`, `GET /api/v1/archive`): the retired repositories, those GitHub archived and those whose `scp.yaml` says
  `lifecycle: deprecated`, with why, when last active, how many repositories still depend on them and how many people wrote their
  recent commits (counted, never named). A product's page says `N retired`.
- **Insights** (`/insights`, `GET /api/v1/insights`): how often the fleet ships, what it is built on, which internal libraries most
  repositories lean on, which repositories are mostly one person's (the repository, never the person) and which have gone quiet. Derived
  from what the portal holds; no new job and no GitHub call.
- The assistant, MCP and WebMCP get the tools `recent_releases`, `retired_repositories` and `fleet_insights`; search finds releases
  (`in:release`); `llms.txt` and a repository's Markdown page say the latest release and mark retired repositories.
- The header's links scroll sideways on a phone instead of wrapping into a taller bar.
- Internal: the browser tests and the load check no longer run in CI (they run in `make check`); the leak and twin tests are far faster.

## 2.2.6

Dependency updates only; no configuration change is needed to upgrade.

- Next.js 16.4.5 (security fixes for `use cache`, Cache Components, the image optimiser and the edge runtime) and oapi-codegen 2.8.2
  (security release; the generated code is unchanged), with Vite 8.3.6, TanStack Query 5.106.1, goldmark 2.1.15, SQLite (modernc) 1.61.1,
  pnpm 12.18, GoReleaser 2.20 and docker/login-action 4.8. TypeScript stays on 6 for the tooling API and on 7.0 for `tsc`.
- The documents pipeline builds its Markdown extensions per render (goldmark 2.1.14 deprecated the shared ones); rendered output is
  unchanged.

## 2.2.5

Fixes from a review of the code, webhooks that are not lost, and every dependency brought up to date. No configuration change is needed
to upgrade. The web UI is now built and run on Node.js 26 (the LTS) in the images and in CI; a host that runs the release bundle can keep
Node.js 24 (the bundle starts and serves under it) but the documentation names 26.

- Every dependency is current: go-github v93, go.yaml.in/yaml v4, oapi-codegen 2.8.1, Next.js 16.4.3, Vitest 5.1, Playwright 1.65,
  jsdom 31, Tailwind 4.3.5, ESLint 10.14, pnpm 12.17, GoReleaser 2.19 and the rest of the Dependency Dashboard; Node.js 24.22.1 and later
  carry security fixes (HTTP agent, TLS IP checks, permission model, brotli, OpenSSL 3.5.9, undici). TypeScript stays on 6 for the tooling
  API and on 7.0 for `tsc`; Renovate holds 7.1.
- The Guide's pictures show the default work that counts (every kind, automation included), the pull requests tile and the side panel.
- New issues and pull requests show without waiting for the next poll: a run a webhook caused that found nothing new reads again after 30 seconds (GitHub's
  listing can lag the event), and the catalogue's listing every 5 minutes asks for a read when a repository's count of open issues and
  pull requests moved, so a missed webhook costs minutes, not the 15-minute TTL. The event stream sends a `ping` event every 25 seconds;
  the page opens a silent or closed stream again and when the tab or the network comes back.
- A webhook is not lost: every event is parked in the store before GitHub is answered (503 when it cannot be kept) and passed on again
  after a restart until the jobs it asked for have run; with an App, the App's webhook delivery log is read every two minutes and every delivery the portal did not answer
  with a success (it was down, restarting or full) is acted on. New result labels `unavailable` and `caught_up` on
  `scp_webhook_deliveries_total`.
- The dashboard and `/present` refresh their data at most once in 15 seconds (a run of snapshots was one refresh each) and no longer
  prefetch every repository link.
- GitHub's conditional-request cache is keyed by credential, not by an installation's hourly token, and is bounded by bytes.
- An instance that cannot read the cluster's state map does not take the lead.
- Text from repositories loses every invisible format character (the tag block spells invisible text); the sitemap, `robots.txt`
  and `opensearch.xml` are not cached by shared caches when `server.public_url` is not set; the webhook receiver reads at most four
  deliveries over 1 MiB at once (`busy`, 503); an OIDC `email_verified` of the string `"false"` is not verified; a tool schema whose constraints
  could not be enforced is refused.
- The web UI: an agent's `open_page` cannot reach `/admin` or `/auth` by a percent-encoded path; an announcement being edited is
  found by its id, not its place; the assistant's answer is read out once whole and a broken stream frame is skipped; a screen's
  wake lock is held once; full screen that the browser refuses is not an error; the search shortcut `/` goes to the page's own search
  first.
- A sign-in lasts at most 7 days however it is renewed (or `auth.session_ttl` when that is longer): the groups and the verified email
  come from the provider at sign-in, so someone it removed is let go. An index that cannot be built keeps its last good value and
  the rest of a publish goes out. The sitemap is built once per snapshot, whatever Host a request names.

## 2.2.4

The presentation page, open pull requests and issues, announcements and commit diff stats; the dashboard's side panel in sections.
No configuration change is needed to upgrade. Pull requests and issues need the GitHub App's *Pull requests* and *Issues* read
permissions and their events (the operator guide, section 1): without them the portal says what is missing and keeps working, or
`features.pulls: false` and `features.issues: false` switch them off.

- `/present`, the fleet on one screen for a tablet, a desk or a wall: the status strip with the data's age, the fleet by health,
  products as tiles that fold the good ones, and a side column. Profiles (Handheld portrait, Handheld landscape, Desk, Wall) each
  keep their own column width; on a wall it stays awake, reloads and shifts against burn-in. An admin designs it as the dashboard.
- Open pull requests and whose turn each is (`/pulls`, `GET /api/v1/pulls`, the tool `open_pull_requests`, search `in:pull`, the
  pulls badges); open issues and where each stands (`/issues`, `GET /api/v1/issues`, the tool `open_issues`, search `in:issue`).
  The health findings for reviews waiting and old updates come from them.
- The dashboard's side panel and `/present`'s side column in sections: the latest commits, the pull requests (newest first) and the
  issues (newest first), sharing the height equally, each scrolling on its own and loading more at its end, each hidden by its
  button; all hidden, the panel folds to a rail. Each screen remembers its choice.
- Announcements at the top of the dashboard and `/present`, from `scp.yaml` and written by admins in the pages, with QR codes on
  `/present`.
- Commit diff stats (+a −d and a five-block bar) in every commit list, from GitHub's GraphQL API, for the last 90 days.
- The portal says what GitHub must grant each feature: `GET /api/v1/prerequisites`, the warnings on the admin status and the pages.
  It stays read-only towards GitHub, and the App subscribes only to the events that change what it shows.
- `features.pulls` and `features.issues` (default on) switch a feature off everywhere: pages, API, tools, search, badges, sitemap.
- The public cannot tell a portal with private repositories from one without them.
- The Guide covers the presentation page, pull requests, issues and trying WebMCP in Chrome; the operator guide, the API guide and
  the operators' plugin follow.

## 2.2.3

Search across what the portal holds. No configuration change is needed to upgrade: the index is built in memory from what the portal
already reads, never stored and never asked of GitHub.

- Search repositories, groups, the sections of READMEs and `docs/` documents, manifests with their dependencies, published packages
  and default-branch commits, best first, from the viewer's view: a public reader never finds a private repository. Not source code.
- The query: every word (the last one also as a prefix), "phrases", -exclusions, OR, and the filters `in:`, `group:`, `repo:`,
  `lang:`, `topic:`, `lifecycle:`, `author:`, `kind:` (hand, ai, automation), `dep:`, `before:` and `after:`; a word nothing holds is
  searched with one letter changed, and the page says so.
- `/search` (rendered on the server, so it works without JavaScript) shows the hits by kind with their counts and snippets, the
  matches marked; a document opens at the section that matched. The header has a search box: `/` or Ctrl/⌘ K, and it suggests
  repositories, groups and documents by name as you type.
- `GET /api/v1/search` and `/api/v1/search/suggest`; agents get the tool `search` (the assistant, MCP and WebMCP) in place of
  `search_repositories`. `/opensearch.xml` lets a browser add the portal as a search engine; robots.txt keeps crawlers off `/search`.
- The admin status shows the index (`search`: segments, terms, bytes, build time); `scp_search_requests_total` and
  `scp_search_duration_seconds` are new metrics.
- The Guide has a section on searching; the API guide, the operator guide, the security model and the operators' plugin cover it.

## 2.2.2

AI agents can ask the portal what a person would, through the portal's own read-only tools. No configuration change is needed to
upgrade: nothing new to set, and no language model runs on the portal for it.

- One tool catalogue for agents: search the repositories, list and read groups, read a repository or a document, the recent
  commits (with the work that counts), a repository's health and its published packages. The assistant (`/ask`) uses it too. Every
  call answers from the caller's view, and what it returns is framed as untrusted repository text.
- MCP at `/mcp` (Model Context Protocol, Streamable HTTP, stateless, protocol 2026-07-28 and earlier) for Claude Code and other MCP
  clients: `claude mcp add --transport http scp https://portal.example.org/mcp`. Without a token the public view; with an access
  token (`auth.tokens`) the token's. A browser's session cookie is never an identity there, and a page of another site is refused.
- WebMCP (`document.modelContext`, the W3C Community Group draft): in a browser with WebMCP the pages offer the same tools to the
  browser's agent with the reader's own sign-in, plus `current_page`, `open_page` and `set_preferences`. Without WebMCP nothing
  changes.
- `GET /api/v1/tools` lists the tools and `POST /api/v1/tools/{name}` calls one; `/llms.txt` names `/mcp`.
- The Guide has a section on agents; the operator guide (2.6), the API guide and the security model cover them.

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
