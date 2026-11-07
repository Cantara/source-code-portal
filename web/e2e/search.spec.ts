import { expect, test } from "./fixtures"

// cmd/scp/e2e_test.go: acme/Whydah-Core's docs/setup.md has the sections Install and Configure; the private acme/vault is staff's.

test("a document section is found by a word and opens at its heading", async ({ page }) => {
  await page.goto("/search?q=configure")
  const hit = page.getByRole("link", { name: /Setup › Configure/ })
  await expect(hit).toHaveAttribute("href", "/repos/acme/whydah-core/docs/docs/setup#doc-configure")
  await expect(page.getByRole("radiogroup", { name: "Kind" }).getByRole("radio", { name: /^Documents \d+$/ })).toBeVisible()
  await hit.click()
  await expect(page).toHaveURL(/\/repos\/acme\/whydah-core\/docs\/docs\/setup#doc-configure$/)
  await expect(page.getByRole("heading", { name: "Configure" })).toBeVisible()
})

test("the header's box suggests by name and opens a suggestion by the keyboard alone", async ({ page }) => {
  await page.goto("/")
  await page.keyboard.press("/")
  const box = page.getByRole("combobox", { name: "Search the portal" })
  await expect(box).toBeFocused()
  await page.keyboard.type("whydah-s")
  const list = page.getByRole("listbox", { name: "Suggestions" })
  await expect(list.getByRole("option").first()).toBeVisible()
  await page.keyboard.press("ArrowDown")
  await page.keyboard.press("Enter")
  await expect(page).toHaveURL(/\/repos\/acme\/whydah-sso$/)

  await page.keyboard.press("ControlOrMeta+k")
  await expect(box).toBeFocused()
  await page.keyboard.type("whydah")
  await expect(list.getByRole("option").first()).toBeVisible()
  await page.keyboard.press("Escape")
  await expect(list).toBeHidden()
  await page.keyboard.press("Enter")
  await expect(page).toHaveURL(/\/search\?q=whydah$/)
  await expect(page.getByRole("link", { name: /whydah-sso/ }).first()).toBeVisible()
})

test("a query the portal cannot search says why", async ({ page }) => {
  await page.goto("/search?q=in%3Arobots+x")
  await expect(page.getByRole("main").getByRole("alert")).toContainText("not a kind")
})

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false })

  test("the header's form searches and the hits are rendered on the server", async ({ page }) => {
    await page.goto("/")
    const box = page.getByLabel("Search the portal")
    await box.fill("configure")
    await box.press("Enter")
    await expect(page).toHaveURL(/\/search\?q=configure$/)
    await expect(page.getByRole("link", { name: /Setup › Configure/ })).toBeVisible()
    await expect(page.locator('link[rel="search"]')).toHaveAttribute("href", "/opensearch.xml")
  })

  test("a refused query says why", async ({ page }) => {
    await page.goto("/search?q=in%3Arobots+x")
    await expect(page.getByRole("main").getByRole("alert")).toContainText("not a kind")
  })
})
