import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "./fixtures"

// The filter panel of the list pages (design/filters.md): every choice in view at the left, hidden with its button and kept for all
// of them, and every list's rows start and end at the same edges.

const lists = ["/commits", "/pulls", "/issues", "/releases?variant=all", "/archive"]

test("the filter panel hides, shows again and is remembered by every list page", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto("/pulls")
  const panel = page.getByRole("complementary", { name: "Filter pull requests" })
  await expect(panel.getByRole("radiogroup", { name: "Whose turn" })).toBeVisible()
  await expect(panel.getByRole("radiogroup", { name: "Product" })).toBeVisible() // every choice at once, no menu to open
  await page.getByRole("button", { name: "Hide the filters" }).click()
  await expect(page.getByRole("radiogroup", { name: "Whose turn" })).toHaveCount(0)
  await page.goto("/issues")
  await expect(page.getByRole("button", { name: "Show the filters" })).toBeVisible() // the same choice on another list
  await page.reload()
  await expect(page.getByRole("button", { name: "Show the filters" })).toBeVisible()
  await page.getByRole("button", { name: "Show the filters" }).click()
  await expect(page.getByRole("radiogroup", { name: "Where it stands" })).toBeVisible()
  await page.goto("/pulls")
  await expect(page.getByRole("radiogroup", { name: "Whose turn" })).toBeVisible()
})

test("a choice applies at once, Clear undoes it, and the panel passes axe in both themes", async ({ page }) => {
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.goto("/commits")
    await page.getByText("Hide automation").click()
    await expect(page).toHaveURL(/\/commits\?automation=false$/)
    const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze()
    expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
    await page.getByRole("link", { name: "Clear" }).click()
    await expect(page).toHaveURL(/\/commits$/)
  }
})

test("a phone has the panel folded to a bar until it is asked for", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 800 })
  await page.context().clearCookies({ name: "scp-filters" })
  await page.goto("/releases")
  await expect(page.getByRole("radiogroup", { name: "Which releases" })).toBeHidden()
  await page.getByRole("button", { name: "Show the filters" }).click()
  await expect(page.getByRole("radiogroup", { name: "Which releases" })).toBeVisible()
})

test("every list's rows end at the same right edge: the aside of a row sits at the list's end", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 })
  const ends: Record<string, number> = {}
  for (const path of lists) {
    await page.goto(path)
    const row = page.locator("main ol > li:has(span.ml-auto)").first() // a row with its aside
    await expect(row).toBeVisible()
    const box = await row.boundingBox()
    const aside = await row.locator("span.ml-auto").first().boundingBox()
    expect(box, path).not.toBeNull()
    expect(aside, `${path} has an aside`).not.toBeNull()
    ends[path] = Math.round((box?.x ?? 0) + (box?.width ?? 0) - ((aside?.x ?? 0) + (aside?.width ?? 0)))
  }
  // Each aside ends the same distance from its row's end (the row's padding), on every list.
  expect(new Set(Object.values(ends)).size, JSON.stringify(ends)).toBe(1)
})
