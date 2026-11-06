import AxeBuilder from "@axe-core/playwright"
import type { Page } from "@playwright/test"
import { expect, test } from "./fixtures"

// Every page of the UI with its h1; each answers 200.
const pages = [
  ["/", "Dashboard"],
  ["/commits", "Commits"],
  ["/groups/whydah", "Whydah"],
  ["/groups/whydah/commits", "Commits in Whydah"],
  ["/repos/acme/whydah-core", "Whydah Core"],
  ["/repos/acme/whydah-core/commits", "Commits in acme/Whydah-Core"],
  ["/repos/acme/whydah-core/commits?sha=0000000000000000000000000000000000000029", "Commits in acme/Whydah-Core"],
  ["/commits?group=whydah", "Commits"],
  ["/repos/acme/whydah-core/docs/docs/setup", "Setup"],
  ["/signin", "Sign in"],
  ["/settings", "Settings"],
  ["/guide", "Read the portal in five minutes, run your own in thirty"],
  ["/admin", "Admin"],
  ["/search", "Search"],
  ["/search?q=whydah", "Search"],
] as const

async function axe(page: Page) {
  const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze()
  expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
}

for (const [path, h1] of pages) {
  test(`${path} renders "${h1}" and passes axe in both themes`, async ({ page }) => {
    for (const scheme of ["light", "dark"] as const) {
      await page.emulateMedia({ colorScheme: scheme })
      const res = await page.goto(path)
      expect(res?.status()).toBe(200)
      await expect(page.getByRole("heading", { level: 1, name: h1, exact: true })).toBeVisible()
      await expect(page.locator("html")).toHaveClass(scheme === "dark" ? /dark/ : /^(?!.*dark)/)
      await axe(page)
    }
  })
}

test("the guide's pictures load in the theme shown, and every section is in its contents", async ({ page }) => {
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.goto("/guide")
    const shown = page.locator("main img:visible")
    await shown.last().scrollIntoViewIfNeeded()
    for (const img of await shown.all()) {
      await img.scrollIntoViewIfNeeded()
      // Another program's window has one picture, its own theme (the WebMCP inspector); the portal's in the theme shown.
      await expect(img).toHaveAttribute("src", new RegExp(`(-${scheme}|^/guide/webmcp)\\.webp$`))
      // A lazy picture loads once scrolled into view: wait for it rather than read it at once.
      await expect.poll(() => img.evaluate((i: HTMLImageElement) => i.complete && i.naturalWidth > 0)).toBe(true)
    }
  }
  const contents = page.getByRole("navigation", { name: "Guide" }).getByRole("link")
  for (const link of await contents.all()) {
    const id = ((await link.getAttribute("href")) ?? "").slice(1)
    await expect(page.locator(`section#${id}`)).toHaveCount(1)
  }
})

test("an unknown address answers 404 with the not-found page", async ({ page }) => {
  const res = await page.goto("/no/such/page")
  expect(res?.status()).toBe(404)
  await expect(page.getByRole("heading", { level: 1, name: "Page not found" })).toBeVisible()
})

test("an unknown repository answers 404 with the not-found page", async ({ page }) => {
  const res = await page.goto("/repos/acme/nope")
  expect(res?.status()).toBe(404)
  await expect(page.getByRole("heading", { level: 1, name: "Page not found" })).toBeVisible()
})

test("navigation moves focus to the new page's h1", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("link", { name: "Whydah", exact: true }).click()
  await expect(page.getByRole("heading", { level: 1, name: "Whydah" })).toBeFocused()
  await expect(page).toHaveTitle("Whydah · Source Code Portal")
})

test("the commit log pages by number and remembers the page size", async ({ page }) => {
  await page.goto("/commits")
  const commits = page.getByRole("region", { name: "Commit log" }).locator("ol > li")
  const pager = page.getByRole("navigation", { name: "Pages", exact: true })
  await expect(commits).toHaveCount(50)
  await expect(pager.locator("[aria-current=page]")).toHaveText("1")
  await pager.getByRole("link", { name: "Page 2", exact: true }).click()
  await expect(page).toHaveURL("/commits?page=2")
  await expect(pager.locator("[aria-current=page]")).toHaveText("2")
  await expect(pager.getByText(/^51–100 of /)).toBeVisible()
  await page.getByRole("navigation", { name: "Pages, at the end" }).getByRole("link", { name: "Previous" }).click()
  await expect(page).toHaveURL("/commits")
  await page.getByLabel("Per page").selectOption("100")
  await expect(commits).toHaveCount(100)
  await page.reload()
  await expect(commits).toHaveCount(100) // the choice is remembered
  await expect(page.getByLabel("Per page")).toHaveValue("100")
})

