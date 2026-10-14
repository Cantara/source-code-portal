import type { Adjacency, GraphNode } from "./model"

export type Direction = "out" | "in"
export interface TreeNode { readonly id: string; readonly label: string; readonly ecosystem: string; readonly cycle: boolean; readonly children: TreeNode[]; readonly more: number }

/**
 * Accessible alternative of the graph: the dependency tree of one unit ("depends on" = out, "used by" = in).
 * A unit that already appears on the path from the root is shown once more as a leaf with `cycle: true` (never expanded),
 * every level is capped at `maxChildren` with the remainder counted in `more`; depth is capped at `maxDepth`.
 */
export function dependencyTree(root: string, dir: Direction, adj: Adjacency, byId: ReadonlyMap<string, GraphNode>, maxDepth = 3, maxChildren = 50): TreeNode | null {
  const rn = byId.get(root)
  if (!rn) return null
  const walk = (id: string, depth: number, path: ReadonlySet<string>): TreeNode => {
    const n = byId.get(id) as GraphNode
    const next = (dir === "out" ? adj.out : adj.in).get(id) ?? []
    if (depth >= maxDepth) return { id, label: n.label, ecosystem: n.ecosystem, cycle: false, children: [], more: next.length }
    const unique = [...new Set(next)].sort((a, b) => (byId.get(a)?.label ?? a).localeCompare(byId.get(b)?.label ?? b) || (a < b ? -1 : 1))
    const shown = unique.slice(0, maxChildren)
    const p = new Set(path).add(id)
    const children = shown.map((c) => (p.has(c) ? { id: c, label: byId.get(c)?.label ?? c, ecosystem: byId.get(c)?.ecosystem ?? "", cycle: true, children: [], more: 0 } : walk(c, depth + 1, p)))
    return { id, label: n.label, ecosystem: n.ecosystem, cycle: false, children, more: unique.length - shown.length }
  }
  return walk(root, 0, new Set())
}
