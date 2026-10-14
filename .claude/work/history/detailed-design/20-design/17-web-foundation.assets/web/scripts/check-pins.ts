// Fails when a dependency range is not an exact version (supply-chain rule, architecture 12: exact pins, Renovate bumps).
import { readFileSync } from "node:fs"

const pkg = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8")) as Record<
  string,
  Record<string, string> | undefined
>
const EXACT = /^\d+\.\d+\.\d+(?:-[\w.]+)?$/
const ALIAS = /^npm:(@?[\w./-]+)@\d+\.\d+\.\d+$/
const bad: string[] = []
for (const section of ["dependencies", "devDependencies", "optionalDependencies"]) {
  for (const [name, range] of Object.entries(pkg[section] ?? {})) {
    if (!EXACT.test(range) && !ALIAS.test(range)) bad.push(`${section}.${name}: ${range}`)
  }
}
const overrides = (pkg["pnpm"] ?? {})["overrides"]
if (overrides) bad.push("package.json pnpm.overrides is not allowed; use pnpm-workspace.yaml overrides with a comment and an expiry")
if (bad.length) {
  console.error("non-exact dependency ranges:\n  " + bad.join("\n  "))
  process.exit(1)
}
console.log("ok   all dependency ranges are exact")