test("the README renders with highlighting and no script", async ({ page }) => {
  await page.goto("/repos/acme/whydah-core")
  await expect(page.locator(".doc .chroma .kd").first()).toHaveText("func")
  await expect(page.locator(".doc .md-alert-note")).toBeVisible()
  await page
    .getByRole("navigation", { name: "Documents" })
    .or(page.getByRole("region", { name: "Documentation" }))
    .getByRole("link", { name: "Setup" })
    .click()
  await expect(page.getByRole("link", { name: "Install" })).toBeVisible()
  await expect(page.locator(".doc script")).toHaveCount(0)
})

test("a repository shows its technology and the repositories that use it", async ({ page }) => {
  await page.goto("/repos/acme/whydah-core")
  const tech = page.getByRole("region", { name: "Technology" })
  await expect(tech.getByText("java 21")).toBeVisible()
  await tech.getByRole("link", { name: "acme/whydah-sso" }).click()
  await expect(page.getByRole("heading", { level: 1, name: "whydah-sso" })).toBeVisible()
  await expect(
    page.getByRole("region", { name: "Dependencies" }).getByRole("link", { name: "no.acme:whydah-core" }),
  ).toBeVisible()
})

test("a stored dark theme paints dark from the first frame", async ({ page }) => {
  await page.addInitScript(() => window.localStorage.setItem("scp-theme", "dark"))
  await page.goto("/", { waitUntil: "domcontentloaded" })
  expect(await page.evaluate(() => [document.documentElement.className, document.documentElement.style.colorScheme])).toEqual([
    "dark",
    "dark",
  ])
})

test("a fleet tile and a legend level name their signals at once, on hover or a tap", async ({ page, isMobile }) => {
  await page.goto("/")
  const fleet = page.locator("section[aria-labelledby=fleet]")
  const tile = fleet.locator("a.cell").first()
  const tip = page.getByRole("tooltip")
  if (isMobile) {
    await tile.tap()
    await expect(page).toHaveURL("/") // the first tap explains, it does not navigate
  } else {
    await tile.hover()
  }
  await expect(tip).toBeVisible()
  await expect(tip).toContainText(/ ago|none read/)
  await page.keyboard.press("Escape")
  await expect(tip).toHaveCount(0)
  await fleet.getByRole("list", { name: "Activity of every repository" }).getByRole("listitem").first().focus()
  await expect(tip).toContainText(/^Active.*Changed in the last 30 days$/)
})

test("the side panel holds the latest commits, the pull requests and the issues in thirds, hides each by its button, folds when all are hidden, and the page remembers it", async ({
  page,
  isMobile,
}) => {
  await page.goto("/")
  const panel = page.getByRole("complementary", { name: "Side panel" })
  const titles = ["Latest commits", "Pull requests", "Issues"]
  // In this order, each with its hide button.
  await expect(panel.getByRole("heading", { level: 2 })).toHaveText(titles.map((t) => new RegExp(`^${t}`)))
  for (const t of titles) await expect(panel.getByRole("button", { name: `Hide ${t.toLowerCase()}` })).toBeVisible()
  const cards = titles.map((t) =>
    panel.locator("section").filter({ has: page.getByRole("heading", { name: new RegExp(`^${t}`) }) }),
  )
  const heights = () => Promise.all(cards.map(async (c) => Math.round((await c.boundingBox())?.height ?? 0)))
  const near = (a: number, b: number) => Math.abs(a - b) <= 2
  if (!isMobile) {
    const products = await page.getByRole("heading", { name: "Products" }).boundingBox()
    const side = await panel.boundingBox()
    expect(side?.x ?? 0).toBeGreaterThan((products?.x ?? 0) + 200) // to the right, not below
    // Viewport-high, the three open sections in thirds.
    const window = page.viewportSize()?.height ?? 0
    expect(near(side?.height ?? 0, window - 96)).toBe(true)
    const [a = 0, b = 0, c = 0] = await heights()
    expect(near(a, b) && near(b, c)).toBe(true)
    const edge = page.getByRole("separator", { name: "Resize the side panel" })
    await edge.focus()
    await page.keyboard.press("ArrowLeft")
    await expect(edge).toHaveAttribute("aria-valuenow", "27")
  }
  // A section hides to its header; the open ones share its space, and it is kept.
  await panel.getByRole("button", { name: "Hide pull requests" }).click()
  const show = panel.getByRole("button", { name: "Show pull requests" })
  await expect(show).toHaveAttribute("aria-expanded", "false")
  await expect(panel.locator("#pulls-body")).toHaveCount(0)
  if (!isMobile) {
    const [a = 0, b = 0, c = 0] = await heights()
    expect(near(a, c) && b < a / 3).toBe(true)
  }
  await page.reload()
  await expect(show).toHaveAttribute("aria-expanded", "false")
  // Hiding the other two folds the whole panel to its rail, which brings every section back.
  await panel.getByRole("button", { name: "Hide latest commits" }).click()
  await panel.getByRole("button", { name: "Hide issues" }).click()
  await expect(panel.getByRole("heading")).toHaveCount(0)
  await page.reload()
  await panel.getByRole("button", { name: "Show the side panel" }).click()
  for (const t of titles) await expect(panel.getByRole("button", { name: `Hide ${t.toLowerCase()}` })).toBeVisible()
  if (!isMobile)
    await expect(page.getByRole("separator", { name: "Resize the side panel" })).toHaveAttribute("aria-valuenow", "27")
})

