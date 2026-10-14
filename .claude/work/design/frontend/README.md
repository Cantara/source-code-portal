# Web UI design brief
Status: input for a design pass (Claude Design), 2026-10-14. Binding order: ../../plan.md > ../web.md > this brief.

## What it is
Source Code Portal is an organisation's mission control for its GitHub repositories (here: Cantara, 134 repositories in 11
products). Its users are engineers and tech leads who want to see, at a glance and at full density, which products and repositories
are alive, released, licensed and healthy, then drill into a repository's README, documents, technology and commits.

## Direction (owner)
- Full width, no centred column: maximum density, every product as a card holding its repositories.
- Health badges on every repository (hard requirement): activity and age, latest release, licence, open issues, language.
- Modern and futuristic ("sci"), dark first, with an equal light theme.

## Pages and data
| Page | Data (fields from `api/openapi.yaml`) |
|---|---|
| Dashboard `/` | groups (`id`, `name`, `description`, `repoCount`, `lastActivity`, `defaultRepo`), all repositories (below), freshness counts, latest commits |
| Group `/groups/{id}` | the group and its repositories; latest commits of the group |
| Repository `/repos/{owner}/{name}` | repository, descriptor (`title`, `lifecycle`, `owners`, `tags`, `links`), README HTML, documents, technology and dependencies, uses and used by, latest commits, SVG badges |
| Document, commit logs | rendered Markdown with an outline; paged commit lists with filters |

A repository: `name`, `owner`, `description`, `license` (SPDX), `openIssues`, `stars`, `pushedAt`, `lastCommitAt`, `release`
(`tag`, `publishedAt`), `language`, `topics`, `archived`, `visibility`, `freshness` (fresh, stale, error).

## Constraints a design must keep
- Server-rendered Next.js with Tailwind 4 classes only: no inline styles (the CSP has no `'unsafe-inline'`), no external fonts or
  images (system font stacks; images only from the portal).
- WCAG 2.2 AA in both themes (axe runs on every page): text contrast 4.5:1, colour never the only signal, visible focus, 24 px targets,
  reflow at 320 px, reduced motion honoured.
- Every page readable without JavaScript.

## Current state
`current/`: the redesign as built (dashboard, group, repository; dark and light, 1600x1000). `v1-java/`: the Java portal's dashboard and
group page for reference. Tokens and component classes live in `web/src/styles/index.css` (`.panel`, `.chip`, `.led`, `.cell`, level
tokens); health logic in `web/src/lib/health.ts`, components in `web/src/shared/health.tsx`.
