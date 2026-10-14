# Search engines (M12)
Status: planning note for M12, 2026-10-15; nothing here is built yet. Binding order: ../plan.md > this note > ../reference/.
Learned from krantho (`/Users/overanheim/Code/descoped/krantho`, its `krantho-site` and `packages/seo*`), adapted to SCP's model:
Go owns the data and every response header, the Next.js server renders pages from the snapshot.

## What M11 already gives
Server-rendered HTML for every page, readable without JavaScript; per-page title, description and canonical from `X-Scp-Origin`;
`/sitemap.xml` (dashboard, commit log, groups, repositories) and `/robots.txt`; `noindex` for a private viewer; `/llms.txt`,
`/llms-full.txt` and Markdown answers; a 404 status for unknown pages.

## Scope (M12)
1. One route of truth. `web/src/lib/paths.ts` is the only builder of page paths; canonical URLs, the sitemap, JSON-LD `@id`s and
   breadcrumbs all use it, and a test compares its output with the addresses Go's `llms` writes, so the two never drift.
2. Structured data: one JSON-LD `@graph` per page from a server component, serialised with `<` escaped and the page's CSP nonce
   (a data block is not executed, the nonce keeps the policy uniform). Site level `WebSite` and `Organization` (the GitHub
   organisation, `sameAs` its GitHub URL); a repository page `SoftwareSourceCode` (`codeRepository`, `programmingLanguage`, `license`
   as an SPDX URL, `dateModified` from the newest commit, `version` from the latest release); a group page `CollectionPage` with an
   `ItemList` of its repositories; a document `TechArticle`; every page a `BreadcrumbList` built from the same path parts it renders.
   No `SearchAction` until the portal has server-side search.
3. Sitemap: documents added; `lastmod` on every URL (a repository's newest commit, a group's newest member commit, a document's
   commit); a sitemap index once the URLs pass 50,000. Served with `s-maxage=3600, stale-while-revalidate=86400`, as `robots.txt`.
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
   `<lastmod>`, and a JSON-LD graph that validates against a pinned Schema.org vocabulary and Google's required properties for the
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
