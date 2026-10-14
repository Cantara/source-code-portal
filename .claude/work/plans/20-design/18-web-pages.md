# Design: unit 18, frontend pages and components (`web/src/features/*`)

| | |
|---|---|
| Unit | `18-web-pages` |
| Status | Design for implementation. Written 2026-10-14 against `10-architecture.md` (section 12 and the revised sections 4-6), `04-decisions.md`, `05-research/08-frontend-stack.md`, `20-design/api/*.yaml` and `20-design/06-httpapi.md`. |
| Owner of | The seven feature slices (`dashboard`, `groups`, `commits`, `docs`, `manifests`, `pages`, `status`), the search palette (`app/search-*`), the page-level shared components, the design tokens (`styles/tokens.css`), the e2e specs and the graph explorer. |
| Does not own | The app shell, router and query-client wiring, snapshot polling, theme, i18n runtime, `components/ui/*`, build/budget scripts, the CSP and the route manifest (unit 17 `web-foundation`, unit 06 `httpapi`, unit 16 `cmd-build-release`). |
| Companion files | `20-design/18-web-pages.assets/` (compile-checked TypeScript for every piece of pure logic in this document, the tokens and the contrast report; README inside). Names of the form `file.ts (cc)` in this document are in that directory and were compiled and tested; everything marked `(spec)` is specified here and not yet compiled. |
| Verification | Independently verified 2026-10-14: 28 findings, 27 fixed in this document and in the companion files (V-26 in part), 1 open (V-28) (section "Verification log (independent verifier)" at the end; report `30-verification/design-18-web-pages.md`). References of the form **V-nn** in this document point to that log. |

How to read this: sections 1-3 say what exists and what the types are; section 4 is the behavioural specification and contains every page (4.2 to 4.14) with wireframes; sections 5-10 are the cross-cutting facts the requirements list asks for (several are short because the unit is a pure client); section 11 is the requirement and guardrail trace; sections 12-15 are the test plan, the task order, deviations and open questions.

---

## 0. Verification log (everything below was checked on 2026-10-14)

### 0.1 Library facts re-verified for this unit

Registry facts come from `npm view` on 2026-10-14; "pin" is what `10-architecture.md` appendix A and unit 17's `package.json` already fix, "latest" is what the registry reported that day. This unit adds no dependency beyond the pins, except the three it must name (section 13, T-18-001): `sigma`, `graphology`, `d3-force` (all in research 08) and `@tanstack/react-virtual` (already in the architecture list, not used in v1, see 4.4).

| Library | Pin | Latest on 2026-10-14 | What was verified and how |
|---|---|---|---|
| React / React DOM | 19.3.0 | 19.3.0 | `renderToStaticMarkup` of the compiled `DataTable` under Vitest (section 12.1). |
| TypeScript | 7.0.2 (6.0.2 alias for tools) | 7.0.2 | `tsc --noEmit` with 7.0.2 **and** with 6.0.3, strict flags listed in 0.2, zero errors. |
| sigma | **3.0.3** | 4.0.1 | Pinned by research 08 (E6/E7). Read `CHANGELOG.md` of sigma 4 on 2026-10-14: 4.0.0 (2026-10-08) is a complete rewrite of the programs system, drops WebGL 1, replaces `hidden`/`forceLabel` node attributes; 4.0.1 followed on 2026-10-13. Research measured 104 KB gz and a 237 ms mount against 36 KB and 79 ms. **Decision: stay on 3.0.3 for v1**; the whole dependency sits behind `GraphController` (4.9.6) so a later move touches one file. Constructor options, `setGraph`, `refresh({skipIndexation, schedule})`, `getCamera().animatedZoom/animatedUnzoom/animatedReset/animate/setState`, `getNodeDisplayData`, `kill` and the `enterNode/leaveNode/clickNode/clickStage` events were checked against the installed 3.0.3 type declarations by compiling `sigma-controller.ts (cc)`. |
| graphology | 0.26.0 | 0.26.0 | Needs `graphology-types@0.24.8` as a dev dependency for the `Graph` class members to type-check (observed). |
| d3-force | 3.0.0 | 3.0.0 | `layout.ts (cc)` uses `forceSimulation`, `forceLink`, `forceManyBody`, `forceCenter`, `forceCollide`, `.randomSource()`, `.alphaDecay()`, `.tick()` and was run: deterministic output, section 4.9.4. |
| TanStack Router | 1.170.41 | 1.170.44 | Probed with `router-probe.test.ts (cc)`: a splat keeps its slashes (`/repos/x/docs/a/b` gives `_splat: "a/b"`), array search values serialise as JSON (`ecosystem=%5B%22maven%22%5D`), `validateSearch` accepts a Zod 4 schema directly (Standard Schema). The router options `defaultPendingMs`, `defaultPendingMinMs`, `parseSearch` and `stringifySearch` exist in the installed `router-core` 1.170.41 type declarations (used by 4.1.1 and OQ-18-04). |
| TanStack Query | 5.104.1 | 5.104.2 | `infiniteQueryOptions` with `getNextPageParam`, `placeholderData: keepPreviousData` compile in `features/*/api.ts (cc)`. |
| TanStack Table | 9.2.6 | 9.3.1 | **v9 API differs from v8.** Verified by compiling and server-rendering `shared/data-table.tsx (cc)`: `tableFeatures({ rowSortingFeature })`, `useTable<typeof features, T>({ features, columns, data, getRowId, manualSorting, enableSortingRemoval, state })` (the option is `features`, not `_features`, and no `_rowModels` is needed with `manualSorting`), `createColumnHelper<typeof features, T>()` with `helper.columns([...])` to keep column value types. |
| `@tanstack/react-virtual` | 3.14.19 | 3.14.19 | Listed, **not used in v1** (4.3: the biggest table is the group repository list, at most 1,000 rows, rendered progressively). |
| Base UI | 1.9.0 | 1.9.0 | peer React ^17 or newer; shadcn registry index (`https://ui.shadcn.com/r/index.json`, 63 items) contains `checkbox`, `combobox`, `accordion`, `native-select`, `button-group`, `pagination`, `sidebar`, `command`, `empty`, `field`, `input-group`, `item`, `kbd`, `spinner`, `table`, `tabs`, `toggle-group`, `chart`. Pages use only items unit 17 already installed plus `checkbox` and `combobox` (T-18-002). |
| cmdk (behind shadcn `command`) | 1.1.1 | 1.1.1 | peer `react ^18 \|\| ^19`; used with `shouldFilter={false}` (the palette ranks itself, 4.13). |
| Lingui | 6.9.0 | 6.9.1 | Message ids only; catalogue extraction is unit 17. |
| Vitest / MSW / Playwright / axe | 5.0.3 / 3.0.2 / 1.63.0 / 4.13.0 | 5.0.4 / 3.0.3 / 1.64.0 / 4.13.0 | Vitest 5.0.3 ran the unit tests (0.2; 133 as authored, 145 after the verification added regression tests). Playwright 1.63.0 (Chromium 153.0.8010.12) was used for one probe only, the CSP/Trusted Types/worker/WebGL check of 10.4; MSW is not run here; the test configuration is unit 17's (`playwright.config.ts` projects `desktop` 1280x800 and `phone` 360x740, the CSP/console gate fixture). |
| Zod | 4.6.5 | 4.6.7 | `.catch(undefined)` on every optional URL field so a tampered URL cannot throw. |
| Tailwind / Vite / lucide | 4.3.3 / 8.3.3 / 1.52.0 | 4.3.3 / 8.3.4 / 1.56.0 | Not exercised here. |

Pins that are behind "latest" are deliberate: the 7-day release cooldown and Renovate move them, not this document.

### 0.2 What was compile-checked and what was not

Directory: `20-design/18-web-pages.assets/web` (53 `.ts/.tsx` files, about 2,285 lines, plus the generated `schema.d.ts` which is rebuilt from the contract, see its README).

* **Compiled:** `tsc --noEmit -p .` with TypeScript **7.0.2** and **6.0.3**, flags `strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `noImplicitOverride`, `noPropertyAccessFromIndexSignature`, `noUnusedLocals`, `noUnusedParameters`, `verbatimModuleSyntax`, `erasableSyntaxOnly`, `isolatedModules`, `skipLibCheck`; types of every request and response come from `openapi-typescript` 7.13.0 run on the real `20-design/api/openapi-admin.yaml` bundle, so a drift in the contract breaks the build of this code. Zero errors.
* **Executed:** Vitest 5.0.3 on Node 26.10.0, **14 files, 145 tests** after the independent verification (11 files and 133 tests as authored; the added tests are the regression tests of V-01 to V-06, V-09, V-13, V-14, V-26, V-27), all green (day grouping with DST golden vectors, avatar initials, freshness and problem mapping, docs id/link/tree logic, graph model, layout determinism and the 1,000-node benchmark, highlight reducers, dependency outline, spread bars, search ranking, status model, display-text hygiene, the contrast gate over `tokens.css`, the server-rendered `DataTable`).
* **Measured:** cold d3-force layout of the 1,000-node/2,500-edge benchmark graph **931 ms** (Node 26.10.0, 300 ticks) and 785-817 ms inside a Chromium module worker, warm layout 181 ms (60 ticks, mean node movement 4.4 units); the whole graph (worker, sigma 3.0.3, WebGL) under the **production CSP with Trusted Types**: zero violations, mount 29-31 ms (10.4, 12.6); graph chunk (sigma 3.0.3 + graphology + controller + reducers, esbuild 0.25 minified, gzip -9) **39.8 KB** (34.7 KB brotli); layout worker **6.0 KB** gz.
* **Not compiled:** every React page component other than `DocContent`, `SafeHtml`, `DataTable` and `DependencyGraphCanvas`; route files (`route.tsx`); Lingui macros; shadcn components; ESLint config. Their props and structure are specified here and in section 13; unit 17's scratch tree compiles the route plumbing they plug into.

### 0.3 Facts relied on from other documents

`10-architecture.md` 11.2/11.3 (routes, static CSP, `routes.json`), 12 (frontend rules), 15 (error model), 18.2 (metrics list); `20-design/api/openapi-public.yaml` and `openapi-admin.yaml` (contract); `20-design/01-kernel.routes.tsv` (route table); `20-design/09-commits-badges.md` 4.9 (day grouping); ADR-009 (live updates), ADR-010 (CSP), ADR-019. Where this document disagrees with one of them it says so in section 14.

---

## 1. Purpose, scope, non-goals, packages

### 1.1 Purpose

Everything a visitor sees in the browser except the shell: twelve page families rendered from the public JSON API (and one admin page from the admin API), designed so that every defect class listed in section 11 is structurally impossible (links from a typed route table, every state designed, untrusted HTML only through one component, no third-party requests, no soft 404, contrast verified by test).

### 1.2 In scope

| Page | Route (SPA) | Slice | Section |
|---|---|---|---|
| Dashboard (the groups index) | `/dashboard` (`/` redirects) | `dashboard` | 4.2 |
| Group | `/groups/$groupId` | `groups` | 4.3 |
| Commit log (all, group, repository) | `/commits`, `/groups/$groupId/commits`, `/repos/$repo/commits` | `commits` (+ `groups` for the group route) | 4.4 |
| Repository overview | `/repos/$repo` | `docs` (layout route owns the composite resource) | 4.5 |
| Documentation viewer | `/repos/$repo/docs/$` | `docs` | 4.6 |
| Repository dependencies | `/repos/$repo/dependencies` | `manifests` | 4.7 |
| Organisation dependencies: graph, table, versions, licences | `/dependencies` | `manifests` | 4.8-4.9 |
| Package (component) | `/packages?purl=` | `manifests` | 4.10 |
| Pages (configured content) | `/pages/$slug` | `pages` | 4.11 |
| Status and admin | `/status` (admin listener only) | `status` | 4.12 |
| Search | command palette, no route | `app/` | 4.13 |
| Error, not-found, unavailable, skew, empty states | root `notFoundComponent`/`errorComponent`, per-route `errorComponent` | `shared/` | 4.14 |

Plus: design tokens and visual language (3.5), cross-cutting behaviour that every page shares (4.1), the dependency graph performance plan (4.9), accessibility (4.15), the e2e world and tests (12), implementation order (13).

### 1.3 Non-goals (v1)

* No shell (header, navigation, announcement banner, theme toggle, connection banner, route announcer): unit 17. This unit states what it needs from them (2.4).
* No Markdown or AsciiDoc rendering, no sanitiser, no syntax highlighter in the browser (server does it; ADR-005). No DOMPurify.
* No charting library in v1 (research 08: Recharts adds 97 KB gz). Spread and licence bars are CSS; the `chart` shadcn component stays installed by unit 17 but unused here.
* No full-text search across document bodies (the API has no search endpoint; 4.13 searches names, titles and summaries; OQ-18-06).
* No writes other than one admin action (retry a parked job, 4.12). No forms that collect credentials.
* No client-side routing for server-answered URLs (`/badges`, `/media`, `*.md`, `llms*.txt`, `/api`, `/_frame`); they are plain `<a href>`/`<img src>` built by `lib/routes.ts`.
* No SSE in v1 (the hook exists in unit 17; this unit only declares which query roots each topic refreshes, 4.1.4). Mermaid rendering is v1.1; v1 shows the diagram source (4.6).
* No i18n beyond English message ids in v1 (catalogue keys are defined here, translations are not; OQ-18-10). Dates and numbers do use `Intl` with the site zone.
* No Firefox/Safari certification beyond what research 08 measured; the supported-browser statement is OQ-18-11.

### 1.4 Directories and files owned (exact)

```
web/src/
  styles/tokens.css                     generated OKLCH tokens, light/dark, viz-1..8, avatar-1..8 (companion: tokens.css (cc))
  app/
    search-model.ts                     pure ranking and item builders (cc)
    search-palette.tsx                  CommandDialog wiring (spec); mounted by the shell
  shared/                               ADDED by this unit (2.3): author-avatar.tsx, commit-row.tsx, badge-strip.tsx, external-link.tsx,
                                        stale-banner.tsx, problem-page.tsx, empty-state.tsx, page-header.tsx, data-table.tsx (cc),
                                        code-blocks.ts, stat.tsx
    avatar.ts (cc) freshness.ts (cc) problem-view.ts (cc) text.ts (cc)      pure helpers behind those components
  features/
    dashboard/   index.ts route.tsx api.ts (cc) search.ts (cc) card-model.ts (cc) group-card.tsx latest-commits-panel.tsx
    groups/      index.ts route.tsx overview.tsx commits.tsx api.ts (cc) search.ts (cc) repo-table.tsx repo-cards.tsx filters.tsx
    commits/     index.ts route.tsx repo-commits.tsx api.ts (cc) search.ts (cc) groupByDay.ts (cc) filter.ts (cc) commit-log.tsx new-commits-pill.tsx
    docs/        index.ts route.tsx readme.tsx doc.tsx api.ts (cc) docId.ts (cc) links.ts (cc) tree.ts (cc) doc-content.tsx (cc)
                 doc-nav.tsx doc-toc.tsx repo-header.tsx repo-aside.tsx status-list.tsx technology-summary.tsx
    manifests/   index.ts repo-dependencies.tsx dependencies-route.tsx package-route.tsx api.ts (cc) search.ts (cc)
                 units-panel.tsx deps-table.tsx versions-view.tsx licenses-view.tsx diagnostics.tsx
                 graph/ model.ts (cc) layout.ts (cc) layout.worker.ts (cc) reducers.ts (cc) sigma-controller.ts (cc) tree.ts (cc) spread.ts (cc)
                        dependency-graph.tsx (cc) graph-toolbar.tsx node-panel.tsx outline.tsx legend.tsx
    pages/       index.ts route.tsx api.ts (cc)
    status/      index.ts route.tsx api.ts (cc) model.ts (cc) sections/*.tsx retry-dialog.tsx
  test/          fixtures.ts (cc) synthetic-graph.ts (cc) handlers.ts (spec, MSW)
web/e2e/         dashboard.spec.ts group.spec.ts commits.spec.ts repo.spec.ts docs.spec.ts deps.spec.ts graph.spec.ts pages.spec.ts status.spec.ts
                 search.spec.ts errors.spec.ts a11y.spec.ts (spec); uses unit 17's e2e/fixtures.ts (gate fixture)
```

Route files named in unit 17's `web/routes.ts` (virtual file routes) are filled in, not renamed: `features/groups/{route,overview,commits}.tsx`, `features/commits/{route,repo-commits}.tsx`, `features/docs/{route,readme,doc}.tsx`, `features/manifests/{repo-dependencies,dependencies-route,package-route}.tsx`, `features/pages/route.tsx`, `features/status/route.tsx`, `features/dashboard/route.tsx`.

---

## 2. Position in the architecture

### 2.1 Import rules (restated, with this unit's files)

```
features/x      -> features/x (itself), components/ui, shared, lib, other features ONLY through their index.ts
                   allowed cross-feature edges:   dashboard -> groups, commits      groups -> commits
                   forbidden by construction:      docs, manifests, pages, status import no other feature
shared          -> components/ui, shared, lib
app             -> everything (through index.ts of a feature)
lib             -> lib
```

Consequences designed into this unit:

1. `CommitRow` and `AuthorAvatar` live in `shared/` because `dashboard`, `groups`, `commits` **and `docs`** (the repository overview shows the latest five commits that arrive inside the composite resource) all draw commit rows.
2. Links between slices are **typed `<Link>`s, not imports**: the group page links to `/dependencies?group=` and `/commits`, the repository page links to `/commits`, the dashboard links to `/groups/$groupId`; the compiler checks route, params and search keys against `routeTree.gen.ts`. No slice reads another slice's query. **Three places needed data of a slice they may not import and are resolved as follows (V-12; the first draft imported `groupsQuery`, `groupQuery` and `repoQuery` from `commits`, an edge the matrix does not have: `commits` imports no feature, and `dashboard -> commits` plus `commits -> dashboard` would also be a cycle):** (a) the group `Select` of `/commits` uses `commitGroupOptionsQuery` in `commits/api.ts`, a second factory over `listGroups` with **the same query key and the same `queryFn` result** as `dashboard`'s `groupsQuery` (one cache entry, two owners of the same endpoint path; a unit test pins the key equality); (b) the title and breadcrumb of `/repos/$repo/commits` (and the blob links of `/repos/$repo/dependencies`) read the **layout route's loader data** through TanStack Router's `getRouteApi("/repos/$repo").useLoaderData()`, which is a route lookup, not a module import; (c) `features/groups/commits.tsx` (the group commit log) imports `CommitLog` from `commits` (allowed) and `groupQuery` from its own slice.
3. The palette (`app/`) is the only consumer that combines data of several slices (groups, repositories, pages, documents, components) and does so through each `index.ts`.
4. The repository composite (`getRepo`) is fetched by the `docs` slice because unit 17's route table makes `features/docs/route.tsx` the layout route of `/repos/$repo` (D-18-07). It carries commits, technology, statuses and dependency digests, so the commits and manifests slices are not needed to render the overview.

### 2.2 Who imports this unit

Only `cmd/scp` (through the embedded build) and unit 17's `app/` (router tree, shell). Nothing else imports a feature's internals. The exported surface of each `index.ts` is the interface list:

```ts
// features/dashboard/index.ts            (cc = compiled)
export { DashboardPage } from "./route"                                    // spec
export { groupsQuery, DASHBOARD_COMMITS } from "./api"                     // (cc) palette + group select
export { dashboardSearch, type DashboardSearch } from "./search"           // (cc)
// features/groups/index.ts
export { GroupPage } from "./overview"; export { GroupCommitsPage } from "./commits"   // spec
export { groupQuery, groupReposQuery, allReposQuery } from "./api"         // (cc) allReposQuery is the palette's repository list
export { groupSearch, REPO_SORTS, type GroupSearch, type RepoSort } from "./search"  // (cc)
// features/commits/index.ts
export { CommitLog, LatestCommits } from "./commit-log"                    // spec
export { commitLogQuery, commitHeadQuery, MAX_LOG_ROWS, type CommitScope } from "./api"   // (cc)
export { commitsSearch, type CommitsSearch } from "./search"               // (cc)
// features/docs/index.ts
export { RepoLayout, RepoOverview, DocPage } from "./route"                // spec
export { repoQuery, docQuery } from "./api"                                // (cc)
export { docSplat, resolveDocId, stripDocExt } from "./docId"              // (cc) palette builds doc links
// features/manifests/index.ts
export { RepoDependenciesPage, OrgDependenciesPage, PackagePage } from "./*" // spec
export { repoManifestsQuery, repoDepsQuery, graphQuery, versionsQuery, licensesQuery, componentQuery, dependentsQuery } from "./api"  // (cc)
export { repoDepsSearch, orgDepsSearch, packageSearch, PURL } from "./search"   // (cc)
// features/pages/index.ts
export { PageView } from "./route"; export { pageQuery, pagesQuery } from "./api"   // (cc for api)
// features/status/index.ts
export { StatusPage } from "./route"; export { statusQuery, useRetryJob, STATUS_POLL_MS } from "./api"   // (cc)
export { overallTone, sortWarnings, rateShare, canRetry, type HealthTone } from "./model"                 // (cc)
```

### 2.3 Consumer-defined interfaces: what this unit asks of `shared/` and `lib/`

The architecture lists `shared/` as freshness-badge, relative-time, safe-html, page-header, data-table, problem-alert, empty-state. Unit 17's scratch tree provides `connection-banner`, `freshness-badge`, `panel-boundary`, `problem-alert`, `relative-time`, `route-announcer`, `safe-html`, `theme-toggle`. This unit therefore **adds** (D-18-06; the cost of a second implementation by two units is higher than the cost of agreeing on this list now):

| Component (`shared/`) | Exact props | Used by |
|---|---|---|
| `PageHeader` | `{ title: string; description?: ReactNode; crumbs?: readonly Crumb[]; actions?: ReactNode; meta?: ReactNode; titleId?: string }` with `type Crumb = { label: string; link?: LinkProps }`; renders `Breadcrumb` (nav `aria-label="Breadcrumb"`), exactly one `h1`, `meta` for badges and freshness | every page |
| `EmptyState` | `{ icon: LucideIcon; title: string; description?: ReactNode; action?: ReactNode; tone?: "neutral" \| "warning" }` over shadcn `Empty` | every list |
| `StaleBanner` | `{ freshness: Freshness \| readonly Freshness[]; subject: string }`; shows nothing for `fresh` and `unknown`; `Alert` with `role="status"` for `stale`, `error` text for `error` (never `role="alert"`: a background refresh failure must not interrupt) | pages with data |
| `ProblemPage` | `{ error: unknown; resource?: "group" \| "repository" \| "document" \| "page" \| "package"; onRetry?: () => void; homeLink?: boolean }`; full-page problem state built on `problemView` (3.3) | route `errorComponent`s |
| `AuthorAvatar` | `{ name: string \| null; login: string \| null; size?: "sm" \| "md" }`; initials from `initialsOf`, background `var(--avatar-N)` from `avatarColour`, `aria-hidden`, no network | commit rows |
| `CommitRow` | `{ commit: Commit; zone: string; showRepo: boolean; time: "relative" \| "clock" }` | all commit lists |
| `BadgeStrip` | `{ badges: readonly Badge[]; max?: number }`; `<img>` with `alt = badge.label`, `height=20`, `loading="lazy"`, same-origin check `isPortalPath(url)` | cards, tables, aside |
| `ExternalLink` | **owned by unit 17** (`shared/external-link.tsx` exists: `{ href; children }`, `target="_blank" rel="noreferrer noopener"`, icon, "(opens in a new tab)", renders only `http:`/`https:` through `isWebUrl`, else plain text). This unit **asks for one additive prop**, `className?: string` (T-18-007), and changes nothing else (V-16: the first draft listed it as a new component and demanded `https://` only; every API URL it receives is a contract `HttpsUrl` anyway) | all GitHub/provider links |
| `DataTable` + `columnHelper` | section 4.1.9, `(cc)` | group repos, dependencies, versions, licences, status tables |
| `Stat` | `{ label: string; value: ReactNode; hint?: ReactNode }` as `dl` group | repo aside, status |
| `code-blocks.ts` | `enhanceCodeBlocks(root: HTMLElement, labels: { copy: string; copied: string; mermaid: string }): () => void` (adds keyboard-focusable `pre`, a Copy button, returns the cleanup) | `DocContent`, `PageView` |

Needed from unit 17 (consumed as-is; the names are the ones in its scratch tree):

| Contract | Signature | Used for |
|---|---|---|
| `lib/api/client.ts` | `api` (openapi-fetch client), `unwrap<T>(Promise<{data?, error?, response}>): Promise<T>` throws `ProblemError` | all `api.ts` files |
| `lib/api/problem.ts` | `class ProblemError { status; code; requestId: string \| null; retryAfterSeconds: number \| null; fieldErrors; detail; isClient }`, client codes `client-network`, `client-aborted`, `client-unparseable`, `client-contract` | `problemView` |
| `lib/api/query-keys.ts` | `qk`, `Topic`, `topicRoots` (query-key roots equal the invalidation topics; `repo` is the composite root) | every query key |
| `lib/api/snapshot.ts` | `useSnapshotSync()` mounted by the shell; `invalidateAll`, `invalidateTopics` | live updates (4.1.4) |
| `lib/format.ts` | `formatRelative(iso, now)`, `formatDateTime(iso, zone)`, `useNow()` (30 s shared ticker) | relative times |
| `lib/persist.ts`, `lib/storage-keys.ts` | `readPersisted(key)`, `writePersisted(key, value \| null)`; `STORAGE` constants | per-viewer conveniences (6.2) |
| `shared/freshness-badge.tsx`, `relative-time.tsx`, `safe-html.tsx`, `panel-boundary.tsx`, `problem-alert.tsx`, `route-announcer.tsx` | as in the scratch tree | everywhere |
| `components/ui/*` | alert, avatar, badge, breadcrumb, button, card, chart, collapsible, command, dialog, dropdown-menu, empty, field, input-group, input, item, kbd, label, popover, progress, scroll-area, select, separator, sheet, skeleton, spinner, switch, table, tabs, textarea, toast, toggle-group, toggle, tooltip | see appendix A |
| `siteQuery()`, `useDisplayZone(): string`, `setDisplayZone` (`lib/api/site.ts`, `lib/format.ts`) | `siteQuery` is a `queryOptions` factory (key `qk.site()`, `staleTime` 60 s); pages read it with `useSuspenseQuery(siteQuery())` (no `useSite()` wrapper is needed); `useDisplayZone()` returns the IANA zone | zone for day grouping and clock times; features flags (`features.manifests`); V-11: both exist in unit 17, the first draft asked for new `useSite()` and `useListener()` hooks |

This unit supplies back to unit 17 (so the shell can use them): `SearchPalette` (`app/search-palette.tsx`, props `{ open: boolean; onOpenChange(open: boolean): void }`) and the key binding contract in 4.13.

### 2.4 Position relative to the Go units (API consumed, nothing imported)

| Unit | What this unit consumes |
|---|---|
| 07 openapi-contract | `openapi-public.yaml`, `openapi-admin.yaml`, `common.yaml` (generated `schema.d.ts`) |
| 06 httpapi, 15 live-ops-overview | content negotiation, problems, ETags, `X-SCP-Build`, `/api/v1/snapshot`, `/api/v1/events` (v1.1), `/api/v1/site`, `/api/v1/status` |
| 08 catalog | groups, repositories, stats, badges |
| 09 commits-badges | commits, `/badges/*` |
| 10 docs-pages-media | docs, pages, `/media/*`, `/repos/{repo}/readme.md` |
| 11-13 manifests | manifests, dependencies, graph, versions, licences, components |
| 14 integrations-access | `ProviderStatus` entries, visibility (the SPA never sees invisible repositories) |
| 16 cmd-build-release | embedding, `-tags spa`, `routes.json`, budgets |

---

## 3. Domain model and types

The SPA has no domain model of its own: every payload type is generated (`components["schemas"][...]`, never re-declared). This section defines what the UI adds: URL state, view models, and the visual language.

### 3.1 URL state (the router is the state container; every filter is shareable)

All schemas are Zod 4 objects passed directly to `validateSearch`; every optional field ends in `.catch(undefined)` (or `.catch(default)`) so a hand-edited or stale URL degrades to the default instead of throwing (GR-15).

| Route | Search schema (`(cc)` file) | Fields |
|---|---|---|
| `/dashboard` | `dashboardSearch` (`dashboard/search.ts`) | `q?: string(<=200)` |
| `/groups/$groupId` | `groupSearch` (`groups/search.ts`) | `q?`, `lifecycle?: ("experimental"\|"production"\|"deprecated")[<=10]`, `type?: ("service"\|"library"\|"website"\|"tool"\|"documentation"\|"other")[<=10]`, `tag?: string[<=10]`, `archived: "include"\|"exclude"\|"only"` (default `include`), `sort: RepoSort` (default `name`; `name`, `-name`, `pushedAt`, `-pushedAt`, `lastCommit`, `-lastCommit`, `openIssues`, `-openIssues`, `lifecycle`, `-lifecycle`) |
| `/commits`, `/groups/$groupId/commits`, `/repos/$repo/commits` | `commitsSearch` (`commits/search.ts`) | `repo?` (slug regexp `^[A-Za-z0-9._-]{1,100}$`), `group?` (`^[a-z0-9-]{1,63}$`), `q?` (filters the loaded rows only) |
| `/repos/$repo` | none (the document is a sub-route) | |
| `/repos/$repo/docs/$` | none; splat = repository path without extension (D-18-01) | |
| `/repos/$repo/dependencies` | `repoDepsSearch` (`manifests/search.ts`) | `scope?: DependencyScope[<=8]`, `relationship: "direct"\|"transitive"\|"all"` (default `direct`), `internal?: boolean`, `ecosystem?: string[<=20]`, `unit?`, `q?`, `sort: "name"\|"-name"\|"ecosystem"\|"-ecosystem"\|"scope"\|"-scope"` (default `ecosystem`) |
| `/dependencies` | `orgDepsSearch` | `view: "graph"\|"table"\|"versions"\|"licenses"` (default `graph`), `group?`, `focus?` (unit id or repo slug, <=420), `depth: 1\|2\|3` (default 1), `ecosystem?`, `scope?`, `intra: boolean` (default false), `q?`, `origin: "all"\|"internal"\|"external"` (default `all`), `minVersions: 1..100` (default 1) |
| `/packages` | `packageSearch` | `purl?` matching `PURL = /^pkg:[a-z][a-z0-9.+-]*\/\S+$/` (<=1024), `scope?`, `group?`. The purl is a **query value, never a path segment** (a purl contains `/`, `@` and `%`) |
| `/pages/$slug`, `/status` | none | |

Array serialisation: TanStack Router 1.170.41 writes arrays as JSON (`ecosystem=["maven","npm"]` percent-encoded, probed). That is valid but unfriendly and differs from the API's repeated-key form. The schemas are written for arrays and are independent of the serialiser; whether unit 17 installs a repeated-key `stringifySearch`/`parseSearch` pair is OQ-18-04. If it does, every array field gets `z.preprocess(v => v === undefined ? undefined : Array.isArray(v) ? v : [v], ...)` (one helper, T-18-006).

Doc identifiers: the API `docId` is `readme` or the repository path with `/` replaced by `~` (`docs~install.md`) **and every byte of the path outside `[A-Za-z0-9._-]` written `%XX` with upper-case hex**, a literal `~` and `%` included, every UTF-8 byte of a non-ASCII name included (contract schema `DocId`: `docs/Release Notes.md` is `docs~Release%20Notes.md`; the encoding is injective; V-04: the first draft refused such paths and its inverse did not decode). The UI URL splat is the path **without the Markdown/AsciiDoc extension** (D-18-01, accepted by unit 10 as its D-9) as the router decoded it. `docIdFromSplat`, `splatFromDocId`, `stripDocExt`, `docSplat(entry)`, `resolveDocId(splat, index)` `(cc, docs/docId.ts)`: the index wins (a path or an extension-less path both resolve to the entry's `id`); the fallback encodes a path to an id by the contract rule and lets the API answer 404 or `not_resident`. A splat with an empty, `.` or `..` segment, a control character, a lone surrogate, or an id longer than 1,024 characters is **not a legal path**: `resolveDocId` returns `null` and the route renders `ProblemPage` (not-found, resource "document") without calling the API. An **empty splat** (`/repos/x/docs/`) replaces to the Documentation entry (4.6).

### 3.2 View models and pure logic (all `(cc)` unless marked)

| Type / function | File | Meaning |
|---|---|---|
| `CardState = {kind:"ok", headline, blurb} \| {kind:"degraded", reason:"missing"\|"unset", blurb}`; `cardState(g)`, `cardBadges(g, max=5)`, `matchesGroup(g, q)` | `dashboard/card-model.ts` | A group whose default repository is missing or unset still gets a card (GR-29); the configured description wins over the repository's (GR-12); the repository-count badge is first |
| `DayGroup<T>`, `dayKey(iso, zone)`, `groupByDay(items, zone)`, `previousDay(day)`, `dayLabel(day, now, zone): {today}\|{yesterday}\|{date}` | `commits/groupByDay.ts` | Calendar-day grouping in the site zone (GR-38); "yesterday" by date arithmetic, never "minus 24 h" |
| `matchesCommit(c, q)`, `countNewer(head, loadedFirstSha): number \| "many"` | `commits/filter.ts` | Client filter on loaded rows; the "N new commits" pill |
| `CommitScope`, `commitLogQuery(scope, pageSize=50)`, `commitHeadQuery(scope)`, `MAX_LOG_ROWS=1000` | `commits/api.ts` | |
| `buildDocTree(entries)`, `flattenDocs(tree)`, `sectionsContaining(entries, id)`, `filterDocs(entries, q)`; `DocNode = {kind:"section", title, children} \| {kind:"doc", entry}` | `docs/tree.ts` | Navigation tree from `DocIndexEntry.section[]` and `order` |
| `buildLinkIndex(links)`, `decideClick(href, clickInfo, index): navigate\|scroll\|native` | `docs/links.ts` | Delegated link policy of rendered HTML |
| `ProblemView`, `problemView(e)`, `retryDelayMs(attempt, retryAfter, rnd)` | `shared/problem-view.ts` | Total mapping from any thrown value to a user-visible problem (3.3) |
| `FreshnessStatus`, `normaliseStatus`, `toneOf`, `worst`, `ageSeconds`, `needsBanner`, `causeId` | `shared/freshness.ts` | Freshness presentation rules (4.1.5) |
| `initialsOf`, `avatarColour`, `fnv1a`, `isBot`, `AVATAR_COLOURS=8` | `shared/avatar.ts` | Initials avatars (3.4) |
| `firstLine`, `truncateGraphemes`, `displayText(s, max=200)` | `shared/text.ts` | Display hygiene for untrusted one-line strings (10.2) |
| `GRAPH_LIMITS`, `buildAdjacency`, `nodeSize`, `ecosystemOrder`, `ecosystemSlot`, `traversalOrder`, `renderability` | `manifests/graph/model.ts` | Graph model (4.9) |
| `layout(LayoutInput): Float32Array`, `seedPosition`, `lcg`, `LayoutRequest/Response` | `manifests/graph/layout.ts` | Deterministic force layout, runs in a worker |
| `HighlightState`, `focusSet`, `nodeView`, `edgeView` | `manifests/graph/reducers.ts` | Hover/selection reducers, pure |
| `dependencyTree(root, dir, adj, byId, maxDepth=3, maxChildren=50)` | `manifests/graph/tree.ts` | Accessible outline with cycle marking |
| `spreadSegments(versions, keep=5)` | `manifests/graph/spread.ts` | Version-spread bar segments: the five most used versions (slots 1-5) and one merged `other` segment (slot 8); shares sum to 1 |
| `sortWarnings`, `rateShare`, `overallTone`, `canRetry` | `status/model.ts` | Status derivations |
| `NavTarget`, `SearchItem`, `fromGroup/fromRepo/fromPage/fromDoc/fromComponent`, `score`, `rank`, `pushRecent`, `GROUP_ORDER`, `MAX_PER_KIND` | `app/search-model.ts` | Palette ranking |
| `sortingFromToken`, `nextSortToken(current, columnId, descFirst)`, `columnHelper`, `DataTable` | `shared/data-table.tsx` | Server-sorted table; a column with `meta: { defaultDesc: true }` starts descending (V-05) |
| `commitKey(c)`, `countNewer(head, firstLoaded)` | `commits/filter.ts` | `repo@sha` identity (GR-07); the pill compares keys, not shas (V-06) |
| `normaliseDocH1(root, pageTitle)` | `docs/headings.ts` | Hides a leading duplicate `h1`, demotes every other `h1` of rendered HTML to `aria-level=2` (V-27) |

### 3.3 `ProblemView` (the single mapping from errors to what the user reads)

```ts
type ProblemKind = "not-found" | "disabled" | "not-ready" | "unavailable" | "rate-limited" | "forbidden"
                 | "unauthorized" | "bad-request" | "server-error" | "offline" | "skew"
interface ProblemView { kind; titleId: string /* problem.<kind>.title */; bodyId: string /* problem.<kind>.body */
  retry: "auto" | "manual" | "none"; retryAfterSeconds: number | null; httpStatus: number; requestId: string | null; code: string }
