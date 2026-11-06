import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "./fixtures"

// cmd/scp/e2e_test.go: acme/whydah-core has a stable release v1.2.0 and a newer pre-release v1.3.0-rc1; no other repository has one.

test("/releases lists the latest release of each repository, the variants filter, and it passes axe in both themes", async ({
  page,
}) => {
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.goto("/releases")
    await expect(page.getByRole("heading", { level: 1, name: "Releases", exact: true })).toBeVisible()
    const rows = page.getByRole("list", { name: "Release list" }).getByRole("listitem")
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText("acme/whydah-core")
    await expect(rows.first()).toContainText("v1.2.0")
    await expect(rows.first()).toContainText("Highlights")
    await expect(rows.first()).not.toContainText("Pre-release")
    const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze()
    expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
  }
  await expect(page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Releases" })).toHaveAttribute(
    "aria-current",
    "page",
  )
  const chips = page.getByRole("navigation", { name: "By variant" })
  await expect(chips.getByRole("link", { name: /Every release 2/ })).toBeVisible()
  await chips.getByRole("link", { name: /Pre-releases 1/ }).click()
  await expect(page).toHaveURL(/\/releases\?variant=pre$/)
  const rows = page.getByRole("list", { name: "Release list" }).getByRole("listitem")
  await expect(rows).toHaveCount(1)
  await expect(rows.first()).toContainText("v1.3.0-rc1")
  await expect(rows.first()).toContainText("Pre-release")
  await page.getByLabel("Published").selectOption("7d")
  await page.getByRole("button", { name: "Apply" }).click()
  await expect(page).toHaveURL(/variant=pre&since=7d/)
  await expect(rows).toHaveCount(1)
})

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false })

  test("/releases shows every release and filters by the form", async ({ page }) => {
    await page.goto("/releases?variant=all")
    await expect(page.getByRole("list", { name: "Release list" }).getByRole("listitem")).toHaveCount(2)
    await page.getByLabel("Order").selectOption("oldest")
    await page.getByRole("button", { name: "Apply" }).click()
    await expect(page).toHaveURL(/variant=all.*sort=oldest/)
    await expect(page.getByRole("list", { name: "Release list" }).getByRole("listitem").first()).toContainText("v1.2.0")
  })
})
