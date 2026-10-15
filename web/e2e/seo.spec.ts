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
  ["/repos/acme/whydah-core", "Whydah Core", "Whydah Core · Source Code Portal", "/repos/acme/whydah-core", "Identity. See"],
  [
    "/repos/acme/whydah-core/commits",
    "Commits in acme/Whydah-Core",
    "Commits in acme/Whydah-Core · Source Code Portal",
    "/repos/acme/whydah-core/commits",
    "Change 30 of repository 0",
  ],
  [
    "/repos/acme/whydah-core/docs/docs/setup",
    "Setup",
    "Setup · Source Code Portal",
    "/repos/acme/whydah-core/docs/docs/setup",
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

test("the sitemap lists every page with its newest commit and robots.txt points to it", async ({ request, baseURL }) => {
  const res = await request.get("/sitemap.xml")
  expect(res.headers()["content-type"]).toBe("application/xml; charset=utf-8")
  expect(res.headers()["cache-control"]).toContain("s-maxage=3600")
  const sitemap = await res.text()
  for (const p of [
    "/",
    "/commits",
    "/groups/whydah",
    "/groups/other",
    "/repos/acme/whydah-core",
    "/repos/acme/whydah-core/docs/docs/setup",
    "/repos/acme/whydah-sso",
    "/repos/acme/tool",
  ]) {
    expect(sitemap).toContain(`<loc>${baseURL}${p}</loc>`)
  }
  expect(sitemap).not.toContain("vault") // private
  const robots = await (await request.get("/robots.txt")).text()
  expect(robots).toContain(`Sitemap: ${baseURL}/sitemap.xml`)
  expect(robots).toContain("Disallow: /api/")
})

type Node = Record<string, unknown> & { "@type": string }

test.describe("every page of the sitemap, as a crawler reads it", () => {
  test.use({ javaScriptEnabled: false })

  test("answers 200 with its canonical, one h1, a lastmod and a JSON-LD graph whose trail is the one shown", async ({
    page,
    request,
  }) => {
    const xml = await (await request.get("/sitemap.xml")).text()
    const urls = [...xml.matchAll(/<url><loc>([^<]+)<\/loc>(<lastmod>[^<]+<\/lastmod>)?<\/url>/g)]
    expect(urls.length).toBeGreaterThan(5)
    for (const [, loc = "", lastmod] of urls) {
      expect(lastmod, `${loc} has a lastmod`).toBeTruthy()
      const res = await page.goto(loc)
      expect(res?.status(), loc).toBe(200)
      expect((await page.locator('link[rel="canonical"]').getAttribute("href"))?.replace(/\/$/, ""), loc).toBe(
        loc.replace(/\/$/, ""),
      )
      await expect(page.locator("h1"), loc).toHaveCount(1)
      const blocks = await page.locator('script[type="application/ld+json"]').allTextContents()
      expect(blocks, loc).toHaveLength(1)
      const graph = (JSON.parse(blocks[0] ?? "{}") as { "@context": string; "@graph": Node[] })["@graph"]
      const types = graph.map((n) => n["@type"])
      expect(types, loc).toContain("WebSite")
      const self = graph.find((n) => n["@id"] === loc)
      expect(self?.url, `${loc} has its page node`).toBe(loc)
      const trail = graph.find((n) => n["@type"] === "BreadcrumbList")
      if (new URL(loc).pathname === "/") {
        expect(trail).toBeUndefined()
        continue
      }
      const items = trail?.itemListElement as { position: number; name: string; item: string }[]
      expect(items.at(-1)?.item, loc).toBe(loc)
      expect(items.map((i) => i.position)).toEqual(items.map((_, i) => i + 1))
      const shown = await page.getByRole("navigation", { name: "Breadcrumb" }).getByRole("listitem").allTextContents()
      expect(
        shown.map((t) => t.replace(/^\//, "").trim()),
        loc,
      ).toEqual(items.map((i) => i.name))
      if (loc.includes("/docs/")) expect(types, loc).toContain("TechArticle")
      else if (loc.includes("/repos/")) {
        const code = graph.find((n) => n["@type"] === "SoftwareSourceCode")
        expect(code?.codeRepository, loc).toMatch(/^https?:\/\//)
        expect(code?.dateModified, loc).toBeTruthy()
      } else if (loc.includes("/groups/")) expect(types, loc).toContain("CollectionPage")
    }
  })
})