```

Mapping by stable `code` first (`not-found`, `repo-renamed` -> not-found; `feature-disabled` -> disabled; `not-ready` -> not-ready, auto retry; `queue-full`, `upstream-unavailable`, `client-unparseable` -> unavailable, manual; `rate-limited` -> rate-limited, auto; `forbidden`; `unauthorized`; `validation-failed`, `invalid-cursor`, `method-not-allowed`, `not-acceptable`, `payload-too-large`, `unsupported-media-type` -> bad-request; `internal`, `client-contract` -> server-error, manual; `client-network`, `client-aborted` -> offline, auto), then by HTTP status for an unknown code (404, 429, 401, 403, 503, other 4xx -> bad-request, other -> server-error), anything that is not a `ProblemError` -> server-error (V-03: the first draft mapped a bare `TypeError` to offline with an automatic retry; `unwrap()` already turns every fetch rejection into `client-network`, so a `TypeError` reaching `problemView` is a bug in our own code, for example "Cannot read properties of undefined" in a route component, and must show the server-error page, not an endless "You are offline" loop). **`detail` and `title` text of a problem are never shown as the message** (GR-16): the user sees the localised string for the kind plus the request id. `retryDelayMs`: `Retry-After` wins (capped 120 s), else `min(30, 2^(attempt+1))` seconds with +-20 % jitter from an injected random source.

Unit 17's `ProblemAlert` (inline panel) currently carries its own code-to-title switch (`problemTitle`). Two tables for one concept will drift; **`ProblemAlert` must call `problemView`** (T-18-004, D-18-06). This unit's `ProblemPage` and the inline alert then share ids and retry policy.

### 3.4 Avatars (presentation choice W19, 10-architecture 12.3)

Commit authors are shown as **initials on a deterministic colour; no request is made**. Rules (`shared/avatar.ts`): take `authorName` (trimmed), else `authorLogin`; strip a trailing `[bot]`; split on whitespace only (so the login `ole-b` is one word); one word gives its first grapheme, several words give the first grapheme of the first and of the last word; upper-case with the locale-independent `toUpperCase`; `?` when nothing is left. Graphemes come from `Intl.Segmenter` so emoji and combining sequences stay whole. The colour index is `fnv1a(lowercase(login || name || "?")) % 8 + 1` (stable per person across sessions and machines), mapped to `--avatar-1..8`, all with white text at >= 4.5:1 in both themes (3.5.3). The avatar is `aria-hidden`; the author name is always printed next to it (a name carried only by a picture is an axe failure). A later `/avatars/{login}` proxy (v1.1, `avatars` is a reserved slug) only changes the `<img>` inside `AuthorAvatar`.

### 3.5 Design tokens and visual language

#### 3.5.1 Principles

1. **Plain and dense.** A developer portal read at a desk and checked on a phone: neutral surfaces, one accent, restrained motion.
2. **State is never colour alone.** Every status is icon + text + colour (WCAG 1.4.1).
3. **Light and dark are first-class**, both contrast-verified; `prefers-color-scheme` through `system` default, explicit choice persisted by unit 17.
4. **No third-party requests.** System font stack (0 bytes, no Geist), inline SVG icons (lucide), initials not photos, server-rendered badges.
5. **Targets >= 24x24 CSS px** (WCAG 2.5.8) and 40 px table rows; focus rings visible on every background (>= 3:1) and never obscured by the sticky header (`scroll-padding-top: 4.5rem`, WCAG 2.4.11).

#### 3.5.2 Tokens (generated, `tokens.css (cc)`; OKLCH; verified by `tokens/contrast.py` and `styles/contrast.test.ts`)

Semantic roles (shadcn names so `components/ui/*` work unchanged; extra roles `subtle`, `link`, `success|warning|danger|info` with `-bg`, `viz-1..8`, `avatar-1..8`):

| Token | Light | Dark | Use |
|---|---|---|---|
| `--background` | `oklch(1 0 0)` | `oklch(0.165 0.012 260)` | page |
| `--foreground` | `oklch(0.17 0.012 260)` | `oklch(0.965 0.004 260)` | body text (19.1:1 / 17.4:1) |
| `--card` | `oklch(1 0 0)` | `oklch(0.205 0.013 260)` | cards, popovers |
| `--muted` / `--muted-foreground` | `oklch(0.962 0.005 260)` / `oklch(0.45 0.014 260)` | `oklch(0.255 0.013 260)` / `oklch(0.74 0.012 260)` | secondary text 7.4:1 on background, 6.7:1 on muted (light); 8.4:1 / 6.8:1 (dark) |
| `--primary` / `--primary-foreground` | `oklch(0.46 0.19 262)` / `oklch(0.985 0.003 260)` | `oklch(0.74 0.132 262)` / `oklch(0.16 0.02 260)` | primary buttons 7.1:1 / 8.4:1 |
| `--link` | `oklch(0.46 0.19 262)` | `oklch(0.78 0.11 262)` | link text 7.4:1 / 9.6:1 |
| `--accent` / `--accent-foreground` | `oklch(0.945 0.026 262)` / `oklch(0.28 0.1 262)` | `oklch(0.3 0.06 262)` / `oklch(0.93 0.03 262)` | active nav item 12.6:1 / 11.1:1 |
| `--border` | `oklch(0.915 0.006 260)` | `oklch(0.3 0.014 260)` | decorative dividers (not a required boundary) |
| `--input` | `oklch(0.6 0.012 260)` | `oklch(0.58 0.014 260)` | control boundary (WCAG 1.4.11): 3.95:1 on background, 3.95:1 on card (light); 4.5:1 / 4.2:1 (dark) |
| `--ring` | `oklch(0.5 0.19 262)` | `oklch(0.72 0.14 262)` | focus ring 6.2:1 on background, 5.6:1 on muted (light) |
| `--success` `--warning` `--danger` `--info` (+ `-bg`) | greens/ambers/reds/blues at L 0.47-0.50 on L 0.955-0.96 | L 0.75-0.80 on L 0.27-0.29 | text on its own background >= 5.7:1 in both themes, text on `muted` >= 5.75:1 |
| `--viz-1..8` | blue, green, orange, magenta, teal, red, olive, grey (L 0.45-0.55) | same hues (L 0.72-0.82) | graph ecosystems (1-7) and everything else (8) |
| `--avatar-1..8` | L 0.46-0.52 | identical (white text) | initials backgrounds |
| `--radius` | `0.5rem` | | |
| `--font-sans` / `--font-mono` | system UI stack / `ui-monospace, SFMono-Regular, Menlo, Consolas` | | |

`contrast-report.txt (cc)`: 27 pairs per theme, **0 failures**. The Vitest gate parses `tokens.css`, converts OKLCH to sRGB (gamut-clipped, as browsers do), and fails the build on any pair below its minimum (4.5:1 text, 3:1 non-text). Colours were chroma-fitted so none leaves the sRGB gamut. Unit 17's `index.css` imports `tokens.css` after its own placeholder neutrals so these win.

#### 3.5.3 Scale

| Aspect | Rule |
|---|---|
| Spacing | Tailwind 4 px base; page gutter 16 px (`px-4`) up to 640 px, 24 px above; sections 24-32 px apart; card padding 16 px (`p-4`), dense table cells `px-3 py-2` |
| Width | main column `max-w-6xl` (1152 px); documentation layout `max-w-7xl` (1280 px) with 16 rem nav and 14 rem TOC columns; reading width of prose capped at `72ch` |
| Breakpoints | `sm` 640, `md` 768, `lg` 1024, `xl` 1280; minimum supported width 360 px with no horizontal page scroll (tables become card lists below `md`) |
| Type | body `text-sm` (14 px, line-height 1.5) in lists and tables, `text-base` (16 px, 1.65) in prose; `h1` `text-2xl font-semibold`, `h2` `text-lg font-semibold`, `h3` `text-base font-semibold`; slugs, shas, purls, versions in `font-mono text-[0.8125rem]` |
| Icons | lucide 16 px, `stroke-width 1.75`, always `aria-hidden` next to text; the GitHub mark is the Octicon (already a dependency) |
| Elevation | none except popovers and dialogs (`shadow-md`); cards are bordered, not shadowed |
| Motion | 150 ms ease-out for hover/disclosure; every transition and the graph camera animations are removed under `prefers-reduced-motion: reduce` (`reducedMotion` is passed to `GraphController`); no autoplay, nothing pulses |
| Status vocabulary | fresh = check icon, "Up to date", success; stale = clock, "Delayed", warning; error = triangle, "Update failing", danger; unknown = dotted circle, "Not loaded yet", muted. Lifecycle: experimental = info, production = success (label "Active"), deprecated = warning. Provider state: ok = success, failing = danger, unstable = warning, unknown/disabled = muted |
| Dark-mode images | badges are server SVG with their own palette (`badges.palette: aa`), shown on a `--card` plate with `rounded-sm` so both themes read; README images are not recoloured |
| Print | `@media print` hides header, nav, asides; prose and tables print in black on white |

---

## 4. Behavioural specification

### 4.1 Rules every page shares

#### 4.1.1 Loading data

* One **primary resource** per page is fetched in the route `loader` with `queryClient.ensureQueryData(options)` (`staleTime` from the `api.ts` factory), so a failure is a route error (`errorComponent` -> `ProblemPage`) and a success has data before first paint of the body. Secondary resources (the latest-commits panel, aside statuses, the dependency table under a header) are `useSuspenseQuery`/`useSuspenseInfiniteQuery` inside `<PanelBoundary><Suspense fallback={skeleton}>`, so they fail alone with an inline `ProblemAlert` and a Retry button. **It must be the suspense hooks (V-17):** `PanelBoundary` is an error boundary, and `useQuery` returns its error instead of throwing it, so a boundary around a plain `useQuery` never fires. The default `throwOnError` of the suspense hooks throws only when the query has no data (`query.state.data === undefined`), which is exactly the stale-while-error rule of 4.1.4 (4): a failed background refetch with data present renders nothing extra.
* Pages read data with `useSuspenseQuery`. The router shows `pendingComponent` (a skeleton with the final layout's dimensions) only after 150 ms and keeps it at least 300 ms (`defaultPendingMs: 150`, `defaultPendingMinMs: 300` in unit 17's `createAppRouter`, which does not set them yet: T-18-006) so a fast response never flashes.
* `placeholderData: keepPreviousData` on every query whose key depends on filters (group repositories, dependencies, graph, versions): changing a filter keeps the old rows on screen (dimmed, `aria-busy="true"`) until the new ones arrive; no empty flash.
* Every `queryFn` forwards `signal`, so leaving a page or typing a new filter aborts the in-flight request (a `client-aborted` problem is never rendered).
* Query keys: root = invalidation topic (unit 17's `qk`): `catalog`, `commits`, `docs`, `manifests`, `pages`, `status`, plus the composite `repo`. Keys used by this unit: `["catalog","groups",{sort:"config"}]`, `["catalog","group",id]`, `["catalog","group-repos",id,search]`, `["catalog","repos","all"]`, `["commits","list",{...}]`, `["commits","log",{repo,group,pageSize}]`, `["commits","head",{repo,group}]`, `["repo",slug]`, `["docs",slug,"doc",docId]`, `["manifests","repo",slug]`, `["manifests","deps",slug,search,pageSize]`, `["manifests","graph",{...}]`, `["manifests","versions",{...},pageSize]`, `["manifests","licenses",{group}]`, `["manifests","component",purl]`, `["manifests","dependents",purl,{...},pageSize]`, `["pages","list"]`, `["pages","page",slug]`, `["status"]`.

#### 4.1.2 The state matrix (every list and every page designs all of these)

| State | Trigger | Presentation | Announced |
|---|---|---|---|
| Loading | pending > 150 ms | skeleton of final size, `aria-busy="true"` on the region, one visually hidden `role="status"` text ("Loading groups") | once |
| Empty (nothing exists) | 200 with zero items and no active filter | `EmptyState` with icon, one-sentence cause, one next action | no |
| Empty (filtered out) | 200 with zero items and a filter | `EmptyState` "No ... match" + "Clear filters" button; the filter controls stay | result count live region |
| Error (page) | primary resource rejects | `ProblemPage` (4.14), request id, Retry (auto for `not-ready`, `rate-limited`, `offline`) | page title changes; focus to `h1` |
| Error (panel) | secondary resource rejects | `ProblemAlert` inside the panel, Retry | `role="alert"` once |
| Stale | `freshness.status = stale` | data stays; `StaleBanner` ("Showing data from 2 h ago; GitHub rate limit reached") + header `FreshnessBadge` | `role="status"`, polite |
| Update failing | `freshness.status = error` with data | same as stale, danger tone text "Updates are failing" + cause | polite |
| Unknown | `freshness.status = unknown` | quiet "Not loaded yet" badge; no banner (a banner for never-fetched would shout during warm-up) | no |
| Partial | `truncated: true`, `nextCursor` present, cap reached | an `Alert` stating the cap and the way to narrow (filters) | no |
| Refetching | background refetch | nothing visible (no spinner); `RelativeTime` keeps ticking | no |

#### 4.1.3 Titles, headings, landmarks

`<title>` is `"{page title} - {site.title}"` (React 19 `<title>` element in the route component, unit 17 convention). Exactly one `h1` per page, text given per page below; sections are `h2`, cards and rows `h3`. Landmarks come from the shell (`header`, `nav aria-label="Primary"`, `main#main`); this unit adds `nav aria-label="Breadcrumb"`, `nav aria-label="Repository"` (route tabs), `nav aria-label="Documentation"`, `aside` regions each with `aria-labelledby`, and `role="search"` on filter groups.

#### 4.1.4 Live updates

v1: unit 17's `useSnapshotSync` polls `GET /api/v1/snapshot` every 30 s (visibility-aware, 304 when unchanged) and, when `version` moves, invalidates every query except `snapshot`; active queries revalidate by ETag (cheap 304). Nothing in this unit polls except the status page (10 s, 4.12). Consequences designed in: (1) refetched data replaces rows in place with stable keys, never remounting a list; (2) the commit log does not insert rows under the reader's cursor (4.4: `commitLogQuery(scope, pageSize, follow)` is a **disabled** query while the reader has scrolled, because unit 17 invalidates every query on a snapshot change and refetches every active query every 120 s, and an infinite query refetches all its pages; V-13); (3) the document viewer keeps scroll position and the open heading (`docQuery` returns an equal `html` string when unchanged, structural sharing keeps the object, and `SafeHtml` memoises the `{ __html }` value on the string: under Trusted Types `createHTML()` returns a **new object on every call** and React re-assigns `innerHTML` whenever the value is not `===`, which rebuilt the whole document on every parent render and removed the Copy buttons; V-02); (4) a refetch error never clears data (stale-while-error is the server's behaviour and the SPA's: a failed background refetch keeps `data`, shows no panel error, and `StaleBanner` appears only when the server says so; TanStack Query's `isError` with `data` present renders nothing extra).

v1.1 (SSE): the hook maps `invalidate.topics` to query roots with `topicRoots` and calls `invalidateTopics`; this unit's keys already satisfy it. Topic to what refreshes here: `site` -> shell; `catalog` -> dashboard, group, palette; `commits` -> commit logs and panels, `repo` composite; `docs` -> doc viewer, README; `manifests` -> dependency pages, `repo` composite; `pages` -> pages; `status` -> status page.

#### 4.1.5 Freshness (GR-26, GR-40)

Every response that has a `freshness` object shows it: the header of each page carries `FreshnessBadge` for the **worst** of the freshness objects the page used (`worst(fs)`: error > stale > unknown > fresh); each section that has its own object (repository `sections.commits|docs|manifests|integrations`, provider statuses) carries a small badge; tooltips (`Tooltip`) give `asOf`, `fetchedAt`, `checkedAt`, `staleSince` in the site zone and the cause. `needsBanner(status)` is true only for `stale` and `error`. The cause text comes from `lastErrorCode` by **exact code first, then prefix** (`causeId`; exact codes `not_found`, `forbidden`, `unavailable` and their `github.` forms exist because unit 09 emits the undotted set, V-26): `github.rate_limited` -> "GitHub rate limit reached", `github.unauthorized` -> "GitHub credentials were rejected", `github.suspended` -> "The GitHub installation is suspended", `github.` -> "GitHub is not answering", `parse.` -> "A file could not be parsed", `descriptor.` -> "The repository descriptor has problems", exact `not_found` / `github.not_found` -> "The source no longer has this item", `forbidden` / `github.forbidden` -> "Access was refused; check the credentials", `unavailable` -> "The source is not answering", otherwise "Unknown cause" with the raw code in the tooltip only. The exact vocabulary of `lastErrorCode` is not pinned in the contract (OQ-18-03); the prefix table keeps the SPA correct for any value. The open enum `status` is normalised: an unrecognised value is `unknown`.

Age text: relative ("3 min ago") with the exact time in the tooltip and as `<time datetime>`; the header status of the shell ("Updated ... , N repositories") is the shell's, from `site.lastFullSync` and `site.repoCount`.

#### 4.1.6 Errors

* Route and panel errors use `problemView` (3.3). Auto-retry: `not-ready` (cold start) retries with `retryDelayMs`, with a visual countdown that is `aria-hidden` (a ticking number must not be announced), `rate-limited` honours `Retry-After`, `offline` retries when the browser fires `online` and on a 15 s timer. Manual: `unavailable`, `server-error`, `skew`.
* **308 for a renamed repository** (`RepoRenamed`): a full page load of an old URL gets the server's own 308 to the current slug (`negotiated.repo`, with the rest of the path and the query kept). Inside the SPA an old slug can still reach an API call (a stale link in cached data, a palette recent): `fetch` follows the redirect to the new slug's API URL and the body carries the new `slug`/`repo`. The repository layout route compares `data.slug` with the URL param and calls `router.navigate({ to: <same route>, params: { repo: data.slug }, replace: true })` (`useCanonicalRepo`, spec). Child routes do the same with `data.repo` (docs, manifests). The old URL therefore keeps working and the address bar converges, without the SPA ever parsing a `Location` header.
* **Version skew**: `X-SCP-Build` mismatch is detected by unit 17 (`serverStatus.buildSkew`); its banner offers a reload. A failed dynamic import after a deploy (`vite:preloadError`) reloads once (guarded by `scp-reload-guard`). Both are unit 17's; pages only need to be lazy-loadable.
* Real status codes (GR-16): unknown **shapes** (`/repos/a/b/c/d` is still a docs splat, but `/nope`) get the server's real 404 with the shell body; an unknown **slug** returns 200 shell + API 404, so the SPA renders `ProblemPage` kind `not-found` whose document title says "Not found" (the HTTP status of that navigation is the server's; the SPA cannot and does not claim one).

#### 4.1.7 Time and numbers

Zone: `useDisplayZone()` (from `site.displayTimeZone`, an IANA name; the server guarantees validity, the SPA still catches `RangeError` and falls back to `UTC`). Day headings, clock times and tooltips use `Intl.DateTimeFormat(locale, { timeZone })`; stored data is UTC. Relative times use `Intl.RelativeTimeFormat` (unit 17's `formatRelative`: under 45 s is "now"). Numbers use `Intl.NumberFormat` (compact above 9,999: "12.4k"). Sizes (bytes) with `Intl.NumberFormat` and `unit: "kilobyte"/"megabyte"` style. Never format with the browser zone unless `site.displayTimeZone` is absent (admin listener: UTC with the label "UTC").

#### 4.1.8 Links

Internal navigation is always a typed `<Link to=... params=... search=...>`: a wrong route, param or search key is a compile error (GR-15). The `uiUrl`/`apiUrl` strings in payloads are **not** used to navigate (the router builds the URL from the route and the identifiers in the payload); the only exception is `DocLink.href` inside server-rendered HTML, which the server rewrote to portal routes and which `DocContent` hands to `router.navigate({ href })` after matching it against `Doc.links`. Server-answered URLs come from `lib/routes.ts` builders (`serverRoutes.badge`, `readmeMd`, `llms`, ...). External links (`htmlUrl`, release `url`, `sourceUrl`, provider `detailUrl`, `Link.url`) go through `ExternalLink` (10.3). ESLint forbids string literals starting with `/api/`, `/repos/`, `/groups/`, `/badges/`, `/media/` outside `web/routes.ts` and `lib/routes.ts`.

#### 4.1.9 Tables and lists

* Tables use `shared/data-table.tsx (cc)`: `caption` (visually hidden, required), header buttons with `aria-sort`, sort token = the API's token (`-pushedAt`), tri-state is two-state (ascending then descending then ascending: the API always sorts, there is no "unsorted"), rows already sorted by the server (`manualSorting`). First activation of a new column sorts ascending except columns whose natural order is descending (`pushedAt`, `lastCommit`, `openIssues` start descending: `defaultDesc` set in the column meta).
* Below `md` (768 px) the same data renders as a **card list** (`<ul>` of `<li>` with the same fields, labels as `dt`/`dd`), chosen by `useMediaQuery("(min-width: 768px)")` so only one is in the DOM; sort becomes a `Select`.
* Rows are not clickable as a whole; the title cell holds the link (a whole-row click handler is invisible to keyboard and assistive tech and fights text selection).
* Long lists render **progressively** in steps of 100 rows ("Show 100 more", `aria-live` count) when the server already returned everything (group repositories, status tables); cursor-paged resources use "Load more" (4.1.10).

#### 4.1.10 Cursor pagination ("Load more")

Infinite queries (`commitLogQuery`, `repoDepsQuery`, `versionsQuery`, `dependentsQuery`) never set `maxPages` (a page cap would silently drop the newest rows). A button `Load more (50)` appends a page; while fetching it is `aria-disabled` with a `Spinner`; after loading, focus moves to the first new row (`tabIndex=-1`) and a polite live region says "50 more commits loaded, 100 shown". No infinite scroll (keyboard users cannot reach the footer; the API is bounded). At a client cap (`MAX_LOG_ROWS=1000` for commits, 2,000 for dependency rows) the button is replaced by a note pointing at filters. A cursor the server rejects (`invalid-cursor` 400, e.g. after a snapshot change) resets the list to its first page with a toast "The list changed; showing the newest items".

#### 4.1.11 i18n message ids

Ids are stable dotted keys, not the English text, for everything shown to users: `page.<slice>.<element>` (`page.dashboard.title`), `problem.<kind>.title|body`, `freshness.cause.<x>`, `state.<name>`, `a11y.<name>`. Plurals use ICU (`{count, plural, one {# repository} other {# repositories}}`). Only `en` is shipped; catalogue extraction and the missing-id lint belong to unit 17.

---

### 4.2 Dashboard (`/dashboard`)

**Purpose.** One card per group in configuration order, plus a feed of the newest commits across everything.

**Data.**

| Call | Query | Role | Notes |
|---|---|---|---|
| `GET /api/v1/groups?sort=config` | `groupsQuery()` | primary (loader) | one page (default limit 1,000); if `page.nextCursor` is non-null the page shows an `Alert` "Showing the first 1,000 groups" |
| `GET /api/v1/commits?limit=10` | `latestCommitsQuery()` | secondary panel | `DASHBOARD_COMMITS = 10` (one named constant, not a literal repeated at each use; optionally configurable, D-18-03; GR-14) |
| `GET /api/v1/site` | shell | header status, zone | |

**Component tree.**

```
DashboardPage (route.tsx)
  PageHeader title="Dashboard" description="{groupCount} groups, {repoCount} repositories"   meta=<FreshnessBadge worst(groups, commits)/>
  StaleBanner subject="the group list"
  div.grid.gap-6.lg:grid-cols-[minmax(0,1fr)_22rem]
    section aria-labelledby="groups-h"
      div.flex  h2#groups-h "Groups"   InputGroup[role=search] (Input type=search "Filter groups" + InputGroupAddon Search icon + Button clear)   p[aria-live=polite].sr-only "{shown} of {total} groups"
      ul.grid.gap-4.sm:grid-cols-2.xl:grid-cols-3
        li > GroupCard
              Card > CardHeader(CardTitle as h3 > Link to=/groups/$groupId, FreshnessBadge only when not fresh) CardDescription(description, line-clamp-3)
                   > CardContent( DefaultRepoLine, RepoCountLink, BadgeStrip max=5 )
                   > CardFooter( Button variant=link size=sm Link "Commits", Button variant=link size=sm Link "Dependencies" [features.manifests] )
    aside aria-labelledby="latest-h"
      PanelBoundary > LatestCommitsPanel
        div.flex  h2#latest-h "Latest commits"  FreshnessBadge(commits.freshness) size=sm
        ul > CommitRow time="relative" showRepo   (x10)
        Button variant=link Link to=/commits "All commits"
```

**Wireframe, desktop (1280 px).**

```
+------------------------------------------------------------------------------------------+
| Source Code Portal   Dashboard  Commits  Dependencies  Docs        [Search  Ctrl K] [Theme]|  <- shell
+------------------------------------------------------------------------------------------+
| Dashboard                                                          (check) Up to date 2 m |
| 6 groups, 94 repositories                                                                 |
|                                                                                           |
| Groups                       [ Filter groups                                  x ]         |
| +----------------------+ +----------------------+ +----------------------+  Latest commits|
| | Platform             | | Billing       Delayed| | Identity             |  (check) 1 m  |
| | Shared libraries and | | Invoicing and        | | Identity and access  |  (KN) Fix VAT  |
| | infrastructure.      | | payments.            | | management.          |   billing-svc  |
| | Start here: Core Lib>| | (!) Default repo not | | Start here: IAM core |   08:12 3 m   |
| | 18 repositories      | |     found            | | 31 repositories      |  (D) Bump vite |
| | [18 repos][3 issues] | | 7 repositories       | | [31 repos][build ok] |   core-lib 7 m |
| | Commits Dependencies | | [7 repos]            | | Commits Dependencies |  ...           |
| +----------------------+ | Commits  Dependencies| +----------------------+  All commits > |
| ...                      +----------------------+                                         |
+------------------------------------------------------------------------------------------+
```

**Wireframe, phone (360 px).**

```
+--------------------------------------+
| = Source Code Portal     [S] [Theme] |
+--------------------------------------+
| Dashboard                            |
| 6 groups, 94 repositories            |
| (check) Up to date 2 m               |
| Groups                               |
| [ Filter groups                  x ] |
| +----------------------------------+ |
| | Platform                         | |
| | Shared libraries ...             | |
| | Start here: Core Library >       | |
| | 18 repositories                  | |
| | [18 repos][3 issues][v2.7]       | |
| | Commits    Dependencies          | |
| +----------------------------------+ |
| | Billing  (Delayed)               | |
| ...                                  |
| Latest commits            1 m ago    |
| (KN) Fix rounding of VAT ...         |
|      billing-service  Kari N.  3 m   |
| ... All commits >                    |
+--------------------------------------+
```

**Behaviour.**

```
cardState(g):
  blurb  = g.description ?? g.defaultRepo?.description ?? null          # configuration wins (GR-12)
  if g.defaultRepoState == "ok" and g.defaultRepo: ok(headline = g.defaultRepo.title ?? g.defaultRepo.name)
  else: degraded(reason = g.defaultRepoState == "unset" ? "unset" : "missing")
DefaultRepoLine:
  ok        -> "Start here: <Link to=/repos/$repo>{headline}</Link>"
  missing   -> warning icon + "Default repository not found"          # card still links to the group (GR-29)
  unset     -> nothing
badges = cardBadges(g, 5)       # kind "repos" first, then API order, max 5; <img alt=label height=20 loading=lazy>
filter  = q (URL, debounced 200 ms, replace:true) matches id, displayName, description, default repo title/name (case-insensitive substring)
order   = server order (configuration order; "sort=name" is not offered on the dashboard: the operator's order is the intended reading order)
```

**States.** Loading: six card skeletons (`min-h-40`) and ten commit-row skeletons. Empty groups: `EmptyState` icon `LayoutGrid`, "No groups are configured", description "Groups are defined in the portal configuration under catalog.groups.", action: Link "Commits". Empty filter: "No group matches \"{q}\"" + "Clear filter". Commits panel empty: "No commits yet" with "The portal is still collecting commits from GitHub." Error (groups): `ProblemPage`; (commits): panel `ProblemAlert`. Stale/error freshness: banner, chips on affected cards. 503 `not-ready`: `ProblemPage` kind not-ready, auto retry.

**Accessibility and keyboard.** Cards are `li` of an unordered list (the count is announced); the title link is the first tab stop in a card; there is no whole-card click. The filter is `type="search"` with a visible label ("Filter groups") and an accessible clear button (`aria-label="Clear filter"`, 24 px). The result count is a polite live region updated 400 ms after typing stops. The commits panel is an `aside` named by its `h2`; its rows are not live (no announcement on refresh: constant updates are noise; GR-40 asks for a visible "as of", which the header badge and `RelativeTime` give).

**Tests.**

| Id | Kind | Assertion |
|---|---|---|
| C18-DASH-01 | component | `GroupCard` ok state: title link, description, default repo link, badges in order `repos`, `issues`, ... max 5, each `img` has non-empty `alt` |
| C18-DASH-02 | component | `GroupCard` with `defaultRepoState: "missing"`: card rendered, warning text present, no default-repo link; with `unset`: no line |
| C18-DASH-03 | component | configured description wins over the repository description; null description falls back |
| C18-DASH-04 | unit (cc) | `cardState`, `cardBadges`, `matchesGroup` table tests |
| E18-DASH-01 | e2e | against `fakegithub`: groups in configuration order; each card title link goes to `/groups/<id>` |
| E18-DASH-02 | e2e | typing in the filter updates `?q=` (replace), result count text, clearing restores; reload keeps the filter |
| E18-DASH-03 | e2e (stub) | groups response with one `stale` freshness: banner text and card chip visible; with `lastErrorCode: github.rate_limited` the cause text appears |
| E18-DASH-04 | e2e (stub) | `/api/v1/commits` answers 503 `not-ready`: the groups still render, the panel shows an alert with Retry |
| E18-DASH-05 | e2e | phone project: no horizontal scroll (`document.documentElement.scrollWidth <= innerWidth`), cards single column |
| E18-DASH-06 | e2e | axe: zero violations in light and dark, desktop and phone; CSP/console gate green |
| E18-DASH-07 | e2e (stub) | `scp.route.ready` for `/dashboard` with a warm cache <= 1,000 ms on CI hardware (informational in v1, hard at M7) |

---

### 4.3 Group (`/groups/$groupId`)

**Purpose.** The repositories of one group with filters, sorting and statistics, the group's default repository, and its latest commits.

**Data.**

| Call | Query | Role |
|---|---|---|
| `GET /api/v1/groups/{groupId}` | `groupQuery(id)` | primary (loader): header, badges, default repo, freshness |
| `GET /api/v1/groups/{groupId}/repos?q&lifecycle&type&tag&archived&sort` | `groupReposQuery(id, search)` | primary (loader with the URL's search): the table (whole collection in one page, `limit` default and maximum 1,000; every filter is server-side). If `page.nextCursor` is non-null (more than 1,000 repositories in the group) the page shows an `Alert` "Showing the first 1,000 repositories of this group; narrow with the filters" exactly like the dashboard does for groups (V-23; the first draft was silent about truncation) |
| `GET /api/v1/commits?group=<id>&limit=50` | `commitHeadQuery({group})` (first 10 shown) | secondary panel; the same key feeds the commits page pill |

`404 not-found` -> `ProblemPage` resource "group" with a link to the dashboard. `RepoList.items[]` is `RepoSummary`: `slug`, `name`, `title` (`{value, source}`), `summary?`, `type?`, `lifecycle?`, `archived`, `fork`, `visibility`, `defaultBranch`, `htmlUrl`, `tags[]`, `groups[]`, `stats` (`openIssues`, `stars`, `license?`, `latestRelease?`, `pushedAt`, `headSha`, `lastCommitAt`), `badges[]`, `freshness`.

**Component tree.**

```
GroupPage
  PageHeader crumbs=[Dashboard > {displayName}] title={displayName} description={group.description}
             meta=<FreshnessBadge/> <BadgeStrip group badges/>
             actions=< Button Link "Commits" /> < Button variant=outline Link "Dependencies" [features.manifests] /> <DropdownMenu "For agents": llms.txt, llms-full.txt, dependencies.md (server URLs)/>
  StaleBanner
  DefaultRepoCard [defaultRepoState==ok]: Card > "Start here" ... title link, description, BadgeStrip
  div.grid.lg:grid-cols-[minmax(0,1fr)_20rem]
    section aria-labelledby="repos-h"
      h2#repos-h "Repositories"  <span aria-live=polite>{n} repositories</span>
      RepoFilters:   InputGroup[role=search] q | Popover(Lifecycle checkboxes) | Popover(Type checkboxes) | Combobox multi (Tags) | NativeSelect archived (shadcn `native-select`, an addition of T-18-002) | (phone) Select sort | Button "Clear all"
      ActiveChips:   Badge variant=secondary + remove button per active filter
      desktop: DataTable(caption="Repositories of group {name}") columns [Repository, Type, Lifecycle, Last commit, Open issues, Release, Status]
      phone:   ul > li > RepoCard
      Button "Show 100 more" when rows > 100
    aside aria-labelledby="latest-h": h2 "Latest commits" + ul CommitRow(time=relative, showRepo) x10 + Link "All commits for this group" -> /groups/$groupId/commits
```

**Wireframe, desktop.**

```
+------------------------------------------------------------------------------------------+
| Dashboard > Platform                                                                      |
| Platform                              [Commits] [Dependencies] [For agents v]            |
| Shared libraries and infrastructure services.             (check) Up to date 3 m          |
| [18 repositories] [3 open issues]                                                         |
| +------------------------------------------------------------------------+               |
| | Start here   Core Library  Shared building blocks for Acme services.    |               |
| +------------------------------------------------------------------------+               |
| Repositories   18 repositories                                         | Latest commits   |
| [ Filter repositories    ] [Lifecycle v] [Type v] [Tags v] [Archived: Include v] [Clear]   |
| Active: (Production x) (service x)                                     | (KN) Fix ...     |
| +-----------------------------------------------------------------------+ core-lib  3 m   |
| | Repository          | Type    | Lifecycle  | Last commit v| Issues | Release| Status |  ...            |
| | Core Library        | library | Active     | 3 min ago    | 3      | v2.7.1 | [build]| All commits for  |
| |  core-lib  Shared building blocks...                                  | this group >    |
| | Auth Client (Archived) ...                                            |                 |
| +-----------------------------------------------------------------------+                 |
| [Show 100 more]                                                                           |
+------------------------------------------------------------------------------------------+
```

Phone: header stack, actions in a `DropdownMenu` "More", filters in a `Sheet` opened by `Filters (2)`, sort `Select`, repositories as cards:

```
+--------------------------------------+
| Dashboard > Platform                 |
| Platform                             |
| Shared libraries ...                 |
| [Commits] [More v]                   |
| [ Filter repositories           ]    |
| [Filters (2)]  Sort: [Last commit v] |
| 18 repositories                      |
| +----------------------------------+ |
| | Core Library      (Active)       | |
| | core-lib  library                | |
| | Shared building blocks ...       | |
| | Last commit 3 m  3 issues v2.7.1 | |
| | [build ok] [security 0]          | |
| +----------------------------------+ |
+--------------------------------------+
```

**Behaviour.**

```
columns (id = sort token without sign):
  name        Repository: Link to=/repos/$repo({title.value}) ; below mono slug ; badges: Archived (Archive icon), Fork, Private (Lock icon + text) ; summary line-clamp-2 (displayText) ;
              trailing icon buttons (aria-label "{title} on GitHub" = ExternalLink(htmlUrl), "Commits of {title}" = Link to=/repos/$repo/commits): every row offers GitHub, docs and commits; the docs link is the title link itself (V-25)
  type        Badge outline (hidden when null)
  lifecycle   Badge: experimental info, production success labelled "Active", deprecated warning
  lastCommit  RelativeTime(stats.lastCommitAt) or "none" ; default sort direction descending (`meta: { defaultDesc: true }`, `nextSortToken(..., descFirst)`)
  openIssues  number (`stats.openIssues` is nullable: null renders "-", never "null" or 0), ExternalLink to {htmlUrl}/issues when > 0 ; default descending
  release     latestRelease.tag as ExternalLink(latestRelease.url) or "-"
  status      BadgeStrip of badges whose kind is "build" or "security" (max 2); else empty cell
sorting: URL `sort` token; header button toggles (4.1.9). `pushedAt` has no column (offered in the phone Select only).
filters: every change navigates with replace:true; q debounced 250 ms. Facet options: lifecycle and type are the closed enums; tag options are the union of tags in the current result and the selected ones (limitation: a tag that no visible repository has is not offered; OQ-18-15).
count:   `{n} repositories` polite live region, debounced.
default repo: shown when defaultRepoState == "ok"; "missing" -> Alert warning "The configured default repository is not tracked." (GR-29), "unset" -> nothing.
```

**States.** Empty (group has no visible repositories): `EmptyState` "No repositories in this group yet" with the cause ("none of its patterns match a repository visible to you"). Empty filtered: "No repositories match these filters" + Clear all. Loading on filter change: old rows dimmed. Error: `ProblemPage` (404 group), panel alert (commits). Stale: banner.

**Accessibility and keyboard.** Table caption names the group; sortable headers are buttons with `aria-sort`; filter popovers are `Popover` + `Checkbox` lists (`role="group"` with legend), `Esc` closes and returns focus to the trigger; the `Sheet` on phones traps focus and restores it; active-filter chips are buttons "Remove filter Production" (24 px). The live count is the only announcement on filtering.

**Tests.**

| Id | Kind | Assertion |
|---|---|---|
| C18-GRP-01 | component | `DataTable` header `aria-sort` follows `?sort=`; activating a header calls `navigate` with the next token (`name` -> `-name`; `lastCommit` starts `-lastCommit` through `meta.defaultDesc`; unit-tested as `nextSortToken(..., descFirst)`, V-05) |
| C18-GRP-02 | component | repository cell renders Archived/Fork/Private as text + icon; `displayText` strips a bidi override from a summary |
| C18-GRP-03 | component | `groupSearch` fallback: `?sort=zzz&archived=maybe` renders with `name`/`include` and no error |
| C18-GRP-04 | component | phone width renders cards, not the table (one of the two in the DOM) |
| E18-GRP-01 | e2e | the group named by `groups[0].id` of `GET /api/v1/repos/payments-api` lists a `payments-api` row (group ids are read from the API, never hard-coded; the dev configuration is unit 16's); `archived-tool` (private, archived) is absent under `public_only` |
| E18-GRP-02 | e2e | filter by lifecycle updates URL and rows; reload keeps them; "Clear all" restores |
| E18-GRP-03 | e2e (stub) | group with `defaultRepoState: missing`: warning shown, page usable |
| E18-GRP-04 | e2e | unknown group id: `ProblemPage` title "Group not found", link to dashboard, document `<title>` says Not found |
| E18-GRP-05 | e2e | keyboard only: tab to Lifecycle, open with Space, tick, `Esc`, focus returns; table header sort with Enter |
| E18-GRP-06 | e2e | axe light/dark desktop/phone |

---

### 4.4 Commit logs (`/commits`, `/groups/$groupId/commits`, `/repos/$repo/commits`)

**Purpose.** The default-branch commit history (Q-18), newest first, grouped by calendar day in the site zone, for everything, one group or one repository. One component (`CommitLog`, exported from `commits/index.ts`) serves the three routes; the group route lives in the `groups` slice file `commits.tsx` and imports `CommitLog` (allowed edge).

**Data.**

| Call | Query | Role |
|---|---|---|
| `GET /api/v1/commits?repo=<slug>` or `?group=<id>`, `&limit=50&cursor=` | `commitLogQuery(scope)` (infinite, no `maxPages`) | primary |
| `GET /api/v1/commits?repo=<slug>` or `?group=<id>`, `&limit=50` | `commitHeadQuery(scope)` | the "new commits" probe: revalidated by the snapshot poll, never rendered as rows |
| `GET /api/v1/groups/{id}` or `/repos/{repo}` | group route: `groupQuery` (own slice `groups`); repository route: the **layout loader data** of `/repos/$repo` through `getRouteApi("/repos/$repo").useLoaderData()` (no import of `docs`); `/commits`: `commitGroupOptionsQuery` for the group `Select` (2.1, V-12) | page title and breadcrumb (cached from the parent page) |

The API retains at most 30 commits per repository (`commits.keep`), so the all-repositories log is bounded by `30 x repositories` and `MAX_LOG_ROWS = 1000` rarely binds. `404` for an unknown repo or group -> `ProblemPage`. A `repo` alias of a renamed repository resolves silently server side.

**Component tree.**

```
CommitsPage / GroupCommitsPage / RepoCommitsPage
  PageHeader crumbs title="Commits" (group: "Commits of {displayName}"; repo: "Commits of {title}") description="Default branch, newest first" meta=<FreshnessBadge commits.freshness/>
             (repo route: the layout above renders `RepoHeader variant="compact"` (no `h1`) and `RepoTabs`; this `PageHeader` is the page's only `h1` and passes no `crumbs`, V-27)
  Toolbar
    InputGroup[role=search] "Filter loaded commits"  (q, filters the rows already loaded; helper text says so)
    [/commits only] Select "Group" (All groups + groups from `commitGroupOptionsQuery`, same cache entry as the dashboard's), scope chip for ?repo= with remove button
  NewCommitsPill  Button variant=secondary "3 new commits - show"   (appears only when countNewer(head, firstLoaded) > 0; compared by `repo@sha`)
  StaleBanner
  for each day:  section aria-labelledby=day-{key}
                   h2#day-{key}  "Today" | "Yesterday" | "Tuesday 14 October 2026"
                   ol > li > CommitRow time="clock" showRepo={scope.repo === undefined}
  LoadMore (4.1.10)  "Load more (50)"   |  at cap: note "Showing the newest 1,000. Narrow with the filters."
```

`CommitRow` (shared): `AuthorAvatar` (aria-hidden), subject (`displayText`, first line, 200 graphemes, full text in `title`) as `ExternalLink` to `commit.url`, then a meta line: repo slug as `Link` to `/repos/$repo` (when `showRepo`), author name (`displayText`), `<time>` (clock `HH:mm` in the zone for the log, relative on panels), short sha (7) in `font-mono`. The row never shows the message body (a list shows subjects; a body can be arbitrarily long and belongs on the commit page).

**Wireframe, desktop.**

```
+------------------------------------------------------------------------------------------+
| Dashboard > Commits                                                                       |
| Commits                                                              (check) Up to date 1 m|
| Default branch, newest first                                                              |
| [ Filter loaded commits           ] [Group: All groups v]                                  |
|                                       [ 3 new commits - show ]                            |
| Today                                                                                     |
|  (KN) Fix rounding of VAT on credit notes                                                 |
|       billing-service   Kari Nordmann   08:12   3f2a9c1                                   |
|  (D)  Bump guava to 33.3.1-jre                                                            |
|       core-lib          Dependabot      07:55   8d1e5b7                                   |
| Yesterday                                                                                 |
|  (OB) Add retry to token refresh                                                          |
|       auth-client       Ole B.          16:40   a91c2de                                   |
|                                      [ Load more (50) ]                                   |
+------------------------------------------------------------------------------------------+
```

Phone: identical structure, one column; repo slug moves under the subject; time right-aligned; the toolbar stacks; the group `Select` is full width.

**Behaviour.**

```
rows        = pages.flatMap(items)                         # API order is time-descending and total; never re-sorted client side
visible     = rows.filter(r => matchesCommit(r, q))        # subject, author name, login, repo slug, sha prefix; case-insensitive
days        = groupByDay(visible, zone)                    # equal days are adjacent; in the SITE zone; DST days are 23/25 h long
label(day)  = dayLabel(day, now, zone)                     # today | yesterday | date; "yesterday" = previousDay(today) by date arithmetic
pill        = countNewer(head.items, rows[0])              # 0 | n | "many" (the loaded head fell out of the 50-row window); keyed `repo@sha` (GR-07, V-06)
click pill  -> invalidate commits queries, scroll to top, focus the first row, polite "3 new commits loaded"
follow      = window.scrollY <= FOLLOW_SCROLL_Y (200)      # one scroll listener that only sets state when the threshold is crossed (passive, rAF-throttled)
log         = useSuspenseInfiniteQuery(commitLogQuery(scope, 50, follow))   # V-13: enabled = follow
while the reader has scrolled (follow == false) the log query is DISABLED: unit 17's invalidateAll (snapshot change) and its 120 s sweep only mark it stale, no row is inserted above the reader (verified with TanStack Query 5.104.1: a disabled infinite observer keeps its data, is not refetched by invalidateQueries, and refetches once when `enabled` flips back to true);
the head query stays enabled and only feeds the pill; clicking the pill calls `log.refetch()` (works on a disabled observer), scrolls to the top and focuses the first row; scrolling back to the top re-enables the log and applies the pending refetch
filter + more pages: q is applied to loaded rows only; the helper text under the input says "Filters the commits loaded so far"; when q is set and the end is not reached, a "Load more" button remains and the count line says "12 matching of 100 loaded"
```

Golden day-grouping vectors (design 09 section 4.9, nine of them in `commits.test.ts (cc)`, all passing): `2026-03-29T23:30:00Z` in `Europe/Oslo` is `2026-03-30` (the 23-hour spring-forward day ends at 22:00Z); `2026-10-25T00:30:00Z` and `2026-10-25T01:30:00Z` are both `2026-10-25` and `2026-10-25T23:30:00Z` is `2026-10-26` (the 25-hour fall-back day); `2026-01-01T00:00:00Z` in `America/Los_Angeles` is `2025-12-31`; `2026-06-15T10:00:00Z` in `Pacific/Kiritimati` (UTC+14) and `2026-06-15T18:30:00Z` in `Asia/Kolkata` are `2026-06-16`; `2026-03-08T09:59:59Z` and `...T10:00:00Z` in `America/Los_Angeles` are both `2026-03-08`; an invalid zone throws `RangeError`. `previousDay("2026-03-01") = "2026-02-28"`, `previousDay("2024-03-01") = "2024-02-29"`, `previousDay("2026-01-01") = "2025-12-31"`; with `now = 2026-03-30T12:00Z` in `Europe/Oslo`, `2026-03-29` is "yesterday" although that day lasted 23 hours.

**States.** Empty (no commits at all): `EmptyState` "No commits yet" / during warm-up "The portal is still collecting commits"; empty by filter: "No loaded commit matches" + "Load more" if more exist; repo with no default-branch commits: "This repository has no commits on its default branch". Error mid-pagination: the loaded rows stay; the "Load more" button becomes "Retry loading more" with the request id. Stale banner as 4.1.5.

**Accessibility and keyboard.** Days are `section`s with `h2` so screen reader users jump by heading; rows are `li` in an `ol` (order matters); the sha link text is the 7-character sha with `aria-label="Commit 3f2a9c1 on GitHub"`; `<time datetime>` with full instant; the avatar is hidden; the new-commits pill is a button (not a live toast), its appearance announced politely once. Focus after "Load more" goes to the first new row. No keyboard trap, no hover-only information (the full subject is in `title` and in the link text when it is short; a truncated subject exposes the full text through `aria-label` on the link).

**Tests.**

| Id | Kind | Assertion |
|---|---|---|
| U18-COM-01 (cc) | unit | `groupByDay`/`dayLabel`/`previousDay` golden vectors above; empty list; one item; equal-day adjacency; no mutation |
| U18-COM-02 (cc) | unit | `matchesCommit` over subject/name/login/repo/sha; empty query keeps all; `countNewer` returns 0, n and `"many"`, compares `repo@sha` (a mirror with the same sha must not hide new commits; V-06) |
| U18-COM-03 (cc) | unit | `displayText` first line, cap 200 graphemes, bidi override removed, control characters removed |
| C18-COM-01 | component | `CommitRow` with `authorLogin: null`, `authorName: "Dependabot"` shows initials `D` and the name; with both null shows `?` and "Unknown author" |
| C18-COM-02 | component | log with 60 rows across 3 days renders 3 `h2`, 60 `li`; filtering to 2 rows removes empty days |
| C18-COM-03 | component | with `scrollY > 200` an `invalidateQueries` (the snapshot sync) and a fake 120 s sweep do not change the rendered rows (the log observer is disabled) while the head query updates the pill with the right count; at `scrollY == 0` the log refetches once; the pill click refetches and focuses the first row (`commitLogQuery(..., follow=false).enabled === false` is unit-tested in `commits.test.ts`) |
| C18-COM-04 | component | `invalid-cursor` on page 2 resets to page 1 with the toast |
| C18-COM-05 | component | two commits with the same sha in different repositories (fork or mirror) both render; list keys are `repo@sha` (GR-07) |
| E18-COM-01 | e2e | `/repos/payments-api/commits` against fakegithub: four rows in the order "Commit without author block" (2024-03-06), "Dependabot bump", "Add architecture notes", "Initial commit"; four day headings in the site zone; the body "Scaffold" of the first commit is not shown; initials `D` for `dependabot[bot]` and the null-login commit shows its name; every row link has `rel="noopener noreferrer"` |
| E18-COM-02 | e2e (stub) | 120 rows, "Load more" twice, focus lands on the first new row, the live region text is exact |
| E18-COM-03 | e2e | `/repos/<slug>/commits` and `/groups/<id>/commits` show the scope in title and breadcrumb; unknown slug shows not-found |
| E18-COM-05 | e2e | axe light/dark desktop/phone; keyboard: Tab through 10 rows then "Load more" reachable without a mouse |

---

### 4.5 Repository overview (`/repos/$repo`; N-01 digest)

**Purpose.** One repository: who it is, how healthy it is, what it is built with, its README and its latest commits. The route `/repos/$repo` is a **layout route** (`RepoLayout`, in the `docs` slice per unit 17's route table) that fetches the composite resource, renders the repository header and tabs and an `<Outlet/>`; the index child is the overview below, the other children are the commit log (4.4), documents (4.6) and dependencies (4.7).

**Data.**

| Call | Query | Role |
|---|---|---|
| `GET /api/v1/repos/{repo}` | `repoQuery(repo)` key `["repo", repo]` | primary (layout loader): `RepoSummary` plus `description`, `owners`, `links`, `system`, `apis`, `dependsOn`, `docs[]` (the whole documentation index), `descriptor`, `statuses[]`, `technology?`, `latestCommits[<=5]`, `sections{commits,docs,manifests,integrations}` freshness |
| `GET /api/v1/repos/{repo}/docs/readme` | `docQuery(repo, "readme")` | README panel (skipped when the index entry `readme` has `state: no_content`) |
| `GET /api/v1/site` | shell | `features.manifests` decides whether the Dependencies tab exists |

Errors: 404 -> `ProblemPage` resource "repository" (also for a repository that exists but is invisible to the caller: the API never distinguishes, 17.3 of the architecture). 308 -> canonical slug replace (4.1.6).

**Component tree.**

```
RepoLayout (features/docs/route.tsx)                                   loader: ensureQueryData(repoQuery(params.repo))
  RepoHeader variant="full" (overview) | "compact" (all other children)
    HEADING RULE (V-27): the `full` variant renders the page's only `h1` (the repository title). The `compact` variant renders the title as a `p` styled like
    the identity (a `Link` to the overview) and **never an `h1`**; every child route supplies exactly one `h1` of its own: documents = the document title (4.6),
    commits = "Commits of {title}" (4.4), dependencies = "Dependencies" (4.7). The first draft let both the layout and the child render an `h1`
    (two per page: axe `page-has-heading-one` is satisfied but the "exactly one h1" rule of 4.1.3 and the route announcer were not).
    crumbs [Dashboard] > [{groups[0].displayName}] > {title}
    h1 {title.value}   p.font-mono {slug}                         <- visually the identity, copy button (aria-label "Copy repository name")
    meta: Badge lifecycle (tone by value; label "Active" for production) | Badge type | Badge Private (Lock icon + text) | Badge Archived (Archive icon + text) | Badge Fork | FreshnessBadge worst(sections)
    p {summary.value | description}      Tooltip(provenance of summary/type/lifecycle, 3.6.1)
    actions: ExternalLink(htmlUrl) "Open on GitHub"   Button variant=outline <a href=readmeMd(repo)> "Raw README"
  RepoTabs  nav[aria-label=Repository] ul > li > Link[aria-current=page]:  Overview | Documentation (docs.length > 1) | Commits | Dependencies (features.manifests && (technology || sections.manifests))
            the Documentation tab links to `docsEntry(repo.docs)` = the first entry of `flattenDocs(buildDocTree(repo.docs))` whose `id != "readme"`, as `/repos/$repo/docs/$` with `_splat = docSplat(entry)`
            (V-14: the first draft named the tab but not its target; a bare `/repos/x/docs` is not an SPA shape, the server answers 404)
  Outlet
RepoOverview (features/docs/readme.tsx)
  StaleBanner freshness=[sections.commits, sections.docs, sections.manifests, sections.integrations]
  [archived]     Alert variant=default "Archived on GitHub: no new commits are expected."
  [descriptor.problems.length]  DescriptorAlert (Alert + Collapsible list: path, message, severity icon+text)
  div.grid.lg:grid-cols-[minmax(0,1fr)_20rem].gap-6
    section aria-labelledby=readme-h  h2#readme-h.sr-only "README"
       ReadmePanel: state switch (below) | DocContent(doc)  | "Rendered by GitHub" note when renderedBy=github | Collapsible "Rendering notes (n)" for doc.problems
    aside aria-label="About this repository"
       AboutSection (dl via Stat)      description, tags, license, default branch, last push, stars, open issues, latest release, groups
       StatusList [statuses.length]    one item per provider
       TechnologySummary [technology]  language bar, ecosystems, build tools, toolchain, counts, Link to /repos/$repo/dependencies
       RelationsSection                owners, links, system, apis, dependsOn
       BadgeSection                    BadgeStrip all badges
       section "Latest commits"        CommitRow time=relative showRepo=false x5 + Link "All commits"
```

**Wireframe, desktop.**

```
+------------------------------------------------------------------------------------------+
| Dashboard > Platform > Core Library                                                       |
| Core Library [Active] [library]                                    (check) Up to date 3 m |
| core-lib [copy]                                                                           |
| Shared building blocks for Acme services.        [Open on GitHub (new tab)] [Raw README]  |
| Overview | Documentation | Commits | Dependencies                                         |
|------------------------------------------------------------------------------------------|
| +--------------------------------------------------------+ About                           |
| | # Core Library                                         | Tags  [java] [shared]           |
| | Shared building blocks ...                             | License  Apache-2.0             |
| | ## Install                                             | Default branch  main            |
| | ```                                                    | Last push  3 min ago            |
| |   mvn install                              [Copy]      | Open issues  3                  |
| | ```                                                    | Latest release  v2.7.1          |
| | ...                                                    | Status                          |
| +--------------------------------------------------------+ (ok) Jenkins  Passing  build #412|
|                                                           (!) Snyk  2 high, 0 critical     |
|                                                          Technology                         |
|                                                           [=====Java 82%==][Kotlin 18%]    |
|                                                           maven, gradle  java 21            |
|                                                           3 units, 41 dependencies >        |
|                                                          Latest commits                     |
|                                                           (KN) Fix rounding ...  3 m        |
+------------------------------------------------------------------------------------------+
```

Phone: header stack; tabs become a horizontally scrollable `nav` (`overflow-x-auto`, focusable items, no clipped active item: `scrollIntoView({inline:"center"})` on mount); order is header, tabs, alerts, README, then each aside section as a `Collapsible` (open: Status and Latest commits; closed: About, Technology, Relations, Badges), so the README is not pushed below the fold.

```
+--------------------------------------+
| Dashboard > Platform > Core Library  |
| Core Library  [Active] [library]     |
| core-lib                             |
| Shared building blocks ...           |
| [Open on GitHub] [Raw README]        |
| < Overview | Docs | Commits | Deps > |
| # Core Library ...README...          |
| [v] Status                           |
|    (ok) Jenkins Passing              |
| [>] About                            |
| [>] Technology                       |
| [v] Latest commits                   |
+--------------------------------------+
```

**Behaviour.**

```
readmeEntry   = repo.docs.find(d => d.id == "readme")
ReadmePanel by readmeEntry.state / doc.html:
  resident          -> fetch docQuery(repo,"readme"); DocContent(doc)            ; renderedBy == "github" -> note "Rendered by GitHub" + (i) "AsciiDoc is rendered by GitHub"
  no_content        -> EmptyState(FileText, "This repository has no README", action ExternalLink(htmlUrl) "View on GitHub")   # GR-29, GR-37: never a blank 200
  too_large         -> EmptyState "The README is larger than the portal indexes" + ExternalLink(entry.sourceUrl)
  unsupported       -> EmptyState "This README format is not supported" + ExternalLink(entry.sourceUrl)
  not_resident      -> EmptyState "This document is not loaded into the portal's memory" + ExternalLink(entry.sourceUrl)   # budget; the GitHub link is the answer
  html == null while resident (race) -> skeleton, one retry, then the same as not_resident
tabs   Documentation shown when repo.docs.length > 1 (README alone is the overview); count in the label "Documentation (7)"
status text by ProviderState: ok "Passing", failing "Failing", unstable "Unstable", unknown "Unknown", disabled "Not configured"; the provider's own summary (<=300) follows; counts render as "critical 0, high 2, medium 5"
technology bar: grid-template-columns = languages[].share (min 2 % so a sliver stays visible), colour viz-1..7 then viz-8 for the rest, role="img" aria-label="Languages: Java 82 %, Kotlin 18 %", the same data as a visible dl (never colour only)
dependsOn item: resolved && slug -> Link to=/repos/$repo ; otherwise plain text + "(not tracked)"
owners: `Owner.kind` is `user | team | group | unresolved`: user -> "@handle", team and group -> "@org/handle" as given, plus the kind as a muted word; `unresolved` -> the handle as plain text with "(unresolved)" (V-24; the first draft covered `user` only). No GitHub profile link: links to profiles are external links through ExternalLink only when the API gave a URL; the contract gives handle, kind and source only
```

#### 4.5.1 Provenance tooltips

`SourcedString.source` and `SourcedRepoType.source` explain where a value came from; the tooltip on summary, type, lifecycle and tags reads: `descriptor` "Set in the repository's descriptor", `catalog-info` "From catalog-info.yaml", `codeowners` "From CODEOWNERS", `github-topics` "From GitHub topics", `github` "From GitHub", `config` "From the portal configuration", `derived` "Derived by the portal", `own` "Declared by the repository", `github-graph` "From the GitHub dependency graph", `sbom` "From an SBOM"; an unknown value (open enum) reads "Source: {value}". The tooltip is keyboard-reachable (the trigger is a focusable `(i)` button with `aria-label="Where does this come from?"`) and its text is duplicated in `aria-describedby`.

**States.** Loading: header skeleton + two-column body skeleton. README loading: `Skeleton` lines inside the panel only. README error: panel `ProblemAlert`. Whole composite error: `ProblemPage`. Stale: banner. Empty aside sections are omitted, not shown empty.

**Accessibility.** One `h1` (the title); the slug is a `p` (not a second heading); `RepoTabs` is a `nav` with `aria-current="page"` on the active link (not `role=tablist`: each tab is a different URL); the aside has `aria-label`; collapsible sections on phones are real disclosure buttons (`aria-expanded`); every badge has text (an icon alone never carries state); the language bar has a text alternative; `Tooltip` content is also reachable by focus.

**Tests.**

| Id | Kind | Assertion |
|---|---|---|
| C18-REPO-01 | component | `ReadmePanel` for each `DocState` renders the right `EmptyState` and a GitHub link; `no_content` never renders an empty region |
| C18-REPO-02 | component | `StatusList` maps every `ProviderState` to icon + text, counts text, `detailUrl` only when `https:` |
| C18-REPO-03 | component | `TechnologySummary` bar `aria-label` equals the visible list; a 0.4 % language still has a visible segment |
| C18-REPO-04 | component | `useCanonicalRepo`: data slug differs from the param -> `navigate({replace:true})` called once; equal -> not called |
| C18-REPO-05 | component | provenance tooltip text for every enum value plus an unknown one |
| C18-REPO-06 | component | a repository in two groups lists both group links in the aside (GR-08); breadcrumb uses the first; neither affects which data is shown |
| E18-REPO-01 | e2e | `/repos/payments-api`: header title, Open-on-GitHub link has `rel="noopener noreferrer"` and `target=_blank`, README headings present, tabs include Documentation (it has docs) and Dependencies (it has a `pom.xml`) |
| E18-REPO-02 | e2e | `/repos/docs-site` (AsciiDoc README): "Rendered by GitHub" note present, no console error, no CSP violation |
| E18-REPO-03 | e2e | unknown repository: `ProblemPage`, title "Repository not found", request id visible, focus on `h1` |
| E18-REPO-04 | e2e (stub) | API answers 308 to a new slug: the address bar converges to `/repos/<new>` without a reload |
| E18-REPO-05 | e2e (stub) | `sections.manifests = stale` with `lastErrorCode: parse.invalid_manifest`: banner names the file-parse cause; the page remains usable |
| E18-REPO-07 | e2e | phone: no horizontal scroll, README before the aside, disclosure keyboard operation, axe light/dark |

---

### 4.6 Documentation viewer (`/repos/$repo/docs/$`; GR-32, GR-37)

**Purpose.** Read any indexed document of a repository with navigation, an outline and working relative links. The server sanitises and rewrites; the SPA only places the HTML and handles clicks.

**URL contract (D-18-01).** The splat is the repository-relative path **without** its Markdown/AsciiDoc extension: `/repos/payments-api/docs/docs/overview` is `docs/overview.md`. A path with the extension is accepted and canonicalised by `replace` navigation (hand-written links, typed URLs, and `uiUrl` values from API fixtures that still carry `.md`). The extension is dropped because `GET /repos/{repo}/docs/{path...}.md` is the raw Markdown route (`doc.md` / `markdownUrl`, class `server` for the `.md` suffix) and one URL cannot be both. The README is the repository overview; `/repos/$repo/docs/README` replaces to `/repos/$repo`. `resolveDocId(splat, repo.docs)` maps a splat to an id through the index; not-in-index falls back to the path rule and lets the API answer.

**Data.**

| Call | Query | Role |
|---|---|---|
| composite `docs[]` | `repoQuery(repo)` (parent loader) | navigation tree, titles, order, states |
| `GET /api/v1/repos/{repo}/docs/{docId}` | `docQuery(repo, docId)` | the document (`html`, `toc`, `links`, `problems`, `renderedBy`, `wordCount`, `frontMatter`, `freshness`); `docId` is `docs~overview.md` for `docs/overview.md` |

**Component tree.**

```
DocPage (features/docs/doc.tsx)              loader: ensureQueryData(docQuery(repo, resolveDocId(splat, repo.docs)))
  RepoHeader variant="compact" + RepoTabs (from the layout)
  div.grid.xl:grid-cols-[16rem_minmax(0,1fr)_14rem].lg:grid-cols-[16rem_minmax(0,1fr)].gap-8
    DocNav  nav[aria-label=Documentation]  (lg: static column; below lg: Sheet opened by Button "Documents")
       Input type=search "Filter documents" (local, filterDocs)           p[aria-live=polite] "{n} documents"
       ul.tree: README ("Overview") link, then buildDocTree(entries):
           section node -> li > Button[aria-expanded][aria-controls] "Guides" > ul
           doc node     -> li > Link[aria-current=page when current]  (entry.state != resident -> muted + "(not loaded)" sr text)
    article#doc  (tabIndex -1 for focus after navigation)
       DocHeader: h1 {entry.title}  p {entry.description}  tags (Badge)  "Updated {RelativeTime updatedAt}  - {n} min read"  FreshnessBadge
                  actions: ExternalLink(entry.sourceUrl) "Edit on GitHub"   <a href=markdownUrl> "Raw Markdown"
       StaleBanner
       state switch: resident -> DocContent | other states -> EmptyState with ExternalLink(sourceUrl)
       Collapsible "Rendering notes (n)" for doc.problems (code + message)
       nav[aria-label="Document navigation"]: "Previous: {title}" / "Next: {title}" (flattenDocs order)
    DocToc  nav[aria-label="On this page"]  (xl: sticky aside; below xl: Collapsible at the top of the article)
       ul > li(depth 2|3) > a[href=#id]   scroll-spy marks aria-current="location"
```

**Wireframe, desktop (1280).**

```
+------------------------------------------------------------------------------------------+
| Dashboard > Platform > Billing Service      Overview | Documentation | Commits | Deps     |
+------------------------------------------------------------------------------------------+
| [Filter documents      ] | Installation                              | On this page         |
| Overview                 | Installing and configuring the service    |  Requirements        |
| v Guides                 | [setup]   Updated 2 weeks ago - 4 min     |  Install             |
|    Installation  <--     | [Edit on GitHub] [Raw Markdown]           |   From source        |
|    Configuration         | ---------------------------------------- |  Configure           |
|   v Advanced             | ## Requirements                           |                      |
|      Tuning              | Go 1.26 ...                               |                      |
| > Reference              | ```                          [Copy]       |                      |
|                          |   go install ./...                        |                      |
|                          | ```                                       |                      |
|                          | < Previous: Overview     Next: Configuration >                    |
+------------------------------------------------------------------------------------------+
```

Phone: `[Documents]` button opens the tree in a `Sheet` (left), the outline is a `Collapsible` "On this page" above the article, code blocks scroll horizontally inside their own focusable region, tables scroll inside a wrapper.

**Behaviour.**

```
DocContent(doc):   (cc: doc-content.tsx, links.ts)
  html placed ONLY by SafeHtml (Trusted Types policy "scp-html"); the SPA never parses Markdown, never rewrites HTML strings
  onClick (delegated on the wrapper):
     a = event.target.closest("a"); href = a.getAttribute("href")                  # the string exactly as the server wrote it
     decideClick(href, {button, ctrl, meta, shift, alt, target, download}, buildLinkIndex(doc.links)):
        modifier / non-primary button / target != _self / download / unknown href   -> native (browser decides; new tab etc.)
        kind == "anchor" and href starts with "#"                                    -> scroll: find element id, preventDefault, scrollIntoView, tabIndex=-1, focus({preventScroll}), history.replaceState(#id)
        kind == "doc" and href starts with "/" (not "//")                            -> router.navigate({href}) (no reload), preventDefault
        kind in {external, repo-file, media}                                         -> native
  effect on [html]: enhanceCodeBlocks(root, labels): every <pre> gets tabIndex=0 (scrollable region must be focusable), a "Copy" <button type=button aria-label="Copy code to clipboard"> that calls navigator.clipboard.writeText(code.textContent) and flips its text to "Copied" for 1.5 s (aria-live polite on a visually hidden span); a <pre><code class="language-mermaid"> gets data-label="Diagram source (Mermaid)" (v1; v1.1 swaps in the sandboxed iframe); cleanup removes the buttons
  tables: the SERVER already wraps every table in `div.md-table` (unit 10 deviation D-3, `safehtml` policy v2), so no wrapper is created here and `display: block` is never put on a `table` (it drops table semantics);
          a ResizeObserver on each `div.md-table` sets `tabindex="0"`, `role="region"` and `aria-label="Table, scrollable"` only while `scrollWidth > clientWidth` (V-22: the first draft said "wrapped by CSS", which cannot wrap)
  h1:     `normaliseDocH1(root, pageTitle)` (docs/headings.ts (cc)) hides a leading `h1` that repeats the page title and demotes every other `h1` to `role="heading" aria-level="2"` (V-27)
on navigation to a hash URL (#doc-install): after the article mounts, scroll to the element and focus it (layout effect); without a hash: focus the article h1 (RouteAnnouncer announces the title)
scroll-spy: IntersectionObserver over headings (rootMargin "-80px 0px -70% 0px"); the topmost intersecting heading's TOC link gets aria-current="location"; nothing is announced
reading time: ceil(wordCount / 200) minutes, shown as "{n} min read" (omitted below 1)
prev/next: flat = flattenDocs(buildDocTree(entries)); readme first; neighbours of the current id; absent at the ends
index order: `repo.docs` arrives in index order (README first; explicit `docs.nav`, then front-matter `order`, then path; unit 10 `listRepoDocs`), `buildDocTree` keeps that order and does **not** re-sort (the first draft's test table said "orders by `order` then title"; the compiled function never did, and the contract does not promise an order, so OQ-18-02 stays open and the README is moved first defensively)
empty splat (`/repos/x/docs/`): `navigate({ to: Documentation entry, replace: true })`; no Documentation entry (only the README): replace to `/repos/$repo`
unresolvable splat (`resolveDocId` null, 3.1): `ProblemPage` kind not-found, resource "document"; no request
nav filter: filterDocs(entries, q) matches title, description, path and tags (case-insensitive); while filtering, the tree is shown flat with full section paths; Esc clears
```

Heading ids arrive prefixed `doc-` from the server (collision-free with the page's own ids). Syntax highlighting is server-side chroma classes with theme-aware CSS variables (`chroma-light.css`, `chroma-dark.css`, unit 17); no highlighter ships to the browser (GR-37).

**States.** Loading: nav from cache (the composite is already loaded) + article skeleton. Document 404: `ProblemPage` resource "document" with link "Back to documentation" (`/repos/$repo`). `state != resident`: `EmptyState` with the reason and the GitHub link; the nav still renders. `html: null` with `resident` (race): skeleton then the same fallback. `renderedBy: github`: note. Offline/stale: banner.

**Accessibility and keyboard.** The tree follows the **disclosure navigation** pattern (buttons with `aria-expanded`, nested lists, links; not `role=tree`, which would require arrow-key semantics and hides links from link lists); `aria-current="page"` on the current link; the filter result count is polite. TOC: `aria-current="location"` while scrolling. Code blocks and scrollable tables are keyboard reachable. Heading hierarchy inside the article continues below the page `h1`: the HTML of a document normally starts with its own `# Title`, so `normaliseDocH1` hides a leading `h1` whose text equals the entry title and demotes every other `h1` to level 2 (CSS cannot compare text with the title, which is what the first draft asked for; V-27). Heading ids may be non-ASCII (unit 10 deviation D-2: `doc-` plus letters, digits, marks, `_:.%-`), so in-page anchors are looked up decoded and as written (`decideClick` returns both; a malformed escape such as `#100%` must not throw, V-01). Links in prose are underlined (not colour alone). Focus is never trapped in the `Sheet` (it returns to the trigger on close). Targets are >= 24 px (copy button 28 px).

**Tests.**

| Id | Kind | Assertion |
|---|---|---|
| U18-DOC-01 (cc) | unit | `docIdFromSplat`/`splatFromDocId` round trip; rejects `..`, `.`, empty segments, control characters, lone surrogates, ids over 1,024; **encodes by the contract's `DocId` rule** (`docs/Release Notes.md` -> `docs~Release%20Notes.md`, `docs/Økonomi.md` -> `docs~%C3%98konomi.md`, `a~b` -> `a%7Eb`, `100%.md` -> `100%25.md`) and decodes it back (V-04); `docSplat`/`stripDocExt` handle `.md`, `.markdown`, `.adoc`, `.asciidoc`, upper case; `resolveDocId` prefers the index (path and extension-less both resolve) and falls back to the path rule |
| U18-DOC-02 (cc) | unit | `decideClick` table: modifier keys, middle button, `target=_blank`, `download`, unknown href -> native; `#x` anchor -> scroll with decoded id and the raw id; **`#100%` (malformed escape) does not throw** (V-01); `/repos/a/docs/b` doc -> navigate; `//evil.example` -> native; external/media/repo-file -> native |
| U18-DOC-03 (cc) | unit | `buildDocTree` keeps the index order (it does not re-sort), nests `section[]`, moves the README first; `flattenDocs` order; `docsEntry` (target of the Documentation tab); `sectionsContaining`; `filterDocs` |
| U18-DOC-04 (cc) | unit (jsdom) | `normaliseDocH1`: a leading `h1` equal to the page title is hidden, any other `h1` becomes `role=heading aria-level=2`, idempotent (V-27) |
| C18-DOC-01 | component | `DocContent` with fixture HTML: clicking an internal doc link calls `router.navigate({href})` and `preventDefault`; ctrl-click does not; anchor click scrolls and focuses the target; external link untouched |
| C18-DOC-02 | component | every `pre` is focusable and has one Copy button; clicking writes the code text to a mocked clipboard; re-render with new html leaves no stale buttons; **a parent re-render with the same html keeps the same DOM node and the Copy button** (`shared/safe-html.test.tsx`, jsdom with a fake Trusted Types policy returning a new object per call; fails on the first draft's `SafeHtml`, V-02) |
| C18-DOC-03 | component | `SafeHtml` is the only element using `dangerouslySetInnerHTML` (ESLint rule has its own fixture test); `html: null` renders nothing |
| C18-DOC-04 | component | `DocNav` disclosure: Enter/Space toggles `aria-expanded`, the current document's ancestors start open, filter shows matches only |
| E18-DOC-01 | e2e | payments-api: Documentation tab -> `docs/overview`; URL is `/repos/payments-api/docs/docs/overview`; `.md` URL variant replaces to it; previous/next work |
| E18-DOC-02 | e2e | a heading anchor in the README scrolls and focuses; the hash survives reload |
| E18-DOC-03 | e2e | `/repos/payments-api/readme.md` is served raw by the server (content type `text/markdown`), not by the SPA |
| E18-DOC-04 | e2e | unknown document: `ProblemPage` "Document not found" with back link; unresolved-but-not-resident entry shows the GitHub fallback |
| E18-DOC-05 | e2e | CSP/console gate green on a document with code, table and image; the image URL starts with `/media/`; no request leaves the origin |
| E18-DOC-06 | e2e | phone: Documents sheet opens, traps and restores focus; no horizontal page scroll with a wide table and code block; axe light/dark |

---

### 4.7 Repository dependencies (`/repos/$repo/dependencies`; N-01)

**Purpose.** What one repository is built from: manifest units, the declared (and optionally locked) dependencies with scopes and versions, internal links to other tracked repositories, parser diagnostics, and downloads (CycloneDX SBOM, `dependencies.md`). Child of `RepoLayout`, so the compact repository header and the tabs are above; **the page heading is the page's `h1` "Dependencies"** (the compact header has no `h1`, 4.5 heading rule; V-27).

**Data.**

| Call | Query | Role |
|---|---|---|
| `GET /api/v1/repos/{repo}/manifests` | `repoManifestsQuery(repo)` | primary (loader): `treeSha`, `commitSha`, `parsedAt`, `truncated`, `languages`, `ecosystems`, `toolchain`, `units[]` (`id`, `dir`, `ecosystem`, `role`, `provides[]`, `toolchain`, `files[]`, `counts{direct,internal,locked}`, `diagnostics[]`), `counts`, `diagnostics[]`, `freshness` |
| `GET /api/v1/repos/{repo}/dependencies?scope&relationship&internal&ecosystem&unit&q&sort&limit=100&cursor` | `repoDepsQuery(repo, search)` (infinite, `keepPreviousData`) | the table; `cursor` is valid only for the sort it was issued with, so any filter or sort change resets to page 1 (the query key contains the whole search) |

`404 feature-disabled` (manifests off) -> `ProblemPage` kind disabled; the tab is not shown when `site.features.manifests` is false. `404 not-found` with manifests on and no facts yet -> the empty state below.

**Component tree.**

```
RepoDependenciesPage (features/manifests/repo-dependencies.tsx)
  h1 "Dependencies"   FreshnessBadge(manifests.freshness)   p "Parsed {RelativeTime parsedAt} from commit {commitSha7}"
  StaleBanner
  [manifests.truncated]  Alert "Some manifest files were skipped (repository limits); the list may be incomplete."
  SummaryRow:  Stat units | Stat direct dependencies | Stat internal | Stat diagnostics (Badge warning if > 0) | ecosystems (Badge list) | toolchain (dl: java 21, go 1.27) | language bar (shared with 4.5)
  UnitsPanel  section "Manifest units" > Collapsible per unit (default closed when > 3 units):
       trigger: {unit.id} mono, ecosystem Badge, role Badge, "{direct} direct, {internal} internal"
       content: dir, provides[] (purl, version), files[] (path as ExternalLink to `{htmlUrl}/blob/{commitSha}/{path}` with every path segment passed through `encodeURIComponent`, built only when `commitSha` is non-null; `htmlUrl` comes from the layout loader data, 2.1; V-24), toolchain dl, unit diagnostics, Button "Only this unit" (sets ?unit=)
  Toolbar [role=search]:  Input q | Popover Scope (Checkbox x8) | ToggleGroup relationship [Direct|Transitive|All] | NativeSelect Origin [All|Internal|External] | Popover Ecosystem (Checkbox) | Select Unit | Button "Clear"
  DataTable caption="Dependencies of {repo}" columns [Package, Requirement, Resolved, Scope, Ecosystem, Source]
  LoadMore (100)  cap 2,000 rows -> note
  DiagnosticsPanel section "Parser diagnostics (n)" > ul > li  severity icon+text, code, message, loc (path:line)
  DownloadsRow: <a download href=serverRoutes.sbom(repo)> "SBOM (CycloneDX JSON)"   <a href=serverRoutes.dependenciesMd(repo)> "dependencies.md"
```

**Wireframe, desktop.**

```
+------------------------------------------------------------------------------------------+
| (repository header and tabs: Overview | Documentation | Commits | [Dependencies])         |
| Dependencies                                                  (!) Delayed  parsed 2 h ago |
| 3 units   41 direct   6 internal   (!) 2 diagnostics    [maven] [npm]   java 21           |
| v Manifest units (3)                                                                      |
|   > billing-service#.        [maven] [application]   18 direct, 3 internal                |
|   > billing-service#web      [npm]   [library]       12 direct, 1 internal                |
| [ Filter dependencies   ] [Scope v] [Direct|Transitive|All] [Origin: All v] [Ecosystem v]  |
| +----------------------------------------------------------------------------------------+|
| | Package v               | Requirement | Resolved      | Scope   | Ecosystem| Source     ||
| | org.acme.platform:core-lib [Internal]  core-lib >     |         |          |            ||
| |                         | ${core.version} | 2.7.1 pinned | runtime | maven    | pom.xml:42 ||
| | com.google.guava:guava  | 33.3.1-jre  | 33.3.1-jre locked | runtime | maven    | pom.xml:57 ||
| +----------------------------------------------------------------------------------------+|
| [ Load more (100) ]                                                                       |
| Parser diagnostics (2): (!) warning  pom.xml:12  property ${x} could not be resolved       |
| [SBOM (CycloneDX JSON)] [dependencies.md]                                                 |
+------------------------------------------------------------------------------------------+
```

Phone: summary stats in a two-column grid; units panel and diagnostics are collapsible; the table becomes cards (`Package` as title, then `dl` rows Requirement / Resolved / Scope / Ecosystem / Source); the toolbar collapses into `Filters (n)` opening a `Sheet`.

**Behaviour.**

```
package cell:  namespace (muted, mono) + ":" + displayName ?? name  as Link to=/packages search={{ purl }}   (the purl is always a search value)
               internal != null -> Badge "Internal" + " -> " Link to=/repos/$repo({internal.repo}) text "{internal.repo} {internal.version}"
requirement:   mono, as given ("${core.version}", "^1.2", ">=2,<3"); empty -> "-"
resolved:      version mono + Badge resolution: declared "Requirement only" (muted), pinned "Pinned", locked "Locked", managed "Managed" ; version null -> "-"
scope:         Badge outline (runtime, dev, test, build, optional, peer, provided, unknown); `optional: true` adds "optional"; `condition` shown as a tooltip
source:        loc.path:loc.line as text (mono) ; ExternalLink to blob@commit when htmlUrl is known; confidence == "heuristic" -> Badge "Heuristic" + tooltip "Read by a lexical parser (Gradle script, Package.swift, ...); may be incomplete"
defaults:      relationship=direct, sort=ecosystem (server default order ecosystem, name, unit); sort header tokens name|ecosystem|scope with sign
relationship: `Dependency.relationship` is `direct | transitive | unknown`; "transitive" shows Badge "Transitive", "unknown" shows Badge "Unknown relationship" with the tooltip "The manifest does not say whether this is direct" (V-24: the first draft handled the first two only); `direct` shows no badge
cap:           2,000 rows rendered (20 pages of 100): then the note "Showing 2,000 of many. Narrow with the filters."
```

**States.** Loading: summary and table skeletons. Empty (no manifests found): `EmptyState` icon `PackageSearch`, "No package manifests were found in this repository", description "The portal reads pom.xml, package.json, go.mod, Cargo.toml and other manifests listed in the documentation of the manifests feature.", action `ExternalLink` GitHub. Empty by filter: "No dependency matches" + Clear. Error: manifests -> `ProblemPage`; table -> panel alert. Stale: banner with `parse.` cause text. A repository with diagnostics but no dependencies shows the diagnostics prominently (not an empty state).

**Accessibility.** Table caption, sortable headers with `aria-sort`; toggle groups have an accessible name ("Relationship"); each `Popover` filter lists checkboxes in a `fieldset` with a legend; badges carry text; the purl link text is the human name, the purl itself is in the `title` and in a visually hidden span so screen readers can read the exact identifier.

**Tests.** C18-DEP-01 component: row rendering for internal, transitive, heuristic and locked variants from fixtures; C18-DEP-02: changing `relationship` resets the cursor (new query key) and keeps previous rows dimmed; U18-DEP-01 (cc): `repoDepsSearch` defaults and `.catch` fallbacks for garbage; E18-DEP-01 e2e `/repos/payments-api/dependencies`: units list contains the `pom.xml` unit, a Maven coordinate links to `/packages?purl=pkg%3Amaven%2F...` with the purl percent-encoded in the query; E18-DEP-02 (stub): `feature-disabled` -> disabled page and no tab; E18-DEP-03: SBOM link has `download`; E18-DEP-04: axe light/dark desktop/phone.

---

### 4.8 Organisation dependencies (`/dependencies`; N-01)

**Purpose.** One page, four views of the same question ("how do our tracked repositories depend on each other and on shared components?"), each shareable by URL. It lists **internal** dependencies (edges between tracked units); external component statistics are in Versions and Licences. Nothing on this page needs a second round trip per node.

**Chrome.** `PageHeader` title "Dependencies", description "Internal dependencies between tracked repositories", meta `FreshnessBadge`. A shared **filter bar** (group `Select` with "All groups", ecosystem multi-select, scope multi-select, "Include edges inside one repository" `Switch` for graph and table) above a `Tabs` control bound to `?view=`:

```
Tabs value=view onValueChange -> navigate({search: prev => ({...prev, view}), replace: false})
  TabsList aria-label="View": Graph | Table | Versions | Licences
  TabsContent: <GraphView/> | <UnitsTable/> | <VersionsView/> | <LicensesView/>   (only the active one mounts; views are lazy chunks, graph is the heaviest)
```

(`Tabs` rather than route tabs because the four views are panels of one page and one filter state; arrow keys move between tabs, Tab enters the panel.)

| View | Data | Section |
|---|---|---|
| Graph | `graphQuery(search)` -> `GET /api/v1/dependencies/graph?group&scope&ecosystem&intra&internal=true&focus&depth&format=json` | 4.9 |
| Table | the same `DependencyGraph` (nodes and edges are already in memory) | 4.8.1 |
| Versions | `versionsQuery(search)` -> `GET /api/v1/dependencies/versions?group&ecosystem&internal&q&minVersions&limit=50&cursor` | 4.8.2 |
| Licences | `licensesQuery(group)` -> `GET /api/v1/dependencies/licenses?group` | 4.8.3 |

`GET .../graph` can answer `text/plain` for `format=mermaid`; the query always asks `format=json` and rejects a string body (`graphQuery (cc)` throws `TypeError`, becomes `client-contract`).

#### 4.8.1 Table view (the complete accessible alternative to the canvas)

Same facts as the canvas, as text: one row per unit, **sorted in memory** by `sortNodes(nodes, token)` (`-inDegree` default: most depended-upon first; `UNIT_SORTS (cc)`), progressive rendering in steps of 100.

```
columns: Unit (label, mono id below; Button "Focus" sets ?focus=<id>&depth=1&view=graph) | Repository (Link to=/repos/$repo) | Ecosystem | Used by (inDegree) | Depends on (outDegree) | Cycle (Badge "In a cycle" when inCycle)
row expansion (Collapsible in the first cell): two lists
   "Depends on"  one li per outgoing edge: target label (Link to /repos/$repo), component purl (Link /packages), requirement -> resolved version, VersionStatus Badge (current "Up to date", behind "Behind", ahead "Ahead", unknown "Unknown") 
   "Used by"     one li per incoming edge: same fields
cycles panel above the table when summary.cycles > 0: "2 dependency cycles" > list of cycles as "a -> b -> a" with Buttons that filter the table to the members
```

#### 4.8.2 Versions view

```
toolbar: Input q (component name, server side) | origin NativeSelect [All|Internal|External] | Switch "Only components used in 2 or more versions" (minVersions 2) | ecosystem (shared bar)
DataTable caption "Component versions in use" columns:
  Component (displayName; Link to=/packages search={{purl}}; ecosystem Badge; Badge "Internal" when internal)
  Used by (users units, repos repositories)
  Versions (distinctVersions)   Latest known (latestKnown or "-")
  Spread: a bar of at most 6 segments from spreadSegments(versions, keep=5) (the five most used versions plus "other"), role="img", aria-label "2.7.1 in 9 units, 2.5.0 in 3 units, 2 other versions in 1 unit"; the legend below the bar is visible text for the top two versions
order: server order (number of using units, then purl); no client sort; "Load more (50)"
```

`spreadSegments` assigns each segment a `slot` 1..5 mapped to `--viz-1..5` plus a pattern-free but text-labelled legend (the bar is never the only carrier of the numbers).

#### 4.8.3 Licences view

Licences are **declared-only** (traceability m4). `LicenseSummary`: `buckets[] {license, components, dependencies, repos}` and `coverage {declared, total, ratio}`.

```
Alert info (always)  "Licences come from package manifests and lockfiles. Components that do not declare one are listed as 'Not declared in the manifest'; that is a data gap, not a finding."
Coverage:  Progress value=ratio   text "{declared} of {total} dependencies declare a licence ({pct} %)"
DataTable caption "Licences in use": License (SPDX id or the bucket label; "unknown" bucket labelled "Not declared in the manifest", muted, last) | Components | Dependencies | Repositories | share bar (CSS, text value next to it)
```

No chart library: a CSS bar per row, with the number printed.

**States for all views.** Loading skeleton per view; empty graph ("No internal dependencies between tracked repositories yet": the commonest cold-start state; action "Open the table of all units" when nodes exist but no edges); `truncated` Alert ("Showing the 2,000 most connected units. Filter by group or ecosystem to see the rest."); stale banner; `feature-disabled` -> disabled page; error per panel.

**URL state.** `view`, `group`, `focus`, `depth`, `ecosystem`, `scope`, `intra`, `q`, `origin`, `minVersions` (3.1). Changing the view keeps shared filters (`group`, `ecosystem`, `scope`) and clears view-specific ones only when they do not apply (`focus`/`depth`/`intra` only in graph and table; `q`/`origin`/`minVersions` only in versions).

**Tests.** U18-ORG-01 (cc): `orgDepsSearch` defaults, bad enum falls back, `depth: 7` falls back to 1, `minVersions: 0` falls back; U18-ORG-02 (cc): `sortNodes` totality; U18-ORG-03 (cc): `spreadSegments` sums to 1 and merges the tail; C18-ORG-01: `Tabs` arrow-key behaviour changes `?view=`; the table view renders every node of the synthetic 1,000-node graph progressively (100, then 100 more); C18-ORG-02: licences unknown bucket text and coverage; C18-ORG-03: versions bar `aria-label` equals the legend data. E18-ORG-01 (stub, fixture graph of 3 nodes with a cycle): all four views reachable by keyboard, cycles panel lists `core-lib#. -> auth-client#.`; E18-ORG-02: reload preserves the view and filters; E18-ORG-03: axe on every view.

---

### 4.9 The dependency graph (library choice, 1,000-node plan, accessibility)

#### 4.9.1 Decision

| Concern | Choice | Evidence |
|---|---|---|
| Renderer | **sigma 3.0.3** (WebGL) over **graphology 0.26.0**, own React wrapper (no `@react-sigma/core`) | research 08 E6 (1,000 nodes, 2,500 edges, Chrome 155, 6x CPU throttle, medians of 5): sigma 3 36 KB gz, mount 79 ms, 8.2 ms frames; `@xyflow/react` +55 KB, 1,006 ms mount; sigma 4.0.1 104 KB, 237 ms mount, breaking rewrite (changelog read 2026-10-14). The React wrapper is 52 lines, compile-checked. |
| Layout | **d3-force 3.0.0**, seeded and deterministic, in a **module Web Worker** | E7: 300 ticks for 1,000/2,500 = 0.61 s; dagre does not finish at 1,000 nodes in 90 s; this unit's run: 931 ms cold, 181 ms warm (Node 26.10.0) |
| Alternative on file | cytoscape 3.34.3 | only if WebGL proves unusable in the owner's browser matrix; switching changes `GraphController` and nothing else |
| Accessibility | the canvas is an enhancement; the **Table view** (4.8.1) and an **Outline** carry every fact | architecture 12.3 |

#### 4.9.2 Data flow

```
graphQuery(search) --(JSON, <= 2,000 nodes / 20,000 edges, truncated flag)--> DependencyGraph
   renderability(g, hasWebGL(document)):  empty | no-webgl | too-many-nodes | too-many-edges -> no canvas, show reason + Table view   (GRAPH_LIMITS = 2000 nodes, 20000 edges)
   ok -> LayoutInput { ids, edges as dense index pairs, sizes = nodeSize(inDegree), previous? } --postMessage--> layout.worker.ts (module worker)
         worker: layout(input) -> Float32Array (x,y interleaved) --transfer--> main
   GraphController.setData(g, positions)   (graphology Graph rebuilt, parallel edges collapsed, sigma.setGraph)
   user input -> controller state (hover/selected) -> reducers -> sigma.refresh({skipIndexation: true, schedule: true})
```

Requests carry a `key` (hash of node ids and edge count); a response whose key is not the latest is dropped (a fast filter change never paints an old layout). The worker is created **per layout run** (`new Worker(url, { type: "module" })` inside the data effect; it is a same-origin module script, allowed by `script-src 'self'` and `worker-src` falling back to `script-src`; the CSP golden test must cover it, T-18-005) and `terminate()`d after its response, when a newer run supersedes it, and on unmount (V-18: the first draft said "created once per mounted canvas" here and "terminated after each response" in section 8; the compiled component does the latter, and starting a module worker costs a few milliseconds). Three failures end in the Outline instead of an endless "Arranging N units..." (V-10): the worker answers `{ type: "error" }` (layout threw), the worker cannot start (`error` event: CSP, network, a 404 of the hashed chunk after a deploy), or `new Worker(...)` throws synchronously (Trusted Types policy refusal); each calls `onStatus("unavailable")`.

#### 4.9.3 Layout parameters (exact, `layout.ts (cc)`)

```
seed:      position(id) = (cos a, sin a) * r   with a = fnv1a(id)/2^32 * 2pi, r = sqrt(fnv1a(id + "#r")/2^32) * spread,  spread = sqrt(n) * 18      # same id, same start, on every machine
random:    lcg(7) as d3 randomSource (jiggle of coincident nodes)                                                                                  # reproducible
forces:    link distance 28 strength 0.35 | manyBody strength -28 theta 0.9 distanceMax 400 | center (0,0) | collide radius = nodeSize + 1.5 (1 iteration)
cold:      alpha 1,   300 ticks;   warm (>= 90 % of nodes have a previous position): alpha 0.3, 60 ticks
cooling:   alphaDecay = 1 - (alphaMin/alpha0)^(1/ticks), alphaMin 0.001  -> the simulation cools exactly at `ticks`, so run time is a function of `ticks` alone
output:    Float32Array(2n); the same input yields byte-identical output (tested)
```

Positions of earlier layouts are kept in memory as **one `Map<id,[x,y]>` per mounted canvas** (a `useRef`, merged across data changes and dropped on unmount; bounded by the number of distinct unit ids of the organisation, at most 2,000 per payload), so changing a filter (a subset or superset of nodes) warm-starts and the picture barely moves (mean displacement 4.4 units in the test). The first draft described an LRU of three layouts keyed by graph key; the compiled component never did that and a single merged map is simpler and warm-starts more often (V-18).

#### 4.9.4 Performance plan for 1,000 nodes (budgets, techniques, proof)

| Budget | Value | Measured / source | Enforced by |
|---|---|---|---|
| Graph route chunk (sigma + graphology + controller + reducers; the 52-line React wrapper adds about 1 KB) | <= 70 KB gz | **39.8 KB gz** (34.7 KB brotli), esbuild 0.25 minified, 2026-10-14 | `size-limit` entry `graph` (unit 17's budget script) |
| Layout worker chunk | <= 15 KB gz | **6.0 KB gz** | `size-limit` |
| Cold layout, 1,000 nodes / 2,500 edges | <= 1.5 s wall on a CI laptop, never on the main thread | 785-817 ms in a Chromium worker, 931 ms in Node 26.10.0, 610 ms (research machine) | Playwright `scp.graph.layout` measure |
| Warm layout | <= 0.3 s | 181 ms | same |
| Mount (setData + first two frames) | on a **real GPU**: unthrottled <= 100 ms, 6x CPU throttle <= 150 ms (product expectation, informational); **gating on CI software WebGL: <= 600 ms unthrottled, <= 1 s at 6x** (V-21: re-run in full Chrome 155 with SwiftShader on a loaded machine, mount was 264-299 ms in three of four runs (once 843 ms), so a 100 ms gate on software GL is flaky; the layout-phase budget and "no long task during layout" were reproduced: 792-844 ms, none) | **29-31 ms unthrottled, 418-466 ms at 6x with SwiftShader** (this unit, Chromium 153); 79 ms at 6x on a real GPU (research E6) | Playwright with CDP `Emulation.setCPUThrottlingRate(6)` |
| Frame time while panning | <= 16 ms | 8.2 ms median (E6) | not asserted frame by frame; asserted by absence of long tasks (below) |
| Main-thread long tasks during layout | none (measured: none at 1x, none during the layout phase at 6x; the only long tasks, 123-140 ms, are the first frame after mount on software GL) | by construction (worker) | Playwright `PerformanceObserver({type:"longtask"})` |
| Hover highlight | one frame | reducers are O(degree) over a Set computed once per selection | component test on `focusSet`/`nodeView` complexity |
| Time to first interactive on `/dependencies?view=graph`, 3G-fast profile | skeleton at < 1 s, canvas at < 4 s | n/a | e2e smoke with throttling (informational in v1) |

Techniques (all in `sigma-controller.ts (cc)`): `hideEdgesOnMove` and `hideLabelsOnMove` (a pan or zoom of 1,000 nodes redraws nodes only); `renderEdgeLabels: false`; `enableEdgeEvents: false` (edge hit-testing is the expensive part of event handling and edges are not interactive); `labelRenderedSizeThreshold: 9` and `labelDensity: 0.6` with a 120 px grid so only the large (much depended-upon) nodes carry labels; edge type `arrow` only up to `GRAPH_LIMITS.arrowEdges = 3000` edges, `line` beyond; **parallel edges collapsed** to one line per unit pair in `setData` (the table lists every edge); `zIndex: true` only to put highlighted nodes on top; reducers are pure functions of `(node id, HighlightState, neighbour Set)`; `refresh({ skipIndexation: true, schedule: true })` on hover so the spatial index is not rebuilt; the controller is created once per mount and `setGraph` replaces the data (no Sigma re-instantiation on filter changes); `kill()` on unmount (also releases the WebGL context) and a `ResizeObserver` calls `resize()`.

Server and client caps keep the worst case bounded: the API keeps the 2,000 highest-degree units; above `GRAPH_LIMITS` the canvas is not offered at all.

#### 4.9.5 Appearance

| Element | Rule |
|---|---|
| Node colour | ecosystem: `ecosystemOrder(nodes)` sorts ecosystems by node count (ties by id); the first 7 take `--viz-1..7`, everything else shares `--viz-8` ("other"); colours are read once from computed style per theme change (`readPalette`) because WebGL needs concrete colours, not `var()` |
| Node size | `nodeSize(inDegree)`: 3 + min(11, log2(1 + inDegree) * 2.2), rounded: from 3 (no dependants) to 14 (many); dependants matter most (D-15) |
| Edges | grey (`--border` darkened for contrast against `--background`), 1 px, arrows <= 3000 edges; highlighted edges `--primary` |
| Hover / selection | the node and its **in and out neighbours** keep colour and get labels; everything else dims to `--muted-foreground` at reduced size, unrelated edges hide; selection beats hover; `Esc` or a click on the background clears |
| Cycles | not drawn as a ring in v1 (a border program would add a dependency); `inCycle` is shown in the node panel, the Table view and the cycles list, and clicking a cycle selects its first member and lists the others |
| Legend | `Legend` component: ecosystem swatch + name (up to 7 plus "Other"), "Larger node = more dependants", "Arrow = depends on" |
| Motion | camera animations 200-300 ms, `0` under `prefers-reduced-motion` |

#### 4.9.6 `GraphController` (compile-checked) and the React wrapper

```ts
interface Palette { viz: readonly string[] /* 8 */; dim: string; edge: string; edgeHi: string; label: string }
interface GraphControllerOptions { palette: Palette; reducedMotion: boolean; onSelect(id: string | null): void; onHover(id: string | null): void }
class GraphController {
  constructor(container: HTMLElement, opts: GraphControllerOptions)
  setData(g: DependencyGraph, positions: Float32Array): void
  setPalette(p: Palette): void
  select(id: string | null): void;  getSelected(): string | null
  zoomIn(): Promise<void>; zoomOut(): Promise<void>; reset(): Promise<void>
  pan(dx: number, dy: number): void            // fraction of the visible area; buttons and arrow keys (no dragging required, WCAG 2.5.7)
  focusOn(id: string): Promise<void>           // centres and zooms to ratio 0.4
  resize(): void; kill(): void
}
function hasWebGL(doc: Document): boolean
interface DependencyGraphProps { graph: DependencyGraph; selected: string | null; onSelect(id: string | null): void; onHover?(id: string | null): void; ref?: Ref<GraphHandle> }  // see dependency-graph.tsx (cc)
```

The wrapper mounts the controller in an effect keyed on the container only (React 19 StrictMode runs effects twice in development: create, `kill`, create again; the cleanup is `kill()` and the worker `terminate()`), pushes data through a second effect, and never touches DOM nodes React owns. `onStatus` and `onSelect` must be referentially stable (`useCallback` or compiler-memoised): they are effect dependencies, and an unstable `onStatus` would restart the layout on every parent render.

#### 4.9.7 Graph view layout

```
GraphView
  Toolbar:  Combobox "Find a unit" (label search over nodes; selecting centres the node)  |  Select "Focus on" (clears focus when empty) + ToggleGroup depth [1 2 3] (enabled with focus)  |  zoom + / - / reset / pan arrows (Button group)  |  ToggleGroup mode [Canvas | Outline]
  StaleBanner, [truncated] Alert, [renderability reason] Alert + switch to Table
  div.grid.lg:grid-cols-[minmax(0,1fr)_20rem]
     Canvas region: div role="group" aria-label="Dependency graph with {n} units and {m} dependencies. Use the unit finder or the Table view to explore without the canvas." tabIndex=0  h-[32rem] (min-h 24rem; phone 60vh)
                    overlays: layout progress (role=status "Arranging {n} units...", Spinner), Legend (bottom-left)
     NodePanel (aside; Sheet on phone): selected node, or "Select a unit to see its dependencies" + cycles list + summary stats (units, edges, cycles, ecosystems)
       selected: h3 label; Link to=/repos/$repo; dir mono; Badge ecosystem; artifacts (purls as Links to /packages); "Used by {inDegree}" "Depends on {outDegree}"; Badge "In a cycle"
                 Buttons: "Focus on this unit" (sets ?focus=&depth=1), "Open repository"
                 two lists (max 10 each, "show all in Table"): Depends on / Used by with VersionStatus Badge; each item is a Button that selects that unit and moves focus to the panel heading
  Outline mode: replaces the canvas by OutlineTree (4.9.8)
