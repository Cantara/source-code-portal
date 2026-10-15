import { expect, test } from "./fixtures"

// Every page as a search engine reads it: without JavaScript, with its h1, title, description and canonical URL in the HTML.
const pages = [
  ["/", "Dashboard", "Dashboard · Source Code Portal", "/", "Whydah"],
  ["/commits", "Commits", "Commits · Source Code Portal", "/commits", "Change 30 of repository"],
  ["/groups/whydah", "Whydah", "Whydah · Source Code Portal", "/groups/whydah", "acme/whydah-sso"],
  [
    "/groups/whydah/commits",
    "Commits in Whydah",
    "Commits in Whydah · Source Code Portal",
    "/groups/whydah/commits",
    "Change 30",
  ],
  ["/repos/acme/whydah-core", "Whydah Core", "Whydah Core · Source Code Portal", "/repos/acme/Whydah-Core", "Identity. See"],
  [
    "/repos/acme/whydah-core/commits",
    "Commits in acme/Whydah-Core",
    "Commits in acme/Whydah-Core · Source Code Portal",
    "/repos/acme/Whydah-Core/commits",
    "Change 30 of repository 0",
  ],
  [
    "/repos/acme/whydah-core/docs/docs/setup",
    "Setup",
    "Setup · Source Code Portal",
    "/repos/acme/Whydah-Core/docs/docs/setup",
    "Run it.",
  ],
] as const

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false })

  for (const [path, h1, title, canonical, text] of pages) {
    test(`${path} is complete HTML`, async ({ page, baseURL }) => {
      const res = await page.goto(path)
      expect(res?.status()).toBe(200)
      await expect(page.getByRole("heading", { level: 1, name: h1, exact: true })).toBeVisible()
      await expect(page.getByText(text).first()).toBeVisible()
      await expect(page).toHaveTitle(title)
      expect((await page.locator('meta[name="description"]').getAttribute("content"))?.length).toBeGreaterThan(0)
      await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
        "href",
        `${baseURL}${canonical === "/" ? "" : canonical}`,
      )
    })
  }

  for (const path of ["/commits", "/repos/acme/tool/commits"]) {
    test(`${path} pages through numbered pages`, async ({ page }) => {
      await page.goto(path)
      await expect(page.getByText("Change 30 of repository 2").first()).toBeVisible()
      const pager = page.getByRole("navigation", { name: "Pages", exact: true })
      await pager.getByRole("link", { name: "Next" }).click()
      await expect(page).toHaveURL(/page=2/)
      await expect(pager.locator("[aria-current=page]")).toHaveText("2")
      await expect(page.getByText("Change 30 of repository 2")).toHaveCount(0)
      await expect(page.getByText(/Old change \d+ of repository 2/).first()).toBeVisible()
      await pager.getByRole("link", { name: "Page 1", exact: true }).click()
      await expect(page.getByText("Change 30 of repository 2").first()).toBeVisible()
    })
  }

  test("an unknown repository answers 404", async ({ page }) => {
    const res = await page.goto("/repos/acme/nope")
    expect(res?.status()).toBe(404)
    await expect(page.getByRole("heading", { level: 1, name: "Page not found" })).toBeVisible()
  })
})

test("the sitemap lists every page and robots.txt points to it", async ({ request, baseURL }) => {
  const sitemap = await (await request.get("/sitemap.xml")).text()
  for (const p of [
    "/",
    "/commits",
    "/groups/whydah",
    "/groups/other",
    "/repos/acme/Whydah-Core",
    "/repos/acme/whydah-sso",
    "/repos/acme/tool",
  ]) {
    expect(sitemap).toContain(`<loc>${baseURL}${p}</loc>`)
  }
  expect(await (await request.get("/robots.txt")).text()).toContain(`Sitemap: ${baseURL}/sitemap.xml`)
})
