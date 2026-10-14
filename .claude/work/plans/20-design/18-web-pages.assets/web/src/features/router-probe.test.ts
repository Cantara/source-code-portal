import { describe, expect, it } from "vitest"
import { createMemoryHistory, createRootRoute, createRoute, createRouter } from "@tanstack/react-router"
import { orgDepsSearch } from "./manifests/search"
import { groupSearch } from "./groups/search"

const root = createRootRoute()
const repo = createRoute({ getParentRoute: () => root, path: "/repos/$repo" })
const docs = createRoute({ getParentRoute: () => repo, path: "/docs/$" })
const deps = createRoute({ getParentRoute: () => root, path: "/dependencies", validateSearch: orgDepsSearch })
const group = createRoute({ getParentRoute: () => root, path: "/groups/$groupId", validateSearch: groupSearch })
const router = createRouter({ routeTree: root.addChildren([repo.addChildren([docs]), deps, group]), history: createMemoryHistory({ initialEntries: ["/"] }) })

describe("TanStack Router behaviour the design depends on (probed 2026-10-14, router 1.170.41)", () => {
  it("splat keeps slashes; param values are encoded", () => {
    const l = router.buildLocation({ to: "/repos/$repo/docs/$", params: { repo: "next.js", _splat: "docs/install.md" } } as never)
    console.log("splat href:", l.href)
    expect(l.href).toBe("/repos/next.js/docs/docs/install.md")
    const l2 = router.buildLocation({ to: "/repos/$repo", params: { repo: "a b" } } as never)
    console.log("space href:", l2.href)
  })
  it("search params: arrays and booleans are JSON in the URL, defaults are not stripped unless asked", () => {
    const l = router.buildLocation({ to: "/dependencies", search: { view: "versions", ecosystem: ["maven", "npm"], intra: true } } as never)
    console.log("search href:", l.href)
    const g = router.buildLocation({ to: "/groups/$groupId", params: { groupId: "platform" }, search: { sort: "-pushedAt", lifecycle: ["production"] } } as never)
    console.log("group href:", g.href)
    expect(l.href.startsWith("/dependencies?")).toBe(true)
  })
  it("matchRoutes of an unknown path yields no leaf route", () => {
    const m = router.matchRoutes("/nope/never", {})
    console.log("unknown matches:", m.map((x) => x.routeId))
    const m2 = router.matchRoutes("/repos/x/readme.md", {})
    console.log("repos/x/readme.md matches:", m2.map((x) => x.routeId))
  })
})
