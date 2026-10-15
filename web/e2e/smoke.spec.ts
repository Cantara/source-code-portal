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
  ["/repos/acme/whydah-core/docs/docs/setup", "Setup"],
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
      await page.waitForLoadState("networkidle")
      await axe(page)
    }
  })
}

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

test("the commit log loads more", async ({ page }) => {
  await page.goto("/commits")
  await expect(page.getByRole("listitem")).toHaveCount(50)
  await page.getByRole("button", { name: "Load more" }).click()
  await expect(page.getByRole("listitem")).toHaveCount(90)
  await expect(page.getByRole("button", { name: "Load more" })).toHaveCount(0)
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

test("the theme menu switches light and dark, and system follows the system preference as it changes", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" })
  await page.goto("/")
  const html = page.locator("html")
  const theme = page.getByRole("combobox", { name: "Theme" })
  await expect(theme).toHaveValue("system")
  await expect(html).not.toHaveClass(/dark/)
  await page.emulateMedia({ colorScheme: "dark" })
  await expect(html).toHaveClass(/dark/)
  await theme.selectOption("light")
  await expect(html).not.toHaveClass(/dark/)
  await expect(html).toHaveCSS("color-scheme", "light")
  await theme.selectOption("dark")
  await page.emulateMedia({ colorScheme: "light" })
  await expect(html).toHaveClass(/dark/)
  await theme.selectOption("system")
  await expect(html).not.toHaveClass(/dark/)
  await page.reload()
  await expect(theme).toHaveValue("system")
})

for (const path of ["/repos/acme/whydah-core", "/repos/acme/whydah-core/commits"]) {
  test(`${path} draws its commits as a git graph, the merge in a second lane`, async ({ page }) => {
    await page.goto(path)
    const merge = page.locator("main li", { hasText: "Merge pull request #7" }).first()
    const graph = merge.locator("svg")
    await expect(graph).toHaveAttribute("viewBox", "0 0 32 52") // two lanes
    await expect(graph.locator("circle")).toHaveAttribute("fill", "none") // a ring, not a dot
    await expect(graph.locator("path")).toHaveCount(3) // in from above, out to both parents
    const branch = page.locator("main li", { hasText: "Change 26 of repository 0" }).first() // on the feature branch
    await expect(branch.locator("svg circle")).toHaveAttribute("cx", "24")
  })
}

test("a product shows its weighed health and what costs it points", async ({ page }) => {
  await page.goto("/")
  await expect(page.getByRole("region", { name: "Telemetry" }).getByText("Health", { exact: true })).toBeVisible()
  await page.goto("/groups/whydah")
  const health = page.getByRole("region", { name: "Health" })
  await expect(health.getByText(/repositories weighed/)).toBeVisible()
  await expect(health.getByRole("listitem").filter({ hasText: "No licence detected" })).toBeVisible()
  await expect(health.getByRole("link", { name: "whydah-sso" })).toHaveAttribute("href", "/repos/acme/whydah-sso")
})

test("the header shows the portal's release", async ({ page }) => {
  await page.goto("/")
  await expect(page.getByRole("banner").getByTitle("Release")).toHaveText(/^(v\d+\.\d+\.\d+.*|dev)$/)
})

test("a page from the running bundle sees no new version", async ({ page }) => {
  const api = page.waitForResponse((r) => r.url().includes("/api/v1/"))
  await page.goto("/")
  expect((await api).headers()["x-scp-build"]).toBeTruthy()
  await page.waitForLoadState("networkidle")
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

// Badges are a hard requirement: every repository on the dashboard and its group shows its health badges, and the repository page
// shows its SVG badges, every one loaded.
for (const path of ["/", "/groups/whydah"]) {
  test(`${path} shows health badges for every repository`, async ({ page }) => {
    await page.goto(path)
    const repos = page.locator('a[href^="/repos/"]:not([href*="/commits"]):not([href*="/docs/"])')
    await expect.poll(() => repos.count()).toBeGreaterThanOrEqual(3)
    for (const row of await page.locator("li:has(> div > a[href^='/repos/']), tr:has(a[href^='/repos/'])").all()) {
      await expect(row.getByText(/issues$/)).toBeVisible()
      await expect(row.getByText(/^(no licence|Apache-2\.0|MIT|[A-Z][\w.-]+)$/).first()).toBeVisible()
    }
  })
}

test("a repository page shows its SVG badges", async ({ page }) => {
  await page.goto("/repos/acme/whydah-core")
  const badges = page.locator('img[src^="/badges/"]')
  await expect(badges).toHaveCount(4)
  for (const img of await badges.all()) {
    await expect.poll(() => img.evaluate((i: HTMLImageElement) => i.complete && i.naturalWidth > 0)).toBe(true)
  }
})
