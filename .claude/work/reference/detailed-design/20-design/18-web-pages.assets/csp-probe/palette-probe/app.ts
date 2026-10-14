import { GraphController } from "@/features/manifests/graph/sigma-controller"
import { syntheticGraph } from "@/test/synthetic-graph"
const out: Record<string, unknown> = {}
async function main() {
  const g = syntheticGraph()
  const n = g.nodes.length
  const positions = new Float32Array(n * 2)
  g.nodes.forEach((_, i) => { positions[i * 2] = Math.cos(i) * (i % 50); positions[i * 2 + 1] = Math.sin(i) * (i % 50) })
  const light = { viz: ["#111111","#222222","#333333","#444444","#555555","#666666","#777777","#888888"], dim: "#cccccc", edge: "#bbbbbb", edgeHi: "#2244cc", label: "#222222" }
  const dark = { viz: ["#aa0000","#bb0000","#cc0000","#dd0000","#ee0000","#ff0000","#ff1111","#ff2222"], dim: "#333333", edge: "#555555", edgeHi: "#88aaff", label: "#eeeeee" }
  const c = new GraphController(document.getElementById("c")!, { palette: light, reducedMotion: true, onSelect: () => {}, onHover: () => {}, onContextLost: () => { (out as any)["onContextLostCallback"] = true } })
  c.setData(g, positions)
  await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)))
  const sig = (c as any).sigma
  const id = g.nodes[0]!.id
  out["colorBefore"] = sig.getNodeDisplayData(id)?.color
  c.setPalette(dark)
  await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)))
  out["colorAfterSetPalette"] = sig.getNodeDisplayData(id)?.color
  out["labelColorSetting"] = sig.getSetting("labelColor")
  const eid = sig.getGraph().edges()[0]
  out["edgeColorAfter"] = sig.getGraph().getEdgeAttribute(eid, "color")
  // context loss
  const canv = Object.entries(sig.getCanvases()).map(([k, v]: any) => [k, (v as HTMLCanvasElement).tagName])
  out["canvases"] = canv
  let lost = 0
  document.getElementById("c")!.addEventListener("webglcontextlost", () => lost++, true)
  const nodesCanvas = sig.getCanvases()["nodes"] as HTMLCanvasElement
  const gl = (nodesCanvas.getContext("webgl2") ?? nodesCanvas.getContext("webgl")) as WebGL2RenderingContext
  gl.getExtension("WEBGL_lose_context")?.loseContext()
  await new Promise((r) => setTimeout(r, 200))
  out["contextLostEventsSeenAtContainerCapture"] = lost
  c.kill()
}
main().catch((e) => { out["error"] = String(e) }).finally(() => { (window as any).__result = out })
