import { chromium } from "@playwright/test"
const [, , base, out, ...pages] = process.argv
const b = await chromium.launch()
for (const scheme of ["dark", "light"]) {
  const p = await b.newPage({ viewport: { width: 1600, height: 1000 }, colorScheme: scheme })
  for (const spec of pages) {
    const [name, path] = spec.split("=")
    await p.goto(base + path, { waitUntil: "networkidle" })
    await p.screenshot({ path: `${out}/${name}-${scheme}.png`, fullPage: false })
  }
  await p.close()
}
await b.close()
