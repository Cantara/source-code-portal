import { GraphController, hasWebGL } from "@/features/manifests/graph/sigma-controller"
import { buildAdjacency, nodeSize, renderability } from "@/features/manifests/graph/model"
import { syntheticGraph } from "@/test/synthetic-graph"
import type { LayoutRequest, LayoutResponse } from "@/features/manifests/graph/layout"

const out: Record<string, unknown> = {}
import { toTrustedScriptUrl } from "@/lib/trusted-types"
// The real module: one "scp-html" policy that mints both HTML and script URLs. ?tt=plain skips it to show what the CSP does to a plain string.
const plain = new URLSearchParams(location.search).get("tt") === "plain"
const workerUrl: string | TrustedScriptURL = plain ? "/assets/worker.js" : toTrustedScriptUrl("/assets/worker.js")

async function main() {
  const g = syntheticGraph()
  out["webgl"] = hasWebGL(document)
  out["renderability"] = renderability(g, true)
  const adj = buildAdjacency(g)
  const input = { ids: g.nodes.map((n) => n.id), edges: g.edges.map((e) => [adj.index.get(e.from)!, adj.index.get(e.to)!] as const), sizes: g.nodes.map((n) => nodeSize(n.inDegree)) }
  let worker: Worker
  try { worker = new Worker(workerUrl as string, { type: "module" }) } catch (e) { out["workerError"] = String(e); return }
  const t0 = performance.now()
  const long: number[] = []
  new PerformanceObserver((l) => l.getEntries().forEach((e) => long.push(e.duration))).observe({ type: "longtask", buffered: true })
  const positions = await new Promise<Float32Array>((res, rej) => {
    worker.onmessage = (m: MessageEvent<LayoutResponse>) => (m.data.type === "layout" ? res(m.data.positions) : rej(new Error(m.data.message)))
    worker.onerror = (e) => rej(new Error("worker error " + e.message))
    worker.postMessage({ type: "layout", key: "k", input } satisfies LayoutRequest)
  })
  out["layoutMs"] = Math.round(performance.now() - t0)
  const cs = getComputedStyle(document.documentElement)
  const palette = { viz: Array.from({ length: 8 }, (_, i) => cs.getPropertyValue(`--viz-${i + 1}`).trim() || "#4466cc"), dim: "#cccccc", edge: "#bbbbbb", edgeHi: "#2244cc", label: "#222222" }
  const m0 = performance.now()
  const c = new GraphController(document.getElementById("c")!, { palette, reducedMotion: true, onSelect: () => {}, onHover: () => {} })
  const s0 = performance.now()
  c.setData(g, positions)
  out["setDataMs"] = Math.round(performance.now() - s0)
  await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)))
  out["mountMs"] = Math.round(performance.now() - m0)
  c.select(g.nodes[0]!.id)
  await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)))
  out["selected"] = c.getSelected()
  out["longTasks"] = long.map(Math.round)
  c.kill(); worker.terminate()
}
main().catch((e) => { out["error"] = String(e) }).finally(() => { (window as unknown as { __result: unknown }).__result = out })
