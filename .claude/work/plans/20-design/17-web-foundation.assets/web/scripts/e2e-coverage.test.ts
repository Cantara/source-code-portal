import { describe, expect, it } from "vitest"
import { PAGES } from "../e2e/pages.ts"
import { shapes } from "./route-shapes.ts"

// "/repos/{repo}/docs/{path...}" -> ^/repos/[^/]+/docs/.+$
function toRegex(pattern: string): RegExp {
  const src = pattern
    .split("/")
    .map((seg) => (seg.endsWith("...}") ? ".+" : seg.startsWith("{") ? "[^/]+" : seg.replace(/[.*+?^${}()|[\\]\\]/g, "\\$&")))
    .join("/")
  return new RegExp(`^${src}$`)
}

describe("e2e page coverage", () => {
  const paths = PAGES.map((p) => p.split("?")[0] as string)
  it.each(shapes().filter((s) => s.listeners.includes("public")))("shape $pattern has a sample URL in e2e/pages.ts", ({ pattern }) => {
    expect(paths.some((p) => toRegex(pattern).test(p))).toBe(true)
  })
  it("every sample URL matches a public shape (no stale entries)", () => {
    const res = shapes()
      .filter((s) => s.listeners.includes("public"))
      .map((s) => toRegex(s.pattern))
    for (const p of paths)
      expect(
        res.some((r) => r.test(p)),
        p,
      ).toBe(true)
  })
})
