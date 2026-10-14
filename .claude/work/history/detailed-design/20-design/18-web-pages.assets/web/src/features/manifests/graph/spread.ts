import type { components } from "@/lib/api/schema"
type VersionCount = components["schemas"]["VersionCount"]
export interface Segment { readonly version: string; readonly units: number; readonly repos: number; readonly share: number; readonly slot: number }

/** Segments of the version-spread bar: the 5 most used versions get viz-1..5, the rest are merged into "other" (viz-8). Shares sum to 1. */
export function spreadSegments(versions: readonly VersionCount[], keep = 5): Segment[] {
  const total = versions.reduce((a, v) => a + v.units, 0)
  if (total === 0) return []
  const sorted = [...versions].sort((a, b) => b.units - a.units || a.version.localeCompare(b.version))
  const head = sorted.slice(0, keep).map((v, i) => ({ version: v.version, units: v.units, repos: v.repos, share: v.units / total, slot: i + 1 }))
  const rest = sorted.slice(keep)
  if (rest.length > 0) {
    const u = rest.reduce((a, v) => a + v.units, 0)
    head.push({ version: "other", units: u, repos: rest.reduce((a, v) => a + v.repos, 0), share: u / total, slot: 8 })
  }
  return head
}
