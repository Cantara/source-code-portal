// Pure force layout, runs inside a module Web Worker (layout.worker.ts) and directly in unit tests (Node).
import { forceCenter, forceCollide, forceLink, forceManyBody, forceSimulation, type SimulationLinkDatum, type SimulationNodeDatum } from "d3-force"
import { fnv1a } from "@/shared/avatar"

export interface LayoutInput {
  readonly ids: readonly string[]                        // node ids in graph order
  readonly edges: readonly (readonly [number, number])[] // dense indices
  readonly sizes: readonly number[]                      // sigma node sizes (collision radius derives from them)
  readonly previous?: ReadonlyMap<string, readonly [number, number]> // positions of an earlier layout of (nearly) the same graph
  readonly ticks?: number
}

/** Deterministic seed position of an id: hash -> angle and radius. Same id, same start, on every machine. */
export function seedPosition(id: string, spread: number): [number, number] {
  const a = (fnv1a(id) / 0x1_0000_0000) * Math.PI * 2
  const r = Math.sqrt(fnv1a(id + "#r") / 0x1_0000_0000) * spread
  return [Math.cos(a) * r, Math.sin(a) * r]
}

/** Linear congruential generator for d3's randomSource (jiggle of coincident nodes): reproducible layouts. */
export function lcg(seed = 1): () => number {
  let s = seed >>> 0
  return () => ((s = (Math.imul(1664525, s) + 1013904223) >>> 0) / 0x1_0000_0000)
}

interface N extends SimulationNodeDatum { i: number }

/** Returns interleaved x,y (length 2n). Cold layout: 300 ticks; warm (>= 90 % of nodes have a previous position): 60 ticks. */
export function layout(inp: LayoutInput): Float32Array {
  const n = inp.ids.length
  const spread = Math.sqrt(n) * 18
  let known = 0
  const nodes: N[] = inp.ids.map((id, i) => {
    const p = inp.previous?.get(id)
    if (p) known++
    const [x, y] = p ?? seedPosition(id, spread)
    return { i, x, y }
  })
  const warm = n > 0 && known / n >= 0.9
  const ticks = inp.ticks ?? (warm ? 60 : 300)
  const links: SimulationLinkDatum<N>[] = inp.edges.map(([s, t]) => ({ source: s, target: t }))
  const alpha0 = warm ? 0.3 : 1
  const sim0 = { alphaMin: 0.001 }
  const sim = forceSimulation<N>(nodes)
    .randomSource(lcg(7))
    .force("link", forceLink<N, SimulationLinkDatum<N>>(links).id((d) => d.i).distance(28).strength(0.35))
    .force("charge", forceManyBody<N>().strength(-28).theta(0.9).distanceMax(400))
    .force("center", forceCenter(0, 0))
    .force("collide", forceCollide<N>().radius((d) => (inp.sizes[d.i] ?? 3) + 1.5).iterations(1))
    .alpha(alpha0)
    .alphaDecay(1 - Math.pow(sim0.alphaMin / alpha0, 1 / ticks)) // cools to alphaMin exactly at `ticks`: runtime is a function of `ticks`
    .stop()
  sim.tick(ticks)
  const out = new Float32Array(n * 2)
  for (const d of nodes) { out[d.i * 2] = d.x ?? 0; out[d.i * 2 + 1] = d.y ?? 0 }
  return out
}

/** Message protocol of layout.worker.ts (structured clone; positions come back as a transferable Float32Array). */
export type LayoutRequest = { readonly type: "layout"; readonly key: string; readonly input: LayoutInput }
export type LayoutResponse = { readonly type: "layout"; readonly key: string; readonly positions: Float32Array; readonly ms: number } | { readonly type: "error"; readonly key: string; readonly message: string }
