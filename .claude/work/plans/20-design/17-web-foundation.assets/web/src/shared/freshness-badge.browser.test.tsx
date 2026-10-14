import { render } from "vitest-browser-react"
import { expect, it } from "vitest"
import { FreshnessBadge } from "./freshness-badge"

// Runs in real Chromium: layout and real CSS apply, which jsdom cannot check.
it("the stale badge is visible, has text and a non-zero box", async () => {
  const screen = await render(
    <FreshnessBadge
      freshness={{ status: "stale", kind: "k", asOf: null, fetchedAt: null, checkedAt: null, staleSince: null, lastErrorCode: null }}
    />,
  )
  const el = screen.getByText("Delayed")
  await expect.element(el).toBeVisible()
  expect(el.element().getBoundingClientRect().width).toBeGreaterThan(0)
})