test("the theme menu switches light and dark, and system follows the system preference as it changes", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" })
  await page.goto("/")
  const html = page.locator("html")
  const portal = page.getByRole("banner").getByRole("navigation", { name: "Portal" })
  await expect(portal.locator("a, button")).toHaveText(["Guide", "Present", "", "Settings"]) // Guide, Present, the theme, Settings
  await expect(portal.getByRole("link", { name: "Present" })).toHaveAttribute("href", "/present")
  const button = portal.getByRole("button", { name: /^Theme/ })
  const choose = async (name: string) => {
    await button.click()
    await page.getByRole("menu", { name: "Theme" }).getByRole("menuitemradio", { name }).click()
    await expect(page.getByRole("menu", { name: "Theme" })).toBeHidden()
  }
  await expect(button).toHaveAccessibleName("Theme: System")
  await expect(html).not.toHaveClass(/dark/)
  await page.emulateMedia({ colorScheme: "dark" })
  await expect(html).toHaveClass(/dark/)
  await choose("Light")
  await expect(html).not.toHaveClass(/dark/)
  await expect(html).toHaveCSS("color-scheme", "light")
  await expect(button).toHaveAccessibleName("Theme: Light")
  await choose("Dark")
  await page.emulateMedia({ colorScheme: "light" })
  await expect(html).toHaveClass(/dark/)
  await button.click()
  await expect(page.getByRole("menuitemradio", { name: "Dark" })).toHaveAttribute("aria-checked", "true")
  await expect(page.getByRole("menuitemradio", { name: "Dark" })).toBeFocused()
  await page.keyboard.press("ArrowUp")
  await page.keyboard.press("Enter") // System
  await expect(html).not.toHaveClass(/dark/)
  await expect(button).toBeFocused()
  await page.reload()
  await expect(button).toHaveAccessibleName("Theme: System")
})

for (const path of ["/repos/acme/whydah-core"]) {
  test(`${path} draws its commits as a git graph, the merge in a second lane`, async ({ page }) => {
    await page.goto(path)
    const merge = page.locator("main li", { hasText: "Merge pull request #7" }).first()
    const graph = merge.locator("svg")
    await expect(graph).toHaveAttribute("viewBox", "0 0 48 52") // three lanes: main, the merged branch and feature/sso
    await expect(graph.locator("circle")).toHaveAttribute("fill", "none") // a ring, not a dot
    await expect(graph.locator("path")).toHaveCount(3) // in from above, out to both parents
    const branch = page.locator("main li", { hasText: "Change 26 of repository 0" }).first() // on the feature branch
    await expect(branch.locator("svg circle")).toHaveAttribute("cx", "24")
  })
}

test("a product shows its weighed health and what costs it points", async ({ page }) => {
  await page.goto("/")
  await expect(page.getByRole("region", { name: "Telemetry" }).getByText("Findings", { exact: true })).toBeVisible()
  await expect(page.getByRole("region", { name: "Fleet" })).toBeVisible()
  await page.getByRole("button", { name: "table" }).click()
  await expect(page.getByRole("table", { name: /Products/ }).getByRole("rowheader", { name: "Whydah" })).toBeVisible()
  await page.reload()
  await expect(page.getByRole("button", { name: "table" })).toHaveAttribute("aria-pressed", "true") // remembered, drawn by the server
  await page.goto("/groups/whydah")
  const health = page.getByRole("region", { name: "Health" })
  await expect(health.getByText(/repositories weighed/)).toBeVisible()
  await expect(health.getByRole("listitem").filter({ hasText: "No licence detected" })).toBeVisible()
  // The repository and the finding's mark open the finding on the repository's page, where its next step is.
  await expect(health.getByRole("link", { name: "whydah-sso: No licence detected" })).toHaveAttribute(
    "href",
    "/repos/acme/whydah-sso#finding-no-licence",
  )
  await expect(health.getByRole("link", { name: /^No licence in whydah-sso/ })).toHaveAttribute(
    "href",
    "/repos/acme/whydah-sso#finding-no-licence",
  )
})

