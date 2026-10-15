# Search engines
Status: M13, built (plan.md section 4). Binding order: ../plan.md > this note. SCP is a technical site: it gives search engines and
link previews what they read, nothing more (P-30).

## What every page gives
Server-rendered HTML readable without JavaScript; its title, description and canonical address (from `X-Scp-Origin`); `noindex`
for a private viewer; a 404 status for unknown pages; `/llms.txt`, `/llms-full.txt` and Markdown answers.

## Scope
1. One route of truth (built, P-29). A repository's page is its lower-case address. `web/src/lib/paths.ts` and Go's
   `internal/snapshot/pages.go` (`RepoPage`, `GroupPage`, `DocPage`, `Segment` as `encodeURIComponent`) are the only builders of
   page paths, held to the same cases (`web/src/lib/paths.cases.json`); links, canonicals, the sitemap, the documents' rewritten
   links, llms, the assistant's sources, JSON-LD `@id`s and breadcrumbs use them.
2. Structured data (built): one JSON-LD `@graph` per page (`lib/seo.tsx`: `JsonLd` with `<` escaped and the page's CSP nonce,
   rendered by `page()` from the route's `structured`). Every page the site's `WebSite` and its own page node (`WebPage`, the
   dashboard and a group `CollectionPage`); a repository `SoftwareSourceCode` (`codeRepository`, `programmingLanguage`, `license`
   as an SPDX URL, `dateModified` from the newest commit, `version` from the latest release, `keywords` from its topics, `author` the
   owning GitHub account as an `Organization`); a group's `mainEntity` an `ItemList` of its repositories; a document `TechArticle`
   part of its repository's code; every page but the dashboard a `BreadcrumbList` from `lib/crumbs.ts`, the trail the page header
   shows (`shared/breadcrumbs.tsx`): the pages above it, never the way the reader came. A repository sits under the first of its
   groups in configuration order (`RepoDetail.group`), under the dashboard when it is in none; its commits and documents under it.
3. Sitemap (built, Go's `llms`): the dashboard, the commit log, groups, repositories and their documents (not the README, which is
   the repository's page, nor hidden ones) for the viewer's view; `lastmod` a repository's newest commit, a group's newest member
   commit, a document's repository's (per-file history is not read), the dashboard's and commit log's the newest of all; an index of
   `/sitemaps/{n}.xml` past 50,000 URLs; absolute URLs from `server.public_url`, else the request's origin. Served with
   `public, max-age=300, s-maxage=3600, stale-while-revalidate=86400` and a strong ETag, as `robots.txt` (Go's too).
4. Shared links (built): every page's Open Graph and X `summary` metadata (`social` in `lib/seo.tsx`, applied by `page()`): its title,
   description and canonical address, for the previews chat tools and sites draw.
5. Gate (built, `e2e/seo.spec.ts`): Playwright reads the sitemap without JavaScript and checks for each page a 200, its canonical
   equal to its URL, one `h1`, a `<lastmod>`, and a JSON-LD graph with its page node and the trail it shows.
