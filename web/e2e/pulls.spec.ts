import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "./fixtures"

// cmd/scp/e2e_test.go: Whydah-Core has #12 waiting for a review (on feature/sso), #11 ready, the draft #10 and Renovate's #9;
// whydah-sso has #3 with failing checks.

test("/pulls lists whose turn first, its states filter, and passes axe in both themes", async ({ page }) => {
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.goto("/pulls")
    await expect(page.getByRole("heading", { level: 1, name: "Pull requests", exact: true })).toBeVisible()
    const list = page.getByRole("list", { name: "Pull requests" }).getByRole("listitem")
    await expect(list.first()).toContainText("Bridge the SSO session")
    await expect(list).toHaveCount(4) // the draft waits until asked
    const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze()
    expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
  }
  const chips = page.getByRole("navigation", { name: "By whose turn" })
  await chips.getByRole("link", { name: /Failing checks 1/ }).click()
  await expect(page).toHaveURL(/\/pulls\?state=failing$/)
  await expect(page.getByRole("list", { name: "Pull requests" }).getByRole("listitem")).toHaveCount(1)
  await expect(page.getByRole("link", { name: "Keep the session on refresh" })).toBeVisible()
  await page.getByLabel("Opened by").selectOption("automation")
  await page.getByRole("button", { name: "Apply" }).click()
  await expect(page).toHaveURL(/state=failing&by=automation/)
  await expect(page.getByText("No open pull request in this state")).toBeVisible()
})

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false })

  test("/pulls filters by state and by the form", async ({ page }) => {
    await page.goto("/pulls?state=review")
    const items = page.getByRole("list", { name: "Pull requests" }).getByRole("listitem")
    await expect(items).toHaveCount(2)
    await expect(items.first()).toContainText("Bridge the SSO session")
    await expect(items.nth(1)).toContainText("chore(deps): update slf4j")
    await page.getByLabel("Show drafts").check()
    await page.getByLabel("Opened by").selectOption("people")
    await page.getByRole("button", { name: "Apply" }).click()
    await expect(page).toHaveURL(/by=people/)
    await expect(page.getByRole("list", { name: "Pull requests" }).getByRole("listitem")).toHaveCount(1)
  })
})

test("the dashboard, a group, a repository and its history show their pull requests", async ({ page }) => {
  await page.goto("/")
  const telemetry = page.getByRole("region", { name: "Telemetry" })
  await expect(telemetry.getByRole("link", { name: "Pull requests" })).toHaveAttribute("href", "/pulls")
  const element = page.getByRole("region", { name: "Pull requests" }).first()
  await expect(element).toContainText("2 waiting for a review, 1 ready to merge")
  await expect(element.getByRole("link", { name: "Bridge the SSO session" })).toBeVisible()
  await element.getByRole("link", { name: "All 5 open" }).click()
  await expect(page).toHaveURL(/\/pulls$/)

  await page.goto("/groups/whydah")
  await expect(page.getByRole("link", { name: "4 open pull requests" })).toHaveAttribute("href", "/pulls?repo=acme%2Fwhydah-core")
  await expect(page.getByRole("region", { name: "Pull requests" })).toContainText("2 waiting for a review")

  await page.goto("/repos/acme/whydah-core")
  const panel = page.getByRole("region", { name: "Pull requests" })
  await expect(panel).toContainText("4 open: Needs review 2 · Ready to merge 1 · Draft 1")
  await expect(panel.getByRole("link", { name: "Draft: rotate the signing keys" })).toBeVisible()

  await page.goto("/repos/acme/whydah-core/commits")
  await expect(page.getByText("pull request #12", { exact: false }).first()).toBeAttached()
  await expect(page.getByTitle(/Pull request #12: Bridge the SSO session \(needs review\)/).first()).toBeVisible()
})
