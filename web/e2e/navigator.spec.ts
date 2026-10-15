import { expect, test } from "./fixtures"

// The fake GitHub of cmd/scp/e2e_test.go: whydah-core has a merged feature branch, a branch feature/sso off the main line, a renovate
// branch the portal leaves out, four tags at its head and v1.2.0 further down; tool has 600 commits, of which 500 are kept.
const merge = "0000000000000000000000000000000000000029"

test("the navigator draws the history with its labels and opens a commit's details", async ({ page }) => {
  await page.goto("/repos/acme/whydah-core/commits")
  const rows = page.getByRole("listbox", { name: "Commits" }).getByRole("option")
  const head = rows.first()
  await expect(head).toContainText("main")
  await expect(head).toContainText("+2") // main and three of the four tags, then +2
  await expect(head.getByText("default branch", { exact: false })).toHaveCount(1)
  await expect(rows.filter({ hasText: "Bridge the SSO session" })).toContainText("feature/sso")
  await expect(rows.filter({ hasText: "v1.2.0" })).toHaveCount(1)
  await expect(page.getByText("renovate/slf4j")).toHaveCount(0)

  const m = rows.filter({ hasText: "Merge pull request #7" })
  await expect(m.locator("svg circle")).toHaveAttribute("fill", "none") // a ring
  await expect(m).toContainText("Merge of 2 parents.")
  await m.click()
  await expect(m).toHaveAttribute("aria-selected", "true")
  await expect(page).toHaveURL(new RegExp(`sha=${merge}`))
  const details = page.getByRole("complementary", { name: "Commit details" })
  await expect(details.getByRole("heading", { name: "Merge pull request #7 from acme/feature" })).toBeVisible()
  await expect(details.getByText(/UTC$/)).toBeVisible()
  await details.getByRole("button", { name: "0000000" }).first().click() // its first parent, Change 28
  await expect(m).toHaveAttribute("aria-selected", "false")
  await expect(rows.filter({ hasText: "Change 28 of repository 0" })).toHaveAttribute("aria-selected", "true")

  const sso = rows.filter({ hasText: "Start the SSO bridge" })
  await sso.click()
  await expect(details).toContainText("The bridge keeps one session per user.")
  await expect(details).toContainText("Bo Example (bo)")
})

test("the navigator works by keyboard", async ({ page }) => {
  await page.goto("/repos/acme/whydah-core/commits")
  const list = page.getByRole("listbox", { name: "Commits" })
  await list.focus()
  await page.keyboard.press("j")
  await expect(list.getByRole("option").first()).toHaveAttribute("aria-selected", "true")
  await page.keyboard.press("ArrowDown")
  await page.keyboard.press("j")
  await page.keyboard.press("k")
  await expect(list.getByRole("option").nth(1)).toHaveAttribute("aria-selected", "true")
  await expect(list).toHaveAttribute("aria-activedescendant", /^commit-/)
  await page.keyboard.press("Enter")
  const details = page.getByRole("complementary", { name: "Commit details" })
  await expect(details).toBeVisible()
  await page.keyboard.press("Escape")
  await expect(details).toHaveCount(0)
  await page.keyboard.press("/")
  await expect(page.getByLabel("Search (press /)")).toBeFocused()
})

test("the navigator's filters work from the URL, and back restores them", async ({ page }) => {
  await page.goto("/repos/acme/whydah-core/commits?ref=feature%2Fsso")
  const rows = page.getByRole("listbox", { name: "Commits" }).getByRole("option")
  await expect(rows.first()).toContainText("Bridge the SSO session")
  await expect(rows.filter({ hasText: "Merge pull request #7" })).toHaveCount(0)

  await page.goto("/repos/acme/whydah-core/commits?q=merge")
  await expect(page.getByText("1 commit match, shown as a list")).toBeVisible()
  await expect(rows).toHaveCount(1)
  await expect(rows.locator("svg")).toHaveCount(0)

  await page.goto("/repos/acme/whydah-core/commits?author=bo")
  await expect(rows).toHaveCount(2)
  await page.goto("/repos/acme/whydah-core/commits?author=nobody")
  await expect(page.getByText("No commits match.")).toBeVisible()

  await page.goto("/repos/acme/whydah-core/commits")
  await page.getByLabel("Author").fill("bo")
  await page.getByRole("button", { name: "Apply" }).click()
  await expect(page).toHaveURL(/author=bo/)
  await expect(rows).toHaveCount(2)
  await page.goBack()
  await expect(page).not.toHaveURL(/author=/)
  await expect(rows.filter({ hasText: "Merge pull request #7" })).toHaveCount(1)
})

test("?sha= selects a commit beyond the first page, or says it is not kept", async ({ page }) => {
  const old = "02" + String(1000 + 600 - 450).padStart(38, "0") // "Old change 150", the 451st commit
  await page.goto(`/repos/acme/tool/commits?sha=${old}`)
  const row = page.getByRole("option", { name: /Old change 150 of repository 2/ })
  await expect(row).toHaveAttribute("aria-selected", "true")
  await expect(row).toBeInViewport()
  await expect(page.getByRole("complementary", { name: "Commit details" })).toBeVisible()

  await page.goto("/repos/acme/tool/commits?sha=deadbeef")
  await expect(page.getByText(/is not in the stored history/)).toBeVisible()
  await expect(page.getByRole("link", { name: "Open it on GitHub" })).toHaveAttribute("href", /\/commit\/deadbeef$/)
})

test("the navigator keeps few rows in the page and scrolls without long tasks", async ({ page }) => {
  await page.goto("/repos/acme/tool/commits")
  const rows = page.getByRole("listbox", { name: "Commits" }).getByRole("option")
  await expect(page.getByText("500 commits")).toBeVisible()
  await page.evaluate(() => {
    const w = window as unknown as { longTasks: number[] }
    w.longTasks = []
    new PerformanceObserver((l) => {
      for (const e of l.getEntries()) w.longTasks.push(e.duration)
    }).observe({ type: "longtask", buffered: false })
  })
  // Scroll to the end until the whole history is loaded.
  for (let i = 0; i < 40; i++) {
    await page.mouse.wheel(0, 2000)
    await page.waitForTimeout(50)
    if ((await page.getByRole("link", { name: "Older commits" }).count()) === 0) break
  }
  await expect(page.getByRole("link", { name: "Older commits" })).toHaveCount(0)
  for (let i = 0; i < 20; i++) {
    await page.mouse.wheel(0, -1500)
    await page.waitForTimeout(30)
  }
  expect(await rows.count()).toBeLessThanOrEqual(120)
  const longTasks = await page.evaluate(() => (window as unknown as { longTasks: number[] }).longTasks)
  expect(longTasks.filter((d) => d > 50)).toEqual([])
})

test("a product's timeline lists its repositories and its commits by day", async ({ page }) => {
  await page.goto("/commits?group=whydah")
  const repos = page.getByRole("region", { name: "Repositories of Whydah" })
  await expect(repos.getByRole("link", { name: /Whydah-Core/ })).toHaveAttribute("href", "/repos/acme/Whydah-Core/commits")
  await expect(page.getByRole("heading", { level: 2 }).first()).toHaveText(/^\w+day, \w+ \d+, \d{4}$/)
  await expect(page.getByText("Change 30 of repository 2")).toHaveCount(0) // tool is not in the product
  await expect(page.locator("main svg circle")).toHaveCount(0) // no graph across repositories
  await repos.getByRole("link", { name: /whydah-sso/ }).click()
  await expect(page).toHaveURL("/repos/acme/whydah-sso/commits")
})