```

Desktop wireframe:

```
+------------------------------------------------------------------------------------------+
| Dependencies   [Graph][Table][Versions][Licences]       group [All v] ecosystem [v] scope [v]|
| [Find a unit v]  Focus [none v] depth [1][2][3]  [+][-][reset][^][v][<][>]  [Canvas|Outline]|
| +----------------------------------------------------+ +-------------------------------+ |
| |            o     o--o                              | | core-lib#.        [maven]     | |
| |        o--O==o     \                               | | repository core-lib           | |
| |           (O)core-lib o--o                          | | Used by 9   Depends on 2      | |
| |        o--o   o                                    | | [In a cycle] [Focus on this]  | |
| |  (blue) maven (green) npm  larger = more dependants | | Depends on:  auth-client 1.2 > | |
| +----------------------------------------------------+ | Used by:  billing-service 2.7.1| |
|  1,000 units, 2,500 dependencies, 2 cycles             +-------------------------------+ |
+------------------------------------------------------------------------------------------+
```

Phone: the canvas takes the width at 60 vh with the toolbar wrapped; selecting a node opens the NodePanel as a bottom `Sheet`; the Outline mode is the default when `matchMedia("(pointer: coarse)")` and the graph has more than 200 nodes (a 1,000-node canvas on a phone is not a good tool; the user can still switch).

#### 4.9.8 Outline (the accessible tree) and keyboard

`dependencyTree(root, dir, adj, byId, maxDepth = 3, maxChildren = 50): TreeNode | null` `(cc)` builds `{id, label, ecosystem, cycle, children, more}`; a node already on the path is returned with `cycle: true` and no children (never recursion); more than `maxChildren` neighbours yields `more: n`. Rendered as nested `ul` with disclosure `Button`s (`aria-expanded`), "(cycle)" and "+12 more" as text, each node a `Button` that selects it (same NodePanel). Two roots in one panel: "Depends on" (direction `out`) and "Used by" (`in`). Default root: the selected unit, else the focus unit, else the highest in-degree unit (`traversalOrder(nodes)[0]`).

Canvas keyboard (focus is on the `role=group` container; nodes are not individual tab stops, 1,000 tab stops would be unusable): arrow keys pan by 10 % of the view; `+`/`=` zoom in, `-` zoom out, `0` reset; `Enter` on the unit finder selects; `Esc` clears the selection; `Tab` leaves the canvas to the NodePanel. Every pointer action has a button or key equivalent; nothing requires dragging (WCAG 2.5.7), targets are 32 px (2.5.8). A polite status line announces "Selected core-lib: used by 9, depends on 2" on selection (not on hover).

#### 4.9.9 States

Loading: graph skeleton; Layout running: `Spinner` + "Arranging {n} units"; WebGL unavailable (`hasWebGL` false or `webglcontextlost`): Alert "The graph needs WebGL, which is not available here" and the view switches to Outline automatically (the preference is not persisted); Empty graph; Truncated; Too large (never on a 200 response, defensive); Unknown focus (API 404): inline `Alert` "That unit is not tracked" with "Clear focus"; Stale; Error (panel `ProblemAlert`); Context loss after load: `GraphControllerOptions.onContextLost` (capture-phase `webglcontextlost` listener on the container; `GraphController` removes it in `kill()`) calls `onStatus("unavailable")`, the view kills the controller, shows the Outline and offers "Reload the graph" (V-10; verified in Chrome 155: one event per lost context, observable at the container in the capture phase). Theme change: `GraphController.setPalette` rewrites node, edge and label colours (V-09).

#### 4.9.10 Tests

| Id | Kind | Assertion |
|---|---|---|
| U18-GRF-01 (cc) | unit | `layout` is byte-deterministic for the 1,000/2,500 synthetic graph, finite, < 3 s (CI headroom), warm run moves nodes < 25 units on average and takes < 1.5 s |
| U18-GRF-02 (cc) | unit | `buildAdjacency` skips edges with a dropped endpoint; `nodeSize` monotone in 3..14; `ecosystemOrder`/`ecosystemSlot` (8th shared); `traversalOrder`; `renderability` for empty, no-webgl, 2,001 nodes, 20,001 edges |
| U18-GRF-03 (cc) | unit | `focusSet` selection beats hover and includes in- and out-neighbours; `nodeView` dims and drops labels of non-neighbours; `edgeView` hides unrelated edges |
| U18-GRF-04 (cc) | unit | `dependencyTree` marks a cycle instead of recursing, caps depth and breadth, returns null for an unknown root |
| C18-GRF-01 | component (browser mode) | `DependencyGraphCanvas` mounts, builds one Sigma instance, `kill` on unmount, StrictMode double mount leaves exactly one canvas and one worker |
| C18-GRF-02 | component | no-WebGL path renders the Outline and the reason, never throws |
| C18-GRF-03 | component | NodePanel lists neighbours with `VersionStatus` text; selecting from the panel moves focus to the heading |
| E18-GRF-01 | e2e (stub 1,000 nodes) | canvas present, `scp.graph.layout` < 1.5 s, `scp.graph.mount` < 600 ms unthrottled and < 1 s at 6x throttle on CI software GL (< 100 ms is checked only on a runner with a real GPU, informational), no long task during the layout phase, heap growth after 10 filter changes recorded (informational in v1) |
| E18-GRF-06 | e2e (stub) | theme switch while the canvas is mounted (`prefers-color-scheme` emulation or the toggle): a node's rendered colour changes to the dark palette and the label colour with it (V-09; browser probe `csp-probe/palette-probe`) |
| E18-GRF-07 | e2e (stub) | the layout worker answers `{type:"error"}` or its URL 404s (route abort): the Outline and the reason appear within 2 s, no endless spinner (V-10) |
| E18-GRF-08 | e2e | `WEBGL_lose_context.loseContext()` on the nodes canvas: `onStatus("unavailable")` fires once and the Outline is shown (V-10) |
| E18-GRF-02 | e2e (stub 3 nodes, one cycle) | keyboard-only: unit finder selects a node, panel updates, polite status text, `Esc` clears; arrow keys pan (camera state changes) |
| E18-GRF-03 | e2e | with WebGL disabled (`--disable-gpu --disable-webgl` project flag) the Outline appears and axe passes |
| E18-GRF-04 | e2e | `prefers-reduced-motion: reduce`: zoom completes in 0 ms (camera state immediate) |
| E18-GRF-05 | e2e | the layout worker loads under the production CSP (no violation reported) |

---

### 4.10 Package page (`/packages?purl=...`; N-01)

**Purpose.** One component by package URL: who produces it, who uses it, which versions are in use. Reached from every purl in the product; also usable as a lookup.

**Data.** `GET /api/v1/components?purl=` -> `componentQuery(purl)` (identity, `internal`, `producers[] {repo, unit, version, kind, ambiguous}`, counts `users`, `repos`, `versions[] {version, units, repos}`, `dependentsUrl`, `freshness`); `GET /api/v1/components/dependents?purl&scope&group&limit=50&cursor` -> `dependentsQuery` (infinite). The purl is validated with `PURL` before any request (a `400` is never provoked by the UI).

```
PackagePage
  without purl:  PageHeader "Packages"; Card with form: Label "Package URL" Input (placeholder pkg:maven/org.apache.logging.log4j/log4j-core) Button "Look up"; example links; Link "Browse versions in use" -> /dependencies?view=versions
                 submit: if PURL.test(value) navigate({search:{purl}}) else inline error (aria-invalid, aria-describedby) "Enter a package URL starting with pkg:"
  with purl:     PageHeader title={namespace/name} crumbs [Dependencies > Package] meta=<Badge ecosystem/> <Badge Internal|External/> <FreshnessBadge/>   p.font-mono purl + Copy button
                 Stat users | repos | versions in use
                 Producers [internal]  ul: Link repo, unit mono, version, kind; ambiguous -> Alert warning "More than one unit publishes this artifact"
                 Versions: list with counts + spread bar (spreadSegments)
                 Dependents: filters (scope Popover, group Select) ; DataTable caption "Units that use {name}": Repository (Link) | Unit | Ecosystem | Requirement | Resolved | Scope | Direct | Position (VersionStatus Badge: Up to date / Behind / Ahead / Unknown) | Source loc ; LoadMore(50)
