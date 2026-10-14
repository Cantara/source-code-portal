// Bundle budgets (architecture 12.3), measured on gzip -9 of what a cold request downloads. Reads the Vite manifest kept in .build/.
import { readFileSync } from "node:fs"
import { gzipSync } from "node:zlib"

type Chunk = { file: string; src?: string; imports?: string[]; dynamicImports?: string[]; css?: string[]; isEntry?: boolean }
const dist = new URL("../dist/", import.meta.url)
const m = JSON.parse(readFileSync(new URL("../.build/manifest.json", import.meta.url), "utf8")) as Record<string, Chunk>
const gz = (file: string) => gzipSync(readFileSync(new URL(file, dist)), { level: 9 }).length
const KB = 1024

function closure(key: string, seen = new Set<string>()): Set<string> {
  if (seen.has(key)) return seen
  seen.add(key)
  for (const i of m[key]?.imports ?? []) closure(i, seen)
  return seen
}
const entryKey = Object.keys(m).find((k) => m[k]?.isEntry)!
const initial = closure(entryKey)
const initialJs = [...initial].reduce((a, k) => a + gz(m[k]!.file), 0)
// each CSS file once, even when several initial chunks list it (a Set: the first version dropped duplicates instead of counting them once)
const initialCssFiles = new Set([...initial].flatMap((k) => m[k]!.css ?? []))
const initialCss = [...initialCssFiles].reduce((a, f) => a + gz(f), 0)

const results: { name: string; bytes: number; limit: number }[] = [
  { name: "initial JS (entry + static imports)", bytes: initialJs, limit: 200 * KB },
  { name: "initial CSS", bytes: initialCss, limit: 25 * KB },
]
// per lazy route chunk (everything reached by a dynamic import, excluding what the initial closure already has)
for (const [k, c] of Object.entries(m)) {
  if (!c.src?.includes("?tsr-split") && !c.src?.includes("features/")) continue
  if (initial.has(k)) continue
  const chunks = [...closure(k)].filter((x) => !initial.has(x))
  const files = new Set(chunks.flatMap((x) => [m[x]!.file, ...(m[x]!.css ?? []).filter((f) => !initialCssFiles.has(f))])) // the route's own CSS counts too
  const own = [...files].reduce((a, f) => a + gz(f), 0)
  results.push({
    name: `lazy ${c.src}`,
    bytes: own,
    limit: c.file.includes("graph") ? 70 * KB : c.file.includes("chart") ? 120 * KB : 60 * KB,
  })
}
let failed = false
for (const r of results) {
  const ok = r.bytes <= r.limit
  failed ||= !ok
  console.log(`${ok ? "ok  " : "FAIL"} ${(r.bytes / KB).toFixed(1).padStart(7)} KB / ${(r.limit / KB).toFixed(0)} KB  ${r.name}`)
}
if (failed) process.exit(1)
