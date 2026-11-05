import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "./fixtures"

// cmd/scp/e2e_test.go: Whydah-Core has #21 needing triage, the bug #20 nobody took, the feature #19 assigned to ann and Renovate's
// dependency dashboard #2; its open pull requests are in GitHub's listing too and stay out.

test("/issues lists what waits first, its states filter, and passes axe in both themes", async ({ page }) => {
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.goto("/issues")
    await expect(page.getByRole("heading", { level: 1, name: "Issues", exact: true })).toBeVisible()
    const items = page.getByRole("list", { name: "Issues" }).getByRole("listitem")
    await expect(items).toHaveCount(4)
    await expect(items.first()).toContainText("Login loops after a password reset")
    await expect(items.nth(2)).toContainText("for ann")
    await expect(items.last()).toContainText("(automation)")
    await page.waitForLoadState("networkidle")
    const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze()
    expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
  }
  await expect(page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Issues" })).toHaveAttribute(
    "aria-current",
    "page",
  )
  const chips = page.getByRole("navigation", { name: "By where they stand" })
  await chips.getByRole("link", { name: /Unassigned 1/ }).click()
  await expect(page).toHaveURL(/\/issues\?state=unassigned$/)
  await expect(page.getByRole("list", { name: "Issues" }).getByRole("listitem")).toHaveCount(1)
  await expect(page.getByRole("link", { name: "Tokens expire an hour early" })).toBeVisible()
  await page.getByLabel("Kind").selectOption("feature")
  await page.getByRole("button", { name: "Apply" }).click()
  await expect(page).toHaveURL(/state=unassigned&kind=feature/)
  await expect(page.getByText("No open issue matches")).toBeVisible()
})

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false })

  test("/issues filters by state and by the form", async ({ page }) => {
    await page.goto("/issues?state=triage")
    const items = page.getByRole("list", { name: "Issues" }).getByRole("listitem")
    await expect(items).toHaveCount(2)
    await page.getByLabel("Opened by").selectOption("people")
    await page.getByRole("button", { name: "Apply" }).click()
    await expect(page).toHaveURL(/by=people/)
    await expect(page.getByRole("list", { name: "Issues" }).getByRole("listitem")).toHaveCount(1)
  })
})

test("a group and a repository show their issues", async ({ page }) => {
  await page.goto("/groups/whydah")
  const panel = page.getByRole("region", { name: "Issues" })
  await expect(panel).toContainText("2 needing triage")
  await panel.getByRole("link", { name: "All 4 open" }).click()
  await expect(page).toHaveURL(/\/issues\?group=whydah$/)

  await page.goto("/repos/acme/whydah-core")
  const repo = page.getByRole("region", { name: "Issues" })
  await expect(repo).toContainText("Needs triage 2 · Unassigned 1 · Assigned 1")
  await expect(repo.getByRole("link", { name: "All 4 open" })).toHaveAttribute("href", "/issues?repo=acme%2Fwhydah-core")
})
