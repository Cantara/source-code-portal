// After `vite build`: move the Vite manifest out of dist (it is not served) and write dist/routes.json (06-httpapi 3.5).
import { mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs"
import { shapes } from "./route-shapes.ts"

const dist = new URL("../dist/", import.meta.url)
const build = new URL("../.build/", import.meta.url)
mkdirSync(build, { recursive: true })
renameSync(new URL(".vite/manifest.json", dist), new URL("manifest.json", build))
rmSync(new URL(".vite/", dist), { recursive: true, force: true })

const buildId = process.env["BUILD_ID"] ?? "dev"
const manifest = { version: 1, buildId, routes: shapes() }
writeFileSync(new URL("routes.json", dist), JSON.stringify(manifest) + "\n")
// The id compiled into the bundle must equal the one in routes.json (the Go side compares it with the ldflags id).
// Rolldown may place app code in a shared chunk, so search the whole initial closure (entry + static imports), not the entry file only.
type Chunk = { file: string; imports?: string[] }
const m = JSON.parse(readFileSync(new URL("manifest.json", build), "utf8")) as Record<string, Chunk>
const seen = new Set<string>()
const walk = (k: string) => {
  if (seen.has(k) || !m[k]) return
  seen.add(k)
  for (const i of m[k].imports ?? []) walk(i)
}
walk("index.html")
const files = [...seen].map((k) => (m[k] as Chunk).file)
if (buildId !== "dev" && !files.some((f) => readFileSync(new URL(f, dist), "utf8").includes(buildId)))
  throw new Error(`build id ${buildId} not found in the initial chunks ${files.join(", ")}`)
console.log(`finalize: routes.json with ${manifest.routes.length} shapes, buildId=${buildId}`)
