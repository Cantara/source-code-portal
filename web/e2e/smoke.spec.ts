import AxeBuilder from "@axe-core/playwright"
import type { Page } from "@playwright/test"
import { expect, test } from "./fixtures"

// Every page of the UI with its h1; each answers 200.
const pages = [
  ["/", "Dashboard"],
  ["/commits", "Commits"],
  ["/groups/whydah", "Whydah"],
  ["/groups/whydah/commits", "Commits in whydah"],
  ["/repos/acme/whydah-core", "Whydah Core"],
  ["/repos/acme/whydah-core/commits", "Commits in acme/whydah-core"],
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

test("static files are immutable and compressed; pages are not stored", async ({ page, request }) => {
  const res = await page.goto("/")
  expect(res?.headers()["cache-control"]).toContain("no-store")
  const script = await page.locator("script[src^='/_next/static/']").first().getAttribute("src")
  expect(script).toMatch(/^\/_next\/static\/chunks\/[\w-]+\.js/)
  const asset = await request.get(script ?? "", { headers: { "Accept-Encoding": "gzip" } })
  expect(asset.headers()["cache-control"]).toBe("public, max-age=31536000, immutable")
  expect(asset.headers()["content-encoding"]).toBe("gzip")
})
