import type { Schemas } from "@/lib/api/client"

/** Element is one block of the landing page (design/layout.md), as the API serves it. */
export type Element = Schemas["LayoutElement"]
export type Kind = Element["kind"]

/** ProductsView is how the products element draws the groups: distilled cards or one sortable table. */
export type ProductsView = NonNullable<Element["view"]>

export const productsViews: ProductsView[] = ["cards", "table"]

/** viewCookie remembers the reader's choice of products view, read on the server as the page sizes are. */
export const viewCookie = "scp-view-products"

/** productsView is v when it names a view, else fallback. */
export function productsView(v: unknown, fallback: ProductsView): ProductsView {
  return productsViews.includes(v as ProductsView) ? (v as ProductsView) : fallback
}

/** rememberView keeps the reader's products view for a year. */
export function rememberView(view: ProductsView) {
  const secure = location.protocol === "https:" ? "; Secure" : ""
  document.cookie = `${viewCookie}=${view}; Path=/; Max-Age=31536000; SameSite=Lax${secure}`
}

/**
 * kinds are the elements the design mode offers, each with what it shows. Every element carries signals, not attributes:
 * exceptions instead of defaults, people's work apart from automation.
 */
export const kinds: { kind: Kind; label: string; about: string }[] = [
  {
    kind: "telemetry",
    label: "Telemetry",
    about: "The fleet at a glance: people, active repositories, automation, findings, sync.",
  },
  {
    kind: "fleet-map",
    label: "Fleet map",
    about: "Every repository as a tile by product, coloured by its last change by a person.",
  },
  { kind: "products", label: "Products", about: "The products as distilled cards or one sortable table." },
  { kind: "latest-commits", label: "Latest commits", about: "The newest commits by people; automation on request." },
  { kind: "heading", label: "Heading", about: "A heading and a line of text that divide the page." },
]

export const kindLabel = (k: Kind) => kinds.find((x) => x.kind === k)?.label ?? k

/** blank is a new element of kind with its usual options. */
export function blank(kind: Kind): Element {
  switch (kind) {
    case "products":
      return { kind, view: "cards" }
    case "latest-commits":
      return { kind, count: 10 }
    case "heading":
      return { kind, text: "A heading" }
    default:
      return { kind }
  }
}

/** defaultLayout is the landing page before its layout is read: the built-in one the API serves without a stored layout. */
export const defaultLayout: Element[] = [
  { kind: "telemetry" },
  { kind: "fleet-map" },
  { kind: "products", view: "cards" },
  { kind: "latest-commits", count: 10 },
]

/** pick is the groups an element selects (none: every group), in its order; ids no group has are left out. */
export function pick<G extends { id: string }>(all: G[], groups: string[] | undefined): G[] {
  if (!groups) return all
  const by = new Map(all.map((g) => [g.id, g]))
  return groups.flatMap((id) => by.get(id) ?? [])
}
