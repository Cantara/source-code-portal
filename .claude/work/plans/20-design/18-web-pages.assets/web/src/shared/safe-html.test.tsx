// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest"
import { act, createElement } from "react"
import { createRoot, type Root } from "react-dom/client"

// Trusted Types is not implemented by jsdom: a fake window.trustedTypes reproduces the property that matters (V-02): createHTML returns a NEW object on every call.
class FakeTrustedHTML { readonly s: string; constructor(s: string) { this.s = s } toString() { return this.s } }
let creates = 0
vi.stubGlobal("trustedTypes", { createPolicy: () => ({ createHTML: (s: string) => { creates++; return new FakeTrustedHTML(s) }, createScriptURL: (s: string) => s }) })
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

const { SafeHtml } = await import("./safe-html")

let root: Root | null = null
afterEach(() => { act(() => root?.unmount()); root = null; document.body.innerHTML = "" })

describe("SafeHtml (V-02)", () => {
  it("does not rebuild the DOM when the parent re-renders with the same html (copy buttons, focus and scroll survive)", async () => {
    const host = document.createElement("div"); document.body.append(host); root = createRoot(host)
    const html = "<pre><code>x</code></pre>"
    await act(async () => root!.render(createElement(SafeHtml, { html, className: "a" })))
    const pre = host.querySelector("pre")!
    const button = document.createElement("button"); pre.append(button) // what enhanceCodeBlocks adds
    await act(async () => root!.render(createElement(SafeHtml, { html, className: "b" }))) // unrelated prop change
    expect(host.querySelector("pre")).toBe(pre)                 // same node: innerHTML was not re-assigned
    expect(host.querySelector("button")).toBe(button)
    expect(creates).toBe(1)                                       // the policy was asked once per distinct html string
    await act(async () => root!.render(createElement(SafeHtml, { html: "<p>new</p>", className: "b" })))
    expect(host.querySelector("pre")).toBeNull(); expect(host.querySelector("p")?.textContent).toBe("new"); expect(creates).toBe(2)
  })
})
