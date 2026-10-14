// Drift check of the committed OpenAPI bundles (design 07 section 12, ADR-020): `redocly bundle` output must equal
// internal/apispec/*.bundled.yaml byte for byte. `redocly` comes from node_modules/.bin (pnpm run puts it on PATH). Run by `make api-check` (pnpm api:bundle-check) and the `api` CI job.
import { execFileSync } from "node:child_process"
import { mkdtempSync, readFileSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"

const repo = new URL("../../", import.meta.url).pathname
const pairs = [
  ["api/openapi.yaml", "internal/apispec/openapi.bundled.yaml"],
  ["api/openapi.admin.yaml", "internal/apispec/openapi.admin.bundled.yaml"],
] as const

const tmp = mkdtempSync(join(tmpdir(), "scp-bundle-"))
const stale: string[] = []
try {
  for (const [source, committed] of pairs) {
    const out = join(tmp, committed.split("/").pop() ?? "out.yaml")
    execFileSync("redocly", ["bundle", join(repo, source), "-o", out], { stdio: ["ignore", "ignore", "inherit"] })
    if (readFileSync(out, "utf8") !== readFileSync(join(repo, committed), "utf8")) stale.push(committed)
  }
} finally {
  rmSync(tmp, { recursive: true, force: true })
}
if (stale.length) {
  console.error(`bundles are out of date: ${stale.join(", ")}; run \`make gen\` and commit`)
  process.exit(1)
}
console.log("ok   committed OpenAPI bundles equal a fresh `redocly bundle`")
