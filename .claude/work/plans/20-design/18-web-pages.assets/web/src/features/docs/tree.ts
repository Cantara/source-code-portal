import type { components } from "@/lib/api/schema"
type DocIndexEntry = components["schemas"]["DocIndexEntry"]

export type DocNode =
  | { readonly kind: "doc"; readonly entry: DocIndexEntry }
  | { readonly kind: "section"; readonly title: string; readonly children: DocNode[] }

/**
 * Builds the navigation tree from the index in API order (docs.nav, then order, then path; unit 10 `listRepoDocs`: "index order, README first").
 * It does not re-sort; it only moves the README first (OQ-18-02: the contract does not promise the order). Sections are the `section` title paths.
 */
export function buildDocTree(entries: readonly DocIndexEntry[]): DocNode[] {
  const root: DocNode[] = []
  for (const e of [...entries.filter((x) => x.id === "readme"), ...entries.filter((x) => x.id !== "readme")]) {
    let level = root
    for (const title of e.section) {
      let sec = level.find((n): n is Extract<DocNode, { kind: "section" }> => n.kind === "section" && n.title === title)
      if (!sec) {
        sec = { kind: "section", title, children: [] }
        level.push(sec)
      }
      level = sec.children
    }
    level.push({ kind: "doc", entry: e })
  }
  return root
}

/** Flat, in-order list of leaf documents: previous / next links at the bottom of a page. */
export function flattenDocs(tree: readonly DocNode[]): DocIndexEntry[] {
  const out: DocIndexEntry[] = []
  const walk = (ns: readonly DocNode[]) => { for (const n of ns) n.kind === "doc" ? out.push(n.entry) : walk(n.children) }
  walk(tree)
  return out
}

/** Titles of the section paths that contain `id`, outermost first, so the nav opens exactly the path to the current document. */
export function sectionsContaining(entries: readonly DocIndexEntry[], id: string): string[][] {
  const e = entries.find((x) => x.id === id)
  if (!e) return []
  return e.section.map((_, i) => e.section.slice(0, i + 1))
}

export function filterDocs(entries: readonly DocIndexEntry[], q: string): DocIndexEntry[] {
  const n = q.trim().toLowerCase()
  if (n === "") return [...entries]
  return entries.filter((e) => e.title.toLowerCase().includes(n) || (e.description ?? "").toLowerCase().includes(n) || e.tags.some((t) => t.toLowerCase().includes(n)) || e.path.toLowerCase().includes(n))
}

/** Target of the "Documentation" tab: the first document that is not the README (V-14). undefined = only the README exists, the tab is not shown. */
export function docsEntry(entries: readonly DocIndexEntry[]): DocIndexEntry | undefined {
  return flattenDocs(buildDocTree(entries)).find((e) => e.id !== "readme")
}
