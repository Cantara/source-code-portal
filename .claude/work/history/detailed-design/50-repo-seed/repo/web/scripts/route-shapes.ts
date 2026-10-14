import { adminOnly, routes } from "../routes.ts"

type Node = { type: "root" | "route" | "index" | "layout"; path?: string; children?: Node[] }
export type Shape = { pattern: string; listeners: ("public" | "admin")[] }

/** TanStack path segment -> webapp (Go ServeMux) segment: `$id` -> `{id}`, a trailing `$` -> `{path...}`. */
export function toPattern(tsPath: string): string {
  const segs = tsPath
    .split("/")
    .filter(Boolean)
    .map((s) => (s === "$" ? "{path...}" : s.startsWith("$") ? `{${s.slice(1)}}` : s))
  return "/" + segs.join("/")
}

/** Every URL shape the SPA can render: leaves and index routes. Pure; the order is the tree order. */
export function shapes(): Shape[] {
  const out = new Map<string, Shape>()
  const walk = (n: Node, prefix: string) => {
    const here = n.type === "route" ? join(prefix, n.path ?? "") : prefix
    const kids = n.children ?? []
    if (n.type === "index" || (n.type === "route" && kids.length === 0)) add(here)
    for (const k of kids) walk(k, here)
  }
  const add = (tsPath: string) => {
    if (tsPath === "" || tsPath === "/") return // "/" is a server redirect, never listed (06 section 3.5)
    const pattern = toPattern(tsPath)
    out.set(pattern, { pattern, listeners: adminOnly.includes(tsPath) ? ["admin"] : ["public"] })
  }
  walk(routes as unknown as Node, "")
  return [...out.values()]
}
function join(a: string, b: string): string {
  return (a + "/" + b).replace(/\/+/g, "/").replace(/\/$/, "")
}
