import { describe, expect, it } from "vitest"
import { render } from "vitest-browser-react"
import { SafeHtml } from "./safe-html"

// Real Chromium has window.trustedTypes, so `policy.createHTML` returns a new TrustedHTML object on every call. A parent that
// re-renders (refetch, tick) must not make React reassign innerHTML: that would reset text selection, <details> state and scroll.
describe("SafeHtml in a real browser", () => {
  it("keeps the DOM it rendered when the parent re-renders with the same html", async () => {
    const html = "<details><summary>More</summary><p>text</p></details>"
    const screen = await render(<SafeHtml html={html} />)
    const details = screen.container.querySelector("details")!
    details.open = true
    await screen.rerender(<SafeHtml html={html} />)
    expect(screen.container.querySelector("details")).toBe(details)
    expect(details.open).toBe(true)
  })
})
