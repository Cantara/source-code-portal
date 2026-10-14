import type { DependencyGraph, GraphEdge, GraphNode } from "@/features/manifests/graph/model"
import { staleManifests } from "./fixtures"
import { lcg } from "@/features/manifests/graph/layout"

/** The benchmark graph of research 08 E6: n nodes in 20 clusters, m edges, 80 % intra-cluster. Deterministic. */
export function syntheticGraph(n = 1000, m = 2500, clusters = 20, seed = 42): DependencyGraph {
  const r = lcg(seed)
  const ecos = ["maven", "npm", "golang", "pypi", "cargo", "nuget", "gem", "docker", "composer"]
  const nodes: GraphNode[] = Array.from({ length: n }, (_, i) => ({ id: `repo-${i}#.`, repo: `repo-${i}`, dir: ".", ecosystem: ecos[i % clusters % ecos.length] as string, label: `repo-${i}`, artifacts: [], inDegree: 0, outDegree: 0, inCycle: false }))
  const edges: GraphEdge[] = []
  const seen = new Set<string>()
  while (edges.length < m) {
    const a = Math.floor(r() * n)
    const intra = r() < 0.8
    const b = intra ? Math.floor(a / (n / clusters)) * (n / clusters) + Math.floor(r() * (n / clusters)) : Math.floor(r() * n)
    if (a === b || seen.has(`${a}>${b}`)) continue
    seen.add(`${a}>${b}`)
    const from = nodes[a] as GraphNode, to = nodes[b] as GraphNode
    nodes[a] = { ...from, outDegree: from.outDegree + 1 }
    nodes[b] = { ...(nodes[b] as GraphNode), inDegree: to.inDegree + 1 }
    edges.push({ from: from.id, to: to.id, component: `pkg:maven/acme/${to.repo}`, requirement: "1.0", version: "1.0", scope: "runtime", intra: false, versionStatus: "current" })
  }
  return { nodes, edges, cycles: [], summary: { repos: n, units: n, edges: m, cycles: 0, ecosystems: [] }, truncated: false, freshness: staleManifests }
}
