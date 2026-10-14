# Web UI
Status: v1 design note, 2026-10-14. Binding order: ../plan.md > this note > ../reference/.

## Scope (v1)
A read-only React SPA for the pages of plan.md section 2: dashboard, group, repository (README), docs viewer, commit logs (all, group,
repository) and a 404 page. It reads only `/api/v1`, shows freshness everywhere and is embedded in the Go binary.

## Decisions (bullets with one-line reasons)
- Stack as plan.md: React 19, TypeScript, Vite, Tailwind 4, shadcn/ui (Base UI), TanStack Router + Query, Vitest, Playwright, pnpm.
- Code-based TanStack Router tree in `src/app/router.tsx`, each page lazy: typed links and params straight from code.
- Client: `openapi-typescript` generates `web/src/lib/api/schema.d.ts` from `api/openapi.yaml` (committed, drift-checked);
  `openapi-fetch` is the thin typed fetch wrapper; one `unwrap()` turns every failure into a `ProblemError`: every request is typed.
- Features never import features (mirror of the Go rule); they share through `shared/`, `lib/` and typed `<Link>`s. One ESLint
  `no-restricted-imports` rule enforces it.
- Freshness by polling `GET /api/v1/snapshot` every 30 s through one hook, so the transport can change behind it.
- One component, `shared/safe-html.tsx`, renders server-sanitised HTML: the server's sanitiser is the single source of safe markup.
- Commit authors are initials on a deterministic colour: every request stays on the portal's origin.
- Static CSP set by the server; scripts and styles are external files: the shell is a cacheable static file.
- English strings in code; dates and numbers through `Intl`, all times shown relative with the exact UTC instant in `<time>`.
- TanStack Query holds server state and the URL holds view state: two places, both inspectable.

## Structure and routes
```
web/
  package.json pnpm-lock.yaml pnpm-workspace.yaml   pins; pnpm policy: 7-day minimumReleaseAge, frozen lockfile in CI
  index.html vite.config.ts vitest.config.ts playwright.config.ts tsconfig.json eslint.config.js components.json
  web.go embed_spa.go embed_placeholder.go placeholder/index.html     go:embed (Build and embed)
  public/theme-init.js favicon.svg                  copied verbatim to dist/
  scripts/precompress.ts budget.ts
  e2e/fixtures.ts smoke.spec.ts
  src/
    main.tsx                                        createRoot, providers, stale-chunk reload handler
    app/       root.tsx (shell: skip link, header, nav, theme toggle, <main>), router.tsx, providers.tsx, not-found.tsx
    features/  dashboard/ groups/ repos/ commits/ docs/ status/      each: route.tsx, api.ts, components, *.test.tsx
    components/ui/                                  shadcn generated (button, card, table, badge, alert, select, ...)
    shared/    safe-html freshness-badge relative-time page-header problem-alert empty-state initials-avatar
    lib/       api/{schema.d.ts,client.ts,query-keys.ts}  theme.ts  format.ts  build.ts
    styles/    index.css (tokens, light and dark)  docs.css (prose and Chroma classes for both themes)
    test/      setup.ts server.ts (MSW, unmocked request fails the test)
```
`api.ts` holds the feature's `queryOptions` factories and is the only place that knows endpoint paths. `status/` owns the shell's
snapshot sync hook and the connection banner; it has no route.

| Path | Feature | Shows |
|---|---|---|
| `/` | dashboard | groups as cards (count badge, freshness), latest commits across the org |
| `/groups/{group}` | groups | group header, repository table (filter by name, sort), latest commits |
| `/groups/{group}/commits` | commits | commit log scoped to the group |
| `/repos/{owner}/{name}` | repos | descriptor (title, owners, lifecycle, tags, links), badges, rendered README, latest commits |
| `/repos/{owner}/{name}/commits` | commits | commit log of the default branch |
| `/repos/{owner}/{name}/docs/{path}` | docs | rendered doc from `docs/` with a file list and heading outline |
| `/commits` | commits | commit log across all repositories, filter by group or repository |
| anything else | app | 404 page (also for an unknown group or repository from a 404 problem) |

