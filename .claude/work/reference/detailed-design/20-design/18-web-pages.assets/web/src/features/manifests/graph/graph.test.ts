import { describe, expect, it } from "vitest"
import { graphSmall } from "@/test/fixtures"
import { syntheticGraph } from "@/test/synthetic-graph"
import { layout, seedPosition } from "./layout"
import { buildAdjacency, ecosystemOrder, ecosystemSlot, GRAPH_LIMITS, nodeSize, renderability, sortNodes, traversalOrder } from "./model"
import { edgeView, focusSet, nodeView } from "./reducers"
import { spreadSegments } from "./spread"
import { dependencyTree } from "./tree"

describe("model", () => {
  it("adjacency skips edges whose endpoint was dropped by the server cap", () => {
    const adj = buildAdjacency({ nodes: graphSmall.nodes, edges: [...graphSmall.edges, { ...graphSmall.edges[0]!, to: "gone#." }] })
    expect(adj.out.get("billing-service#.")).toEqual(["core-lib#."]); expect(adj.in.get("core-lib#.")).toEqual(["billing-service#.", "auth-client#."])
  })
  it("size is monotone, bounded 3..14", () => {
    const s = [0, 1, 3, 10, 100, 100000].map(nodeSize)
    expect(s[0]).toBe(3); expect([...s].sort((a, b) => a - b)).toEqual(s); expect(Math.max(...s)).toBeLessThanOrEqual(14)
  })
  it("ecosystem slots: by frequency, 8th shared", () => {
    const order = ecosystemOrder(syntheticGraph(100, 100, 10).nodes)
    expect(ecosystemSlot(order[0]!, order)).toBe(1); expect(ecosystemSlot(order[7] ?? "x", order)).toBe(8); expect(ecosystemSlot("unknown", order)).toBe(8)
  })
  it("traversal order: most depended upon first, deterministic", () => {
    expect(traversalOrder(graphSmall.nodes)).toEqual(["auth-client#.", "core-lib#.", "billing-service#."])
  })
  it("renderability", () => {
    expect(renderability(graphSmall, true)).toEqual({ ok: true }); expect(renderability(graphSmall, false)).toEqual({ ok: false, reason: "no-webgl" })
    expect(renderability({ nodes: [], edges: [] }, true)).toEqual({ ok: false, reason: "empty" })
    expect(renderability({ nodes: new Array(GRAPH_LIMITS.renderNodes + 1).fill(graphSmall.nodes[0]!), edges: [] }, true)).toEqual({ ok: false, reason: "too-many-nodes" })
  })
})

describe("layout (1,000 nodes, 2,500 edges: the E6/E7 benchmark graph)", () => {
  const g = syntheticGraph()
  const adj = buildAdjacency(g)
  const input = { ids: g.nodes.map((n) => n.id), edges: g.edges.map((e) => [adj.index.get(e.from)!, adj.index.get(e.to)!] as const), sizes: g.nodes.map((n) => nodeSize(n.inDegree)) }
  it("is deterministic, finite and fast enough for a worker", () => {
    const t0 = performance.now(); const a = layout(input); const ms = performance.now() - t0
    const b = layout(input)
    expect(Buffer.from(a.buffer).equals(Buffer.from(b.buffer))).toBe(true)
    expect(a.every(Number.isFinite)).toBe(true)
    console.log(`cold layout 1000/2500: ${ms.toFixed(0)} ms (300 ticks, node ${process.version})`)
    expect(ms).toBeLessThan(3000) // measured ~0.6 s on the research machine; 5x headroom for CI
  })
  it("a warm layout of the same graph barely moves it (stable between snapshots)", () => {
    const cold = layout(input)
    const prev = new Map(input.ids.map((id, i) => [id, [cold[i * 2]!, cold[i * 2 + 1]!] as const]))
    const t0 = performance.now(); const warm = layout({ ...input, previous: prev }); const ms = performance.now() - t0
    let moved = 0; for (let i = 0; i < warm.length; i++) moved += Math.abs(warm[i]! - cold[i]!)
    console.log(`warm layout: ${ms.toFixed(0)} ms, mean move ${(moved / warm.length).toFixed(2)} units`)
    expect(moved / warm.length).toBeLessThan(25)
    expect(ms).toBeLessThan(1500)
  })
  it("seed positions are stable per id", () => { expect(seedPosition("a#.", 100)).toEqual(seedPosition("a#.", 100)); expect(seedPosition("a#.", 100)).not.toEqual(seedPosition("b#.", 100)) })
  it("an empty graph lays out to an empty array", () => { expect(layout({ ids: [], edges: [], sizes: [] })).toHaveLength(0) })
})

