# Search engines
Status: M13, in progress (plan.md section 4): items 1 to 3 and the sitemap gate of 8 are built; 4 to 7 and Lighthouse follow. Binding order: ../plan.md > this note.
Learned from krantho (its `krantho-site` and `packages/seo*`), adapted to SCP's model:
Go owns the data and every response header, the Next.js server renders pages from the snapshot.

## What the web UI already gives
Server-rendered HTML for every page, readable without JavaScript; per-page title, description and canonical from `X-Scp-Origin`;
`/sitemap.xml` (dashboard, commit log, groups, repositories) and `/robots.txt`; `noindex` for a private viewer; `/llms.txt`,
`/llms-full.txt` and Markdown answers; a 404 status for unknown pages.

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
   shows (`shared/breadcrumbs.tsx`). No `SearchAction` until the portal has server-side search.
3. Sitemap (built, Go's `llms`): the dashboard, the commit log, groups, repositories and their documents (not the README, which is
   the repository's page, nor hidden ones) for the viewer's view; `lastmod` a repository's newest commit, a group's newest member
   commit, a document's repository's (per-file history is not read), the dashboard's and commit log's the newest of all; an index of
   `/sitemaps/{n}.xml` past 50,000 URLs; absolute URLs from `server.public_url`, else the request's origin. Served with
   `public, max-age=300, s-maxage=3600, stale-while-revalidate=86400` and a strong ETag, as `robots.txt` (Go's too).
4. Open Graph and social cards: `openGraph` and `twitter: summary_large_image` on every page; one `next/og` route, `/og/...`, that
   draws a branded card per repository and group (name, description, the health badges, activity strip) from the snapshot, allow-
   listed to known addresses, `Cache-Control: public, max-age=86400`; its font ships with the server, not as a page font.
5. App manifest (`app/manifest.ts`), `themeColor` per colour scheme, PNG and Apple touch icons beside the SVG.
6. Caching for crawlers: public pages cached by Next.js keyed on the snapshot version (`'use cache'` with `cacheTag` per page and
   the version; Go calls a loopback revalidate endpoint when it publishes), so a crawler gets a warm page and an edit shows at once.
   Private viewers are never cached.
7. IndexNow: when a snapshot changes repositories or documents, Go submits their URLs (with `server.public_url` set and a key file
   served at `/{key}.txt`); `seo.indexnow_key_file` turns it on.
8. Quality gates in CI: Playwright reads the sitemap and checks for each page a 200, its canonical equal to its URL, one `h1`, a
   `<lastmod>`, a JSON-LD graph with its page node and the trail it shows (built: `e2e/seo.spec.ts`), and a graph that validates against a pinned Schema.org vocabulary and Google's required properties for the
   types used; Lighthouse CI with SEO and accessibility at 100 on the dashboard, a group and a repository.

## Borrowed from krantho, and what SCP does differently
- Borrowed: the route of truth, the per-page `@graph` with breadcrumbs, git-derived `lastmod`, the branded OG route, `s-maxage` with
  stale-while-revalidate on machine-read files, the post-deploy structured-data check, IndexNow, llms.txt (SCP has it).
- Different: krantho invalidates on admin edits; SCP's content changes with the snapshot, so its version is the cache key. krantho
  sets no CSP; SCP keeps its nonce CSP and puts the nonce on JSON-LD and every script. krantho deploys to Vercel; SCP stays one
  process behind Go, so caching is Next.js's own cache handler, not a CDN.

## UI notes from krantho (for the design pass, not SEO)
Fluid type with `clamp()`, motion tokens dropped to 0 ms under reduced motion, `text-wrap: balance` on headings, `content-visibility`
for long lists, the theme switch rendered on the server with its state in CSS.
