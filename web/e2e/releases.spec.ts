import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "./fixtures"

// the rows of the releases list (the notes of a row hold lists of their own)
const rowsOf = (page: import("@playwright/test").Page | import("@playwright/test").Locator) =>
  page.getByRole("list", { name: "Releases" }).locator(":scope > li")

// cmd/scp/e2e_test.go: acme/whydah-core has a stable release v1.2.0 and a newer pre-release v1.3.0-rc1; no other repository has one.

test("/releases lists the latest release of each repository, the variants filter, and it passes axe in both themes", async ({
  page,
}) => {
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.goto("/releases")
    await expect(page.getByRole("heading", { level: 1, name: "Releases", exact: true })).toBeVisible()
    const rows = rowsOf(page)
    await expect(rows).toHaveCount(2) // whydah-sso has no GitHub release: its Maven package is its latest
    await expect(rows.first()).toContainText("acme/whydah-core")
    await expect(rows.first()).toContainText("v1.2.0")
    await expect(rows.first()).toContainText("Highlights")
    await expect(rows.first()).not.toContainText("Pre-release")
    await expect(rows.first()).toContainText("GitHub")
    await expect(rows.nth(1)).toContainText("acme/whydah-sso")
    await expect(rows.nth(1)).toContainText("1.4.0")
    await expect(rows.nth(1)).toContainText("Maven")
    const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze()
    expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
  }
  await expect(page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Releases" })).toHaveAttribute(
    "aria-current",
    "page",
  )
  const source = page.getByRole("radiogroup", { name: "Source" })
  await source.getByRole("radio", { name: /Packages 2/ }).click() // each repository's newest package
  await expect(page).toHaveURL(/\/releases\?source=packages$/)
  await expect(rowsOf(page)).toHaveCount(2)
  await expect(page.getByRole("list", { name: "Releases" })).not.toContainText("GitHub")
  await source.getByRole("radio", { name: "GitHub and packages" }).click()
  await expect(page).toHaveURL(/\/releases$/)
  const which = page.getByRole("radiogroup", { name: "Which releases" })
  await expect(which.getByRole("radio", { name: /Every release 4/ })).toBeVisible()
  await which.getByRole("radio", { name: /Pre-releases 1/ }).click()
  await expect(page).toHaveURL(/\/releases\?variant=pre$/)
  const rows = rowsOf(page)
  await expect(rows).toHaveCount(1)
  await expect(rows.first()).toContainText("v1.3.0-rc1")
  await expect(rows.first()).toContainText("Pre-release")
  await page.getByRole("radio", { name: "Last 7 days" }).click()
  await expect(page).toHaveURL(/variant=pre&since=7d/)
  await expect(rows).toHaveCount(1)
})

test("a release's notes are a disclosure: its own body, or the changelog's section of its version", async ({ page }) => {
  await page.goto("/releases")
  const rows = rowsOf(page)
  const own = rows.first().locator("details")
  await expect(own.getByText("Single sign-on")).toBeHidden()
  await own.getByText("Release notes", { exact: true }).click()
  await expect(own.getByText("Single sign-on")).toBeVisible() // the release's body, its heading a bold paragraph
  await expect(own.locator("h1, h2, h3")).toHaveCount(0)
  const pkg = rows.nth(1).locator("details") // the Maven package has no GitHub release: the changelog tells 1.4.0
  await expect(pkg.getByText("Release notes from the changelog")).toBeVisible()
  await pkg.getByText("Release notes from the changelog").click()
  await expect(pkg.getByText("Sign in over OIDC")).toBeVisible()
  await page.goto("/repos/acme/whydah-core")
  const repo = page.getByRole("region", { name: "Releases" })
  await repo.getByText("Release notes").first().click() // a repository's panel shows them too
  await expect(repo.getByText("Single sign-on")).toBeVisible()
})

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false })

  test("/releases shows every release and filters by the form", async ({ page }) => {
    await page.goto("/releases?variant=all")
    await expect(rowsOf(page)).toHaveCount(4) // two GitHub releases, two Maven packages
    await page.getByRole("radio", { name: "Oldest first" }).check()
    await page.getByRole("button", { name: "Apply" }).click()
    await expect(page).toHaveURL(/variant=all.*sort=oldest/)
    await expect(rowsOf(page).first()).toContainText("1.4.0") // the Maven packages are older
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
  await expect(rowsOf(repo)).toHaveCount(3) // its Maven package too
  await expect(repo).toContainText("Pre-release")
  await expect(repo.getByRole("link", { name: "All 3 held" })).toHaveAttribute(
    "href",
    "/releases?repo=acme%2Fwhydah-core&variant=all",
  )

  await page.goto("/groups/other")
  await page.getByRole("link", { name: "1 retired" }).click()
  await expect(page).toHaveURL(/\/archive\?group=other$/)
})
