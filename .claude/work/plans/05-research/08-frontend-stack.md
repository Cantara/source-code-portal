# 08 - Frontend stack (React + shadcn/ui + TypeScript) and API contract (research for D-16)

Status: research input for the architecture, `web/`, `internal/webapp`, `internal/httpapi`, the feature `api.go` files and the ADR set. Written 2026-10-13 and finished 2026-10-14 (every web fact was checked on 2026-10-13). It conforms to `04-decisions.md` (D-16 binding; D-05, D-06, D-09, D-12, D-15 read together). Where it proposes a change to a decision it says so in section 9 ("Proposed deviations"), with evidence.

Citation convention (same as `06-web-ui-badges-security.md`): `(S12, 2026-10-13)` = source S12 in Appendix A, fetched or queried on that date. `(E3)` = an experiment I ran on 2026-10-13 (code and raw results in `05-research/08-prototypes/`, throwaway, not production). Every web fact in this report was checked on 2026-10-13 (today) unless the date says otherwise. Package versions come from the npm registry (S1) and the Go module proxy (S77); "latest" below means the `latest` dist-tag on 2026-10-13. Anything I could not verify is marked **UNVERIFIED** and collected at the end.

---

## Executive summary (10 lines)

1. **Pinned stack (2026-10-13):** React 19.3.0 (+ React Compiler 1.0 on), TypeScript 7.0.2 for `tsc` with a TypeScript 6.0.2 alias for every tool that needs the compiler API (typescript-eslint, openapi-typescript, hey-api), Vite 8.3 (Rolldown + Oxc, `@vitejs/plugin-react` 6), Tailwind 4.3.3 (CSS-first), shadcn CLI 4.21 on **Base UI 1.x** (shadcn's new default since July 2026; Radix still supported), Node 24 LTS now and 26 LTS after 2026-10-28, pnpm 12 with a 7-day release cooldown. Exact pins and the "oldest-acceptable" rule are in section 1.
2. **Routing and server state:** TanStack Router 1.170 with **virtual file routes** mapping the route tree onto `src/features/*` (typed `Link`, params and Zod-validated search params verified to fail at compile time under TS 7, E9) plus TanStack Query 5.104. v1 live updates = visibility-aware polling with ETag (the browser does the conditional GET), SSE as an invalidation-only channel in v1.1; staleness shown from the API `freshness` fields plus `dataUpdatedAt`. React Router 8 is the recorded alternative (typed routes only in its framework mode).
3. **API contract (one end-to-end approach):** OpenAPI **3.1** is the source of truth, split **per Go feature package** (`internal/<feature>/api/*.yaml` + shared `internal/httpapi/openapi/common.yaml`) -> `oapi-codegen` v2.8 **strict std-lib server per feature** (compiles and serves on one `ServeMux`, E8) -> kin-openapi request-validation middleware in production -> `redocly bundle` -> one `api/openapi.bundled.yaml` -> `openapi-typescript` 7.13 + `openapi-fetch` 0.17 (2.5 KB gz). Contract tests: Schemathesis 4.31 against the running binary (it caught two real defects in my prototype, E8) and `oasdiff` for breaking changes. Errors are RFC 9457; pagination is cursor-based; JSON is camelCase; times are RFC 3339 UTC `Z`.
4. **CSP and Trusted Types are achievable without nonces, but shadcn needs three patches (all verified in a real browser, E3/E4):** (a) Base UI `<CSPProvider disableStyleElements>` + one CSS rule for the scrollbar class, (b) replace the `<style>` that `chart.tsx` injects with CSSOM custom properties, (c) disable the `<style>` that the shadcn `ThemeProvider` injects for transition suppression. After that: zero violations under `style-src 'self'` and under `require-trusted-types-for 'script'; trusted-types scp-html`, and React 19.3 accepts a `TrustedHTML` value in `dangerouslySetInnerHTML`. Mermaid does **not** survive in-page under that CSP (layout differs, 72 console errors and 216 violation events for two diagrams, E5): render it in a sandboxed iframe with its own relaxed CSP.
5. **Graph (~1,000 nodes / 2,500 edges, E6/E7; medians of 5 runs, Chrome 155, 6x CPU throttle):** use **sigma 3.0.3 + graphology** (36 KB gz, WebGL; 79 ms mount, display-limited frames) rather than @xyflow/react (+55 KB gz, 1,006 ms mount, ~50 ms/frame while panning; 447 ms and 28 ms with `onlyRenderVisibleElements`) or cytoscape (133 KB gz, 555 ms, 19 ms/frame). Do **not** adopt sigma 4.0.x yet (released 2026-10-08, 104 KB gz, rewritten program system). Layout: d3-force (0.6 s for 300 ticks, in a Web Worker); dagre only for small neighbourhood views (3 s at 750 nodes, no result after 90 s at 1,000 nodes/2,500 edges). Always ship an accessible table/tree alternative.
6. **Quality toolchain (all exercised in E10/E11):** Vitest 5.0 (jsdom project + browser-mode project on Playwright/Chrome), Testing Library 16, MSW 3.0 (ESM only, Node 22+), Playwright 1.64 + axe-core 4.14 (`wcag22aa` tag), ESLint 10 flat config + typescript-eslint + react-hooks 7 (Compiler rules) + **eslint-plugin-boundaries 7.2** (feature boundaries verified), Prettier. No Storybook, no Zustand/Jotai, no React Hook Form in v1; i18n via **Lingui 6** (+2.6 KB gz vs +15.5 KB for react-i18next, E1).
7. **Hard facts that change D-16's assumptions:** lucide-react 1.x **has no GitHub icon** (brand icons removed; use Octicons or an inline SVG); shadcn's default tokens **fail WCAG 2.2 AA** in several places (computed: input border 1.26:1 vs 3:1, muted text on muted 4.34:1 vs 4.5:1, E13); shadcn pulls `shadcn` (CLI, 33 direct deps) into `dependencies` (move it to `devDependencies`: production closure 453 -> 165 packages, E2); corepack is not shipped from Node 25; `jsx-a11y` has not been released since Oct 2024 (works on ESLint 10 at runtime, peer range stops at 9, E10).
8. **Supply chain is a first-order design input in 2026:** TanStack's Router/Start monorepo was compromised on 2026-05-11 (42 packages, cleared 05-15; Query and Table unaffected), Shai-Hulud-class worms continue. Policy: pnpm `minimumReleaseAge: 10080`, `trustPolicy: no-downgrade`, `blockExoticSubdeps`, `strictDepBuilds`, exact pins, committed lockfile, SHA-pinned GitHub Actions, no `pull_request_target` with fork code.
9. **Svelte 5 comparison (for the ADR, no argument against the owner decision):** Svelte hello-world is 12.0 KB gz vs React's 65.8 KB gz (E15) and Svelte's "admired" score is higher (59.4% vs 46.7%), but React has ~30x the npm downloads (815.6M vs 27.6M last month), 41.5% vs 7.8% usage in the Stack Overflow 2026 survey, a first-party shadcn/ui, TanStack Table/Router/Query/Charts at first-class level, and measurably more LLM training data (indirect evidence only). SvelteKit 3 shipped 2026-10-01; its own docs warn that SPA mode has "a large negative performance impact".
10. **Open decisions for the owner/ADR (section 9):** commit generated Go and TS API code with a CI drift check (D-14 tension); production Zod validation of responses is off by default (types come from the contract; build-id skew detection instead); admin listener serves the same SPA for `/status`; static CSP without nonce (differs from `06` section 4.3); Mermaid in a sandboxed iframe; graph library pin (sigma 3 vs cytoscape fallback).

---

## 1. Verified version matrix and pins

Rule used for the "Pin" column (supply-chain policy, S53/S54): the newest **stable** version of the **same major** that was published at least 7 days before 2026-10-13 (pnpm's own default cooldown is 1 day, S53; I recommend 7). Re-run the script at implementation time (`08-prototypes/` has none; it is 25 lines of Node/Python against `registry.npmjs.org`). The lockfile, not this table, is authoritative once the repository exists.

| Package | Latest (published) | Pin | Licence | Notes (all from S1 unless another source is given) |
|---|---|---|---|---|
| Node.js | 24.21.0 LTS (2026-09-07), 26.11.1 Current (2026-10-07) | **24.21.x** now; move to **26 LTS** after 2026-10-28 | MIT | Node 24 enters Maintenance 2026-10-20 (EOL 2028-04-30); Node 26 becomes LTS 2026-10-28 (EOL 2029-04-30); Node 22 is Maintenance (EOL 2027-04-30) (S58, S59). Engines: Vitest 5 needs `^22.12 || ^24 || >=26`; MSW 3 `>=22.12`; Lingui 6 `>=22.19`; hey-api `>=22.18`; React Router 8 `>=22.22` (S1, S50). `engines.node: "^24.21.0 || ^26.0.0"` |
| pnpm | 12.13.0 on GitHub releases (2026-10-13, S52); npm `latest` read 12.11.2 | **12.10.1** (2026-10-06, S52) | MIT | settings live in `pnpm-workspace.yaml` from v11 (S56); `devEngines.packageManager` takes a range (S56); standalone install or `npx get-pnpm` (S55) |
| Go (backend, for the embed/CI sections) | 1.27.2 (S78); Docker `golang:1.27.2-alpine` digest `sha256:f92b6ef8...` (S60, 2026-10-10) | 1.27.2 | BSD-3 | 1.27.0 released 2026-08-19 (S78 via go.dev/doc/devel/release) |
| react, react-dom | 19.3.0 (2026-09-09) | **19.3.0** | MIT | S2, S3; repository now under the React Foundation (S7) |
| @types/react, @types/react-dom | 19.3.0 | 19.3.0 | MIT | |
| typescript (for `tsc`) | 7.0.2 (2026-07-08) | **npm:typescript@7.0.2** under alias `@typescript/native` | Apache-2.0 | native Go port, no JS API until 7.1 (S10) |
| typescript (for tools) | `@typescript/typescript6` 6.0.2 (2026-07-06) | **`"typescript": "npm:@typescript/typescript6@6.0.2"`** | Apache-2.0 | the pattern the TS team documents (S10); verified working with typescript-eslint, openapi-typescript, hey-api (E9, E10) |
| vite | 8.3.4 (2026-10-08) | **8.3.3** | MIT | Rolldown + Oxc; Node `^20.19 || >=22.12` (S13, S1) |
| @vitejs/plugin-react | 6.1.2 (2026-10-05) | 6.1.2 (8 days old) | MIT | Oxc React Refresh, no Babel; `reactCompilerPreset` helper (S13, S8) |
| @rolldown/plugin-babel | 0.2.4 | 0.2.4 | MIT | needed only for the React Compiler (S8); 0.x |
| babel-plugin-react-compiler | 1.0.0 (2025-10-07) | **1.0.0 exact** | MIT | docs: pin an exact version (S5) |
| tailwindcss, @tailwindcss/vite | 4.3.3 (2026-07-16) | 4.3.3 | MIT | v4.3 released 2026-05-08 (S17); browsers: Chrome 111, Safari 16.4, Firefox 128 (S19) |
| @tailwindcss/typography | 0.5.20 (2026-06-08) | 0.5.20 | MIT | `@plugin "@tailwindcss/typography"` (S20) |
| shadcn (CLI + `shadcn/tailwind.css`) | 4.21.4 (2026-10-07) | **4.21.3**, as a **devDependency** | MIT | CLI v4 (2026-03), Base UI default (2026-07) (S21, S22) |
| @base-ui/react | 1.9.0 (2026-10-09) | **1.8.0** (or 1.9.0 after 2026-10-16) | MIT | v1.0.0 stable 2025-12-11 (S32) |
| radix-ui (only if Radix is chosen) | 1.8.1 (2026-10-12) | 1.7.0 | MIT | S34 |
| cn | 0.4.0 (2026-09-22) | 0.4.0 | MIT | shadcn's new `twMerge(clsx())` replacement; npm maintainer `shadcn`, provenance attestation present (S30, S1) |
| class-variance-authority, tw-animate-css | 0.7.1, 1.4.0 | same | Apache-2.0, MIT | both emitted by `shadcn init` (E2); both unreleased for >12 months |
| lucide-react | 1.56.0 (2026-10-13) | 1.52.0 | ISC | **no brand icons** (S109, E2) |
| @primer/octicons-react | 19.40.0 | 19.39.0 | MIT | for the GitHub mark |
| @tanstack/react-router | 1.170.44 (2026-10-12) | **1.170.41** | MIT | |
| @tanstack/router-plugin | 1.168.45 | **1.168.42** (peer `react-router ^1.170.41`, checked in S1) | MIT | Vite plugin: generation + auto code splitting (S43) |
| @tanstack/virtual-file-routes | 1.162.0 | 1.162.0 | MIT | S41 |
| @tanstack/react-query | 5.104.2 (2026-10-11) | 5.104.1 | MIT | not part of the 2026-05-11 compromise (S37) |
| @tanstack/react-table | 9.3.1 (2026-10-12) | 9.2.6 | MIT | v9 stable (S39); the shadcn Data Table guide targets v9 (S25) |
| @tanstack/react-virtual | 3.14.19 | 3.14.13 | MIT | for >200-row tables |
| zod | 4.6.7 (2026-10-12) | 4.6.5 | MIT | Standard Schema; direct use in TanStack Router (S42) |
| recharts | 3.10.1 (2026-07-25) | shadcn pins **3.8.0** in `chart` (E2) | MIT | |
| cmdk | 1.1.1 (2025-03-14) | 1.1.1 | MIT | used by shadcn `Command` even on Base UI (E2); 19 months without a release |
| sigma | 4.0.1 (2026-10-13); **3.0.3 (2026-04-30)** | **3.0.3** | MIT | v4.0.0 2026-10-08, rewrite (S85) |
| graphology | 0.26.0 (2025-01-26) | 0.26.0 | MIT | |
| d3-force / d3-zoom / d3-selection | 3.0.0 | 3.0.0 | ISC | stable since 2021 |
| @dagrejs/dagre | 3.1.1 | 3.1.1 | MIT | |
| @xyflow/react | 12.12.0 (2026-09-24) | not adopted | MIT | benchmark only (E6) |
| cytoscape | 3.34.3 (2026-09-07) | fallback only | MIT | |
| mermaid | 12.2.0 (2026-10-13) | **12.1.0** | MIT | CSS-injection CVEs fixed in 11.15.0 per a secondary source (S96) |
| openapi-typescript / openapi-fetch | 7.13.0 / 0.17.0 (both 2026-02-11) | same | MIT | no release for 8 months (S1) |
| @hey-api/openapi-ts | 0.99.0 (2026-06-22) | 0.99.0 exact (pre-1.0) | MIT | only for the optional Zod output (E9) |
| @redocly/cli | 2.63.0 (2026-10-13) | 2.59.0 | MIT | `bundle` + `lint` (E8) |
| vitest, @vitest/browser-playwright | 5.0.4 (2026-10-12) | 5.0.3 | MIT | released 2026-09-03 (S105) |
| vitest-browser-react | 2.3.0 | 2.3.0 | MIT | |
| jsdom | 30.1.2 | 30.1.2 | MIT | |
| @testing-library/react, /dom, /user-event | 16.3.3, 10.4.2, 14.6.8 | 16.3.3, 10.4.2, 14.6.7 | MIT | |
| msw | 3.0.3 (2026-10-10) | 3.0.2 | MIT | 3.0 released 2026-09-30, ESM-only, Node 22/24/26 (S106) |
| @playwright/test | 1.64.0 (2026-10-07) | 1.63.0 | Apache-2.0 | |
| axe-core, @axe-core/playwright | 4.14.0, 4.13.0 | 4.14.0, 4.13.0 | MPL-2.0 | `wcag22aa` tag exists (S100) |
| eslint | 10.12.0 (2026-10-02) | 10.12.0 | MIT | v9 reached EOL 2026-08-06 (S103) |
| typescript-eslint | 8.71.2 (2026-10-12) | 8.71.1 | MIT | peer `typescript >=4.8.4 <6.1.0` => needs the TS6 alias (S11, S1) |
| eslint-plugin-react-hooks | 7.1.1 | 7.1.1 | MIT | includes React Compiler lint rules (S5) |
| eslint-plugin-jsx-a11y | 6.10.2 (2024-10-26) | 6.10.2 | MIT | peer range ends at ESLint 9 (S1); runs on 10 (E10) |
| eslint-plugin-boundaries | 7.2.0 (2026-08-09) | 7.2.0 | MIT | new `boundaries/dependencies` policy DSL (S104, E10) |
| eslint-import-resolver-typescript | 4.4.5 | 4.4.5 | ISC | alias resolution for the boundaries plugin (E10) |
| prettier, prettier-plugin-tailwindcss | 3.9.10, 0.8.1 | 3.9.9, 0.8.1 | MIT | what `shadcn init` emits (E2) |
| @lingui/core, react, cli, vite-plugin | 6.9.1 (2026-10-09) | 6.9.0 | MIT | `lingui({ macroTransform: true })` (S108) |
| size-limit, @size-limit/file | 14.2.0 | 14.1.0 | MIT | bundle budgets |
| lighthouse | 13.5.0 (2026-09-18) | 13.5.0 | Apache-2.0 | `@lhci/cli` last released 2025-06-25 (S1) |
| @fontsource-variable/geist, geist-mono | 5.3.0 | 5.3.0 | OFL-1.1 | what `shadcn init` emits (E2) |
| @types/node | 26.6.5 | **24.x** (match Node 24) | MIT | |
| knip | 6.41.0 | 6.40.0 | ISC | dead code and unused deps |

Also verified but not adopted: React Router 8.4.0 (2026-09-15, S50), TanStack Start 1.0 (2026-10-13, S36), Storybook 10.6.2, Biome 2.5.15 (S102), jotai 3.0.1, zustand 5.0.15, react-hook-form 7.90.0, react-i18next 17.0.16, i18next 26.4.4, visx 4.0.0, TanStack Charts 1.0 (2026-10-05, S36), Lighthouse CI 0.15.1.

---
## 2. Frontend stack decisions

### 2.1 React 19.3 (what it changes, what it does not)

- **Current:** React 19.3.0, published 2026-09-09 (S2, S3). The React repositories moved to the React Foundation (Linux Foundation; platinum members Amazon, Callstack, Expo, Huawei, Meta, Microsoft, Software Mansion, Vercel) announced 2026-02-24 (S7). Licence MIT (S1).
- **What 19.0 -> 19.3 gives an SPA** (S6 for 19.0, S4 for 19.2, S3 for 19.3):
  - 19.0: Actions (async transitions with pending/error state), `useActionState`, `useOptimistic`, `use()` for promises and conditional context reads, `ref` as a plain prop (no `forwardRef`), `<Context value>` as provider, `<title>/<meta>/<link>` hoisted to `<head>` (useful: per-route page titles without a head library), `<link rel=stylesheet precedence>`, ref cleanup functions, `onCaughtError`/`onUncaughtError` root options. Removed: `ReactDOM.render`, `defaultProps` on function components, string refs, `propTypes`, UMD builds (S6).
  - 19.2: `<Activity mode="visible|hidden">` (keeps state of hidden screens; useful for the repo page tabs), `useEffectEvent` (S4).
  - 19.3: `<ViewTransition>` stable, Fragment refs stable, `browser()` for browser-only components, **Trusted Types support** (React no longer coerces values to strings before DOM sinks, so `TrustedHTML` passes through; confirmed in E4), independent Transitions, `useDeferredValue` and `useSyncExternalStore` fixes (S3).
- **How SCP should use it:** data via TanStack Query (not `useEffect`); `useTransition`/`useDeferredValue` for filter inputs bound to URL search params; `Suspense` + `useSuspenseQuery` at route boundaries with skeletons; optional `<ViewTransition>` for route changes (respect `prefers-reduced-motion`); `use()` only for context. No manual `memo/useMemo/useCallback` (Compiler).
- **React Compiler 1.0 (stable since 2025-10-07, S5): turn it on, exact-pin it.** Vite 8 setup with `@vitejs/plugin-react` 6 is `react()` + `babel({ presets: [reactCompilerPreset()] })` from `@rolldown/plugin-babel` (S8). Measured on the shadcn spike (E14): build works, the bundle gains +9.5 KB raw / +2.5 KB gz (+1%), build time 0.4 s -> 1.3 s. Lint rules ship in `eslint-plugin-react-hooks` 7 (S5). Docs advise pinning an exact compiler version because memoisation changes can alter effect timing (S5). CI must run the test suite against the compiled build, not only the dev server.
- **Server Components are not needed** and not used (D-16: no SSR by default). Two React advisories in December 2025 concerned RSC server packages (titles only read: "Critical Security Vulnerability in React Server Components" 2025-12-03 and "Denial of Service and Source Code Exposure in RSC" 2025-12-11, S2); a client-only bundle does not ship those packages. I did not read the advisories (UNVERIFIED detail).
- **Not adopted:** Create React App (sunset 2025-02-14, S2), Next.js/Start (SSR frameworks, out of scope; TanStack Start 1.0 went stable on 2026-10-13 and had a critical reflected-XSS fix on 2026-09-30 that concerns the Start server packages only, S36, S38).

### 2.2 TypeScript 7 (native port) + TypeScript 6 alias; strictest sensible tsconfig

Facts (S10 unless noted): TypeScript 7.0 (2026-07-08) is the Go-based native port, typically 8-12x faster in full builds (vscode 125.7 s -> 10.6 s; playwright repo 12.8 s -> 1.47 s), `npm install -D typescript` provides `tsc` 7. **It ships no JavaScript API**; a new API is expected in 7.1 (releases every 3-4 months). Tools that import `typescript` (typescript-eslint, openapi-typescript, hey-api) must keep a 6.x compiler through an npm alias; the TS team documents exactly this pattern with `@typescript/typescript6` (which exposes `tsc6`). typescript-eslint's supported range is `>=4.8.4 <6.1.0` and its page does not mention TS 7 (S11). TS 7 changes defaults: `strict: true`, `module: esnext`, `types: []` (must list ambient types), `noUncheckedSideEffectImports: true`, `rootDir` defaults to `./`; removes `baseUrl`, `moduleResolution: node/classic`, `target: es5`, `esModuleInterop: false` and treats 6.0 deprecations as errors (S10).

Verified together (E9): with

```json
"devDependencies": {
  "typescript": "npm:@typescript/typescript6@6.0.2",
  "@typescript/native": "npm:typescript@7.0.2"
}
```

`pnpm exec tsc` runs 7.0.2, `pnpm exec tsc6` runs 6.x, `openapi-typescript` 7.13 (peer `typescript ^5.x`) and `@hey-api/openapi-ts` 0.99 generate correctly against the 6.x alias, and typescript-eslint 8.71 loads (E10). A strict consumer of the generated client compiles clean under both compilers in 0.8 s (7) .

Recommended `tsconfig.app.json` (all flags compile clean on the shadcn-generated Base UI components plus a typed openapi-fetch consumer, E2/E9; `erasableSyntaxOnly` keeps the code compatible with Node/Oxc type stripping):

```jsonc
{
  "compilerOptions": {
    "target": "ES2023", "lib": ["ES2023", "DOM", "DOM.Iterable"], "module": "ESNext", "moduleResolution": "bundler",
    "jsx": "react-jsx", "noEmit": true, "types": ["vite/client", "trusted-types"],
    "strict": true, "noUncheckedIndexedAccess": true, "exactOptionalPropertyTypes": true,
    "noImplicitOverride": true, "noImplicitReturns": true, "noFallthroughCasesInSwitch": true,
    "noPropertyAccessFromIndexSignature": true, "noUnusedLocals": true, "noUnusedParameters": true,
    "verbatimModuleSyntax": true, "isolatedModules": true, "erasableSyntaxOnly": true,
    "noUncheckedSideEffectImports": true, "forceConsistentCasingInFileNames": true, "skipLibCheck": true,
    "paths": { "@/*": ["./src/*"] }          // no baseUrl: removed in TS 7 (S10)
  },
  "include": ["src"]
}
```

Gotchas found: (1) the default shadcn Base UI `scroll-area.tsx` has an unused `import * as React` that fails `noUnusedLocals` (E2); the component is owned source, so fix it in place. (2) `window.trustedTypes` is not in `lib.dom` of TS 6.0; add `@types/trusted-types` (E4). (3) `shadcn init` generates `"typescript": "~6.0.2"` and `tsc -b` (E2); replace with the alias pair above. (4) Vite/Oxc strip types without the TS API, so the alias is needed only for lint, codegen and editors; drop it when 7.1 ships a compatible API and the three tools support it (S10).

### 2.3 Vite 8 (Rolldown, Oxc)

- Vite 8.0 (2026-03-12) replaced esbuild + Rollup with **Rolldown** (Rust) and Oxc; claims "10-30x faster builds"; requires Node 20.19+/22.12+; ESM-only; install about 15 MB larger (S13). 8.1 (2026-06-23) adds an *experimental* "Bundled Dev Mode" (not for use here) (S14). Latest 8.3.4 (2026-10-08, S1).
- Migration facts that matter (S15): `build.rollupOptions` -> `build.rolldownOptions`; `output.manualChunks` function form deprecated in favour of `output.codeSplitting`; Oxc minifier and Lightning CSS minifier are the defaults; default `build.target` = Baseline Widely Available: Chrome 111, Edge 111, Firefox 114, Safari 16.4 (S15, S16). Tailwind 4 needs Firefox 128 (S19), so the real floor is **Chrome/Edge 111, Safari 16.4, Firefox 128**.
- **Rolldown status:** stable inside Vite 8 (default bundler). Evidence that it works for this stack: every build in this report (E1-E6, E14) ran on Vite 8.3.4/Rolldown 1.2.x; the `inlineDynamicImports` option I used in benchmarks printed a deprecation warning pointing to `codeSplitting: false` (E1).
- Stale-chunk handling after a deploy: because the Go binary replaces all assets, an open tab will request deleted chunk names. Vite fires `vite:preloadError`; call `event.preventDefault()` and reload (S16). Serve `index.html` with `Cache-Control: no-cache` (S16; 06 section 3.2).
- Recommended `vite.config.ts` core (E2/E14/E9 composite; nothing here is exotic):

```ts
export default defineConfig({
  plugins: [
    tanstackRouter({ target: "react", autoCodeSplitting: true, routesDirectory: "./src",
                     virtualRouteConfig: "./routes.ts", generatedRouteTree: "./src/routeTree.gen.ts" }),
    react(),
    babel({ presets: [reactCompilerPreset()] }),   // React Compiler 1.0
    tailwindcss(),
  ],
  resolve: { alias: { "@": resolve(import.meta.dirname, "./src") } },
  build: { target: "baseline-widely-available", sourcemap: false, assetsInlineLimit: 0, cssCodeSplit: true },
  server: { port: 5173, proxy: { "/api": "http://127.0.0.1:9090", "/badges": "http://127.0.0.1:9090",
                                 "/media": "http://127.0.0.1:9090", "/llms.txt": "http://127.0.0.1:9090" } },
})
```

(`assetsInlineLimit: 0` keeps `img-src`/`font-src` free of `data:`; 06 section 3.1 reaches the same conclusion. Plugin order: the TanStack plugin must precede `react()`, S43.)

### 2.4 Tailwind CSS 4.3 (CSS-first)

- Current 4.3.3 (2026-07-16); v4.3 announced 2026-05-08 (scrollbar utilities, more logical properties); v4.2 had no post; **no v5 post exists** (S17, S1). v4.0 (2025-01-22) introduced CSS-first configuration: `@import "tailwindcss"`, theme tokens in `@theme`, no `tailwind.config.js`; this is exactly what `shadcn init` generated (`@import "tailwindcss"; @import "tw-animate-css"; @import "shadcn/tailwind.css"; @custom-variant dark (&:is(.dark *)); @theme inline {...}`, E2).
- Browser floor Chrome 111, Safari 16.4, Firefox 128; relies on native CSS variables, `color-mix()` (S19).
- **Ownership news:** Tailwind Labs joined Shopify on 2026-09-09; all open-source projects stay MIT, the team keeps maintaining them, no governance details disclosed (S18). Risk: low-moderate (recorded in Risks).
- Output size with ~25 shadcn components imported (E2): 92.5 KB CSS raw / 15.5 KB gzip; +typography plugin 106 KB raw / 17.4 KB gz.
- Tailwind produces no inline styles; with `unlayered`/`@layer` defaults the CSS is one external file, compatible with `style-src 'self'` (E3).

### 2.5 shadcn/ui (CLI v4, Base UI default, Tailwind v4, React 19)

**Status (S21, S22, S24, S27, S30):**

| Topic | Fact |
|---|---|
| Tailwind v4 / React 19 | supported since 2025-02 and 2024-10 respectively (changelog) |
| CLI | CLI 3.0 + MCP server 2025-08; **CLI v4 2026-03**; `shadcn init -t vite -b base`; presets (`apply`, `preset`), `shadcn eject`, registries incl. GitHub and private GitHub registries (2026-06/08) |
| Primitives | **Base UI is the default since 2026-07** ("Base UI as the Default"); `-b radix` keeps Radix; every update and component ships for both (except Base-UI-only components); existing projects need no migration; progressive per-component migration via an agent skill (S22) |
| `cn` | components import `cn` from the new `cn` package (2026-09); `lib/utils.ts` becomes a one-line re-export; `shadcn migrate cn` available (S30) |
| Components | 64 listed, including Sidebar, Command, Combobox, Data Table (guide), Chart, Typography, Table, Breadcrumb, Pagination, Skeleton, Empty, Field, Item, Kbd, Spinner, Input Group, Button Group, Sheet, Drawer, Resizable, Tabs, Toast (S24) |
| Data Table | not a component: a guide on `<Table>` + **TanStack Table v9** (`tableFeatures()`, `useTable`, `createColumnHelper`): sorting, filtering, pagination, row selection, column visibility; **no virtualization** in the guide (S25) |
| Chart | Recharts **v3**; `accessibilityLayer`; colours from `--chart-1..5` CSS variables; any Recharts chart type (S26); the CLI pins `recharts` `3.8.0` (E2) |
| Toast | the new Toast component is built on Base UI Toast (`toast.add({title})`); Sonner no longer needed (S29, E2) |
| Theming | OKLCH CSS variables, `@theme inline`, `.dark` class with `@custom-variant dark`, add tokens by defining under `:root` and `.dark` and mapping in `@theme inline` (the docs show `--warning`) (S27) |
| Typography | `Typography` page + **shadcn/typeset** (2026-07): one CSS file you own, `typeset typeset-docs` classes; licence not stated on the page (S31) |

**What `shadcn init -t vite -b base` actually produced (E2, 2026-10-13):** Vite 8, React 19.3, Tailwind 4, ESLint 10 + typescript-eslint + react-hooks 7 + react-refresh, Prettier + tailwind plugin, `typescript ~6.0.2`, `@base-ui/react ^1.9.0`, `cn ^0.4.0`, `class-variance-authority`, `tw-animate-css`, `lucide-react ^1.56.0`, `@fontsource-variable/geist`, `components.json` style `base-nova`, `pnpm-workspace.yaml` with `allowBuilds`, a `ThemeProvider` (localStorage, `d` key toggles dark). Triggers use the **`render` prop** (`<DialogTrigger render={<Button/>} />`) instead of Radix `asChild`; most public code and LLM output still uses `asChild`, so reviews must watch for it.

**Findings that D-16 did not anticipate (all reproduced, E2-E4, E13):**

1. **`shadcn` is added to `dependencies`**, bringing the CLI (33 direct deps) into the production closure: 453 packages versus 165 when it is moved to `devDependencies` (the only runtime use is the build-time CSS import `shadcn/tailwind.css`, 16 KB). Move it to `devDependencies` (or vendor the CSS) and pin it.
2. **Three places inject inline `<style>` and break `style-src 'self'`** (E3, Chrome 155, `securitypolicyviolation` listener): Base UI `ScrollArea.Viewport`/`Select` (class `base-ui-disable-scrollbar`), the shadcn `chart.tsx` `ChartStyle`, and the `ThemeProvider` transition-suppression `<style>`. Fixes, all verified with zero remaining violations while exercising Dialog, Select, Tooltip, DropdownMenu, Popover, Sheet, Toast, Tabs, Command, Chart and theme toggling (E3):
   - wrap the app in `<CSPProvider disableStyleElements>` (from `@base-ui/react/csp-provider`; documented, S33) and add `.base-ui-disable-scrollbar{scrollbar-width:none}.base-ui-disable-scrollbar::-webkit-scrollbar{display:none}` to the global CSS (the class is still applied when elements are disabled);
   - in `chart.tsx` delete `<ChartStyle>` and set the colour variables through the `style` prop (`style={{ "--color-visits": "var(--chart-1)" }}`), which React writes through CSSOM and CSP allows; light/dark variants then come from CSS variables, not from `config.theme`;
   - `<ThemeProvider disableTransitionOnChange={false}>` (or remove `disableTransitionsTemporarily`).
   Inline `style=""` attributes created by React/Floating UI are CSSOM writes and are not blocked (19 elements with `style` attributes, zero violations, E3).
   The alternative in `06` (nonce wiring into `CSPProvider`) also works (S33 documents it) but is unnecessary once these three are removed; either way the Playwright "fail on any `securitypolicyviolation`" gate stays.
3. **Trusted Types enforcement works** (`require-trusted-types-for 'script'; trusted-types scp-html`): the whole spike ran with zero violations, and a `SafeHtml` component that passes `policy.createHTML(html)` to `dangerouslySetInnerHTML` renders correctly with React 19.3 (E4). This resolves the UNVERIFIED item in `06` section 4.1; the "ref + innerHTML" workaround in `06` section 8.4 is not required (both are fine; `dangerouslySetInnerHTML` keeps React in charge of updates). Trusted Types is Baseline "newly available" since 2026-02-24 (Chrome/Edge 83, Firefox 148, Safari 26) (S97, S98), so enforcement applies to current browsers only; older ones simply ignore the header.
4. **Default tokens fail WCAG 2.2 AA** (computed from the generated `index.css`, E13): light `--input`/`--border` oklch(0.922) vs white 1.26:1 (SC 1.4.11 needs 3:1 for the boundary of an input), `--muted-foreground` on `--muted` 4.34:1 (SC 1.4.3 needs 4.5:1), `--ring` 2.59:1, `--chart-1` 1.48:1; dark `--input` 1.47:1, `--border` 1.25:1. Proposed replacements that pass (E13): light `--muted-foreground: oklch(0.50 0 0)` (6.0:1; 5.5:1 on muted), `--input: oklch(0.62 0 0)` (3.64:1), `--ring: oklch(0.60 0 0)` (3.95:1); dark `--input: oklch(0.52 0 0)` (>3:1 on background and card), `--ring: oklch(0.65 0 0)` (6.1:1); status text colours: success oklch(0.52 0.14 150), warning oklch(0.55 0.13 75), danger oklch(0.55 0.20 27), info oklch(0.52 0.15 255) all 4.9-5.6:1 on white; dark equivalents 7-11:1. Decorative card borders may stay low-contrast (SC 1.4.11 applies to what identifies a control). Ship a Vitest test that computes contrast for every foreground/background token pair in both themes (the script in `08-prototypes/contrast.py` is the logic; port it to TS).
5. **The generated Sidebar writes `document.cookie`** (`sidebar_state`, E2 grep). SCP has no server use for it; replace with `localStorage` (cookies add a consent and CSRF-surface question for nothing).
6. **Generated code compiles under the strict tsconfig in 2.2** once the unused `React` import is removed (0 errors with `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `noPropertyAccessFromIndexSignature`, E2).
7. **axe-core run on the spike** (E17) flagged only my own test markup (a `Select` trigger without a name and an unlabeled checkbox) and returned `color-contrast` and `target-size` as "incomplete" (needs manual review): automated axe alone cannot certify AA; keep the token-contrast test and a manual checklist.

**Base UI or Radix?** Choose **Base UI** (the shadcn default and what the generated code, `CSPProvider` and Toast are built on). Evidence: Base UI v1.0.0 stable 2025-12-11, v1.9.0 on 2026-10-09 with monthly feature releases; 37 components; ships a CSP provider (S32, S33). Radix: unified `radix-ui` package (2025-01-22), last release 2026-07-20 with React 19.2 fixes, but 2025 maintenance concerns after the WorkOS acquisition are reported by secondary sources only (S34, S35 - UNVERIFIED as fact). Both are supported by shadcn "for now" (S22). Escape hatch: `shadcn init -b radix` and per-component migration (S22). Cost: Base UI + 8 primitives measured +59 KB gz vs Radix +35 KB gz over the React baseline (E1, imports were not individually tree-shaken, treat as an upper bound).

**Typography for server-rendered Markdown (D-06/D-16).** The Go pipeline returns sanitised HTML; the SPA wraps it in a typography container.
- Recommended: **`@tailwindcss/typography` 0.5.20** (`@plugin "@tailwindcss/typography"`, `prose dark:prose-invert max-w-none`, `not-prose` escape hatch, S20). Verified in E2 that the generated CSS styles `table`, `thead`, `pre`, `blockquote`, `kbd`, `figure`, `hr`, `img`; it does **not** style `details/summary`, task-list checkboxes, GitHub alerts, footnotes or Chroma token classes, so a `docs.css` of ~80 lines is needed. Map prose colours to shadcn tokens via `--tw-prose-body: var(--foreground)` etc. so dark mode comes from the same variables.
- Alternative: **shadcn/typeset** (S31): single CSS file, theme-aware, size/leading/flow variables, `typeset-scroll` for wide tables; I could not confirm its licence, which elements it covers (tables and lists are named, code/blockquote/task lists are not) or CSP behaviour. Revisit after the licence is confirmed.
- **Syntax highlighting is CSS only:** the Go renderer emits Chroma class names (`html.WithClasses(true)`), the SPA ships two scoped stylesheets (light under `:root`, dark under `.dark`) generated once from two Chroma styles; no JS, no inline styles, works with `style-src 'self'`. chroma v2.27.0 exists (S77); I did not run the generator (UNVERIFIED output). Copy-to-clipboard on code blocks: one delegated click handler (progressive enhancement).
- Links in rendered HTML: one delegated click handler routes same-origin `/repos/...` links through the router when no modifier key is pressed (as in `06` section 8.4).

**Sidebar, Command, Data Table, Chart usage** is specified page by page in section 7. `cmdk` 1.1.1 (last release 2025-03-14) still powers shadcn `Command` on Base UI (E2): acceptable (small, MIT) but a maintenance risk to note; the fallback is Base UI's own `Autocomplete`/`Combobox` (S32).

### 2.6 Routing: TanStack Router vs React Router 8

| | TanStack Router 1.170 | React Router 8.4 (library/SPA use) |
|---|---|---|
| Typed `to`, params, search | yes: unknown path, missing `$param`, wrong search type all fail to compile; verified under TS 7 and TS 6 (E9) | **only in Framework mode** (type-safe `href`, `Route.LoaderArgs` via generated `+types`); Declarative and Data modes have no typed routes; code splitting also Framework-only (S51) |
| Search params | validated with Zod 4 directly (Standard Schema), `retainSearchParams`, `stripSearchParams` middlewares (S42) | string-based; typed validation is DIY (S40, a table written by the TanStack team, treat as biased) |
| Feature-sliced layout | **virtual file routes** map any file to any route (S41), verified generating a tree from `features/*` (E9) | Framework mode has `routes.ts` config too (S51), SSR-first conceptual model |
| Code splitting | `autoCodeSplitting: true` splits each route's components out of the critical config (S43); verified: separate route chunks of 0.09-0.21 KB gz each in E9 | framework mode only |
| Loaders + Query | `createRootRouteWithContext<{queryClient}>`, loader calls `queryClient.query`/`ensureQueryData`, `defaultPreloadStaleTime: 0` (S44, S46) | loaders in Data/Framework modes |
| Size (hello app, E1) | +25.1 KB gz over React baseline | +28.9 KB gz |
| Requirements | Node >=20.19 (router), React >=18 (S1) | Node >= 22.22, React >= 19.2.7, Vite >= 7, ESM-only (S50) |
| Releases / risk | v1 line, new patch almost daily (1.170.44 on 2026-10-12); **Router/Start monorepo compromised 2026-05-11** (42 packages, 84 versions, removed within hours, all-clear 05-15; Query/Table/Form etc. unaffected) (S37); Start-only critical XSS fix 2026-09-30 (S38) | v8.0.0 2026-06-17, 8.4.0 2026-09-15; v8 removed `react-router-dom`, middleware always on (S50) |

**Decision: TanStack Router** (+ Query), because SCP's UX is URL-state heavy (filters, tabs, graph focus, pagination cursors) and D-16 requires "typed routes, no hard-coded URL strings"; only TanStack gives that in a pure SPA without adopting a framework mode. Conditions: exact pins, the pnpm cooldown policy in 2.18, and no use of TanStack Start. React Router 8 framework mode in SPA mode is the recorded alternative (ADR).

Single source of URLs: `web/routes.ts` (virtual route config, committed). Verified (E9):

```ts
// web/routes.ts
import { rootRoute, route, index } from "@tanstack/virtual-file-routes"
export const routes = rootRoute("app/root.tsx", [
  index("app/index-redirect.tsx"),                     // "/" -> "/dashboard" (redirect in beforeLoad)
  route("/dashboard", "features/dashboard/route.tsx"),
  route("/groups/$groupId", "features/groups/route.tsx", [
    index("features/groups/overview.tsx"),
    route("/commits", "features/groups/commits.tsx"),
  ]),
  route("/repos/$repo", "features/docs/route.tsx"),
])
```

Facts learned: one file cannot be mapped to two routes (bundler error "already declared", E9), so `/` is its own redirect route; `routeTree.gen.ts` is generated into `src/` and exposes `FileRoutesByFullPath`; `Link to="/nope"`, a missing `params`, and `search={{ page: "x" }}` fail compilation with TS 7.0.2 and 6.x (E9). Search schemas live next to each route with Zod 4 (`z.object({ sort: z.enum([...]).default("name") })` passed directly, S42).

**Route manifest for the Go side** (resolves the UNVERIFIED proposal in `06` section 3.2): `routes.ts` is a plain data structure that Node 26 can import natively (type stripping), and a 15-line script flattened it to `["/","/dashboard","/groups/$groupId","/groups/$groupId/commits","/repos/$repo"]` (E9). Emit that as `web/dist/routes.json` during `pnpm build`; the Go `webapp` translates `$id` to `{id}` for shape-matching. The tree file `routeTree.gen.ts` (`FileRoutesByFullPath`) is an alternative source.

Other router facts: browser history needs the server-side SPA fallback (hash history exists only for servers that cannot rewrite, S45); head/title: use React 19 `<title>` in each route component (S6); set `defaultPreload: "intent"` and `defaultPreloadStaleTime: 0` so Query owns freshness (S46). TanStack Router does not announce route changes to assistive technology out of the box as far as I know (UNVERIFIED); implement the focus/live-region pattern in 2.20.

### 2.7 Server state: TanStack Query 5, polling, ETag, SSE, staleness

Facts: v5.104.2 (2026-10-11), MIT (S1). Defaults (S48): `staleTime: 0`, `gcTime: 5 min`, refetch on window focus (default `true`, from the JSDoc shipped in `@tanstack/query-core` 5.104.2, S114) and on reconnect, 3 silent retries with exponential backoff, structural sharing on. `refetchInterval` accepts a number or a function of the query; `refetchIntervalInBackground` defaults to `false`, so hidden tabs do not poll (S114, matches `06` section 6.2). `invalidateQueries` marks matching queries stale and refetches active ones (S47). `isFetching`/`useIsFetching` expose background refetch state (S49). Query was not part of the May 2026 compromise (S37).

**Conventions to adopt** (design, not all prototyped):
- One `queryOptions` factory per feature (`features/<x>/api.ts`), keys `[feature, resource, params]`; a shared `unwrap()` converts openapi-fetch `{data,error,response}` into data or a thrown `ProblemError` (typed from the `Problem` schema); `retry` returns false for 4xx.
- Defaults: `staleTime: 10_000`, `refetchInterval: 30_000` on the latest-commits panel, `placeholderData: keepPreviousData` for paginated lists, `refetchOnWindowFocus: true`.
- **ETag/conditional GET needs no client code:** with `Cache-Control: no-cache` and an `ETag`, the browser revalidates with `If-None-Match` and hands script the cached body on 304; `fetch`/XHR do not expose the 304 (S82). The server computes the ETag from the snapshot generation (06 section 6.4). Do not hand-roll `If-None-Match` in the client (my first draft did; it is redundant, E9 client sketch).
- **Staleness display:** two clocks. (1) client: `dataUpdatedAt`, `isFetching`, `isStale`, `fetchStatus === "paused"` (offline); (2) server: `freshness {status: fresh|stale|error|unknown, asOf, fetchedAt}` on every list/detail response (field names align with 06 sections 6.2/14). UI: a `FreshnessBadge` (icon + text + tooltip; never colour alone) in the header and per panel: "Updated 2 min ago - GitHub data from 4 min ago - 2 sources delayed". Times rendered with `<time dateTime>` and `Intl.RelativeTimeFormat`, advanced by one shared 30 s ticker (`useSyncExternalStore`); only state *changes* are announced via `aria-live="polite"`, never the ticking text.
- **SSE as an invalidation channel (v1.1, optional)**, consistent with `06` section 6.2: `event: invalidate`, `id: <generation>`, `data: {"topics":["commits","groups","docs:{repo}","manifests","status"],"generation":123}`; a first `event: hello` carries `{generation, buildId}`; comment lines `: hb` every 20 s. Client: `EventSource('/api/v1/events')`; on `invalidate` call `queryClient.invalidateQueries({ queryKey: [topic] })`; on `error` fall back to polling (EventSource reconnects by itself and resends `Last-Event-ID`; GET only, no custom headers; one connection per tab, HTTP/1.1 allows about 6 per origin across tabs, HTTP/2 about 100 streams) (S83). Close the stream when the tab has been hidden >60 s. Prototype E12: a Go handler using `http.NewResponseController(w).SetWriteDeadline(time.Time{})` kept streaming past `Server.WriteTimeout=10s` (9 events in 14 s) and the stream passed unbuffered through the Vite dev proxy. Compression middleware must skip `text/event-stream` (06 section 3.4).
- **Version skew detection** (replaces most of the value of production response validation): every response carries `X-SCP-Build`; `hello` and `GET /api/v1/site` return `buildId`; when it differs from `import.meta.env.VITE_BUILD_ID`, show "A new version is available - reload". Combined with the `vite:preloadError` reload (S16).
- Not used: `streamedQuery` (I could not load its documentation page, HTTP 500, UNVERIFIED), `@tanstack/react-query-devtools` in production.

### 2.8 Forms and validation (Zod, React Hook Form)

SCP v1 has no data-entry forms (the v1 scope in 04 section 2 needs only filters, search, theme and a read-only admin status page). Filters are URL state (router search params with Zod), not form state. So: **no react-hook-form in v1.** If forms appear (private-repo access settings, feedback): `react-hook-form` 7.90 + `@hookform/resolvers` 5.9 + Zod 4 + shadcn `Field`/`Input Group` is the default; TanStack Form v2 is still alpha (S36), React 19 Actions (`useActionState`) suffice for trivial forms (S6).

Zod 4.6 is used for: router search/params validation (Standard Schema, no adapter with Zod 4, S42), parsing `import.meta.env`, and generated response validators in dev/test (section 3.6). Size (E1): full `zod` +23.1 KB gz, `zod/mini` +5.4 KB gz; prefer `zod/mini` for route search schemas in the initial chunk only if the budget is tight (Standard Schema keeps both interchangeable, S42). A gotcha from the generated output (E9): `date-time` becomes `z.iso.datetime()`, which by default **rejects offsets** (`+02:00`), so the Go side must serialise every `time.Time` as UTC (`.UTC()`, RFC 3339 `Z`).

### 2.9 Graph visualisation for the org-wide dependency graph (~1,000 nodes)

Method (E6): synthetic graph, 1,000 nodes in 20 clusters, 2,500 edges (80% intra-cluster), positions precomputed with d3-force, same positions for every library; Chrome 155 via Playwright on an Apple-silicon laptop with a 120 Hz display (so an 8.3 ms frame is vsync-bound and says "no slower than 120 fps"); CPU throttled 6x via CDP; medians of 5 page loads; measure mount (construct + 2 animation frames) and 90 programmatic pan/zoom frames. Raw data: `08-prototypes/graph-bench/results.json`.

| Library (version) | Rendering | Bundle (gz, incl. graph data lib, E1) | Mount 1x / 6x | Frame avg 6x (p95) | Notes |
|---|---|---|---|---|---|
| **sigma 3.0.3 + graphology 0.26** | WebGL | **35.8 KB** | 24 / 79 ms | 8.2 ms (9.7) | stable, `@react-sigma/core` 5.0.6 targets sigma ^3 (S1); canvas = no DOM a11y |
| sigma 4.0.1 | WebGL 2 only, SDF labels | 104.3 KB | 76 / 237 ms | 8.3 ms (9.9) | released 2026-10-08/13, rewrote the programs system, breaking vs v3 (S85); wait |
| cytoscape 3.34.3 | canvas | 132.8 KB | 85 / 555 ms | 19.1 ms (45.9) | mature, many graph algorithms built in; heavy |
| @xyflow/react 12.12 (all nodes) | DOM/SVG, React nodes | 120.7 KB (+55 KB over React) + CSS | 150 / 1,006 ms | 50.2 ms (62.4) | best for editors and rich custom nodes; `onlyRenderVisibleElements` helps: 72 / 447 ms, 28 ms |
| d3-force + d3-zoom + canvas (hand-rolled) | canvas 2D | 18.6 KB | 12 / 9 ms | vsync | no interaction model, labels, hit-testing: you write them |
| visx 4.0 (shape+scale+axis) | SVG primitives | +19.3 KB over React | n/m | n/m | a toolkit for charts, not a graph engine |

Caveats: xyflow renders real labelled React nodes (functional UI) while the canvas/WebGL candidates drew bare dots and edges, so the xyflow gap is partly work done, not only overhead; panning only, no hover/selection/label rendering; one machine. The ranking by cost per node is still unambiguous. The xyflow docs describe the performance problem only qualitatively ("a large number of nodes", memoise, collapse trees; no threshold or mention of `onlyRenderVisibleElements`) (S86).

**Recommendation:** **sigma 3.0.3 + graphology 0.26 for the explorer** (overview + focus on a repo: highlight neighbours, dim the rest; node size = number of dependants), lazy-loaded route chunk (~36 KB gz + glue), own 60-line React hook (do not depend on `@react-sigma/core` for v1). **Fallback: cytoscape 3.34.3** if sigma 3 turns out to lack a needed feature; revisit sigma 4 after two minor releases. **Do not use @xyflow/react for the 1,000-node view**; it is a reasonable choice only for a future editable or ≤150-node view with rich cards.

**Layout** (E7, Node 26, same graph): d3-force 300 ticks = **0.61 s** for 1,000 nodes/2,500 edges; dagre (layered): 100 nodes 10 ms, 250 nodes 35 ms, 500 nodes 279 ms, **750 nodes (1,386 edges) 3.0 s, 1,000 nodes (2,500 edges) did not finish in 90 s** (dagre scales super-linearly; D-15 graphs are DAG-like so layered layout is tempting); elkjs layered 1,000 nodes 2.06 s but 1.6 MB raw / 467 KB gz bundle (and an EPL-2.0/GPL licence choice). So: run d3-force (or graphology ForceAtlas2) in a **Web Worker** (`new Worker(new URL(...), {type: "module"})`; CSP `worker-src` falls back to `script-src 'self'`), cache the resulting positions per `snapshot generation + filter` in memory/IndexedDB; use dagre only for server-limited neighbourhood subgraphs (depth <= 2, <= 150 nodes). The server must cap the node count (filters: ecosystem, group, focus+depth, "internal only") and return `truncated: true` rather than 5,000 nodes.

**Accessibility (non-negotiable for a canvas):** the graph is an enhancement; every fact must also exist in the "Dependencies" table (producer, consumers, versions) and a keyboard-operable tree ("used by" / "depends on"). Provide zoom/pan buttons (no drag-only operation, WCAG 2.5.7), a text description, and `prefers-reduced-motion` (no animated layout).

### 2.10 Charts

- shadcn `chart` = Recharts 3 wrapper with CSS-variable colours and an `accessibilityLayer` (S26). Cost measured (E1): Recharts bar chart +97.2 KB gz over React; therefore **lazy-load** (a route-level chunk for pages that need charts) and keep the dashboard free of it.
- Use charts only where they carry information: commit activity per group/repo, ecosystem distribution, licence mix, version spread, sync-age history. Dashboard card sparklines: hand-written inline SVG components (~1 KB), no library.
- Alternatives considered: visx 4.0.0 (+19.3 KB gz, DIY axes, MIT), TanStack Charts 1.0 (released 2026-10-05; "SVG is my default", bundle-size minded, API stability promised with migration paths, no accessibility statement; too new to adopt, S36). 
- Colours: the default chart tokens are greys (`--chart-1: oklch(0.87 0 0)` is 1.48:1 on white, E13); define a categorical palette with >=3:1 against the surface and add non-colour encodings (labels, patterns, direct labelling).

### 2.11 Icons, fonts

- **lucide-react 1.56** (ISC): tree-shaken, 5 icons add about 2 KB gz (E1). **v1 removed all brand icons; `Github` does not exist** (checked: `lucide-react` exports 6,422 icons and `Github` is not among them, E2; S109 says brand icons were removed and recommends Simple Icons). SCP links to GitHub constantly: use `@primer/octicons-react` `MarkGithubIcon` (MIT) or an inline SVG component. GitHub's logo-usage terms were not checked (UNVERIFIED). `aria-hidden` is now `true` by default on Lucide icons (S109): icon-only buttons need `aria-label` (axe `button-name`).
- **Fonts, self-hosted:** Fontsource variable fonts (OFL-1.1), Geist is what `shadcn init` emits (five unicode-range subsets; the latin file is 29.4 KB woff2, E2). The browser downloads only the subsets used; æøå are in the latin subset. `font-src 'self'`. Zero-cost alternative: the system UI stack (no files, no FOUT). Mono font only if code blocks need it; otherwise `ui-monospace` stack.

---
### 2.12 Mermaid under a strict CSP

D-16 allows Mermaid "rendered client-side from fenced blocks under CSP-safe configuration". Measured (E5; mermaid 12.2.0, Chrome 155, policy `default-src 'self'; style-src 'self'` served by `cspserve.mjs`):

| Approach | Result |
|---|---|
| (A) `securityLevel: "strict"`, inline `<svg>` via `innerHTML` in the main page | Works functionally but **violates `style-src-elem` and `style-src-attr` constantly**: 72 console errors and 216 `securitypolicyviolation` events (201 `style-src-attr`, 15 `style-src-elem`) for one flowchart + one sequence diagram; the flowchart SVG carries 23 `style=""` attributes and one `<style>` element. Worse, the layout **differs** from the unrestricted run (flowchart viewBox `4 4 606.5 222` under CSP vs `4 4 574.5 190` unrestricted) because Mermaid measures text with styles that the CSP blocked. Not acceptable: a wrong layout with a silent console error. |
| (B) render, then show the SVG as `<img src="data:image/svg+xml,...">` | Image displayed (213 x 150 for the sequence diagram) but Mermaid still executed in the main page with the same violations and layout skew; also needs `img-src data:` in the main CSP. |
| **(C) Mermaid runs in a sandboxed iframe** (`<iframe sandbox="allow-scripts" src="/mermaid-frame.html">`), host sends the diagram source with `postMessage`, frame replies with the SVG string, host shows it as an `<img>` | Layout identical to the unrestricted run (viewBox `4 4 574.5 190`). The frame is served with its **own** policy: `sandbox allow-scripts; default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'` (no network, opaque origin, no cookies/storage access). Main page: policy unchanged. |

The 4 `style-src-attr` events I still saw in the *host* page in run (C) are most likely from my own test markup (`style="display:none"` on the iframe element in the host HTML); I did not isolate them (UNVERIFIED), and a production host would use a class.

**Decision:** run (C). Details and costs:
- Output is an image: not selectable, not themeable by the page's CSS, no links inside the diagram. Always render an accessible fallback: the diagram source in a collapsed `<details>` plus a text `alt`/`aria-label`; theme (light/dark) is a Mermaid `theme` option re-rendered on theme change.
- Weight: `import mermaid from "mermaid"` as one entry is **5.0 MB raw / 1.39 MB gzip** (E1); loaded lazily by diagram type the flowchart + sequence page fetched **30 JS files, 2.19 MB raw** (E5). It must be its own asset set, loaded only inside the iframe and only when a rendered document contains a Mermaid block; never in a route chunk of the main app.
- Security: Mermaid has had CSS-injection CVEs (CVE-2026-41148, CVE-2026-41149, titles only from a search result; fixed in 11.15.0 per a secondary source, S96, UNVERIFIED primary source). `securityLevel: "strict"` + sandboxed iframe with `connect-src 'none'` bounds the damage. The `postMessage` handler in the host must check `event.source === frame.contentWindow` (the sandboxed frame's origin is `null`, so origin checks are useless) and accept only `{id, svg|error}`; the SVG is placed in an `<img>`, never parsed into the DOM.
- Go side: the sanitiser (bluemonday) must keep `<pre><code class="language-mermaid">` as is (the language class is already emitted by goldmark); the SPA finds those nodes in the rendered doc container and replaces them. Without JavaScript the reader still sees the diagram source (LLM-friendly, D-06).
- `/mermaid-frame.html` and its assets are static files in `web/dist` served with an extra response header (`Content-Security-Policy` for the frame, `X-Frame-Options` not needed since the main policy has `frame-src 'self'`); this makes the Go `webapp` handler apply a per-path header table (design, not prototyped).
- **Recommendation for v1 scope:** ship docs without client-side Mermaid (fenced block shown as code with a "diagram source" label) and add the sandbox renderer in v1.1; it is isolated, lazy and does not change anything else. Server-side rendering of Mermaid is not evaluated (would need a headless browser or a Go port; I found no source for a maintained Go renderer, UNVERIFIED).

### 2.13 Client state, theming, URL state

- **State layers:** (1) server state: TanStack Query; (2) URL state: router search params (filters, sort, cursor, selected node, tab); (3) ephemeral UI state: `useState`/`useReducer` in the component; (4) persisted per-viewer convenience: `localStorage` (theme, sidebar collapsed, table column visibility) behind a tiny `usePersistedState` hook with try/catch (private windows throw). No global store: Zustand 5.0.15 would add +0.5 KB gz (E1: 66.3 vs 65.8) but there is no state that is both global and not server/URL state in the v1 scope (04 section 2). Revisit only for the graph explorer if its state grows (sigma state lives in the sigma instance anyway).
- **Theme**: shadcn tokens in `:root` and `.dark`; mode = `light | dark | system`, stored in `localStorage`; `prefers-color-scheme` as the default. The pre-paint script that avoids a flash must not be inline under `script-src 'self'`: ship it as a **separate tiny file `/theme-init.js`** (about 300 bytes, `<script src>` blocking in `<head>`, `no-cache`), or have Go inject a hash into the CSP at startup (D-16 allows either; 06 section 4.3 prefers a nonce). The file approach needs no template logic in Go, costs one cached request. Design only: I verified the generated `ThemeProvider` toggling (E3) but not a pre-paint script (UNVERIFIED in a browser). Set `<meta name="color-scheme" content="light dark">` and `color-scheme` in CSS so scrollbars and form controls follow.
- **URL as the source of truth for lists:** every filter in a list page is a validated search param; pagination keeps cursors in the URL (`?cursor=...`), so back/forward and shared links work; TanStack Query uses `placeholderData: keepPreviousData` to avoid layout jumps.

### 2.14 i18n (English now, ready for more)

- Requirement (D-16): "i18n-ready strings (English now)". Candidates measured in the same hello app (E1, gzip delta over the React baseline 65.8 KB): **Lingui 6.9: +2.6 KB** (68.4), react-intl 12: +13.6 KB (79.4), i18next 26 + react-i18next 17: +15.5 KB (81.3). Lingui compiles ICU messages ahead of time (macros `t`, `Trans`, `plural`), keeping the runtime tiny; the `@lingui/vite-plugin` has a `macroTransform` option for non-Babel pipelines (S108). Lingui 6 needs Node >= 22.19 (S1). **Recommendation: Lingui 6**; message catalogues in `web/src/locales/en/messages.po` (PO, human-reviewable) compiled at build; locale files are lazily imported.
- Not verified end to end: the Lingui macro/Vite 8/React Compiler combination in a real build (only the runtime size was measured; UNVERIFIED). If the plugin fails with Rolldown, fall back to `react-intl`/`i18next` (+13-15 KB) without architecture change, because all strings go through one `t()` wrapper.
- Formatting: `Intl.DateTimeFormat`, `Intl.RelativeTimeFormat`, `Intl.NumberFormat`, `Intl.ListFormat` (all baseline), time zone = viewer's, shown in UTC in tooltips (`<time dateTime>`; the API sends RFC 3339 UTC). Not translated: repository names, commit messages, README content, API `code` values. API problem `title` is English; the SPA maps stable `code` values to localised strings and falls back to `title`.
- Rules (lint): no string concatenation for sentences; no hard-coded user-visible strings outside `t`/`Trans` (the `lingui/no-unlocalized-strings` rule exists in `eslint-plugin-lingui` - not evaluated, UNVERIFIED); plurals via ICU; RTL not required now (use logical properties: Tailwind 4 has them, S17).

### 2.15 Testing strategy

All tools below were installed and exercised in the spike (E10) unless stated.

| Layer | Tool | What it covers | Notes |
|---|---|---|---|
| Unit/logic | **Vitest 5.0.4** project `unit` (jsdom 30) | pure functions, hooks, formatting, zod schemas, reducers | 2 tests, 1.9 s total including MSW (E10) |
| Component (real browser) | Vitest 5 browser mode, `@vitest/browser-playwright` 5.0.4 + `vitest-browser-react` 2.3.0, project `browser`, files `*.browser.test.tsx` | anything with overlays/focus/layout (Dialog, Select, Command, Tooltip, Sidebar) | jsdom has no layout/popover/focus fidelity for Floating UI; the browser project ran against local Chrome (E10). Whether Base UI overlays work in jsdom I did **not** test (UNVERIFIED), so the policy is: overlays go to the browser project |
| Network mocking | **MSW 3.0.3** (`msw/node`; `setupServer(...)`, `server.listen()`), handlers typed from the generated `paths` | unit/component tests and Storybook-less development | MSW 3 is ESM-only, Node >= 22.12 (S106, S1). `server.listen({onUnhandledRequest: "error"})` is a **type error** in v3 (E10); the v3 equivalent of strict unhandled-request handling was not located (UNVERIFIED), do not copy v2 snippets. `openapi-msw` 2.1.0 (typed handlers from `paths`) exists but was not tried |
| Contract (server) | Schemathesis 4.31 + oasdiff (section 3.7) | Go handlers vs OpenAPI | runs in Go CI |
| E2E + a11y + CSP | **Playwright 1.64** against the real Go binary built with `-tags spa` and a seeded snapshot fixture; `@axe-core/playwright` 4.13 with tags `wcag2a, wcag2aa, wcag21a, wcag21aa, wcag22aa` (S100); a global `securitypolicyviolation` listener that fails the test | every page in section 6, light and dark, 360 px and 1280 px, keyboard-only paths, route-link crawl (replaces the Go route-table crawl test, D-16) | axe tag `wcag22aa` exists (S100). Spike result: axe reported only my test-markup defects and put `color-contrast` and `target-size` in "incomplete" (E17); automated checks are necessary, not sufficient |
| Visual regression | none in v1 | | add Playwright screenshots for 4 key pages once the design is stable |
| Storybook | **not adopted** | | Storybook 10.6.2 exists (S1); with ~30 owned components, Vitest browser tests give the same isolation without a second build toolchain and a second Vite config; D-16 left it optional |

Test file conventions: next to the code (`foo.test.ts`, `foo.browser.test.tsx`), e2e in `web/e2e/`. Coverage thresholds are not set (cheap to game, not evidence of quality); mutation testing is out of scope.

### 2.16 Lint, format, boundaries

- **ESLint 10.12 flat config** (v9 EOL 2026-08-06, S103) + typescript-eslint 8.71 (type-aware rules need the TS6 alias, 2.2) + `eslint-plugin-react-hooks` 7.1 (incl. the React Compiler rules, S5) + `eslint-plugin-jsx-a11y` 6.10.2 (no release since 2024-10-26; its peer range ends at ESLint 9 but it ran without error on 10 and flagged a missing `alt`, E10 - accept, monitor; `@eslint-react/eslint-plugin` not evaluated) + `eslint-plugin-react-refresh` (what `shadcn init` emits). **Prettier 3.9** + `prettier-plugin-tailwindcss`.
- Biome 2.5.15 exists (S102, version only from the registry) and would replace ESLint+Prettier in one binary, but D-16 requires feature-boundary enforcement (`eslint-plugin-boundaries` or equivalent), and I did not find a Biome equivalent; Biome's type-aware rules are also not comparable to typescript-eslint's (UNVERIFIED). **Decision: ESLint + Prettier.**
- **Feature boundaries (D-16)**, verified (E11) with `eslint-plugin-boundaries` 7.2.0 using its policy DSL (`boundaries/dependencies`, S104) and `eslint-import-resolver-typescript` for the `@/` alias: a feature may import itself, `shared/`, `lib/`, and **other features only through their `index.ts`**; `shared` may not import features; `lib` imports only `lib`. Observed: deep import `@/features/a/internal` from feature b: error; `shared -> features/b`: error; `@/features/a` (index): allowed. The config is in `08-prototypes/eslint-boundaries/eslint.boundaries.config.js`. Gotchas: use folder patterns for `shared` and `lib` (file-level patterns left files unclassified); the plugin's older `element-types` rule/`boundaries/entry-point` are replaced by `dependencies` policies in v7 (S104).
- Further project rules (all `no-restricted-*`, design not prototyped):
  - `fetch(`/`XMLHttpRequest`/`EventSource` only inside `lib/api`;
  - `dangerouslySetInnerHTML` and `.innerHTML` only inside `shared/safe-html.tsx`;
  - string literals matching `^/(api|repos|groups|commits|badges)` forbidden outside `routes.ts` and `lib/api` (no hard-coded URLs, D-16);
  - `useEffect` data fetching forbidden (use Query);
  - `eval`, `new Function`, `document.write`.
- **Dead code and dependency hygiene:** knip 6.40 in CI; `pnpm audit` is advisory only (the source of truth is the cooldown + lockfile review, 2.18).
- Prettier formatting is checked, not auto-fixed, in CI (`prettier --check`).

### 2.17 TypeScript project layout, scripts

- Project references are not needed (single app); `tsc --noEmit -p tsconfig.app.json` (TS 7) is the type gate, `tsc6` available for tools. `vite build` does not type-check (Oxc strips types).
- `package.json` scripts (all names stable, used by CI and Docker):

```jsonc
{
  "scripts": {
    "dev": "vite",
    "gen": "pnpm gen:api && pnpm gen:routes && pnpm gen:i18n",
    "gen:api": "redocly bundle ../api/openapi.yaml -o ../api/openapi.bundled.yaml && openapi-typescript ../api/openapi.bundled.yaml -o src/lib/api/schema.d.ts",
    "typecheck": "tsc --noEmit -p tsconfig.app.json",
    "lint": "eslint . && prettier --check . && knip",
    "test": "vitest run",
    "test:browser": "vitest run --project browser",
    "e2e": "playwright test",
    "build": "pnpm gen && vite build && node scripts/precompress.mjs && node scripts/routes-manifest.mjs",
    "size": "size-limit"
  },
  "engines": { "node": "^24.21.0 || ^26.0.0" },
  "devEngines": { "packageManager": { "name": "pnpm", "version": "12.10.1", "onFail": "error" } }
}
```

(`devEngines.packageManager` with `onFail` is how pnpm 11+ pins itself without corepack, S56; the `scripts/precompress.mjs` shown in `08-prototypes/shadcn-csp-spike/scripts/` has zero dependencies; the routes manifest script is the 15-line walker from E9.)

### 2.18 Package manager, Node pin, supply chain

- **pnpm 12.10.1.** pnpm 11+ moved settings to `pnpm-workspace.yaml` and stopped reading most `.npmrc` keys (S56). `shadcn init` already emitted a `pnpm-workspace.yaml` with `allowBuilds` (E2).
- **Node:** `.nvmrc`/`.node-version` = `24.21.0` now, then `26` (LTS from 2026-10-28; 24 enters Maintenance on 2026-10-20, S58, S59). `engines` as above. **Corepack is not shipped from Node 25** (search result, S119; not read in a primary source, UNVERIFIED), so do not rely on `corepack enable`: CI uses `pnpm/action-setup` (v6.x documented to work with pnpm 11 and 12, S57) or the standalone installer; the Dockerfile installs pnpm via `npm i -g pnpm@12.10.1` or the standalone script with a checksum.
- **Policy (S53, S54; the settings exist in pnpm 12 docs, I did not test each one against a hostile package):**

```yaml
# pnpm-workspace.yaml
minimumReleaseAge: 10080          # minutes = 7 days; new versions are not installable until a week old
trustPolicy: no-downgrade         # refuse a version published with weaker trust evidence than before
blockExoticSubdeps: true          # transitive deps may not come from git/tarball URLs
strictDepBuilds: true             # install scripts run only for packages listed in allowBuilds
allowBuilds: {}                   # explicit, reviewed allow-list of packages that may run install scripts (shadcn init generated one, E2)
```

  plus: exact versions in `package.json` (no `^`), `pnpm install --frozen-lockfile` in CI, committed `pnpm-lock.yaml`, Renovate with `minimumReleaseAge` and grouped PRs for the frontend, GitHub Actions pinned to **full commit SHAs** (versions seen on 2026-10-13: `actions/checkout` v7.0.1, a v7.0.0 and a v7.1.0 for `actions/setup-go`/`actions/setup-node` - the page-to-action mapping of the last two was ambiguous in my fetches, resolve the SHAs at implementation; S115, S116, S117), `permissions: contents: read` by default, no `pull_request_target` that checks out fork code, `npm audit signatures`/provenance verification where available (the shadcn `cn` package has a provenance attestation, S30/S1).
- **Why this is not paranoia:** the TanStack Router/Start monorepo was compromised on 2026-05-11 (42 packages, 84 versions, removed within hours; Query/Table/Form/Virtual unaffected, S37) and pnpm's own 2025-12 newsroom post describes the worm-style attacks and these mitigations (S54). The routing library we depend on is exactly the one affected, which is why the 7-day cooldown is a must (the malicious versions would not have been installable).
- Runtime dependencies are only what ships in the bundle; everything else (`shadcn` CLI, `@redocly/cli`, ESLint, Playwright) is `devDependencies`. `pnpm why`/`knip` check this; the shadcn CLI move to `devDependencies` is mandatory (2.5, finding 1).

### 2.19 Performance and bundle budgets

Measured building blocks (gzip KB unless stated; E1 hello-world apps built by Vite 8/Rolldown, one entry each, so shared code is counted once per entry; deltas are upper bounds):

| Block | KB gz | | Block | KB gz |
|---|---|---|---|---|
| React 19.3 + react-dom | 65.8 | | TanStack Router | +25.1 |
| TanStack Query | +9.3 | | TanStack Table 9 | +10.7 |
| @tanstack/react-virtual | +7.8 | | Zod 4 / zod mini | +23.1 / +5.4 |
| openapi-fetch | +2.5 (alone 2.5) | | lucide (5 icons) | about +2 |
| Base UI + 8 primitives | +59.2 | | Radix + 8 primitives | +34.9 |
| Recharts bar chart | +97.2 | | sigma 3 + graphology | 35.8 total |
| DOMPurify | 11.1 | | Lingui | +2.6 |
| CSS (25 shadcn comps, Tailwind) | 15.5 (+1.9 typography) | | Geist variable font latin subset | 29.4 woff2 |

A full-feature build of the shadcn spike (about 25 components, Base UI, all in one chunk, no router) gave 563.9 KB raw / **182.7 KB gz** JS without the chart (E14); the build with Recharts is larger, which is why charts are lazy.

**Proposed budgets** (a proposal derived from the table, not measured on the real app; `size-limit` fails CI when exceeded):

| Asset | Budget (gz) | Basis |
|---|---|---|
| Initial JS for `/dashboard` (entry + shared chunks) | **<= 200 KB** | 65.8 React + 25 router + 9 query + ~40 Base UI subset + ~5 zod mini + ~40 app code + headroom |
| Initial CSS | <= 25 KB | measured 17.4 with typography |
| Any lazy route chunk | <= 60 KB | table/cards/forms |
| Graph chunk | <= 70 KB | sigma 36 + graphology + glue |
| Charts chunk | <= 120 KB | Recharts +97 |
| Mermaid sandbox assets | out of budget, loaded only in the iframe | 1.39 MB gz as one entry |
| Fonts | <= 100 KB woff2 in total, `font-display: swap`, preload only the latin subset | |
| Total transferred on a cold `/dashboard` load | <= 350 KB incl. fonts and API JSON | |

Targets: Core Web Vitals "good" at p75 (LCP <= 2.5 s, INP <= 200 ms, CLS <= 0.1; S92, page last updated 2024-10-31, so confirm the current set); the SPA's first render needs HTML -> JS -> `/api/v1/...` (three sequential round trips); mitigate with `<link rel="modulepreload">` (Vite emits it), router `defaultPreload: "intent"`, and **a Go-side `Link: </api/v1/...>; rel=preload; as=fetch` hint only if measurement shows a need** (design idea, untested). No SSR (D-16); a skeleton paints at once from the HTML shell (the shell contains only the root div, the `<title>` and a CSS-only skeleton).
- Lighthouse in CI: `lighthouse` 13.5 CLI/API against the Go binary with a `budget.json`, since `@lhci/cli` last released 2025-06-25 (S1, S112). Treat Lighthouse as a regression alarm (run 3 times, median), not as a pass/fail on absolute scores; field data does not exist for an internal portal.
- Memory/long-lived tab: TanStack Query `gcTime` 5 min default; the graph page destroys the sigma instance on unmount; SSE connection closed when hidden.

### 2.20 Accessibility (WCAG 2.2 AA) as an implementation plan

W3C WCAG 2.2 (S91) adds, relative to 2.1: 2.4.11 Focus Not Obscured (Minimum), 2.5.7 Dragging Movements, 2.5.8 Target Size (Minimum, 24 x 24 CSS px), 3.2.6 Consistent Help, 3.3.7 Redundant Entry, 3.3.8 Accessible Authentication (Minimum); 4.1.1 Parsing was removed. For SCP:

| Concern | Implementation |
|---|---|
| Landmarks and skip link | one `<header>`, `<nav aria-label>`, `<main id>`, skip link as the first focusable element; sticky header: `scroll-padding-top` equal to its height so focused elements are not obscured (2.4.11) |
| Route changes (SPA) | on router `onResolved`: set `document.title` (React 19 `<title>` per route), move focus to the page `<h1>` (`tabindex="-1"`), and update a polite live region ("Navigated to ..."); TanStack Router has no built-in announcer as far as I know (UNVERIFIED) |
| Colour/contrast | token overrides from 2.5 finding 4; test computes contrast for all token pairs in both themes; status never by colour alone (icon + text) |
| Target size (2.5.8) | icon buttons >= 24 px (shadcn `icon` size is 36 px by default, `icon-xs` smaller: check); table row actions with spacing |
| Dragging (2.5.7) | graph pan/zoom has buttons; column resizing, if used, has a keyboard alternative; no drag-only interaction anywhere |
| Keyboard | Base UI/Radix handle roving focus, escape, typeahead; command palette `Ctrl/Cmd+K` plus a visible button; no keyboard traps in the sandbox iframe (decorative `<img>` outside it) |
| Live data | freshness text is not live; only *state changes* (fresh -> stale, error) are announced `aria-live="polite"`; skeletons carry `aria-busy` on the region and a visually hidden "Loading ..." |
| Tables | real `<table>` with `<caption>`/`aria-label`, `scope`, sort state via `aria-sort` on the `th` button; shadcn Data Table guide uses Table + TanStack (S25), keep native semantics, virtualise only >200 rows (virtualised tables need explicit `aria-rowcount`/`aria-rowindex`) |
| Graph | canvas is not accessible: table + tree alternative, 2.9 |
| Rendered docs | headings hierarchy preserved from Markdown (server adds `id`s; the Go sanitiser keeps them), skip-to-section TOC as a `<nav>`, `<pre>` scrollable regions are focusable (`tabindex=0`, labelled) so keyboard users can scroll them, images keep `alt`, GitHub alerts (`> [!NOTE]`) rendered with a text label not just colour |
| Motion | `prefers-reduced-motion` disables View Transitions, graph animations, skeleton shimmer |
| Zoom/reflow | 320 CSS px reflow (1.4.10), text spacing (1.4.12): no fixed heights on text containers; verified by an e2e screenshot at 360 px |
| Tooltips/popovers | 1.4.13: dismissible, hoverable, persistent. Base UI Tooltip behaviour not tested (UNVERIFIED) |
| Forced colors | `@media (forced-colors: active)`: borders on cards/inputs, focus ring via `outline` (not `box-shadow`, which disappears) |

Verification: axe `wcag22aa` in every e2e page test; the token-contrast unit test; a manual checklist per release (keyboard-only walk-through of the 8 main flows, 200%/400% zoom, one screen reader pass with VoiceOver and NVDA - not performed in this research).

### 2.21 Build, CI, Docker, go:embed, dev proxy

**Repository layout.** `web/` (SPA), `api/openapi.yaml` (+ bundled output), `internal/webapp`, `internal/httpapi`, feature packages with `api/*.yaml`.

**`go:embed` with a placeholder (verified, E18/E8):**

```go
// web/embed_placeholder.go
//go:build !spa
//go:embed placeholder          // one tiny index.html; `go test ./...` works without Node
// web/embed_spa.go
//go:build spa
//go:embed all:dist             // real build: `go build -tags spa ./cmd/scp`
```

Verified: `go test ./...` passes with the placeholder; `go test -tags spa` compiled the embed against a populated `dist` (E18: `SPA_TAG_OK`). `all:` is required so files starting with `_` or `.` are embedded. The handler (`internal/webapp`, 70 lines, tests pass, E18) does: GET/HEAD only; never falls back for API prefixes (404 there); exact file hit -> `Cache-Control: public, max-age=31536000, immutable` for `assets/*`, `no-cache` otherwise; `.br`/`.gz` negotiated from sibling files with `Vary: Accept-Encoding` and the right `Content-Type`; an unknown path **with** an extension is a real 404, **without** one gets `index.html` (200, `no-cache`); `X-SCP-Build` on every response. Gaps to close in the real package: HTTP status for deep links to unknown *routes* (the server cannot know; use the route manifest `routes.json` to answer 404 for non-matching shapes, 06 section 3.2), `Content-Length` for the in-memory index, ETag for non-hashed files (`http.ServeContent` provides it on the compressed path only when a modtime is given; embedded files have zero modtime, so set an ETag from the build id), per-path headers (Mermaid frame CSP), and the `/theme-init.js` file.

**Dev loop:** `pnpm dev` (Vite on :5173) proxying `/api`, `/badges`, `/media`, `/llms.txt` to the Go server on :9090 (config in 2.3). SSE through the Vite proxy streamed without buffering (E12). CSP is **not** enforced by the dev server (HMR needs inline module code); the CSP is tested by the e2e job against the real binary. Alternatively `air`/`watchexec` for the Go side; not evaluated.

**CI jobs** (GitHub Actions; design, YAML not executed):

1. `web`: setup Node (pinned) + pnpm; `pnpm install --frozen-lockfile`; `pnpm gen` then `git diff --exit-code` (drift check for generated TS, section 3.6); `typecheck`, `lint`, `test`, `test:browser`, `build`, `size`.
2. `go`: `go generate ./...` + `git diff --exit-code`; `go vet`, `go test -race ./...` (placeholder embed, no Node needed); `oasdiff breaking` against the base branch's bundled spec; Schemathesis against the binary started with a fixture snapshot.
3. `e2e`: needs the `web` build artifact; `go build -tags spa`; Playwright (Chromium) + axe + CSP-violation listener; upload traces.
4. `image`: Docker build, run as non-root read-only, smoke `/readyz` and `/`.

**Dockerfile (multi-stage, design not executed; the digests below are the Docker Hub index digests read on 2026-10-13, S60; re-read them at implementation):**

```dockerfile
FROM node:24.21.0-alpine@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1 AS web
WORKDIR /src
RUN npm i -g pnpm@12.10.1
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/
COPY api/ api/
RUN cd web && pnpm install --frozen-lockfile
COPY web/ web/
RUN cd web && pnpm build                      # -> web/dist (+ .br/.gz, routes.json)

FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist web/dist
RUN CGO_ENABLED=0 go build -tags spa -trimpath -ldflags "-s -w -X main.build=${BUILD_ID}" -o /scp ./cmd/scp

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /scp /scp
USER nonroot
ENTRYPOINT ["/scp"]
```

Notes: the Go stage compiles with `CGO_ENABLED=0` only if the chosen SQLite driver is pure Go (D-04 research; not my scope); the `.dockerignore` must exclude `web/node_modules` and `web/dist`; the Node stage layer caches the dependency install on lockfile changes only. Build id = git SHA, also passed to Vite as `VITE_BUILD_ID` so the client can detect version skew (2.7). An `alpine`/`distroless` final image choice is for the Docker research (06/07 docs); `distroless/static` has no shell (good for the read-only, non-root rule in D-09).

---
## 3. API contract: one recommended end-to-end approach

D-16 fixes the premise: OpenAPI 3.1 is the source of truth, handlers are verified against it or generated from it, the TypeScript client and types are generated, errors are RFC 9457, and "pagination, ETag/conditional GET and field naming conventions are fixed in the architecture". This section recommends the tools and writes the conventions down.

### 3.1 Go side: options compared (versions and dates from the Go module proxy, S77, and the cited project pages)

| Tool | Version (date) | Source of truth | OpenAPI 3.1 | Fit with D-05 (`net/http` ServeMux, stdlib first) and package-by-feature | Errors / SSE | Verdict |
|---|---|---|---|---|---|---|
| **oapi-codegen v2** | v2.8.0 (2026-07-17); runtime v1.7.0 | **spec-first** | **Initial support** (issue #373 closed 2026-06-20 by PR #2336; v2.8.0 notes list webhooks/callbacks, `oneOf` enums, `type: [T,"null"]`); multi-type unions such as `type: [string, number]` generate `any`; a June 2026 maintainer comment still noted problems with nullable types on the branch (S61-S63) | **`std-http-server`** (Go 1.24+ output; tool needs Go 1.25+), `StrictServerInterface` with typed request/response objects, `import-mapping` so each feature package generates its own server and shares a `common` package (verified, E8). 8.6k stars, no defined release cadence | RFC 9457 not built in (we define `Problem` in `common.yaml`); SSE not modelled (handwritten handler) | **Recommended** |
| ogen | v1.24.0 (2026-08-07) | spec-first | **Not stated** on its README (only "OpenAPI v3", "supported in OpenAPI 3.0+") (S64) | own generated static radix router, not `ServeMux`; no reflection, `jx` JSON; strong typed sum types | SSE client generation only (S64) | Reject for now: router and JSON stack diverge from D-05, 3.1 not confirmed |
| Huma v2 | v2.39.1 (2026-07-29) | **code-first** (spec generated from Go types; an explicit design-first stance is not on the pages I read) | emits OpenAPI 3.1 (S66) | router adapters ("bring your own router"); validation built in | **RFC 9457 `application/problem+json` by default** (`status`, `title`, `detail`, `errors[] {location, message, value}`, `$schema`); `sse` package; "conditional requests" package (S67, S68) | Good framework, but **contradicts the contract-first mandate** unless the owner changes D-16 to "spec generated from code and diffed". Keep as the documented alternative |
| Goa v3 | v3.34.0 (2026-10-07) | design-first in a **Go DSL** (second source of truth) | not stated on the page I read (S70) | generates HTTP/gRPC/JSON-RPC servers, clients, OpenAPI | SSE/WebSocket mentioned | Reject: DSL instead of OpenAPI as source |
| Connect (connect-go) | v1.21.0 (2026-09-08) | **protobuf** | none (OpenAPI generation not mentioned on the page) (S71) | protocol: Connect, gRPC, gRPC-Web; JSON via POST; CNCF sandbox | streaming supported | Reject: protobuf toolchain, POST-only RPC shape, no GET/ETag/LLM-friendly URLs |
| kin-openapi | v0.150.0 (2026-10-13) | library | README lists 3.1, partial 3.2; `openapi3filter` uses JSON Schema 2020-12 for 3.1 (S72) | request/response validation; basis of `oapi-codegen/nethttp-middleware` v1.2.0 | | **Use as the runtime validator** (E8) |
| libopenapi + libopenapi-validator | v0.41.6 / v0.16.1 (2026-10-10/11) | library | validation of 3.0, 3.1, 3.2 documents (S73) | validator is called directly on `http.Request`/`Response`; MIT; 147 stars, active | | Alternative validator if kin-openapi 3.1 handling disappoints |

Notes that shape the recommendation:
- OpenAPI 3.2.0 exists (a secondary source dates it 2025-09-19) and adds `itemSchema` for `text/event-stream` and other streaming types; **no generator in the table targets 3.2** (oapi-codegen README says 3.1; Schemathesis and kin-openapi are partial/complete for 3.2 validation only) (S74, UNVERIFIED primary source). Stay on 3.1 and describe SSE with a hand-written event schema (3.5).
- **oapi-codegen had three code-injection advisories in 2026** (v2.7.1 2026-06-05 GHSA-rjwr-m7qx-3fjr; v2.7.2 2026-07-07 GHSA-jwgf-6xff-4hvp and GHSA-xrqw-w576-xgj2); v2.8.0 validates the spec before generating (S62). Treat spec files as trusted source code (CODEOWNERS review, no remote `$ref`, pinned tool version, generated diff reviewed).
- v2.8.0 breaking notes that matter: on `net/http`, trailing-slash paths no longer act as catch-alls; security scopes are not emitted by default (S62).
- If oapi-codegen's 3.1 handling proves flaky: (1) restrict the spec to a 3.0-compatible subset of 3.1 (arrays for nullability are the only 3.1-only idiom we use), (2) the README documents an Overlay to downgrade a 3.1 spec to 3.0 (S61), (3) fall back to Huma code-first. Decision gate: the E8 prototype generated, compiled, served and validated the exact patterns SCP needs (cross-file `$ref`, nullable fields, `default` problem response, 304 with ETag header, strict responses).

### 3.2 TypeScript side: options compared

| Tool | Version (date) | What it produces | Notes |
|---|---|---|---|
| **openapi-typescript + openapi-fetch** | 7.13.0 / 0.17.0 (both 2026-02-11; previous release 2026-02-08, none in 8 months) | `paths`/`components` types only (5.9 KB `.d.ts` for the 3-operation prototype); a typed `fetch` wrapper of 2.5 KB gz with middleware | OpenAPI 3.0 and 3.1 (3.2 not mentioned) (S75). Peer `typescript ^5.x` but works on the TS 6 alias (E9). No runtime code generated, no coupling to TanStack Query: our own `queryOptions` factories wrap the client. **Recommended** |
| Hey API `@hey-api/openapi-ts` | 0.99.0 (2026-06-22), pre-1.0, "initial development, pin an exact version" (S76) | types, SDK functions, client, **Zod schemas** (`zod.gen.ts`, 2.2 KB for the prototype), TanStack Query plugins | Needs Node >= 22.18, Vite plugin for Vite 5-8; peer `typescript >=5.5.3 \|\| >=6.0.0`; worked with the TS 6 alias (E9). Use only if generated Zod validators are wanted (3.6) |
| Orval | 8.42.0 (S79) | React Query/SWR hooks, Axios/Fetch clients, Zod, **MSW mocks with Faker** | the page I read did not mention 3.1 support (UNVERIFIED); output is opinionated hooks, heavier; not adopted |
| Connect-ES | n/a | protobuf | rejected with Connect |

### 3.3 The recommended pipeline (all steps run in E8/E9 unless marked)

```
 internal/<feature>/api/<feature>.yaml      hand-written OpenAPI 3.1 fragments (paths + schemas), one per Go feature package
 internal/httpapi/openapi/common.yaml       shared Problem, Freshness, PageInfo, parameters, headers, responses
 api/openapi.yaml                           root: $ref's the path items of every feature fragment
        |
        |-- go generate: oapi-codegen v2.8.0 per fragment  ->  <feature>/api/*.gen.go   (models + std-http-server + strict-server;
        |                                                      import-mapping -> common package)         [E8: compiles, serves]
        |-- go generate: oapi-codegen for common.yaml with skip-prune + strict-server + std-http-server
        |                                                  (needed or the features' strict responses do not compile)    [E8]
        |-- redocly bundle api/openapi.yaml -> api/openapi.bundled.yaml        (single self-contained file)           [E8]
        |        |-- embedded in the binary, served at GET /api/v1/openapi.yaml (LLM/agent surface, 01 section 6.6)
        |        |-- oasdiff breaking <base> <head>                                                                      [E16]
        |        |-- schemathesis run ... --url <running binary>                                                          [E8]
        |        `-- openapi-typescript -> web/src/lib/api/schema.d.ts   (+ optional @hey-api/openapi-ts -> zod.gen.ts)  [E8/E9]
        |
 runtime: validator middleware (kin-openapi via oapi-codegen/nethttp-middleware v1.2.0, mounted under /api/ only)
          -> ServeMux (HandlerFromMux per feature) -> StrictHandler -> feature handler implements StrictServerInterface
```

Concrete configuration (from the prototype; the generated `*.gen.go` files are not kept in `08-prototypes/`, regenerate with the commands):

```yaml
# internal/commits/api/oapi.yaml           (same shape for every feature)
package: api
output: commits.gen.go
generate: { models: true, std-http-server: true, strict-server: true }
import-mapping:
  ../../httpapi/openapi/common.yaml: example.com/scp/internal/httpapi/openapi
# internal/httpapi/openapi/oapi.yaml       (the shared package)
package: openapi
output: common.gen.go
generate: { models: true, std-http-server: true, strict-server: true }
output-options: { skip-prune: true }       # keep every shared schema/response even if this file has no paths
```

```go
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -config oapi.yaml commits.yaml
// (prefer a `tool` directive in go.mod to pin it; the go run form is what I executed)
```

Handler shape (verified, E8): `func (h Handler) ListCommits(ctx, req api.ListCommitsRequestObject) (api.ListCommitsResponseObject, error)`; typed responses such as `api.ListCommits200JSONResponse{Body: ..., Headers: ...ETag}`, `api.ListCommits304Response{Headers: ...}`, `api.ListCommitsdefaultApplicationProblemPlusJSONResponse{StatusCode: 404, Body: openapi.Problem{...}}`. The strict interface makes it a compile error to return a status or body the spec does not declare, which is the "contract by construction" half; Schemathesis is the other half.

Wiring (verified, E8): `commitsapi.HandlerFromMux(commitsapi.NewStrictHandler(commits.Handler{...}, nil), mux)` for each feature on the **single** ServeMux owned by `cmd/scp`/`httpapi`; features never see each other's generated packages. Validator: `nethttpmw.OapiRequestValidatorWithOptions(doc, &nethttpmw.Options{ErrorHandler: ...})` loading the bundled spec with `loader.IsExternalRefsAllowed = true` (only for the multi-file dev case; production loads the bundled single file) and `doc.Servers = nil` so any host matches.

**Request validation in production, response validation only in tests.** Request validation rejects `limit=201`, a bad `groupId` pattern, etc. with problem+json before any handler runs (E8). Response validation costs CPU per request and only protects against our own bugs: do it in handler tests (`openapi3filter.ValidateResponse`, design) and Schemathesis. The validator must be mounted only below `/api/` (it answers 404 for paths the spec does not know, which would break SPA fallback and badges).

### 3.4 What the experiment found (E8, Schemathesis 4.31.0 against the prototype binary)

1. **Defect 1 (real):** with only generated stubs, `GET /api/v1/commits?limit=201` returned 200 although the spec says `maximum: 200`: "API accepted schema-violating request". Generated strict servers do **not** validate requests. Fixed by the validator middleware (a 400 `at '': maximum: got 201, want 200`).
2. **Defect 2 (real):** the validator's default error body is `text/plain`, which contradicts the documented `application/problem+json` (Schemathesis: "Undocumented Content-Type"). Fixed by the middleware's `ErrorHandler`, which writes a problem document. The production handler must also add `code`, `errors[]` and `instance` (06 section 14) by parsing the validator's error type rather than the message string (not prototyped).
3. After both fixes: Fuzzing 3/3 passed, Stateful 51 scenarios passed (links inferred from `groupId`), Coverage had **3 remaining failures in the "unsupported methods" check**; I did not isolate the cause. The likely candidate is Go's built-in plain-text 405 (06 section 14 notes the 405 path inside `/api/v1` was not prototyped); treat as an open item: all 404/405 under `/api/` must be problem+json with an `Allow` header on 405.
4. Prototype artefact: the prototype schemas used snake_case (`fetched_at`, `request_id`) because I wrote them before aligning with 06. **The production convention is camelCase** (3.5); Zod/TS output follows whatever the spec says.
5. `oasdiff` v1.33.0 (2026-09-16): `oasdiff breaking base.yaml head.yaml` on bundled 3.1 specs reported `1 changes: 1 error` - `[request-parameter-became-required] ... GET /api/v1/commits ... the 'query' request parameter 'group' became required`, i.e. it detects the kind of change that breaks existing clients (E16). The README page I read could not confirm the CI exit-code flag (`--fail-on ERR`); verify the flag in the oasdiff docs when wiring CI (UNVERIFIED).
6. The path item `$ref` into `internal/<feature>/api/*.yaml` from the root spec worked with kin-openapi (`LoadFromFile`), `redocly bundle`, openapi-typescript and oapi-codegen (E8).

### 3.5 Conventions (to be copied into the architecture document; they are consistent with 01 section 6.6, 05 section 3.5 and 06 sections 6 and 14 except where listed in "Corrections")

| Topic | Convention |
|---|---|
| Base path, versions | `/api/v1`; URL carries the **major** version only; additive changes stay in v1 (new optional response fields, new endpoints, new query parameters, new enum values); anything else is a v2 |
| Names | resources are plural nouns (`/groups`, `/repos`, `/commits`); `operationId` camelCase and unique (`listCommits`); one `tags` entry per feature package; JSON **camelCase** field names (as in 01/06: `fetchedAt`, `apiUrl`, `blobSha`); enum values lowercase, hyphenated; identifiers are strings; `groupId` pattern `^[a-z0-9-]+$` (lower-case only, so an id has exactly one spelling, GR-07/GR-08) |
| Time | RFC 3339 `date-time`, **always UTC with `Z`** (the generated Zod `z.iso.datetime()` rejects offsets, E9); durations as integer `...Seconds`; sizes in bytes |
| Null vs absent | absent = not applicable or unknown to this endpoint; explicit `null` only where the schema says `type: [T, "null"]` and means "known to be empty"; empty collections are `[]`, never null |
| Envelopes | list responses are objects: `{ "items": [...], "page": { "nextCursor": string\|null, "limit": n }, "freshness": {...} }` (never a bare array, so fields can be added); single resources are objects with `freshness` where data is cached from GitHub |
| Freshness | `freshness: { status: "fresh"\|"stale"\|"error"\|"unknown", asOf, fetchedAt, staleSince?, lastErrorCode? }` where `asOf` is the upstream data time and `fetchedAt` the time SCP fetched it (names from 06 section 6.2) |
| Pagination | **cursor-based** keyset pagination for unbounded collections (commits, dependency edges): `limit` (default 50, max 200), `cursor` (opaque base64url, validated; invalid -> 400 `invalid-cursor`), no total count by default; also emit RFC 8288 `Link: <...>; rel="next"` so agents can follow links (01 says "cursor links"). Collections bounded by configuration (groups, repos of a group, ecosystems) return all items in one page (limit max 1000) and support `q` and explicit filters; sorting is `sort=<field>` or `sort=-<field>` from a per-endpoint enum, never free-form |
| Filtering | explicit query parameters, arrays as repeated parameters (`ecosystem=maven&ecosystem=npm`; oapi-codegen v2.7 binds all style/explode combinations, S62), `q` for substring on name/title; no query language |
| Conditional GET | every cacheable GET returns `ETag` and `Cache-Control: no-cache` (always revalidate, never serve stale without asking); `If-None-Match` -> `304` with the ETag and no body; HEAD supported (RFC 9110 sections 13.1.2, 13.2, S88). ETag is derived from the snapshot generation plus the representation variant (not by hashing the body, 06 section 6.2): **recommend weak validators `W/"g<gen>-<variant>"`** so on-the-fly compression and proxies cannot make a "strong" tag lie (05 uses `W/"v<version>"`, 06 uses strong `"commits-<gen>"`: reconcile, see Corrections) |
| Errors | RFC 9457 `application/problem+json` for every non-2xx under `/api/` (S87). Members: `type`, `title`, `status`, `detail`, `instance` (`urn:uuid:<request id>`) + extensions `code` (stable machine value, the slug of `type`), `errors[] {pointer, detail}` (JSON Pointer to the offending field/parameter), `retryAfterSeconds`; `X-Request-Id` response header; `Cache-Control: no-store`; no stack traces; the SPA localises by `code` and never shows `detail` raw (06 section 14). Catalogue: `validation-failed` 400, `invalid-cursor` 400, `not-found` 404, `method-not-allowed` 405 (+`Allow`), `not-acceptable` 406, `payload-too-large` 413, `rate-limited` 429 (+`Retry-After`), `unauthorized` 401, `forbidden` 403, `upstream-unavailable` 503, `internal` 500 |
| Not-ready / stale | cold start: `503` problem `not-ready` with `Retry-After` on endpoints that cannot answer; once any snapshot exists, answer 200 with `freshness.status: "stale"` instead of failing (06 section 14) |
| Live updates | `GET /api/v1/snapshot` -> `{ version, builtAt, readiness }`, weak ETag `W/"v<version>"`, 304 when unchanged (05 section 3.5). `GET /api/v1/events` -> `text/event-stream`, `id: <version>`, `retry: 3000`, `: hb` every 15-20 s, events `hello` `{generation, buildId}`, `invalidate` `{topics, generation}`, `resync` (client behind the ring). Payloads are never streamed |
| Deprecation | RFC 9745 `Deprecation` header (a structured-field date, e.g. `Deprecation: @1788000000`) plus `Link: <...>; rel="deprecation"` on deprecated operations (RFC 9745 is Proposed Standard, S89); `Sunset` (RFC 8594, not fetched, UNVERIFIED) with the removal date; operations carry `deprecated: true` in the spec; v1 is supported at least 6 months after v2 ships (proposal) |
| Compatibility rules (CI-enforced by oasdiff) | forbidden in v1: removing or renaming a field/endpoint, making an optional request parameter/field required, narrowing a type or enum in a response, changing a status code, changing a field's nullability from nullable to non-null in requests. Allowed: adding optional fields, endpoints, enum values (clients must treat enums as open: the TS client must have a `default` branch) |
| Auth | none in v1 (public data); `access` mode (D-17) will use a trusted proxy or session cookie, same-origin; no CORS in production (06 section 9) |
| Discovery | `GET /api/v1/openapi.yaml` serves the embedded bundled spec; `/llms.txt` links to it (01 section 6.6); Swagger UI/Redoc are **not** embedded (they would add third-party-style JS and CSP surface); a static HTML rendering can be a CI artefact |

Reference `common.yaml` (corrected to the conventions; this is a design sketch, not the prototype file):

```yaml
components:
  schemas:
    Problem:
      type: object
      required: [type, title, status]
      properties:
        type: { type: string, format: uri-reference, default: "about:blank" }
        title: { type: string }
        status: { type: integer, minimum: 100, maximum: 599 }
        detail: { type: string }
        instance: { type: string, format: uri-reference }   # urn:uuid:<request id>
        code: { type: string }                              # stable machine code
        retryAfterSeconds: { type: integer, minimum: 0 }
        errors:
          type: array
          items: { type: object, required: [pointer, detail],
                   properties: { pointer: { type: string }, detail: { type: string } } }
    Freshness:
      type: object
      required: [status, fetchedAt]
      properties:
        status: { type: string, enum: [fresh, stale, error, unknown] }
        asOf: { type: [string, "null"], format: date-time }
        fetchedAt: { type: [string, "null"], format: date-time }
        staleSince: { type: [string, "null"], format: date-time }
        lastErrorCode: { type: [string, "null"] }
    PageInfo:
      type: object
      required: [nextCursor, limit]
      properties: { nextCursor: { type: [string, "null"] }, limit: { type: integer } }
  parameters:
    Limit:  { name: limit,  in: query, schema: { type: integer, minimum: 1, maximum: 200, default: 50 } }
    Cursor: { name: cursor, in: query, schema: { type: string, maxLength: 512 } }
    IfNoneMatch: { name: If-None-Match, in: header, schema: { type: string } }
  responses:
    Problem: { description: Problem details, content: { application/problem+json: { schema: { $ref: '#/components/schemas/Problem' } } } }
    NotModified: { description: Not modified, headers: { ETag: { schema: { type: string } } } }
```

Every GET operation references `IfNoneMatch`, `200` with an `ETag` header, `304: NotModified` and `default: Problem`; a Redocly custom rule or a 20-line script in CI fails the build if one is missing (design).

### 3.6 TypeScript consumption (verified, E8/E9; sketch)

```ts
// web/src/lib/api/client.ts
import createClient, { type Middleware } from "openapi-fetch"
import type { paths, components } from "./schema"          // generated by openapi-typescript
export type Problem = components["schemas"]["Problem"]
export class ProblemError extends Error { constructor(readonly problem: Problem) { super(problem.title) } }

const build: Middleware = {
  onResponse({ response }) {                                  // version-skew detection (2.7)
    const b = response.headers.get("X-SCP-Build"); if (b && b !== __BUILD_ID__) notifyNewVersion()
  },
}
export const api = createClient<paths>({ baseUrl: "/" }); api.use(build)
export async function unwrap<T>(p: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
  const { data, error } = await p
  if (error) throw new ProblemError(error as Problem)
  return data as T
}
```

- `api.GET("/api/v1/groups/{groupId}", { params: { path: { groupId } } })` is checked against the spec; a typo in the path, a missing param or a wrong query type fails `tsc` (E8: strict consumer compiles under TS 7.0.2 and the 6.x alias).
- No client-side ETag code (2.7). A hand-written ETag middleware that I first wrote is redundant and removed from the recommendation.
- **Runtime response validation with Zod: off by default in production.** Types come from the contract; the server validates requests, tests validate responses; the failure mode runtime validation covers (SPA built against an older server) is handled by the build-id check. Optional: generate `zod.gen.ts` with `@hey-api/openapi-ts` and use it (a) in MSW handler tests to prove mocks match the contract, and (b) behind `import.meta.env.DEV` in the client for developer feedback. Cost if enabled in production: Zod +23.1 KB gz (zod/mini +5.4 KB) plus per-response CPU (E1).
- Generated TS is committed or built? See section 9 (proposal: commit `schema.d.ts` and `routeTree.gen.ts`, with CI drift checks).

### 3.7 Contract testing (what runs where)

| Check | Tool | Runs | Catches |
|---|---|---|---|
| Spec is valid, consistent | `redocly lint` + `redocly bundle` (2.59, E8) and oapi-codegen v2.8's pre-generation validation (S62) | every PR | broken `$ref`, missing operationId/default response/304 |
| Server obeys the spec | Schemathesis 4.31.0 against the binary with a fixture snapshot (`uvx --from schemathesis==4.31.0 schemathesis run api/openapi.bundled.yaml --url ...`; Python >= 3.11; supports OpenAPI 3.1 and 3.2 per its docs, S81; Python requirement from PyPI metadata, S120) | every PR (seeded, limited examples), nightly longer | the two defects above; schema violations, 5xx, wrong content types, stateful link flows |
| No accidental breaking change | oasdiff 1.33.0 `breaking` base vs head (S84, E16) | every PR touching `api/` | removed/renamed fields, new required params, type narrowing |
| Compile-time handler contract | oapi-codegen strict server | every build | returning an undeclared status/body |
| Client matches contract | openapi-typescript types + `tsc` | every build | UI code using a field that does not exist |
| Mocks match contract | MSW handlers typed by `paths` (+ optional generated Zod parse in test) | unit/component tests | drifting mocks |
| End to end | Playwright against the real binary | every PR | wiring, CSP, a11y |

Consumer-driven contract testing (Pact) is not needed: there is one consumer shipped in the same repository and the same binary; external agents are protected by oasdiff and the versioning rule.

### 3.8 Adding an endpoint (the developer loop)

1. Edit `internal/<feature>/api/<feature>.yaml` (and `common.yaml` only for shared pieces); `redocly lint`.
2. `go generate ./internal/<feature>/...` (regenerates `*.gen.go`); implement the new strict handler method (compile error until done).
3. Handler test with a fixture snapshot; response validated against the spec.
4. `pnpm gen:api` (bundle + TS types); write the `queryOptions` and the component; MSW handler in the test.
5. CI: lint, oasdiff, Schemathesis, `git diff --exit-code` on generated files.

---
## 4. Svelte 5 comparison (for the ADR; the owner has decided on React)

Purpose: a short, evidence-based record of why React was chosen, not an argument against the decision. Numbers are from 2026-10-13 unless stated; everything here is background for one ADR paragraph.

| Dimension | React 19.3 | Svelte 5 | Evidence |
|---|---|---|---|
| Hello-world bundle (Vite 8, minified, gzip; React: bare `createRoot` + one `<div>`; Svelte: counter + list with `$state`/`$derived`) | 65.8 KB | **12.0 KB** (svelte 5.57.2: 30.8 KB raw, 11.0 KB brotli) | E1, E15. Fixed framework runtime cost; says little about a 100-component app, where app code and UI-kit code dominate (the gap should narrow, I did not measure a full app in Svelte, UNVERIFIED) |
| Developer sentiment (Stack Overflow Developer Survey 2026, n = 10,281) | used 41.5%, admired 46.7% | used **7.8%**, admired **59.4%** | S94 (Vue 17.2/47.2, Angular 16.0/42.1, Next.js 20.6/39.3) |
| npm downloads, last 30 days (2026-09-12..10-11) | react 815.6M (preact 159.0M) | svelte 27.6M; @sveltejs/kit 13.0M (vue 73.7M) | npm downloads API (S121); includes transitive installs and CI, an indicator of footprint, not of developers |
| Component kit | shadcn/ui first-party, 64 components, Base UI or Radix, CLI v4, registries | shadcn-svelte 1.7.0 (community port by Huntabyte and CokaKoala, MIT, 64 components on its list; underlying primitives `bits-ui` 2.19.5; I did not confirm the primitives from the page) | S24, S101, npm registry |
| Data table / query / charts / graph | TanStack Table 9, Query 5, Charts 1.0, Router (typed), Recharts via shadcn Chart, sigma, xyflow | `@tanstack/svelte-table` 9.3.1, `@tanstack/svelte-query` 6.3.2, `layerchart` 2.8.0 (shadcn-svelte charts), `@xyflow/svelte` 1.7.0; sigma and graphology are framework-agnostic | npm registry. A typed router with search-param validation comparable to TanStack Router: not evaluated for Svelte (UNVERIFIED); routing would be SvelteKit or a small router |
| Meta-framework in SPA mode | not needed (Vite + TanStack Router) | SvelteKit 3 released 2026-10-01, `adapter-static` 4.0.0; its own docs say SPA mode has "a large negative performance impact" and recommend prerendering; a plain Svelte + Vite SPA avoids SvelteKit, and then routing and the data layer are assembled by hand | S93, S107, S110 |
| Security record (2025-2026) | RSC-only advisories in Dec 2025 (not applicable to a client-only SPA); Trusted Types support in 19.3 | five advisories in one coordinated post (devalue DoS x2, SvelteKit remote-functions DoS, prerender/adapter-node DoS+SSRF, `svelte` `hydratable` XSS CVE-2025-15265), almost all in SvelteKit server features an SPA does not use | S2, S99 |
| LLM-assisted development (a real operating factor in this project) | most training data of any UI framework; benchmarks still favour React | runes (`$state`, `$derived`, `$effect`) post-date much training data (SvelteBench author's statement); Web-Bench (arXiv 2505.07473, May 2025): Claude 3.7 pass@2 React 65 vs Svelte 25 (60 vs 55 with thinking) | S95 (secondary search result, dated, indirect: no 2026 head-to-head benchmark found) |
| Governance | React Foundation under the Linux Foundation (announced 2026-02-24) | not researched (UNVERIFIED) | S7 |
| Ergonomics | Hooks + React Compiler 1.0 (auto-memoisation) narrow the gap; JSX | runes compile-time reactivity, less boilerplate, scoped CSS | S5, S111 (v5 migration guide read), opinion not measured |

**Honest summary for the ADR:** Svelte 5 wins on runtime size, compile-time reactivity and developer satisfaction; React wins on ecosystem depth at the points SCP needs (first-party shadcn/ui with Base UI/Radix, TanStack Router typed search params, TanStack Table/Query/Charts, many graph and chart libraries), on hiring and contributor familiarity (inference from the usage numbers, not measured) and on assistant/LLM familiarity. The roughly 54 KB gzip runtime difference is small relative to the budgets in 2.19 for an internal portal behind a LAN or VPN. Decision: React, as directed; revisit only if the bundle budget cannot be met or the React/Base UI/TanStack supply chain becomes untenable (2.18).

---

## 5. Frontend directory structure

Mirrors the backend package-by-feature layout (D-16, 04 section 5): one frontend feature per backend feature package that owns UI; `features/*/index.ts` is the only cross-feature import surface; enforced by lint (2.16).

```
web/
  package.json  pnpm-workspace.yaml  pnpm-lock.yaml  .node-version  .prettierrc  eslint.config.js
  vite.config.ts  vitest.config.ts  playwright.config.ts  tsconfig.json  tsconfig.app.json  tsconfig.node.json
  components.json                     shadcn config (style base-nova, cssVariables, aliases to @/components/ui, @/lib)
  routes.ts                           TanStack virtual route tree: THE single source of SPA URLs (section 6)
  index.html                          shell: <div id=root>, <script src="/theme-init.js">, no inline script
  public/ theme-init.js  favicon.svg  mermaid-frame.html (+ frame assets built separately)
  scripts/ precompress.mjs  routes-manifest.mjs  contrast.test-data.ts
  e2e/ *.spec.ts  fixtures/           Playwright against the Go binary (-tags spa) with a seeded snapshot
  src/
    main.tsx                          createRoot, providers, vite:preloadError handler, CSPProvider, Trusted Types policy
    app/                              root.tsx (layout, sidebar, header, route announcer), index-redirect.tsx, providers.tsx,
                                      router.ts (createRouter with context {queryClient}), routeTree.gen.ts (generated)
    features/
      dashboard/    index.ts route.tsx api.ts group-card.tsx latest-commits-panel.tsx *.test.tsx
      groups/       index.ts route.tsx overview.tsx commits.tsx api.ts repo-table.tsx
      commits/      index.ts route.tsx api.ts day-group.tsx commit-item.tsx filters.tsx
      docs/         index.ts route.tsx doc.tsx api.ts doc-content.tsx toc.tsx mermaid-frame.tsx docs.css
      manifests/    index.ts dependencies-route.tsx package-route.tsx repo-panel.tsx api.ts graph/ (sigma, layout worker) tables/ charts/
      pages/        index.ts route.tsx api.ts
      status/       index.ts route.tsx api.ts installations-table.tsx
    components/ui/                    shadcn generated + patched (button.tsx ... chart.tsx patched, scroll-area.tsx patched)
    shared/                           cross-feature app components: freshness-badge, relative-time, safe-html, page-header,
                                      empty-state, problem-alert, data-table (TanStack wrapper), copy-button, tree-view
    lib/
      api/    client.ts schema.d.ts (generated) problem.ts query-keys.ts zod.gen.ts (optional)
      routes.ts   serverRoutes: typed builders for Go-served URLs (badges, raw markdown, llms.txt, media, openapi.yaml)
      format.ts   Intl wrappers (dates, relative time, numbers, lists)
      i18n.ts  persist.ts (localStorage with try/catch)  theme.ts  build.ts (build id, version-skew)
    locales/en/messages.po
    styles/ index.css (tailwind + tokens + overrides)  docs.css  chroma-light.css  chroma-dark.css
```

Rules (lint-enforced): `features/x` imports `x` itself, `components/ui`, `shared`, `lib`, other features only via `index.ts`; `shared` imports `components/ui`, `shared`, `lib`; `components/ui` imports `components/ui` and `lib`; `lib` imports `lib`; `app` may import everything. The verified boundaries config covers feature/shared/lib (`08-prototypes/eslint-boundaries/eslint.boundaries.config.js`); adding the `ui` and `app` element types is a mechanical extension I did not run (UNVERIFIED). A feature owns its `api.ts` (queryOptions factories using `lib/api`), so adding an endpoint touches one feature.

---

## 6. Typed route map

### 6.1 URL surface (SPA routes vs routes the Go server answers)

D-12 lists the canonical routes; N-01 (D-15) needs more. Proposed additions are marked **new** and need to be added to D-12 (section 9).

| SPA route (TanStack path) | Source | Search params (validated with Zod, defaults) | API calls (feature `api.ts`) | Feature |
|---|---|---|---|---|
| `/` | D-12 | none; `beforeLoad` redirects to `/dashboard` (Go also redirects `/` with 302 before the SPA, D-16 `webapp`) | none | root redirect |
| `/dashboard` | D-12 | `q?: string` (filter groups), `tag?: string[]` | `GET /api/v1/groups`, `GET /api/v1/commits?limit=5` (poll 30 s, hidden tab paused), `GET /api/v1/site` (header status) | group cards, latest commits, app shell, header status |
| `/groups/$groupId` | D-12 | `q?`, `sort: "name"\|"lastCommit"\|"lifecycle" = "name"`, `dir: "asc"\|"desc"`, `tab: "repos"\|"commits"\|"dependencies" = "repos"` | `GET /api/v1/groups/{groupId}`, `GET /api/v1/groups/{groupId}/repos` | group page, pattern membership, default repo |
| `/groups/$groupId/commits` | D-12 | `cursor?`, `repo?`, `author?` | `GET /api/v1/commits?group=` | commit log per group |
| `/commits` | D-12 | `cursor?`, `group?`, `repo?`, `author?` | `GET /api/v1/commits` | commit log, all repositories |
| `/repos/$repo` | D-12 (docs) | `section?: string` (anchor) | `GET /api/v1/repos/{repo}`, `GET /api/v1/repos/{repo}/docs/readme` | README viewer, Markdown rendering, highlighting |
| `/repos/$repo/docs/$` **new** | 01 section 6.6 (doc ids `docs/install`) | none | `GET /api/v1/repos/{repo}/docs/{id}` (splat = doc id) | docs viewer |
| `/repos/$repo/commits` | D-12 | `cursor?`, `branch?` (only if Q-18 keeps branches) | `GET /api/v1/commits?repo=` | commit log per repository |
| `/repos/$repo/dependencies` **new** | D-15 consumer 1 | `scope?: "runtime"\|"dev"\|"test"\|"build"`, `direct: boolean = true`, `q?` | `GET /api/v1/repos/{repo}/dependencies` | N-01 |
| `/dependencies` **new** | D-15 consumers 2-5 | `view: "graph"\|"table"\|"versions"\|"licenses" = "graph"`, `focus?: string` (repo or purl), `depth: 1\|2\|3 = 1`, `ecosystem?: string[]`, `internal: boolean = true` | `GET /api/v1/dependencies/graph?focus&depth&ecosystem&internal`, `GET /api/v1/dependencies/versions`, `GET /api/v1/dependencies/licenses` | N-01 |
| `/packages/$purl` **new** | D-15 reverse-dependency view | none (`$purl` is URL-encoded) | `GET /api/v1/packages/{purl}` ("used by", version spread) | N-01 |
| `/pages/$slug` | D-12 | none | `GET /api/v1/pages/{slug}` | config-driven pages, `internal/pages` |
| `/status` | D-12 (admin listener) | none | `GET /api/v1/status` | status, D-17 installations view |
| not found / error | D-12 | | root `notFoundComponent`, `errorComponent` showing `code` and request id | error pages |

Routes the **Go** server answers and the SPA only links to (typed builders in `lib/routes.ts`, plain `<a>`, never `Link`): `/badges/{repo}/{kind}.svg`, `/repos/{repo}/readme.md` and `/repos/{repo}/docs/{id}.md`, `/llms.txt`, `/llms-full.txt`, `/media/...`, `/api/v1/openapi.yaml`, `/webhooks/github`, `/livez`, `/readyz`. The route manifest `dist/routes.json` (2.6) lets Go answer 404 for deep links that match no SPA route (06 section 3.2).

Group-id case policy is decided once: lower-case only, matching the API pattern `^[a-z0-9-]+$` (GR-07, GR-15).

### 6.2 `web/routes.ts` for all pages (design; the first four route groups were generated and type-checked in E9, the rest follows the same pattern)

```ts
import { rootRoute, route, index } from "@tanstack/virtual-file-routes"
export const routes = rootRoute("app/root.tsx", [
  index("app/index-redirect.tsx"),
  route("/dashboard", "features/dashboard/route.tsx"),
  route("/groups/$groupId", "features/groups/route.tsx", [
    index("features/groups/overview.tsx"),
    route("/commits", "features/groups/commits.tsx"),
  ]),
  route("/commits", "features/commits/route.tsx"),
  route("/repos/$repo", "features/docs/route.tsx", [
    index("features/docs/readme.tsx"),
    route("/docs/$", "features/docs/doc.tsx"),
    route("/commits", "features/commits/repo-commits.tsx"),
    route("/dependencies", "features/manifests/repo-dependencies.tsx"),
  ]),
  route("/dependencies", "features/manifests/dependencies-route.tsx"),
  route("/packages/$purl", "features/manifests/package-route.tsx"),
  route("/pages/$slug", "features/pages/route.tsx"),
  route("/status", "features/status/route.tsx"),
])
```

One route file per feature exports `createFileRoute(...)` with `validateSearch: z.object({...})` (Zod schemas passed directly, Standard Schema, S42), `loader: ({context}) => context.queryClient.ensureQueryData(...)`, `component`, `pendingComponent` (skeleton), `errorComponent`, `head`/`<title>` via React 19. A cross-feature link from `dashboard` to a group uses `<Link to="/groups/$groupId" params={{ groupId }}>`: compile error if the route, the param or a search key is wrong (E9). The `/repos/$repo/commits` file lives in `features/commits` because that feature owns commit UI; the route tree allows a file from any feature, and the file imports only through `index.ts` boundaries.

---

## 7. shadcn/ui component inventory by page

Install set (shadcn component names as listed by S24; Toast per S29): `alert avatar badge breadcrumb button card chart collapsible command dialog dropdown-menu empty field input input-group item kbd popover progress scroll-area select separator sheet sidebar skeleton spinner switch table tabs toast toggle-group tooltip` plus `typography` styles (`@tailwindcss/typography`, 2.5) - about 32 components, all owned source after `shadcn add`. Not installed: `drawer`, `carousel`, `calendar`/date picker, `form` (no forms in v1), `resizable` (only if the graph + details split is built), `pagination` (cursor "Load more" instead), `menubar`, `navigation-menu`, `accordion`, `slider`.

| Page / area | shadcn components | Custom components (not in shadcn) | Notes |
|---|---|---|---|
| App shell (navigation, header status) | `sidebar` (collapsible, mobile `sheet`), `breadcrumb`, `button`, `tooltip`, `dropdown-menu` (theme: light/dark/system radio items), `command` + `dialog` (palette), `kbd`, `separator`, `badge`, `toast` | `FreshnessBadge`, `RouteAnnouncer`, `SkipLink`, `StaleBanner` (`alert`) | sidebar cookie replaced by localStorage; palette opens with Ctrl/Cmd+K and a visible button; header status = repo count + "Updated 2 min ago" with a `popover` listing delayed sources |
| Dashboard (group cards, latest commits) | `card`, `badge`, `item`, `avatar`, `skeleton`, `empty`, `tooltip`, `input-group` (filter) | `GroupCard`, `LatestCommitsPanel`, `RelativeTime`, sparkline (inline SVG) | avatars: GitHub avatar URLs are cross-origin and blocked by `img-src 'self'`; use a same-origin `/media/avatar/...` proxy (06 section 8.5) or initials fallback; empty state: a group with a missing default repo degrades, it is not skipped |
| Group page | `tabs` (Repos, Commits, Dependencies), `card`, `table` + **TanStack Table 9** (sort, filter), `input-group`, `select`, `badge` (lifecycle, licence), `breadcrumb`, `skeleton`, `empty` | `RepoTable`, `DataTable` wrapper | bounded data set: client-side sort/filter OK; keep the URL as the state (`sort`, `dir`, `q`) |
| Commit logs (all, per group, per repository) | `item` (commit row), `badge` (repo), `select`/combobox (group, repo), `input` (author), `button` ("Load more", cursor), `skeleton`, `empty`, `tooltip` (absolute time), `separator` | `DayGroup` (sticky day heading, semantic `<h2>`/`<ol>`), `CommitItem` | grouped by day in the viewer's time zone, UTC in the tooltip; no virtualisation (50 per page); `aria-busy` while loading more |
| Docs / repo page | `tabs` (Docs, Commits, Dependencies), `card` (metadata), `badge` (lifecycle, type, "bridged format"), `alert` (bridged AsciiDoc notice, stale), `button`/`dropdown-menu` (open on GitHub, copy raw .md link), `scroll-area` (TOC, patched), `collapsible` (docs tree), `breadcrumb`, `skeleton`, `empty` ("no README") | `DocContent` (`SafeHtml` + `prose`, link interception, copy-code buttons, heading anchors), `Toc` (scrollspy), `MermaidFrame` (2.12), `Provenance` indicator ("from descriptor" vs "derived") | typography via `@tailwindcss/typography` + `docs.css`; `> [!NOTE]` alerts styled in CSS from classes emitted by the Go renderer; scrollable `<pre>` focusable |
| Repo dependencies panel + table (N-01) | `table` + TanStack Table (sorting, filtering, column visibility), `@tanstack/react-virtual` above 200 rows, `tabs` (direct / transitive), `badge` (scope, ecosystem), `input-group`, `toggle-group`, `tooltip`, `skeleton`, `empty` | `PurlCell`, `VersionBadge` | ecosystems shown with text, not colour only |
| Org dependencies (N-01) | `toggle-group` (view and depth), `select`/combobox multi (ecosystem), `switch` (internal only), `sheet` (node details), `card`, `chart` (version spread bar, licence mix; lazy chunk), `table`, `tabs`, `skeleton`, `empty`, `alert` (`truncated: true` notice), `tooltip` | `DependencyGraph` (sigma 3 + graphology, worker layout), `GraphControls` (zoom/pan buttons, reduced motion), `DependencyTree` (accessible `role="tree"` alternative built on `collapsible` + roving tabindex) | graph is an enhancement (2.9); table/tree carry all facts |
| Package page (N-01) | `card`, `table`, `chart` (version spread), `badge` | `UsedByList` | `$purl` is URL-encoded; render the purl in `<code>` with copy button |
| Pages (config-driven) | `alert` (announcement), typography container | | content is sanitised HTML from the API |
| Status (D-17) | `card`, `table` (installations: account, repos, last sync, rate limit remaining/reset, token expiry), `badge`, `progress` (rate limit), `alert`, `button` (refresh), `tooltip`, `skeleton` | `FreshnessBadge` | proposed to be served on the admin listener (section 9) |
| 404 / error | `empty` / `alert`, `button` | `ProblemAlert` (title, localised message by `code`, request id with copy button) | distinguishes "not found" from "not ready" (503) |

Accessible names: every icon-only `button` needs `aria-label` (lucide icons are `aria-hidden` by default in v1, S109); tooltips supplement, never replace, labels.

---

## 8. ADR-ready text

**ADR-0xx: Frontend is a React + shadcn/ui + TypeScript SPA embedded in the Go binary.**
- Status: accepted (owner directive 2026-10-13, D-16). Date: 2026-10-13.
- Context: the portal needs a rich UI (org dependency graph, searchable tables, live status) while keeping a single static binary and a strict security posture. Node is acceptable as a build-time tool only.
- Decision: SPA built with Vite 8, React 19.3 (+ React Compiler), TypeScript 7 (TS 6 alias for tooling), Tailwind 4, shadcn/ui on Base UI, TanStack Router + Query, Zod for route params, Lingui for i18n; tests with Vitest 5, MSW 3, Playwright + axe; pnpm 12 with a 7-day release cooldown; output embedded with `go:embed` behind build tag `spa`; served by `internal/webapp` with history fallback, hashed immutable assets, precompression, a strict CSP (`script-src 'self'`, `style-src 'self'`, Trusted Types) and no third-party origins. Versions and the evidence are in `05-research/08-frontend-stack.md`.
- Consequences: (+) typed routes and API, large ecosystem, accessibility primitives from Base UI, one binary at runtime. (-) a Node toolchain in CI/Docker builds, supply-chain exposure (mitigated by cooldown, exact pins, lockfile review, SHA-pinned actions), shadcn components are owned code needing three CSP patches and token-contrast fixes, bundle budget to police, SPA first paint needs JS (no SSR; LLM/crawler surface is server-generated).
- Alternatives considered: Svelte 5 (+ shadcn-svelte, SvelteKit or plain Vite) - smaller runtime and higher developer satisfaction, smaller ecosystem for typed routing/data tables/charts, less LLM familiarity (section 4); React Router 8 (typed routes only in framework mode); server-rendered templ/HTMX (rejected by the owner for the target UX); Radix instead of Base UI (still supported by shadcn, kept as an escape hatch).

**ADR-0xy: API contract is OpenAPI 3.1, spec-first, generated on both sides.**
- Decision: split OpenAPI 3.1 fragments per feature package + shared `common.yaml`; oapi-codegen v2.8 strict `net/http` servers per feature; kin-openapi request validation under `/api/`; `redocly bundle`; `openapi-typescript` + `openapi-fetch` on the client; Schemathesis and oasdiff in CI; RFC 9457 errors; cursor pagination; weak ETags from the snapshot generation; camelCase JSON; RFC 3339 UTC; SSE (v1.1) as invalidation only; `/api/v1` with additive evolution and `Deprecation`/`Sunset` headers.
- Consequences: handlers cannot return undeclared responses (compile time) and the server cannot drift unnoticed (Schemathesis); generated code is committed with drift checks (section 9); oapi-codegen's OpenAPI 3.1 support is initial, so the spec is limited to a small 3.1 subset and an Overlay downgrade path is documented.
- Alternatives: Huma (code-first, RFC 9457 and SSE built in; contradicts contract-first), ogen (own router, 3.1 unconfirmed), Goa (DSL), Connect (protobuf).

---

## 9. Proposed deviations from `04-decisions.md` (for the owner/ADR)

| # | Proposal | Reason and evidence | Impact |
|---|---|---|---|
| 1 | **Commit generated source** (`*.gen.go`, `schema.d.ts`, `routeTree.gen.ts`, `api/openapi.bundled.yaml`) with CI drift checks; keep `web/dist`, `.br/.gz`, `routes.json` uncommitted. D-14 says "no committed generated assets" | assets are the target of D-14 (committed build output drifts from its sources, GR-19); generated **source** that `go build`/`go test` need must exist without Node or codegen so a plain `go test ./...` works (verified with the placeholder embed, E18); reviewers see API changes as diffs; oasdiff compares committed bundles. Without committing, every contributor and CI job must run codegen first | small policy edit in D-14: "assets and build outputs are not committed; generated source is, verified by CI" |
| 2 | **No Zod response validation in production**; build-id skew detection instead; Zod used for route params, env, and dev/test response checks. D-16 says "Zod for runtime validation at the API boundary" | server validates requests, tests validate responses, types are generated; production validation costs +23 KB gz and per-response CPU and only catches version skew (2.7, 3.6) | D-16 wording |
| 3 | **Admin listener serves the SPA shell for `/status`** (with `/api/v1/status` only on the admin listener); the public listener has `GET /api/v1/site` for the header status | D-12 puts `/status` on the admin listener; the SPA is one bundle | `webapp` mounted on both listeners with different route manifests |
| 4 | **Static CSP without nonce**: `script-src 'self'`, `style-src 'self'`, external `/theme-init.js`, three shadcn patches (CSPProvider `disableStyleElements`, chart CSS variables via `style`, theme provider transition `<style>` removed). `06` section 4.3 recommended a per-request nonce | verified zero violations (E3), no template logic in Go, `index.html` can be ETagged and cached; a nonce is still available as a fallback | `06` CSP table simplified; Playwright CSP gate stays |
| 5 | **Mermaid in a sandboxed iframe**, v1.1; `frame-src 'self'` and `img-src 'self' data:` (or `blob:`, untested) | in-page Mermaid violates the CSP and renders a different layout (E5) | CSP gets two additions; docs show diagram source until then |
| 6 | **Graph: sigma 3.0.3 + graphology** (cytoscape 3.34.3 fallback); sigma 4 later | E6/E7 | pin recorded |
| 7 | **Base UI** (shadcn default since 2026-07) rather than Radix | 2.5 | D-16 says "Radix UI/Base UI": choose Base UI |
| 8 | **Add routes** `/repos/{repo}/docs/{id}`, `/repos/{repo}/dependencies`, `/dependencies`, `/packages/{purl}` to D-12; **assets** are `/assets/<name>-<hash>.<ext>` not `/assets/{hash}/...` (already noted in 06) | N-01 consumers and 01 section 6.6 doc ids | D-12 edit |
| 9 | **Node 24.21 now, 26 LTS after 2026-10-28**; pnpm 12.10.1 pinned via `devEngines.packageManager` (no corepack); `.node-version` rather than `.nvmrc` (either) | 2.18 | CI/Docker pins |
| 10 | **TypeScript 7.0.2 for `tsc` plus the TS 6 alias for tools**; revisit at TS 7.1 | 2.2 | `package.json` pair |
| 11 | **Lighthouse (Node API/CLI) instead of Lighthouse CI**; **no Storybook** | `@lhci/cli` last released 2025-06-25; Storybook optional in D-16 | CI jobs |
| 12 | **GitHub mark via Octicons**, not lucide | lucide-react 1.x removed brand icons (S109, E2) | one dependency |
| 13 | **shadcn CLI as devDependency**, sidebar cookie replaced by localStorage, `cn` package accepted, shadcn tokens overridden for WCAG 2.2 AA | 2.5 | repo conventions |
| 14 | **Cursor pagination + weak ETags + camelCase** as the architecture-level conventions (3.5) | 05 and 06 differ on strong vs weak ETags; 01 uses `status: "ok"` where 06 uses a staleness vocabulary | architecture document |

---
## Appendix A. Sources (all fetched, queried or run on 2026-10-13)

`WebFetch` returns a model-written summary of a page, not the page; where a claim is quoted or numeric I used it only when the summary carried it explicitly, and I note when a fetch failed or lacked the fact. IDs that exist only to keep numbering stable (index pages, failed fetches) are listed so a reader can see what was and was not read. "npm" = the registry JSON at `https://registry.npmjs.org/<package>` (version, publish time, licence, engines, peer dependencies).

| ID | Source | Used for |
|---|---|---|
| S1 | https://registry.npmjs.org/ (about 150 packages) | every version, date, licence, engine and peer range in section 1; "no release since" statements |
| S2 | https://react.dev/blog | React release list, Dec 2025 RSC advisory titles, CRA sunset |
| S3 | https://react.dev/blog/2026/09/09/react-19-3 | React 19.3: ViewTransition, Fragment refs, Trusted Types support |
| S4 | https://react.dev/blog/2025/10/01/react-19-2 | Activity, useEffectEvent |
| S5 | https://react.dev/blog/2025/10/07/react-compiler-1 | Compiler 1.0, lint rules, pin advice |
| S6 | https://react.dev/blog/2024/12/05/react-19 | Actions, `use`, ref as prop, metadata hoisting, removals |
| S7 | https://react.dev/blog/2026/02/24/the-react-foundation | React Foundation |
| S8 | https://react.dev/learn/react-compiler/installation | Vite + `@rolldown/plugin-babel` setup |
| S9 | https://devblogs.microsoft.com/typescript/ | index only |
| S10 | https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/ | TS 7 facts, TS 6 alias pattern, removed options |
| S11 | https://typescript-eslint.io/users/dependency-versions | supported TS range `>=4.8.4 <6.1.0` |
| S12 | https://vite.dev/blog | index only |
| S13 | https://vite.dev/blog/announcing-vite8 | Vite 8 / Rolldown / Oxc |
| S14 | https://vite.dev/blog/announcing-vite8-1 | Vite 8.1 (bundled dev mode, experimental) |
| S15 | https://vite.dev/guide/migration | `rolldownOptions`, `codeSplitting`, default target |
| S16 | https://vite.dev/guide/build | browser targets, `vite:preloadError`, caching |
| S17 | https://tailwindcss.com/blog | Tailwind 4.3 (2026-05-08), no v5 post |
| S18 | https://tailwindcss.com/blog/tailwind-is-joining-shopify | 2026-09-09 announcement |
| S19 | https://tailwindcss.com/docs/compatibility | Chrome 111 / Safari 16.4 / Firefox 128 |
| S20 | https://github.com/tailwindlabs/tailwindcss-typography | `@plugin`, `prose`, `not-prose` |
| S21 | https://ui.shadcn.com/docs/changelog | shadcn timeline |
| S22 | https://ui.shadcn.com/docs/changelog/2026-07-base-ui-default | Base UI default, `-b radix`, per-component migration |
| S23 | https://ui.shadcn.com/docs/installation/vite | init flow (index) |
| S24 | https://ui.shadcn.com/docs/components | component list (64) |
| S25 | https://ui.shadcn.com/docs/components/data-table | Data Table guide on TanStack Table v9 |
| S26 | https://ui.shadcn.com/docs/components/chart | Recharts v3 chart wrapper |
| S27 | https://ui.shadcn.com/docs/theming | OKLCH tokens, `@theme inline` |
| S28 | https://ui.shadcn.com/docs/dark-mode/vite | ThemeProvider (index) |
| S29 | https://ui.shadcn.com/docs/components/toast | Base UI Toast |
| S30 | https://ui.shadcn.com/docs/changelog/2026-09-cn | `cn` package |
| S31 | https://ui.shadcn.com/docs/components/typography, .../changelog/2026-07-typeset, https://ui.shadcn.com/docs/typeset | Typography and typeset (licence not stated) |
| S32 | https://base-ui.com/react/overview/releases | Base UI v1.0.0 2025-12-11, releases |
| S33 | https://base-ui.com/react/utils/csp-provider | `CSPProvider`, `disableStyleElements` |
| S34 | https://www.radix-ui.com/primitives/docs/overview/releases | Radix release list |
| S35 | web search "Radix UI maintenance status 2026 WorkOS Base UI migration shadcn" | secondary claims only |
| S36 | https://tanstack.com/blog and https://tanstack.com/blog/tanstack-charts-1-0 | Start 1.0, Charts 1.0, Form alpha |
| S37 | https://tanstack.com/blog/npm-supply-chain-compromise-postmortem | 2026-05-11 compromise |
| S38 | https://tanstack.com/blog/tanstack-start-security-update-cve-2026-102989 | Start XSS fix 2026-09-30 |
| S39 | https://tanstack.com/blog/announcing-tanstack-table-v9 | Table v9 |
| S40 | https://tanstack.com/router/latest/docs/framework/react/comparison | router comparison table (authored by TanStack: biased) |
| S41 | https://tanstack.com/router/latest/docs/framework/react/routing/virtual-file-routes | virtual file routes |
| S42 | https://tanstack.com/router/latest/docs/framework/react/guide/search-params | Zod search params |
| S43 | https://tanstack.com/router/latest/docs/framework/react/guide/code-splitting | auto code splitting, plugin order |
| S44 | https://tanstack.com/router/latest/docs/framework/react/guide/external-data-loading | Router + Query loaders |
| S45 | https://tanstack.com/router/latest/docs/framework/react/guide/history-types | browser vs hash history |
| S46 | https://tanstack.com/router/latest/docs/framework/react/guide/router-context | `createRootRouteWithContext` |
| S47 | https://raw.githubusercontent.com/TanStack/query/main/docs/framework/react/guides/query-invalidation.md | invalidation |
| S48 | https://tanstack.com/query/latest/docs/framework/react/guides/important-defaults | defaults (does not cover `refetchIntervalInBackground`) |
| S49 | https://tanstack.com/query/latest/docs/framework/react/guides/background-fetching-indicators | `isFetching` |
| S50 | https://reactrouter.com/changelog | React Router 8 |
| S51 | https://reactrouter.com/start/modes | typed routes only in framework mode |
| S52 | https://github.com/pnpm/pnpm/releases | pnpm versions |
| S53 | https://pnpm.io/supply-chain-security | cooldown and trust settings, defaults |
| S54 | https://pnpm.io/blog/2025/12/05/newsroom-npm-supply-chain-security | supply-chain context |
| S55 | https://pnpm.io/installation | standalone install |
| S56 | https://pnpm.io/package_json | `devEngines.packageManager`, `onFail` |
| S57 | https://github.com/pnpm/action-setup | action usage, works with pnpm 11/12 |
| S58 | https://raw.githubusercontent.com/nodejs/Release/main/schedule.json | Node schedule |
| S59 | https://nodejs.org/dist/index.json | Node versions and dates |
| S60 | https://hub.docker.com/v2/repositories/library/{node,golang}/tags/... | image digests |
| S61 | https://github.com/oapi-codegen/oapi-codegen | README: 3.1, servers, strict, import-mapping |
| S62 | https://github.com/oapi-codegen/oapi-codegen/releases | v2.7.x/v2.8.0 notes, advisories |
| S63 | https://github.com/oapi-codegen/oapi-codegen/issues/373 | 3.1 support issue history |
| S64 | https://github.com/ogen-go/ogen | ogen features |
| S65 | https://github.com/ogen-go/ogen/issues/1130 | fetched by mistake (unrelated issue) |
| S66 | https://huma.rocks/ | Huma overview |
| S67 | https://huma.rocks/features/response-errors/ | RFC 9457 errors |
| S68 | https://huma.rocks/features/server-sent-events-sse/ | SSE package |
| S69 | https://huma.rocks/why/ | design stance (not stated) |
| S70 | https://goa.design/ | Goa |
| S71 | https://connectrpc.com/docs/introduction | Connect |
| S72 | https://github.com/getkin/kin-openapi | kin-openapi 3.1/3.2 status |
| S73 | https://github.com/pb33f/libopenapi-validator | alternative validator |
| S74 | https://spec.openapis.org/ (no version facts on the page) and a web search on OpenAPI 3.2 / `itemSchema` | 3.2 existence (secondary) |
| S75 | https://openapi-ts.dev/introduction | openapi-typescript / openapi-fetch |
| S76 | https://heyapi.dev/openapi-ts/get-started | Hey API status |
| S77 | https://proxy.golang.org/<module>/@latest | Go module versions and dates |
| S78 | https://go.dev/dl/?mode=json and https://go.dev/doc/devel/release | Go 1.27.2, 1.27.0 date |
| S79 | https://orval.dev/ and npm | Orval 8 |
| S80 | https://schemathesis.readthedocs.io/en/stable/changelog/ | **HTTP 404, not read** |
| S81 | https://schemathesis.readthedocs.io/en/stable/ | supported OpenAPI versions |
| S82 | https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Caching | validation, 304 |
| S83 | https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events | EventSource facts |
| S84 | https://github.com/oasdiff/oasdiff | oasdiff (CLI flags not documented on the page read) |
| S85 | https://github.com/jacomyal/sigma.js/releases | sigma 3/4 releases |
| S86 | https://reactflow.dev/learn/advanced-use/performance | xflow performance guidance |
| S87 | https://www.rfc-editor.org/info/rfc9457 | problem details |
| S88 | https://www.rfc-editor.org/info/rfc9110 | conditional requests |
| S89 | https://www.rfc-editor.org/info/rfc9745 | Deprecation header |
| S90 | https://pkg.go.dev/net/http#ResponseController | `SetWriteDeadline`, `Flush` |
| S91 | https://www.w3.org/TR/WCAG22/ | WCAG 2.2 |
| S92 | https://web.dev/articles/vitals | Core Web Vitals thresholds (page last updated 2024-10-31) |
| S93 | https://svelte.dev/blog | Svelte posts, SvelteKit 3 date |
| S94 | https://survey.stackoverflow.co/2026/technology/data | usage and admired percentages (the `/technology` page lacked them) |
| S95 | web search on LLM benchmarks for Svelte 5 vs React (Web-Bench arXiv 2505.07473, SvelteBench mentions) | indirect LLM-familiarity evidence |
| S96 | web search on Mermaid CSP / CVE-2026-41148, -41149 (sentinelone, feedly result titles) and https://mermaid.js.org/config/usage.html | Mermaid `securityLevel`, CVE titles |
| S97 | https://developer.mozilla.org/en-US/docs/Web/API/Trusted_Types_API | Trusted Types API, enforcement behaviour |
| S98 | https://web-platform-dx.github.io/web-features-explorer/features/trusted-types/ | Baseline "newly available" 2026-02-24, browser versions |
| S99 | https://svelte.dev/blog/cves-affecting-the-svelte-ecosystem | Svelte advisories |
| S100 | https://github.com/dequelabs/axe-core/blob/develop/doc/API.md | rule tags incl. `wcag22aa` |
| S101 | https://www.shadcn-svelte.com/docs/installation/vite | shadcn-svelte |
| S102 | https://biomejs.dev/blog/ (index) and npm | Biome version only |
| S103 | https://eslint.org/blog/ | ESLint 10, v9 EOL |
| S104 | https://github.com/javierbrea/eslint-plugin-boundaries, https://www.jsboundaries.dev/docs/ (+ /policies, /classification) | boundaries v7 policy DSL |
| S105 | https://vitest.dev/blog/vitest-5 | Vitest 5 |
| S106 | https://mswjs.io/blog/ and https://mswjs.io/blog/introducing-msw-3.0 | MSW 3 |
| S107 | https://svelte.dev/blog/sveltekit-3 | SvelteKit 3 |
| S108 | https://lingui.dev/installation | Lingui 6 setup |
| S109 | https://lucide.dev/guide/version-1 and a web search | brand icons removed, `aria-hidden` default |
| S110 | https://svelte.dev/docs/kit/single-page-apps | SvelteKit SPA mode caveats |
| S111 | https://svelte.dev/docs/svelte/v5-migration-guide | Svelte 5 concepts |
| S112 | https://github.com/GoogleChrome/lighthouse-ci | Lighthouse CI status (+ npm) |
| S113 | https://mermaid.js.org/config/usage.html | `render`, `securityLevel` |
| S114 | `@tanstack/query-core@5.104.2` type declarations (JSDoc) read from the installed package; https://tanstack.com/query/latest/docs/framework/react/reference/useQuery | `refetchOnWindowFocus`, `refetchIntervalInBackground` defaults |
| S115-S117 | https://github.com/actions/checkout/releases/latest, .../setup-node/releases/latest, .../setup-go/releases/latest | action major versions (the three summaries came back in an order I could not map reliably) |
| S118 | https://www.i18next.com/ | i18next (comparison only) |
| S119 | web search "Node.js 25 corepack no longer distributed" (nodejs.org corepack docs result) | Corepack removal |
| S120 | https://pypi.org/pypi/schemathesis/json | Schemathesis 4.31.0, Python >= 3.11 |
| S121 | https://api.npmjs.org/downloads/point/last-month/<package> | download counts |
| S122 | `streamedQuery` pages (tanstack.com and raw GitHub) | **HTTP 500 / 404, not read** |

Roughly 120 distinct sources were consulted; the numbered list above has 122 ids, of which S9, S12, S23, S28, S65, S69, S80 and S122 are index pages, mistaken fetches or failures (listed for transparency).

---

## Appendix B. Experiments (all throwaway, in `05-research/08-prototypes/`; none of it is production code)

| ID | What was run (2026-10-13, macOS arm64, Node 26.10.0, npm 11.19.1, pnpm 10.11.0 (not the pinned 12.10.1), Chrome 155 via Playwright) | Result in one line | Where |
|---|---|---|---|
| E1 | 27 one-entry Vite 8.3.4 builds of hello-world apps per library, raw/gzip/brotli sizes | React 65.8 KB gz; router +25.1; query +9.3; Base UI +59.2 vs Radix +34.9; Recharts +97.2; mermaid 1,385 KB gz as one entry; Lingui +2.6 vs i18next +15.5 | `bundle-bench/` |
| E2 | `shadcn init -t vite -b base` + ~25 components, build, `tsc` strict | works; `shadcn` in dependencies (453 vs 165 packages), no lucide `Github`, sidebar cookie, unused-React-import error under `noUnusedLocals` | `shadcn-csp-spike/` |
| E3 | Chrome with `style-src 'self'`, `securitypolicyviolation` listener, exercised Dialog/Select/Tooltip/DropdownMenu/Popover/Sheet/Toast/Tabs/Command/Chart/theme toggle | 3 offenders; after three patches zero violations | `shadcn-csp-spike/`, `cspserve*.mjs` |
| E4 | `require-trusted-types-for 'script'; trusted-types scp-html` with a `SafeHtml` component | zero violations, `TrustedHTML` accepted by React 19.3 | `shadcn-csp-spike/` |
| E5 | Mermaid 12.2.0 in page (inline, as image) and in a sandboxed iframe under CSP | in-page: 72 console errors, 216 violation events, different layout; iframe: correct layout | `mermaid-csp/` |
| E6 | 1,000 nodes / 2,500 edges in sigma 3, sigma 4, cytoscape, xyflow (full and visible-only), d3 canvas; 6x CPU throttle, medians of 5 | section 2.9 table | `graph-bench/` (`results.json`) |
| E7 | layout timings, Node 26: d3-force, dagre, elkjs | d3-force 0.61 s; dagre 3.0 s at 750 nodes, no result at 1,000 nodes in 90 s; elk 2.06 s but 467 KB gz | `bundle-bench/lb-*.mjs` |
| E8 | OpenAPI 3.1 split per feature, oapi-codegen 2.8.0 strict std servers with import-mapping, kin-openapi validator, redocly bundle, openapi-typescript client, Schemathesis 4.31 | pipeline works; two real defects found; three unsupported-method failures open | `contract/` |
| E9 | TS 7.0.2 + TS 6.0.2 alias; openapi-typescript, hey-api (zod) on the alias; TanStack virtual routes with typed `Link` | all work; type errors for bad route/param/search; Zod `date-time` rejects offsets | `contract/web/`, `router/` |
| E10 | Vitest 5 jsdom + browser projects, MSW 3, typescript-eslint 8.71 on the alias, jsx-a11y on ESLint 10 | all run; `server.listen({onUnhandledRequest})` is a type error in MSW 3 | `shadcn-csp-spike/` |
| E11 | eslint-plugin-boundaries 7.2.0 policies on a sample tree | deep cross-feature import and shared->feature errors; index import allowed | `eslint-boundaries/` |
| E12 | Go SSE handler with `ResponseController.SetWriteDeadline(time.Time{})` under `WriteTimeout=10s`; browser through the Vite dev proxy | 9 events over 14 s, unbuffered through proxy | `contract/cmd/sse/` |
| E13 | WCAG contrast of the generated shadcn tokens, light and dark, plus replacement values | defaults fail 1.4.3/1.4.11 in several pairs; replacements pass | `contrast.py` |
| E14 | React Compiler 1.0 via `@rolldown/plugin-babel` on the spike build | +9.5 KB raw / +2.5 KB gz; build 0.4 s -> 1.3 s | `shadcn-csp-spike/` |
| E15 | Svelte 5.57.2 counter + list build | 30.8 KB raw, 12.0 KB gz | `bundle-bench/build-sv.mjs` |
| E16 | `oasdiff breaking` on two bundled specs | detected `request-parameter-became-required` as an error | `contract/` |
| E17 | axe-core 4.14.0 (`wcag2a`, `wcag2aa`, `wcag21a`, `wcag21aa`, `wcag22aa`) on the spike page, both themes | only defects in my own test markup; `color-contrast`, `target-size` incomplete | `shadcn-csp-spike/` |
| E18 | Go `internal/webapp` handler tests; `go test` with placeholder embed; `go test -tags spa` against a populated `dist` | handler tests pass; both embed variants compile | `contract/internal/webapp`, `contract/web/embed_*.go` |

Limits that apply to every experiment: one machine, one browser engine (Chrome 155); **Firefox and Safari were not tested**, including CSP and Trusted Types behaviour (Trusted Types is Baseline "newly available" only since 2026-02-24, S98); packages were the **latest** published versions on 2026-10-13 (for example Vite 8.3.4, router 1.170.44), not the cooldown-compliant pins of section 1; the whole app was never built, only spikes.

---

## Corrections to prior docs

Items that conflict with, or need an edit in, earlier documents (all resolved in this report; the owner decides section 9 items):

1. **`04-decisions.md` D-16, primitives:** "shadcn/ui ... built on Radix UI/Base UI primitives" - since 2026-07 shadcn defaults to Base UI (`-b radix` keeps Radix); recommend Base UI, so name it in the decision.
2. **D-16, icons:** "lucide icons" - lucide-react 1.x has **no GitHub (or any brand) icon**; the portal's main outbound link needs `@primer/octicons-react` or an inline SVG.
3. **D-16, runtime validation:** "Zod for runtime validation at the API boundary" - recommend Zod for route/search params, env and dev/test response checks only; production response validation off (build-id skew detection instead). Section 9 item 2.
4. **D-16, CSP:** "inline theme pre-paint handled with a hash/nonce injected by Go or moved to a small external script" - the external-script option is verified and removes all nonce plumbing; with three shadcn patches `style-src 'self'` produces zero violations. **`06` section 4.3 (per-request nonce, `style-src-elem` nonce, nonce into `CSPProvider`)** is therefore not needed, and **`06` section 4.1/4.4** statements that React 19.3 under Trusted Types and Mermaid under CSP were unverified are resolved here (React works; Mermaid does not run in-page, use a sandboxed iframe). **`06` section 4.1** `img-src 'self'` needs `data:` (or `blob:`) only if Mermaid images ship.
5. **D-16, tooling:** "Node pinned via `.nvmrc`/`engines`" - also pin the package manager through `devEngines.packageManager` because Corepack is not shipped from Node 25; "Lighthouse CI" - `@lhci/cli` last released 2025-06-25, use Lighthouse directly; "Biome or Prettier" - ESLint + Prettier (boundaries plugin); "Storybook optional" - not adopted; "typed router candidates" - TanStack Router (with the 2026-05-11 compromise noted).
6. **D-16, API verification wording:** "contract tests (and/or generated strict-server stubs)" - do both: strict-server compile-time contract plus Schemathesis/oasdiff; generated stubs do **not** validate requests (E8), a validator middleware is required.
7. **D-12:** `/assets/{hash}/...` is `/assets/<name>-<hash>.<ext>` with Vite (already in `06`); routes for doc ids, repo dependencies, `/dependencies` and `/packages/{purl}` are missing; `/status` is on the admin listener but the SPA header status needs a public endpoint.
8. **D-14 "no committed generated assets":** keep for build outputs, but generated **source** (`*.gen.go`, `schema.d.ts`, `routeTree.gen.ts`, bundled OpenAPI) should be committed with drift checks (section 9 item 1).
9. **Server-rendered templates (templ/HTMX), esbuild and "no Node tooling"** are superseded by D-16 (already stated in D-16). In `02-design-guardrails.md`, GR-11 (distinct view types) and GR-39 (HTML validity in CI) read as follows for an SPA: view structs become API DTOs, and HTML validity becomes axe plus a test of the Go-rendered Markdown fragments. GR-37, GR-38, GR-39, GR-19 and GR-40 apply as written (Chroma classes + CSS variables, UTC + `<time>`, inline SVG icons via lucide/Octicons, hashed assets, visibility-aware polling).
10. **`05-store-sync-engine.md` vs `06`:** snapshot ETag is weak `W/"v<version>"` in 05 and strong `"commits-<gen>"` in 06; this report recommends weak everywhere (3.5). **`01-docs-format-llm.md` section 6.6** uses `freshness.status: "ok"` where `06` uses a stale/fresh vocabulary: pick `fresh|stale|error|unknown`. 01 says "cursor links": the envelope field `page.nextCursor` plus a `Link: rel="next"` header covers both.
11. **`06` section 3.2 route manifest** (UNVERIFIED proposal) is now concrete: `routes.ts` -> `dist/routes.json` (2.6).
12. **My own earlier drafts during this research:** a hand-written ETag middleware in the TS client (redundant, the browser does it), snake_case in the prototype schemas (production is camelCase), a single-run graph benchmark (replaced by medians of 5) and the claim that `@xyflow/react` documents a node-count threshold (its performance page gives none).

---

## Risks

| # | Risk | Likelihood / impact | Mitigation |
|---|---|---|---|
| 1 | **npm supply-chain compromise** (precedent: TanStack Router/Start 2026-05-11; worms in 2025-26) | medium / high (CI and developer machines, bundle contents) | 7-day cooldown, `trustPolicy`, `blockExoticSubdeps`, `strictDepBuilds`, exact pins, lockfile review, SHA-pinned Actions, read-only CI token, no secrets in the build stage, image built from a clean checkout, `npm audit signatures` (npm CLI) where available |
| 2 | **Pins were not the tested versions** (experiments used the latest; pins are the newest versions >= 7 days old) | high / low | after pinning run the whole gate (typecheck, lint, tests, e2e, CSP gate, size) once and record results in the implementation plan |
| 3 | **oapi-codegen OpenAPI 3.1 support is "initial"** (nullable-type problems reported June 2026; multi-type unions become `any`; three injection advisories in 2026) | medium / medium | restrict the spec to the tested subset, review generated diffs, pin tool version, CODEOWNERS on specs, Overlay/3.0 downgrade or Huma code-first as fallbacks |
| 4 | **TypeScript 7 has no JS API until 7.1**: tools must use the TS 6 alias; typescript-eslint's range stops below 6.1 (2026-10-13) | medium / medium | alias pair in `package.json`; CI runs `tsc` (7) and lint (6 alias); remove the alias only when the three tools support TS 7 |
| 5 | **shadcn owned code drifts**: CLI v4 changes monthly (Base UI default 2026-07, `cn` 2026-09); our three CSP patches and token overrides must survive `shadcn add/update` | high / low-medium | patches live in the repo and are covered by the Playwright CSP-violation gate and a contrast unit test; update components by diff review, never blind overwrite |
| 6 | **Base UI is young** (1.0 on 2025-12-11; monthly releases); `cmdk` has had no release in 19 months; shadcn also supports Radix | medium / medium | escape hatch `-b radix` and per-component migration (S22); wrap overlays in our own components so primitives can be swapped; Base UI `Autocomplete`/`Combobox` as `cmdk` replacement |
| 7 | **TanStack Router/Query concentration**: a single vendor ecosystem, daily patch cadence, one monorepo already compromised | medium / medium | exact pins, cooldown; React Router 8 framework mode recorded as the migration target; our route definitions are data (`routes.ts`) and API access is behind `lib/api`, which limits blast radius |
| 8 | **Tailwind Labs joined Shopify** (2026-09-09; MIT stays) | low / low | none needed beyond watching the licence and governance |
| 9 | **Graph at 1,000+ nodes**: sigma 3 is canvas/WebGL (no DOM accessibility), WebGL may be unavailable; sigma 4 moves fast; layout cost (dagre does not finish at 1,000 nodes) | medium / medium | server caps and `truncated`, table/tree alternative always shipped, worker-based d3-force, cytoscape fallback, graph feature flag |
| 10 | **Mermaid**: 1.4 MB gz, CSS-injection CVEs in 2026, cannot run in-page under the CSP | medium / low | sandboxed iframe, lazy, v1.1, source fallback |
| 11 | **Accessibility**: shadcn defaults fail AA contrast; automated checks cover a fraction; canvas graph; live regions | high / medium | token overrides + contrast test, axe on every route, manual keyboard/screen-reader checklist (not yet performed), non-visual alternatives |
| 12 | **Performance budgets are proposals**: first load needs HTML -> JS -> API; no field data; budgets unmeasured on the real app | medium / medium | size-limit in CI from the first feature, Lighthouse regression runs, `modulepreload` and router preloading, API preload hint if measured necessary |
| 13 | **Browser coverage**: experiments only in Chrome 155; Trusted Types and strict CSP are newly available in Firefox 148 / Safari 26 | medium / medium | Playwright projects for Firefox and WebKit in CI from day one; headers degrade gracefully (unsupported TT is ignored) |
| 14 | **Version skew after deploys** (long-lived tabs request deleted chunks or old API shapes) | high / low | `vite:preloadError` reload, `X-SCP-Build` check, additive API evolution, oasdiff gate, `index.html` no-cache |
| 15 | **React Compiler / Vite 8 / Vitest 5 / MSW 3 are new majors** (Vitest 5 2026-09-03, MSW 3 2026-09-30, Vite 8 2026-03); MSW 3 already differed from v2 snippets | medium / low | pin exactly, run tests against the compiled build, keep Compiler optional behind one plugin entry |
| 16 | **SPA is invisible to crawlers/LLMs by itself** | medium / low | server-generated `llms.txt`, raw `.md`, JSON API, sitemap (D-16); no SSR by decision; Open Graph prerender for shareable links left as an option |
| 17 | **Scope creep**: command palette/search, org graph and N-01 views are beyond the v1 page set | medium / medium | ship v1 pages from section 6 first; palette is a thin client-side filter over a cached list endpoint (proposal), not a search engine |
| 18 | **Lingui macros under Vite 8/Rolldown not built** | low-medium / low | fallback to react-intl/i18next (+13-15 KB gz) behind the same `t()` wrapper |

---

## Unverified

Everything here is stated in the report with a hedge; collected so a reader sees the open items in one place.

- **Pins and the whole app:** the exact pinned versions of section 1 were not the ones built and tested (the latest were); no full application exists, so bundle budgets (2.19), Lighthouse results and first-load timings are proposals, not measurements.
- **Browsers:** Firefox and Safari were not tested (CSP behaviour, Trusted Types, Base UI, sigma/WebGL); one machine, one OS.
- **Accessibility:** no screen-reader testing, no keyboard walk-through of a real app; axe reported `color-contrast` and `target-size` as "incomplete"; Base UI Tooltip against SC 1.4.13 untested; TanStack Router route-change announcement behaviour assumed absent (unverified); `forced-colors` rules untested.
- **CSP and theme:** the external `/theme-init.js` pre-paint approach (no flash) was designed, not run; the per-path header table for the Mermaid frame and a `blob:` variant of Mermaid images were not tested; the cause of the 4 main-page violations in the iframe run was not isolated; `sandbox`-origin `postMessage` handling is a design.
- **Mermaid:** CVE-2026-41148/41149 only as search-result titles (fixed in 11.15.0 per a secondary source); no Go-native Mermaid renderer evaluated.
- **API contract:** the remaining three Schemathesis "unsupported methods" failures were not diagnosed; production validator error mapping to problem+json `code`/`errors[]` not built; `oasdiff` CI flag (`--fail-on`) not confirmed from its docs; `Sunset` (RFC 8594) not fetched; interaction of weak ETags with on-the-fly compression and the validator not prototyped; kin-openapi's version (proxy says v0.150.0, its README mentions v0.151.0 in the changelog); OpenAPI 3.2 release date from a secondary source; Orval and Goa 3.1 support not stated on the pages read.
- **Frontend tooling:** Lingui macro + Rolldown + React Compiler build not run (only runtime size); `eslint-plugin-lingui` rule not evaluated; MSW 3's equivalent of strict unhandled-request handling not located; `openapi-msw` and `openapi-react-query` not tried; Base UI overlays in jsdom not tested; the boundaries config for `ui`/`app` element types not run; `streamedQuery` documentation could not be loaded (HTTP 500/404).
- **shadcn:** `shadcn/typeset` licence and coverage; Radix maintenance concerns (secondary sources only); Chroma-generated CSS (`html.WithClasses`) not run; GitHub logo terms for the Octicons mark; whether shadcn `Toast` has features Sonner had.
- **Svelte comparison:** Svelte governance, a typed router for Svelte, `bits-ui` as the shadcn-svelte primitive layer, full-app Svelte bundle size, and a 2026 LLM benchmark were not established; the LLM evidence is a dated, indirect secondary result.
- **Supply chain:** all experiments ran under pnpm 10.11.0, not the pinned 12.10.1, so the `pnpm-workspace.yaml` policy and `devEngines` flow were not exercised; the pnpm settings are documented (S53) but were not tested against a hostile package; pnpm 12.13.0 appears on GitHub releases while npm `latest` read 12.11.2; GitHub Actions SHAs not resolved and the action-to-version mapping from three summaries was ambiguous; Node 25 Corepack removal comes from a search result summarising the Node docs.
- **React:** details of the December 2025 RSC advisories were not read (titles only).
- **Graph numbers:** vsync-limited frames on a 120 Hz display (so "frame time" means "no worse than"), one machine, synthetic graph, xyflow rendered real labelled nodes while the others drew dots (so xyflow's gap is partly work done).
