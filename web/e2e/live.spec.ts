import { expect, test } from "./fixtures"

// The control server of cmd/scp/e2e_test.go: POST /push makes a commit on the fake GitHub and pushes it to the engine.
const control = "http://127.0.0.1:9191"

test("README images load from the portal", async ({ page }) => {
  await page.goto("/repos/acme/whydah-core")
  const logo = page.getByRole("img", { name: "Whydah logo" })
  await expect(logo).toHaveAttribute("src", /^\/images\/acme\/whydah-core\/[0-9a-f]{32}$/)
  await expect.poll(() => logo.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth)).toBe(1)
})

test("a push reaches an open page without polling", async ({ page, request }, info) => {
  const snapshots: string[] = []
  page.on("request", (r) => {
    if (new URL(r.url()).pathname === "/api/v1/snapshot") snapshots.push(r.url())
  })
  const stream = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/v1/events")
  await page.goto("/repos/acme/whydah-sso/commits")
  await expect(page.getByText("Change 30 of repository 1")).toBeVisible()
  expect((await stream).headers()["content-type"]).toBe("text/event-stream")
  const before = snapshots.length
  const message = `Pushed from ${info.project.name} ${Date.now()}`
  const res = await request.post(`${control}/push?repo=whydah-sso&message=${encodeURIComponent(message)}`)
  expect(res.status()).toBe(204)
  // The snapshot is read every 30 s without the stream; the commit shows within a few seconds through it.
  await expect(page.getByText(message)).toBeVisible({ timeout: 10_000 })
  expect(snapshots.length).toBe(before)
})
