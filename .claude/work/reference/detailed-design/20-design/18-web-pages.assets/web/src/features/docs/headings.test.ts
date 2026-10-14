// @vitest-environment jsdom
import { describe, expect, it } from "vitest"
import { normaliseDocH1 } from "./headings"

const mount = (html: string): HTMLElement => { const d = document.createElement("div"); d.innerHTML = html; return d }

describe("normaliseDocH1 (V-27)", () => {
  it("hides a leading h1 that repeats the page title", () => {
    const r = mount('<h1 id="doc-install">Installation</h1><p>x</p><h2>Requirements</h2>')
    normaliseDocH1(r, "Installation")
    expect(r.querySelector("h1")?.hidden).toBe(true)
  })
  it("keeps a different leading h1 visible as a level-2 heading, and demotes every later h1", () => {
    const r = mount("<h1>Other</h1><h1>Later</h1>")
    normaliseDocH1(r, "Installation")
    const hs = [...r.querySelectorAll("h1")]
    expect(hs.map((h) => [h.hidden, h.getAttribute("role"), h.getAttribute("aria-level")])).toEqual([[false, "heading", "2"], [false, "heading", "2"]])
  })
  it("a title match that is not leading is demoted, not hidden; idempotent; no h1 is a no-op", () => {
    const r = mount("<p>intro</p><h1>Installation</h1>")
    normaliseDocH1(r, "Installation"); normaliseDocH1(r, "Installation")
    expect(r.querySelector("h1")?.hidden).toBe(false); expect(r.querySelector("h1")?.getAttribute("aria-level")).toBe("2")
    expect(() => normaliseDocH1(mount("<p>x</p>"), "t")).not.toThrow()
  })
})
