// Mirrors webapp.New's start-time checks (06-httpapi 4.9) so a bad bundle fails in the web job, not at server start.
import { readdirSync, readFileSync, statSync, lstatSync } from "node:fs"
import { extname, join } from "node:path"
import { brotliDecompressSync, gunzipSync } from "node:zlib"

const root = new URL("../dist", import.meta.url).pathname
const ALLOWED = new Set([
  ".html",
  ".js",
  ".mjs",
  ".css",
  ".json",
  ".webmanifest",
  ".svg",
  ".png",
  ".jpg",
  ".jpeg",
  ".gif",
  ".webp",
  ".avif",
  ".ico",
  ".woff2",
  ".woff",
  ".txt",
  ".br",
  ".gz",
])
const HASHED = /-[0-9a-f]{8}\.[a-z0-9]+(\.br|\.gz)?$/
const problems: string[] = []
let total = 0,
  html = 0
const files = readdirSync(root, { recursive: true })
  .map(String)
  .filter((n) => statSync(join(root, n)).isFile())
const names = new Set(files)
for (const n of files) {
  const p = join(root, n)
  if (lstatSync(p).isSymbolicLink()) problems.push(`${n}: symlink`)
  const size = statSync(p).size
  total += size
  if (size > 8 << 20) problems.push(`${n}: larger than 8 MiB`)
  const ext = extname(n)
  if (ext === ".map") problems.push(`${n}: source maps must not be embedded`)
  else if (!ALLOWED.has(ext)) problems.push(`${n}: extension ${ext} not allowed`)
  if (n.startsWith("api/")) problems.push(`${n}: a file under api/ would shadow the API`)
  if (n.endsWith(".html")) html++
  if (n.startsWith("assets/") && !HASHED.test(n)) problems.push(`${n}: files under assets/ must carry a content hash`)
  if (ext === ".br" || ext === ".gz") {
    const base = n.slice(0, -ext.length)
    if (!names.has(base)) {
      problems.push(`${n}: orphan sibling`)
      continue
    }
    const plain = readFileSync(join(root, base))
    const back = ext === ".br" ? brotliDecompressSync(readFileSync(p)) : gunzipSync(readFileSync(p))
    if (!plain.equals(back)) problems.push(`${n}: does not decompress to ${base}`)
  } else if ([".html", ".js", ".css", ".json", ".svg", ".webmanifest", ".txt"].includes(ext) && size >= 1024 && !names.has(n + ".br")) {
    problems.push(`${n}: compressible file >= 1 KiB without a .br sibling`)
  }
}
if (total > 16 << 20) problems.push("dist larger than 16 MiB")
if (html !== 1 || !names.has("index.html")) problems.push(`exactly one html file (index.html) allowed, found ${html}`)
if (!names.has("routes.json")) problems.push("routes.json missing")
if (!names.has("theme-init.js")) problems.push("theme-init.js missing")
const index = readFileSync(join(root, "index.html"), "utf8")
if (/<script(?![^>]*\bsrc=)[^>]*>/i.test(index)) problems.push("index.html contains an inline script (CSP script-src 'self')")
if (/<style[\s>]/i.test(index) || /\sstyle=/i.test(index)) problems.push("index.html contains inline style (CSP style-src 'self')")
if (/\son[a-z]+=/i.test(index)) problems.push("index.html contains an inline event handler")
if (/https?:\/\//.test(index.replace(/<noscript>[\s\S]*?<\/noscript>/g, "")))
  problems.push("index.html references an absolute URL (no third-party origins)")
if (
  process.env["RELEASE"] === "1" &&
  (JSON.parse(readFileSync(join(root, "routes.json"), "utf8")) as { buildId: string }).buildId === "dev"
)
  problems.push("RELEASE build with buildId dev")
if (problems.length) {
  console.error("verify-dist FAILED:\n  " + problems.join("\n  "))
  process.exit(1)
}
console.log(`verify-dist: ${files.length} files, ${(total / 1024).toFixed(0)} KiB, ok`)
