import type { components } from "@/lib/api/schema"
export type DependencyGraph = components["schemas"]["DependencyGraph"]
export type GraphNode = components["schemas"]["GraphNode"]
export type GraphEdge = components["schemas"]["GraphEdge"]

/** Hard client limits (the API caps at 2,000 nodes / 20,000 edges; beyond these the canvas is not offered, the table is). */
export const GRAPH_LIMITS = { renderNodes: 2000, renderEdges: 20000, arrowEdges: 3000 } as const

export interface Adjacency {
  readonly index: ReadonlyMap<string, number>          // node id -> dense index (order of graph.nodes)
  readonly out: ReadonlyMap<string, readonly string[]> // id -> units it depends on
  readonly in: ReadonlyMap<string, readonly string[]>  // id -> units that depend on it
}

export function buildAdjacency(g: Pick<DependencyGraph, "nodes" | "edges">): Adjacency {
  const index = new Map<string, number>()
  g.nodes.forEach((n, i) => index.set(n.id, i))
  const out = new Map<string, string[]>()
  const inn = new Map<string, string[]>()
  for (const n of g.nodes) { out.set(n.id, []); inn.set(n.id, []) }
  for (const e of g.edges) {
    // an edge whose endpoint was dropped by the server cap is skipped, never invented
    if (!index.has(e.from) || !index.has(e.to)) continue
    out.get(e.from)?.push(e.to)
    inn.get(e.to)?.push(e.from)
  }
  return { index, out, in: inn }
}

/** Radius in sigma "size" units: 3 (no dependants) .. 14 (very many), log scale on in-degree (dependants matter most, D-15). */
export function nodeSize(inDegree: number): number {
  return Math.round((3 + Math.min(11, Math.log2(1 + inDegree) * 2.2)) * 10) / 10
}

/** Ecosystems ordered by node count (ties by id): the first 7 get viz-1..7, everything else shares viz-8 ("other"). */
export function ecosystemOrder(nodes: readonly GraphNode[]): string[] {
  const c = new Map<string, number>()
  for (const n of nodes) c.set(n.ecosystem, (c.get(n.ecosystem) ?? 0) + 1)
  return [...c.entries()].sort((a, b) => b[1] - a[1] || (a[0] < b[0] ? -1 : 1)).map(([k]) => k)
}
export function ecosystemSlot(eco: string, order: readonly string[]): number {
  const i = order.indexOf(eco)
  return i === -1 || i >= 7 ? 8 : i + 1
}

/** Node ids sorted for keyboard traversal and for the "Table" view default: most depended-upon first, then label, then id. */
export function traversalOrder(nodes: readonly GraphNode[]): string[] {
  return [...nodes].sort((a, b) => b.inDegree - a.inDegree || a.label.localeCompare(b.label) || (a.id < b.id ? -1 : 1)).map((n) => n.id)
}

export type Renderability = { readonly ok: true } | { readonly ok: false; readonly reason: "too-many-nodes" | "too-many-edges" | "empty" | "no-webgl" }
export function renderability(g: Pick<DependencyGraph, "nodes" | "edges">, webgl: boolean): Renderability {
  if (g.nodes.length === 0) return { ok: false, reason: "empty" }
  if (!webgl) return { ok: false, reason: "no-webgl" }
  if (g.nodes.length > GRAPH_LIMITS.renderNodes) return { ok: false, reason: "too-many-nodes" }
  if (g.edges.length > GRAPH_LIMITS.renderEdges) return { ok: false, reason: "too-many-edges" }
  return { ok: true }
}

export const UNIT_SORTS = ["-inDegree", "inDegree", "-outDegree", "outDegree", "name", "-name", "ecosystem", "-ecosystem"] as const
export type UnitSort = (typeof UNIT_SORTS)[number]

/** The accessible "Table" view sorts in memory (the graph payload is complete, at most 2,000 units). Ties always fall back to label then id: the order is total. */
export function sortNodes(nodes: readonly GraphNode[], token: UnitSort): GraphNode[] {
  const desc = token.startsWith("-")
  const key = desc ? token.slice(1) : token
  const sign = desc ? -1 : 1
  const primary = (a: GraphNode, b: GraphNode): number => {
    switch (key) {
      case "inDegree": return a.inDegree - b.inDegree
      case "outDegree": return a.outDegree - b.outDegree
      case "ecosystem": return a.ecosystem < b.ecosystem ? -1 : a.ecosystem > b.ecosystem ? 1 : 0
      default: return a.label.localeCompare(b.label)
    }
  }
  return [...nodes].sort((a, b) => sign * primary(a, b) || a.label.localeCompare(b.label) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
}
