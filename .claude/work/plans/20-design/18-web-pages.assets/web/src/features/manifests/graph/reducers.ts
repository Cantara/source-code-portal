// Pure display logic of the graph (hover / selection highlight). The sigma hook only forwards these results to nodeReducer / edgeReducer.
import type { Adjacency } from "./model"

export interface HighlightState { readonly hovered: string | null; readonly selected: string | null }

/** The focus node (selection wins over hover) and its direct neighbours in both directions. null = nothing highlighted. */
export function focusSet(s: HighlightState, adj: Adjacency): ReadonlySet<string> | null {
  const f = s.selected ?? s.hovered
  if (f === null || !adj.index.has(f)) return null
  const set = new Set<string>([f])
  for (const x of adj.out.get(f) ?? []) set.add(x)
  for (const x of adj.in.get(f) ?? []) set.add(x)
  return set
}

export interface NodeView { readonly color?: string; readonly label?: string | null; readonly hidden?: boolean; readonly zIndex?: number; readonly forceLabel?: boolean }
export interface EdgeView { readonly color?: string; readonly hidden?: boolean; readonly size?: number; readonly zIndex?: number }

export function nodeView(id: string, base: { color: string; label: string }, focus: ReadonlySet<string> | null, s: HighlightState, dim: string): NodeView {
  if (focus === null) return {}
  const isFocus = id === (s.selected ?? s.hovered)
  if (focus.has(id)) return { color: base.color, forceLabel: true, zIndex: isFocus ? 2 : 1 }
  return { color: dim, label: null, zIndex: 0 }
}

export function edgeView(from: string, to: string, focus: ReadonlySet<string> | null, s: HighlightState, hi: string): EdgeView {
  if (focus === null) return {}
  const f = s.selected ?? s.hovered
  if (from === f || to === f) return { color: hi, size: 2, zIndex: 1 }
  return { hidden: true }
}
