# Web UI
Status: as built. Binding order: ../plan.md > this note.

## Scope
A read-only web UI rendered on the server with Next.js for the pages: dashboard, group, repository (README), docs viewer, commit logs
(all, group, repository) and a 404 page. Every page arrives as complete HTML, readable
without JavaScript, with its own title, description and canonical URL; `/sitemap.xml` and `/robots.txt` list the pages. After load the
page is a React application that reads only `/api/v1`, shows freshness everywhere and follows `/api/v1/events`.

## Decisions (bullets with one-line reasons)
- Stack: Next.js 16 (App Router, `output: "standalone"`), React 19, TypeScript, Tailwind 4 (PostCSS), TanStack Query, `vitest`,
  Playwright, pnpm. The few components (button, card, badge, alert) are written in `components/ui/` in the shadcn style: plain elements.
- Go stays the one public entry point. `scp serve` runs the Next.js server as a child process (`node server.js` from `web.dir`) on a
  loopback port and proxies every page path to it; API, badges, images, llms, Markdown and the webhook stay in Go. One port, one
  process to deploy, one place for headers, access and logs.
- The viewer travels with the page: Go adds `X-Scp-Token` (random per process) and `X-Scp-Viewer` (`private` or `public`) to the
  proxied request, the Next.js server sends both on its API calls, and Go believes `X-Scp-Viewer` only with the matching token (constant
  time). A private viewer's page is rendered from the full snapshot, a public viewer's from the public view; the browser never sees the token.
- The Next.js server reaches the API at `SCP_API` (the public listener on loopback, set by Go) and sends the page's `X-Request-Id`, so
  one request id follows a page through every API call.
- Each page is a route module of its feature (`route.tsx`: `load`, `metadata`, `render`, the `Route` type of `lib/page.tsx`); the
  `app/` page file is `page(route)`. `load` fills a per-request `QueryClient` with the same `queryOptions` factories the browser uses
  and `page` hands it over with `HydrationBoundary`; the views are client components that read those queries. One data path, no second
  set of components.
- Query factories take the API client as their first argument: `serverApi()` (per request, `server-only`) on the server, `api` (the
  browser's) in client components. Query keys (`lib/api/queries.ts` `keys`) do not include the client, so server data hydrates the
  browser's cache.
- A page's main query decides the status (`lib/load.ts`): a 404 problem calls `notFound()` (Next.js answers 404 with the not-found
  page), a renamed repository calls `permanentRedirect` (308); other failures render the page without data and the browser retries.
