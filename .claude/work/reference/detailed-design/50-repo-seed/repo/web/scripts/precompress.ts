// Zero-dependency precompression of dist/ (brotli q11 + gzip -9). A sibling is kept only when smaller (06-httpapi 6.3).
import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs"
import { extname, join } from "node:path"
import { brotliCompressSync, constants, gzipSync } from "node:zlib"

const root = new URL("../dist", import.meta.url).pathname
const COMPRESSIBLE = new Set([".html", ".js", ".mjs", ".css", ".json", ".webmanifest", ".svg", ".txt", ".ico"])
let raw = 0,
  br = 0,
  gz = 0
for (const name of readdirSync(root, { recursive: true })) {
  const p = join(root, String(name))
  if (!statSync(p).isFile() || !COMPRESSIBLE.has(extname(p)) || statSync(p).size < 1024) continue
  const buf = readFileSync(p)
  const b = brotliCompressSync(buf, { params: { [constants.BROTLI_PARAM_QUALITY]: 11, [constants.BROTLI_PARAM_SIZE_HINT]: buf.length } })
  const g = gzipSync(buf, { level: 9 })
  if (b.length < buf.length) writeFileSync(p + ".br", b)
  if (g.length < buf.length) writeFileSync(p + ".gz", g)
  raw += buf.length
  br += b.length
  gz += g.length
}
console.log(`precompress: raw ${(raw / 1024).toFixed(0)} KiB, gzip ${(gz / 1024).toFixed(0)} KiB, brotli ${(br / 1024).toFixed(0)} KiB`)
