// One sample URL per public SPA shape of dist/routes.json (params are sample values that exist in e2e/fixtures/api).
// scripts/e2e-coverage.test.ts fails when a shape of routes.ts has no entry here, so a new route cannot ship without an axe run.
export const PAGES = [
  "/dashboard",
  "/groups/platform",
  "/groups/platform/commits",
  "/commits",
  "/repos/core-lib",
  "/repos/core-lib/docs/docs/install",
  "/repos/core-lib/commits",
  "/repos/core-lib/dependencies",
  "/dependencies",
  "/packages?purl=pkg%3Anpm%2Freact",
  "/pages/about",
] as const
