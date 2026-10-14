# 18-web-pages companion files

Compile-checked and executed support code for `20-design/18-web-pages.md`. Verified 2026-10-14.

## What is here

| Path | What | Status |
|---|---|---|
| `web/src/features/**`, `web/src/app/search-model.ts`, `web/src/shared/*.ts(x)`, `web/src/lib/trusted-types.ts` | pure logic and the three compiled components (`DocContent`, `DependencyGraphCanvas`, `DataTable`) of unit 18; this is the starting point of the real `web/src` files | owned by unit 18 |
| `web/src/lib/api/{client,problem,query-keys}.ts`, `web/src/lib/server-status.ts`, `web/src/vite-env.d.ts` | **copies of unit 17's foundation files** (as of 2026-10-14), present only so the logic compiles; do not edit, take unit 17's | owned by unit 17 |
| `web/src/test/fixtures.ts`, `synthetic-graph.ts` | typed fixtures (values from the contract examples) and the 1,000-node benchmark graph | unit 18 |
| `web/src/lib/api/schema.d.ts` | **not included** (4,125 generated lines); generate it, see below | generated |
| `tokens/` | `tokens.py` (design values), `viz.py`, `fit.py` (chroma fit to sRGB), `contrast.py` (WCAG ratios, OKLCH to sRGB), `gen.py` (writes `tokens.css`), `contrast-report.txt` (0 failures), `tokens.css` | unit 18 |
| `csp-probe/palette-probe/` | `app.ts`, `build.mjs`, `run.mjs`: mounts `GraphController` in Chromium, calls `setPalette(dark)`, reads the rendered node, label and edge colours, then loses the WebGL context and checks `onContextLost` | verifier |
| `csp-probe/` | `app.ts`, `serve.mjs`, `run.mjs`, `page.css`: serves a page under the architecture's exact document CSP and runs the layout worker plus the sigma graph in Chromium; `?tt=plain` shows the Trusted Types block of `new Worker()` | unit 18 |

## Reproduce

```sh
cd web
pnpm add -D typescript@npm:@typescript/typescript6@6.0.2 vitest@5.0.3 vite@8.3.3 @redocly/cli@2.59.0 openapi-typescript@7.13.0 @types/node@24.19.1 @types/trusted-types@2.0.7 graphology-types@0.24.8 @types/d3-force@3.0.10 @types/react@19.3.0 @types/react-dom@19.3.0
pnpm add react@19.3.0 react-dom@19.3.0 @tanstack/react-router@1.170.41 @tanstack/react-query@5.104.1 @tanstack/react-table@9.2.6 sigma@3.0.3 graphology@0.26.0 d3-force@3.0.0 openapi-fetch@0.17.0 zod@4.6.5
pnpm gen:api            # redocly bundle of ../../api/openapi-admin.yaml, then openapi-typescript -> src/lib/api/schema.d.ts
pnpm typecheck          # tsc --noEmit -p .   (TypeScript 6.0.3 here)
npx -p typescript@7.0.2 tsc --noEmit -p .    # TypeScript 7.0.2: clean as well
pnpm test               # vitest run: 14 files, 145 tests (node by default; jsdom 26.1.0 for files with a `// @vitest-environment jsdom` pragma)
```

`package.json` here lists the exact pins that were used (`npm` with `--legacy-peer-deps` because `openapi-typescript` 7.13 declares a `typescript ^5` peer; unit 17 resolves it with its alias).

## CSP probe

```sh
# from a directory that has esbuild and playwright installed
esbuild csp-probe/app.ts --bundle --minify --format=esm --alias:@=<web>/src --outfile=csp-probe/app.js --platform=browser
esbuild <web>/src/features/manifests/graph/layout.worker.ts --bundle --minify --format=esm --alias:@=<web>/src --outfile=csp-probe/worker.js
cp <web>/src/styles/tokens.css csp-probe/tokens.css
node csp-probe/serve.mjs &            # http://127.0.0.1:4731, CSP = architecture 11.3 string, worker served from /assets/worker.js
node csp-probe/run.mjs ""             # expect: no violations, layoutMs ~800, mountMs ~30
node csp-probe/run.mjs "?tt=plain"    # expect: workerError "This document requires 'TrustedScriptURL' assignment"
node csp-probe/run.mjs "" throttle    # 6x CPU throttle (software WebGL: mount ~440 ms)
```

Result recorded 2026-10-14 with Chromium 153.0.8010.12 (Playwright 1.63.0 headless shell): section 10.4 and 12.6 of the design document.

## Notes added by the independent verification (2026-10-14)

- `web/src/features/docs/index.ts` is deliberately pure (it re-exports `docSplat`, `resolveDocId`, `stripDocExt` from `docId.ts` only). Exporting `./api` from it would pull `client.ts`, which reads `window`, into node-environment tests and into `app/search-model.ts`.
- Test environments: the default is `node`. Files that render React or touch `window` carry a `// @vitest-environment jsdom` pragma; `styles/contrast.test.ts` pins `node`. Making jsdom the global default breaks `contrast.test.ts` (`URL must be of scheme file`). Unit 17 owns the real `unit` project and must keep this split or pin per file.
- Palette probe:

```sh
# from a directory that has esbuild and playwright-core installed
node csp-probe/palette-probe/build.mjs <web>
CHROME=/path/to/chrome node csp-probe/palette-probe/run.mjs   # expect colorBefore #111111 -> colorAfterSetPalette #aa0000, onContextLostCallback true
```