```

`404 not-found` -> `ProblemPage` resource "package" with "No tracked repository uses or provides this package" and the link to Versions. Empty dependents (a producer nobody uses): `EmptyState` "Nothing depends on this yet".

**Tests.** C18-PKG-01: lookup form validation (valid, invalid, whitespace, trailing version in the purl is accepted and normalised by the API); U18-PKG-01 (cc): `packageSearch` rejects a non-purl and keeps the rest; C18-PKG-02: producers with `ambiguous`; E18-PKG-01 (stub): a purl with `@`, `%` and `/` round-trips through the URL (`router.buildLocation` then reload) and reaches the API as the same string; E18-PKG-02: unknown purl -> not-found page; E18-PKG-03: axe.

---

### 4.11 Pages (`/pages/$slug`)

**Purpose.** Configured Markdown pages (about, onboarding, policies): organisation content lives in configuration, not in the bundle (GR-14). The list of pages in the navigation is the shell's (`site.nav` entries of `kind: page`); this slice renders one page.

**Data.** `GET /api/v1/pages/{slug}` -> `pageQuery(slug)` (loader, `retry: false` so a 404 is immediate): `slug`, `title`, `summary`, `order`, `updatedAt`, `html` (sanitised), `toc`, `freshness`. `GET /api/v1/pages` -> `pagesQuery()` feeds the palette only.

```
PageView (features/pages/route.tsx)
  PageHeader title={page.title} description={page.summary} meta=<span>Updated <RelativeTime iso=updatedAt/></span>
  div.grid.xl:grid-cols-[minmax(0,1fr)_14rem].gap-8
    article: SafeHtml className="prose dark:prose-invert docs max-w-[72ch]"  + enhanceCodeBlocks (copy buttons, focusable pre)
    PageToc nav[aria-label="On this page"]   only when toc has >= 3 entries of depth 2-3 (xl: sticky aside; below: Collapsible above the article)
