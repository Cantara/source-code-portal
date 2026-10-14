// Fails the build when the bundle outgrows its budget (gzip sizes): initial JS 200 KB, initial CSS 25 KB, any lazy chunk 60 KB.
import { readdirSync, readFileSync } from "node:fs"
import { join } from "node:path"
import { gzipSync } from "node:zlib"

const dist = new URL("../dist/", import.meta.url).pathname
const kb = 1000
const gz = (file: string) => gzipSync(readFileSync(join(dist, file))).length

const html = readFileSync(join(dist, "index.html"), "utf8")
const initial = new Set([...html.matchAll(/(?:src|href)="\/(assets\/[^"]+)"/g)].map((m) => m[1] ?? ""))
const assets = readdirSync(join(dist, "assets")).map((f) => `assets/${f}`)

const failures: string[] = []
const sum = (ext: string) => [...initial].filter((f) => f.endsWith(ext)).reduce((n, f) => n + gz(f), 0)
const js = sum(".js")
const css = sum(".css")
if (js > 200 * kb) failures.push(`initial JS ${js} B > 200 KB`)
if (css > 25 * kb) failures.push(`initial CSS ${css} B > 25 KB`)
for (const f of assets) {
  if (f.endsWith(".js") && !initial.has(f) && gz(f) > 60 * kb) failures.push(`lazy chunk ${f} ${gz(f)} B > 60 KB`)
  if (/\.(woff2?|ttf|otf)$/.test(f)) failures.push(`font ${f}: the UI uses the system font stack`)
}
console.log(`budget: initial JS ${js} B, initial CSS ${css} B (gzip)`)
if (failures.length > 0) {
  console.error(failures.join("\n"))
  process.exit(1)
}
