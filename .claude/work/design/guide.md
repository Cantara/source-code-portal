# The guide and the operators' plugin (G1)

Two ways in for newcomers: the portal's own *Guide* page for its readers, and a Claude Code plugin for the people who run a
released portal (P-44, P-45).

## The guide page

`/guide` ("Guide" in the header, the page titled *Getting started*) explains the portal in two parts, the readers'
first and the operators' after, in sixteen numbered sections. **Part 1, Read the portal**: signals not lists, the telemetry, the fleet
map, a product card (its picture numbered row by row beside the explanations), health and findings (the score rings of the three
levels, the legend of the findings: each kind's icon on its severity's tile, what it means, when it is found and what it costs; the
severities), the work that counts (the three kinds, Settings), tables, commits, pull requests and issues (the side panel, Commits, Pull requests, Issues, the repository page),
search (the box, the query language), on a wall (the presentation page, its addresses, the home screen) and make it yours
(every preference). **Part 2, Run a portal**, in the order of the work: connect GitHub (the read-only GitHub App: why, the webhook secret, the App's
permissions and webhook, its key and installation; a personal access token only to try one), run your own (the App in `scp.yaml`,
the groups, check, run), sign-in and private repositories (what an anonymous,
a signed-in and an administrator viewer see, that the server filters every answer, access tokens first, in three steps (the
secrets, the configuration, handing them out and signing in), then single sign-on: any OpenID Connect provider, GitHub accounts through Dex), packages and private registries (what is read
per language, the release chip, private registries with their credentials), agents (MCP clients at `/mcp` with or without a
token, browser agents through WebMCP, the read-only tools, and *Try WebMCP in Chrome*: the flag, the Model Context Tool Inspector, a Gemini key from Google AI Studio, a tool by hand and a prompt, beside the inspector's picture), and options and documents (the
options, the documents and the plugin).

- **Contents**: one `nav` "Guide" grouped by part; chips under the hero on a narrow screen, a sticky "On this page" list beside the
  page from `lg`. The hero leads to both parts.
- **Pictures at their own size**: an `<img>` has the capture's CSS pixels as its width and is never drawn larger (`max-w-full`), a
  figure fits its picture; only a narrow screen makes them smaller. The prose keeps a reading width (`max-w-prose`).

- **Pictures are screenshots of a made-up catalogue**: `cmd/scp` `TestServeForGuide` (organisation `acme`, products Payments,
  Identity and Developer tools, people named *Example*, dates relative to now) serves it on `GUIDE_SCP_ADDR`. Its cards show every
  signal: work by hand and with AI, a coding agent, automation, a sweep, dormant, quiet and unlicensed repositories, open issues,
  unreleased changes, a product to watch and an archived repository. No picture ever shows a real portal.
- `.claude/skills/guide-page/capture.js` takes them with the Playwright MCP tool, in both themes at twice the pixels, and measures
  where the product card's rows are; `publish.mjs` writes WebP (`cwebp -q 90`) to `web/public/guide/` and the sizes and callouts to
  `web/src/features/guide/shots.json`. The skill `guide-page` holds the procedure and the design rules.
- **The facts are imported where they can be** (the fleet map's ranges from `entities/repo/health`, the kinds from
  `lib/preferences`, the level names from `entities/health/health`); the findings' kinds, icons and severities from `entities/health/kinds` (drawn without client JavaScript, as the cards draw them); when each is found and its points copy `internal/health/health.go`.
- A server component without data or client JavaScript; each picture is two `<img>` (`dark:hidden`, `hidden dark:block`, lazy, sized
  from `shots.json`). The numbered card is one SVG holding both pictures and the numbers in a gutter, since the CSP forbids inline
  styles. The run commands name the running release (`SCP_VERSION`), `<version>` on a development build.
- Checks: `web/src/app/guide.test.tsx` (one h1, a contents entry per section, every picture known in both themes, a number per
  row, the version in the commands); `e2e/smoke.spec.ts` (axe in both themes; the pictures of the shown theme load; every contents
  entry has its section). Not in the sitemap: it is the same for every portal.

## The operators' plugin

The repository is a Claude Code plugin marketplace (`.claude-plugin/marketplace.json`) with one plugin, `plugins/scp` (its own
version, semver), holding the skill `run-scp`: from someone's wishes to a configuration that passes `scp config check`, a
deployment (Docker Compose with Caddy, Kubernetes, a binary under systemd) and a portal that answers; upgrades, rotation and
troubleshooting. It works from the release, not the source: it settles the version first and reads that tag's `docs/` from GitHub,
runs the release's own `scp` (the image), keeps secrets as files it never reads, and asks before acting on a running portal.
`references/essentials.md` and `references/deploy.md` summarise the documents for when they cannot be fetched. The skill and its
references follow `docs/` in the same commit; `.claude/skills/configure-scp` stays the owner's skill for this repository's checkout.
Install: `/plugin install scp --marketplace Cantara/source-code-portal`.