```

Links inside a page are plain anchors (the HTML is sanitised and rewritten server side; external links carry `rel="noopener noreferrer"` from the sanitiser); there is no `Doc.links` index for pages, so no delegation: portal-internal links cause a normal navigation to the server, which answers the SPA shell (acceptable: pages are rarely cross-linked into the app). Anchors within the page scroll natively with `scroll-padding-top`.

**States.** 404 -> `ProblemPage` resource "page" (title "Page not found", body names no slug detail), link to the dashboard; empty `html` -> `EmptyState` "This page has no content yet"; stale: banner (pages come from files, so `stale` means the last reload failed: cause text `descriptor.`/`parse.`); loading: title skeleton and four paragraph skeletons.

**Wireframe, desktop.**

```
+------------------------------------------------------------------------------------------+
| About this portal                                                           Updated 3 d ago|
| What it is and how it is run.                                                              |
| +----------------------------------------------------------+  On this page                 |
| | ## Purpose                                               |   Purpose                     |
| | The portal shows ...                                     |   Who runs it                 |
| | ## Who runs it                                           |   Contact                     |
| +----------------------------------------------------------+                               |
+------------------------------------------------------------------------------------------+
```

**Tests.** C18-PAGE-01: `PageView` renders html only through `SafeHtml`, TOC hidden for two entries, shown for three; C18-PAGE-02: empty html state; E18-PAGE-01 (stub, unit 17's `pages_about.json`): headings, `Updated` time, no console error; E18-PAGE-02: unknown slug -> not-found page; E18-PAGE-03: a page containing `<img src="https://evil.example/x.png">` as delivered by a hostile stub renders without a request leaving the origin (CSP blocks, the gate records the violation as expected via `allowConsole`; proves the policy, not the sanitiser, which unit 10 owns); E18-PAGE-04: axe light/dark.

---

### 4.12 Status and admin (`/status`; admin listener only; GR-02, GR-17, GR-40, GR-41)

**Purpose.** The operator's view: is the portal ready, what is it doing, what is stuck, what is it about to run out of (rate limits, tokens, memory budget), and one write action: retry a parked job. It is **not** linked from the public listener and does not exist there (the public `routes.json` has no `/status`; a request answers the real 404). The admin listener serves the same bundle with a route manifest of `/status` only, **and it also serves the 22 public API operations, `getSite` and `getSnapshot` included** (kernel RT-14 and `01-kernel.routes.tsv`: admin table = 24 API rows; the first draft said the admin listener had neither, which is false, V-11). The page nevertheless **does not depend on them** (the shell may call both; a failure of `getSite` must not hide the operator's page) and formats every time in **UTC** with the label "Times are shown in UTC": that is an operator convention (logs and metrics are UTC), no longer a workaround. There is no probe and no `useListener()` (OQ-18-01 resolved): on the public listener `/status` answers the real 404 with the shell body, the SPA matches its own `/status` route, `GET /api/v1/status` answers the `not-found` problem, and the page renders the **root not-found content** ("Page not found"), not a resource-specific message.

**Data.** `GET /api/v1/status` -> `statusQuery()`: `refetchInterval 10 s` (`STATUS_POLL_MS`), `staleTime 5 s`, paused while the tab is hidden (TanStack default `refetchIntervalInBackground: false`), conditional by ETag (an idle portal answers 304). `POST /api/v1/admin/jobs/retry {key}` -> `useRetryJob()` with header `X-SCP-Request: 1` and a JSON body (the endpoint refuses anything else); `202 {key, accepted}`, `400`, `404`, `429`, `503 queue-full`.

**Component tree.**

```
StatusPage (features/status/route.tsx)
  PageHeader title="Status" description="Operational state of this portal instance"
             meta=<HealthLight tone=overallTone(status)/> <span>Updated <RelativeTime generatedAt zone=UTC/></span> <Badge>{build.version}</Badge>
  [warnings]  WarningsPanel  section "Warnings ({n})"  ul (sortWarnings: critical, warning, info; server order is the tiebreak) li: severity icon + text ("Critical", "Warning", "Notice"), code (mono), message (displayText), since (RelativeTime)
  Grid of Cards (each section h2, content as dl via Stat or DataTable):
    ReadinessCard   state Badge (warming|ready|degraded|shutting-down), readySince, gates table (name, ok icon+text, detail), coverage Progress ("{have} of {total} repositories, core kinds: commits")
    SnapshotCard    version, builtAt + age, repoCount, invalidPublishes (Badge danger when > 0) + lastInvalidMessage
    StoreCard       state (ok|degraded + reason), size (bytes), rebuilds, lastBackupAt ("never" when null)
    BuildCard       version, commit, buildId, goVersion?, startedAt, uptime
    UpstreamsCard   table: name, concurrency, pending, running, stuck (Badge danger > 0), paused until + reason (rate_limit|unauthorized|breaker), breaker (closed|open|half-open|none)
    InstallationsCard table: source, credentialKind, account (accountType), state Badge (active|suspended|removed|unknown), repoCount, lastReconcileAt, tokenExpiresAt (Badge warning when < 24 h), keyFingerprint (mono), rate: per resource Progress + "{remaining} of {limit}, resets {RelativeTime resetAt}"
    JobsCard        table per kind: kind (mono), upstream, items, oldest fetch age, p95 age, lastSuccessAt, failing (Badge), parked (Badge)
    ParkedCard      section "Parked jobs ({parkedTotal})": DataTable key (mono), since, retryAt, reason (displayText), Button "Retry" -> RetryDialog ; "showing 200 of {parkedTotal}" when truncated
    TasksCard       table: name, every, lastAttemptAt, lastSuccessAt, lastError (displayText), running, skipped
    DocsCard        residency Progress residentBytes/budgetBytes, notResidentDocs + bytes
    ContentCard     descriptors: repositoriesWithProblems, problems ; manifests: enabled, repositoriesWithDiagnostics, disabledParsers (list), danglingUnits, staleRepos
    IntegrationsCard table: provider, enabled, state (ProviderState text), lastSweepAt, targets
RetryDialog (Dialog): title "Retry this job?", body "{key}" (mono), reason, text "Retrying releases the job from its parked state and schedules it now. It does not change stored data.", Buttons [Cancel] [Retry]
```

**Wireframe, desktop.**

```
+------------------------------------------------------------------------------------------+
| Status                                              (!) Degraded   Updated 4 s ago  v0.1.0 |
| Operational state of this portal instance            Times are shown in UTC               |
| Warnings (2)                                                                              |
|  [Critical] github.unauthorized  Credentials were rejected for installation acme  3 h      |
|  [Warning]  docs.budget-exceeded 14 documents are not resident                      1 d    |
| +--------------------------+ +--------------------------+ +----------------------------+  |
| | Readiness   (ok) Ready   | | Snapshot   v 1,284       | | Store     (ok) OK 38 MB    |  |
| | gates: store ok, sync ok | | built 12 s ago 94 repos  | | rebuilds 0  backup 2 h ago |  |
| | coverage [=======] 94/94 | | invalid publishes 0      | |                            |  |
| +--------------------------+ +--------------------------+ +----------------------------+  |
| Installations                                                                              |
|  acme  installation  Organization  active  94 repos  token 41 min  core [=====-] 4,812/5,000|
| Parked jobs (1)                                                                            |
|  docs.refresh/billing-service  since 3 h  retry 21:00  "parse.invalid_front_matter" [Retry]|
+------------------------------------------------------------------------------------------+
```

Phone: cards stack in one column (`Collapsible`, Warnings and Parked jobs open, the rest closed with a one-line summary in the trigger); tables become definition lists.

**Behaviour.**

```
overallTone(s) = down     if readiness.state == "shutting-down" or any gate not ok or store.state != "ok"
                 degraded if readiness.state == "degraded" or any warning with severity != "info"
                 ok       otherwise
HealthLight text: ok "Healthy", degraded "Degraded", down "Unavailable"; icon + text + tone, never colour alone
rateShare(r) = limit > 0 ? clamp(remaining/limit, 0, 1) : null           # unknown limit is "Unknown", never "0 % left"
canRetry(key) = /^[a-z][a-z0-9._-]*\/\S+$/ and length <= 300             # guard: the UI never builds a key, it only echoes the server's; a key failing the guard has a disabled button
retry flow:  Button -> Dialog (focus trapped; initial focus on Cancel) -> Retry -> mutate(key)
   pending:  Retry button aria-disabled + Spinner
   202:      Toast "Retry scheduled for {key}"; invalidate ["status"]; focus returns to the Parked heading if the row disappeared, else to the row's button
   404:      Toast "That job is no longer parked"; refetch
   429/503:  Toast with the problemView text and Retry-After; dialog stays open
   any:      request id in the toast details
