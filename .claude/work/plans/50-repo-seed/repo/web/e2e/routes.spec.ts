import AxeBuilder from "@axe-core/playwright"
import { test, expect } from "./fixtures"
import { PAGES } from "./pages.ts"

const TAGS = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]

for (const theme of ["light", "dark"] as const) {
  for (const path of PAGES) {
    test(`axe ${theme} ${path}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme })
      const res = await page.goto(path)
      expect(res?.status()).toBe(200)
      await expect(page.locator("main h1")).toBeVisible()
      // WCAG 1.4.10 reflow: no horizontal page scroll at 360 px (and none at 1280 px)
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
      const results = await new AxeBuilder({ page }).withTags(TAGS).analyze()
      expect(results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(" | ")}`)).toEqual([])
    })
  }
}

test("unknown route is a real 404 that renders the NotFound page", async ({ page }) => {
  const res = await page.goto("/nope/never")
  expect(res?.status()).toBe(404)
  await expect(page.getByRole("heading", { level: 1, name: "Page not found" })).toBeVisible()
})

test("a stale chunk name is a plain 404, never HTML", async ({ request }) => {
  const res = await request.get("/assets/index-deadbeef.js")
  expect(res.status()).toBe(404)
  expect(res.headers()["content-type"]).toContain("text/plain")
})

// WCAG 1.4.10 sets the reflow threshold at 320 CSS px (the `phone` project is 360 px, the layout target): prove the smaller width too.
test("no horizontal scroll at 320 px on every page", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 640 })
  for (const path of PAGES) {
    await page.goto(path)
    await expect(page.locator("main h1")).toBeVisible()
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    expect(overflow, path).toBeLessThanOrEqual(0)
  }
})