describe("unit table sort", () => {
  const g = syntheticGraph()
  it("is total and deterministic; descending in-degree puts the most depended-upon first", () => {
    const a = sortNodes(g.nodes, "-inDegree"), b = sortNodes(g.nodes, "-inDegree")
    expect(a.map((n) => n.id)).toEqual(b.map((n) => n.id))
    expect(a[0]!.inDegree).toBeGreaterThanOrEqual(a[1]!.inDegree)
    expect(sortNodes(g.nodes, "inDegree")[0]!.inDegree).toBeLessThanOrEqual(sortNodes(g.nodes, "inDegree")[1]!.inDegree)
  })
  it("name sort ignores nothing and does not mutate the input", () => {
    const copy = g.nodes.map((n) => n.id)
    sortNodes(g.nodes, "name")
    expect(g.nodes.map((n) => n.id)).toEqual(copy)
  })
})

describe("highlight reducers", () => {
  const adj = buildAdjacency(graphSmall)
  it("selection wins over hover; neighbours in both directions", () => {
    expect([...focusSet({ hovered: "billing-service#.", selected: "auth-client#." }, adj)!].sort()).toEqual(["auth-client#.", "core-lib#."])
    expect([...focusSet({ hovered: "billing-service#.", selected: null }, adj)!].sort()).toEqual(["billing-service#.", "core-lib#."])
    expect(focusSet({ hovered: null, selected: null }, adj)).toBeNull(); expect(focusSet({ hovered: "ghost", selected: null }, adj)).toBeNull()
  })
  it("non-neighbours dim and lose their label; unrelated edges hide", () => {
    const st = { hovered: "billing-service#.", selected: null }; const f = focusSet(st, adj)
    expect(nodeView("auth-client#.", { color: "#abc", label: "a" }, f, st, "#ddd")).toEqual({ color: "#ddd", label: null, zIndex: 0 })
    expect(nodeView("core-lib#.", { color: "#abc", label: "c" }, f, st, "#ddd")).toMatchObject({ color: "#abc", forceLabel: true })
    expect(edgeView("core-lib#.", "auth-client#.", f, st, "#00f")).toEqual({ hidden: true })
    expect(edgeView("billing-service#.", "core-lib#.", f, st, "#00f")).toMatchObject({ color: "#00f" })
    expect(nodeView("x", { color: "#abc", label: "a" }, null, { hovered: null, selected: null }, "#ddd")).toEqual({})
  })
})

describe("dependency tree (the accessible alternative)", () => {
  const adj = buildAdjacency(graphSmall); const byId = new Map(graphSmall.nodes.map((n) => [n.id, n]))
  it("marks a cycle instead of recursing", () => {
    const t = dependencyTree("billing-service#.", "out", adj, byId, 5)!
    expect(t.children[0]!.id).toBe("core-lib#."); expect(t.children[0]!.children[0]!.id).toBe("auth-client#.")
    expect(t.children[0]!.children[0]!.children[0]).toMatchObject({ id: "core-lib#.", cycle: true, children: [] })
  })
  it("caps depth and breadth", () => {
    expect(dependencyTree("billing-service#.", "out", adj, byId, 1)!.children[0]).toMatchObject({ more: 1, children: [] })
    const big = syntheticGraph(300, 900, 5); const a = buildAdjacency(big); const m = new Map(big.nodes.map((n) => [n.id, n]))
    const hub = [...big.nodes].sort((x, y) => y.inDegree - x.inDegree)[0]!
    const t = dependencyTree(hub.id, "in", a, m, 1, 3)!
    expect(t.children.length).toBeLessThanOrEqual(3); expect(t.children.length + t.more).toBe(hub.inDegree)
  })
  it("unknown root is null", () => expect(dependencyTree("nope", "in", adj, byId)).toBeNull())
})

describe("version spread segments", () => {
  it("merges the tail into other and sums to 1", () => {
    const s = spreadSegments([...Array.from({ length: 8 }, (_, i) => ({ version: `1.${i}`, units: 10 - i, repos: 5 }))])
    expect(s).toHaveLength(6); expect(s[5]).toMatchObject({ version: "other", slot: 8 }); expect(s.reduce((a, x) => a + x.share, 0)).toBeCloseTo(1, 10)
    expect(spreadSegments([])).toEqual([])
  })
})
