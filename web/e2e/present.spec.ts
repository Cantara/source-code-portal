import AxeBuilder from "@axe-core/playwright"
import { expect, test } from "./fixtures"
import { screens } from "./screens"

// design/present.md: the presentation page fits the screen it is on, without the header, every product a tile or in the fold.

// The screens the page is designed on (screens.ts, shared with the owner), and a few common others.
const sized = [
  ...Object.values(screens).map((x) => ({ width: x.width, height: x.height, profile: x.profile })),
  { width: 1180, height: 820, profile: "handheld-landscape" },
  { width: 1920, height: 1080, profile: "desk" },
  { width: 820, height: 1180, profile: "handheld-portrait" },
]

for (const { width, height, profile } of sized) {
  test(`/present fills ${width}×${height} without scrolling, as ${profile}`, async ({ page }) => {
    await page.setViewportSize({ width, height })
    await page.goto("/present")
    await expect(page.locator("[data-profile]")).toHaveAttribute("data-profile", profile)
    const first = page.getByRole("region", { name: "Latest commits" }).getByRole("listitem").first()
    await expect(first).toBeVisible()
    // Its subject is the commit's link on GitHub, as the dashboard's are.
    await expect(first.getByRole("link").last()).toHaveAttribute("href", /\/commit\/[0-9a-f]+$/)
    await expect(page.getByRole("banner")).toHaveCount(0)
    const size = await page.evaluate(() => {
      const d = document.documentElement
      return { sh: d.scrollHeight, ch: d.clientHeight, sw: d.scrollWidth, cw: d.clientWidth }
    })
    expect(size.sh).toBe(size.ch)
    expect(size.sw).toBe(size.cw)
    const products = page.getByRole("region", { name: "Products" })
    const groups = (await (await page.request.get("/api/v1/dashboard")).json()) as { groups: { name: string }[] }
    const text = (await products.textContent()) ?? ""
    for (const g of groups.groups) expect(text).toContain(g.name)
    await expect(page.getByRole("status").filter({ hasText: /Live|Polling/ })).toBeVisible()
  })
}

test("/present?groups= shows only the products it names", async ({ page }) => {
  await page.goto("/present?groups=whydah")
  const products = page.getByRole("region", { name: "Products" })
  await expect(products.getByRole("link", { name: "Whydah" })).toBeVisible()
  await expect(products.getByRole("link", { name: "Other" })).toHaveCount(0)
})

test("/present follows the system on a tablet, is dark on a wall unless the reader chose, and a tap or a key shows its bar", async ({
  page,
}) => {
  await page.emulateMedia({ colorScheme: "light" })
  await page.goto("/present")
  await expect(page.locator("html")).not.toHaveClass(/dark/)
  await page.goto("/present?profile=wall")
  await expect(page.locator("html")).toHaveClass(/dark/)
  const bar = page.getByRole("navigation", { name: "Presentation" })
  await expect(bar).toHaveCSS("opacity", "0")
  await expect(bar.getByRole("button", { name: "Design this page" })).toHaveCount(0)
  await expect(async () => {
    await page.mouse.click(10, 10)
    await expect(bar).toHaveCSS("opacity", "1", { timeout: 1000 })
  }).toPass()
  await bar.getByRole("button", { name: /^Theme/ }).click()
  await expect(page.locator("html")).toHaveClass(/dark/)
  await bar.getByRole("button", { name: /^Theme/ }).click()
  await expect(page.locator("html")).not.toHaveClass(/dark/)
  await expect(bar.getByRole("link", { name: "Dashboard" })).toHaveAttribute("href", "/")
  const manifest = await (await page.request.get("/manifest.webmanifest")).json()
  expect(manifest).toMatchObject({ start_url: "/present", display: "standalone" })
  await page.evaluate(() => localStorage.removeItem("scp-theme"))
})

test("/present passes axe in both themes, the reader's choice", async ({ page }) => {
  for (const theme of ["dark", "light"] as const) {
    await page.goto("/present")
    await page.evaluate((t) => localStorage.setItem("scp-theme", t), theme)
    await page.reload()
    await expect(page.getByRole("heading", { level: 1, name: "Presentation" })).toBeAttached()
    await expect(page.locator("html")).toHaveClass(theme === "dark" ? /dark/ : /^(?!.*dark)/)
    await page.waitForLoadState("networkidle")
    const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze()
    expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
  }
  await page.evaluate(() => localStorage.removeItem("scp-theme"))
})

