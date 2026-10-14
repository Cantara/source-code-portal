import { useEffect, useRef, useState } from "react"
import type { LayoutRequest, LayoutResponse } from "./layout"
import { toTrustedScriptUrl } from "@/lib/trusted-types"
import { buildAdjacency, nodeSize, type DependencyGraph as G } from "./model"
import { GraphController, hasWebGL, type Palette } from "./sigma-controller"

export interface DependencyGraphProps {
  readonly graph: G
  readonly snapshotKey: string                                 // changes when the data changes: layout cache key
  readonly palette: Palette
  readonly selected: string | null
  readonly onSelect: (id: string | null) => void
  readonly onStatus: (s: "layouting" | "ready" | "unavailable") => void
}

/** Mounts sigma once, lays out in a module worker, re-uses previous positions on data change. The table/tree stay the source of truth. */
export function DependencyGraphCanvas(p: DependencyGraphProps) {
  const el = useRef<HTMLDivElement>(null)
  const ctl = useRef<GraphController | null>(null)
  const positions = useRef<Map<string, readonly [number, number]>>(new Map())
  const [ok] = useState(() => hasWebGL(document))
  const { onSelect, onStatus } = p

  useEffect(() => {
    if (!ok || !el.current) { onStatus("unavailable"); return }
    const c = new GraphController(el.current, { palette: p.palette, reducedMotion: window.matchMedia("(prefers-reduced-motion: reduce)").matches, onSelect, onHover: () => undefined, onContextLost: () => onStatus("unavailable") })
    ctl.current = c
    const ro = new ResizeObserver(() => c.resize()); ro.observe(el.current)
    return () => { ro.disconnect(); c.kill(); ctl.current = null }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- created once per mount; palette changes go through setPalette below
  }, [ok])

  useEffect(() => { ctl.current?.setPalette(p.palette) }, [p.palette])
  useEffect(() => { const c = ctl.current; if (c && c.getSelected() !== p.selected) c.select(p.selected) }, [p.selected])

  useEffect(() => {
    const c = ctl.current
    if (!c) return
    onStatus("layouting")
    // `new Worker` is a Trusted Types sink: the URL must come from the single "scp-html" policy (lib/trusted-types.ts). The DOM lib types the argument as string | URL.
    let w: Worker
    try { w = new Worker(toTrustedScriptUrl(new URL("./layout.worker.ts", import.meta.url).href) as string, { type: "module" }) } catch { onStatus("unavailable"); return } // policy refusal or CSP: the constructor throws synchronously
    const adj = buildAdjacency(p.graph)
    const req: LayoutRequest = { type: "layout", key: p.snapshotKey, input: { ids: p.graph.nodes.map((n) => n.id), edges: p.graph.edges.flatMap((e) => { const a = adj.index.get(e.from), b = adj.index.get(e.to); return a === undefined || b === undefined ? [] : [[a, b] as const] }), sizes: p.graph.nodes.map((n) => nodeSize(n.inDegree)), previous: positions.current } }
    // V-10: the worker can fail in two ways the first draft ignored, leaving "Arranging N units..." on screen forever: a thrown layout (a message of type
    // "error") and a worker that cannot start (CSP, network, 404 of the hashed chunk after a deploy: `error` event). Both fall back to the Outline.
    w.onerror = () => { onStatus("unavailable"); w.terminate() }
    w.onmessage = (m: MessageEvent<LayoutResponse>) => {
      if (m.data.type === "error") { if (m.data.key === p.snapshotKey) { onStatus("unavailable"); w.terminate() } return }
      if (m.data.key !== p.snapshotKey) return // a newer request superseded this one
      const pos = m.data.positions
      p.graph.nodes.forEach((n, i) => positions.current.set(n.id, [pos[i * 2] ?? 0, pos[i * 2 + 1] ?? 0]))
      c.setData(p.graph, pos); onStatus("ready"); w.terminate()
    }
    w.postMessage(req)
    return () => w.terminate() // unmount or newer data: the in-flight layout is discarded
  }, [p.snapshotKey, p.graph, onStatus])

  return <div ref={el} className="h-[min(70vh,720px)] w-full rounded-lg border bg-card" role="group" aria-roledescription="dependency graph" tabIndex={0} />
}