- Metadata through `generateMetadata` on the same per-request loader (`React.cache`): title `"<page> · Source Code Portal"`,
  description from the descriptor or repository description, canonical from the origin Go sends (`X-Scp-Origin`: `server.public_url`,
  else the request's scheme and host). Robots: index everything public; a private viewer's pages carry `noindex`.
- Nonce CSP: Go makes a nonce per page request, sets the page CSP on the response and on the proxied request (Next.js reads its nonce
  from that header and puts it on every script it emits). Components use classes only, so `style-src` needs no `'unsafe-inline'`.
- Every page is dynamic (it reads request headers); the `/_next/static` files are immutable. Next.js compresses its responses; Go's
  gzip leaves a response with a `Content-Encoding` alone.
- Typed client: `openapi-typescript` generates `web/src/lib/api/schema.d.ts` from `api/openapi.yaml` (committed, drift-checked);
  `openapi-fetch` wraps fetch; one `unwrap()` turns every failure into a `ProblemError`.
- Layers, bottom up: `lib`, `components/ui`, `shared` (generic UI), `entities` (repository and commit pieces), `features`, `app`. A
  layer imports only those below it (through `@/` or `./`, dynamic imports included); a feature never imports another feature (mirror
  of the Go rule). ESLint enforces it. Only `app/` composes: the repository page takes the panels other features contribute
  (`Panel` in `lib/page.tsx`; `app/repos/[owner]/[name]/panels.ts` lists the manifests panels).
- One component, `shared/safe-html.tsx`, renders server-sanitised HTML.
- Development: `next dev` on 127.0.0.1:3000 rewrites `/api`, `/badges`, `/images`, `/llms*.txt` and the `.md` addresses to `SCP_API`
  (default `http://127.0.0.1:9090`, a running `scp serve`): hot reload without Go in front.

## Structure and routes
```
web/
  package.json pnpm-lock.yaml next.config.ts postcss.config.mjs tsconfig.json eslint.config.js vitest.config.ts playwright.config.ts
  public/theme-init.js favicon.svg
  scripts/bundle.ts                                  assembles web/bundle (standalone server, static files, public/) and checks the budget
  e2e/global-setup.ts fixtures.ts smoke.spec.ts live.spec.ts seo.spec.ts   against cmd/scp/e2e_test.go (fake GitHub, Go, the Next.js child)
  src/
    app/       layout.tsx (html, head, shell, metadata), providers.tsx (client: QueryClient, snapshot sync, focus), shell.tsx,
               not-found.tsx, error.tsx, sitemap.ts, robots.ts, one page.tsx per route below (page(route) of its feature),
               repos/[owner]/[name]/panels.ts, views.test.tsx
    features/  dashboard/ groups/ repos/ docs/ commits/ (each route.tsx, view.tsx, api.ts with its queries where it has its own)
               manifests/ (api.ts, panels.tsx, view.tsx: the technology and dependency panels)  status/ (sync.ts, banners.tsx)
    entities/  repo/ (health.ts, health-badges.tsx, visibility-badge.tsx)  commit/ (commit-list.tsx)  entities.test.tsx
    components/ui/                                  button card badge alert
    shared/    safe-html freshness-badge relative-time page-header problem-alert empty-state initials-avatar panel-boundary, shared.test.tsx
    lib/       api/{schema.d.ts,client.ts,server.ts,queries.ts,snapshot.ts}  page.tsx load.ts query.ts paths.ts connection.ts clock.ts
               theme.ts format.ts build.ts docs.ts cn.ts, lib.test.ts
    styles/    index.css (tokens, light and dark)  docs.css (prose and Chroma classes for both themes)  chroma.css (generated)
    test/      setup.ts server.ts (MSW, unmocked request fails the test) navigation.ts link.tsx (next/navigation and next/link for tests)
```
| Path | Feature | Shows |
|---|---|---|
| `/` | dashboard | telemetry strip (repositories, active, released, licensed, open issues, in sync; activity legend), one card per group holding every member repository with its activity light and health badges, newest change first (large groups span columns), the repositories in no group, latest commits in a side rail |
| `/groups/{group}` | groups | group header, repository table with activity and health badges (filter by name, sort), latest commits |
| `/groups/{group}/commits` | commits | commit log scoped to the group |
| `/repos/{owner}/{name}` | repos (+ manifests panels) | descriptor, SVG badges, rendered README, technology (uses, used by), dependencies per manifest, latest commits |
| `/repos/{owner}/{name}/commits` | commits | commit log of the default branch |
| `/repos/{owner}/{name}/docs/{path}` | docs | rendered doc with a file list and heading outline; `{path}` is the repository path without its extension |
| `/commits` | commits | commit log across all repositories, filter by group or repository |
| `/sitemap.xml`, `/robots.txt` | app | the dashboard, the commit log, every group and repository of the viewer's view, absolute |
| anything else | app | 404 page (also for an unknown group or repository from a 404 problem) |

A document's own leading `h1` is dropped on the page, whose `h1` already names it.

Look: full width (no centred column), dense, dark first. Surfaces are `.panel` (lit top edge, glow on hover and focus within) over a
faint grid backdrop (`.cosmos`). Health is drawn in HTML, not with badge images: a repository is classified by its
newest commit (else its last push) as active (30 days), recent (6 months), quiet (2 years), dormant or archived (`entities/repo/health.ts`);
`entities/repo/health-badges.tsx` renders the light (`.led`), the chips (`.chip`: age, latest release, licence, open issues, language, each with its meaning in text)
and the per-group strip (one `.cell` per repository). The level colours are tokens (`--active`, `--active-fg`, ...) with AA text
contrast in both themes. The header shows whether the live stream is open (`LiveIndicator`).

## Behaviour (numbered)
1. Queries: `staleTime` 10 s, `gcTime` 5 min, retry network/429/5xx at most twice (honour `Retry-After`), refetch on window focus,
   previous data kept on error.
2. Snapshot sync: one `useSnapshotSync()` in the providers opens an `EventSource` on `/api/v1/events`; each `snapshot` event replaces
   the snapshot query's data. While the stream is not open it polls `GET /api/v1/snapshot` every 30 s while the tab is visible. A
   different `version` (equality) invalidates every other query; active ones refetch. Readiness `warming` shows a banner.
3. Commit logs page with "Load more" (cursor, 50 rows); the server renders the first page. With more than one page loaded a snapshot
   change leaves the log as it is; a "New commits" button refetches and moves focus to the first row.
4. Freshness: every view shows a `FreshnessBadge` (icon plus text). Relative ages re-render on one 30 s ticker, paused while hidden. The
   server passes its instant (`renderedAt`, on the 30 s grid) to `NowProvider`, so hydration renders against the same clock; then the
   ticker takes over. A view reads the clock once (`useNow`) and hands `now` to the entity components.
5. Failure is data: a failed refetch keeps the data on screen; one polite banner says "Cannot refresh right now. Showing the last data
   that loaded." after network, 429 or 5xx failures and clears on the next success.
6. Problems: `ProblemAlert` titles come from `code` and always show the request id. Each independent panel has its own error boundary
   (`PanelBoundary`, with Retry); `error.tsx` catches the rest and `global-error.tsx` a failing layout.
7. Deploy skew: the bundle records its build id in `BUILD` (`scripts/bundle.ts`); `scp` sends the running bundle's as `X-SCP-Build`
   (`webui.Server.Build`, read at every start of the web UI server), and an API response whose build differs from the page's shows
   "A new version is available" with Reload. The binary's own build does not take part, so the two can be upgraded apart;
   Next.js's `deploymentId` (the build id) makes a navigation across a deployment a full load.
8. `SafeHtml` sets the API field `html` with `dangerouslySetInnerHTML`, memoised on the string. Lint allows it in this component only.
9. Theme: `system | light | dark` in `localStorage` key `scp-theme`. The blocking `/theme-init.js` in `<head>` sets the `dark` class
   and `colorScheme` before first paint (`<html suppressHydrationWarning>`); `lib/theme.ts` resolves the same way, and while `system`
   is chosen it follows the system preference as it changes (`matchMedia` change events).
10. CSP (Go sets it): `default-src 'none'; script-src 'self' 'nonce-<n>'; style-src 'self' 'nonce-<n>'; img-src 'self';
    font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none';
    object-src 'none'`, plus the `Permissions-Policy` of http-api.md.
11. Accessibility (WCAG 2.2 AA): skip link to `#main`, landmarks, one `h1` per page, focus to `h1` after a client navigation (Next.js
    announces the title), visible focus ring, targets >= 24 px, real `<table>`s with keyboard sortable headers, reflow at 320 px,
    `prefers-reduced-motion` honoured.
12. URLs are state: filters and sort live in search params (the server renders them too), written with `history.replaceState` (no
    server round trip) and read back from the URL by the inputs; repositories are `{owner}/{name}`, doc paths
    a catch-all encoded per segment (`lib/paths.ts`).
13. Commit graph: where the commits share one history (the repository page's "Latest commits" and the repository's commit log) they
    draw as a git graph, like `git log --graph`; the merged feeds (dashboard, group, all repositories) stay a plain list, since their
    commits are unrelated. `entities/commit/graph.ts` lays the rows out from each commit's `parents`, newest first: a commit takes the
    lane that expects it (else the first free one), every other lane expecting it converges into it, its first parent continues the
    lane and each further parent opens a lane (or joins the one already expecting it); freed lanes are reused, lanes never shift. Each
    row draws its own `aria-hidden` SVG (lanes 12 units apart, the node on the subject line, `h-13` with the width from the viewBox, so
    it scales with the text and needs no style attribute under the CSP): lines in from above, the node (a ring for a merge, filled
    otherwise), lines out to the parents' lanes, pass-through lanes straight. Lines to parents outside the loaded rows run to the bottom
    dashed. Colours are six `--lane-*` tokens per theme (at least 3:1 against the card), the first lane `--primary`; at most 8 lanes
    are drawn, a commit beyond them sits in the last. Rows have a fixed height: the subject is one line, truncated with the full text in
    `title`. No day headers: the day depends on the viewer's time zone, which the server render does not know.

## Build and serve
- `pnpm build`: `next build` with `BUILD_ID` (git SHA, `dev` locally) as `deploymentId` and `NEXT_PUBLIC_BUILD_ID`, then `scripts/bundle.ts`: copies
  `.next/standalone`, `.next/static` and `public/` into `web/bundle`, writes `BUILD` (the `BUILD_ID`) last, and fails when the gzip JS under `/_next/static` exceeds 300 KB,
  the CSS 25 KB, or any font is present. `web/bundle` is platform-independent JavaScript (`sharp` is not traced).
- `web.dir` (the bundle directory) and `web.node` (default `node`): `scp serve` starts `node server.js` with `PORT` (a free loopback
  port), `HOSTNAME=127.0.0.1`, `NODE_ENV=production`, `SCP_API` and nothing else from its environment; output goes to the log
  (`component=web`). It restarts the child after an exit (1 s doubling to 30 s) and at once when the bundle is replaced (its `BUILD` file's
  content or modification time changes, checked every 2 s: a running server cannot load another build's files; Next.js's own
  `.next/BUILD_ID` is the same for every build), stops it at shutdown after the listeners drained,
  and `/readyz` waits for the child's first answer. Without `web.dir` page paths answer 503 with a short text naming `/llms.txt` and
  the API.
- Proxy (`httpapi.Pages`): `httputil.ReverseProxy` to the child; it sets the CSP and `Permissions-Policy`, the `X-Scp-*` headers and
  `X-Forwarded-*`, removes the identity headers, keeps the response's caching headers; an unreachable child answers 503 with
  `Retry-After: 5`.
- Release: `scp-web_<version>.tar.gz` (the bundle) beside the binaries, in the signed checksums; the image is
  `distroless/nodejs24-debian13` with `/scp`, the bundle in `/web`, `SCP_WEB_DIR=/web` and `SCP_WEB_NODE=/nodejs/bin/node`.

## Tests
- `vitest` + Testing Library (jsdom) + MSW typed from `paths`, any unmocked request fails (`app/views.test.tsx`, `entities/entities.test.tsx`,
  `shared/shared.test.tsx`, `lib/lib.test.ts`): the views with test `next/navigation` and `next/link`, `unwrap`, freshness labels,
  snapshot invalidation, load-more and "New commits", theme script parity, ProblemAlert mapping, the manifests panels.
- Go: `TestPages` (headers, token, nonce in request and response CSP, 503); `TestPrivateViewer` (the token admits a private viewer only
  with the right value); `webui` `TestServer` starts a fake child (a Go test binary acting as `node`), restarts it, stops it.
- Playwright against `cmd/scp/e2e_test.go` (the real binary path: Go supervising the built bundle) at 1280x800 and 360x740: every
  route renders its `h1`; with JavaScript disabled every page shows its content, title, description and canonical; the sitemap lists
  the pages; unknown paths and repositories answer 404; dark mode paints dark from the first frame; static files are immutable.
- An auto fixture fails any test on a `securitypolicyviolation`, `pageerror` or unexpected `console.error` (the CSP and hydration check).
- axe (`@axe-core/playwright`, tags up to `wcag22aa`) on every route in both themes: zero violations.
- CI `web` job: frozen install, gen drift, lint, typecheck, format check, `vitest`, build with budget; `e2e` job: build plus Playwright;
  both behind the required `gate`.
