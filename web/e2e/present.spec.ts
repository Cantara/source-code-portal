import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "./fixtures"

// design/present.md: the presentation page fits the screen it is on, without the header, every product a tile or in the fold.

for (const [width, height] of [
  [1180, 820],
  [1366, 1024],
  [1920, 1080],
  [820, 1180],
] as const) {
  test(`/present fills ${width}×${height} without scrolling`, async ({ page }) => {
    await page.setViewportSize({ width, height })
    await page.goto("/present")
    await expect(page.getByRole("region", { name: "Latest commits" }).getByRole("listitem").first()).toBeVisible()
    await expect(page.getByRole("banner")).toHaveCount(0)
    const size = await page.evaluate(() => {
      const d = document.documentElement
      return { sh: d.scrollHeight, ch: d.clientHeight, sw: d.scrollWidth, cw: d.clientWidth }
    })
    expect(size.sh).toBe(size.ch)
    expect(size.sw).toBe(size.cw)
    const products = page.getByRole("region", { name: "Products" })
    const groups = (await (await page.request.get("/api/v1/dashboard")).json()) as { groups: { name: string }[] }
    const text = (await products.textContent()) ?? ""
    for (const g of groups.groups) expect(text).toContain(g.name)
    await expect(page.getByRole("status").filter({ hasText: /Live|Polling/ })).toBeVisible()
  })
}

test("/present?groups= shows only the products it names", async ({ page }) => {
  await page.goto("/present?groups=whydah")
  const products = page.getByRole("region", { name: "Products" })
  await expect(products.getByRole("link", { name: "Whydah" })).toBeVisible()
  await expect(products.getByRole("link", { name: "Other" })).toHaveCount(0)
})

test("/present is dark unless the reader chose, and a tap or a key shows its bar", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" })
  await page.goto("/present")
  await expect(page.locator("html")).toHaveClass(/dark/)
  const bar = page.getByRole("navigation", { name: "Presentation" })
  await expect(bar).toHaveCSS("opacity", "0")
  await expect(bar.getByRole("button", { name: "Design this page" })).toHaveCount(0)
  await expect(async () => {
    await page.mouse.click(10, 10)
    await expect(bar).toHaveCSS("opacity", "1", { timeout: 1000 })
  }).toPass()
  await bar.getByRole("button", { name: /^Theme/ }).click()
  await expect(page.locator("html")).toHaveClass(/dark/)
  await bar.getByRole("button", { name: /^Theme/ }).click()
  await expect(page.locator("html")).not.toHaveClass(/dark/)
  await expect(bar.getByRole("link", { name: "Dashboard" })).toHaveAttribute("href", "/")
  const manifest = await (await page.request.get("/manifest.webmanifest")).json()
  expect(manifest).toMatchObject({ start_url: "/present", display: "standalone" })
  await page.evaluate(() => localStorage.removeItem("scp-theme"))
})

test("/present passes axe in both themes, the reader's choice", async ({ page }) => {
  for (const theme of ["dark", "light"] as const) {
    await page.goto("/present")
    await page.evaluate((t) => localStorage.setItem("scp-theme", t), theme)
    await page.reload()
    await expect(page.getByRole("heading", { level: 1, name: "Presentation" })).toBeAttached()
    await expect(page.locator("html")).toHaveClass(theme === "dark" ? /dark/ : /^(?!.*dark)/)
    await page.waitForLoadState("networkidle")
    const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze()
    expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
  }
  await page.evaluate(() => localStorage.removeItem("scp-theme"))
})
