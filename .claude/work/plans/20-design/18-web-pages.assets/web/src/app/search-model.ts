// Pure model of the global search palette (Ctrl/Cmd+K). The dialog (cmdk + shadcn Command inside Dialog) only renders `rank()` output.
import { docSplat } from "@/features/docs" // through the slice index, as the import rule demands (V-07)
import type { components } from "@/lib/api/schema"
type RepoSummary = components["schemas"]["RepoSummary"]
type GroupSummary = components["schemas"]["GroupSummary"]
type PageSummary = components["schemas"]["PageSummary"]
type DocIndexEntry = components["schemas"]["DocIndexEntry"]
type ComponentSpread = components["schemas"]["ComponentSpread"]

export type NavTarget =
  | { readonly to: "/dashboard" } | { readonly to: "/commits" } | { readonly to: "/dependencies" } | { readonly to: "/status" }
  | { readonly to: "/groups/$groupId"; readonly params: { groupId: string } }
  | { readonly to: "/repos/$repo"; readonly params: { repo: string } }
  | { readonly to: "/repos/$repo/docs/$"; readonly params: { repo: string; _splat: string } }
  | { readonly to: "/pages/$slug"; readonly params: { slug: string } }
  | { readonly to: "/packages"; readonly search: { purl: string } }

export type ItemKind = "group" | "repo" | "doc" | "page" | "component" | "go"
export interface SearchItem { readonly key: string; readonly kind: ItemKind; readonly title: string; readonly hint: string; readonly haystack: readonly string[]; readonly target: NavTarget }

export const GROUP_ORDER: readonly ItemKind[] = ["go", "group", "repo", "doc", "page", "component"]
export const MAX_PER_KIND: Readonly<Record<ItemKind, number>> = { go: 4, group: 5, repo: 8, doc: 6, page: 4, component: 6 }

export const fromGroup = (g: GroupSummary): SearchItem => ({ key: `group:${g.id}`, kind: "group", title: g.displayName, hint: g.id, haystack: [g.id, g.displayName, g.description ?? ""], target: { to: "/groups/$groupId", params: { groupId: g.id } } })
export const fromRepo = (r: RepoSummary): SearchItem => ({ key: `repo:${r.slug}`, kind: "repo", title: r.title.value, hint: r.slug, haystack: [r.slug, r.title.value, r.summary?.value ?? "", ...r.tags.map((t) => t.value)], target: { to: "/repos/$repo", params: { repo: r.slug } } })
export const fromPage = (p: PageSummary): SearchItem => ({ key: `page:${p.slug}`, kind: "page", title: p.title, hint: p.slug, haystack: [p.slug, p.title, p.summary ?? ""], target: { to: "/pages/$slug", params: { slug: p.slug } } })
export const fromDoc = (repo: string, d: DocIndexEntry): SearchItem => ({
  key: `doc:${repo}:${d.id}`, kind: "doc", title: d.title, hint: `${repo} / ${d.path}`, haystack: [d.title, d.path, d.description ?? "", ...d.tags],
  target: d.id === "readme" ? { to: "/repos/$repo", params: { repo } } : { to: "/repos/$repo/docs/$", params: { repo, _splat: docSplat(d) } },
})
export const fromComponent = (c: ComponentSpread): SearchItem => ({ key: `component:${c.purl}`, kind: "component", title: c.namespace ? `${c.namespace}/${c.name}` : c.name, hint: `${c.ecosystem}, ${c.repos} repositories`, haystack: [c.name, c.namespace ?? ""], target: { to: "/packages", search: { purl: c.purl } } })

/** 0 = no match. Tiers: exact 100, prefix 80, word prefix 60, substring 40, in-order subsequence of the first (identifier) field 10. */
export function score(query: string, haystack: readonly string[]): number {
  const q = query.trim().toLowerCase()
  if (q === "") return 1
  let best = 0
  haystack.forEach((raw, field) => {
    const h = raw.toLowerCase()
    if (h === "") return
    let s = 0
    if (h === q) s = 100
    else if (h.startsWith(q)) s = 80
    else if (h.split(/[^a-z0-9]+/).some((w) => w.startsWith(q))) s = 60
    else if (h.includes(q)) s = 40
    else if (field === 0 && subsequence(q, h)) s = 10
    if (s > 0) best = Math.max(best, s - field * 0.5) // an identifier hit beats the same hit in a description
  })
  return best
}
function subsequence(q: string, h: string): boolean {
  let i = 0
  for (const ch of h) if (ch === q[i] && ++i === q.length) return true
  return false
}

export interface Ranked { readonly kind: ItemKind; readonly items: readonly SearchItem[] }

/** Grouped, capped, deterministic (score desc, title asc, key asc). Empty query returns only the `go` shortcuts and the given recents. */
export function rank(query: string, items: readonly SearchItem[], recent: readonly SearchItem[] = []): Ranked[] {
  const q = query.trim()
  const pool = q === "" ? [...items.filter((i) => i.kind === "go"), ...recent] : items
  const scored = pool.map((i) => ({ i, s: score(q, i.haystack) })).filter((x) => x.s > 0)
  scored.sort((a, b) => b.s - a.s || a.i.title.localeCompare(b.i.title) || (a.i.key < b.i.key ? -1 : 1))
  const out: Ranked[] = []
  for (const kind of GROUP_ORDER) {
    const its = scored.filter((x) => x.i.kind === kind).slice(0, MAX_PER_KIND[kind]).map((x) => x.i)
    if (its.length > 0) out.push({ kind, items: its })
  }
  return out
}

/** Most-recently-used list (persisted in localStorage by the shell, try/catch around every access): newest first, unique by key. */
export function pushRecent(list: readonly string[], key: string, max = 5): string[] {
  return [key, ...list.filter((k) => k !== key)].slice(0, max)
}
