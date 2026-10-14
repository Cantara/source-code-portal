import { describe, expect, it } from "vitest"
import { displayText, firstLine, truncateGraphemes } from "./text"

describe("display text hygiene", () => {
  it("keeps the first line only", () => {
    expect(firstLine("Fix VAT\n\nLong body")).toBe("Fix VAT")
    expect(firstLine("a\r\nb")).toBe("a")
    expect(firstLine("  padded  ")).toBe("padded")
  })
  it("removes bidi overrides and controls", () => {
    expect(displayText("admin\u202Egpj.exe")).toBe("admingpj.exe")
    expect(displayText("a\u0007b\u0000c\u009Fd")).toBe("abcd")
    expect(displayText("tab\there")).toBe("tab\there")
  })
  it("truncates by grapheme and never splits a cluster", () => {
    const s = "\u{1F468}\u200D\u{1F469}\u200D\u{1F467}".repeat(5) // five family emoji, 8 UTF-16 units each
    expect(truncateGraphemes(s, 3)).toBe("\u{1F468}\u200D\u{1F469}\u200D\u{1F467}".repeat(3) + "\u2026")
    expect(truncateGraphemes("short", 10)).toBe("short")
    expect(displayText("x".repeat(300), 200)).toHaveLength(201)
  })
  it("null and undefined become the empty string", () => {
    expect(displayText(null)).toBe("")
    expect(displayText(undefined)).toBe("")
  })
})
