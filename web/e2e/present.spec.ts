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
