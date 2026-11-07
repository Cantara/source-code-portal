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
    const rows = page.getByRole("list", { name: "Releases" }).getByRole("listitem")
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
  const which = page.getByRole("radiogroup", { name: "Which releases" })
  await expect(which.getByRole("radio", { name: /Every release 2/ })).toBeVisible()
  await which.getByRole("radio", { name: /Pre-releases 1/ }).click()
  await expect(page).toHaveURL(/\/releases\?variant=pre$/)
  const rows = page.getByRole("list", { name: "Releases" }).getByRole("listitem")
  await expect(rows).toHaveCount(1)
  await expect(rows.first()).toContainText("v1.3.0-rc1")
  await expect(rows.first()).toContainText("Pre-release")
  await page.getByRole("radio", { name: "Last 7 days" }).click()
  await expect(page).toHaveURL(/variant=pre&since=7d/)
  await expect(rows).toHaveCount(1)
})

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false })

  test("/releases shows every release and filters by the form", async ({ page }) => {
    await page.goto("/releases?variant=all")
    await expect(page.getByRole("list", { name: "Releases" }).getByRole("listitem")).toHaveCount(2)
    await page.getByRole("radio", { name: "Oldest first" }).check()
    await page.getByRole("button", { name: "Apply" }).click()
    await expect(page).toHaveURL(/variant=all.*sort=oldest/)
    await expect(page.getByRole("list", { name: "Releases" }).getByRole("listitem").first()).toContainText("v1.2.0")
  })
})

test("a group and a repository show their releases, and a group its retired repositories", async ({ page }) => {
  await page.goto("/groups/whydah")
  const panel = page.getByRole("region", { name: "Releases" })
  await expect(panel).toContainText("acme/whydah-core")
  await expect(panel).toContainText("v1.2.0")
  await expect(panel.getByRole("link", { name: /retired/ })).toHaveCount(0)
  await panel.getByRole("link", { name: "All releases" }).click()
  await expect(page).toHaveURL(/\/releases\?group=whydah$/)

  await page.goto("/repos/acme/whydah-core")
  const repo = page.getByRole("region", { name: "Releases" })
  await expect(repo.getByRole("listitem")).toHaveCount(2)
  await expect(repo).toContainText("Pre-release")
  await expect(repo.getByRole("link", { name: "All 2 held" })).toHaveAttribute(
    "href",
    "/releases?repo=acme%2Fwhydah-core&variant=all",
  )

  await page.goto("/groups/other")
  await page.getByRole("link", { name: "1 retired" }).click()
  await expect(page).toHaveURL(/\/archive\?group=other$/)
})
