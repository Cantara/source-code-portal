// Framework-agnostic wrapper around sigma 3.0.3 + graphology 0.26.0 (API verified against the installed type declarations, 2026-10-14).
// React only mounts it: new GraphController(el, opts) in an effect, setData() on data change, kill() in the cleanup.
import Graph from "graphology"
import Sigma from "sigma"
import { ecosystemOrder, ecosystemSlot, GRAPH_LIMITS, nodeSize, buildAdjacency, type Adjacency, type DependencyGraph } from "./model"
import { edgeView, focusSet, nodeView, type HighlightState } from "./reducers"

export interface Palette {
  /** resolved CSS colours (read from getComputedStyle of the container once per theme change; sigma needs concrete colours, not var()) */
  readonly viz: readonly string[]   // 8 entries, viz-1..viz-8
  readonly dim: string              // dimmed node
  readonly edge: string
  readonly edgeHi: string
  readonly label: string
}

export interface GraphControllerOptions {
  readonly palette: Palette
  readonly reducedMotion: boolean
  readonly onSelect: (id: string | null) => void
  readonly onHover: (id: string | null) => void
  /** The WebGL context was lost (GPU reset, too many contexts): the owner switches to the Outline and offers a reload (design 4.9.9). */
  readonly onContextLost?: (() => void) | undefined
}

interface NodeAttrs { x: number; y: number; size: number; label: string; color: string; baseColor: string; slot: number /* 1..8 palette slot of the ecosystem */ }
interface EdgeAttrs { color: string; size: number; type: string }

export class GraphController {
  private readonly sigma: Sigma<NodeAttrs, EdgeAttrs>
  private state: HighlightState = { hovered: null, selected: null }
  private adj: Adjacency | null = null
  private focus: ReadonlySet<string> | null = null // neighbour set of the hovered/selected node, recomputed once per state change (not per node)
  private palette: Palette
  private readonly opts: GraphControllerOptions
  private readonly container: HTMLElement
  /** `webglcontextlost` does not bubble: it is observed in the capture phase on the container (probed in Chrome 155, one event per lost context). */
  private readonly onLost = (): void => { this.opts.onContextLost?.() }

  constructor(container: HTMLElement, opts: GraphControllerOptions) {
    this.opts = opts
    this.container = container
    this.palette = opts.palette
    this.sigma = new Sigma<NodeAttrs, EdgeAttrs>(new Graph<NodeAttrs, EdgeAttrs>(), container, {
      renderEdgeLabels: false,
      enableEdgeEvents: false,              // edge hit-testing is the expensive part of event handling; edges are not interactive
      hideEdgesOnMove: true,                // pan/zoom of 1,000 nodes stays on the node program only
      hideLabelsOnMove: true,
      labelRenderedSizeThreshold: 9,        // labels only for nodes drawn >= 9 px: the big (much depended upon) nodes
      labelDensity: 0.6,
      labelGridCellSize: 120,
      labelColor: { color: opts.palette.label },
      zIndex: true,                         // required for the highlight ordering in the reducers
      minCameraRatio: 0.05,
      maxCameraRatio: 8,
      nodeReducer: (id, data) => ({ ...data, ...this.nodeDisplay(id, data) }),
      edgeReducer: (edge, data) => ({ ...data, ...this.edgeDisplay(edge, data) }),
    })
    container.addEventListener("webglcontextlost", this.onLost, true)
    this.sigma.on("enterNode", ({ node }) => this.setState({ ...this.state, hovered: node }, true))
    this.sigma.on("leaveNode", () => this.setState({ ...this.state, hovered: null }, true))
    this.sigma.on("clickNode", ({ node }) => this.select(node))
    this.sigma.on("clickStage", () => this.select(null))
  }

