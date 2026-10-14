import { describe, expect, it } from "vitest"
import { problemTitle } from "./problem-title"

describe("problemTitle", () => {
  it.each([
    ["not-ready", "The portal is still warming up"],
    ["rate-limited", "Too many requests"],
    ["client-network", "Cannot reach the portal"],
  ])("%s", (code, want) => {
    expect(problemTitle(code)).toBe(want)
  })
  it("never echoes the server's `detail` or an unknown code; unknown codes get a generic title", () => {
    expect(problemTitle("some-future-code")).toBe("Unexpected problem")
  })
})