polling:     visibility-aware; a failed poll keeps the last data and the shell's ConnectionBanner explains; the "Updated" label turns to a stale Badge when the last successful poll (`dataUpdatedAt`, client clock against client clock) is older than 60 s; `generatedAt` is displayed but never compared with the client clock (clock skew between operator laptop and server would show a false "stale")
untrusted:   warning.message, task.lastError, parked.reason, store.reason, snapshot.lastInvalidMessage are rendered as text through displayText (they can contain upstream strings)
```

**States.** Loading: card skeletons. Error on first load: `ProblemPage` (an admin who cannot reach `/api/v1/status` needs the request id more than anyone); `401/403` -> `ProblemPage` kind unauthorized/forbidden with "The admin listener requires credentials presented by your gateway"; refetch error with data: last data stays, banner from the shell. Empty sub-sections (no parked jobs, no warnings) show a positive one-line text ("No parked jobs", "No warnings"), never an empty card.

**Accessibility.** Each card is a `section` with an `h2`; tables carry captions; `Progress` has `aria-label` and a text value; the retry dialog is a `Dialog` with `role="alertdialog"` semantics (initial focus on Cancel; Esc cancels); toasts use the shell's polite live region; refreshes every 10 s announce nothing (only state changes of the overall light are announced once, via `role="status"` on the HealthLight text).

**Tests.**

| Id | Kind | Assertion |
|---|---|---|
| U18-STA-01 (cc) | unit | `overallTone` for ready, degraded readiness, a failed gate, degraded store, shutting-down, info-only warnings; `sortWarnings` puts critical first and is stable; `rateShare` for limit 0, remaining > limit, negative; `canRetry` accepts `docs.refresh/billing-service`, rejects spaces, empty, 301 characters |
| C18-STA-01 | component | `RetryDialog`: Esc cancels, initial focus on Cancel, Retry posts exactly `{key}` with the `X-SCP-Request: 1` header (MSW handler asserts both), 404 shows the toast and refetches |
| C18-STA-02 | component | untrusted `reason` containing a bidi override and 400 characters renders as sanitised, capped text |
| C18-STA-03 | component | installation with `rate: []` and with `limit: 0` renders "Unknown", not a full or empty bar |
| E18-STA-01 | e2e (admin listener, real binary + fakegithub) | `/status` renders readiness gates, snapshot version, installation `acme`; with `/api/v1/site` and `/api/v1/snapshot` stubbed to 500 the page still renders completely (independence), times are in UTC with the label |
| E18-STA-02 | e2e (public listener) | `/status` answers a real 404 with the shell body; the SPA shows the not-found page; no navigation link to `/status` exists anywhere |
| E18-STA-03 | e2e (stub) | parked job -> Retry -> dialog -> confirm -> 202 -> toast; keyboard only |
| E18-STA-04 | e2e | polling pauses when the page is hidden (`page.evaluate` visibility override) |
| E18-STA-05 | e2e | axe light/dark desktop/phone |

---

### 4.13 Search (command palette; no route)

**Decision.** The product has no search endpoint and no `/search` route (the route table has none; `/search` would be a 404). Search is a **global command palette** over what the SPA already has or can fetch cheaply: groups, repositories, documents of the current repository, configured pages, and components (package names). It finds *things to go to*, not text inside documents (non-goal; OQ-18-06 asks whether a server-side full-text endpoint is wanted for v1.1). Per-page filters (`q` on dashboard, group, dependency tables, commit log) cover "search within this list".

**Trigger.** `Ctrl+K` / `Cmd+K` anywhere, `/` when focus is not in an editable element, and a visible "Search" button with a `Kbd` hint in the shell header (icon-only on phones). Key handling lives in the shell; this unit supplies `SearchPalette` (`{ open, onOpenChange }`). The shortcut is announced in the button's accessible name ("Search (Ctrl K)") and documented on the keyboard help (4.15).

```
SearchPalette (app/search-palette.tsx)
  CommandDialog  (shadcn Command in Dialog; DialogTitle sr-only "Search"; DialogDescription sr-only "Search groups, repositories, documents, pages and packages")
    CommandInput  label "Search the portal"  placeholder "Search groups, repositories, docs ..."   (shouldFilter={false}: ranking is ours)
    CommandList aria-live via cmdk; sr-only status "{n} results"
      CommandGroup heading=Go to | Groups | Repositories | Documents (this repository) | Pages | Packages  <- Ranked[] from rank(query, items, recents), GROUP_ORDER, MAX_PER_KIND
        CommandItem value=item.key onSelect -> navigate(item.target); pushRecent; onOpenChange(false)
          layout: icon by kind (lucide: LayoutGrid, FolderGit2, FileText, StickyNote, Package, CornerDownRight) | title | hint (muted, mono for slugs)
      CommandEmpty  "No results for \"{q}\""
      loading row   Spinner "Looking up packages ..." (only while the components request is in flight)
```

**Sources.**

| Kind | Source | When fetched | Match fields (`haystack`, first = identifier) |
|---|---|---|---|
| Go to | static list: Dashboard, Commits, Dependencies (if `features.manifests`), configured `site.nav` pages. **No Status entry** (OQ-18-01 resolved: nothing in the SPA can know which listener it is on without a probe, and `/status` is deliberately not a public feature; an operator types the admin URL) | always | title |
| Groups | `groupsQuery()` | on first open (cached 10 s) | id, displayName, description |
| Repositories | `allReposQuery()` (`GET /api/v1/repos`, one page of up to 1,000; with more, `page.nextCursor` is non-null and the palette adds the row "Some repositories are not listed, narrow the search" (V-23: the first draft silently ignored the cursor)) | on first open (cached 60 s) | slug, title, summary, tags |
| Documents | `repoQuery(current repo).docs` (already cached when on a repository route) | only on `/repos/$repo*` | title, path, description, tags |
| Pages | `pagesQuery()` | on first open | slug, title, summary |
| Packages | `GET /api/v1/dependencies/versions?q={query}&limit=6` | when `features.manifests` and the query has >= 2 characters, debounced 250 ms, aborted on the next keystroke | name, namespace |

**Ranking (`search-model.ts (cc)`).** `score(query, haystack)`: per field, exact match 100, prefix 80, word-prefix 60, substring 40, in-order subsequence (first field only) 10; field `i` loses `0.5 * i` so an identifier hit beats the same hit in a description; the best field wins; 0 means no match; empty query scores 1 for everything. `rank` sorts by score descending, then title, then key (deterministic), groups by `GROUP_ORDER = go, group, repo, doc, page, component`, caps each group (`MAX_PER_KIND`: 4, 5, 8, 6, 4, 6). Empty query shows only the "Go to" items and the recents (`pushRecent(list, key, 5)`, newest first, unique). Recents are stored under `scp-recent-search` (JSON array of item keys, validated with Zod, `try/catch` around storage); a key that no longer resolves is dropped silently.

**Behaviour and edge cases.** A source that fails does not block the others (an inline row "Some results are unavailable" with no detail); typing a purl (`pkg:...`) adds a top item "Open package {purl}" targeting `/packages?purl=` (validated with `PURL`); a query equal to a repository slug ranks that repository first (exact tier); the dialog is not mounted until first opened (the sources are not fetched at page load); closing returns focus to the trigger; on phones the dialog is full width, anchored to the top so the keyboard does not cover results; query text never leaves the browser except the packages request.

**Tests.** U18-SRCH-01 (cc): tiers and tie-breaks (identifier beats description, deterministic order), empty query semantics, `MAX_PER_KIND` caps, `pushRecent` uniqueness and max, doc item for README targets the repository overview; C18-SRCH-01: palette opens on Ctrl/Cmd+K and `/` (not inside inputs), Esc closes and restores focus, Enter on an item navigates and records the recent; C18-SRCH-02: a failing packages request still shows other groups; E18-SRCH-01: search `payments` finds the repository and navigates; E18-SRCH-02: keyboard only; E18-SRCH-03: axe with the dialog open; E18-SRCH-04: network log shows no request before first open and exactly one packages request per settled query.

---

### 4.14 Error, not-found, unavailable, skew and empty states

#### 4.14.1 `ProblemPage`

```
ProblemPage({ error, resource?, onRetry?, homeLink = true })
  v = problemView(error)
  main-level structure (inside the shell's <main>):
    Icon (lucide by kind: SearchX not-found, PowerOff disabled, Hourglass not-ready, CloudOff unavailable|offline, Gauge rate-limited, Lock forbidden|unauthorized, TriangleAlert bad-request|server-error, RefreshCw skew)  aria-hidden
    h1  title (message id problem.<kind>.title; for not-found: "{Resource} not found")     tabIndex=-1, focused on mount (RouteAnnouncer announces the document title)
    p   body  (problem.<kind>.body)
    p   details: "Error code {code}, HTTP {httpStatus}"  +  "Request id {requestId}" with a Copy button (24 px) when present
    actions: [Try again] (retry manual) | auto-retry text "Retrying in {n} s" (aria-hidden countdown; the sentence "Retrying automatically." is the only announced part) | [Go to the dashboard] | [Back]
  document <title> = the h1 text
```

| Kind | Title | Body | Retry |
|---|---|---|---|
| not-found | "{Group \| Repository \| Document \| Page \| Package} not found" (generic "Not found") | "This {thing} does not exist, or you do not have access to it." (the API never says which; neither does the UI) | none |
| disabled | "This feature is switched off" | "The administrator has not enabled {feature} on this installation." | none |
| not-ready | "The portal is starting up" | "It is collecting data from GitHub for the first time. This page loads automatically when it is ready." | auto, `retryDelayMs` |
| unavailable | "Temporarily unavailable" | "The portal could not complete the request. Try again in a moment." | manual |
| rate-limited | "Too many requests" | "Please wait a moment. This page retries automatically." | auto (`Retry-After`) |
| forbidden | "Access denied" | "You are not allowed to see this." | none |
| unauthorized | "Sign-in required" | "Sign in through your organisation's gateway and reload this page." | none |
| bad-request | "That request was not valid" | "The address or the filters on this page are not valid. Clear the filters and try again." + "Clear filters" action when the route has search params | none |
| server-error | "Something went wrong on our side" | "It has been logged. If it keeps happening, send the request id below to the portal administrator." | manual |
| offline | "You are offline" | "Check your connection. This page retries when the connection returns." | auto on `online` event and every 15 s |
| skew | "A new version is available" | "Reload to continue." | manual (Reload button) |

The root `notFoundComponent` (unknown **shape**, where the server already answered the real 404) uses kind not-found without a resource: title "Page not found", body "Check the address, or search for what you were looking for.", actions: "Search" (opens the palette), "Go to the dashboard". The root `errorComponent` handles render exceptions: kind server-error with `code: client-error`, no request id.

#### 4.14.2 Inline `ProblemAlert` (unit 17) and `StaleBanner` (this unit)

`ProblemAlert` shows the same title text and a Retry for a panel; it must be implemented over `problemView` (3.3). `StaleBanner` copy: stale: "Showing data from {age}. {cause}" (cause omitted when unknown); error: "Updates are failing: {cause}. Showing data from {age}." Both are `role="status"`, never `alert`.

#### 4.14.3 Empty-state catalogue

| Place | Title | Description | Action |
|---|---|---|---|
| Dashboard, no groups | "No groups are configured" | "Groups are defined in the portal configuration under catalog.groups." | Commits |
| Dashboard, filter | "No group matches \"{q}\"" | | Clear filter |
| Group, no repositories | "No repositories in this group yet" | "None of this group's patterns match a repository you can see." | Dashboard |
| Group, filters | "No repositories match these filters" | | Clear all |
| Commit log, none | "No commits yet" | "The portal is still collecting commits, or this scope has none on its default branch." | Dashboard |
| Commit log, filter | "No loaded commit matches \"{q}\"" | "Only the commits loaded so far are searched." | Load more / Clear |
| README missing | "This repository has no README" | | View on GitHub |
| Document not loaded | "This document is not available here" | reason by state (too large, unsupported, not loaded) | View on GitHub |
| Dependencies none | "No package manifests were found" | | View on GitHub |
| Graph none | "No internal dependencies between tracked repositories yet" | "Dependencies on external packages are listed in the Versions view." | Open Versions |
| Dependents none | "Nothing depends on this yet" | | |
| Page empty | "This page has no content yet" | | |
| Search none | "No results for \"{q}\"" | | |
| Status, no warnings / parked | "No warnings" / "No parked jobs" | | |

---

### 4.15 Accessibility (WCAG 2.2 AA) and keyboard

#### 4.15.1 Rules that hold on every page

| Concern | Rule | Where verified |
|---|---|---|
| Landmarks and headings | shell landmarks, one `h1`, sections `h2`, no skipped levels; `aside`s named; a skip link to `#main` (shell) | axe `landmark-*`, `heading-order`, `page-has-heading-one`; Playwright heading outline snapshot per route |
| Names and roles | every control has a visible label or `aria-label`; icon-only buttons have `aria-label`; badges have text; images have `alt` (badges: `badge.label`, GR-39); decorative icons `aria-hidden` | axe `button-name`, `image-alt`, `link-name`; ESLint `jsx-a11y` |
| Colour and contrast | tokens verified (3.5.2); text >= 4.5:1, UI components and focus >= 3:1; status never colour alone (1.4.1); links underlined in prose | contrast Vitest gate, axe `color-contrast` in both themes |
| Focus | visible 2 px ring, never obscured by the sticky header (2.4.11: `scroll-padding-top`), logical order, no trap except dialogs (restored on close), focus to `h1`/article after navigation | Playwright keyboard specs; manual |
| Targets | >= 24x24 CSS px (2.5.8): copy buttons 28, filter chips 24, table sort buttons full header cell, graph toolbar 32 | Playwright bounding-box assertion on all `button, a` in main (excluding inline text links) |
| Dragging | no function needs dragging (2.5.7): graph has buttons and keys | e2e |
| Motion | `prefers-reduced-motion` removes transitions and camera animation; nothing auto-plays | e2e |
| Reflow and zoom | no horizontal scroll at 320 CSS px and at 400 % zoom (1.4.10) except inside code blocks, tables-in-prose and the graph canvas (each focusable regions) | Playwright 320 px project assertion |
| Live regions | `aria-live` only for state changes: result counts, "N more loaded", status light; never for polling refreshes | component tests assert the live-region text and that a refetch changes none |
| Forced colours | badges and chips keep a 1 px `CanvasText` border under `@media (forced-colors: active)`; the graph is canvas (outline mode is the accessible path) | Playwright `forcedColors: "active"` smoke with axe |
| Language | `<html lang>` from the active locale (shell); rendered documents set `lang="en"` only when `renderedBy: "github"` (known English); otherwise inherit | component test |
| Time | `<time datetime>` everywhere; relative text plus exact time in the tooltip and in the accessible name of the time element | unit 17's `RelativeTime` |
| Error identification | inline validation (`/packages` lookup) uses `aria-invalid` + `aria-describedby` | component tests |

#### 4.15.2 Keyboard summary

| Context | Keys |
|---|---|
| Anywhere | `Ctrl/Cmd+K` or `/` opens search; `Esc` closes dialogs, popovers, sheets; `Tab`/`Shift+Tab` in DOM order |
| Palette | type to filter; `Up`/`Down` move; `Enter` go; `Esc` close; `Backspace` on empty query does nothing |
| Tables | `Tab` to a header button, `Enter`/`Space` sorts; rows are not focusable (links inside are) |
| Filter popovers | `Enter`/`Space` opens, arrows move between checkboxes (native checkbox semantics), `Space` toggles, `Esc` closes |
| Doc navigation | `Enter`/`Space` toggles a section button; links as usual; filter input clears with `Esc` |
| Commit log | `Tab` through rows' links; "Load more" is a button; the new-commits pill is a button |
| Graph canvas (when focused) | arrows pan, `+`/`-` zoom, `0` reset, `Esc` clears selection; the unit finder is a combobox (`Up`/`Down`/`Enter`) |
| Status | `Tab` to Retry, `Enter` opens the dialog, `Esc` cancels |

#### 4.15.3 Verification

Automated: `@axe-core/playwright` 4.13.0 on **every route** in four configurations (light/dark x desktop 1280 x phone 360) with tags `wcag2a, wcag2aa, wcag21a, wcag21aa, wcag22aa` and the rule `color-contrast` enabled, failing the build on any violation or incomplete result that is not on an explicit, commented allow-list; the open-dialog states (palette, retry dialog, filter sheet) are scanned separately. Manual, each release and logged in the release notes: keyboard-only walk of the five main journeys (dashboard -> group -> repository -> document; commits; search; dependency graph with the outline; status retry), NVDA with Firefox and VoiceOver with Safari on the document viewer and the table views, 200 % browser zoom and 320 px reflow, forced-colors mode.

---

## 5. Data

### 5.1 Server-side data

None. This unit owns no store table, no snapshot slot, no migration and no server cache. Everything it shows is read through the API (the readmodel snapshot serves it; unit 03/15). There is nothing to add to the architecture's section 9.

### 5.2 Browser-side data (all disposable)

| What | Where | Lifetime | Rules |
|---|---|---|---|
| Server state | TanStack Query cache, **memory only** | `gcTime` 5 min after the last observer (unit 17 default); dies with the tab | never persisted (no `persistQueryClient`, no IndexedDB): a deploy can change shapes and the tab must never revive an old shape; infinite queries keep all loaded pages while observed |
| URL state | router search params and path | the address bar | every filter, sort, view, focus and selection that should be shareable (3.1); nothing secret |
| Ephemeral UI state | component state | the component | open popovers, hover, scroll-spy, dialog state, "which collapsible is open" |
| Per-viewer conveniences | `localStorage` | until cleared | table below; every access in `try/catch` (private windows, blocked storage, previews and thumbnail capture can throw or return nothing); the page renders correctly with no storage at all; parsed values validated with Zod and discarded when invalid |

| Key | Owner | Content | Validation | Written when |
|---|---|---|---|---|
| `scp-recent-search` | this unit | JSON array of up to 5 palette item keys (`repo:payments-api`, `doc:payments-api:docs~overview.md`) | `z.array(z.string().max(300)).max(5).catch([])` | an item is chosen in the palette |
| `scp-graph-mode` | this unit | `"canvas"` or `"outline"`, the viewer's explicit choice | `z.enum([...]).catch(undefined)` | the viewer toggles the mode (the automatic no-WebGL switch is never stored) |
| `scp-theme`, `scp-theme-default`, `scp-announcement-dismissed`, `scp-reload-guard` | unit 17 (`STORAGE`) | listed for completeness | unit 17 | unit 17 |

`scp-recent-search` and `scp-graph-mode` are added to unit 17's `STORAGE` constants object (T-18-007) so the list of keys lives in one file. No cookies, no `sessionStorage`, no IndexedDB, no service worker in v1.

---

## 6. Configuration

### 6.1 Keys this unit owns (YAML)

None are required. One additive server key is **proposed** (D-18-03) so that the dashboard feed length is operator-configurable instead of a constant (a hard-coded limit cannot be tuned by an operator; GR-14):

| Key | Type | Default | Validation | Env | Consumer |
|---|---|---|---|---|---|
| `site.dashboard_commits` (proposed) | integer | `10` | 1..50 and `<= commits.latest_n` | `SCP_SITE_DASHBOARD_COMMITS` | exposed as `Site.dashboardCommits` (additive, optional integer in `openapi-public.yaml`); the dashboard requests `limit=` this value |

Until accepted, the SPA constant `DASHBOARD_COMMITS = 10` applies (`dashboard/api.ts (cc)`). The key would belong to unit 15's `site:` block; this unit only reads the field, falling back to the constant when the API does not send it.

### 6.2 Server configuration this unit's behaviour depends on (read through the API, never read directly)

| Key (architecture section 16) | Appears as | Effect on the UI |
|---|---|---|
| `site.title`, `site.description` | `Site.title`, `description` | document title suffix, shell |
| `site.nav[]` | `Site.nav` | shell navigation; pages named there are rendered by 4.11 |
| `site.display_time_zone` | `Site.displayTimeZone` | day grouping, clock times, tooltips (4.1.7) |
| `site.default_theme` | `Site.defaultTheme` | unit 17 |
| `pages.announcement.*` (written `site.announcement.*` before CD-CF-09; the keys are owned by `pages`, unit 10) | `Site.announcement` | unit 17 (plain text + level + dismiss by content-hash id) |
| `catalog.groups[].*` | `GroupSummary` | configuration order on the dashboard, descriptions (GR-12), default repository state |
| `commits.keep` (30), `commits.latest_n` (50) | bounds of the commit log (30 x repositories) and of the head page | `MAX_LOG_ROWS` is a client cap well above the usual bound |
| `manifests.enabled` | `Site.features.manifests` | hides the Dependencies nav entry, tab, group button and palette group; direct URLs get `feature-disabled` |
| `live.sse.enabled` | `Site.features.sse` | unit 17 chooses polling or SSE; pages are indifferent |
| `integrations.*` | `Repo.statuses[]`, badge kinds `build`, `security` | status list and badges appear only when configured |
| `badges.palette` | server SVG colours | none |
| `docs.max_*`, `docs.resident_budget` | `DocState` values | empty-state wording (4.5, 4.6) |

### 6.3 Compile-time constants (not configuration; named so nothing is a magic number)

| Constant | Value | Where | Why |
|---|---|---|---|
| `DASHBOARD_COMMITS` | 10 | `dashboard/api.ts (cc)` | feed length |
| commit page size | 50 | `commitLogQuery(scope, pageSize = 50)` (cc) | API default; max 200 |
| `MAX_LOG_ROWS` | 1000 | `commits/api.ts (cc)` | DOM size and reading value; far above `30 x repositories` for most installations |
| dependency page size / cap | 100 / 2,000 rows | `repoDepsQuery(repo, s, pageSize = 100)` (cc) / page | |
| versions and dependents page size | 50 | `versionsQuery`, `dependentsQuery` (cc) | |
| `RENDER_STEP` | 100 rows | `shared/data-table` consumers | progressive rendering of complete lists |
| `GRAPH_LIMITS` | `renderNodes 2000, renderEdges 20000, arrowEdges 3000` | `graph/model.ts (cc)` | equals the API caps; arrows only below 3,000 edges |
| layout ticks | cold 300, warm 60 (warm when >= 90 % of nodes have a previous position) | `graph/layout.ts (cc)` | 4.9.3 |
| `STATUS_POLL_MS` | 10,000 | `status/api.ts (cc)` | |
| `MAX_PER_KIND` | `go 4, group 5, repo 8, doc 6, page 4, component 6` | `app/search-model.ts (cc)` | palette caps |
| recent searches | 5 | `pushRecent` (cc) | |
| `AVATAR_COLOURS` | 8 | `shared/avatar.ts (cc)` | |
| display text cap | 200 graphemes | `displayText` (cc) | |
| retry backoff | `min(30, 2^(n+1))` s, +-20 % jitter, `Retry-After` capped at 120 s | `retryDelayMs` (cc) | |
| scroll-spy margin | `-80px 0px -70% 0px` | `DocToc` | |

### 6.4 Build and test environment (unit 17's, listed because tests here depend on it)

`__BUILD_ID__` (Vite `define`; `dev` in development), `SCP_E2E_URL` (default `http://127.0.0.1:9090`), `PW_CHANNEL` (Playwright browser channel), `CI`. This unit adds none.

---

## 7. HTTP and API surface owned

**None.** The unit adds no endpoint, middleware or handler. What it owns on the HTTP side is the **SPA URL space** (section 3.1 and 1.2: 12 routes, their params and search schemas) and its requests, listed per page below so unit 07's contract tests can check coverage (every operation in the public contract that has a UI use appears here; `getOpenApiDocument`, `getSnapshot`, `streamEvents`, `getSite` are the shell's):

| operationId | Used by | Section |
|---|---|---|
| `listGroups` | dashboard, palette, commits group select | 4.2, 4.13 |
| `getGroup` | group page header | 4.3 |
| `listGroupRepos` | group repository table | 4.3 |
| `listRepos` | palette | 4.13 |
| `getRepo` | repository layout and overview, doc navigation, palette documents | 4.5, 4.6 |
| `listCommits` | dashboard panel, group panel, commit logs, "new commits" probe | 4.2-4.4 |
| `getRepoDoc` | README panel, document viewer | 4.5, 4.6 |
| `listRepoDocs` | **not used** (the composite carries the same index; kept for non-SPA consumers and for `q` search in v1.1) | |
| `listPages`, `getPage` | palette; pages | 4.11, 4.13 |
| `getRepoManifests`, `listRepoDependencies` | repository dependencies | 4.7 |
| `getRepoSbom` | link only (`download`) | 4.7 |
| `getDependencyGraph` | graph and table views | 4.8, 4.9 |
| `getDependencyVersions` | versions view, palette package search | 4.8.2, 4.13 |
| `getDependencyLicenses` | licences view | 4.8.3 |
| `getComponent`, `listComponentDependents` | package page | 4.10 |
| `getStatus`, `retryJob` (admin) | status page | 4.12 |

Conventions this unit relies on (contract, restated so a mistake is visible): JSON is camelCase; errors are RFC 9457 `application/problem+json` with a stable `code`; collections answer `{items, page{nextCursor, limit}, freshness}`; bounded collections (groups, repositories, documents, pages) fit in one page (`LimitBounded` default 1,000), unbounded ones (commits, dependencies, dependents, versions) page by cursor and limit 1..200 (default 50); responses carry weak ETags and `Cache-Control: no-cache`, so the browser's HTTP cache performs revalidation and the SPA has no ETag code; every response carries `X-SCP-Build` (unit 17 checks it); `308` for a renamed repository; `503 not-ready` only on cold start; array query parameters use repeated keys (`ecosystem=maven&ecosystem=npm`, `openapi-fetch` default `explode: true` serialisation is what `getDependencyGraph` and friends declare).

**Requests of the API (additive, none blocking):** (1) `Site.dashboardCommits` (6.1); (2) `author` and `q` filters on `listCommits` for v1.1 so the commit filter can search beyond loaded rows (the client filter is the v1 behaviour and says so in the UI); (3) `uiUrl` of a document without the Markdown extension (D-18-01); (4) the `lastErrorCode` vocabulary pinned in the contract (OQ-18-03).

---

## 8. Concurrency and lifecycle

This is a single-threaded client with one worker; the rules below are what keeps it correct.