  /** Replaces the graph. `positions` is the interleaved x,y output of layout(); same order as graph.nodes. */
  setData(g: DependencyGraph, positions: Float32Array): void {
    const order = ecosystemOrder(g.nodes)
    const gr = new Graph<NodeAttrs, EdgeAttrs>({ type: "directed", multi: false })
    g.nodes.forEach((n, i) => {
      const slot = ecosystemSlot(n.ecosystem, order)
      const color = this.palette.viz[slot - 1] ?? this.palette.dim
      gr.addNode(n.id, { x: positions[i * 2] ?? 0, y: positions[i * 2 + 1] ?? 0, size: nodeSize(n.inDegree), label: n.label, color, baseColor: color, slot })
    })
    const type = g.edges.length <= GRAPH_LIMITS.arrowEdges ? "arrow" : "line"
    for (const e of g.edges) {
      if (!gr.hasNode(e.from) || !gr.hasNode(e.to) || gr.hasDirectedEdge(e.from, e.to)) continue // collapse parallel edges: one line per unit pair
      gr.addDirectedEdge(e.from, e.to, { color: this.palette.edge, size: 1, type })
    }
    this.adj = buildAdjacency(g)
    this.state = { hovered: null, selected: this.state.selected !== null && gr.hasNode(this.state.selected) ? this.state.selected : null }
    this.focus = focusSet(this.state, this.adj)
    this.sigma.setSettings({ defaultEdgeType: type })
    this.sigma.setGraph(gr)
  }

  private nodeDisplay(id: string, data: NodeAttrs) {
    const v = nodeView(id, { color: data.baseColor, label: data.label }, this.focus, this.state, this.palette.dim)
    const { forceLabel, ...rest } = v
    return { ...rest, ...(forceLabel ? { forceLabel: true } : {}) }
  }
  private edgeDisplay(edge: string, _data: EdgeAttrs) {
    const g = this.sigma.getGraph()
    return edgeView(g.source(edge), g.target(edge), this.focus, this.state, this.palette.edgeHi)
  }

  private setState(s: HighlightState, hoverOnly: boolean): void {
    this.state = s
    this.focus = this.adj ? focusSet(s, this.adj) : null
    this.sigma.refresh({ skipIndexation: true, schedule: true }) // only reducers changed: no spatial reindex
    if (hoverOnly) this.opts.onHover(s.hovered)
  }

  getSelected(): string | null { return this.state.selected }

  select(id: string | null): void {
    this.setState({ ...this.state, selected: id }, false)
    this.opts.onSelect(id)
  }

  /**
   * Theme change: node and edge colours and the label colour are baked into the graph at setData time (WebGL needs concrete colours, not var()),
   * so they must be rewritten here. V-09: the first draft only stored the palette, so after a light/dark switch the nodes kept the other theme's
   * colours (probed in Chrome: node 0 stayed #111111 after setPalette with a dark palette) until the next data load.
   */
  setPalette(p: Palette): void {
    this.palette = p
    const g = this.sigma.getGraph()
    g.updateEachNodeAttributes((_id, a) => { const color = p.viz[a.slot - 1] ?? p.dim; return { ...a, color, baseColor: color } })
    g.updateEachEdgeAttributes((_e, a) => ({ ...a, color: p.edge }))
    this.sigma.setSetting("labelColor", { color: p.label })
    this.sigma.refresh()
  }

  zoomIn(): Promise<void> { return this.sigma.getCamera().animatedZoom({ duration: this.opts.reducedMotion ? 0 : 200 }) }
  zoomOut(): Promise<void> { return this.sigma.getCamera().animatedUnzoom({ duration: this.opts.reducedMotion ? 0 : 200 }) }
  reset(): Promise<void> { return this.sigma.getCamera().animatedReset({ duration: this.opts.reducedMotion ? 0 : 200 }) }
  /** Keyboard / button panning, a fraction of the visible area (no drag required: WCAG 2.5.7). */
  pan(dx: number, dy: number): void {
    const cam = this.sigma.getCamera()
    const s = cam.getState()
    cam.setState({ x: s.x + dx * s.ratio, y: s.y + dy * s.ratio })
  }
  async focusOn(id: string): Promise<void> {
    const d = this.sigma.getNodeDisplayData(id)
    if (!d) return
    await this.sigma.getCamera().animate({ x: d.x, y: d.y, ratio: 0.4 }, { duration: this.opts.reducedMotion ? 0 : 300 })
  }
  resize(): void { this.sigma.resize() }
  kill(): void { this.container.removeEventListener("webglcontextlost", this.onLost, true); this.sigma.kill() }
}

/** WebGL probe used before offering the canvas (sigma throws on context creation failure). */
export function hasWebGL(doc: Document): boolean {
  try {
    const c = doc.createElement("canvas")
    const gl = c.getContext("webgl2") ?? c.getContext("webgl")
    gl?.getExtension("WEBGL_lose_context")?.loseContext() // the probe must not hold one of the browser's ~16 live contexts
    return gl !== null
  } catch {
    return false
  }
}
