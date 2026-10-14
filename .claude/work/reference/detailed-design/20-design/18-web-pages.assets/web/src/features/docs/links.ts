import type { components } from "@/lib/api/schema"
type DocLink = components["schemas"]["DocLink"]
type LinkKind = DocLink["kind"]

/** href exactly as it appears in the sanitised HTML -> kind from `Doc.links`. First occurrence wins. */
export function buildLinkIndex(links: readonly DocLink[]): ReadonlyMap<string, LinkKind> {
  const m = new Map<string, LinkKind>()
  for (const l of links) if (!m.has(l.href)) m.set(l.href, l.kind)
  return m
}

export interface ClickInfo { readonly button: number; readonly ctrl: boolean; readonly meta: boolean; readonly shift: boolean; readonly alt: boolean; readonly target: string | null; readonly download: boolean }

export type ClickDecision =
  | { readonly action: "navigate"; readonly href: string }       // router.navigate({ href })
  | { readonly action: "scroll"; readonly id: string; readonly rawId: string } // in-page anchor: scroll + focus the heading (`id` percent-decoded when valid, `rawId` as written)
  | { readonly action: "native" }                                 // let the browser do it (external, media, modifier keys)

/** `decodeURIComponent` throws URIError on a lone `%` (a heading "100%" or an author-written `#50%`): a click handler must never throw (V-01). */
export function safeDecode(s: string): string {
  try { return decodeURIComponent(s) } catch { return s }
}

/** Delegated click policy of DocContent. Pure: the component only wires DOM events to it. */
export function decideClick(href: string | null, e: ClickInfo, index: ReadonlyMap<string, LinkKind>): ClickDecision {
  if (href === null) return { action: "native" }
  if (e.button !== 0 || e.ctrl || e.meta || e.shift || e.alt || e.download || (e.target !== null && e.target !== "" && e.target !== "_self")) return { action: "native" }
  const kind = index.get(href)
  if (kind === "anchor" && href.startsWith("#") && href.length > 1) { const rawId = href.slice(1); return { action: "scroll", id: safeDecode(rawId), rawId } }
  if (kind === "doc" && href.startsWith("/") && !href.startsWith("//")) return { action: "navigate", href }
  return { action: "native" }
}