test("the commit column widens by its edge, is remembered, and the left keeps what matters most", async ({ page }, info) => {
  test.skip(info.project.name !== "desktop", "a landscape screen")
  await page.setViewportSize({ width: 1180, height: 820 })
  await page.goto("/present")
  const edge = page.getByRole("separator", { name: "Resize latest commits" })
  await expect(edge).toHaveAttribute("aria-valuenow", "30")
  const strip = page.getByRole("region", { name: "Telemetry" })
  await expect(strip.getByText("Commits by hand", { exact: true })).toBeVisible()
  await edge.focus()
  for (let i = 0; i < 6; i++) await page.keyboard.press("ArrowLeft")
  await expect(edge).toHaveAttribute("aria-valuenow", "60")
  await page.reload()
  await expect(edge).toHaveAttribute("aria-valuenow", "60")
  // Narrow, the strip drops the commits by hand and the products but keeps the data's age; the fleet keeps its counts, not its tiles.
  await expect(strip.getByText("Commits by hand", { exact: true })).toBeHidden()
  await expect(strip.getByText("Findings", { exact: true })).toBeVisible()
  await expect(strip.getByRole("status")).toBeVisible()
  await expect(page.getByRole("list", { name: "Health of every repository" })).toBeVisible()
  const size = await page.evaluate(() => [document.documentElement.scrollHeight, document.documentElement.clientHeight])
  expect(size[0]).toBe(size[1])
  // A tap leaves the width; a double click or a double tap brings back the usual one.
  await edge.click()
  await expect(edge).toHaveAttribute("aria-valuenow", "60")
  await page.waitForTimeout(500)
  await edge.dblclick()
  await expect(edge).toHaveAttribute("aria-valuenow", "30")
})

test("a double tap on the commit column's edge brings back its usual width", async ({ browser }) => {
  const baseURL = test.info().project.use.baseURL ?? ""
  const ctx = await browser.newContext({ baseURL, viewport: { width: 1180, height: 820 }, hasTouch: true, isMobile: true })
  await ctx.addCookies([{ name: "scp-present-column", value: "handheld-landscape:50", url: baseURL }])
  const page = await ctx.newPage()
  await page.goto("/present")
  const edge = page.getByRole("separator", { name: "Resize latest commits" })
  await expect(edge).toHaveAttribute("aria-valuenow", "50")
  const box = await edge.boundingBox()
  if (!box) throw new Error("no edge")
  await page.touchscreen.tap(box.x + box.width / 2, box.y + box.height / 2)
  await page.touchscreen.tap(box.x + box.width / 2, box.y + box.height / 2)
  await expect(edge).toHaveAttribute("aria-valuenow", "30")
  await ctx.close()
})

test("each profile keeps its own commit column: a desk's monitor starts at 60%, a tablet at 30%, a wall is told", async ({
  page,
}, info) => {
  test.skip(info.project.name !== "desktop", "a landscape screen")
  const frame = page.locator("[data-profile]")
  const edge = page.getByRole("separator", { name: "Resize latest commits" })
  // A 27-inch monitor at its usual resolution is a desk: the commit log takes most of it.
  await page.setViewportSize({ width: 2560, height: 1440 })
  await page.goto("/present")
  await expect(frame).toHaveAttribute("data-profile", "desk")
  await expect(edge).toHaveAttribute("aria-valuenow", "60")
  await edge.focus()
  await page.keyboard.press("ArrowRight")
  await expect(edge).toHaveAttribute("aria-valuenow", "55")
  // The same browser as a tablet on its side: its own width, untouched by the desk's.
  await page.setViewportSize({ width: 1180, height: 820 })
  await expect(frame).toHaveAttribute("data-profile", "handheld-landscape")
  await expect(edge).toHaveAttribute("aria-valuenow", "30")
  // Back on the desk after a reload: its width was kept, and the server rendered it at once.
  await page.setViewportSize({ width: 2560, height: 1440 })
  await page.reload()
  await expect(frame).toHaveAttribute("data-profile", "desk")
  await expect(edge).toHaveAttribute("aria-valuenow", "55")
  // A wall has a desk's pixels: the address tells the screen, which remembers it until told auto.
  await page.goto("/present?profile=wall")
  await expect(frame).toHaveAttribute("data-profile", "wall")
  await page.goto("/present")
  await expect(frame).toHaveAttribute("data-profile", "wall")
  await page.goto("/present?profile=auto")
  await expect(frame).toHaveAttribute("data-profile", "desk")
})
