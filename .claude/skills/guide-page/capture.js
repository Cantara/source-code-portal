// The guide page's screenshots, for the Playwright MCP tool browser_run_code_unsafe (filename: this file). It reads the demo
// catalogue of TestServeForGuide at BASE, shoots every picture in both themes at twice the pixels into OUT (PNG; the skill turns
// them into WebP) and returns where the product card's rows are, as percentages of the card, for the callouts (web/src/features/guide/shots.json).
// Change BASE and OUT here before a run if they differ.
async (page) => {
  const BASE = "http://127.0.0.1:9290"
  const OUT = "/tmp/scp-guide"
  const browser = page.context().browser()
  const ctx = browser ? await browser.newContext({ viewport: { width: 1280, height: 900 }, deviceScaleFactor: 2 }) : page.context()
  const p = browser ? await ctx.newPage() : page
  const card = (name) => p.getByRole("link", { name, exact: true }).locator("xpath=ancestor::li[1]")
  const rows = {}
  for (const theme of ["light", "dark"]) {
    await ctx.clearCookies()
    await p.goto(BASE + "/")
    await p.evaluate((t) => localStorage.setItem("scp-theme", t), theme)
    await p.reload()
    await p.getByRole("link", { name: "Payments", exact: true }).waitFor()
    await p.waitForTimeout(500)
    const shot = (locator, name) => locator.screenshot({ path: `${OUT}/${name}-${theme}.png`, animations: "disabled" })
    await shot(p.getByRole("region", { name: "Telemetry" }), "telemetry")
    await shot(p.getByRole("region", { name: "Fleet" }), "fleet")
    await shot(card("Payments"), "card")
    await shot(card("Developer tools"), "card-watch")
    // The latest commits, cut after the fifth.
    const latest = p.locator('[aria-labelledby="latest"]')
    await latest.scrollIntoViewIfNeeded()
    const top = await latest.boundingBox()
    const fifth = await latest.locator("li").nth(4).boundingBox()
    await p.screenshot({
      path: `${OUT}/latest-${theme}.png`,
      animations: "disabled",
      clip: { x: top.x, y: top.y, width: top.width, height: fifth.y + fifth.height - top.y + 12 },
    })
    if (theme === "light") {
      // Where the rows of the Payments card are, from its top left, in percent of its size.
      const li = card("Payments")
      const box = await li.boundingBox()
      const at = async (locator) => {
        const b = await locator.first().boundingBox()
        return { x: +(((b.x - box.x) / box.width) * 100).toFixed(1), y: +(((b.y - box.y + b.height / 2) / box.height) * 100).toFixed(1) }
      }
      rows.name = await at(li.locator("h3"))
      rows.score = await at(li.locator("h3").locator("xpath=following-sibling::*[1]"))
      rows.work = await at(li.locator("p").nth(0))
      rows.trend = await at(li.locator("svg[role=img]"))
      rows.people = await at(li.locator("p").nth(1))
      rows.recent = await at(li.locator("ul").first())
      rows.findings = await at(li.locator(".border-t"))
      rows.shared = await at(li.locator(".border-t p").last())
    }
    // The table at its full width: the latest commits' side panel folded away first.
    await p.getByRole("button", { name: "Hide latest commits" }).click()
    await p.getByRole("button", { name: "table", exact: true }).click()
    await shot(p.getByRole("table"), "table")
    await p.goto(BASE + "/settings")
    await shot(p.locator("[aria-labelledby]").filter({ has: p.getByText("Work that counts", { exact: true }) }), "settings")
    // The presentation page as an iPad in landscape shows it, the whole screen.
    await p.setViewportSize({ width: 1180, height: 820 })
    await p.goto(BASE + "/present")
    await p.getByRole("region", { name: "Latest commits" }).getByRole("listitem").first().waitFor()
    await p.waitForTimeout(500)
    await p.screenshot({ path: `${OUT}/present-${theme}.png`, animations: "disabled" })
    await p.setViewportSize({ width: 1280, height: 900 })
  }
  await ctx.clearCookies()
  if (browser) await ctx.close()
  return rows
}