test("the header shows the portal's release", async ({ page }) => {
  await page.goto("/")
  await expect(page.getByRole("banner").getByTitle("Release")).toHaveText(/^(v\d+\.\d+\.\d+.*|dev)$/)
})

test("a page from the running bundle sees no new version", async ({ page }) => {
  const api = page.waitForResponse((r) => r.url().includes("/api/v1/"))
  await page.goto("/")
  expect((await api).headers()["x-scp-build"]).toBeTruthy()
  await expect(page.getByText("A new version is available.")).toHaveCount(0)
})

test("static files are immutable and compressed; pages are not stored", async ({ page, request }) => {
  const res = await page.goto("/")
  expect(res?.headers()["cache-control"]).toContain("no-store")
  const script = await page.locator("script[src^='/_next/static/']").first().getAttribute("src")
  expect(script).toMatch(/^\/_next\/static\/chunks\/[\w-]+\.js/)
  const asset = await request.get(script ?? "", { headers: { "Accept-Encoding": "gzip" } })
  expect(asset.headers()["cache-control"]).toBe("public, max-age=31536000, immutable")
  expect(asset.headers()["content-encoding"]).toBe("gzip")
})

// Badges are a hard requirement: every repository of a group shows its health badges on the group's page (the dashboard shows
// products, not their members: P-36), and the repository page shows its SVG badges, every one loaded.
for (const path of ["/groups/whydah"]) {
  test(`${path} shows health badges for every repository`, async ({ page }) => {
    await page.goto(path)
    const repos = page.locator('a[href^="/repos/"]:not([href*="/commits"]):not([href*="/docs/"])')
    await expect.poll(() => repos.count()).toBeGreaterThanOrEqual(3)
    for (const row of await page.locator("tbody tr:has(a[href^='/repos/'])").all()) {
      await expect(row.getByRole("link", { name: /open issues$/ })).toBeVisible()
      await expect(row.getByText(/^(no licence|Apache-2\.0|APL2|MIT|[A-Z][\w.-]+)$/).first()).toBeVisible()
      await expect(row.getByRole("rowheader").locator(".led")).toBeVisible()
    }
  })
}

test("a published package shows where there is no GitHub release, and on the repository page", async ({ page }) => {
  await page.goto("/groups/whydah")
  const row = page.getByRole("row").filter({ has: page.getByRole("link", { name: "whydah-sso", exact: true }) })
  await expect(row.getByRole("link", { name: "1.4.0, published package no.acme:whydah-sso on Maven" })).toBeVisible()
  await expect(row.getByRole("cell", { name: "Maven", exact: true })).toBeVisible()
  await page.goto("/repos/acme/whydah-core")
  const card = page.getByRole("region", { name: "Packages" }).or(page.locator("[aria-labelledby=packages]"))
  await expect(card).toContainText("no.acme:whydah-core")
  await expect(card.getByRole("link", { name: "1.4.0" })).toHaveAttribute(
    "href",
    "https://central.sonatype.com/artifact/no.acme/whydah-core/1.4.0",
  )
})

test("a repository page shows its SVG badges", async ({ page }) => {
  await page.goto("/repos/acme/whydah-core")
  const badges = page.locator('img[src^="/badges/"]')
  await expect(badges).toHaveCount(4)
  for (const img of await badges.all()) {
    await expect.poll(() => img.evaluate((i: HTMLImageElement) => i.complete && i.naturalWidth > 0)).toBe(true)
  }
})

// Diff stats (design/diffstat.md): the fake's commits carry them, so every commit list says what each changed.
test("the commit lists show what each commit changed", async ({ page }) => {
  await page.goto("/")
  const latest = page.getByRole("region", { name: "Latest commits" }).or(page.locator("[aria-labelledby=latest]")).first()
  await expect(latest.getByText(/^\d+ additions? and \d+ deletions? in \d+ files?$/).first()).toBeAttached()
  await expect(latest.getByText(/^\+\d+$/).first()).toBeVisible()
  await page.goto("/commits")
  await expect(page.getByText(/^\+\d+$/).first()).toBeVisible()
})
