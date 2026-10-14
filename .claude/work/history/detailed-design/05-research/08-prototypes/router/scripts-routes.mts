import { routes } from "./routes.ts"
type N = { type: string; path?: string; file?: string; children?: N[]; id?: string }
const out: string[] = []
const walk = (n: N, prefix: string) => {
  const seg = n.type === "route" ? (n.path ?? "") : ""
  const full = (prefix + (seg.startsWith("/") ? seg : seg ? "/" + seg : "")) || "/"
  if (n.type === "index") out.push(prefix || "/")
  else if ((n.type === "route" || n.type === "root") && !n.children?.length) out.push(full)
  for (const c of n.children ?? []) walk(c, n.type === "route" ? full.replace(/\/$/, "") : prefix)
}
walk(routes as unknown as N, "")
console.log(JSON.stringify([...new Set(out)], null, 0))
