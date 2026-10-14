import { test, expect } from "./fixtures"

test("theme-init.js applies the stored theme before the app bundle runs", async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem("scp-theme", "dark"))
  await page.route("**/assets/index-*.js", (r) => r.abort())
  await page.goto("/dashboard")
  await expect(page.locator("html")).toHaveClass(/dark/)
  expect(await page.evaluate(() => document.documentElement.style.colorScheme)).toBe("dark")
})

test("staleness is shown as icon plus text, from the API freshness object", async ({ page }) => {
  await page.goto("/dashboard")
  await expect(page.locator("[data-freshness=stale]").first()).toContainText("Delayed")
  await expect(page.locator("[data-freshness=fresh]").first()).toContainText("Up to date")
})

test.describe("expected boundary errors", () => {
  test.use({ allowConsole: [/caught|ProblemError|Error:/] })
  test("a 503 not-ready problem renders ProblemAlert with the request id", async ({ page }) => {
    await page.route("**/api/v1/groups**", (r) =>
      r.fulfill({
        status: 503,
        contentType: "application/problem+json",
        headers: { "Retry-After": "15" },
        body: JSON.stringify({
          type: "urn:scp:problem:not-ready",
          title: "Not ready",
          status: 503,
          code: "not-ready",
          instance: "urn:uuid:c2d94e10-7b6a-4f38-9e5d-0a1b8c3f7d62",
          retryAfterSeconds: 15,
        }),
      }),
    )
    await page.goto("/dashboard")
    const alert = page.locator("[data-problem-code=not-ready]")
    await expect(alert).toContainText("still warming up", { timeout: 20_000 })
    await expect(alert).toContainText("c2d94e10-7b6a-4f38-9e5d-0a1b8c3f7d62")
  })
})

test("a different server build id shows the update banner", async ({ page }) => {
  await page.route("**/api/v1/snapshot", (r) =>
    r.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ version: 1, builtAt: "2026-10-14T09:41:07Z", readiness: "ready", buildId: "other-build" }),
    }),
  )
  await page.goto("/dashboard")
  await expect(page.getByTestId("connection-banner")).toContainText("new version")
})

test("client-side typed links navigate without a full reload", async ({ page }) => {
  await page.goto("/dashboard")
  await page.evaluate(() => ((window as unknown as { __marker: number }).__marker = 1))
  await page.getByRole("link", { name: "Platform" }).click()
  await expect(page).toHaveURL(/\/groups\/platform$/)
  expect(await page.evaluate(() => (window as unknown as { __marker?: number }).__marker)).toBe(1)
})

test.describe("trusted types", () => {
  test.use({ allowConsole: [/requires 'TrustedHTML' assignment/] }) // the deliberate refusal below logs one console error
  test("server HTML renders through the Trusted Types policy with zero CSP violations (the gate fixture fails on any)", async ({
    page,
  }) => {
    await page.goto("/pages/about")
    await expect(page.getByRole("heading", { level: 1, name: "About" })).toBeVisible()
    await expect(page.locator("#doc-purpose")).toHaveText("Purpose")
    await expect(page.getByRole("columnheader", { name: "Owner" })).toBeVisible()
    // a raw innerHTML string assignment must be refused by the browser: only the scp-html policy may create HTML
    const refused = await page.evaluate(() => {
      try {
        document.body.innerHTML = "<b>x</b>"
        return false
      } catch {
        return true
      }
    })
    expect(refused).toBe(true)
    // the refusal above is a deliberate violation: clear it so the gate fixture only judges the app
    await page.evaluate(() => ((window as unknown as { __csp: unknown[] }).__csp = []))
  })
})
