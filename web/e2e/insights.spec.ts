import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "./fixtures"

// cmd/scp/e2e_test.go: acme/whydah-core has one stable release (v1.2.0) and a pre-release; acme/legacy-sso is archived (retired, so left out).

const questions = [
  "How often does the fleet ship?",
  "What is the fleet built on?",
  "What does the fleet lean on?",
  "Where does knowledge sit with one person?",
  "What has gone quiet?",
]

test("/insights answers its five questions and passes axe in both themes", async ({ page }) => {
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.goto("/insights")
    await expect(page.getByRole("heading", { level: 1, name: "Insights", exact: true })).toBeVisible()
    for (const q of questions) await expect(page.getByRole("heading", { name: q })).toBeVisible()
    await expect(page.getByText("1 stable release in the last 12 months.")).toBeVisible()
    await expect(page.getByRole("img", { name: /Stable releases per month/ })).toBeVisible()
    const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze()
    expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
  }
  await expect(page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Insights" })).toHaveAttribute(
    "aria-current",
    "page",
  )
  await page.getByLabel("Product").selectOption("whydah")
  await page.getByRole("button", { name: "Apply" }).click()
  await expect(page).toHaveURL(/\/insights\?group=whydah$/)
  await expect(page.getByText("1 stable release in the last 12 months.")).toBeVisible()
})

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false })

  test("/insights is on the page and the form picks a product", async ({ page }) => {
    await page.goto("/insights")
    for (const q of questions) await expect(page.getByRole("heading", { name: q })).toBeVisible()
    await page.getByLabel("Product").selectOption("other")
    await page.getByRole("button", { name: "Apply" }).click()
    await expect(page).toHaveURL(/\/insights\?group=other$/)
    await expect(page.getByRole("heading", { name: questions[0] })).toBeVisible()
  })
})
