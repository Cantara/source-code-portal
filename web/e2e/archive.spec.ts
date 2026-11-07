import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "./fixtures"

// cmd/scp/e2e_test.go: acme/legacy-sso is archived on GitHub, so it is the portal's one archive entry; everything else is live.

test("/archive lists the retired repositories with why, and passes axe in both themes", async ({ page }) => {
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.goto("/archive")
    await expect(page.getByRole("heading", { level: 1, name: "Archive", exact: true })).toBeVisible()
    const rows = page.getByRole("list", { name: "Archive" }).getByRole("listitem")
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText("acme/legacy-sso")
    await expect(rows.first()).toContainText("Archived")
    await expect(rows.first()).toContainText("Retired single sign-on")
    const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze()
    expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
  }
  await expect(page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Archive" })).toHaveAttribute(
    "aria-current",
    "page",
  )
  const reasons = page.getByRole("radiogroup", { name: "Reason" })
  await expect(reasons.getByRole("radio", { name: /Deprecated 0/ })).toBeDisabled() // none: dimmed, still in view
  await reasons.getByRole("radio", { name: /Archived 1/ }).click()
  await expect(page).toHaveURL(/\/archive\?reason=archived$/)
  await page.getByText("Still depended on").click()
  await expect(page).toHaveURL(/reason=archived&used=true$/)
  await expect(page.getByText("No retired repository matches.")).toBeVisible()
})

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false })

  test("/archive filters by the form", async ({ page }) => {
    await page.goto("/archive")
    await expect(page.getByRole("list", { name: "Archive" }).getByRole("listitem")).toHaveCount(1)
    await page.getByLabel("Name contains").fill("nothing-like-this")
    await page.getByRole("button", { name: "Apply" }).click()
    await expect(page).toHaveURL(/q=nothing-like-this/)
    await expect(page.getByText("No retired repository matches.")).toBeVisible()
  })
})
