# 17 - Web foundation: `web/` tooling, structure and the shared layers

Status: design, ready to implement; independently verified on 2026-10-14 (section "Verification log" at the end; defects found were fixed here and in the prototype). Date: 2026-10-14 (all library facts below were checked against the npm registry and the vendors' pages on this date). Unit: `17-web-foundation`. Binding inputs, in precedence order: `04-decisions.md` (D-16, D-06, D-09, section 7 amendments A-14, A-16, A-17), `10-architecture.md` (sections 4-6 and 8-23, in particular 11.3, 11.4, 12, 20, 22, 23), `11-adr/` (ADR-009, ADR-010, ADR-016, ADR-017, ADR-019, ADR-020). Companion files: `17-web-foundation.assets/` (the verified throw-away prototype, appendix A).

This document is self-sufficient: an engineer who has never seen the research or this conversation can build `web/` up to the point where feature slices (unit 18) can be added, and can wire it into the Go build, from this text alone. Everything stated as verified was executed in the prototype on 2026-10-14; everything not executed is marked **(specified, not prototyped)**.

## Table of contents

0. How to read this document; verification log
1. Purpose, scope, non-goals, exact packages and directories
2. Position in the architecture
3. Domain model and types
4. Behavioural specification
5. Data
6. Configuration
7. HTTP/API surface owned
8. Concurrency and lifecycle
9. Observability
10. Security
11. Capability and guardrail trace
12. Test plan
13. Implementation tasks
14. Deviations proposed
15. Open questions
Appendix A. Companion files and how to reproduce the verification
Appendix B. Quick reference for feature authors (unit 18)
Appendix C. Verbatim configuration of the sensitive files
Verification log (independent verifier, 2026-10-14)

---

## 0. How to read this document; verification log

### 0.1 Conventions

- "SPA" is the React application under `web/`. "The Go side" is `internal/webapp`, `web/` embed files and `cmd/scp` (unit 06 and the composition root). "Unit 18" is `18-web-pages` (the feature slices: dashboard, groups, commits, docs, manifests, pages, status; the data table, the dependency graph, the search model, initials avatars).
- Paths are relative to the repository root unless they start with `web/`.
- `[verified]` means the claim was executed in the prototype (appendix A) on 2026-10-14 with the output recorded in 0.3. `[specified]` means it is a requirement that was not executed.
- "Pinned" means an exact version in `package.json` (no `^`, no `~`), enforced by `scripts/check-pins.ts`.

### 0.2 Library verification log (npm registry, 2026-10-14)

Rule applied: pin the newest release of the major line that is at least 7 days old on 2026-10-14 (`pnpm minimumReleaseAge: 10080`), unless a newer one is required by a verified incompatibility. `pins.py` in the assets prints the registry data used here (latest, release date, newest-older-than-7-days, licence, engines).

| Package | Pinned | Why this one |
|---|---|---|
| `react`, `react-dom`, `@types/react`, `@types/react-dom` | 19.3.0 | architecture 12.1; React Compiler 1.0.0 supports it |
| `babel-plugin-react-compiler` | 1.0.0 | exact pin: the compiler is a code transform, never float |
| `typescript` (alias `npm:@typescript/typescript6@6.0.2`) and `@typescript/native` (alias `npm:typescript@7.0.2`) | 6.0.2 and 7.0.2 | the `tsc` binary is **7.0.2** (verified `tsc -v`); the TS 6 alias keeps the JS API that `typescript-eslint` 8.71.1 needs until TS 7.1 (architecture 12.1) |
| `vite` | 8.3.3 | Rolldown/Oxc bundler; `build.rolldownOptions` |
| `@vitejs/plugin-react` | 6.1.2 | exports `reactCompilerPreset` |
| `@rolldown/plugin-babel` | 0.2.4 | runs the React Compiler and the Lingui macro under Vite 8 |
| `@babel/core` | 8.0.6 | peer of the above |
| `tailwindcss`, `@tailwindcss/vite` | 4.3.3 | CSS-first configuration, no `tailwind.config.js` |
| `@tailwindcss/typography` | 0.5.20 | the `prose` styles of server-sanitised HTML |
| `shadcn` (CLI, devDependency) | 4.21.3 | `style: base-nova`, Base UI |
| `@base-ui/react` | 1.8.0 | the primitives under shadcn since 2026-07 |
| `cn` | 0.4.0 | the standalone `cn` package (shadcn change of 2026-09, accepted) |
| `class-variance-authority`, `tw-animate-css`, `lucide-react`, `cmdk` | 0.7.1, 1.4.0, 1.52.0, 1.1.1 | shadcn dependencies; `cmdk` was written as `^1.1.1` by the CLI and pinned by hand |
| `@primer/octicons-react` | 19.39.0 | the GitHub mark only |
| `@tanstack/react-router` | 1.170.41 | the newest release older than 7 days (ADR-019 quotes 1.170.44, which is newer than the cooldown; Renovate will bump) |
| `@tanstack/router-plugin`, `@tanstack/virtual-file-routes` | 1.168.42, 1.162.0 | virtual file routes, `autoCodeSplitting` |
| `@tanstack/react-query` | 5.104.1 | |
| `@tanstack/react-table` | 9.2.6 | a dependency of unit 18; pinned here so there is one lockfile |
| `zod` | 4.6.5 | imported only as `zod/mini` (lint-enforced) |
| `recharts` | 3.10.1 | through the shadcn chart component; the first draft pinned 3.8.0 (the version the shadcn registry was written against) although 3.10.1 (2026-07-25) is older than the cooldown; the whole check suite typechecks with 3.10.1 (verification V-14) |
| `openapi-typescript`, `openapi-fetch` | 7.13.0, 0.17.0 | generated types + the typed fetch |
| `@redocly/cli` | 2.59.0 | bundle + lint of the OpenAPI documents |
| `@lingui/core`, `react`, `cli`, `vite-plugin`, `babel-plugin-lingui-macro`, `format-po` | 6.9.0 | 6.9.1 was released 2026-10-09 (5 days ago) and is inside the cooldown |
| `eslint-plugin-lingui` | 0.16.0 | `no-unlocalized-strings` |
| `vitest`, `@vitest/browser-playwright` | 5.0.3 | 5.0.4 is 2 days old |
| `vitest-browser-react` | 2.3.0 | |
| `jsdom` | 30.1.2 | |
| `@testing-library/react`, `dom`, `jest-dom`, `user-event` | 16.3.3, 10.4.2, 7.0.1, 14.6.7 | |
| `msw`, `openapi-msw` | 3.0.2, 2.1.0 | MSW 3 option `onUnhandledFrame` (v2 name was `onUnhandledRequest`) |
| `@playwright/test`, `playwright` | 1.63.0 | |
| `axe-core`, `@axe-core/playwright` | 4.14.0, 4.13.0 | tags `wcag2a wcag2aa wcag21a wcag21aa wcag22aa` |
| `eslint`, `@eslint/js` | 10.12.0, 10.0.1 | flat config only |
| `typescript-eslint` | 8.71.1 | |
| `eslint-plugin-boundaries` | 7.2.0 | element/dependency policy syntax of v7 (`boundaries/dependencies`, `from: {element}`/`allow: {to}`) |
| `eslint-plugin-import-x`, `eslint-import-resolver-typescript` | 4.17.1, 4.4.5 | `import-x/no-cycle` |
| `eslint-plugin-react-hooks`, `-react-refresh`, `-jsx-a11y` | 7.1.1, 0.5.7, 6.10.2 | the hooks plugin includes the React Compiler rules |
| `prettier`, `prettier-plugin-tailwindcss` | 3.9.9, 0.8.1 | |
| `globals` | 17.13.0 | |
| `@types/node` | 24.19.1 | deliberately the 24 line (CI runs Node 24.21.0), not 26.6.5 |
| `@types/trusted-types` | 2.0.7 | |
| pnpm | 12.10.1 | 12.11.2 is 5 days old; pinned through `devEngines.packageManager` with `onFail: error` (Corepack is not shipped from Node 25) |
| Node | 24.21.0 in CI and the Docker build stage; `engines.node` is `^24.21.0 \|\| ^26.0.0` | the prototype ran on **Node 26.10.0** (what the machine had) and on pnpm 12.10.1; the switch of CI to the 26 LTS line is an operational decision after 2026-10-28 (open question Q7) |

Considered and **not adopted** (registry verified the same day): `size-limit` 14.2.0 (replaced by `scripts/budget.ts`, deviation D2), Storybook 10.6.2 (section 4.13), Biome 2.5.15 (no equivalent of `eslint-plugin-boundaries`; Biome has no type-aware rules), `@lhci/cli` (last release 2025-06-25; Lighthouse 13.5.0 is used directly, nightly), `@fontsource-variable/geist` 5.3.0 (deviation D4), `react-hook-form` 7.90.0 (no forms in v1; the only inputs are search boxes bound to URL search params), `i18next` 26 / `react-i18next` 17 (Lingui chosen, section 4.9), `knip` 6.41.0 (a candidate for M3, open question Q10), `vite-plugin-compression2` 2.5.3 (a 19-line script does the same, no dependency), TanStack devtools (dev-only; not installed in the prototype; adding them is allowed by the lint rules because they are `devDependencies` imported behind `import.meta.env.DEV`).

### 0.3 What was executed (prototype `17-web-foundation.assets/`, 2026-10-14)

| Check | Result |
|---|---|
| `pnpm install --frozen-lockfile` under the pnpm 12 supply-chain policy (4.15) | ok after two policy fixes recorded in section 14 (D8) |
| `tsc -v` | `Version 7.0.2`; `pnpm typecheck` (app + node projects, strict flags of 4.1.3) clean |
| `eslint . --max-warnings 0` (type-aware, boundaries, import cycles, jsx-a11y, lingui, hooks) | exit 0, no warnings; 19 violation fixtures in `scripts/lint-rules.test.ts` (12 in the first draft, 7 added by the verification) each fire the rule they target and 2 allowed cases pass |
| `vitest run --project unit --project tooling` | 21 files, **144 tests** passed (the first draft had 20 files and 130 tests; the verification removed `doc-id.test.ts` and added 15 regression tests); the contrast test reads the real `index.css` tokens |
| `vitest run --project browser` (real Chromium) | 2 tests passed (the badge box; `SafeHtml` keeps its DOM on a same-html re-render); unit 18 adds more |
| `pnpm check` (check-pins, prettier --check, typecheck, eslint --max-warnings 0, tests) | exit 0 (about 65 s) |
| `pnpm build` with `BUILD_ID=abc123def456` | 44 files, 846 KiB in `dist/`; initial JS **139.0 KB** gzip of 200, initial CSS **17.5 KB** of 25; every lazy route chunk below 4 KB (stubs) |
| Playwright 1.63.0 through the **real** `internal/webapp` (unit 06 prototype) under the full enforcing CSP including Trusted Types | **62 passed** (31 per viewport: desktop 1280x800 and phone 360x740; the first draft had 60, the verification added the 320 px reflow test): axe `wcag22aa` and no horizontal scroll on 11 pages x light/dark, real 404 shell, stale-chunk plain 404, theme-init before the bundle, staleness text, 503 `not-ready` problem alert with request id, build-skew banner, typed link without reload, Trusted Types `SafeHtml` page. Zero CSP violations, zero console errors, zero page errors (the gate fixture fails any) |
| Vite dev server on `127.0.0.1:5173` with the proxy to the Go binary | `/` 200, `/dashboard` 200 (history fallback), `/api/v1/snapshot` 200 `application/json` through the proxy, `/theme-init.js` 200 |
| Go: `go build ./...`, `go vet ./cmd/e2eserve ./internal/webapp ./web`, `go test ./internal/webapp ./web` in the copy of the 06 prototype with the e2e server | ok (Go 1.27.1 on this machine; the module says `go 1.27`) |

What was **not** executed: the Go seam test against the real `routes.Public().SPAShapes()` (unit 01 has no such code yet; the golden test in section 12 pins the TS side), Lighthouse, the Docker multi-stage build, the SSE hook (v1.1), a real unit-18 feature, the Renovate configuration, the GitHub Actions workflow file.

---

## 1. Purpose, scope, non-goals, exact packages and directories

### 1.1 Purpose

D-16 makes the portal's UI a React + shadcn/ui + TypeScript single-page application served from the Go binary. This unit builds the **foundation** every feature slice stands on, so that unit 18 can add pages by writing only feature code:

1. the `web/` toolchain: exact-pinned `package.json`, pnpm 12 supply-chain policy, TypeScript 7 configuration, Vite 8 configuration with the React Compiler and Lingui, Tailwind 4 and shadcn/ui (Base UI) setup, ESLint boundaries, Prettier;
2. the feature-sliced source structure and the lint rules that keep it that way;
3. typed routing (TanStack Router, virtual file routes) and the generation of `dist/routes.json` for the Go side;
4. the generated API client workflow (OpenAPI 3.1 -> `schema.d.ts` -> `openapi-fetch`), the single error type, the query layer, polling, invalidation and the SSE integration point;
5. the cross-feature shared layer (`shared/`: staleness badge, relative time, safe HTML, problem alert, panel boundary, connection banner, route announcer, theme toggle, external link, announcement, header status) and the application shell (`app/`);
6. theming (light/dark, tokens, CSP-safe pre-paint), accessibility baseline, i18n readiness;
7. performance budgets and their enforcement;
8. the test stack (Vitest unit + browser, Testing Library, MSW 3, Playwright + axe) and the CI gates of the `web` and `e2e` jobs;
9. the dev workflow (Vite proxy) and the embed build that hands `web/dist` to `go build -tags spa`.

### 1.2 In scope

All files under `web/` except `web/src/features/*` page contents (unit 18), plus the web part of CI and the Makefile targets `gen`, `web`, `dev`, `test` (architecture 12.4). The feature slices exist in this unit only as **route stubs** (one `route.tsx` per slice, `export {}` in `index.ts`) so that the route tree, the boundaries and the build are proven end to end; the real `dashboard` slice (list of groups with freshness) and the `pages` slice (server HTML) are included as the two **reference slices** that show how features use the foundation.

### 1.3 Non-goals

- Feature pages, the data table, the dependency graph, commit lists, the search model, initials avatars: unit 18.
- The Go hosting (`internal/webapp`), CSP strings, header classes, `routes.json` parsing: unit 06 (this unit **produces** the bundle that unit 06 consumes and records the contract in section 7.2).
- The OpenAPI documents, DTOs and error catalogue: units 06/07 and each feature's `<feature>api`.
- Markdown parsing and sanitising: the backend (`docs`, `safehtml`). The SPA never parses Markdown and ships no sanitiser (ADR-005, architecture 12.3).
- A design system beyond shadcn tokens, server-side rendering, PWA/offline support (the manifest exists only for installability metadata), analytics, client-side error reporting service (open question Q3), authentication UI (v1 has none: `access` mode uses a trusted proxy).

### 1.4 Exact directories (the tree to create)

```
web/
  package.json            exact pins, scripts, engines, devEngines.packageManager
  pnpm-workspace.yaml     supply-chain policy (4.15)
  pnpm-lock.yaml          committed
  .nvmrc  .node-version   24.21.0 (the architecture tree names .node-version; .nvmrc is kept for nvm users, both hold the same line, D14)
  .gitignore              node_modules/ dist/ .build/ test-results/ playwright-report/ .tmp/
  .prettierrc.json        .prettierignore       (generated files and public/theme-init.js are ignored)
  components.json         shadcn: style base-nova, aliases, css src/styles/index.css
  tsconfig.json           references + the "@/*" paths the shadcn CLI reads
  tsconfig.app.json       src + routes.ts (browser, strict flags)
  tsconfig.node.json      configs, scripts, e2e (Node)
  vite.config.ts          build, define __BUILD_ID__, dev proxy
  vite.plugins.ts         plugin chain shared by Vite and Vitest
  vitest.config.ts        projects: unit (jsdom), tooling (node), browser (Chrome)
  playwright.config.ts    projects desktop and phone; baseURL SCP_E2E_URL
  eslint.config.js        flat config; FEATURE_MAY_IMPORT rank table
  lingui.config.ts        source locale en, .po catalogues
  routes.ts               the route tree (virtual file routes) and adminOnly
  index.html              the only HTML file; no inline script or style
  public/                 theme-init.js  favicon.svg  manifest.webmanifest   (copied to dist/ verbatim)
  scripts/                finalize.ts  precompress.ts  verify-dist.ts  budget.ts  check-pins.ts  route-shapes.ts
                          route-shapes.test.ts  e2e-coverage.test.ts  lint-rules.test.ts   (the `tooling` Vitest project)
  e2e/                    fixtures.ts (CSP/console gate)  pages.ts (one sample URL per shape)  routes.spec.ts  behaviour.spec.ts  fixtures/api/*.json
  src/
    main.tsx              createRoot, error hooks, vite:preloadError guard
    app/                  providers.tsx  router.ts  root.tsx  index-redirect.tsx  routeTree.gen.ts (generated, committed)
    features/             dashboard/ groups/ commits/ docs/ manifests/ pages/ status/   (index.ts, route.tsx, api.ts, *.tsx)
    components/ui/        shadcn-generated and patched components (owned code)
    shared/               cross-feature app components (section 3.4)
    lib/                  api/ (client, problem, query-client, query-keys, snapshot, site, schema.d.ts generated)
                          format.ts i18n.ts persist.ts storage-keys.ts theme.ts server-status.ts routes.ts url.ts stale-chunk.ts
    locales/en/messages.po
    styles/index.css      Tailwind + tokens + prose + scrollbar CSS
    test/                 setup.ts setup-browser.ts server.ts render.tsx contrast.ts
web/dist/                 build output (not committed); embedded by web/embed_spa.go
web/embed_placeholder.go  web/embed_spa.go  web/placeholder/   (unit 06 owns these files)
```

`routeTree.gen.ts`, `schema.d.ts`, `messages.po` and `pnpm-lock.yaml` are **committed** (ADR-020: generated source is committed with CI drift checks); `dist/` and `.build/` are not (A-14).

### 1.5 Division with unit 18 (parallel unit; the seam is explicit)

| Concern | Owner | Notes |
|---|---|---|
| Toolchain, configs, scripts, CI web job, lint rules | 17 | unit 18 only adds code under `src/features/*` and, where noted, `shared/` |
| `app/` shell, router, providers, root error and not-found | 17 | |
| `lib/api/*` (client, problem, query client, snapshot sync, site query, `unwrap`) | 17 | unit 18's compile-check copy of `client.ts`, `problem.ts`, `query-keys.ts` must be replaced by these files at integration |
| `shared/` primitives listed in 3.4 | 17 | unit 18 adds `data-table`, `avatar` (initials), `text` helpers, `freshness.ts` helpers (`worst`, `toneOf`), `problem-view.ts` |
| Token **mechanism** (CSS variable names, `:root` and `.dark` blocks, contrast test harness `src/test/contrast.ts`) | 17 | |
| Token **values** and the extended semantic tokens (`--success`, `--warning`, `--danger`, `--info`, `--viz-*`, `--sidebar-*`) | 18's `tokens.css` is the source of values once it lands | integration rule: `src/styles/index.css` imports `src/styles/tokens.css` and loses its own `:root`/`.dark` blocks; the contrast test then reads `tokens.css` (open question Q8) |
| Route tree (`routes.ts`) | 17 defines the shapes; 18 fills the slices | adding a route changes `routes.ts`, `routes.json` golden and the Go seam test together |
| Real `dashboard`, `pages` reference slices | 17 (as references) | 18 may replace their content, not their structure |
| Document identifiers (`docId`, the extension-less `/repos/$repo/docs/$` splat, `resolveDocId`) | **18** (`features/docs/docId.ts`) | the first draft had `lib/doc-id.ts` here; it was wrong against 07 D-3 (percent-encoded, injective ids) and 18 D-18-01 (extension-less splat resolved through the repository's doc index) and was **removed** (V-09) |
| `useSite()` (suspense read of `getSite`, key `qk.site()`), `useDisplayZone()` | 17 (`lib/api/site.ts`, `lib/format.ts`) | requested by 18 section 2.3; added (the shell itself uses the plain `siteQuery` so it renders before `Site` loads) |
| `ExternalLink` props | 17 (`shared/external-link.tsx`) | 18 expects `className`; added. 18's text says "only `https://`"; 17 renders `http:` and `https:` as links (a GitHub Enterprise on plain http is legal) and everything else as text. 17's rule stands unless 18 asks otherwise |
| Router pending behaviour | 17 (`app/router.ts`) | `defaultPendingMs: 150`, `defaultPendingMinMs: 300` (requested by 18, T-18-006); added |
| Array search-param serialisation, `useListener()` | open | Q11 (listener detection) and Q13 (array serialiser) with defaults |

---

## 2. Position in the architecture

### 2.1 Where the unit sits

`web/` is a separate build artefact: a static bundle produced by Node tooling and embedded into the binary by `go build -tags spa` (ADR-010). At runtime there is no Node. The SPA is a pure client of the HTTP API and the server-only routes; it imports nothing from Go and Go imports nothing from it except the embedded file system.

```
Makefile / CI
  make gen   -> go generate ./...  +  pnpm -C web gen:bundle gen:types   (committed outputs, drift-checked)
  make web   -> pnpm -C web install --frozen-lockfile && pnpm -C web build   -> web/dist (+ .br/.gz, routes.json)
  make build -> make web && CGO_ENABLED=0 go build -tags spa -trimpath -ldflags "-s -w -X main.build=$BUILD_ID"

browser --HTTP--> internal/webapp (unit 06) serves web/dist   (GET/HEAD, history fallback, CSP, X-SCP-Build)
browser --HTTP--> /api/v1/*, /badges/*, /media/*, /llms.txt     (units 07 and the feature APIs)
```

### 2.2 Allowed imports inside `web/src` (lint-enforced; verified by `scripts/lint-rules.test.ts`)

| From \ To | `features/<self>` | `features/<other>` | `components/ui` | `shared` | `lib` | `app` | `styles` |
|---|---|---|---|---|---|---|---|
| `features/<f>` | yes | only via that feature's `index.ts`, and only in the direction of `FEATURE_MAY_IMPORT` | yes | yes | yes | no | no |
| `shared` | no | no | yes | yes | yes | no | no |
| `components/ui` | no | no | yes | no | yes | no | no |
| `lib` | no | no | no | no | yes | no | no |
| `app` | n/a | yes (route components) | yes | yes | yes | yes | no |
| `main.tsx` | n/a | no | no | no | yes | yes | yes (`./styles/index.css`) |

`FEATURE_MAY_IMPORT` (exported from `eslint.config.js`, the web mirror of the Go rank table, architecture 12.2): `dashboard: [groups, commits]`, `groups: [commits]`, `commits: []`, `docs: []`, `manifests: []`, `pages: []`, `status: []`. `import-x/no-cycle` is an error. `main.tsx` is excluded from the boundaries plugin (`boundaries/ignore`) because nothing imports it and it wires `app`, `lib` and `styles`; with element patterns for single files the plugin prints a warning on every run (verified), so the exclusion keeps the lint output clean. Tests may import `src/test/*`; `src/test/*` may import anything (it is not a layer).

Third-party import bans (rule `no-restricted-imports`): `zod` (use `zod/mini`, bundle budget), `react-router` (TanStack Router only). Banned globals in `src` (rule `no-restricted-globals`, relaxed per file in the overrides): `fetch`, `XMLHttpRequest`, `EventSource`, `localStorage`, `sessionStorage`, `eval`.

### 2.3 Who depends on this unit

- Unit 18 imports `@/shared/*`, `@/lib/*`, `@/components/ui/*` and uses the route stubs, `createFileRoute`, `qk`, `unwrap`, `api`.
- Unit 06 consumes `dist/` (contract in 7.2) and `routes.json`; its `webapp.New` fails the server start on a bad bundle, so unit 17's `verify-dist.ts` mirrors those checks to fail earlier, in the web job.
- Unit 01 (`routes`) owns the Go route table whose `SPAShapes()` must equal the shapes this unit generates; the seam test sits in `internal/wiring` (06 section 12) and in the e2e job.
- Unit 07 owns the OpenAPI documents this unit generates types from; a contract change that is breaking for the SPA fails `pnpm typecheck` after `pnpm gen:types`.

### 2.4 Consumer-defined interfaces

The SPA is TypeScript, so "interfaces" are TS types; the **Go signatures this unit depends on** are listed so a change in unit 06 is visible here. All were compile-checked by building `cmd/e2eserve` against the 06 prototype (`go build ./...` and `go vet` clean on Go 1.27.1; re-checked by the verifier against the 06 prototype as verified at 03:06, where the first draft's `For(l) http.Handler` and the missing `RootRedirect`/`AdminRedirect` were corrected):

```go
// internal/webapp (unit 06). The consumer here is the web build; these are the only symbols it relies on.
package webapp

func New(fsys fs.FS, opts Options) (*Handler, error)
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request)
func (h *Handler) For(l Listener) *Handler            // webapp.Public | webapp.Admin (same files, other route manifest); returned type is *Handler, not http.Handler
func (h *Handler) CSPReports() http.Handler           // POST /csp-reports

type Options struct {
	BuildID              string   // X-SCP-Build; with CheckBuild it must equal dist/routes.json "buildId"
	CheckBuild           bool     // true only in a spa build
	Reserved             []string // "/api/" mandatory; computed from the route table in cmd/scp
	CSP                  CSPMode  // CSPReportOnly | CSPEnforceCore | CSPEnforce (config key web.csp.mode)
	RequirePrecompressed bool     // New fails if a compressible file >= 1 KiB has no .br sibling
	RootRedirect         string   // GET / on the public listener (default "/dashboard")
	AdminRedirect        string   // GET / on the admin listener (default "/status"): the SPA's own "/" redirect is therefore never reached on admin
	// ... ReportPath, ReferrerPolicy, OnCSPReport, Observe are 06's business
}
```

The embedded file system contract (what `fsys` must look like) is the **dist contract** of 7.2 and is enforced twice: by `scripts/verify-dist.ts` (this unit) and by `webapp.New` (unit 06).

TypeScript interfaces other units code against (full definitions in section 3): `ProblemError`, `unwrap`, `createApi`, `qk`/`Topic`/`topicRoots`, `invalidateTopics`, `useSnapshotSync`, `siteQuery`, `snapshotQuery`, `FreshnessBadge`, `RelativeTime`, `SafeHtml`, `ProblemAlert`, `PanelBoundary`, `ExternalLink`, `serverStatus`, `useSite`, `readPersisted`/`writePersisted` (the latter returns `boolean`), `STORAGE`, `setPreference`/`applyServerDefault`, `formatRelative`/`formatDateTime`/`useNow`, `serverRoutes`, `onPreloadError`.

---

## 3. Domain model and types

### 3.1 Wire types

`src/lib/api/schema.d.ts` is **generated** by `openapi-typescript` 7.13.0 from the bundled admin document (a superset of the public one) and committed. No hand-written API type exists; feature code takes types as `components["schemas"]["Name"]`. Enumerations are **open**: any `switch` over an API enum must have a `default`/fallback (additive evolution, architecture 11.4); `FreshnessBadge` renders an unknown status as "Unknown" (tested).

### 3.2 Problems

```ts
// src/lib/api/problem.ts
export type Problem = components["schemas"]["Problem"]          // RFC 9457 + code, errors[], retryAfterSeconds
export type ClientCode = "client-network" | "client-aborted" | "client-unparseable" | "client-contract"

export class ProblemError extends Error {
  readonly status: number                  // HTTP status; 0 for client-minted problems
  readonly code: string                    // stable machine code (api catalogue) or a ClientCode
  readonly requestId: string | null        // from instance "urn:uuid:<id>", else the X-Request-Id header
  readonly retryAfterSeconds: number | null // body member, else the Retry-After header
  readonly fieldErrors: readonly FieldError[]
  readonly detail: string | null           // kept for logs; never rendered (see 4.6)
  get isClient(): boolean                  // status === 0
}
export function isProblem(e: unknown): e is ProblemError
export function problemFromResponse(response: Response, body: unknown): ProblemError
export function problemFromThrown(e: unknown): ProblemError   // fetch rejection -> client-network, AbortError -> client-aborted, ProblemError passes through
export function isTransient(e: unknown): boolean               // retry policy input
```

`isTransient` is true for status 0 with code `client-network`, 429 and **every 5xx except 501** (500, 502, 503, 504; this includes a `client-unparseable` problem that carries a 5xx status, i.e. a proxy's HTML 502 during a deploy, which is exactly what should be retried), and for any error that is not a `ProblemError`; it is false for every other 4xx, 501, `client-aborted` and `client-contract` (table-tested; the first draft's text said `client-unparseable` was never transient, which the code and the test contradicted, V-15). `problemFromThrown` recognises an abort by `name === "AbortError"`, not by `instanceof DOMException`, because the error can come from another realm (V-02).

### 3.3 State types

```ts
// src/lib/server-status.ts  (module-level external store, read with useSyncExternalStore)
export type ServerStatus = {
  reachable: boolean                                    // the last API call did not fail with network error, 429 or 5xx
  buildSkew: boolean                                    // X-SCP-Build or snapshot.buildId differs from __BUILD_ID__ (and the bundle id is not "dev")
  readiness: "warming" | "ready" | "degraded" | "unknown"  // from GET /api/v1/snapshot
  lastOkAt: number | null
}
export const serverStatus: { get(): ServerStatus; subscribe(l: () => void): () => boolean;
  reportOk(now?: number): void; reportFailure(): void; reportBuild(serverBuild: string | null, bundleBuild?: string): void;
  reportReadiness(r: ServerStatus["readiness"]): void; reset(): void }
export function useServerStatus(): ServerStatus

// src/lib/theme.ts
export type ThemePreference = "light" | "dark" | "system"
// src/lib/api/query-keys.ts
export type Topic = "site" | "catalog" | "commits" | "docs" | "manifests" | "pages" | "status"   // = api.SseInvalidate.topics (ADR-009)
export const qk: { snapshot(): readonly ["snapshot"]; site(): readonly ["site"]; catalog: {...}; commits: {...}; repo(slug): ...; docs: {...}; manifests: {...}; pages: {...}; status(): ... }
export const topicRoots: Record<Topic, readonly string[]>   // topic -> query-key roots to invalidate
// src/app/router.ts
export type RouterContext = { queryClient: QueryClient }
```

### 3.4 Shared components (public surface of `src/shared`)

| Component / module | Props / signature | Rule |
|---|---|---|
| `FreshnessBadge` | `{ freshness: Freshness }` | always icon + text, never colour alone (`aria-hidden` icon); `data-freshness=<status>`; open enum; shows `RelativeTime` of `asOf` when present (GR-26, GR-40) |
| `RelativeTime` | `{ iso: string; zone?: string }` | `<time datetime title>`; label advances on the shared 30 s ticker `useNow` |
| `SafeHtml` | `{ html: string; className?: string }` | the **only** DOM-from-string sink; Trusted Types policy `scp-html` created once; input must be an API field named `html` (lint selector) |
| `ProblemAlert` | `{ error: unknown; onRetry?: () => void }` | localised title by `code` (`problemTitle`; an unknown code gets the generic "Unexpected problem", `client-unparseable` its own title), request id, auto-retry countdown for `not-ready`, `rate-limited`, `upstream-unavailable`, `queue-full`, `client-network`: it calls `onRetry` when the countdown reaches zero and **restarts the countdown**, so a persisting problem is retried every `min(retryAfterSeconds ?? 15, 60)` s and never on every render; **never renders the server's `title` or `detail`** |
| `problemTitle` | `(code: string) => string` | in `shared/problem-title.ts` (separate file so the component file exports only components) |
| `PanelBoundary` | `{ children: ReactNode }` | error boundary + `QueryErrorResetBoundary` around one panel so one failing query cannot blank the page; offers retry (resets the query errors and re-renders) |
| `ConnectionBanner` | none | one `role="status" aria-live="polite"` region: build skew (reload button), unreachable, warming |
| `RouteAnnouncer` | none | when the **pathname** changes (not on the first load, and not when only the search string changes, so typing in a URL-bound filter keeps focus in the input) focuses `main h1` (`tabindex=-1`) and announces `document.title` |
| `ThemeToggle` | none | cycles `system -> light -> dark`, persisted through `lib/theme` |
| `ExternalLink` | `{ href: string; children: ReactNode; className?: string }` | `target="_blank" rel="noreferrer noopener"` plus a screen-reader "(opens in a new tab)"; only `http:`/`https:` URLs render as links, others render as plain text (`isWebUrl` in `lib/url.ts`) |
| `HeaderStatus` | `{ site: Pick<Site, "lastFullSync" \| "repoCount"> }` | "N repositories" (plural-aware) and "Updated <relative time of lastFullSync>" or "Not synced yet" |
| `AnnouncementBanner` | `{ announcement }` | plain text only (`Site.announcement`), dismissible, dismissal keyed by the announcement id in `localStorage`; the component holds the **dismissed id**, not a boolean, so a new id arriving through a site refetch shows the banner again without a reload (V-06) |

`src/lib/format.ts`: `formatRelative(iso, now, locale = i18n.locale)` (via `Intl.RelativeTimeFormat`, `numeric: "auto"`, "now" under 45 s, one formatter per locale), `formatDateTime(iso, zone, locale = i18n.locale)` (via `Intl.DateTimeFormat`); the default is the **UI locale**, never the browser's, so an English UI does not print "for 5 minutter siden" to a Norwegian browser (V-08; the first draft passed `undefined`, i.e. the browser locale, while 4.9 claimed the opposite), `useNow()` (one shared 30 s `setInterval`, reference-counted, subscribed through `useSyncExternalStore`, skipped while `document.hidden` and caught up on `visibilitychange`), `setDisplayZone(zone)` / `useDisplayZone()` (the zone from `Site.displayTimeZone`, set once by the shell; every `RelativeTime` title uses it).

### 3.5 The route manifest

```ts
// scripts/route-shapes.ts
export type Shape = { pattern: string; listeners: ("public" | "admin")[] }
export function toPattern(tsPath: string): string   // "/repos/$repo/docs/$" -> "/repos/{repo}/docs/{path...}"
export function shapes(): Shape[]                   // leaves and index routes of routes.ts; "/" never listed
```

`dist/routes.json` is `{"version":1,"buildId":"<BUILD_ID>","routes":[{"pattern":"/dashboard","listeners":["public"]}, ...]}` (format owned by unit 06 section 3.5; produced by `scripts/finalize.ts`).

---
## 4. Behavioural specification

### 4.1 Toolchain

#### 4.1.1 `package.json`

Rules: every `dependencies`/`devDependencies` entry is an exact version or an exact `npm:` alias (`scripts/check-pins.ts` fails otherwise, and also fails on `pnpm.overrides` in `package.json`); `"type": "module"`; `"engines": {"node": "^24.21.0 || ^26.0.0"}`; `"devEngines": {"packageManager": {"name": "pnpm", "version": "12.10.1", "onFail": "error"}}`; `"private": true`. `shadcn` (the CLI) is a **devDependency** so the CLI version is pinned like any other tool (deviation D7). The `.nvmrc` holds `24.21.0`.

Scripts (all verified unless marked):

| Script | Command | Purpose |
|---|---|---|
| `dev` | `vite` | dev server on `127.0.0.1:5173` with the proxy of 4.14 |
| `gen` | `pnpm gen:bundle && pnpm gen:types` | the web part of `make gen` |
| `gen:bundle` **(specified)** | `redocly bundle ../api/openapi.yaml -o ../internal/apispec/openapi.bundled.yaml && redocly bundle ../api/openapi.admin.yaml -o ../internal/apispec/openapi.admin.bundled.yaml` | ADR-020: the bundles live in `internal/apispec`; the prototype (which has no Go tree) bundles into `../api/` |
| `gen:types` **(specified; prototype name `gen:api`)** | `openapi-typescript ../internal/apispec/openapi.admin.bundled.yaml -o src/lib/api/schema.d.ts` | the admin bundle is a superset, so one type file serves both listeners |
| `typecheck` | `tsc --noEmit -p tsconfig.app.json && tsc --noEmit -p tsconfig.node.json` | TypeScript 7 |
| `lint` | `eslint . --max-warnings 0` | warnings are errors |
| `format` / `format:check` | `prettier --write .` / `prettier --check .` | CI runs the check only |
| `test` | `vitest run --project unit --project tooling` | jsdom units and Node tooling tests |
| `test:browser` | `vitest run --project browser` | component tests in real Chromium |
| `e2e` | `playwright test` | against `SCP_E2E_URL` (the real binary) |
| `build` | `vite build && node scripts/finalize.ts && node scripts/precompress.ts && node scripts/verify-dist.ts && node scripts/budget.ts` | the embed build (4.15) |
| `check-pins`, `budget`, `extract` | `node scripts/check-pins.ts`, `node scripts/budget.ts`, `lingui extract --clean` | |
| `check` | `pnpm check-pins && pnpm format:check && pnpm typecheck && pnpm lint && pnpm test` | the local pre-push command; CI runs the same steps as separate jobs |

`pnpm gen` does **not** write `routeTree.gen.ts`: the TanStack router plugin writes it during `vite`, `vite build` and `vitest`, and the drift check is `pnpm build && git diff --exit-code` (the architecture's `make gen` text lists the route tree under `pnpm gen`; recorded as deviation D15).

Scripts under `scripts/` are **type-stripped TypeScript run directly by Node** (`node scripts/x.ts`; Node 24.21 and 26 strip types by default), so there is no build step and no `tsx` dependency; they are limited to erasable syntax (`erasableSyntaxOnly` in `tsconfig.node.json`) and use explicit `.ts` import specifiers (`allowImportingTsExtensions`).

#### 4.1.2 Vite and the plugin chain

`vite.plugins.ts` exports `appPlugins({router, css})`, shared by `vite.config.ts` and `vitest.config.ts` so that tests run against the **same transforms** as production (React Compiler, Lingui macros, virtual routes are not mocked):

```
plugins, in this order:
  tanstackRouter({target:"react", autoCodeSplitting:true, routesDirectory:"./src",
                  virtualRouteConfig:"./routes.ts", generatedRouteTree:"./src/app/routeTree.gen.ts"})   // only when router:true
  react()                                                    // @vitejs/plugin-react 6.1.2 (Oxc transform, refresh)
  babel({ plugins:["@lingui/babel-plugin-lingui-macro"],
          presets:[reactCompilerPreset()] })                  // @rolldown/plugin-babel; compiler + macros in one pass
  lingui()                                                   // @lingui/vite-plugin: import "./messages.po" compiles on the fly
  tailwindcss()                                              // only when css:true (the node "tooling" project has no CSS)
```

Verified in this combination: the Lingui macro (`t` from `@lingui/core/macro`, `Trans` from `@lingui/react/macro`) works in Vite 8 with the React Compiler preset; `.po` files import as modules (`import { messages } from "@/locales/en/messages.po"`, typed through `vite/client`).

`vite.config.ts` (verbatim facts):

- `define: { __BUILD_ID__: JSON.stringify(process.env.BUILD_ID ?? "dev") }`; `RELEASE=1` with no `BUILD_ID` throws at config time.
- `resolve.alias: { "@": "<web>/src" }`.
- `build.target: "baseline-widely-available"`, `sourcemap: false` (GR-19: `webapp` refuses `.map`), `assetsInlineLimit: 0` (no `data:` URIs: `img-src`/`font-src` stay `'self'`), `cssCodeSplit: true`, `manifest: true` (read by `finalize.ts` and `budget.ts`, then moved out of `dist/`).
- `build.rolldownOptions.output.hashCharacters: "hex"` so every hashed name is `name-<8 hex>.<ext>`. Vite 8's default base64url hashes contain `-` and `_`, which broke the first version of `webapp`'s hashed-name rule in the prototype (deviation D3). The entry, chunk and asset names keep Vite's default `[name]-[hash]` templates; only the alphabet is pinned.
- `server: { host: "127.0.0.1", port: 5173, strictPort: true, proxy: { <each proxied prefix>: process.env.SCP_DEV_API ?? "http://127.0.0.1:9090" } }`; the host is pinned because `localhost` resolves to `::1` first on macOS and the proxy target is IPv4 (observed).

#### 4.1.3 TypeScript

`tsconfig.json` has `files: []`, two references and the `paths` `"@/*": ["./src/*"]` (**required**: the shadcn CLI reads `paths` from the root file only; without it `shadcn add` wrote a literal `@/components/ui` directory; verified). `tsconfig.app.json` (browser; includes `src` and `routes.ts`): `target ES2023`, `lib [ES2023, DOM, DOM.Iterable]`, `module ESNext`, `moduleResolution bundler`, `jsx react-jsx`, `types ["vite/client", "trusted-types"]`, and the strict set `strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `noImplicitOverride`, `noImplicitReturns`, `noFallthroughCasesInSwitch`, `noPropertyAccessFromIndexSignature`, `noUnusedLocals`, `noUnusedParameters`, `verbatimModuleSyntax`, `isolatedModules`, `erasableSyntaxOnly`, `noUncheckedSideEffectImports`, `forceConsistentCasingInFileNames`, `skipLibCheck`. `tsconfig.node.json` adds `lib [ES2023, DOM]` (the Playwright specs use `window` inside `page.evaluate`), `types ["node"]`, `allowImportingTsExtensions`, and includes the config files, `scripts` and `e2e`.

`exactOptionalPropertyTypes` makes `prop?: T | undefined` explicit: the shared components that forward optional props declare them that way (`onRetry?: (() => void) | undefined`).

#### 4.1.4 Tailwind and shadcn/ui (Base UI)

- `src/styles/index.css`: `@import "tailwindcss"; @import "tw-animate-css"; @plugin "@tailwindcss/typography"; @import "shadcn/tailwind.css";`, `@custom-variant dark (&:is(.dark *))`, the `@theme inline` block mapping `--color-*` to the variables, `:root` and `.dark` token blocks, a `base` layer, and the `.base-ui-disable-scrollbar` rule (CSP patch 1, below). No `tailwind.config.js`. Fonts: the system stack `ui-sans-serif, system-ui, ...` (deviation D4: no font file is shipped, so `font-src 'self'` has nothing to serve and the budget has 100 KB of headroom).
- `components.json`: `style: "base-nova"`, `rsc: false`, `tsx: true`, `tailwind.css: "src/styles/index.css"`, `baseColor: "neutral"`, `cssVariables: true`, `iconLibrary: "lucide"`, aliases `@/components`, `@/components/ui`, `@/lib`, `@/lib/utils`, `@/hooks`.
- Adding a component: `pnpm exec shadcn add <name>` **with the pinned pnpm first on `PATH`** (the CLI shells out to whatever `pnpm` it finds; pnpm 10 on a pnpm-12 lockfile fails with `ERR_PNPM_UNEXPECTED_STORE`, verified). After the command, the PR must (a) pin every version the CLI wrote (`check-pins` fails otherwise; it wrote `cmdk ^1.1.1`), (b) re-apply the three CSP patches if the component touches them, (c) run `pnpm test:browser` and `pnpm e2e` (the CSP gate).
- Generated and kept (34 files in `src/components/ui`): alert, avatar, badge, breadcrumb, button, card, chart, collapsible, command, dialog, dropdown-menu, empty, field, input-group, input, item, kbd, label, popover, progress, scroll-area, select, separator, sheet, skeleton, spinner, switch, table, tabs, textarea, toast, toggle-group, toggle, tooltip. **Not generated: `sidebar`** (deviation D1: the shell is a header with a nav list and a command palette, which keeps the 360 px layout and the route announcer simple and avoids the sidebar's cookie persistence and `<style>` usage).
- Base UI differences from Radix that feature authors must know: components compose with the `render` prop instead of `asChild`; the generated components import `cn` from the standalone `cn` package directly (there is no `lib/utils.ts`; the `utils` alias in `components.json` is unused).

**The three CSP patches (architecture 11.3), owned code, each with a test or a gate:**

1. `CSPProvider` from `@base-ui/react/csp-provider` with `disableStyleElements` wraps the router in `app/providers.tsx`; Base UI then never injects `<style>` elements. Its scroll-lock class `.base-ui-disable-scrollbar` is defined in `index.css`.
2. `components/ui/chart.tsx`: the generated `ChartStyle` component (an inline `<style>` with per-theme variables) and the `theme` config option are **removed**; series colours are passed as CSS variables through the `style` prop on the container (CSSOM, allowed by `style-src 'self'`).
3. No theme-transition `<style>`: the theme change is a class toggle (4.7); no `next-themes` or provider that injects CSS is installed.

Other owned-code edits: `scroll-area.tsx` unused `React` import removed (TS 7 `noUnusedLocals`). Any further patch is listed in the PR description and in this section.

#### 4.1.5 ESLint, boundaries, Prettier

Flat config (`eslint.config.js`) composed in this order: ignores (`dist`, `.build`, `node_modules`, `playwright-report`, `test-results`, `schema.d.ts`, `routeTree.gen.ts`) -> `@eslint/js` recommended -> `typescript-eslint` `recommendedTypeChecked` with `projectService: true` (all `**/*.{ts,tsx}`; `allowDefaultProject` conflicts with `tsconfig.node.json` includes, observed) plus `no-floating-promises`, `consistent-type-imports` (inline), `no-unused-vars` (`^_`), and `only-throw-error` allowing `Redirect` and `NotFoundError` from `@tanstack/router-core` (route `beforeLoad` throws them by design) -> Node globals for scripts/e2e/config -> the `src` block (boundaries, import-x/no-cycle, react-refresh, react-hooks flat recommended, jsx-a11y recommended, the restricted syntax/globals/imports) -> overrides.

`no-restricted-syntax` (built by `restrictedSyntax({allowApi, urls})`; an override may drop only the **URL** rules, never the HTML-sink, cookie or `AvatarImage` bans: the first draft turned the whole list off for `components/ui/**`, `lib/api/**`, `lib/routes.ts` and `shared/safe-html.tsx`, so a future `shadcn add` bringing a `dangerouslySetInnerHTML` would have passed lint; `safe-html.tsx` alone is exempt from the innerHTML rules, V-12):

| Ban | Selector idea | Escape |
|---|---|---|
| Hard-coded portal path in a string or template | `Literal[value=/^\/(api\|repos\|groups\|commits\|badges\|media\|llms\|dependencies\|packages\|pages\|status\|dashboard)(\/\|$\|\?)/]` except in typed-route contexts: `<Link to>`, `redirect({to})`, `createFileRoute("...")` arguments | `lib/routes.ts`, `lib/api/**`; endpoint paths are allowed in `features/*/api.ts` (`allowApi`) |
| HTML sinks | `dangerouslySetInnerHTML`, `.innerHTML`, `outerHTML =`, `insertAdjacentHTML` | `shared/safe-html.tsx` only |
| `<SafeHtml html={x}>` where `x` is not a member named `html` | selector on the JSX attribute | none (the rule is the type brand, deviation D6) |
| `document.cookie` | | none |
| `<AvatarImage>` | remote avatar (risk W19) | none in v1 |

Other bans: `no-restricted-globals` (`fetch`, `XMLHttpRequest`, `EventSource`, `localStorage`, `sessionStorage`, `eval`; relaxed in `lib/persist.ts` for storage and in `lib/api/**` for `fetch`/`EventSource`), `no-restricted-imports` (`zod`, `react-router`), `no-console` (allow `error`, `warn`), `no-new-func`, `lingui/no-unlocalized-strings` in `features|shared|app/**/*.tsx` (ignore patterns for non-words, kebab-case identifiers, three- or four-letter upper-case zones, the `main h1` selector; ignored attribute names `className type id to href src role key data-testid variant size name kind`). Overrides: `components/ui/**` get `disableTypeChecked`, lose the URL rule and three jsx-a11y rules whose findings are false positives on the generated markup, and keep every sink ban (axe in Playwright is the accessibility gate), `public/**/*.js` get browser globals and `allowEmptyCatch` (the pre-paint script must never throw), `app/root.tsx` and `features/*/route.tsx` are exempt from `react-refresh/only-export-components` (route modules export `Route` next to components by design), tests are exempt from the URL, global and unsafe-assignment rules.

Prettier: `.prettierrc.json` `{semi:false, printWidth:140, trailingComma:"all", plugins:[prettier-plugin-tailwindcss], tailwindStylesheet:"./src/styles/index.css", tailwindFunctions:["cn","cva"]}`; `.prettierignore` excludes generated files and `src/locales`. CI runs `prettier --check` only. **Verified**: `prettier --write .` was applied once and `--check` passes; the whole check suite was re-run after formatting.

### 4.2 Routing

**Typed routes with virtual file routes.** The route tree is code (`routes.ts`, built with `rootRoute`, `route`, `index` from `@tanstack/virtual-file-routes`), not a file-name convention, because the URL shapes are a contract with the Go side and must be reviewable in one file:

```
/                       app/index-redirect.tsx     beforeLoad: throw redirect({to:"/dashboard", replace:true})   (the Go side answers "/" with 302 before the SPA loads; this covers client-side navigation and the dev server)
/dashboard              features/dashboard/route.tsx
/groups/$groupId        features/groups/route.tsx           (layout)
    index               features/groups/overview.tsx
    /commits            features/groups/commits.tsx
/commits                features/commits/route.tsx
/repos/$repo            features/docs/route.tsx             (layout)
    index               features/docs/readme.tsx
    /docs/$             features/docs/doc.tsx               (splat: the repository path WITHOUT its .md/.adoc extension, may contain slashes; unit 18 D-18-01. A URL ending in .md is the server's raw Markdown route, never the SPA, so e2e samples must not end in .md)
    /commits            features/commits/repo-commits.tsx
    /dependencies       features/manifests/repo-dependencies.tsx
/dependencies           features/manifests/dependencies-route.tsx
/packages               features/manifests/package-route.tsx
/pages/$slug            features/pages/route.tsx
/status                 features/status/route.tsx           (adminOnly = ["/status"])
```

`autoCodeSplitting: true` splits every route component into its own lazy chunk (`?tsr-split=component`), so a route's code is fetched only when the route is visited; loaders stay in the entry. The generated `src/app/routeTree.gen.ts` is committed (drift check: `pnpm build` or `vite` regenerates it, CI runs `git diff --exit-code`).

Router options (`app/router.ts`): `context: { queryClient }` (typed by `RouterContext`), `defaultPreload: "intent"` (hover/focus/touch preloads the route chunk and its loader), `defaultPreloadStaleTime: 0` (TanStack Query owns freshness; the router never serves its own cache), `scrollRestoration: true`, `defaultPendingMs: 150` and `defaultPendingMinMs: 300` (a pending component appears only after 150 ms and then stays 300 ms, so a fast response never flashes a skeleton; requested by unit 18); the `Register` module augmentation makes every `<Link to>`, `navigate`, `useParams`, `useSearch` typed: a wrong route, param or search key is a **compile error**, so SPA-internal links need no runtime crawl (GR-15).

**Search params** are validated by Zod (`zod/mini`) in `validateSearch` of each route; a validation failure never shows a blank page: the route's `errorComponent` receives it, and the link builders cannot produce invalid search because they are typed from the same schema. Example (verified, `features/dashboard/search.ts`): `z.object({ q: z.optional(z.string().check(z.maxLength(200))) })`. Search state that the user can share (filters, sort, cursor) lives in the URL, never in component state.

**Route modules** (`route.tsx`) follow one pattern: `createFileRoute("<path>")({ validateSearch, loader: ({context}) => <ensureQueryData>, component, errorComponent?, pendingComponent? })`. **Loaders fail fast** (`retry: false` on the `ensureQueryData` call, deviation D10): with the global retry policy a loader would block rendering for the whole retry schedule (verified: the `not-ready` e2e test timed out). The route's (or the root's) `errorComponent` shows the problem and owns the retry (`ProblemAlert` countdown, then `router.invalidate()`).

**Not-found and errors.** Root `notFoundComponent` renders the "Page not found" page with a link to the dashboard (the Go side answered with a real 404 and the shell for an unknown shape, rule 8 of 06 section 4.10.1; the document status stays 404, verified). Unknown **entities** (`/repos/nope`) are client-side: the query returns `404 not-found`, the route shows `ProblemAlert` with the localised "Not found" title; the status of that document was 200, which is the documented behaviour (ADR-010: "repository existence stays a client-side 404"). The root `errorComponent` (`RootError`) renders `ProblemAlert` with a retry that calls `reset()` and `router.invalidate()`.

**Server-answered URLs** (badges, raw Markdown, `llms.txt`, OpenAPI) are not routes: `lib/routes.ts` exports `serverRoutes` builders (`badge(repo,file)`, `groupBadge`, `readmeMd`, `llms`, `llmsFull`, `openapi`), the only module besides `routes.ts` where path literals may live; segments are `encodeURIComponent`-encoded. A Go test compares their output with the `routes.Table` builders (specified; task M2-6).

**Link crawl (GR-15).** There is no Go-side crawl of SPA links. Instead: (1) typed routes make internal links unbreakable at compile time; (2) generated text surfaces (`llms.txt`, the `.md` files, the sitemap, `url` fields of API payloads and links inside rendered documentation HTML; architecture 20, GR-15) are crawled by an e2e crawl spec **(specified, not prototyped; milestone M2-8; the spec itself is unit 18's E18-CRAWL-01, run in this unit's `e2e` job with the gate fixture)**; (3) Playwright visits one URL per shape in `routes.json` (`e2e/pages.ts`; `scripts/e2e-coverage.test.ts` fails when a public shape has no sample URL or a sample matches no shape; verified).

### 4.3 Generated API client workflow

```
api/openapi.yaml, api/openapi.admin.yaml          hand-written roots (units 06/07; fragments per feature)
  | redocly bundle                                -> internal/apispec/*.bundled.yaml   (committed; embedded; served at /api/v1/openapi.yaml)
  | openapi-typescript 7.13.0                     -> web/src/lib/api/schema.d.ts      (committed; `paths`, `components`, `operations`)
  v
src/lib/api/client.ts   createApi(baseUrl) = openapi-fetch createClient<paths>({baseUrl, fetch: (req) => globalThis.fetch(req)}) + middleware
src/lib/api/client.ts   unwrap(promise) -> data | throws ProblemError
features/*/api.ts       queryOptions factories: the only place a feature knows an endpoint path
```

Rules:

1. **No hand-written fetch types, no `fetch` call outside `lib/api`.** Lint-enforced.
2. The `fetch` option is **late-bound** (`(request) => globalThis.fetch(request)`): `openapi-fetch` otherwise captures `globalThis.fetch` when the client is created, i.e. before MSW (tests) patches it, and every test request would bypass the mocks (observed: `fetch failed`).
3. The middleware `onResponse` reads `X-SCP-Build` into `serverStatus.reportBuild` on **every** response (the build-skew detector).
4. `unwrap` is the single conversion point:

```
unwrap(p):
  try r = await p
  catch e: p = problemFromThrown(e)
           if p.code != "client-aborted": serverStatus.reportFailure()          // a cancelled request (navigation, query removal) says nothing about the server
           throw p
  if r.error !== undefined or !r.response.ok:
      if status >= 500 or status == 429: serverStatus.reportFailure()
      else:                              serverStatus.reportOk()                // any other answer proves the server is reachable: a 404 after an outage clears the banner
      throw problemFromResponse(r.response, r.error)
  serverStatus.reportOk()
  if r.data === undefined: throw ProblemError{status:0, code:"client-contract", title:"Empty response body"}
  return r.data
```

5. **Conditional requests are the browser's job.** Responses carry `Cache-Control: no-cache` and a weak ETag (architecture 11.4); the browser revalidates with `If-None-Match` and hands the SPA a 200 with the cached body when the server says 304, so there is no ETag code in the SPA and `unwrap` never sees a 304 (documented in the code; the e2e server returns real 304s and the polling test passes).
6. **No response validation in production** (ADR-019): types are compile-time only; the contract is held by the Go side (`apitest` response validation, Schemathesis) and the **build-id skew check** (4.4.5) covers the one thing runtime validation would catch in practice, a stale bundle against a new server. `client-contract` exists for the cases that cannot be typed (a 2xx with an empty body).
7. **Contract drift** fails `pnpm typecheck`: CI runs `pnpm gen:types` and `git diff --exit-code`, then typechecks; an additive change is invisible, a breaking change is caught by `oasdiff breaking` (unit 07) before it gets here.

Mapping `Problem` -> UI is in 4.6.

### 4.4 Data fetching, caching, polling, SSE

#### 4.4.1 Query client defaults (`createQueryClient()`; `app/providers.tsx` creates one per app instance with `useState`)

| Option | Value | Why |
|---|---|---|
| `staleTime` | 10 s (`STALE_MS`) | reads within 10 s of each other share one request; the snapshot poll invalidates sooner when data changes |
| `gcTime` | 5 min | |
| `retry` | `retryPolicy`: only `isTransient(error)` and `failureCount < 2` (three attempts in all) | never retry 4xx |
| `retryDelay` | `Retry-After` seconds (capped at 60 s) when the problem carries one, else `min(1000 * 2^attempt, 8000)` ms | honours the server's back-pressure |
| `refetchOnWindowFocus` | true | |
| `refetchInterval` | 120 s (`SWEEP_MS`), `refetchIntervalInBackground: false` | freshness sweep (deviation D11): the snapshot version covers "something changed"; the slow sweep covers `freshness` ageing (a `fresh` item turns `stale` with no version change) and missed polls; a hidden tab does nothing |

#### 4.4.2 Query keys and topics

`qk` builds every key; the **first element is the key root and equals an SSE invalidation topic** (`site`, `catalog`, `commits`, `docs`, `manifests`, `pages`, `status`) plus the composite root `repo` (a repository composite view reads several topics). `topicRoots` maps a topic to the roots to invalidate: `catalog -> [catalog, repo]`, `commits -> [commits, repo]`, `docs -> [docs, repo]`, `manifests -> [manifests, repo]`, `site -> [site]`, `pages -> [pages]`, `status -> [status]`. A feature adds keys to `qk` under its own root; a new topic is an additive change in `ADR-009`, `query-keys.ts` and this table together.

#### 4.4.3 Snapshot polling (v1 baseline, A-17)

`useSnapshotSync()` is mounted **once**, in the shell. `snapshotQuery()` = `GET /api/v1/snapshot` with `refetchInterval: 30_000`, `staleTime: 0`, `retry: false`. On each result it reports `readiness` and `buildId` to `serverStatus`; when `version` differs from the previous value it calls `invalidateAll(qc)` (every query except the snapshot itself). The refetch of the snapshot is a conditional GET answered with a 304 by the server when nothing changed (weak ETag `W/"v<version>"`), so an idle tab costs one tiny request per 30 s; hidden tabs poll nothing (`refetchIntervalInBackground` false). Tested with a version change (names in section 12).

#### 4.4.4 SSE integration (v1.1, flag `live.sse.enabled`) **(specified, not prototyped)**

The hook replaces the poll without changing any consumer:

```ts
// src/lib/api/events.ts   (the only file allowed to use EventSource; lint override)
export function useEventStream(qc: QueryClient): void
```

Behaviour: opens `new EventSource("/api/v1/events")` **once per tab** (module-level guard; `BroadcastChannel` is not used in v1.1: one stream per tab is the ADR-009 limit, four per IP). Events: `hello` -> report ok and the `generation`; `invalidate {topics, generation}` -> `invalidateTopics(qc, topics)`; `resync` -> `invalidateAll(qc)`; comment heartbeats every 15 s are ignored. The browser reconnects by itself with the `Last-Event-ID` it saw (`retry: 3000`); while the stream is `OPEN` the snapshot poll interval is raised to 120 s (a safety net) and while it is `CLOSED` or `CONNECTING` for more than 20 s the poll returns to 30 s. A `404 feature-disabled` on connect switches the hook off for the page's lifetime and keeps polling. The hook closes the stream on `pagehide` and on tab hidden for more than 5 minutes (reopened on `visibilitychange`), so a forgotten tab holds no connection. Payloads are never streamed: the hook only invalidates, the data arrives through normal ETag-validated GETs.

#### 4.4.5 Build-skew detection

`__BUILD_ID__` (compiled in) is compared with `X-SCP-Build` on every API response and with `snapshot.buildId`. A difference sets `serverStatus.buildSkew` (never when the bundle id is `dev`), which makes `ConnectionBanner` offer "A new version of the portal is available" with a **Reload** button (verified in e2e). Navigation to a lazy route whose chunk no longer exists (a deploy replaced every hashed file) raises Vite's `vite:preloadError`; `lib/stale-chunk.ts` (`onPreloadError`, registered by `main.tsx`) reloads **once**, records the time under `scp-reload-guard` and cancels the event **only when it reloads**: Vite rethrows the original error unless the event is cancelled, and a cancelled event makes the failed `import()` resolve to `undefined` (read in `vite/dist/node/chunks/node.js`, `handlePreloadError`), which would surface as an unrelated TypeError. A second failure within 30 s does nothing and leaves the event alone, so the route's error boundary shows the real error. **When the time cannot be stored (blocked site data, a private window) the page never reloads**: the first draft read an absent guard as "never reloaded" and would have reloaded forever (V-07). Unit tests: `lib/stale-chunk.test.ts` (four cases). The Go side answers the missing chunk with a plain-text 404, never HTML (06 rule 7; verified in e2e).

### 4.5 Staleness UI (GR-26, GR-40)

Every view that shows server data shows a `FreshnessBadge` for the `freshness` object of the response (list level) and, for lists of independently refreshed items, one per item (verified: per-group badges on the dashboard). The status vocabulary is the API's: `fresh` -> "Up to date", `stale` -> "Delayed", `error` -> "Update failing", `unknown` -> "Not loaded yet", anything else -> "Unknown"; icon + text, never colour alone, plus `data-freshness`. With `asOf` present the badge shows `RelativeTime` ("5 minutes ago") whose `<time>` carries the exact UTC instant and a zone-formatted title. The relative text re-renders on the shared 30 s ticker, which pauses while the tab is hidden and catches up on `visibilitychange`.

Page-level state, in one live region (`ConnectionBanner`, `role="status"`, polite): build skew > unreachable ("Cannot refresh right now. Showing the last data that loaded.") > warming ("The portal is still loading data from GitHub. Some views may be incomplete."). Unreachable means the last API call failed with a network error, 429 or 5xx; the previous data stays on screen (TanStack Query keeps it), which is the client half of stale-while-error (GR-40). Ticking countdowns are `aria-hidden`; they are never announced.

### 4.6 Error boundaries and problem handling

Layers, innermost first:

1. **Query level**: `unwrap` turns every failure into `ProblemError` (4.3).
2. **Panel level**: `PanelBoundary` (error boundary + `QueryErrorResetBoundary`) wraps each independent panel (latest commits, a status section, a chart); its retry resets the query error and re-renders. One failing panel never blanks a page.
3. **Route level**: a route's `errorComponent` for loader and `validateSearch` failures.
4. **Root level**: `RootError` (router `errorComponent`) with `ProblemAlert` and `router.invalidate()`.
5. **Global hooks** in `main.tsx`: `createRoot({ onUncaughtError, onCaughtError, onRecoverableError })` log through `console.error`/`console.warn` (the e2e gate fails on unexpected `console.error`); `window` `vite:preloadError` as in 4.4.5. There is no `window.onerror` handler of our own and no third-party reporter (open question Q3).

`ProblemAlert` rules: the **title is localised from `code`** (`problemTitle`: `not-found`, `feature-disabled`, `not-ready`, `upstream-unavailable`, `rate-limited`, `queue-full`, `validation-failed`/`invalid-cursor`, `unauthorized`, `forbidden`, `client-network`, `client-aborted`, `internal`, default "Unexpected problem"); the server's `title` and `detail` are **never rendered**, not even for a code this build does not know (they are English, may carry upstream text and are not localisable; the first draft fell back to `error.message`, i.e. the server title, for unknown codes, and a unit test now pins the opposite, V-04); the request id (`<code>` element) is always shown when present so an operator can find the request in the logs (GR-42); for `not-ready`, `rate-limited`, `upstream-unavailable`, `queue-full` and `client-network` with an `onRetry` it shows "Retrying automatically." and an `aria-hidden` countdown of `min(retryAfterSeconds ?? 15, 60)` seconds, then calls `onRetry` and starts the countdown again (a persisting problem is retried once per countdown; the first draft re-fired `onRetry` on every re-render once the counter was 0, because the effect depended on the parent's fresh closure, V-05); a "Try again" button is always present when `onRetry` exists. `validation-failed` field errors are for forms (none in v1); `fieldErrors[].pointer` is a JSON pointer into the request.

Typed errors the SPA distinguishes (all `ProblemError`): API problems by `code`; client codes `client-network`, `client-aborted`, `client-unparseable` (proxy HTML 502, empty 500), `client-contract`. `AbortError` from query cancellation is **not** shown (TanStack Query discards it); it only reaches `ProblemAlert` if a component catches it itself.

### 4.7 Theming (light/dark, tokens, CSP-safe pre-paint)

**Model.** Preference `ThemePreference = system | light | dark`, stored under `scp-theme`; the server may supply a default for visitors with no choice (`Site.defaultTheme`, remembered under `scp-theme-default` so the next load is flash-free). Resolution: stored choice > server default > `system` (`prefers-color-scheme`). The resolved theme is the `dark` class on `<html>`, `style.colorScheme`, and `data-theme=<preference>`; **all three are written through the DOM API (CSSOM), never `style=""` attribute strings or `<style>` elements**.

**Pre-paint without inline script.** `index.html` has no inline script or style; it loads the **external, blocking, same-origin** `/theme-init.js` in `<head>` (`public/theme-init.js`, 433 bytes, hand-written and excluded from Prettier, copied verbatim to `dist/`), which reads the two keys inside `try/catch`, resolves exactly as `lib/theme.ts` does and applies the class, `colorScheme` and `data-theme` before first paint. `<meta name="color-scheme" content="light dark">` makes the pre-script canvas match. The script is `no-cache` + ETag (06 D22, open question Q5 on hashing it). **Two tests keep the script and `theme.ts` in step**: (1) a unit test requires the script text to contain both storage keys of `storage-keys.ts` and no `eval|innerHTML|cssText|fetch|XMLHttpRequest|setAttribute('style'`; (2) a unit test evaluates the script for all 32 combinations of stored choice (4) x server default (4) x OS scheme (2) and requires it to decide the same as `theme.apply()`. E2E proves it works **without the app bundle** (the bundle request is aborted, the `dark` class is still set).

**Tokens.** shadcn CSS variables in `:root` and `.dark` (OKLCH). Overrides of the shadcn neutral defaults, each pinned by the contrast test (`src/lib/tokens.contrast.test.ts`, a TypeScript port of `05-research/08-prototypes/contrast.py`, verified over 11 pairs x 2 themes):

| Token | Light | Dark | Requirement |
|---|---|---|---|
| `--muted-foreground` | `oklch(0.5 0 0)` | `oklch(0.708 0 0)` | >= 4.5:1 on `background` and `muted` (WCAG 1.4.3) |
| `--input` (control border) | `oklch(0.62 0 0)` | `oklch(0.52 0 0)` | >= 3:1 on `background` (1.4.11) |
| `--ring` (focus indicator) | `oklch(0.6 0 0)` | `oklch(0.65 0 0)` | >= 3:1 on `background` (1.4.11; 2.4.13 Focus Appearance is Level AAA in WCAG 2.2 and is not claimed) |
| `--destructive` | `oklch(0.55 0.2 27)` | `oklch(0.704 0.191 22.216)` | >= 4.5:1 on `background` |
| foreground/background pairs of `card`, `popover`, `primary`, `secondary`, `accent` | shadcn values | shadcn values | >= 4.5:1 |

Alpha tokens (`--border` in dark) are composited over `--background` in gamma-encoded sRGB before the ratio is computed (browsers composite in sRGB; the test does the same). When unit 18's `tokens.css` becomes the value source (section 1.5) the same test reads that file, and the extra semantic pairs (`success`, `warning`, `danger`, `info` on their `-bg`) are added to `PAIRS`.

Highlighted code (server-rendered Chroma classes) has its own light and dark palettes in unit 18's `docs.css`; the foundation only guarantees that `.dark` switches them (class-based, no `prefers-color-scheme` media query, so the toggle works).

### 4.8 Accessibility baseline (WCAG 2.2 AA)

Built in to the foundation; every feature inherits it:

- Landmarks: skip link (first focusable, `href="#main"`), `<header>`, `<nav aria-label="Primary">`, one `<main id="main">`, one `<h1>` per route (axe `page-has-heading-one`; the e2e asserts `main h1` visible on every shape).
- `RouteAnnouncer`: after each client navigation that changes the **pathname** (not the first load, not a search-only change) focus moves to `main h1` (`tabindex="-1"`) and `document.title` is announced in a polite live region. Titles are set with React 19's `<title>` element in each route component.
- Focus: visible 3:1 indicator through `--ring`; Base UI dialogs/popovers trap and restore focus; no positive `tabindex`; `ExternalLink` announces "(opens in a new tab)".
- Colour is never the only carrier (staleness has text, links are underlined in prose).
- Target size >= 24x24 CSS px (WCAG 2.2 2.5.8) for icon buttons (`size="icon"`), checked by axe `target-size` in the axe run (tag `wcag22aa`).
- Reflow (WCAG 1.4.10, threshold **320 CSS px**): the layout target is 360 px (architecture 12.3), the Playwright `phone` project (360x740) runs the same suite and asserts `scrollWidth <= clientWidth` on every page, and one extra test resizes to **320x640** and asserts the same for every sample URL (the first draft proved only 360, which is not the criterion's width); the check caught a real defect in the first shell (65 px of overflow because the header and nav did not wrap), fixed with `flex-wrap` (verified).
- `prefers-reduced-motion`: `tw-animate-css` animations are disabled by shadcn's `motion-reduce:` variants; no feature may auto-play animation.
- Live regions: exactly one `ConnectionBanner` status region and one route announcer; features add `aria-live` only for results of a user action (search result counts).
- Dependency graph (unit 18) is an enhancement over an accessible table with the same facts.

Gate: `@axe-core/playwright` with tags `wcag2a wcag2aa wcag21a wcag21aa wcag22aa` on 11 pages x 2 themes x 2 viewports (verified: 0 violations).

### 4.9 Internationalisation readiness

Lingui 6.9.0, source locale `en`, catalogues `src/locales/{locale}/messages.po` (committed; `lingui extract --clean` in `pnpm extract`; CI fails if `git diff` is not empty after extract). Strings are authored with the compile-time macros `t` (`@lingui/core/macro`) and `<Trans>` (`@lingui/react/macro`), which the Babel plugin turns into message ids at build time. `lib/i18n.ts` exports the `i18n` singleton and `initI18n(locale)` (`i18n.load(locale, messages)` + `activate`); `I18nProvider` wraps the app; tests call `initI18n()` in `setup.ts`. Rules: no concatenated sentences (use placeholders: `Request id: <code>{id}</code>`), no hard-coded plural logic (use `plural()`), dates and numbers through `Intl` (`formatRelative`, `formatDateTime`; the zone comes from `Site.displayTimeZone`, the locale from `i18n.locale`), `<html lang>` is `en` in `index.html` and setting it from the active locale is part of adding a second locale. `lingui/no-unlocalized-strings` fails the lint for user-visible literals in features, shared and app. v1 ships **English only** (`locales: ["en"]`); adding a locale is: add it to `lingui.config.ts`, `pnpm extract`, translate the `.po`, load it with a dynamic `import()` in `initI18n` (the catalogue is a separate chunk, <= 20 KB). API-supplied text that is displayed as is (`Site.title`, group display names, announcement text, doc HTML) is content, not UI, and is not translated. Right-to-left support is out of scope (`rtl: false` in `components.json`).

### 4.10 Untrusted content (`SafeHtml`)

`SafeHtml` is the **only** module that turns a string into DOM. It creates the Trusted Types policy `scp-html` once (`trustedTypes.createPolicy("scp-html", { createHTML: (s) => s })`, identity because the input is **already sanitised by the backend** `safehtml.SafeHTML`, ADR-005; adding DOMPurify would silently alter golden-tested output) and renders `<div dangerouslySetInnerHTML={{__html: policy.createHTML(html)}}>`. Under the production CSP (`require-trusted-types-for 'script'; trusted-types scp-html`) any other sink throws (verified in e2e: `document.body.innerHTML = "<b>x</b>"` is refused with "This document requires 'TrustedHTML' assignment"). The lint rules of 4.1.5 make the bypass structurally impossible in feature code, and the selector requires the argument to be a member named `html` (the API field name for sanitised HTML), so a plain string cannot be passed by accident (deviation D6: a compile-time brand was prototyped and abandoned, `openapi-fetch`'s `Readable<T>` mapping strips intersection brands). The policy is created at module load, once (a second `createPolicy("scp-html")` would throw under `trusted-types scp-html` without `allow-duplicates`); where Trusted Types is unsupported the policy is `null` and the string is passed through (the CSP directive is then not enforced either).

### 4.11 Performance budgets

Measured on **gzip -9** of what a cold request downloads, by `scripts/budget.ts` from the Vite manifest (kept in `.build/`, not served). The script walks the entry's static-import closure (**including shared chunks**, each CSS file counted once and a route's own CSS counted in its chunk budget; the first draft dropped a CSS file listed by two initial chunks instead of counting it once: Rolldown places app code in a shared chunk named after a helper, e.g. `preload-helper-<hash>.js`, so reading only the entry file under-counts and, in `finalize.ts`, hid the build id) and applies:

| Budget | Limit | Measured (prototype, 2026-10-14) |
|---|---|---|
| initial JS (entry + static imports) | 200 KB | **139.0 KB** (React 19, router, query, Lingui runtime, shell) |
| initial CSS | 25 KB | **17.5 KB** |
| any lazy route chunk | 60 KB | <= 3.3 KB (stubs; unit 18 re-measures) |
| graph chunk (sigma) | 70 KB | n/a (unit 18) |
| charts chunk (recharts) | 120 KB | n/a (unit 18) |
| fonts | 100 KB | 0 (system stack) |
| cold `/dashboard` transfer (HTML + JS + CSS + 3 API calls, brotli) | 350 KB | not measured (Lighthouse nightly, M3) |

A budget that fails fails `pnpm build` (non-zero exit). The numbers are limits, not targets: unit 18's features must stay inside them, and the **per-route chunk list in the output names the offender**. `precompress.ts` writes brotli q11 and gzip -9 siblings (kept only when smaller; files >= 1 KiB; `.html .js .mjs .css .json .webmanifest .svg .txt .ico`) and prints raw 539 KiB, gzip 160 KiB, brotli 140 KiB for the prototype. Lighthouse 13.5.0 (Node API, not `@lhci/cli`) runs nightly against the built binary as a regression alarm (three runs, median), not as a pass/fail on absolute scores; `size-limit` is not used (deviation D2).

Runtime performance rules: route components are lazy by construction; lists over 100 rows use `@tanstack/react-virtual` (unit 18); the React Compiler removes manual `memo`/`useMemo` (do not add them; the `react-hooks` compiler rules are errors); images are `loading="lazy"` with explicit dimensions (the only raster content is `/media/...` inside server HTML).

### 4.12 Testing stack and CI gates

| Layer | Tool | Where | What it proves |
|---|---|---|---|
| Unit and component (jsdom) | Vitest 5.0.3 `unit` project, Testing Library 16, jest-dom, user-event | `src/**/*.test.ts(x)` | pure logic, components, hooks, MSW-backed queries |
| Tooling (Node) | Vitest `tooling` project | `scripts/**/*.test.ts` | the route-shapes golden, the lint-rule fixtures |
| Component in a real browser | Vitest `browser` project (`@vitest/browser-playwright`, Chromium, headless) + `vitest-browser-react` | `src/**/*.browser.test.tsx` | layout and real CSS (the `index.css` import is in `setup-browser.ts`; the unit project blanks CSS) |
| Network | MSW 3.0.2 with `setupServer()` and `openapi-msw` `createOpenApiHttp<paths>`; `server.listen({ onUnhandledFrame: "error" })` | `src/test/server.ts`, `setup.ts` | handlers typed from the generated `paths` (a wrong path, status or body is a compile error); **any unmocked request fails the test** |
| End to end | Playwright 1.63.0 (`desktop` 1280x800, `phone` 360x740) against the **real Go binary** (`-tags spa`) | `e2e/*.spec.ts` | routing, real CSP, Trusted Types, compression, caching headers, axe, error paths |
| Accessibility | `@axe-core/playwright` 4.13.0 (axe-core 4.14.0) | `e2e/routes.spec.ts` | WCAG 2.2 AA on every shape, both themes |
| Contract | `pnpm gen:types` + drift + typecheck; Schemathesis and `oasdiff` in the api job (unit 07) | CI | the SPA's types equal the bundled OpenAPI |

**The Playwright gate fixture** (`e2e/fixtures.ts`, `auto: true`, applies to every test): installs a `securitypolicyviolation` listener before any page script, records `console.error` (except "Failed to load resource" noise and patterns a test explicitly allows through the `allowConsole` option) and `pageerror`, and fails the test at the end if any list is non-empty. This is the CSP gate of ADR-010 and risk W11: an upgrade that makes a component inject `<style>` fails here.

**Determinism.** No test uses wall-clock time or sleeps: `vi.useFakeTimers()` with `vi.setSystemTime(...)` and per-tick `act()` for timers (`ProblemAlert` countdown, `useNow`), a fixed `now` argument for `formatRelative`, server fixtures with fixed timestamps (`e2e/fixtures/api/*.json`, `builtAt: 2026-10-14T09:41:07Z`), the API is never real in unit tests (MSW strict), Playwright waits on state (`expect(...).toBeVisible()`, `toHaveURL`) never on time, retries are off locally and 1 in CI (`retries: CI ? 1 : 0`, `forbidOnly` in CI). Random ids are not used by the foundation.

**CI jobs (web part of architecture 22).** `web`: `pnpm install --frozen-lockfile`; `pnpm gen:types` + `git diff --exit-code` (schema, route tree, `.po` after `extract`); `check-pins`; `format:check`; `typecheck`; `lint`; `test`; `test:browser`; `build` (includes `verify-dist` and `budget`). `e2e`: `go build -tags spa` of the real binary with `BUILD_ID=<sha>` passed to both `pnpm build` and `-ldflags`, run against `cmd/fakegithub`, `pnpm e2e` (Chromium), upload traces on failure. `nightly`: Lighthouse, long Playwright run with `--repeat-each=3` (flake hunt). Branch protection requires `web` and `e2e`.

### 4.13 Storybook decision

**Not adopted** (D-16 left it optional). With about 35 owned components and a browser-mode Vitest project that renders each in real Chromium with the real CSS, Storybook would add a second Vite configuration, a second set of plugins (it must replicate the React Compiler, Lingui and Tailwind chain), its own CSP exceptions and a 10.x dependency tree, without a gate the existing stack lacks. Revisit if a design-review need appears (open question Q9). `msw-storybook-addon` 3.0.3 stays unused.

### 4.14 Dev proxy and the dev loop

`make dev` runs the Go server against the fake GitHub (`go run ./cmd/scp serve --config deploy/dev.yaml`, port 9090) and `pnpm -C web dev` (Vite on 127.0.0.1:5173). The proxy forwards `/api`, `/badges`, `/media`, `/schemas`, `/llms.txt`, `/llms-full.txt`, `/sitemap.xml`, `/robots.txt`, `/csp-reports` and the regular expression `^/(repos|groups)/.+\.(md|txt)$` (the server-answered files under SPA-looking prefixes: `/repos/{repo}/readme.md`, `/groups/{id}/llms.txt`, `.../dependencies.md`; without it Vite answers them itself; verified against a running stand-in, V-22) to `SCP_DEV_API` (default `http://127.0.0.1:9090`). Content negotiation on `/repos/{repo}/docs/{key}` (`Accept: text/markdown`) is not reproducible through the proxy and is tested only in the e2e job; everything else is Vite's (HMR, `/theme-init.js` from `public/`, the history fallback). Verified through the dev server: `/` 200, `/dashboard` 200, `/api/v1/snapshot` 200 JSON through the proxy, `/theme-init.js` 200. **The dev server does not enforce the production CSP** (HMR needs inline module code and the React refresh preamble); the production policy is exercised only by the e2e job against the real binary, which is why that job is required. A developer who wants the real thing locally runs `pnpm build && go run -tags spa ./cmd/scp ...` or the e2e server of the assets.

### 4.15 Supply chain and the embed build

**pnpm 12 policy** (`pnpm-workspace.yaml`, verified in the install): `minimumReleaseAge: 10080` (7 days, minutes), `trustPolicy: no-downgrade`, `trustPolicyIgnoreAfter: 129600` (90 days, in minutes; without it the install failed on `High-risk trust downgrade for semver@6.3.1`, a package whose old versions predate provenance, D8), `blockExoticSubdeps: true`, `strictDepBuilds: true` with `allowBuilds: { unrs-resolver: false }` (a native-build dependency of the import resolver: the build script is **denied**, the resolver falls back to its WASM/JS path; without the entry `pnpm install` fails with `ERR_PNPM_IGNORED_BUILDS`, D8). New build scripts are allowed only by a reviewed line in this file. The lockfile is committed; CI uses `--frozen-lockfile`. Actions are SHA-pinned (unit 02/CI). Renovate (7-day `minimumReleaseAge`) updates `package.json` pins and the lockfile together.

**The embed build** (what `pnpm build` does, in order; any failure stops the chain):

```
1 vite build                 -> dist/  (index.html, assets/*-<8 hex>.{js,css}, public/* copied: theme-init.js favicon.svg manifest.webmanifest) and dist/.vite/manifest.json
2 finalize.ts                move the Vite manifest to .build/manifest.json (not served); write dist/routes.json
                             {"version":1,"buildId":BUILD_ID,"routes":shapes()}; assert BUILD_ID occurs in the initial chunks
3 precompress.ts             brotli q11 + gzip -9 siblings for compressible files >= 1 KiB, kept only when smaller
4 verify-dist.ts             the checks of webapp.New (below) so a bad bundle fails here, not at server start
5 budget.ts                  the budgets of 4.11
```

`verify-dist.ts` rejects: symlinks, files > 8 MiB, dist > 16 MiB, `*.map`, extensions outside the allow-list (`.html .js .mjs .css .json .webmanifest .svg .png .jpg .jpeg .gif .webp .avif .ico .woff2 .woff .txt .br .gz`), a second `.html`, any file under `api/` (it would shadow the API; the first draft listed this in 7.2 but did not implement it), an `assets/` file without an `-<8 hex>` hash, orphan or non-round-tripping `.br`/`.gz` siblings (both are **decompressed and compared**, which also closes the 06 gap that Go cannot decode brotli), a compressible file >= 1 KiB without a `.br` sibling, a missing `index.html`, `routes.json` or `theme-init.js`, an inline `<script>` without `src`, any `<style>` or `style=` attribute, any `on*=` handler, any absolute `http(s)://` URL outside `<noscript>`, and a `RELEASE=1` build whose `routes.json` buildId is `dev`.

**Handing over to Go.** `web/dist` is embedded by `web/embed_spa.go` (`//go:build spa`, `//go:embed all:dist`); without the tag the committed `web/placeholder/` is embedded and `go test ./...` needs no Node (unit 06). `BUILD_ID` is the git SHA, passed to `pnpm build` (the `define` and `routes.json`) **and** to `go build -ldflags "-X main.build=$BUILD_ID"`; `webapp.New` with `CheckBuild` refuses a `dist` whose `routes.json` build id differs (stale dist). Verified end to end: the prototype's e2e server loads the real build through `webapp.New(os.DirFS("dist"), Options{BuildID:"abc123def456", CheckBuild:true, RequirePrecompressed:true, ...})` and serves it under `csp=enforce`.

### 4.16 Edge cases and failure modes

| Situation | Behaviour |
|---|---|
| API unreachable on first load | `/dashboard` loader fails fast -> `RootError` shows "Cannot reach the portal" with an auto-retry countdown; `ConnectionBanner` shows unreachable once the shell renders; never a blank page |
| API 503 `not-ready` on first load (cold start) | `ProblemAlert` "The portal is still warming up", `Retry-After` honoured, request id shown, retry automatic (e2e verified) |
| Snapshot says `warming` | banner "still loading data from GitHub"; views render what they have with their own `freshness` |
| A deploy while a tab is open | next API response carries a new `X-SCP-Build` -> reload banner; a missing lazy chunk -> `vite:preloadError` -> one guarded reload |
| `localStorage` blocked or full (private window) | `readPersisted` returns `null`, `writePersisted` swallows; theme falls back to `system`; the page works (unit tested) |
| JavaScript disabled | `<noscript>` explains and links to `/llms.txt` and `/api/v1/openapi.yaml` (server-rendered surfaces) |
| Unknown enum value from a newer server | open-enum fallbacks (`FreshnessBadge`: "Unknown"); `switch` without `default` is a review rejection |
| Search param invalid (`?q=` too long) | route `errorComponent` shows the problem; typed links cannot generate it |
| Very long repository slug or doc path | `%2F`-safe: slugs are single segments; doc ids use the splat and are `encodeURIComponent`-ed per segment by the route helper |
| A slug that looks like a file (`/repos/next.js`) | matches the SPA shape on the Go side before the extension rule (06 D5); the SPA treats it as a normal param |
| An external link with a non-web scheme in `Site.nav` | `ExternalLink` renders it as plain text (`isWebUrl`; tested) |
| Clock skew between browser and server | relative times are computed against the browser clock; a negative difference renders "in x minutes"; `asOf` is never shown as the browser's time |
| The admin listener serves the same bundle | the route manifest of the admin listener is `/status` only and `GET /` answers 302 `/status` (`AdminRedirect`); the current route table (01, `routes.Admin()`) registers `api.site` and `api.snapshot` as well, so the shell works there unchanged and its nav links to public pages navigate client-side (their API calls succeed); a reload of such a URL is a real 404 shell, by design. Unit 18's D-18-09 assumed the opposite (no site/snapshot on admin); until Q11 is answered the shell tolerates a 404 from both (no banner: a 404 is not a server failure) |
| Storage blocked and a chunk is missing after a deploy | no automatic reload (4.4.5); the route's error boundary shows the failure and the user reloads by hand |
| Two tabs | each polls independently (304s); SSE (v1.1) is one stream per tab within the per-IP limit of 4; a 5th stream gets 429 -> the hook falls back to polling |

---
## 5. Data

The SPA owns **no durable data**. Everything it holds is a cache of server state or a per-viewer convenience.

### 5.1 Browser storage (the complete list)

All access goes through `lib/persist.ts` (`readPersisted(key)`, `writePersisted(key, value | null)`), which wraps every call in `try/catch` (private windows, blocked site data and full quotas throw) and never throws; the page renders correctly with storage unavailable. Keys are constants in `lib/storage-keys.ts`:

| Key | Value | Writer | Reader | Purpose |
|---|---|---|---|---|
| `scp-theme` | `light` \| `dark` \| `system` | `ThemeToggle` via `setPreference` | `theme-init.js`, `lib/theme.ts` | the viewer's explicit choice |
| `scp-theme-default` | `light` \| `dark` \| `system` | `applyServerDefault(Site.defaultTheme)` | `theme-init.js`, `lib/theme.ts` | remembers the server default so the **next** load is flash-free |
| `scp-announcement-dismissed` | announcement `id` (content hash) | `AnnouncementBanner` | same | a changed announcement text shows again |
| `scp-reload-guard` | epoch milliseconds | `main.tsx` | same | at most one automatic reload per 30 s after a stale-chunk failure |

No cookies (`document.cookie` is lint-banned; there is no session). No `sessionStorage`, no IndexedDB, no service worker, no Cache API. Nothing identifying a person is stored.

### 5.2 In-memory caches

| Cache | Owner | Lifetime / policy |
|---|---|---|
| TanStack Query cache | `QueryClient` per app instance | `staleTime` 10 s, `gcTime` 5 min, invalidated by the snapshot version, topics (SSE v1.1) and the 120 s sweep (4.4) |
| Browser HTTP cache | the browser | `no-cache` + ETag on API and shell; hashed `assets/*` `immutable` for a year; revalidation is automatic (4.3) |
| Router cache | TanStack Router | preload cache with `defaultPreloadStaleTime: 0`: the router keeps no data of its own |
| `serverStatus` store | module singleton | session; reset in tests |
| `useNow` ticker | module singleton | one `setInterval` while at least one `RelativeTime` is mounted |

### 5.3 Committed generated files and fixtures

| File | Generated by | Drift check |
|---|---|---|
| `src/lib/api/schema.d.ts` | `openapi-typescript` from `internal/apispec/openapi.admin.bundled.yaml` | `pnpm gen:types && git diff --exit-code` |
| `src/app/routeTree.gen.ts` | the TanStack router plugin (during `vite`/`vite build`) | `pnpm build && git diff --exit-code` |
| `src/locales/en/messages.po` | `lingui extract --clean` | `pnpm extract && git diff --exit-code` |
| `pnpm-lock.yaml` | pnpm | `--frozen-lockfile` |
| `e2e/fixtures/api/*.json` | hand-written, schema-checked | a unit test validates each fixture against `components` types by compile-time `satisfies` (specified: task M2-5); in the prototype they are plain JSON served by the e2e server |

Build outputs not committed: `dist/`, `.build/`, `playwright-report/`, `test-results/`.

### 5.4 Data the SPA reads (endpoint map)

The SPA calls only `GET` endpoints of `/api/v1` (24 operations in the catalogue, unit 07). Foundation-level consumers:

| operationId | Path | Consumer in this unit | Freshness behaviour |
|---|---|---|---|
| `getSnapshot` | `GET /api/v1/snapshot` | `useSnapshotSync` (every 30 s, ETag/304) | `version`, `builtAt`, `readiness`, `buildId` |
| `getSite` | `GET /api/v1/site` | `siteQuery` (staleTime 60 s): shell title, nav, announcement, default theme, display zone, header status, feature flags (`features.manifests`) | invalidated by topic `site` |
| `listGroups` | `GET /api/v1/groups` | reference slice `dashboard` | `freshness` envelope |
| `getPage` | `GET /api/v1/pages/{slug}` | reference slice `pages` (`html` field through `SafeHtml`) | per-resource ETag |
| `streamEvents` | `GET /api/v1/events` (v1.1) | `useEventStream` | `text/event-stream`: `hello`, `invalidate`, `resync` |

All other operations (`listRepos`, `getRepo`, `listCommits`, `listRepoDocs`, `getRepoDoc`, `listPages`, `getRepoManifests`, `listRepoDependencies`, `getDependencyGraph`, `getDependencyVersions`, `getDependencyLicenses`, `getComponent`, `listComponentDependents`, `getStatus`) belong to unit 18's slices. `getOpenApiDocument`, `getRepoSbom` and the admin `retryJob` are not called by the SPA (the first two are linked, `retryJob` is a POST that v1 does not expose in the UI).

---

## 6. Configuration

The foundation has **no Go configuration keys** of its own (`config_keys_owned` is empty): `web.csp.mode` (and the other `web.*` keys) belong to unit 06's `webapp.Config`. This unit's configuration is build-time and dev-time, plus values the SPA reads from the API. Every item below states type, default, validation, name and consumer.

### 6.1 Build, dev and test environment variables

| Name | Type | Default | Validation | Consumer |
|---|---|---|---|---|
| `BUILD_ID` | string | `dev` | non-empty; with `RELEASE=1` must not be `dev` (config-time throw in `vite.config.ts`, again in `verify-dist.ts`) | `vite.config.ts` (`__BUILD_ID__`), `finalize.ts` (`routes.json` `buildId`), `webapp` (`CheckBuild`); must equal `go build -ldflags "-X main.build=..."` |
| `RELEASE` | `"1"` or unset | unset | only `"1"` is meaningful | `vite.config.ts`, `verify-dist.ts` |
| `SCP_DEV_API` | URL | `http://127.0.0.1:9090` | must be an `http(s)` origin (Vite proxy `target`) | `vite.config.ts` dev proxy |
| `SCP_E2E_URL` | URL | `http://127.0.0.1:9090` | an origin serving the **real** binary | `playwright.config.ts` `baseURL` |
| `PW_CHANNEL` | string | unset (bundled Chromium) | a Playwright channel such as `chrome` | `playwright.config.ts`, `vitest.config.ts` browser provider |
| `CI` | any | unset | | `playwright.config.ts` (`retries`, `forbidOnly`) |

`BUILD_ID`, `RELEASE` and `CI` are consumed at build or test time only; none reaches the browser except `BUILD_ID` through the compile-time constant.

### 6.2 Static build configuration (files, reviewed like code)

| File | Keys that matter |
|---|---|
| `package.json` | exact pins; `engines.node`; `devEngines.packageManager`; scripts (4.1.1) |
| `pnpm-workspace.yaml` | `minimumReleaseAge: 10080` (minutes), `trustPolicy: no-downgrade`, `trustPolicyIgnoreAfter: 129600` (minutes), `blockExoticSubdeps: true`, `strictDepBuilds: true`, `allowBuilds: {unrs-resolver: false}` |
| `.nvmrc` | `24.21.0` |
| `components.json` | shadcn `style`, aliases, CSS path |
| `lingui.config.ts` | `sourceLocale: "en"`, `locales: ["en"]`, `catalogs[0].path` `<rootDir>/src/locales/{locale}/messages`, `format: formatter({lineNumbers:false})` (line numbers off so unrelated edits do not churn the `.po` diff) |
| `eslint.config.js` | `FEATURE_MAY_IMPORT` (exported; imported by `scripts/lint-rules.test.ts`), restricted syntax, element policies |
| `routes.ts` | the route tree and `adminOnly` (the only place a route is added) |
| `scripts/budget.ts` | the budget constants (200 KB / 25 KB / 60 KB / 70 KB / 120 KB) |

### 6.3 Values the SPA reads from the API (`getSite`, owned by unit `overview`)

| Field | Type | Default when absent | SPA behaviour |
|---|---|---|---|
| `title` | string | "Source Code Portal" (localised fallback) | header brand text |
| `defaultTheme` | `system` \| `light` \| `dark` | `system` | `applyServerDefault` |
| `displayTimeZone` | IANA zone string | `UTC` | `setDisplayZone` -> every `RelativeTime` title and `formatDateTime` |
| `lastFullSync`, `repoCount` | `Timestamp \| null`, integer | "Not synced yet", hidden until `Site` loads | `HeaderStatus` |
| `features.manifests` | boolean | false | shows the Dependencies nav entry |
| `nav[]` | `{label, kind: page \| link, page?, url?}` | empty | `<Link to="/pages/$slug">` or `ExternalLink` |
| `announcement` | `{id, text, level: info \| warning \| critical, dismissible}` or absent | none | `AnnouncementBanner` (plain text, `critical` uses the destructive variant) |

The `site:` configuration keys behind these belong to unit `overview`/`platform/config`; the SPA never reads config files.

### 6.4 Storage keys

Listed in 5.1 (they are configuration of the browser side: names are constants, never user input).

---

## 7. HTTP/API surface owned

### 7.1 Server routes owned

None. This unit adds no handler. It produces static files that `webapp` serves.

### 7.2 The dist contract (what `web/dist` must look like; the interface to unit 06)

| Path in `dist/` | Served as | Notes |
|---|---|---|
| `index.html` | the SPA shell; 200 for shapes in `routes.json`, 404 with the same body otherwise (06 4.10.1 rule 8) | the only HTML file; no inline script, no inline style, no `on*=`, no absolute URL outside `<noscript>`; `<script src="/theme-init.js">` in `<head>`; `<script type="module" src="/assets/index-<hash>.js">`, `<link rel="stylesheet" href="/assets/index-<hash>.css">` injected by Vite |
| `assets/<name>-<8 hex>.js\|.css` | `public, max-age=31536000, immutable` | every file under `assets/` has a content hash; lazy chunks, CSS chunks |
| `assets/*.br`, `assets/*.gz` and the same for `.html .json .svg .webmanifest .txt .ico` and `theme-init.js` (>= 1 KiB) | selected by `Accept-Encoding` (br > gzip > identity) | round-trip-checked by `verify-dist.ts` |
| `theme-init.js` | `no-cache` + ETag, blocking in `<head>` | 433 bytes; **must be byte-stable between deploys unless the theme logic changed** |
| `favicon.svg`, `manifest.webmanifest` | `no-cache` + ETag | `manifest-src 'self'`, `icon` is the SVG |
| `routes.json` | **not served** (consumed by `webapp.New`, then removed from the file set) | `{"version":1,"buildId":"...","routes":[{"pattern","listeners"}]}`, format of 06 section 3.5 |

Everything else is a `verify-dist` failure: no `.map`, no fonts in v1, no raster images (all raster content is `/media/...` at runtime), no second `.html`, no file under `api/`, no symlinks, nothing over 8 MiB, total under 16 MiB.

**Cross-unit requirements recorded here (for 06 and 01):**

1. `webapp`'s hashed-name rule: unit 06 (as verified at 03:06, V1) accepts `<name>-<8 characters of [A-Za-z0-9_-]>` and rejects unhashed names; this build emits `name-<8 hex>.<ext>` (D3), which is a subset of that rule, so either side may change its alphabet without breaking the other. The first draft's request that 06 be made robust is **met** (06 section 4.9). The contract is: exactly 8 hash characters after the last `-`, at least one name character before it.
2. `routes.json` is generated from `routes.ts` by `finalize.ts`; its `routes` equal `routes.Public().SPAShapes()` for public shapes plus the admin-only ones (06 and 01 seam test; the TS side is pinned by the golden test of section 12).
3. `X-SCP-Build` must be present on **API** responses as well as static ones (the build-skew detector reads it from every API response; the e2e server sets it on all). `httpapi` middleware owns this.
4. The reserved-prefix list passed to `webapp` and the proxy list in `vite.config.ts` (`/api /badges /media /llms.txt /llms-full.txt /csp-reports`) must stay consistent; the `/_frame/` prefix (Mermaid, v1.1) is added to both when it lands.
5. `/csp-reports` must stay same-origin POST, 64 KiB cap (ADR-010 m9); the SPA never sends reports itself (the browser does).

### 7.3 Endpoints and headers the SPA relies on

- Every API GET answers with `Cache-Control: no-cache` and a weak ETag so the browser revalidates; problems are `application/problem+json` with `code`; `Retry-After` on 429/503; `X-Request-Id` on every response.
- The shell has the document CSP (06 4.10.3); the SPA is written for the **enforced** policy `default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; require-trusted-types-for 'script'; trusted-types scp-html; report-to csp`. Consequences accepted by design: no inline anything, no `eval`, no `data:` images or fonts, no third-party origin, `fetch`/`EventSource` only to the same origin.

---

## 8. Concurrency and lifecycle

The runtime is the browser's single JavaScript thread; "concurrency" is the ordering of timers, requests and renders.

### 8.1 Application lifecycle

```
HTML parse -> /theme-init.js (blocking, sets class)           ; first paint has the right theme
-> module entry: main.tsx
     add vite:preloadError handler (reload guard)
     initI18n()  (load + activate "en")
     createRoot(...).render(<StrictMode><App/></StrictMode>)
-> App: QueryClient created once (useState initialiser), router created once, effects: theme apply + watch(prefers-color-scheme)
-> Router matches the URL; loader -> ensureQueryData (fail fast); component chunk loads lazily
-> Shell effects: useSnapshotSync (poll every 30 s), site query (60 s stale), theme default
-> RouteAnnouncer moves focus on navigation
```

StrictMode runs effects twice in development only; every effect here is idempotent and has a cleanup (timers cleared, listeners removed, `matchMedia` listener removed, ticker reference-counted).

### 8.2 Timers and subscriptions (the complete list)

| Timer / subscription | Started by | Period | Stopped by |
|---|---|---|---|
| snapshot poll | `useSnapshotSync` (`refetchInterval`) | 30 s, only while the tab is visible | unmount of the shell |
| freshness sweep | query defaults | 120 s per active query, only while visible | query unmount |
| `useNow` ticker | first `RelativeTime` mounted | 30 s; skipped while `document.hidden`; catches up on `visibilitychange` | last `RelativeTime` unmounted |
| `ProblemAlert` countdown | `AutoRetry` | 1 s `setTimeout` chain (not `setInterval`); at zero it calls `onRetry` through a ref and restarts from `retryAfterSeconds` | unmount |
| `prefers-color-scheme` listener | `App` effect | event | unmount |
| `vite:preloadError` listener | `main.tsx` | event | page lifetime |
| EventSource (v1.1) | `useEventStream` | persistent | `pagehide`, hidden > 5 min, feature-disabled |

### 8.3 Request lifecycle and cancellation

Every `queryFn` receives TanStack Query's `AbortSignal` and passes it to `api.GET(..., { signal })`; navigation away or query removal aborts in-flight requests, and the resulting `AbortError` is discarded by the query layer, never rendered. Mutations do not exist in v1 (the one admin POST is not in the UI). Parallel loaders: the router runs the matched routes' loaders in parallel; a loader that needs data from a parent reads it from the query cache (`ensureQueryData` dedupes by key).

### 8.4 Ordering guarantees the code relies on

- `serverStatus` updates are synchronous and publish through `useSyncExternalStore`, so a failure and the banner appear in the same render.
- The snapshot version change invalidates **after** the snapshot data has been stored (effect on `version`), so a refetch triggered by the invalidation observes the new server state.
- `applyServerDefault` writes the key first, then applies only if there is no stored choice, so a viewer's explicit choice is never overwritten.

### 8.5 Build and CI lifecycle

The `pnpm build` chain is sequential and fail-fast (4.15). CI parallelism is per job, not within the build. `go build -tags spa` runs strictly after `pnpm build` (the embed needs `dist`); the placeholder keeps `go test ./...` independent of Node. The e2e job starts the binary, waits for `/readyz`, runs Playwright with `--workers` equal to the runner's CPUs (specs are independent: each test navigates from scratch and no test mutates server state), and kills the binary by process group.

---

## 9. Observability

### 9.1 Metrics

**No metrics are owned by this unit** (`metrics_owned` is empty). The SPA exposes no Prometheus endpoint and ships no analytics. The metrics that describe the SPA's delivery are owned and named by unit 06 and are the operator's view of this unit's output:

| Metric (owner 06) | Use for the web foundation |
|---|---|
| `scp_csp_reports_total{directive,disposition}` | any non-zero `enforce` count after a release means a component began injecting inline style/script: the upgrade PR missed the CSP gate |
| `scp_webapp_responses_total{kind,encoding}` | `shell404` rate = unknown-URL traffic; `notfound` spike right after a deploy = stale-chunk requests from open tabs; `encoding=br` share confirms precompression is used |
| `http.server.request.duration` (`r.Pattern`) | the polling load of `GET /api/v1/snapshot` (expected: one 304 per tab per 30 s) |

A client-side error reporting endpoint is **not** specified for v1 (open question Q3). If added, it would be a `POST /api/v1/client-errors` rate-limited class with a 4 KiB cap and the counter `scp_client_errors_total{kind}`; none of that exists now and nothing in the SPA depends on it.

### 9.2 Logs

Browser logging is deliberately minimal: `console.error` for caught and uncaught React errors with the component stack (`main.tsx` hooks; `PanelBoundary`), `console.warn` for recoverable errors. The e2e gate fails on any unexpected `console.error`, so logs are also a test signal. Fields of interest for a human: `ProblemError.code`, `.status`, `.requestId` (shown in the UI so a user can quote it; the server logs carry the same id, GR-42). **Never logged or sent anywhere**: response bodies, `detail`, query strings, storage contents.

### 9.3 Spans / tracing

None in the browser. The server creates spans per request (`otelhttp`, route pattern); the SPA does not propagate `traceparent` in v1 (it would need `connect-src` and CORS decisions for nothing; same origin means the server already correlates by request id).

### 9.4 Build observability

`finalize` prints the shape count and build id; `precompress` prints raw/gzip/brotli totals; `verify-dist` prints the file count and size; `budget` prints one line per budget with `ok`/`FAIL` and measured KB. These lines are the CI log's record of bundle size per commit; the nightly job uploads `budget` output as an artefact for trend inspection.

---

## 10. Security

Threats and the structural controls in this unit (CSP and headers themselves are unit 06; the architecture's trust zones are 17.1).

| # | Threat | Control | Evidence |
|---|---|---|---|
| S1 | Stored/reflected XSS through documentation HTML (GR-32) | HTML arrives sanitised from the backend (`safehtml.SafeHTML`); `SafeHtml` is the only sink; Trusted Types policy `scp-html` enforced by CSP; lint bans every other sink and non-`html` arguments | e2e: `/pages/about` renders under the enforced CSP; `innerHTML` assignment refused; lint-rule tests |
| S2 | Inline script/style injection, `eval`, third-party code | static CSP without nonce (ADR-010); the build produces no inline code; `verify-dist` fails on inline `<script>`, `<style>`, `style=`, `on*=`; `no-new-func`, `eval` banned | 60 e2e tests with zero violations (the gate fixture) |
| S3 | A component library starts injecting `<style>` (W11) | three owned patches; e2e CSP gate fails the upgrade PR; `CSPProvider disableStyleElements` | gate |
| S4 | Supply chain: a malicious or hijacked npm release (W16) | exact pins (`check-pins`), 7-day cooldown, `trustPolicy: no-downgrade`, `blockExoticSubdeps`, build scripts denied by default (`strictDepBuilds`), frozen lockfile in CI, SHA-pinned Actions, Renovate PRs run all gates, `devEngines` pins pnpm | install verified under the policy |
| S5 | Third-party requests / tracking (GR-18, GR-35) | `connect-src/img-src/font-src 'self'`; no CDN, no web fonts, no analytics; commit authors as initials (W19); `AvatarImage` lint-banned; the GitHub mark is an inline Octicon | CSP gate; lint test |
| S6 | `javascript:` / `data:` URLs from config (nav links, announcement) | `ExternalLink` renders only `http:`/`https:` as links; announcement is plain text; no `href` is ever built from API strings without `isWebUrl` or a typed route | unit tests |
| S7 | Tabnabbing | `target="_blank"` only through `ExternalLink` with `rel="noreferrer noopener"` | unit test |
| S8 | Open redirects / DOM clobbering | no redirect target is taken from the URL; `base-uri 'none'`; router redirects are literals | review rule |
| S9 | Sensitive data in storage | only theme, dismissal id, reload timestamp (5.1); no tokens, no personal data | review rule; `no-restricted-globals` forces `lib/persist` |
| S10 | Stale bundle against a new API (confused-deputy of types) | build-id skew banner; hashed immutable assets; `index.html` `no-cache`; plain-text 404 for missing chunks | e2e |
| S11 | Information disclosure through error text | `ProblemAlert` never renders server `title`/`detail`; only the localised title and the request id | unit test (`stack trace here` is not shown) |
| S12 | Clickjacking | `frame-ancestors 'none'`, `X-Frame-Options: DENY` (06) | n/a here |
| S13 | CORS / credentialed cross-origin reads (GR-18) | the SPA is same-origin only; the dev proxy keeps it same-origin in development; no `Access-Control-*` anywhere | by design |
| S14 | Private repository data in the SPA bundle | the bundle contains no data; visibility is enforced by the API per request (`access`); the SPA caches only what the API returned to this viewer, in memory, never persisted | architecture 17.3 |
| S15 | `noscript` content spoofing | static text with two same-origin links | verify-dist (absolute URLs allowed only inside `<noscript>`; the current text uses relative links) |

Rules for contributors (lint-enforced where possible): no new HTML sink, no `fetch` outside `lib/api`, no direct storage, no third-party script, style or font, no `dangerouslySetInnerHTML`, no secrets in `.env*` (there is no `.env` file; the only variables are those of 6.1), no `postinstall` scripts added to the allow-list without review.

---
## 11. Capability and guardrail trace

### 11.1 Capabilities

| Capability | Mechanism in this unit |
|---|---|
| Root redirect: `/` lands on `/dashboard` | `app/index-redirect.tsx` (client) + `webapp` 302 (06); `routes.ts` declares no other entry route |
| Dashboard cards, latest-commits panel with live refresh, group page | the shell, route stubs and the **reference `dashboard` slice** (cards + `FreshnessBadge`); live refresh = snapshot poll + `PanelBoundary` (JSON, never HTML fragments); the commits panel itself is unit 18 |
| Commit logs (all, group, repository) | routes `/commits`, `/groups/$groupId/commits`, `/repos/$repo/commits` (stubs); search state in the URL; content is unit 18 |
| README and documentation viewer | route `/repos/$repo` and `/repos/$repo/docs/$`; `SafeHtml` + `prose` styles; the splat-to-`docId` mapping (`resolveDocId`, extension-less URLs) is unit 18's `features/docs/docId.ts`, not a 17 module (V-09); rendering is unit 18 |
| Operator-authored pages and the site announcement | `pages` reference slice (server HTML through `SafeHtml`) and `AnnouncementBanner` (plain text from `Site.announcement`); branding is configuration (GR-14) |
| Shared layout, navigation, header | `app/root.tsx` shell: skip link, header with site title, primary nav + `Site.nav`, theme toggle, `ConnectionBanner`, announcement, `<main>`, route announcer |
| Dark/light theme | `lib/theme.ts`, `public/theme-init.js`, tokens, `ThemeToggle` (4.7) |
| Not-found and error pages | real 404 with shell (06) + `notFoundComponent`, root `errorComponent`, `ProblemAlert` with request id, panel boundaries (4.6) |
| Syntax highlighting | no client highlighter and no CDN (server-side Chroma classes); the theme switches by `.dark` class so palettes follow the toggle (GR-37); the CSS is unit 18's `docs.css` |
| Static assets, favicon, robots | `public/` files, hashed `assets/`, `verify-dist` allow-list; `robots.txt` and `sitemap.xml` are server-generated (`overview`) |
| Header status ("Updated", repository count) | `HeaderStatus`: repository count (not group count) and `lastFullSync` as a relative `<time>`; "Not synced yet" when null; never render time (GR-05) |
| Repository-count and status badges | no third-party request exists, because a browser-fetched third-party badge leaks visitor IPs and bypasses the cache and the CSP (GR-18): `connect-src/img-src 'self'`, `verify-dist` rejects absolute URLs, the count is text in `HeaderStatus` and any badge is a same-origin `/badges/*` image built by `serverRoutes` (unit 09/18, GR-33) |
| Frontend asset pipeline | the whole of section 4.1 and 4.15: one pipeline, hashed names, no committed output, CI-verified clean build (GR-19) |

### 11.2 Guardrails (GR-nn)

Each row names a guardrail of `02-design-guardrails.md` and how this unit satisfies it.

| GR | Rule as it applies here | Mechanism |
|---|---|---|
| GR-04 | nothing without a consumer | no Storybook, no unused dependency (`knip` candidate, Q10), no state library beyond Query, no form library, no devtools in the bundle |
| GR-05 | names tell the truth | names follow the API (`lastFullSync`, `repoCount`, `freshness`); `HeaderStatus` has no render-time input; no rendered "null" strings |
| GR-11 | wire and view types are distinct; empty states are explicit | views read generated API types; typed API data with explicit empty and "Not synced yet"/"unknown" states; the shell reads one `getSite` query and missing optional fields have defined fallbacks (6.3); the `Empty` component is generated and available |
| GR-14 | neutral defaults; branding is configuration | title, nav, announcement, theme default and zone come from `getSite`; no event-specific branding in code |
| GR-15 | one route table; links are generated and crawled | typed routes make a wrong internal link a compile error; lint ban on path literals + typed `<Link>`; `serverRoutes` builders; e2e visits one URL per shape and a unit test fails when a shape lacks a sample (`e2e/pages.ts`, `scripts/e2e-coverage.test.ts`, verified); the e2e crawl of generated text surfaces (4.2, M2-8) |
| GR-16 | truthful responses | real 404 + NotFound page; problem alerts for every failure class; no redirect on unknown ids |
| GR-17 | the management plane is separate | the same bundle is served on both listeners with different route manifests: `/status` and every admin operation exist only in the admin manifest (`AdminRedirect`); the SPA never links `/status` from public pages and the public manifest has no `/status` (real 404); the SPA offers no admin action in v1 (4.16, Q11) |
| GR-18 | strict browser security policy | the bundle is CSP-clean by construction, proven by the e2e gate under the enforced policy including Trusted Types: `verify-dist` forbids inline script/style/handlers and absolute URLs; system fonts; `connect/img/font-src 'self'`; no CDN, no analytics; the SPA is same-origin only, sends no credentials cross-origin and reads or writes no `Access-Control-*` (S13) |
| GR-19 | one reproducible asset build; only its output is served | one Vite pipeline, `dist/` not committed, content-hashed immutable assets; `*.map` and unknown extensions fail `verify-dist`; `webapp.New` loads the bundle at start only; `index.html` `no-cache`; build-skew banner and guarded reload; CI verifies a clean build |
| GR-26, GR-40 | freshness is data; live updates respect visibility and show staleness | `Freshness` type from the API; `FreshnessBadge` per list and per item; TanStack Query keeps the last data on screen; `ConnectionBanner` (unreachable); visibility-aware polling, ETag 304 of one tiny resource, JSON not HTML (4.4, 4.5; verified in e2e) |
| GR-29 | absence and unsupported input are typed states | unsupported or missing README formats arrive as a typed state from the API; nullable dates are rendered "unknown"; rendering of these states is unit 18 |
| GR-32 | trusted HTML is minted only by the sanitiser | HTML arrives sanitised from the backend; `SafeHtml` is the sole sink, Trusted Types policy `scp-html`, lint bans on every other sink (also inside owned shadcn code, V-12), `SafeHtml html=` must be an API member named `html`; e2e refuses a raw `innerHTML` assignment |
| GR-35 | personal data is minimised | commit authors as initials (W19); `AvatarImage` lint-banned; no third-party image request carries visitor data |
| GR-38 | UTC storage, explicit display zone | `RelativeTime` is a `<time datetime>` whose title is the exact instant in `Site.displayTimeZone`; day grouping uses `Site.displayTimeZone` through `formatDateTime`/`useDisplayZone`; formats use the UI locale (V-08) |
| GR-39 | accessible, purpose-written frontend | WCAG 2.2 AA gate (axe, both themes, both viewports) on every shape; icon-only controls require `aria-label` (jsx-a11y); `aria-hidden` on decorative icons; inline SVG icons (lucide, one Octicon), no icon fonts; pre-paint `theme-init.js` (no flash, proven with the bundle aborted); every storage access through `lib/persist` with try/catch (a blocked-storage unit test and the reload-guard rule 4.4.5); Tailwind emits only used CSS (initial CSS 17.5 KB) |
| GR-42 | logs are correlated | `ProblemAlert` shows the request id of the failing call (`instance` urn or `X-Request-Id`, parsed in `problem.ts`), so a user report names the server log line |
| GR-43, GR-44 | tests assert; CI is the verifier | small purpose-written fixtures with fixed timestamps; `pnpm check` and CI are the verifier (every gate exits non-zero on failure; each was seen failing during the prototype) |

---

## 12. Test plan

Named cases; "(v)" means the case exists and passed in the prototype (counts after verification: **144 unit/tooling tests in 21 files**, **2 browser tests**, **62 Playwright runs = 31 per viewport**; the first draft had 130, 1 and 60). Cases without "(v)" are **specified** for the milestone given in section 13.

### 12.1 Unit and component (Vitest `unit`, jsdom)

| File | Cases |
|---|---|
| `lib/api/problem.test.ts` (v) | `problemFromResponse` reads code, request id from `instance`, retry-after and field errors; falls back to `X-Request-Id`/`Retry-After`; a non-problem body (proxy HTML 502) becomes `client-unparseable` with the status kept; `problemFromThrown` classifies fetch rejection and `AbortError` (also one from another realm, new) and passes a `ProblemError` through; `isTransient` table (9 rows) |
| `lib/api/client.test.ts` (v) | `unwrap` returns data and marks reachable; maps RFC 9457 to `ProblemError` with code, status and `retryAfterSeconds` and marks unreachable on 503; classifies a network failure; a 404 does not mark the server unreachable **and clears an earlier outage** (new, V-03); **a cancelled request does not mark it unreachable** (new, V-01); `X-SCP-Build` mismatch sets `buildSkew`; an unmocked request fails loudly (MSW `onUnhandledFrame: "error"`) |
| `lib/api/snapshot.test.tsx` (v) | a version change invalidates every non-snapshot query; `invalidateTopics` maps `catalog` to `catalog` + `repo` and leaves `pages`/`commits` alone; `retryPolicy` retries transient problems twice and never 4xx |
| `lib/theme.test.ts` (v) | default `system` follows `prefers-color-scheme`; a stored choice beats the server default and the OS; the server default applies only without a stored choice and is remembered; blocked storage degrades to `system`; **`theme-init.js` uses the same keys, is CSP-clean, and decides identically to `theme.apply()` for all 32 combinations** |
| `lib/tokens.contrast.test.ts` (v) | 11 foreground/background pairs x light/dark meet 4.5:1 or 3:1 (control border, focus ring); the test parses the real `index.css` |
| `lib/format.test.ts` (v) | `formatRelative` rows (now / 5 minutes / 3 hours / yesterday / in 20 minutes) with a fixed `now`; **the default locale is the UI locale** (activating `nb` yields "for 5 minutter siden", new, V-08); `useNow` advances every 30 s, does not advance while hidden, catches up on `visibilitychange`, and clears its timer when the last subscriber unmounts |
| `shared/freshness-badge.test.tsx` (v) | each status renders its **text**; an unknown enum value renders "Unknown"; `asOf` renders a `<time datetime>` |
| `shared/problem-alert.test.tsx` (v) | localised title and request id, **no raw `detail`**; **an unknown code never shows the server title** (new, V-04); `not-ready` auto-retries after the countdown (per-tick `act`) and the countdown is `aria-hidden`; a persisting problem is retried exactly once per countdown across parent re-renders (new, V-05); a 404 never auto-retries |
| `shared/problem-title.test.ts` (v) | known codes map to titles; an unknown code gets the generic title |
| `shared/safe-html.test.tsx` (v) | renders server HTML verbatim and applies the class |
| `shared/external-link.test.tsx` (v) | `isWebUrl` table (6 rows); a web URL renders a new-tab link with `noopener`; `javascript:` renders as text |
| `shared/header-status.test.tsx` (v) | count singular/plural, `<time>` from `lastFullSync`, "Not synced yet", the display zone changes the `title` |
| `shared/panel-boundary.test.tsx` (v) | a throwing child renders `ProblemAlert` with the request id and leaves its sibling alone; Try again resets and re-renders; a child that keeps failing is retried on the countdown, not in a loop (new, V-05) |
| `shared/connection-banner.test.tsx` (v) | silent when fine but the `role="status"` region exists; precedence skew > unreachable > warming; never skew for a `dev` bundle; recovers on the next success |
| `shared/announcement.test.tsx` (v) | plain text only (markup is not interpreted); dismissal persists per `id`, a new `id` shows again, **also when it arrives while the banner is mounted** (new, V-06); no dismiss button when not dismissible |
| `shared/route-announcer.test.tsx` (v, new) | first load does not steal focus; a **search-only** navigation keeps focus in the filter input (fails on the first draft, V-16); navigating to another path focuses `main h1` (memory-history router) |
| `lib/stale-chunk.test.ts` (v, new) | reloads once and cancels the event; no second reload within 30 s and the event is left alone (Vite rethrows the real error); reloads again after the window; **never reloads when the time cannot be stored** |
| `lib/persist.test.ts` (v) | round trip and removal; read returns `null` when the accessor throws `SecurityError`; write swallows `QuotaExceededError` |
| `app/root.test.tsx` | the shell renders the `Site` title, nav pages and external links, hides Dependencies when `features.manifests` is false (M1-8) |

### 12.2 Tooling and lint (Vitest `tooling`, Node)

| File | Cases |
|---|---|
| `scripts/route-shapes.test.ts` (v) | `toPattern` for `$param` and the splat; **golden list of shapes** (inline snapshot; a route change must update it, the Go seam test and `e2e/pages.ts` together); `/status` is admin-only; `/` is never listed |
| `scripts/e2e-coverage.test.ts` (v) | every public shape has a sample URL in `e2e/pages.ts`; every sample matches a shape |
| `scripts/lint-rules.test.ts` (v) | 19 violation fixtures each fire the intended rule (feature importing another feature's internals; rank direction; shared importing a feature; portal path literal; API path outside allowed files; `innerHTML`; `fetch`; `localStorage`; `zod`; `react-router`; unlocalised string; **remote `AvatarImage`, `dangerouslySetInnerHTML` outside safe-html and inside `components/ui`, `innerHTML` inside `lib/api`, `document.cookie`, `sessionStorage`, `EventSource` outside `lib/api`** (the last seven are new; the first draft claimed 13 fixtures including `AvatarImage` but had 12 and no `AvatarImage` case); `SafeHtml` fed by a non-`html` expression) and 2 allowed cases pass (a higher feature importing a lower one through `index.ts`; an endpoint path in `features/*/api.ts`). Still untested: `import-x/no-cycle` (needs two files on disk; M3 candidate) |
| `scripts/finalize.test.ts`, `verify-dist.test.ts`, `budget.test.ts` | run each script against small generated `dist` fixtures: a hand-edited dist with an inline script, a `.map`, a bad hash, an orphan `.br`, an over-budget chunk each fail with the expected message (M1-9) |
| `scripts/check-pins.test.ts` | `^1.2.3`, `~1.2.3`, `latest`, a `pnpm.overrides` block fail; `npm:` aliases with exact versions pass (M1-9) |

### 12.3 Browser component tests (Vitest `browser`, real Chromium, real CSS)

`shared/freshness-badge.browser.test.tsx` (v): the stale badge is visible, has text and a non-zero box. `shared/safe-html.browser.test.tsx` (v, new): a same-html re-render keeps the DOM (`<details open>` survives); in Chromium `createHTML` returns a fresh `TrustedHTML` object per call, so this passes only because the React Compiler memoises it, and the test is the guard for that dependency. Planned: `ProblemAlert` focus order and `PanelBoundary` layout at 360 px (M1-7).

### 12.4 End to end (Playwright against the real binary; 62 runs verified)

| Spec | Cases (each x desktop, phone) |
|---|---|
| `routes.spec.ts` (v) | `axe light|dark <page>` for the 11 sample URLs: status 200, `main h1` visible, **no horizontal scroll** (WCAG 1.4.10), zero axe violations for `wcag2a wcag2aa wcag21a wcag21aa wcag22aa`; unknown route is a real 404 that renders "Page not found"; a stale hashed chunk name is a plain-text 404, never HTML; **no horizontal scroll at 320 px on every sample URL** (new, 1.4.10). The doc sample is `/repos/core-lib/docs/docs/install` (extension-less, D-18-01): a `.md` URL is the raw route on the real binary |
| `behaviour.spec.ts` (v) | `theme-init.js` applies the stored theme with the app bundle aborted; staleness is icon + text from the API `freshness`; a 503 `not-ready` renders `ProblemAlert` with the request id and the "still warming up" title; a different server build id shows the reload banner; a typed link navigates without a full reload (a window marker survives); server HTML renders through the Trusted Types policy with zero violations and a raw `innerHTML` assignment is refused |
| planned (M2) | the **crawl** of generated text surfaces and rendered-doc links (unit 18 E18-CRAWL-01, 4.2); a stale-chunk scenario (open `/dashboard`, swap `dist` for a build with other hashes, navigate: exactly one reload, then the real error); the admin listener serves `/status` and 404s every public shape; `Accept-Encoding: br` is served brotli; `If-None-Match` on the shell returns 304; the snapshot poll sends conditional requests (assert one 304 per 30 s with fake clock via `page.clock`); the `scp-theme` choice survives reload without flash; keyboard-only traversal of the shell (skip link, nav, toggle) |

The **gate fixture** (`e2e/fixtures.ts`) wraps every test; a test that deliberately triggers an error lists the console text it expects through `allowConsole` and clears recorded violations only for the deliberate one.

### 12.5 Fixtures and determinism

API fixtures in `e2e/fixtures/api/*.json` (`snapshot`, `site`, `groups`, `pages_about`) use fixed timestamps (`2026-10-14T09:41:07Z`) and the e2e server derives weak ETags from content. The e2e server (`17-web-foundation.assets/go/cmd/e2eserve`) is a stand-in for `cmd/scp` + `cmd/fakegithub` until unit 08+ can serve real data; in M2 the same specs run against the real binary with the fake GitHub, and the stand-in is deleted. Unit tests never touch the network or the clock (4.12).

### 12.6 CI gates (what must be green to merge)

`check-pins`, `format:check`, `typecheck`, `lint --max-warnings 0`, `test`, `test:browser`, `build` (including `verify-dist` and `budget`), generated-file drift (`gen:types`, route tree, `extract`), `e2e` on the `-tags spa` binary. Nightly: Lighthouse (regression alarm), `--repeat-each=3` e2e, dependency audit (`pnpm audit --prod` report, informational).

---

## 13. Implementation tasks (ordered; each ends with its checks green)

Milestones follow architecture 22/23: **M0** = skeleton and generation with no behaviour (parallel to the Go spikes), **M1** = shared layers and gates, **M2** = integration with the real Go binary and the feature slices, **M3** = hardening. Dependencies on other units are marked `[dep: unit]`.

### M0. Toolchain skeleton (no behaviour)

1. `web/` base files: `package.json` (exact pins of 0.2), `pnpm-workspace.yaml`, `.nvmrc`, `.node-version`, `.gitignore`, `tsconfig*.json`, `vite.config.ts`, `vite.plugins.ts`, `index.html`, `public/*`, `.prettierrc.json`, `.prettierignore`. Exit: `pnpm install --frozen-lockfile` under the pnpm 12 policy, `pnpm typecheck` clean on an empty `src/main.tsx`. `[dep: 02-platform for CI image/Node pin]`
2. shadcn: `components.json`, then `pnpm exec shadcn add` for the 34 components with the pinned pnpm first on `PATH`; pin written versions; apply the three CSP patches. Exit: `check-pins` passes; chart has no `<style>`.
3. `routes.ts` with the route tree and stub slices (`index.ts` with `export {}`, `route.tsx` with an `<h1>`); `src/app/{providers,router,root,index-redirect}`; the generated `routeTree.gen.ts` committed. Exit: `pnpm build` produces a `dist/` with lazy route chunks.
4. `scripts/{finalize,precompress,verify-dist,budget,check-pins,route-shapes}.ts`. Exit: `pnpm build` runs the five-step chain; each script fails on its negative fixture (tests in M1-9).
5. `eslint.config.js` (element policies, restricted syntax, overrides, `FEATURE_MAY_IMPORT`). Exit: `pnpm lint` clean; `scripts/lint-rules.test.ts` green.
6. `lingui.config.ts`, `lib/i18n.ts`, first `messages.po`. Exit: `pnpm extract` stable (second run no diff).
7. `vitest.config.ts`, `src/test/{setup,server,render,contrast}.ts`, `playwright.config.ts`. Exit: one passing test per project.
8. `[dep: 07-openapi-contract]` `gen:bundle` and `gen:types` wired to `internal/apispec/*.bundled.yaml`; commit `schema.d.ts`. Exit: `pnpm gen:types && git diff --exit-code`; until 07 delivers, the prototype's copies of the fragments are used.

### M1. Shared layers and gates

1. `lib/api/{problem,client,query-client,query-keys,snapshot,site}.ts` with tests.
2. `lib/{persist,storage-keys,theme,format,routes,url,stale-chunk,server-status}.ts` with tests; `public/theme-init.js` with the equivalence test.
3. `styles/index.css` tokens and the contrast test.
4. `shared/` components (3.4) and `app/root.tsx` shell (header, status, banner, announcement, route announcer, not-found, root error).
5. Reference slices `dashboard` (groups + freshness) and `pages` (`SafeHtml`) end to end against MSW. `[dep: 08-catalog for GET /groups shape (the schema exists already)]`
6. `e2e/fixtures.ts`, `routes.spec.ts`, `behaviour.spec.ts`, `pages.ts`; the stand-in e2e server. Exit: 60 passing runs, zero CSP violations.
7. More browser tests (`ProblemAlert` focus order, `PanelBoundary` layout at 360 px); the route-announcer unit test (memory-history router) is done.
8. `app/root.test.tsx` (shell from a `Site` fixture).
9. Script tests (`finalize`, `verify-dist`, `budget`, `check-pins`) with generated dist fixtures.
10. CI `web` job definition (pins, format, types, lint, tests, build, drift). `[dep: 02-platform CI skeleton]`

### M2. Integration

1. `[dep: 06-httpapi]` replace the stand-in server by the real `cmd/scp -tags spa` in the `e2e` job; `BUILD_ID` passed to both builds; the seam test of `routes.json` against `routes.Public().SPAShapes()` (+ the `serverRoutes` builders against `routes.Table`). Exit: both green; stand-in deleted.
2. `[dep: 06-httpapi]` cross-unit fixes of 7.2: `hashedName` robustness, `X-SCP-Build` on API responses, reserved-prefix list shared with the Vite proxy list.
3. `[dep: 18-web-pages]` integration of the feature slices: replace 18's compile-check copies of `client.ts`, `problem.ts`, `query-keys.ts` by the foundation's; adopt `tokens.css` as the token source (Q8); add the new shapes' samples to `e2e/pages.ts`; re-run budgets (graph chunk 70 KB, charts 120 KB).
4. `[dep: 09-commits-badges, 08-catalog, docs, manifests]` extend the e2e fixtures with the real API shapes; move fixtures to `satisfies` checks.
5. Multi-stage Dockerfile web stage (`node:24.21.0-alpine` by digest) and the image smoke (`/` serves the SPA). `[dep: 02-platform / ADR-017]`
6. Renovate config for `web/` (7-day cooldown, grouped by ecosystem, lockfile maintenance).
7. `[dep: 15-live-ops-overview]` `getSite` field alignment (nav, announcement, `features`, `displayTimeZone`) and the `scp.yaml`/config keys behind them.
8. `[dep: 18-web-pages]` the crawl spec of generated text surfaces and rendered-doc links (unit 18 E18-CRAWL-01) runs in the `e2e` job; the stale-chunk scenario of 12.4; the admin-listener answer of Q11 is implemented in the shell.
9. `[dep: 06-httpapi, 07-openapi-contract]` one `@redocly/cli` pin for `gen:bundle` and the api job (Q12).

### M3. Hardening and v1.1

1. Lighthouse nightly job and budget JSON; `--repeat-each=3` nightly.
2. `knip` (or equivalent) for unused exports/dependencies, decided by Q10.
3. `useEventStream` (4.4.4) behind `live.sse.enabled`, with the Playwright shutdown test of ADR-009. `[dep: 15-live-ops (live)]`
4. Mermaid frame (Q2) and `/_frame/` prefix in `verify-dist`, Vite proxy list and the reserved list. `[dep: 06-httpapi, docs]`
5. TypeScript 7.1 migration: remove the TS 6 alias when `typescript-eslint` supports the TS 7 API; re-verify `pnpm check`.
6. Node 26 LTS switch of CI and Docker after 2026-10-28 (Q7); `@types/node` to the 26 line.

---

## 14. Deviations proposed

Each deviation has evidence from the prototype. None changes a binding decision's intent.

| # | Binding text | Proposed change | Evidence |
|---|---|---|---|
| D1 | architecture 12.2 lists a `sidebar` among the shadcn components and "layout, sidebar, header" in `app/root.tsx` | the shell is a **header** with the primary nav, `Site.nav`, the status line and the theme toggle; no `sidebar` component; the command palette (`cmdk` via shadcn `command`) is available for unit 18's repository search | the stock `sidebar` (registry `base-nova`, read with `shadcn view sidebar` on 2026-10-14) writes its state to `document.cookie`, which this app bans (no session, nothing to persist server-side); a wrapping header passes reflow at 360 px and axe in all 60 runs |
| D2 | architecture 12.3 and 22: budgets "enforced by `size-limit` in CI" | `scripts/budget.ts` (45 lines, zero dependencies) walks the Vite manifest and enforces the same numbers per initial closure and per lazy chunk | `size-limit` measures files, not the static-import closure of Rolldown's shared chunks; the app code sat in `preload-helper-*.js`, so a file-based check under-counted; the script has no dependency, no plugin to pin, and prints the offender |
| D3 | 06 section 4.9: pin `assets/[name]-[hash:8].[ext]` with the default base64url hash alphabet | pin the **alphabet** too: `rolldownOptions.output.hashCharacters: "hex"` (a subset of 06's rule; 06's `hashedName` was since made robust, so this is belt and braces, V-17) | the real `webapp.New` rejected the first Vite 8 build (names ending in `-`, containing `_`); the hex build starts under `RequirePrecompressed`, `CheckBuild` and the full enforcing CSP |
| D4 | the shadcn Vite template installs `@fontsource-variable/geist` | system font stack, no font files | `font-src 'self'` has nothing to serve; zero font bytes against a 100 KB budget; no FOUT; no licence file to carry |
| D5 | architecture 12.1 "Zod for route params and env only" | import only `zod/mini` (lint-enforced `no-restricted-imports`), for route search params | full Zod is a larger bundle; `zod/mini` covers `z.object`, `optional`, `string().check(maxLength)`, `infer` (verified in `dashboard/search.ts`) |
| D6 | 05-research/08 proposed a branded `SafeHtmlString` type | plain `string` + the lint selector "`<SafeHtml html>` accepts only a member named `html`" | `openapi-fetch`'s `Readable<T>` mapping strips intersection brands from response types, so the brand does not survive `schema.d.ts`; `openapi-typescript`'s `transform` hook needs `ts.factory` nodes, which would couple the generator to the compiler API |
| D7 | none (CLI invoked ad hoc in 12.1) | `shadcn` 4.21.3 is a pinned devDependency; `shadcn add` runs with the pinned pnpm first on `PATH` | a floating CLI writes floating versions (`cmdk ^1.1.1`) and invokes whatever pnpm is on `PATH` (`ERR_PNPM_UNEXPECTED_STORE` with pnpm 10 on a pnpm-12 lockfile) |
| D8 | architecture 12.1 "pnpm 12 with a 7-day release cooldown" | the policy also sets `trustPolicyIgnoreAfter: 129600` and `allowBuilds: {unrs-resolver: false}` (plus `trustPolicy: no-downgrade`, `blockExoticSubdeps`, `strictDepBuilds`) | the install failed without them: `High-risk trust downgrade for semver@6.3.1` and `ERR_PNPM_IGNORED_BUILDS` |
| D9 | architecture 12.2 puts `CSPProvider` in `main.tsx` and lists `lib/build.ts` | `CSPProvider disableStyleElements` is in `app/providers.tsx` (providers live together); the build-skew logic lives in `lib/server-status.ts` (it is shared state, not a build constant); added `lib/storage-keys.ts`, `lib/url.ts`, `lib/doc-id.ts` | cohesion; each has a test |
| D10 | architecture 12.2 "loader (ensureQueryData)" | loaders use `retry: false`; the error component owns retry | with the global retry schedule a loader blocks rendering for the whole backoff (the `not-ready` e2e timed out); fail-fast shows the alert at once and retries visibly |
| D11 | architecture 12.3: invalidate on snapshot version / SSE | additionally a 120 s visibility-aware sweep (`refetchInterval`) | a `fresh` item becomes `stale` with no version change (freshness is derived at read time); without a sweep the badge would never age on an idle page; cost one conditional GET per active query per 2 minutes (Q10 on infinite queries) |
| D12 | architecture 12.1 "Prettier or Biome (pnpm workspace decides)" | Prettier 3.9.9 (decision recorded) | Biome has no `eslint-plugin-boundaries` equivalent and no type-aware lint; two tools would split the rule set |
| D13 | ADR-019 / 12.1 pins TanStack Router 1.170.44 | pin 1.170.41 (newest older than 7 days on 2026-10-14) | the project's own cooldown rule; Renovate lifts it when the cooldown passes |
| D14 | the architecture tree (section 7) lists `web/.node-version` | the prototype has `.nvmrc` **and** `.node-version`, both `24.21.0` (nvm reads `.nvmrc`, fnm/asdf read `.node-version`) | cheap and harmless; recorded because the tree names only one |
| D15 | architecture 12.4: `pnpm -C web gen` includes "TanStack route tree"; 12.2 names the poll hook `useSnapshotVersion` | `pnpm gen` is bundle + types only; `routeTree.gen.ts` is written by the router plugin on `vite`, `vite build` and `vitest` and drift-checked by `pnpm build && git diff --exit-code`; the hook is `useSnapshotSync` (it also reports readiness and build id) | the plugin has no standalone CLI in this setup (virtual file routes are configured in the Vite plugin); naming follows what the hook does |

---

## 15. Open questions

| # | Question | Needed from | Default if unanswered |
|---|---|---|---|
| Q1 | Resolved by the verification: the docId mapping is not this unit's (it was `lib/doc-id.ts`, wrong against 07 D-3 and 18 D-18-01). Remaining question for 18/10: the splat helper must percent-encode every byte outside `[A-Za-z0-9._-]` and the router must not double-decode; 18's `docId.ts` rejects `~` and spaces in a UI path, which differs from 07's encodable ids (a file `Release Notes.md` has no UI URL) | 18, 10 | such a path renders "Not found" |
| Q2 | 06 allows exactly one `.html` in `dist` (`index.html`). The v1.1 Mermaid frame (`/_frame/mermaid.html`, ADR-010) needs a second document with a stricter CSP. Allow-list one named exception in `webapp.New` and `verify-dist`, or serve the frame from Go? | 06, docs | Mermaid stays out of v1; the frame is added together with the exception |
| Q3 | Client error reporting (uncaught React errors, `preloadError` loops): add a rate-limited `POST /api/v1/client-errors` and counter, or rely on the server log of failing API calls and the CSP report endpoint? | owner, 06 | no reporting in v1 |
| Q4 | Should the shell show the build version (`Site.build`) in a footer for supportability? | owner | no footer; `/status` (admin) shows it |
| Q5 | Hash `theme-init.js` and reference it as `/assets/theme-init-<hash>.js` (immutable cache, no per-view revalidation) as 06 Q5 asks? The script is on the critical path and rarely changes. Cost: `verify-dist` and the `Vite` HTML transform must emit the hashed reference, and the pre-paint test must read the built file. | 06 | stay with `no-cache` + ETag (one revalidation per view) |
| Q6 | The owner's confirmation of W19: commit authors render as initials in v1 (no avatar request); `AvatarImage` is lint-banned until then. | owner | initials |
| Q7 | When to move CI and the Docker build stage from Node 24.21.0 to the 26 LTS line (available after 2026-10-28)? The prototype already ran on Node 26.10.0 and passed every gate. | owner / 02-platform | stay on 24.21.0 until the first Renovate cycle after 2026-10-28 |
| Q8 | Token ownership: adopt unit 18's `tokens.css` as the single value source (extended semantic tokens, `--viz-*`), with unit 17's contrast harness reading it? | 18 | adopt it at M2-3 |
| Q9 | Storybook: revisit only if a design-review need appears (4.13). | owner | not adopted |
| Q10 | Three smaller items: (a) infinite queries (commit lists) under the global 120 s sweep refetch every loaded page; set `refetchInterval: false` on `useInfiniteQuery` and rely on the snapshot version, or keep the sweep? (b) adopt `knip` for unused exports and dependencies in CI? (c) Lighthouse is a nightly alarm only; is a PR-blocking performance gate wanted? | 18, owner | (a) disable the sweep for infinite queries in unit 18; (b) add in M3; (c) alarm only |
| Q11 | How does the SPA know it runs on the admin listener? 18 (OQ-18-01, D-18-09) assumes the admin listener serves neither `/api/v1/site` nor `/api/v1/snapshot` and asks for `useListener()`; the route table (01 `routes.Admin()`, verified 2026-10-14) registers both on admin | 01, 06, 15, 18 | no `useListener()`: the shell is listener-agnostic (it calls site and snapshot; a 404 from either is tolerated, 4.16); the Status page needs neither |
| Q12 | Which `@redocly/cli` version produces the committed `internal/apispec/*.bundled.yaml`? Architecture appendix A says 2.59, this unit pins 2.59.0 (the newest older than 7 days), units 06/07 pin 2.63.0 (released 2026-10-13, inside the cooldown). Two versions would make the drift check flip | 06, 07 | one pin, in `web/package.json` (the only Node tool in the repo); the api job runs `pnpm -C web exec redocly`; 2.59.0 until Renovate moves it |
| Q13 | Array search params (OQ-18-04): JSON (`?ecosystem=%5B%22maven%22%5D`, router default) or repeated keys like the API? | 18 | JSON default; 18's schemas accept either through a `z.preprocess` helper |

---

## Appendix A. Companion files and how to reproduce the verification

`17-web-foundation.assets/` (about 150 files): `web/` (the prototype tree without `node_modules`, `dist`, `schema.d.ts`), `api/` (the three OpenAPI source documents the types were generated from; refreshed by the verification to the 07 state of 2026-10-14 03:12, the typecheck and all tests stay green), `go/cmd/e2eserve/main.go` (the stand-in server over the real `webapp`), `pins.py` (registry check), `README.txt` (the commands below).

```
cd web && pnpm install --frozen-lockfile        # pnpm 12.10.1 first on PATH; Node 24.21 or 26
pnpm gen:api && pnpm extract && pnpm check       # check = check-pins, format:check, typecheck, lint --max-warnings 0, test  (144 tests)
BUILD_ID=abc123def456 pnpm build                 # finalize, precompress, verify-dist, budget
# build go/cmd/e2eserve inside a checkout of the 06 prototype (module github.com/cantara/source-code-portal, Go 1.27), then:
bin/e2eserve -dist dist -fixtures e2e/fixtures/api -addr 127.0.0.1:9090 -build abc123def456
PW_CHANNEL=chrome pnpm exec playwright test      # 62 runs; omit PW_CHANNEL after `playwright install chromium`
```

What was compile-checked in Go: the 06 prototype with `cmd/e2eserve` added (`go build ./...`, `go vet ./cmd/e2eserve ./internal/webapp ./web`, `go test ./internal/webapp ./web`, Go 1.27.1), i.e. the signatures quoted in 2.4. No Go signature in this unit is new: the unit adds no Go package.

## Appendix B. Quick reference for feature authors (unit 18)

- **Add a route**: edit `routes.ts` (one line), create the module (`route.tsx` with `createFileRoute("/path")`), run `pnpm build` (regenerates `routeTree.gen.ts`), update the golden in `scripts/route-shapes.test.ts`, add a sample URL to `e2e/pages.ts`, and tell unit 06/01 (the Go seam test fails otherwise).
- **Fetch data**: in `features/<f>/api.ts` write `queryOptions({ queryKey: qk.<root>..., queryFn: ({signal}) => unwrap(api.GET("/api/v1/...", {signal})) })`; add the key to `qk` under the topic root; load it in the route `loader` with `ensureQueryData({...q, retry:false})` and read it with `useSuspenseQuery`.
- **Show an error**: let it throw; wrap independent panels in `PanelBoundary`; use `ProblemAlert` directly only for non-throwing flows.
- **Show time and staleness**: `FreshnessBadge` for the API `freshness`, `RelativeTime` for instants; never `new Date().toLocaleString()`.
- **Add a string**: `t\`...\`` or `<Trans>`; run `pnpm extract`.
- **Link**: `<Link to="/groups/$groupId" params={{ groupId }}>`; badges and raw Markdown through `serverRoutes`; off-site through `ExternalLink`.
- **Show server HTML**: `<SafeHtml html={data.html} className="prose dark:prose-invert" />`; nothing else.
- **Add a shadcn component**: 4.1.4 procedure; run `pnpm e2e` before merging.
- **Never**: `fetch`, `localStorage`, `innerHTML`, `zod` (use `zod/mini`), `react-router`, a hard-coded path, an `<img>` of a remote URL, `<style>` or `style=""` strings, `eval`.

## Appendix C. Verbatim configuration of the sensitive files

The authoritative copies are in the companion `web/` directory; the files whose exact content is load-bearing for security or for the Go contract are:

- `pnpm-workspace.yaml`:

```yaml
minimumReleaseAge: 10080
trustPolicy: no-downgrade
trustPolicyIgnoreAfter: 129600
blockExoticSubdeps: true
strictDepBuilds: true
allowBuilds:
  unrs-resolver: false
```

- `public/theme-init.js` (433 bytes; the unit test pins the keys and the decision table):

```js
(function(){try{var d=document.documentElement,s=null,f=null;try{s=localStorage.getItem('scp-theme');f=localStorage.getItem('scp-theme-default')}catch(e){}var p=s==='light'||s==='dark'||s==='system'?s:f==='light'||f==='dark'?f:'system';var k=p==='dark'||(p==='system'&&matchMedia('(prefers-color-scheme: dark)').matches);d.classList.toggle('dark',k);d.style.colorScheme=k?'dark':'light';d.setAttribute('data-theme',p)}catch(e){}})()
```

- `index.html` (shell; Vite injects the hashed module script and stylesheet link):

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="color-scheme" content="light dark" />
    <meta name="referrer" content="strict-origin-when-cross-origin" />
    <title>Source Code Portal</title>
    <link rel="icon" href="/favicon.svg" type="image/svg+xml" />
    <link rel="manifest" href="/manifest.webmanifest" />
    <script src="/theme-init.js"></script>
  </head>
  <body>
    <div id="root"></div>
    <noscript><p>Source Code Portal needs JavaScript. Machine-readable content: <a href="/llms.txt">/llms.txt</a>, <a href="/api/v1/openapi.yaml">/api/v1/openapi.yaml</a>.</p></noscript>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

---

## Verification log (independent verifier, 2026-10-14)

Verifier: independent (not the author). Scope: this document and `17-web-foundation.assets/`. Full report with severities and evidence: `30-verification/design-17-web-foundation.md`. Scratch work (copies of the prototype, probe tests, the 06 prototype with `e2eserve`) was kept in a temporary directory outside the package and is not part of it.

**Reproduced as claimed (before any edit):** `pnpm install --frozen-lockfile` under pnpm 12.10.1; `pnpm gen:api`; `pnpm check` (130 tests); `pnpm build` (139.0 KB / 17.5 KB, 44 files); the 60 Playwright runs against the real `webapp` (`e2eserve` built from the 06 prototype with Go 1.27.1: `go build ./...` and `go vet` clean); `routeTree.gen.ts` and `messages.po` free of drift. Every pin exists on the registry, none is inside the 7-day cooldown, none is deprecated (`pinscheck.py`); pnpm 12 setting names and units (`minimumReleaseAge`, `trustPolicyIgnoreAfter` in minutes, `allowBuilds`) checked at pnpm.io/settings; Node 24.21.0 (2026-09-07) and the 26 LTS date 2026-10-28 checked at nodejs.org; Base UI `CSPProvider.disableStyleElements` read in the installed typings; `vite:preloadError` semantics read in Vite's source.

**Fixed by the verifier (ids V-nn in the report):** V-01 cancelled requests marked the server unreachable; V-02 `AbortError` detection by `instanceof` failed across realms; V-03 a 404 did not clear an outage banner; V-04 `ProblemAlert` rendered the server's title for unknown codes; V-05 `AutoRetry` re-fired `onRetry` on every render after the first countdown; V-06 a new announcement id did not reappear while mounted; V-07 the stale-chunk reload guard looped on blocked storage and cancelled the event unconditionally; V-08 relative times and titles used the browser locale; V-09 `lib/doc-id.ts` contradicted 07 D-3 and 18 D-18-01 (removed); V-10 an e2e sample used a `.md` doc URL (the raw route on the real binary); V-11 `budget.ts` dropped duplicated CSS; V-12 lint overrides dropped every ban and the fixture count was wrong; V-13 `verify-dist` lacked the `api/` check; V-14 `recharts` pin; V-15 `isTransient` text; V-16 `RouteAnnouncer` stole focus on search-only navigation; V-17 stale hashed-name requirement; V-18 `For`/`Options` signatures; V-19 undeclared deviations and the 33 versus 34 component count; V-20 WCAG references and the 320 px reflow; V-21 trace gaps; V-22 dev proxy; V-23 seam with unit 18 (`useSite`, `ExternalLink`, pending options, admin listener); V-24 the generated-text crawl had no named test; V-25 redocly pin conflict with 06/07.

**Checked and found correct:** the 12 SPA route shapes equal the SPA-class entries of `01-kernel.routes.tsv` (11 public plus `/status` admin) and satisfy 06's `routes.json` grammar; the import matrix of 2.2 equals architecture 12.2 and the rank table (`FEATURE_MAY_IMPORT`) equals the architecture's one-way matrix; the three CSP patches and the static CSP of 7.3 equal ADR-010 and 06 4.11; `theme-init.js` is 433 bytes and equal in decision to `theme.apply()` over 32 combinations; with the 07 documents of 2026-10-14 03:12 (new slug/docId/timestamp patterns) `pnpm gen:api`, `pnpm typecheck` and all tests still pass; the Go side builds against the latest 06 prototype.

**Still open (cross-unit, defaults in section 15):** Q11 admin-listener detection (18 assumes `/api/v1/site` and `/snapshot` are absent on admin; 01's table has them), Q12 redocly pin (2.59.0 here versus 2.63.0 in 06/07), Q13 array search serialisation, Q1 the docId percent-encoding seam with 18.

## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Changed: `api/openapi-public.yaml` `docInstall` uiUrl without extension (CD-RT-04); `common.yaml` `ErrCode` description and example `rate_limited` (CD-ER-01); `RetryJobRequest.key` maxLength 512 (CD-JB-02); fixture `groups.json` freshness codes (CD-ER-01). The copies under this unit's assets are a working snapshot and differ from `20-design/api/` in all three files; `20-design/api/` is authoritative, and the asset copies must be refreshed from it before implementation.
