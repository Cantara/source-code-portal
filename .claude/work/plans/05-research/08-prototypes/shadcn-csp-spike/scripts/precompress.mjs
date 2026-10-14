// Zero-dependency precompression of dist/ (br + gzip) for the Go embed handler.
import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs"
import { join, extname } from "node:path"
import { brotliCompressSync, gzipSync, constants } from "node:zlib"

const root = new URL("../dist", import.meta.url).pathname
const compressible = new Set([".js", ".css", ".html", ".svg", ".json", ".txt", ".map", ".xml"])
let raw = 0, br = 0, gz = 0
for (const name of readdirSync(root, { recursive: true })) {
  const p = join(root, String(name))
  if (!statSync(p).isFile() || !compressible.has(extname(p)) || statSync(p).size < 512) continue
  const buf = readFileSync(p)
  const b = brotliCompressSync(buf, { params: { [constants.BROTLI_PARAM_QUALITY]: 11, [constants.BROTLI_PARAM_SIZE_HINT]: buf.length } })
  const g = gzipSync(buf, { level: 9 })
  writeFileSync(p + ".br", b); writeFileSync(p + ".gz", g)
  raw += buf.length; br += b.length; gz += g.length
}
console.log(`precompressed: raw ${(raw / 1024).toFixed(0)} KiB, gzip ${(gz / 1024).toFixed(0)} KiB, brotli ${(br / 1024).toFixed(0)} KiB`)
