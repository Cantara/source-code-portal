import { describe, expect, it } from "vitest"
import { shapes, toPattern } from "./route-shapes.ts"

describe("route shapes (the SPA/Go contract, dist/routes.json)", () => {
  it("converts TanStack segments to ServeMux patterns", () => {
    expect(toPattern("/repos/$repo/docs/$")).toBe("/repos/{repo}/docs/{path...}")
    expect(toPattern("/groups/$groupId")).toBe("/groups/{groupId}")
  })
  it("matches the golden list (a route change must update this and the Go routes test together)", () => {
    expect(shapes()).toMatchInlineSnapshot(`
      [
        {
          "listeners": [
            "public",
          ],
          "pattern": "/dashboard",
        },
        {
          "listeners": [
            "public",
          ],
          "pattern": "/groups/{groupId}",
        },
        {
          "listeners": [
            "public",
          ],
          "pattern": "/groups/{groupId}/commits",
        },
        {
          "listeners": [
            "public",
          ],
          "pattern": "/commits",
        },
        {
          "listeners": [
            "public",
          ],
          "pattern": "/repos/{repo}",
        },
        {
          "listeners": [
            "public",
          ],
          "pattern": "/repos/{repo}/docs/{path...}",
        },
        {
          "listeners": [
            "public",
          ],
          "pattern": "/repos/{repo}/commits",
        },
        {
          "listeners": [
            "public",
          ],
          "pattern": "/repos/{repo}/dependencies",
        },
        {
          "listeners": [
            "public",
          ],
          "pattern": "/dependencies",
        },
        {
          "listeners": [
            "public",
          ],
          "pattern": "/packages",
        },
        {
          "listeners": [
            "public",
          ],
          "pattern": "/pages/{slug}",
        },
        {
          "listeners": [
            "admin",
          ],
          "pattern": "/status",
        },
      ]
    `)
  })
  it("lists /status for the admin listener only and never lists /", () => {
    const s = shapes()
    expect(s.find((x) => x.pattern === "/status")?.listeners).toEqual(["admin"])
    expect(s.some((x) => x.pattern === "/")).toBe(false)
    expect(s.filter((x) => x.pattern !== "/status").every((x) => x.listeners[0] === "public")).toBe(true)
  })
})
