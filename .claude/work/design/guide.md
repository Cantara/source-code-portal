# The guide and the operators' plugin (G1)

Two ways in for newcomers: the portal's own *Guide* page for its readers, and a Claude Code plugin for the people who run a
released portal (P-44, P-45).

## The guide page

`/guide` ("Guide" in the header, the page titled *Getting started*) explains how to read the portal and how to run one, in eight
numbered sections: signals not lists, the telemetry, the fleet map, a product card (its picture numbered row by row beside the
explanations), health and findings (the levels and what each finding costs), the work that counts (the three kinds, Settings),
tables and commits, and running a portal (access, groups, check, run, the options, the documents and the plugin).

- **Pictures are screenshots of a made-up catalogue**: `cmd/scp` `TestServeForGuide` (organisation `acme`, products Payments,
  Identity and Developer tools, people named *Example*, dates relative to now) serves it on `GUIDE_SCP_ADDR`. Its cards show every
  signal: work by hand and with AI, a coding agent, automation, a sweep, dormant, quiet and unlicensed repositories, open issues,
  unreleased changes, a product to watch and an archived repository. No picture ever shows a real portal.
- `.claude/skills/guide-page/capture.js` takes them with the Playwright MCP tool, in both themes at twice the pixels, and measures
  where the product card's rows are; `publish.mjs` writes WebP (`cwebp -q 90`) to `web/public/guide/` and the sizes and callouts to
  `web/src/features/guide/shots.json`. The skill `guide-page` holds the procedure and the design rules.
- **The facts are imported where they can be** (the fleet map's ranges from `entities/repo/health`, the kinds from
  `lib/preferences`, the level names from `entities/health/health`); the penalty table copies `internal/health/health.go`.
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