The server's SPA shapes (http-api.md Routes) equal this table; the Playwright smoke pins both sides.

## Behaviour (numbered)
1. Queries: `staleTime` 10 s, `gcTime` 5 min, retry network/429/5xx at most twice (honour `Retry-After`), refetch on window focus,
   previous data kept on error. ETag revalidation is the browser's (API answers `no-cache` + ETag).
2. Snapshot sync: one `useSnapshotSync()` in the shell polls `GET /api/v1/snapshot` every 30 s while the tab is visible (304 when
   unchanged). A new `version` invalidates every other query; active ones refetch. Readiness `warming` shows a banner.
3. Commit logs page with "Load more" (cursor, 50 rows) on `GET /api/v1/commits` with the page's `group` or `repo` filter. When more than
   one page is loaded a snapshot change leaves the log as it is; a "New commits" button refetches and moves focus to the first row.
4. Freshness: every view shows a `FreshnessBadge` from the response's `freshness` (`fresh` "Up to date", `stale` "Delayed", `error`
   "Update failing", unknown value "Unknown"): icon plus text. Ages re-render on one 30 s ticker, paused while hidden.
5. Failure is data on the client too: a failed refetch keeps the data on screen; one polite `ConnectionBanner` says "Cannot refresh right
   now. Showing the last data that loaded." after network, 429 or 5xx failures and clears on the next success.
6. Problems (`application/problem+json`): `ProblemAlert` titles come from `code` (`not-found`, `not-ready`, `queue-full`, `internal`,
   default "Unexpected problem") and always show the request id. Each independent panel has its own error boundary, so one failing
   panel leaves the page standing; a root boundary catches the rest.
7. Deploy skew: `X-SCP-Build` on any API response differing from the compiled `BUILD_ID` shows "A new version is available" with Reload;
   a missing lazy chunk (`vite:preloadError`) reloads once, guarded against loops.
8. `SafeHtml` takes the API field `html` and sets it with `dangerouslySetInnerHTML`, memoised on the string so a refetch with equal
   content keeps scroll and DOM. Lint allows `dangerouslySetInnerHTML` in this component only. The docs HTML carries classes (Chroma)
   and no inline styles; relative images point to `raw.githubusercontent.com` and absolute https images load from their hosts.
9. Theme: `system | light | dark` in `localStorage` key `scp-theme`. The blocking external `/theme-init.js` in `<head>` sets the `dark`
   class and `colorScheme` before first paint; `<meta name="color-scheme" content="light dark">`. `lib/theme.ts` resolves the same
   way; a unit test runs the script for every stored value and compares. Blocked storage falls back to `system`.
10. CSP: the shell policy of http-api.md behaviour 3 (`img-src 'self' https:`, everything else `'self'` or `'none'`). shadcn/Base UI runs
    with `CSPProvider disableStyleElements`; runtime styles go through the CSSOM.
11. Accessibility (WCAG 2.2 AA): skip link to `#main`, landmarks, one `h1` per page, focus to `h1` and title announced after client
    navigation, visible focus ring (token contrast >= 3:1, text >= 4.5:1 in both themes), targets >= 24 px, real `<table>`s with keyboard
    sortable headers, reflow at 320 px, `prefers-reduced-motion` honoured, `<noscript>` pointing to `/llms.txt` and the OpenAPI document.
12. URLs are state: filters and sort live in search params, so every view is shareable; repositories are `{owner}/{name}`, doc paths a
    splat encoded per segment.

## Build and embed
- `pnpm build`: `vite build` (assets `assets/<name>-<8 hex>.<ext>`, no source maps, `assetsInlineLimit: 0` so every asset is a file, system
  font stack), then `precompress.ts` (brotli and gzip siblings for files >= 1 KiB, kept when smaller), then `budget.ts` (fails the build).
