// Assembles web/bundle, the directory `scp serve` runs (web.dir): the standalone server of `next build` with its static files and
// public/. Fails when the static files outgrow their budget (gzip): JavaScript 300 KB, CSS 25 KB, and no fonts.
import { cpSync, readdirSync, readFileSync, rmSync } from "node:fs"
import { join } from "node:path"
import { gzipSync } from "node:zlib"

const web = new URL("..", import.meta.url).pathname
const out = join(web, "bundle")
rmSync(out, { recursive: true, force: true })
cpSync(join(web, ".next/standalone"), out, { recursive: true, verbatimSymlinks: true })
cpSync(join(web, ".next/static"), join(out, ".next/static"), { recursive: true })
cpSync(join(web, "public"), join(out, "public"), { recursive: true })

const kb = 1000
const files = readdirSync(join(web, ".next/static"), { recursive: true, encoding: "utf8" })
const gz = (f: string) => gzipSync(readFileSync(join(web, ".next/static", f))).length
const sum = (ext: string) => files.filter((f) => f.endsWith(ext)).reduce((n, f) => n + gz(f), 0)
const js = sum(".js")
const css = sum(".css")
const failures: string[] = []
if (js > 300 * kb) failures.push(`JavaScript ${js} B > 300 KB`)
if (css > 25 * kb) failures.push(`CSS ${css} B > 25 KB`)
for (const f of files) if (/\.(woff2?|ttf|otf)$/.test(f)) failures.push(`font ${f}: the UI uses the system font stack`)
console.log(`bundle: ${out}; JavaScript ${js} B, CSS ${css} B (gzip)`)
if (failures.length > 0) {
  console.error(failures.join("\n"))
  process.exit(1)
}
