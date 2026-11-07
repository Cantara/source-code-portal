import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "@playwright/test"

// cmd/scp/e2e_setup_test.go serves the personal edition's setup page against a fake GitHub (the GitHub CLI "has" a token); the entry
// code works once, so the whole walk is one test. It runs among the @writes (one worker, after the readers).
const setup = "http://127.0.0.1:9192"

test("setting up a portal: sign in, read the repositories, sort them into products by drag and drop, start @writes", async ({
  page,
}) => {
  const problems: string[] = []
  page.on("pageerror", (e) => problems.push(String(e)))
  page.on("console", (m) => {
    if (m.type() === "error") problems.push(m.text())
  })
  await page.goto(`${setup}/setup/?code=e2e-entry-code`)
  await page.getByRole("button", { name: "Use my GitHub CLI login" }).click()
  await expect(page.getByRole("heading", { name: "Signed in as octo" })).toBeVisible()
  await page.getByRole("button", { name: "Continue" }).click()

  // The read shows its progress, then the editor opens with the suggestions: whydah-* and kcp-* each a product with a rule.
  await expect(page.getByRole("heading", { name: "Sort your repositories into products" })).toBeVisible()
  const whydah = page.locator(".pcard", { has: page.locator("input[value=Whydah]") })
  await expect(whydah.locator(".rule-text")).toHaveText("name starts with “whydah-”")
  await expect(whydah.locator(".meta")).toContainText("new repositories fit")
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])

  // Start again from nothing: drag one repository into a new product; the page notices the three others that start the same.
  await page.getByRole("button", { name: "Clear everything" }).click()
  await expect(page.locator(".pcard")).toHaveCount(0)
  await page.locator("#q").fill("kcp-triage")
  await page.locator("li.repo", { hasText: "kcp-triage" }).dragTo(page.locator(".zone", { hasText: "New product" }))
  const hint = page.locator(".hint")
  await expect(hint).toContainText("3 more repositories start with “kcp-”")
  await hint.getByRole("button", { name: "Add them with a rule" }).click()
  const kcp = page.locator(".pcard").first()
  await expect(kcp.locator(".rule-text")).toHaveText("name starts with “kcp-”")
  await expect(kcp.locator(".meta")).toContainText("covers 4")
  await expect(kcp.getByText("repositories start with")).toHaveCount(0)

  // A rule of one's own, tested before it is added; the keyboard's way: tick, then move with the menu.
  await page.locator("#q").fill("whydah")
  await page.getByRole("button", { name: /^Select the 4 shown$/ }).click()
  await page.locator("#move").selectOption("new")
  await page.getByRole("button", { name: "Move", exact: true }).click()
  await expect(page.locator(".pcard")).toHaveCount(2)
  await page.locator("#q").fill("")
  await page.locator("li.repo", { hasText: "notes" }).locator("input[type=checkbox]").check()
  await page.locator("#move").selectOption("hide")
  await page.getByRole("button", { name: "Move", exact: true }).click()
  await expect(page.locator(".zone", { hasText: "Hidden" })).toContainText("1")

  await page.getByRole("button", { name: "Start my portal" }).click()
  await expect(page.getByRole("heading", { name: "Starting your portal" })).toBeVisible()
  await expect(page.locator("progress")).toBeVisible()
  expect(problems).toEqual([])
})