| Concern | Design |
|---|---|
| Request cancellation | every `queryFn` forwards TanStack Query's `signal`; leaving a route or changing a filter aborts the request; `client-aborted` problems are swallowed by the query client and never rendered |
| Retries | query-level: transient problems retried twice with `Retry-After` honoured (unit 17's `retryPolicy`, `retryDelay`: 1, 2, 4 s capped at 8 s, `Retry-After` capped at 60 s); page-level auto retry for `not-ready`, `rate-limited`, `offline` is `ProblemPage`'s countdown (`retryDelayMs`, cap 120 s) and **does not stack** with the query retries (the page retry calls `router.invalidate()` once per tick); 4xx other than 429 are never retried |
| Polling | snapshot version every 30 s (unit 17, visibility-aware); status page every 10 s (`refetchInterval`, `refetchIntervalInBackground: false`); a 120 s sweep refetch on every active query (unit 17 default); nothing else polls; hidden tabs make no requests |
| Debounce | filter inputs write the URL 200-250 ms after the last keystroke (`replace: true`); the palette's package request is debounced 250 ms; timers are cleared in effect cleanups; the debounced value is derived state, never a source of truth |
| Request ordering | the query key contains the whole filter state, so a late response for an old filter can only fill its own cache entry; the layout worker uses an explicit `key` and drops stale responses |
| Pagination | infinite queries fetch the next page only on click; `fetchNextPage` is guarded by `isFetchingNextPage`; a rejected cursor resets the list (4.1.10) |
| Worker | `layout.worker.ts` is created per layout run, receives one message, returns a transferred `Float32Array` (or `{type:"error"}`), and is `terminate()`d after the response, when superseded and on unmount; a worker `error` event, an error message or a synchronous constructor failure switch the view to the Outline (V-10); no shared state, no `importScripts`; its URL is minted by the Trusted Types policy (10.4) |
| Sigma and WebGL | one `GraphController` per mounted canvas: constructed in an effect, `kill()` in its cleanup (frees the WebGL context); React StrictMode's development double mount (create, destroy, create) is therefore harmless and is covered by C18-GRF-01; `webglcontextlost` kills the controller and offers a reload; a `ResizeObserver` is disconnected in the same cleanup |
| React rules | the code is compiled by the React Compiler (unit 17); components are pure, no mutation of props or query data (all array operations copy: `sortNodes`, `groupByDay`, `rank`); refs are read only in effects and handlers; subscriptions use `useSyncExternalStore` or TanStack hooks; no `useEffect` for derived data. **One deliberate manual memo (V-02):** `SafeHtml` keeps `useMemo(() => ({ __html: toTrustedHtml(html) }), [html])` because the Trusted Types policy returns a new object per call and React re-assigns `innerHTML` on `!==`; the compiler's automatic memoisation of an impure policy call is not something to rely on (and it is off under Vitest). Unit 17's "do not add `useMemo`" rule needs an exception comment on that one line (the compiler preserves manual memoisation, `preserve-manual-memoization`); the regression test is `shared/safe-html.test.tsx` |
| Long-lived tabs | query cache `gcTime` 5 min; the graph and its adjacency are released when the view unmounts; position cache (last three layouts) is a module-level LRU of at most 3 x 2,000 entries; no listeners outlive their component; the palette is unmounted when closed |
| Clipboard and timers | the "Copied" text resets after 1.5 s via a cleared timeout; auto-retry countdowns clear on unmount and on success |
| Server-sent events (v1.1) | unit 17's hook owns the `EventSource`; on `invalidate`, `topicRoots` maps topics to query roots (4.1.4); on `resync` or reconnect it invalidates everything; nothing in this unit opens a stream |
| Shutdown behaviour | during a server restart the API answers errors or nothing: queries fail, last data stays, the shell's connection banner appears, `ProblemPage` appears only if the primary resource of a freshly opened page fails |

---

## 9. Observability (frontend side)

The browser sends nothing to a third party and the portal has no analytics (privacy posture, GR-18, GR-35). What exists:

1. **Request ids for humans.** Every problem shows the server's request id (from `Problem.instance` `urn:uuid:...` or `X-Request-Id`) with a Copy button, so a report quotes the same id that is in the server logs (`request_id`).
2. **CSP reports.** The browser posts violation reports to `/csp-reports` (counted by `scp_csp_reports_total` on the server, unit 06/16). During rollout the policy is Report-Only; the Playwright gate (unit 17 `e2e/fixtures.ts`) fails any test on a violation, a `console.error` or a `pageerror`.
3. **Console policy.** Production code logs nothing to the console except the single `console.error("panel error", ...)` in unit 17's `PanelBoundary`; e2e treats any other console error as a failure, so accidental logging is caught.
4. **User Timing marks and measures** (also the data the performance budgets in the tests read). Created through `lib/perf.ts` (spec): `mark(name)`, `measure(name, startMark, detail?)`, both no-ops when `performance` lacks the API and wrapped in `try/catch`:

| Measure name | From, to | Used by |
|---|---|---|
| `scp.route.ready` | navigation start (router `onBeforeNavigate`) to the primary query settled and the page body committed (`useEffect` after first render with data); `detail.route` is the route id | e2e asserts per page; budgets in 12.6 |
| `scp.docs.render` | `SafeHtml` html set to `enhanceCodeBlocks` done | e2e on a large document (< 100 ms for 500 KB of HTML in CI) |
| `scp.graph.layout` | worker `postMessage` to response | graph budgets (4.9.4) |
| `scp.graph.mount` | `GraphController.setData` call to the second animation frame after it | graph budgets |
| `scp.search.open` | palette open to first rendered result group | e2e (< 300 ms warm) |
| `scp.list.render` | route data ready to the last row of the first 100 committed (large tables) | e2e on the 1,000-row stub |

5. **Test-visible state hooks** (kept minimal, no `data-testid` unless no role or label identifies the element): `data-freshness="fresh|stale|error|unknown"` (unit 17's badge), `data-problem-code="<code>"` (alert and page), `data-doc-id` on the document wrapper.
6. **Not provided in v1, on purpose:** a client-error beacon endpoint (would need a contract addition, rate limit and a privacy statement; OQ-18-09), real-user monitoring, source maps in production.

Server metrics are not owned here. The list in architecture 18.2 is unchanged; the frontend contributes the user-timing measures above only.

---

## 10. Security

### 10.1 Trust boundaries in this unit

| Input | Trust | Handling |
|---|---|---|
| Structure of API JSON | trusted (generated types, contract-tested); not validated at runtime in production (the build-id skew check replaces Zod response validation, architecture 12.1) | typed access; every access of an optional or nullable field is explicit; `noUncheckedIndexedAccess` |
| Strings marked `x-scp-untrusted` (GitHub text, descriptor text, provider text, commit subjects, author names, tags, warning messages, task errors) | untrusted text | rendered only as React text children; length-capped and control/bidi-stripped by `displayText` where one line is expected (10.2); never as HTML, never in an attribute that is a URL, never concatenated into a URL |
| Strings marked `x-scp-safe-html` (`Doc.html`, `Page.html`) | sanitised by the server (SafeHTML), still treated as hostile by the browser policy | only `SafeHtml`, only through the `scp-html` Trusted Types policy; CSP `script-src 'self'` and `require-trusted-types-for 'script'` are defence in depth |
| URLs from the API (`htmlUrl`, `url`, `sourceUrl`, `detailUrl`, `markdownUrl`, badge `url`) | trusted shape, checked anyway | `ExternalLink` (unit 17) renders a link only for `http:`/`https:` through `isWebUrl` (anything else is plain text; every API value here is a contract `HttpsUrl` or a nullable plain string such as `ProviderStatus.detailUrl`, which is additionally required to start with `https:`); badge and media URLs must start with `/` and not `//` (`isPortalPath`); `javascript:`, `data:` and protocol-relative values never become an `href` or `src` |
| URL (path, search) typed by a user or attacker | untrusted | Zod schemas with `.catch` defaults; splat segments `^[A-Za-z0-9._-]+$`; purl `^pkg:[a-z][a-z0-9.+-]*\/\S+$` and <= 1,024 chars; values go into requests through `openapi-fetch` parameter serialisation (encoded), never by string concatenation |
| `localStorage` | untrusted (another script on the origin, or the user) | `try/catch`, Zod validation, discard on failure |
| Clipboard | write-only, user-initiated | copy buttons only; no read, no paste handling |

### 10.2 Display hygiene (`shared/text.ts (cc)`)

`displayText(s, max = 200)`: first line only; removes C0/C1 control characters and the bidirectional override/isolate characters (U+202A-202E, U+2066-2069) that make "Trojan Source" style spoofing possible in a list of repository summaries, authors and commit subjects; truncates by **grapheme cluster** (never splits an emoji family) and appends one ellipsis; the full original is available in the `title` attribute (text only). Tested with an RLO character, NUL, a 300-character string and a ZWJ emoji sequence. The test source contains the characters only as `\u` escapes (a literal bidi character in the source would defeat the purpose and trips ESLint `no-irregular-whitespace`).

### 10.3 Links

* `ExternalLink`: `target="_blank" rel="noopener noreferrer"`, an external-link icon (`aria-hidden`) and a visually hidden "(opens in a new tab)". Referrer policy is the document's `strict-origin-when-cross-origin`; `noreferrer` drops it entirely for these links.
* No `target="_blank"` link is ever built without `rel`. ESLint (`react/jsx-no-target-blank` with `enforceDynamicLinks`) enforces it.
* Server-answered same-origin URLs (`/badges/...`, `/repos/{repo}/readme.md`, SBOM) are plain anchors/images from `lib/routes.ts` builders that percent-encode each segment (`encodeURIComponent`), so a slug can never inject a path segment, query or fragment.
* `DocContent` only calls `router.navigate({ href })` for hrefs that appear in `Doc.links` with `kind: "doc"` **and** start with a single `/`; `//host` and `http:` never navigate in-app.

### 10.4 Content Security Policy, Trusted Types and the worker (verified 2026-10-14)

The architecture's document CSP (11.3) is static: `default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; require-trusted-types-for 'script'; trusted-types scp-html`. Findings of this unit, run against that exact string in Chromium 153.0.8010.12 (Playwright 1.63.0 headless shell, SwiftShader WebGL) with `csp-probe/` (companion directory), 2026-10-14:

1. **`new Worker(url)` is a Trusted Types sink.** With a plain string or `URL`, the constructor throws `TypeError: Failed to construct 'Worker': This document requires 'TrustedScriptURL' assignment` and a `require-trusted-types-for` violation is reported. The layout worker therefore needs a `TrustedScriptURL` from a policy, and because the CSP lists only the name `scp-html`, **the single policy must provide both `createHTML` and `createScriptURL`** (a second `createPolicy("scp-html")` throws). `lib/trusted-types.ts (cc)` does this; `createScriptURL` accepts only same-origin URLs whose path starts with `/assets/` (the hashed bundle directory) and throws `TypeError` otherwise. Unit 17's `SafeHtml` currently creates the policy itself at module load; it must import `toTrustedHtml` from this module instead (T-18-005). Without that change the graph is dead on arrival in production although it works in `vite dev`.
2. With the policy in place, in the same probe, **zero violations**: module worker from `/assets/`, the whole sigma 3.0.3 + graphology graph (WebGL canvas, `ResizeObserver`, camera, reducers), the tokens stylesheet, and `getComputedStyle` palette reads. Sigma sets its canvas sizes through CSSOM (`element.style.x = ...`), which `style-src 'self'` allows; **inline `style="..."` attributes in markup are blocked**, so the rule for this unit is: never render a `style` attribute string and never `setAttribute("style", ...)`; React `style={{...}}` objects are applied through CSSOM and are allowed (used for the language bar's grid columns and the spread bar widths).
3. `worker-src` is not set; it falls back through `child-src` to `script-src 'self'`, which the probe confirms is enough. No `worker-src` directive is needed.
4. The document CSP does not apply `trusted-types` to the worker's own scope (a worker takes its CSP from its response); the worker script response is a static asset and carries none, which is fine because it uses no DOM sinks.

### 10.5 The one write action

`POST /api/v1/admin/jobs/retry`: only from the Status page on the admin listener; the key is echoed from the server's own `ParkedJob.key` (never typed by the user, never built by the UI) and must pass `canRetry`; the request is `application/json` with `X-SCP-Request: 1` (the server rejects anything else, so a cross-site form post cannot trigger it); a confirmation dialog shows the key before sending; the admin listener is network-restricted and optionally bearer-authenticated by the gateway (the SPA stores and sends no credentials). No other mutating request exists in the SPA.

### 10.6 Privacy and third parties

No third-party request of any kind (CSP `connect-src 'self'`, `img-src 'self'`, `font-src 'self'`): avatars are initials, fonts are system fonts, badges are same-origin SVG, icons are inline SVG. No cookies, no analytics, no fingerprinting. The palette keeps queries in the browser; only the package lookup sends the typed text (same origin). Private repositories never reach the SPA unless the viewer is allowed (visibility enforcement is server-side, architecture 17.3); the SPA adds no client-side access logic and assumes none.

### 10.7 Supply chain

Dependencies are exactly the pins of `10-architecture.md` appendix A and unit 17's `package.json` (lockfile committed, `pnpm install --frozen-lockfile`, seven-day release cooldown, build scripts disabled except an allow-list). This unit adds `sigma`, `graphology`, `d3-force`, their types, `graphology-types` (dev) and, in v1.1, nothing. Every shipped `script`/`link` is first party (`/assets/*`); `size-limit` fails the build when a budget (section 4.9.4, architecture 12.3) is exceeded.

---

## 11. Requirement and guardrail trace (every item assigned to this unit, with where it is met and what proves it)

### 11.1 Features

| Feature | How it is met | Section | Proof |
|---|---|---|---|
| Root redirect and landing page | `/` replaces to `/dashboard` (unit 17 `index-redirect.tsx`); the dashboard is the landing page | 4.2 | E18-DASH-01 asserts `/` ends at `/dashboard` |
| Dashboard, one card per group | `GroupCard` in configuration order, description from configuration, default repository, count, badges, links | 4.2 | C18-DASH-01..04, E18-DASH-01 |
| Latest commits panel and live refresh | `LatestCommits` panel (10), refreshed by snapshot polling, visible "as of" | 4.2, 4.1.4 | E18-DASH-04, C18-COM-03 |
| Group page (repository table, latest commits) | filters, server-side sort, table and cards, default repository, commits panel | 4.3 | C18-GRP-*, E18-GRP-* |
| Commit log, all repositories | `/commits`, day groups, cursor paging, filter | 4.4 | U18-COM-*, E18-COM-* |
| Commit log per group | `/groups/$groupId/commits` through `CommitLog` | 4.4 | E18-COM-03 |
| Commit log per repository (default branch), first line capped | `/repos/$repo/commits`; `displayText` first line, 200 graphemes | 4.4 | E18-COM-01, U18-COM-03 |
| README and contents viewer | overview README panel, document viewer, every `DocState` has a page, GitHub fallback links | 4.5, 4.6 | C18-REPO-01, E18-DOC-* |
| Configured Markdown pages | `/pages/$slug` renders configured Markdown pages | 4.11 | E18-PAGE-* |
| Shared layout, navigation, header status | the shell is unit 17; this unit supplies `PageHeader`, breadcrumbs, per-page freshness; required `title` prop | 2.3, 4.1.3 | type check; heading outline snapshots |
| Dark and light theme | tokens in both themes with contrast gate; toggle is unit 17 | 3.5 | contrast Vitest gate; axe in both themes |
| Site announcement | no banner component; the plain-text announcement is unit 17's | 4.11 | E18-BRAND-01 (no hard-coded brand strings or third-party hosts in the bundle) |
| Not-found and error pages | `ProblemPage` for every problem kind, root not-found, request id, real status from the server | 4.14 | E18-*-04 per page, errors.spec.ts |
| Syntax highlighting | server chroma classes; theme-aware CSS variables; no browser highlighter | 4.6 | E18-DOC-05 (no third-party request; code readable in dark) |
| Static assets, favicon, robots | `webapp` (unit 17/16); this unit adds no asset beyond inline SVG | 10.6 | CSP gate |
| Header status: updated time, repository count | shell from `Site.lastFullSync`, `repoCount`; pages add freshness | 4.1.5 | E18-DASH-03 |
| Repository-count badge | group badge (kind `repos`) first in `cardBadges`; same-origin SVG with `alt` | 4.2 | U (cc) `cardBadges`, C18-DASH-01 |
| Markdown rendering | server; SPA only places `html` through `SafeHtml` | 4.6 | C18-DOC-03 |
| AsciiDoc rendering (GitHub-rendered bridge) | `renderedBy: "github"` note; same placement path | 4.5, 4.6 | E18-REPO-02 |
| Latest-commit count limits | `DASHBOARD_COMMITS` (10, configurable via D-18-03), group panel 10, log paged 50, `commits.latest_n` bounds the head | 4.2, 6.1 | C18-DASH-*, E18-DASH-04 |
| N-01 Package manifest intelligence | repository dependencies, organisation graph/table/versions/licences, package page | 4.7-4.10 | U18-ORG-*, U18-GRF-*, E18-DEP-*, E18-GRF-* |
| Dependency "last seen" | status page job and integration freshness | 4.12 | E18-STA-01 |
| Build info | status build card; `Site.build` in the shell | 4.12 | E18-STA-01 |
| Freshness model | `Freshness` shown everywhere it exists, open enum normalised | 4.1.5 | U (cc) freshness tests |
| Readiness gating | `not-ready` auto-retry page; status readiness card | 4.14, 4.12 | E18-ERR-02 |
| Stale-while-error with visible staleness | `StaleBanner`, `FreshnessBadge`, data stays on failed refresh | 4.1.2, 4.1.5 | E18-DASH-03, E18-REPO-05 |
| Admin listener | `/status` exists only there; retry guarded | 4.12, 10.5 | E18-STA-01/02 |
| Route table with named routes and a crawl test | typed `<Link>`s, no URL literals (ESLint), `lib/routes.ts` builders, link crawl | 4.1.8 | E18-CRAWL-01 |
| Licence and release data (V-19: the first draft showed both without tracing them) | `stats.license` (SPDX id) in the repository aside, `stats.latestRelease` (tag as `ExternalLink` to `release.url`) in the group table column "Release" and the aside; null fields render "-", never "null" | 4.3, 4.5 | C18-GRP-02, E18-REPO-01 |
| Jenkins and Snyk status (optional integrations) | `Repo.statuses[]` as `StatusList` (one item per provider, `ProviderState` text + counts, `detailUrl` only when `https:`) and badges of kind `build` / `security` in the group "Status" column; absent when not configured | 4.3, 4.5 | C18-REPO-02 |
| Issues, last-commit, release, licence badges and the fallback "unknown" badge | `BadgeStrip` renders the server's same-origin `/badges/...` SVGs with `alt = badge.label`; the fallback body is the server's (unit 09), the SPA never substitutes an image | 4.2, 4.5 | C18-SH-02 |
| Liveness and readiness probes | the SPA never calls `/livez` or `/readyz`; the operator view is the admin-listener Status page | 4.12 | E18-STA-01/02 |
| Visibility policy | enforced server-side; the SPA has no client-side access logic and shows `forbidden`/`unauthorized` problem states | 10.6 | E18-STA-02, C18-SH-05 |
| Frontend asset pipeline | **not owned** (units 16/17); this unit's contribution is one checked-in `tokens.css` (no Sass, no committed build output) and the size budgets of the graph chunks | 3.5, 4.9.4 | contrast gate, `size-limit` |
| `llms.txt` | server-generated (unit 15); the group and repository pages link to `/groups/{id}/llms.txt`, `llms-full.txt`, `dependencies.md` and `/repos/{repo}/readme.md` through "For agents" and "Raw README" (server URLs from `lib/routes.ts`) | 4.3, 4.5 | E18-CRAWL-01 (server routes answer 200) |

### 11.2 Design guardrails (`02-design-guardrails.md`)

| GR | Guardrail (as it applies to this unit) | How it is met | Section | Proof |
|---|---|---|---|---|
| GR-02 | readiness is gated on data; the user is told when it is not there yet | `not-ready` problem page with automatic retry (V-19); the shell's warming banner is unit 17's | 4.14 | E18-ERR-02 |
| GR-04 | nothing ships half-built: no stub columns or "in development" placeholders | a column ships only with data behind it; the group table has no branches column | 4.3 | C18-GRP-02 |
| GR-05 | names tell the truth | the header status labels come from `Site.repoCount`/`lastFullSync` (unit 17), named for what they hold | 3.2 | unit tests |
| GR-07 | every entity has its own identity: a commit is `repo@sha` | React list keys are `repo@sha`, never the sha alone (a fork or mirror shares shas); **the "N new commits" pill compares `repo@sha` too** (V-06: the first draft's `countNewer` compared shas, which under-counts in the all-repositories log) | 4.4 | C18-COM-05 (two repositories, same sha, both rows render), U18-COM-02 (`countNewer` with a mirror) |
| GR-08 | multi-group membership is modelled fully and independently of order | the repository page lists **all** `groups[]`; no "first group wins" data logic (breadcrumb uses the first only as navigation, labelled) | 4.5 | C18-REPO-06 (a repository in two groups shows both links) |
| GR-11 | view types are distinct from domain and wire types; nothing prints "null" | explicit view models (`CardState`, `DayGroup`, `ProblemView`), `displayText`; required `PageHeader.title`; one `Site` query for the shell; freshness is read from payloads that always carry it | 3.2 | type check; unit tests; E18-DASH-03, E18-TXT-01 |
| GR-12 | configuration is authoritative | the configured group description (`GroupSummary.description`) wins; falls back to the repository's | 4.2 | C18-DASH-03 |
| GR-13 | credentials never leave headers | the SPA holds no credentials and logs no URLs or tokens | 10.6 | console gate |
| GR-14 | defaults are neutral; branding and limits are configuration | named constants, optional config (D-18-03); title, nav and announcement from `Site`; no hard-coded brand strings | 4.2, 6.1 | E18-BRAND-01 |
| GR-15 | one route table; links are generated, escaped and crawled | typed `Link`, TanStack Router with typed params and Zod search, `lib/routes.ts` builders, ESLint literal ban; slugs validated, splat segments `^[A-Za-z0-9._-]+$`, purls in the query, branch never in a URL (default branch only, Q-18), builders encode each segment; unknown shape is the server's 404 | 3.1, 4.1.8 | router-probe round trips (cc), E18-CRAWL-01, E18-PKG-01, E18-ERR-01 |
| GR-16 | HTTP responses are truthful | `ProblemPage` for each kind, real server status for unknown shapes; the UI treats status codes as meaning what HTTP says (`not-ready` is 503 and retried, 4xx not retried); problem `detail` is never the message | 4.1.6, 4.14 | errors.spec.ts, U (cc) `problemView`, E18-GRP-03/04 |
| GR-17 | the management plane is separate | `/status` and retry exist on the admin listener only | 4.12 | E18-STA-02 |
| GR-18 | browser security policy is strict | no inline handler or style attribute (React `style` objects are CSSOM and allowed, 10.4), no `setAttribute("style")`, first-party assets only, pinned dependencies, same-origin only (`connect-src 'self'`, no `fetch` to other origins); the CSP gate fails the build | 10.4, 10.6 | CSP gate on every e2e, E18-SEC-01/02 |
| GR-19 | one reproducible asset build; only its output is served | one Vite pipeline (unit 17), one embedded `dist`, no templates, no source maps referenced or shipped; tokens are one checked-in CSS file generated by a script, committed as source, not build output; no Sass; no raster images in this unit (inline SVG icons; favicon and manifest are unit 17's) | 3.5 | build gate and verify-dist (unit 17), bundle inspection |
| GR-26 | freshness is data | `Freshness` rendered wherever a payload carries it | 4.1.5 | U (cc) freshness tests |
| GR-28 | the read model is a snapshot with write-time indexes | not a browser concern; the SPA's own work is linear in rows shown (progressive rendering, O(n) palette ranking, O(degree) highlight) | 4.9.4 | benchmarks 4.9.4; `scp.list.render` |
| GR-29 | absence and unsupported input are typed states | every `DocState` has a designed panel (`no_content`, `too_large`, `unsupported`, `not_resident`); a group whose default repository is missing or unset keeps a degraded card | 4.2, 4.5 | C18-REPO-01, C18-DASH-02 |
| GR-32 | trusted HTML is a type minted only by the sanitiser | server sanitiser + single `SafeHtml` + Trusted Types + CSP; ESLint forbids `dangerouslySetInnerHTML` elsewhere | 4.6, 10.4 | C18-DOC-03, E18-PAGE-03, E18-DOC-05 |
| GR-33 | badges are rendered locally | badges are same-origin server SVG; a badge the server cannot render is the server's grey SVG; `BadgeStrip` renders no `<img>` when `badges` is empty; provider status is structured data rendered as text and icons | 4.2, 4.5 | C18-SH-02, C18-REPO-02 |
| GR-34 | untrusted documents are parsed safely | not applicable to the SPA: it parses no XML or SVG; documents arrive as sanitised HTML | 4.6 | n/a |
| GR-35 | access is an explicit policy; personal data is minimised | the SPA shows exactly what the API returns and renders `forbidden`/`unauthorized` states; the API exposes no e-mail addresses and only visible repositories; the SPA shows name and login only and stores none | 10.6 | E18-STA-*; server tests of units 08/09/14 |
| GR-36 | no embedded interpreters | rendering is the server's; the browser runs no document renderer or highlighter | 4.6 | n/a |
| GR-37 | document rendering is faithful | every `DocState` has a message and a GitHub link; server rewrites relative links and images, `DocContent` navigates doc links in-app and leaves the rest native; server highlighting with CSS variables per theme | 4.5, 4.6 | C18-REPO-01, U18-DOC-02, E18-DOC-02, E18-DOC-05 |
| GR-38 | times are stored in UTC and shown with an explicit zone | `Commit.committedAt` is required by the contract; `groupByDay` takes an explicit zone; site zone formatting, `<time datetime>`, tooltip with the zone; null author handled; DST goldens | 4.1.7, 4.4 | U18-COM-01/03, E18-COM-01 |
| GR-39 | the frontend is accessible and purpose-written | 4.15, axe matrix, `jsx-a11y`, manual screen-reader pass; `alt` from `Badge.label`, `AuthorAvatar` never renders an image, `displayText` returns `""` for null; inline SVG (lucide), Tailwind purge, tokens only, tokens gate | 4.15 | axe on every route; C18-COM-01 |
| GR-40 | live updates respect visibility and show staleness | visibility-aware polling (unit 17, status page), JSON only, errors shown by `ProblemAlert`, freshness visible everywhere | 4.1.5 | E18-STA-04, E18-DASH-04 |
| GR-41 | health derives from outcomes; probes are local | the Status page renders what the server computed (gates, coverage, ages, readiness separately from liveness) and never derives health from client facts; the UI never calls `/livez` | 4.12 | U18-STA-01, E18-STA-01 |

---

## 12. Test plan

### 12.1 Layers

| Layer | Tooling (pins as unit 17) | What it covers | Where |
|---|---|---|---|
| Unit | Vitest 5; the companion config uses the `node` environment and the few tests that touch `window` carry `// @vitest-environment jsdom`; unit 17's `unit` project runs everything in jsdom, which is why `contrast.test.ts` carries `// @vitest-environment node` (V-08) | all pure logic marked `(cc)`: day grouping, search params, docs ids/links/tree, graph model/layout/reducers/tree/spread, status model, search ranking, display text, avatars, freshness, problem mapping, sort tokens, contrast of tokens, TanStack Router probes | `*.test.ts(x)` next to the file; **133 tests exist and pass** (12.8) |
| Component | Vitest 5 + jsdom 30 + Testing Library 16 + user-event 14; focus, layout and WebGL cases in Vitest browser mode (`@vitest/browser-playwright`, `vitest-browser-react`) | each component's states (4.x tables), keyboard behaviour, live-region text, no-network assertions | `*.test.tsx` |
| API contract | MSW 3 with `openapi-msw` handlers typed from the contract; `onUnhandledRequest: "error"` | the exact requests a page makes (path, query, headers), error mappings | `src/test/handlers.ts` |
| End to end | Playwright 1.63 against the real `-tags spa` binary and `cmd/fakegithub`; **stub mode** (`page.route` fulfilling typed fixtures) for cases the fake world cannot produce (stale, errors, 1,000 nodes, renamed repository, large lists) | journeys, URL state, redirects, real headers and CSP, performance budgets | `web/e2e/*.spec.ts` |
| Accessibility | `@axe-core/playwright` 4.13.0 | every route x light/dark x desktop/phone, plus open dialogs | `a11y.spec.ts` |
| Security probe | Playwright + the production CSP | Trusted Types, worker, WebGL under the real policy | `csp-probe/` (run once, 10.4) and E18-GRF-05 on every CI run |

### 12.2 Determinism rules

Fixed clock (`page.clock.install({ time: "2026-10-14T10:00:00Z" })` in e2e, `vi.useFakeTimers` + `setSystemTime` in component tests); fixed zone (`timezoneId: "Europe/Oslo"`, and `TZ=Europe/Oslo` for Vitest) and locale `en-GB`; `reducedMotion: "reduce"` except in tests that assert the opposite; no network in unit/component tests (MSW errors on an unhandled request); layout is seeded (byte-deterministic); system fonts only (no font loading races); randomness injected (`retryDelayMs(attempt, retryAfter, rnd)`); no `waitForTimeout` in e2e (web-first assertions and `expect.poll`); screenshots are not used as oracles in v1 (cross-platform rendering noise; the contrast gate and axe carry the visual-regression load for colour).

### 12.3 Fixtures

`src/test/fixtures.ts (cc)` copies the `x-scp-fixtures` values of the contract into **typed** constants (`components["schemas"][...]`): if the schema drifts, the file stops compiling (unit 07 additionally validates the examples against the schemas). `src/test/synthetic-graph.ts (cc)` is the research benchmark generator (1,000 nodes in 20 clusters, 2,500 edges, 80 % intra-cluster, seed 42). `handlers.ts` builds the MSW handlers from the same constants.

### 12.4 The e2e world (real mode)

Real-mode specs run against `scp serve --config deploy/dev.yaml` talking to `cmd/fakegithub` with the demo world of design 05 section 7 (`acme`: `payments-api` public, Java, 3 open issues, release `v1.0.0`, `pom.xml`, four commits by Ada Lovelace, Grace Hopper, `dependabot[bot]` with no user and one with no commit author block, docs `docs/overview.md` and `docs/architecture.md`, `CHANGELOG.md`; `docs-site` with an AsciiDoc README; `archived-tool` private and archived, hidden under `public_only`). Group ids are read from the API, never hard-coded, because `deploy/dev.yaml` is unit 16's. Admin-listener specs use `SCP_E2E_ADMIN_URL` (default `http://127.0.0.1:9091`; a second environment variable owned by this unit). CI order: `pnpm gen` drift check, `pnpm test` (unit, component), build, start binary and fake, wait for `/readyz`, `pnpm e2e`.

### 12.5 Cross-cutting specs (beyond the per-page tables)

| Id | Assertion |
|---|---|
| E18-CRAWL-01 | Starting at `/dashboard` the crawler follows every same-origin `a[href]` breadth-first (cap 300 pages, depth 4): each navigation ends 200 (or the documented 308 to a 200), has exactly one `h1`, no console error, no CSP violation; every link is either a typed SPA route (manifest shape) or in the server-route allow-list (badge, media, `.md`, `llms.txt`, SBOM), and the server routes answer 200 to `HEAD`/`GET`; generated text (README links) is crawled too (GR-15) |
| E18-BRAND-01 | the built `dist/` contains none of the third-party asset hosts `cdnjs`, `unpkg`, `jsdelivr`, `rawgit`, `fonts.googleapis` and none of the dev configuration's site title, organisation name or page titles (branding arrives from `Site` at runtime, GR-14) |
| E18-ERR-01 | `/nope/never`: real 404 status with the shell body, `ProblemPage` "Page not found", focus on the heading, search button opens the palette |
| E18-ERR-02 | stub `503 not-ready` with `Retry-After: 2`: page shows the starting-up message, retries automatically, loads when the stub flips to 200 |
| E18-ERR-03 | stub network failure (`route.abort`): offline page, recovers on `online` event |
| E18-ERR-04 | stub `X-SCP-Build` mismatch: unit 17's banner appears; pages stay usable |
| E18-A11Y-01 | axe matrix (4.15.3) |
| E18-A11Y-02 | at 320 CSS px and 400 % zoom no horizontal page scroll except inside code blocks, tables and the graph |
| E18-A11Y-03 | `forcedColors: "active"` smoke: badges and chips keep borders, axe passes |
| E18-A11Y-04 | every `button, a` in `main` that is not inline text has a bounding box >= 24x24 px |
| E18-SEC-01 | no request leaves the origin on any crawled page (Playwright `request` listener) |
| E18-TXT-01 | on every crawled page, `main` contains none of the visible texts `null`, `undefined`, `NaN`, `[object Object]` and no `<img src="">`/`href=""` (GR-11, GR-39: a missing value must never print as "Updated: null" or an empty `src`; the first draft had no test for these literals, V-20) |
| E18-SEC-02 | CSP header on the document equals the golden string (unit 17 golden) and the whole suite ran with it enforced |

### 12.6 Performance budgets as tests

| Budget | Value | Test |
|---|---|---|
| `/dashboard` initial JS (gz) | <= 200 KB | unit 17 `budget.ts` (measured by that unit) |
| lazy route chunk (gz) | <= 60 KB each | `budget.ts` |
| graph chunk / layout worker (gz) | <= 70 KB / <= 15 KB | `budget.ts` (39.8 KB / 6.0 KB measured) |
| `scp.route.ready`, dashboard, warm cache | <= 1,000 ms on CI hardware | E18-DASH-07 (informational in v1, hard at M7; the first draft cited E18-DASH-06, which is the axe test) |
| document render, 500 KB html | `scp.docs.render` <= 100 ms | stub test |
| 1,000-row group table | first 100 rows committed <= 300 ms, no long task > 200 ms | stub test |
| graph 1,000 nodes / 2,500 edges | layout <= 1.5 s (worker), no long task > 100 ms during layout; mount <= 600 ms unthrottled on CI software WebGL (<= 100 ms on a real GPU, informational); at 6x CPU throttle mount <= 1 s and one long task <= 250 ms **with software WebGL** (measured below) | E18-GRF-01 |

Measured on 2026-10-14 (Chromium 153.0.8010.12 headless shell, Apple silicon, SwiftShader software WebGL, the production CSP, five runs each, `csp-probe/`): worker layout 785-817 ms; `setData` 12 ms; mount to second frame 29-31 ms (no long tasks); with 6x CPU throttle `setData` 73-79 ms, mount 418-466 ms, long tasks of 50-55 ms and 123-140 ms (first frame, shader compile and draw on a software GL). Research E6 (real GPU, 6x throttle): mount 79 ms, 8.2 ms frames. Budgets therefore state the software-GL numbers for CI and the GPU numbers as the product expectation.

### 12.7 Component test inventory by page (ids in the page tables)

Dashboard C18-DASH-01..04; Group C18-GRP-01..04; Commits C18-COM-01..05; Repository C18-REPO-01..06; Documents C18-DOC-01..04; Dependencies C18-DEP-01..02, C18-ORG-01..03; Graph C18-GRF-01..03; Package C18-PKG-01..02; Pages C18-PAGE-01..02; Status C18-STA-01..03; Search C18-SRCH-01..02; shared components: C18-SH-01 `ExternalLink` (non-https renders text, rel set), C18-SH-02 `BadgeStrip` (alt, same-origin guard, max), C18-SH-03 `AuthorAvatar` (initials, colour index stable, `aria-hidden`), C18-SH-04 `StaleBanner` (nothing for fresh/unknown, status role, cause text), C18-SH-05 `ProblemPage` (each kind, focus on `h1`, countdown `aria-hidden`), C18-SH-06 `PageHeader` (one `h1`, breadcrumb nav), C18-SH-07 `DataTable` (cc server render, keyboard sort), C18-SH-08 `enhanceCodeBlocks` (idempotent, cleanup).

### 12.8 Current status of the executable part

`20-design/18-web-pages.assets/`: 14 test files, **145 tests, all passing**, `tsc` 7.0.2 and 6.0.3 clean (0.2; 11 files and 133 tests before the verification). The pure-logic modules are at full statement coverage by construction of their tests (every exported function has a table test); the React components are specified, not yet written, and carry the component and e2e ids above.

---

## 13. Implementation tasks (ordered milestones)

Dependencies are on other units' deliverables ("17 M1" = unit 17's shell and router wired; "07" = generated `schema.d.ts`; "fake" = `cmd/fakegithub` and `deploy/dev.yaml` runnable). Every task ends with its tests green and `pnpm lint` clean.

### M0 Foundations agreed and installed (needs 17 M1, 07)

| Task | Description | Depends on | Done when |
|---|---|---|---|
| T-18-001 | Add `sigma@3.0.3`, `graphology@0.26.0`, `d3-force@3.0.0`, `@types/d3-force@3.0.10`, dev `graphology-types@0.24.8`; add `size-limit` entries `graph` (70 KB) and `layout-worker` (15 KB) | 17 package.json | install and budget script green |
| T-18-002 | `pnpm dlx shadcn@4.21.x add checkbox combobox native-select button-group` (Base UI style; V-15: `native-select` and `button-group` are used by the group, dependency and graph pages but are in neither unit 17's installed list nor the first draft's "+" list), CSP patches as in 17 | 17 components | components in `components/ui`, axe clean in a Storybook-free smoke page |
| T-18-003 | Import `styles/tokens.css (cc)` after unit 17's placeholder tokens; add the contrast Vitest gate; wire `avatar-1..8`; add `html { scroll-padding-top: 4.5rem }` (sticky header must not obscure focus, WCAG 2.4.11) | 17 `index.css` | contrast gate runs in `pnpm test`; 0 failures |
| T-18-004 | `shared/problem-view.ts (cc)` and `ProblemPage`; make unit 17's `ProblemAlert` use `problemView`; message catalogue ids `problem.*` | 17 `problem-alert.tsx` | one mapping table; C18-SH-05 green |
| T-18-005 | Move the Trusted Types policy to `lib/trusted-types.ts (cc)` (HTML + script URL); `SafeHtml` imports it **and memoises `{ __html }` on the html string (V-02)**; CSP golden test includes a worker | 17 `safe-html.tsx`, `webapp` CSP golden | probe reproduced in CI (E18-GRF-05) |
| T-18-006 | Search helper for arrays (`z.preprocess`) behind OQ-18-04's decision; `STORAGE` keys `scp-recent-search`, `scp-graph-mode`; router `defaultPendingMs: 150`, `defaultPendingMinMs: 300` | 17 router/query | decision recorded |
| T-18-007 | Shared components with tests: `PageHeader`, `EmptyState`, `StaleBanner`, `ExternalLink` (only the additive `className` prop of unit 17's component, V-16), `BadgeStrip`, `AuthorAvatar`, `CommitRow`, `Stat`, `useMediaQuery`, `code-blocks.ts`, `DataTable (cc)` | T-18-003, 17 `components/ui` | C18-SH-01..08 green |
| T-18-008 | `lib/routes.ts` builders: `sbom`, `dependenciesMd`, `llmsRepo`, `llmsGroup`, `llmsFull`; `lib/perf.ts` | 17 `lib/routes.ts` | Go-side equality test (unit 06) lists them |
| T-18-009 | `src/test/handlers.ts` (MSW + openapi-msw) and e2e stub helpers over `fixtures.ts (cc)`; e2e `SCP_E2E_ADMIN_URL` | 07, 17 e2e fixtures | a smoke spec reads a typed stub |

### M1 Dashboard and commits (needs 08, 09 APIs, fake)

| Task | Description | Depends on | Done when |
|---|---|---|---|
| T-18-010 | Dashboard route: `GroupCard`, filter, degraded card, states, `LatestCommits` panel | T-18-007 | C18-DASH-*, E18-DASH-* |
| T-18-011 | `CommitLog`, `NewCommitsPill`, load more, day groups; routes `/commits`, `/repos/$repo/commits`, `/groups/$groupId/commits` | T-18-007, 09 | C18-COM-*, E18-COM-* |

### M2 Group page (needs 08)

| Task | Description | Depends on | Done when |
|---|---|---|---|
| T-18-020 | Group header, default repository card, repository table, cards, filters (popovers, sheet), chips, counts | T-18-002, T-18-007 | C18-GRP-*, E18-GRP-* |

### M3 Repository and documents (needs 10, 14)

| Task | Description | Depends on | Done when |
|---|---|---|---|
| T-18-030 | `RepoLayout` (loader, canonical slug, header, tabs), overview, aside sections (about, statuses, technology, relations, commits) | T-18-007 | C18-REPO-*, E18-REPO-* |
| T-18-031 | README panel states, `DocContent` wiring, descriptor alert | T-18-030, 10 | C18-REPO-01, E18-REPO-02 |
| T-18-032 | Document page: nav tree, filter, TOC with scroll-spy, prev/next, states; extension-less URLs (D-18-01) | T-18-030, 10 | U18-DOC-*, C18-DOC-*, E18-DOC-* |

### M4 Pages, status, search (needs 10, 15, 06)

| Task | Description | Depends on | Done when |
|---|---|---|---|
| T-18-040 | Pages route and TOC | T-18-007 | E18-PAGE-* |
| T-18-041 | Status page, cards, retry dialog; independence from site/snapshot (D-18-09); public-listener `/status` renders the root not-found content | 15, 06 | E18-STA-* on the admin listener |
| T-18-042 | Search palette and sources; shell trigger contract with 17 | T-18-010, T-18-020, T-18-030, T-18-040 | C18-SRCH-*, E18-SRCH-* |

### M5 Dependencies (needs 11-13 APIs)

| Task | Description | Depends on | Done when |
|---|---|---|---|
| T-18-050 | Repository dependencies page (units, filters, table, diagnostics, downloads) | T-18-030 | C18-DEP-*, E18-DEP-* |
| T-18-051 | Organisation page chrome, Tabs, table view with cycles panel | T-18-007 | C18-ORG-01 |
| T-18-052 | Versions and licences views | T-18-051 | C18-ORG-02/03, E18-ORG-* |
| T-18-053 | Package page | T-18-051 | C18-PKG-*, E18-PKG-* |

### M6 Graph (needs T-18-001, T-18-005, T-18-051)

| Task | Description | Depends on | Done when |
|---|---|---|---|
| T-18-060 | Graph view: toolbar, canvas wrapper, legend, node panel, outline, finder, WebGL fallback | T-18-051 | C18-GRF-*, E18-GRF-02/03/04 |
| T-18-061 | Performance harness (`scp.graph.*` measures, CDP throttling, long-task observer) and budgets | T-18-060 | E18-GRF-01 green with the numbers of 12.6 |
| T-18-062 | CSP probe in CI against the real binary | T-18-005, 16 | E18-GRF-05 |

### M7 Hardening

| Task | Description | Depends on | Done when |
|---|---|---|---|
| T-18-070 | Error matrix (`ProblemPage` kinds, auto-retry), root not-found/error, `errors.spec.ts` | T-18-004 | E18-ERR-* |
| T-18-071 | Accessibility matrix, 320 px reflow, forced colours, target sizes, manual screen-reader pass recorded | all pages | E18-A11Y-* |
| T-18-072 | Crawl, brand grep, third-party request guard | all pages | E18-CRAWL-01, E18-BRAND-01, E18-SEC-* |
| T-18-073 | Bundle budgets verified, message catalogue extraction, ESLint boundary matrix (`dashboard` -> `groups`, `commits`; `groups` -> `commits`) | 17 | CI green |
| T-18-074 | Flip the CSP from Report-Only to enforced for this unit's routes (ADR-010 rollout) | 16 | zero violations across the whole suite for 5 consecutive runs |

### M8 v1.1 hooks (not v1)

SSE-driven invalidation uses the existing keys (no code here beyond testing the topic map); Mermaid iframe in `enhanceCodeBlocks`; `/avatars/{login}` proxy swap inside `AuthorAvatar`; server-side commit author/text filters (D-18-02) and palette full-text if OQ-18-06 says yes; re-evaluate sigma 4 after its minor releases (research measured +68 KB gz, rewrite, WebGL 2 only).

---

## 14. Deviations proposed

| Id | What deviates from, or adds to, the architecture/contract | Why | Affects | Needs |
|---|---|---|---|---|
| D-18-01 | **Document `uiUrl` is extension-less**: `/repos/{repo}/docs/{path-without-extension}`; in-document `kind: doc` hrefs the same. The SPA accepts the path with extension and canonicalises. The contract fixtures (unit 07 `getRepo`/`listRepoDocs`) currently put `uiUrl` equal to `markdownUrl` (`.../docs/docs/install.md`) | architecture 11.2 makes `.md`/`.adoc` suffixed paths the raw route; one URL cannot be both the page and the raw file | 07 fixtures and schema description, 10 link rewriting (`kind: doc` hrefs), 06 negotiation tests | unit 07 and 10 agree; until then the SPA copes with both |
| D-18-02 | Additive, optional `author` and `q` query parameters on `listCommits` (v1.1) | the v1 commit filter only searches loaded rows | 09, 07 | owner decision (OQ-18-05) |
| D-18-03 | Additive config key `site.dashboard_commits` and `Site.dashboardCommits` (integer 1..50, default 10) | a hard-coded feed length cannot be tuned by an operator (GR-14); commit limits are meant to be configurable | 15 (`site:` block), 07 | accept or keep the constant |
| D-18-04 | **No `/search` route and no `/groups` index route**: search is a command palette; the dashboard is the groups index | the route table has neither; both would be 404 shapes | 17 (shell trigger) | none |
| D-18-05 | The dashboard shows **10** latest commits and group pages show 10 with a link to the full log | a short fixed feed hides activity in a busy organisation; 10 fills the panel at desktop height and the full log is one click away (GR-14) | users | owner awareness |
| D-18-06 | `shared/` is **extended** beyond the architecture list: `ProblemPage`, `StaleBanner`, `AuthorAvatar`, `CommitRow`, `BadgeStrip`, `Stat`, `code-blocks.ts`, `useMediaQuery`, `DataTable`/`columnHelper`; `ExternalLink` is unit 17's and only gains an optional `className` (V-16); unit 17's `ProblemAlert` must call `problemView`; `STORAGE` and `serverRoutes` gain entries; `qk` is used as is | components needed by several slices cannot live in one slice (import rules) | 17 | agreement before M0 |
| D-18-07 | Clarification, not a change: the repository composite (`getRepo`) is loaded by the `docs` slice's layout route (as unit 17's route table already has it) and carries what commits and manifests panels need | keeps `docs` free of imports of other slices | 17 | none |
| D-18-08 | The Trusted Types policy `scp-html` **also mints script URLs** (`createScriptURL`, same-origin `/assets/` only) and is created in exactly one module | `new Worker(url)` is a Trusted Types sink; verified blocked under the architecture's CSP; no `worker-src` change needed | 17 (`SafeHtml`), 06/16 (CSP golden: add a worker case) | T-18-005 |
| D-18-09 | The Status page **does not depend on** `/api/v1/site` or `/api/v1/snapshot` and formats times in UTC (operator convention). **Corrected by the verification (V-11):** the first draft justified this by "the admin listener serves neither (route table)"; the admin table serves both (kernel RT-14: 24 API rows incl. `getSite`, `getSnapshot`), so the shell needs no listener-specific skipping and no `useListener()` | the operator's page must work when the public-view endpoints fail, and logs are UTC | none | none (OQ-18-01 closed) |

Added by the verification (V-12, V-19): **D-18-10** the architecture's stack list names Recharts "through the shadcn chart component"; this unit uses **no chart library** (1.3; CSS bars with printed numbers), the `chart` component stays installed and unused, and the `charts` chunk budget is not consumed; **D-18-11** `commits/api.ts` carries a second `listGroups` factory (`commitGroupOptionsQuery`) with the same query key as the dashboard's, because the import matrix gives `commits` no edge to `dashboard` or `groups` (2.1); both are recorded here so a reviewer does not read them as accidents.

Not deviations but stated for the record: sigma stays at 3.0.3 although 4.0.1 exists (research decision, 0.1); `listRepoDocs` is unused by the SPA; TanStack Table 9 is used through `features`/`manualSorting` only.

---

## 15. Open questions

| Id | Question | Owner | Assumption made until answered |
|---|---|---|---|
| OQ-18-01 | ~~How does the SPA know it is on the admin listener, and how does the shell avoid `/api/v1/site` and `/api/v1/snapshot` there?~~ **Closed by the verification (V-11):** the premise was false (the admin listener serves both); no probe, no `useListener()`; the palette has no Status entry; a public-listener `/status` ends in the root not-found content | 17, 06, 15 | none needed |
| OQ-18-02 | Is the Markdown README entry (`id: readme`) always first in `repo.docs`, and may it have a `section`? | 10 | the UI puts README first regardless and ignores its `section` |
| OQ-18-03 | **CLOSED by CD-ER-01 (cross-design review, 2026-10-14): one closed snake_case vocabulary in `92-error-catalogue.md` section 3; the contract and fixtures no longer use dotted codes.** What values can `Freshness.lastErrorCode` take (the contract says string only)? **Known disagreement (V-26):** unit 07's examples are dotted (`github.rate_limited`, `github.not_found`, `parse.failed`), unit 09 emits the closed undotted set `permanent`, `not_found`, `forbidden`, `unavailable`, `error` (its own Q-10 asks the same) | 07, 04, 09 | `causeId` handles both: exact codes `not_found`, `forbidden`, `unavailable` and their `github.` forms, then prefixes (4.1.5); `permanent`, `error` and unknown values read "Unknown cause" with the raw code in the tooltip |
| OQ-18-16 | **CLOSED by CD-JOB-02: the kernel now rejects white space in key parts and the contract allows 512 bytes.** Job key grammar: kernel JB-01 allows keys up to 512 bytes whose parts may contain spaces, while the contract's `RetryJobRequest.key` has `maxLength: 300` and `pattern ^[a-z][a-z0-9._-]*/[^\s]+$` (and so has `canRetry`); a parked job with a longer key or a space cannot be retried from the UI or the API (V-28) | 01, 07 | `canRetry` mirrors the contract; real keys are `kind/source/owner/name` over GitHub-legal characters, so none is affected in practice |
| OQ-18-04 | Array search params: keep the router's JSON default (`?ecosystem=%5B%22maven%22%5D`) or install repeated keys (`?ecosystem=maven&ecosystem=npm`, same form as the API)? | 17 | JSON default; schemas are written for arrays and accept either after T-18-006 |
| OQ-18-05 | Do we want server-side commit filtering (`author`, `q`) in v1.1 (D-18-02)? | owner, 09 | client filter over loaded rows, labelled as such |
| OQ-18-06 | Is document full-text search wanted (new endpoint, indexing in the snapshot)? | owner | no; palette searches names, titles, summaries |
| OQ-18-07 | Avatars: confirm initials without network (W19) or a proxy for GitHub avatars in v1.1? | owner | initials |
| OQ-18-08 | Which group is "the" breadcrumb group for a repository in several groups, and is `RepoSummary.groups` in configuration order? | 08 | first element of `groups[]`, order assumed stable |
| OQ-18-09 | Should a client-error beacon exist (an endpoint, rate limit and privacy text)? | owner, 06 | none; request ids in the UI are the support path |
| OQ-18-10 | Locale catalogues beyond English (the user base may want Norwegian)? | owner | English only; message ids are ready |
| OQ-18-11 | Supported browser matrix? Research tested Chromium only (CSP, Trusted Types, Base UI, WebGL); Trusted Types is feature-detected and `Intl.Segmenter` needs a 2024+ browser | owner, 17 | evergreen Chromium, Firefox and Safari current and previous version; Firefox/Safari manual pass before release |
| OQ-18-12 | Should the unit table (4.8.1) offer CSV export? | owner | no |
| OQ-18-13 | Which organisation pages (about, onboarding, policies) should ship as configured pages in the first deployment? | owner | none |
| OQ-18-15 | Should `listRepos`/`listGroupRepos` return facet counts (available tags, types, lifecycles) so the tag filter can offer tags that the current filter excludes? | 08 | tag options come from the current result plus the selected tags |
| OQ-18-14 | Raw-license text, vulnerability data and "outdated against the registry" are post-v1 providers; should the Versions view reserve a column for them now? | 11-13 | no |

---

## Appendix A. shadcn/ui components used per page (Base UI style; all installed by unit 17 except the four marked +: `checkbox`, `combobox`, `native-select`, `button-group`, T-18-002)

| Page | Components |
|---|---|
| Shared | `button`, `badge`, `tooltip`, `skeleton`, `spinner`, `separator`, `breadcrumb`, `empty`, `alert`, `card`, `input`, `input-group`, `label`, `kbd`, `scroll-area`, `toast` |
| Dashboard | `card`, `input-group`, `badge`, `skeleton`, `empty`, `alert`, `tooltip` |
| Group | `card`, `table` (via `DataTable`), `popover`, `checkbox`+, `combobox`+, `native-select`+ (archived), `select` (phone sort), `sheet`, `badge`, `toggle-group`, `dropdown-menu` |
| Commits | `input-group`, `select`, `button`, `badge`, `avatar` (fallback only), `skeleton`, `empty` |
| Repository | `tabs`-styled `nav` (own markup), `collapsible`, `badge`, `alert`, `tooltip`, `progress`-free language bar (own markup), `item` for provider statuses, `card` |
| Documents | `sheet`, `collapsible`, `input`, `scroll-area`, `button`, `empty`, `alert` |
| Dependencies | `tabs`, `table`, `toggle-group`, `popover`, `checkbox`+, `combobox`+, `native-select`+ (origin), `switch`, `select`, `collapsible`, `sheet`, `progress`, `alert`, `badge`, `empty` |
| Graph | `toggle-group`, `combobox`+, `button-group`+, `sheet` (phone panel), `collapsible` (outline), `tooltip`, `badge` |
| Pages | `collapsible` (TOC on small screens), `empty` |
| Status | `card`, `table`, `progress`, `badge`, `dialog`, `toast`, `collapsible`, `alert` |
| Search | `command`, `dialog`, `kbd`, `spinner` |
| Errors | `empty`/own layout, `alert`, `button` |

`chart` stays installed by unit 17 and unused in v1 (research 08: Recharts is +97 KB gz; the bars here are CSS with their numbers printed).

## Appendix B. Route and component index

| Route | Component (export) | File |
|---|---|---|
| `/` | redirect | unit 17 `app/index-redirect.tsx` |
| `/dashboard` | `DashboardPage` | `features/dashboard/route.tsx` |
| `/groups/$groupId` | `GroupPage` | `features/groups/overview.tsx` (layout `route.tsx`) |
| `/groups/$groupId/commits` | `GroupCommitsPage` | `features/groups/commits.tsx` |
| `/commits` | `CommitsPage` | `features/commits/route.tsx` |
| `/repos/$repo` | `RepoLayout` (layout), `RepoOverview` (index) | `features/docs/route.tsx`, `readme.tsx` |
| `/repos/$repo/docs/$` | `DocPage` | `features/docs/doc.tsx` |
| `/repos/$repo/commits` | `RepoCommitsPage` | `features/commits/repo-commits.tsx` |
| `/repos/$repo/dependencies` | `RepoDependenciesPage` | `features/manifests/repo-dependencies.tsx` |
| `/dependencies` | `OrgDependenciesPage` | `features/manifests/dependencies-route.tsx` |
| `/packages` | `PackagePage` | `features/manifests/package-route.tsx` |
| `/pages/$slug` | `PageView` | `features/pages/route.tsx` |
| `/status` (admin) | `StatusPage` | `features/status/route.tsx` |
| (dialog) | `SearchPalette` | `app/search-palette.tsx` |

---

## Verification log (independent verifier)

Date: 2026-10-14. Verifier: independent of the author. Full report: `30-verification/design-18-web-pages.md`. **28 findings (5 blocker, 11 major, 12 minor): 27 fixed (one of them, V-26, only in part), 1 open (V-28).** Every finding was fixed in this document and, where the companion code was wrong, in `18-web-pages.assets/` (tests added for each behavioural fix; type-checks with TypeScript 6.0.3 and 7.0.2; 145 tests green).

### Findings

| ID | Sev | Issue | Fixed | Evidence |
|---|---|---|---|---|
| V-01 | minor | `decideClick` called `decodeURIComponent` on the hash: a heading or author link `#100%` throws `URIError` inside the click handler (the page then navigates natively and loses the in-page scroll). | yes (`safeDecode`, raw id fallback; U18-DOC-02) | `decodeURIComponent('100%')` throws in node 26; new test `a malformed percent escape in an anchor never throws` |
| V-02 | blocker | `SafeHtml` called `policy.createHTML()` in render, which returns a NEW TrustedHTML object each time; React compares `__html` by identity, so every parent re-render re-assigned `innerHTML`, destroying the Copy buttons and scroll anchors added by `DocContent`'s effect. | yes (`useMemo` on the html string; C18-DOC-02 extended) | jsdom test with a fake `trustedTypes` that returns a new object per call fails on the first-draft implementation and passes on the fixed one (`safe-html.test.tsx`) |
| V-03 | major | A non-`ProblemError` (a `TypeError` from a bug, or the graph endpoint answering a string body) was mapped to the "You are offline" page with automatic retry; a code bug looked like a network problem and retried forever. | yes (`server-error` kind; `assertGraphBody` throws `client-contract`) | `problemView(new TypeError(...)).kind === 'server-error'`; `graph body guard` tests |
| V-04 | blocker | `docs/docId.ts` rejected every path with a space or a non-ASCII letter (`docs/Release Notes.md`, `docs/Okonomi.md`) and mapped `~` back by a different rule from the contract's `DocId` (`/` -> `~`, `%XX` for every byte outside `[A-Za-z0-9._-]`). | yes (rewritten to the contract; malformed input returns null) | round-trip and rejection tests (`..`, `.`, empty, controls, lone surrogates, > 1,024); contract schema `DocId` in `07` read |
| V-05 | major | Section 4.3 promised that `lastCommit` and `pushedAt` start descending, but `nextSortToken` had no such parameter: the first click sorted ascending. | yes (`meta.defaultDesc`, `descFirst`; C18-GRP-01) | `data-table.test.tsx`: `a column whose natural order is descending starts descending` |
| V-06 | major | `countNewer` compared the sha only; a mirror or fork shares shas, so the "N new commits" pill under-counted (the `repo@sha` identity of GR-07 lost in the pill). | yes (`commitKey` = `repo@sha`) | `commits.test.ts` regression test |
| V-07 | minor | `app/search-model.ts` imported `docSplat` from `@/features/docs/docId`: a cross-feature import that bypasses `index.ts` (architecture import rule). | yes (new pure `features/docs/index.ts`) | grep over the companion tree: the only cross-slice import left in `src/` outside the slices is `test/synthetic-graph.ts` (test support, not a page import); no slice imports another slice |
| V-08 | minor | The doc claimed the companion `vitest` config could run everything in jsdom; `contrast.test.ts` reads a file by `new URL(import.meta.url)` and fails under jsdom (`URL must be of scheme file`). | yes (per-file pragmas, README, 12.1 text) | run with a jsdom default: 1 failure; reverted to node + pragmas: 145/145 |
| V-09 | blocker | `GraphController.setPalette` stored the palette but did not rewrite per-node and per-edge colours or the label colour: after a light/dark switch the graph kept the other theme. | yes (`updateEachNodeAttributes`, `updateEachEdgeAttributes`, `setSetting('labelColor')`, `refresh`; E18-GRF-06) | Chromium probe `csp-probe/palette-probe`: node colour `#111111` -> `#aa0000`, label `#eeeeee`, edge `#555555` after `setPalette(dark)` |
| V-10 | major | The layout worker could fail (thrown layout, `onerror`, 404 of the worker URL) and a WebGL context could be lost; neither was handled, so the page showed "Arranging N units" forever. | yes (`onerror`, `{type:'error'}` branch, try/catch around `new Worker`, `webglcontextlost` capture listener, probe context released; E18-GRF-07/08) | palette probe: `onContextLostCallback: true`, one `webglcontextlost` seen at the container in the capture phase |
| V-11 | major | D-18-09 / OQ-18-01 justified "Status needs no `/api/v1/site`" by "the admin listener serves neither": false. Kernel RT-14 lists 24 admin API rows including `getSite` and `getSnapshot` (the 22 public operations plus two admin ones), so no listener-specific skipping is needed. | yes (D-18-09 corrected, OQ-18-01 closed) | `01-kernel` RT-14 table and `06-httpapi` admin table read |
| V-12 | blocker | The import matrix was violated: `commits` imported `groupsQuery`, `groupQuery`, `repoQuery` from other features (an edge that does not exist, and with `dashboard -> commits` it would be a cycle). | yes (`commitGroupOptionsQuery` duplicate factory with the same key, `getRouteApi` for the layout data; D-18-11) | architecture 12 import matrix compared with every import in the companion tree |
| V-13 | blocker | Unit 17 invalidates every query on a snapshot change and refetches every active query every 120 s (`SWEEP_MS`). A live infinite commit log would have its loaded pages refetched and reordered while the reader scrolls. | yes (`follow` switch: `enabled: follow`, `FOLLOW_SCROLL_Y = 200`; pill takes over when scrolled) | TanStack Query 5 probe: a disabled infinite observer is not refetched on `invalidateQueries` and refetches when re-enabled; `commit log follow switch` tests |
| V-14 | minor | The "Documentation" tab named no target; a bare `/repos/x/docs` is not an SPA shape (the server answers 404). | yes (`docsEntry(entries)`: first non-README document; tab hidden when only the README exists) | `docs.test.ts` ordering and target test |
| V-15 | major | `native-select` and `button-group` are used by the group, dependency and graph pages but are in neither unit 17's installed list nor the first draft's additions. | yes (T-18-002, Appendix A: four `+` components) | unit 17 list compared with Appendix A |
| V-16 | minor | `ExternalLink` was listed as a new component demanding `https://`; it exists in unit 17 with `isWebUrl` (http and https). | yes (only the additive `className` prop is requested, T-18-007) | `design-17-web-foundation.md` section on `ExternalLink` |
| V-17 | major | Section 5 said panels fail alone through `PanelBoundary`, but with a plain `useQuery` the boundary never fires (the hook returns the error instead of throwing). | yes (suspense hooks required, text in 5) | TanStack Query 5 semantics; `PanelBoundary` is an error boundary (unit 17) |
| V-18 | minor | The worker lifecycle contradicted itself: "created once per mounted canvas" in 4.9 and "terminated after each response" in 8. | yes (one worker per layout run; terminated on response, supersede and unmount) | 4.9 and 8 now identical; positions are kept in a `Map` per canvas |
| V-19 | major | Trace gaps: licence and release data, security headers and inline handlers (GR-18), serving before data exists (GR-02) and the Recharts statement of the architecture were not traced. | yes (rows added to section 11, D-18-10) | `04-decisions.md` scope and `02-design-guardrails.md` re-read |
| V-20 | minor | No test for the visible literals `null`, `undefined`, `NaN`, `[object Object]`, `src=""` (GR-11, GR-39). | yes (E18-TXT-01) | the new spec fails on a fixture page that renders `Updated: null` |
| V-21 | minor | The mount-time claim (29 to 31 ms) cannot be reproduced under software WebGL; the gate would flake in CI. | yes (gate loosened to <= 600 ms / <= 1 s at 6x on software GL; real-GPU numbers informational) | Chrome 155, SwiftShader, load average about 50: mount 264 to 843 ms in repeated runs (claimed 29 to 31 ms); layout stays about 800 ms |
| V-22 | major | "Tables are wrapped by CSS so they scroll" cannot be done in CSS: the scroll region needs `tabindex` and a label only when it overflows (WCAG 2.1.1 / axe `scrollable-region-focusable`). | yes (ResizeObserver on `div.md-table`; unit 10 D-3) | unit 10 design D-3 (`div.md-table`) read |
| V-23 | minor | The search palette silently truncated the repository list when the API has more than 1,000 repositories. | yes (row "Some repositories are not listed, narrow the search" when `nextCursor` is non-null) | `ListRepos` `limit` maximum 1,000 in the contract |
| V-24 | minor | `Owner.kind = unresolved` was not specified (it must be plain text, never a link). | yes (4.6: handle as text plus "(unresolved)") | contract `Owner` schema |
| V-25 | minor | Each group table row must offer GitHub, docs and commit links; the first draft dropped the docs link without saying so. | yes (the title link is the docs link; recorded) | 4.3 row specification and wireframe re-read |
| V-26 | major | `freshness.ts` mapped `lastErrorCode` by prefix only; units 07 (dotted: `github.not_found`) and 09 (closed set) disagree, so a 404 cause read "Unknown cause". | partly: exact causes checked before prefixes and `causeId` used where present; the vocabulary disagreement itself is OQ-18-03 | `shared.test.ts` cases for both vocabularies; `07` and `09` examples compared |
| V-27 | major | Two `h1` per page: the repository header and the page heading, and every `h1` inside rendered Markdown (WCAG one-h1 rule of 4.1.3; axe `page-has-heading-one`, GR-39). | yes (`normaliseDocH1`, compact repository header has no `h1`) | `headings.test.ts` (jsdom), idempotence test |
| V-28 | minor | Job key grammar differs between the kernel (JB-01: up to 512 bytes, parts may contain spaces) and the contract (`RetryJobRequest.key` `maxLength 300`, pattern `^[a-z][a-z0-9._-]*/[^\s]+$`): a long or spaced key can be shown but not retried. | no (cross-unit; `canRetry` mirrors the contract, OQ-18-16) | `01-kernel` JB-01 and `20-design/api/openapi-admin.yaml` compared |

### Facts re-verified

| Fact | How verified (2026-10-14) | Result |
|---|---|---|
| Static CSP with `require-trusted-types-for 'script'` blocks `new Worker(plainUrl)`; one policy with `createHTML` and `createScriptURL` is enough | Chrome 155 + Playwright, `csp-probe/run.mjs` with and without `?tt=plain` | zero violations with the policy; `This document requires 'TrustedScriptURL' assignment` with a plain URL |
| sigma 3.0.3 `updateEachNodeAttributes`, `setSetting('labelColor')`, `refresh` repaint after a palette change | `csp-probe/palette-probe` in Chrome 155 (SwiftShader) | node `#111111` -> `#aa0000`, label `#eeeeee`, edge `#555555` |
| `WEBGL_lose_context.loseContext()` raises `webglcontextlost` that reaches a capture listener on the container | same probe | seen once; `onContextLost` called |
| TanStack Query 5: a disabled infinite observer is not refetched by `invalidateQueries` and refetches when re-enabled | probe test in the scratch tree | confirmed (V-13 mechanism) |
| Graph chunk 39.2 KB gzip, layout worker 6.0 KB gzip | esbuild bundle + gzip | reproduced as authored |
| Mount time 29 to 31 ms | Chrome 155, SwiftShader, load average about 50 | not reproduced: 264 to 843 ms (V-21) |
| Design tokens: generator reproduces `tokens.css`; contrast | `python3 -I gen.py` + diff; `contrast.py` (FAILS 0); independent check with culori 4.0.2 (26 foreground/background pairs, light and dark, plus 8 avatar backgrounds against white) | identical file; 0 failures in both |
| Type checking | `tsc --noEmit` with TypeScript 6.0.3 and with 7.0.2 | clean, both |
| Tests | `vitest run` 5.0.3 on Node 26.10.0 | 14 files, 145 tests passed (133 as authored + 12 regression tests) |

### Changed files

`18-web-pages.md` (many edits; this log appended), `18-web-pages.assets/README.md`, `18-web-pages.assets/package.json` (jsdom 26.1.0), `18-web-pages.assets/csp-probe/palette-probe/{app.ts,build.mjs,run.mjs}` (new), and under `18-web-pages.assets/web/src/`: `features/docs/{links.ts,doc-content.tsx,docId.ts,tree.ts,index.ts (new),headings.ts (new)}`, `features/commits/{filter.ts,api.ts}`, `features/manifests/api.ts`, `features/manifests/graph/{sigma-controller.ts,dependency-graph.tsx}`, `shared/{safe-html.tsx,problem-view.ts,data-table.tsx,freshness.ts}`, `app/search-model.ts`, `styles/contrast.test.ts`, and the tests `features/docs/{docs.test.ts,headings.test.ts (new)}`, `features/commits/commits.test.ts`, `features/manifests/api.test.ts (new)`, `shared/{data-table.test.tsx,shared.test.ts,safe-html.test.tsx (new)}`. No other unit's document was touched.

### Still open after the verification

* **V-28 / OQ-18-16**: the kernel (JB-01) and the contract disagree on the job key grammar; needs units 01 and 07 (and 09 for `lastErrorCode`, OQ-18-03) to agree. The UI mirrors the contract and shows the server's problem if a retry is refused.
* **OQ-18-03** (V-26 residual): `lastErrorCode` vocabularies differ between units 07 and 09; `causeId` and exact-then-prefix mapping make the UI robust to both, but the contract still says only `string`.
* **OQ-18-02**: the contract does not promise the order of the documents index; the UI sorts README first and keeps the server order after it.
* **Software-GL timing** (V-21): the mount budget is only a gate on real GPUs or relaxed numbers; a runner with a slow software renderer can still exceed 1 s at 6x throttle.

## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Changed: `shared/freshness.ts` and its test use the closed `ErrCode` vocabulary and the open question is closed (CD-ER-01); `canRetry` allows 512 characters (CD-JB-02); fixtures use extension-less doc uiUrl (CD-RT-04); ghost keys removed (CD-CF-09).
- Not adopted for v1: `author` and `q` filters on `listCommits` (D-18-02) and `site.dashboard_commits` (D-18-03) (CD-RT-07, CD-CF-12).
