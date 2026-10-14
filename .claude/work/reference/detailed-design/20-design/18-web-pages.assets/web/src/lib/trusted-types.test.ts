import { describe, expect, it } from "vitest"
import { toTrustedHtml, toTrustedScriptUrl } from "./trusted-types"

// Node has no window.trustedTypes: the module must fall back to plain strings (the browser path is covered by csp-probe/run.mjs, 12.5).
describe("trusted types fallback", () => {
  it("returns the plain string when Trusted Types is unavailable", () => {
    expect(toTrustedHtml("<p>x</p>")).toBe("<p>x</p>")
    expect(toTrustedScriptUrl("/assets/layout.worker-abc.js")).toBe("/assets/layout.worker-abc.js")
  })
})