- Budget (gzip, cold load): initial JS <= 200 KB, initial CSS <= 25 KB, any lazy route chunk <= 60 KB, fonts 0. The report names the offender.
- `BUILD_ID` (git SHA, `dev` locally) goes to Vite's `define` and to `go build -ldflags -X main.build=...`.
- `web/embed_spa.go` (`//go:build spa`, `//go:embed all:dist`); `web/embed_placeholder.go` (`!spa`, `//go:embed placeholder`) so
  `go test ./...` runs with Go alone. `make build` = `pnpm -C web build` then `CGO_ENABLED=0 go build -tags spa`.
- Serving (`httpapi`): GET/HEAD; `assets/*` `public, max-age=31536000, immutable`; `index.html`, `theme-init.js`, `favicon.svg`
  `no-cache` + ETag from the build id; `.br`/`.gz` chosen by `Accept-Encoding`; `X-SCP-Build` on every response. `index.html` answers 200
  for the SPA shapes above and 404 for other extensionless paths outside the reserved prefixes (http-api.md Routes); a missing file with an
  extension is a plain 404.
- Dev: `make dev` runs `scp serve` on :9090 (against a test org or the fake GitHub) and Vite on 127.0.0.1:5173, which proxies `/api`,
  `/badges`, `/llms.txt` and `^/(repos|groups)/.+\.(md|txt)$` to `SCP_DEV_API` (default `http://127.0.0.1:9090`). The e2e job enforces
  the CSP. `make gen` regenerates `schema.d.ts`; CI fails on drift.

## Tests
- Vitest + Testing Library (jsdom) + MSW typed from `paths` (`openapi-msw`), any unmocked request fails: components, hooks, `unwrap`,
  freshness labels, snapshot invalidation on a version change, load-more and "New commits", theme script parity, ProblemAlert mapping.
  Fake timers throughout.
- Playwright smoke against the real `-tags spa` binary with the fake GitHub, desktop 1280x800 and phone 360x740: every route renders its
  `h1`; unknown path answers 404 with the shell; asset cache headers and compression; a dark-mode reload paints dark from the first frame.
- An auto fixture fails any test on a `securitypolicyviolation` event, `pageerror` or unexpected `console.error`: the CSP check, which
  also catches a component upgrade that starts injecting `<style>`.
- axe (`@axe-core/playwright`, tags up to `wcag22aa`) on every route in both themes: zero violations.
- CI `web` job: frozen install, gen drift, typecheck, lint, Vitest, build with budget; `e2e` job: binary build plus Playwright. Both required.

## Reference (archive pointers)
- `../reference/detailed-design/20-design/17-web-foundation.md` 4.3 (client, `unwrap`), 4.4 (query defaults, polling, skew), 4.5-4.8
  (freshness, errors, theme, accessibility), 4.11 (budgets), 4.14 (dev proxy), 7.2 (dist contract).
- `../reference/detailed-design/20-design/18-web-pages.md` 4.2-4.6 (page layouts and state matrices), 4.1.10 (load more), 4.14 (404, empty states).
- `../reference/detailed-design/11-adr/ADR-010-*` (embedding, fallback, CSP), `ADR-019-*`.
- `../reference/detailed-design/10-architecture.md` section 12 (structure, data, budgets, build loop).
- `../reference/detailed-design/50-repo-seed/repo/web/`: `vite.config.ts` (build options, proxy), `pnpm-workspace.yaml`,
  `public/theme-init.js`, `embed_*.go` + `placeholder/`, `e2e/fixtures.ts` (CSP gate), `scripts/precompress.ts`, `scripts/budget.ts`,
  `src/lib/stale-chunk.ts`, `src/test/`, and the pins of `package.json` for the stack above.
