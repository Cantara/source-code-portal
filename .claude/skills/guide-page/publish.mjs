// Publishes the guide's screenshots: every PNG that capture.js wrote to the folder (argument 1, default /tmp/scp-guide) becomes WebP
// in web/public/guide/ (cwebp, quality 90), and web/src/features/guide/shots.json gets each picture's size in CSS pixels (half its
// pixels) and the card's callouts, read from the JSON file of argument 2 (what capture.js returned), or kept when it is not given.
// Run from the repository root: node .claude/skills/guide-page/publish.mjs /tmp/scp-guide /tmp/scp-guide/callouts.json
import { execFileSync } from "node:child_process"
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs"
import { join } from "node:path"

const src = process.argv[2] ?? "/tmp/scp-guide"
const out = "web/public/guide"
const json = "web/src/features/guide/shots.json"
mkdirSync(out, { recursive: true })
const old = existsSync(json) ? JSON.parse(readFileSync(json, "utf8")) : { callouts: {}, sizes: {} }
const callouts = process.argv[3] ? JSON.parse(readFileSync(process.argv[3], "utf8")) : old.callouts
const sizes = {}
for (const f of readdirSync(src).filter((f) => f.endsWith(".png")).sort()) {
  const png = readFileSync(join(src, f))
  const name = f.replace(/-(light|dark)\.png$/, "")
  sizes[name] = { width: png.readUInt32BE(16) / 2, height: png.readUInt32BE(20) / 2 }
  execFileSync("cwebp", ["-quiet", "-q", "90", "-m", "6", join(src, f), "-o", join(out, f.replace(/\.png$/, ".webp"))])
}
writeFileSync(json, JSON.stringify({ callouts, sizes }, null, 2) + "\n")
console.log(`${Object.keys(sizes).length} pictures, ${Object.keys(callouts).length} callouts -> ${out}, ${json}`)
