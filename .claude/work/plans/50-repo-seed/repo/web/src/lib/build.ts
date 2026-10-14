/**
 * The build id compiled into the bundle (the git SHA; "dev" outside a release build), injected by vite.config.ts from the BUILD_ID
 * environment variable. scripts/finalize.ts requires it in the initial chunks and writes it to dist/routes.json; the Go side compares
 * it with `main.build` (webapp CheckBuild) and the SPA compares it with X-SCP-Build to detect a stale tab (design 17 section 6).
 */
export const BUILD_ID: string = __BUILD_ID__
