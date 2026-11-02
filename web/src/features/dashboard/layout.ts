/** ProductsView is how the products element draws the groups: distilled cards or one sortable table. */
export type ProductsView = "cards" | "table"

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

/** Groups selects products: every group, or the listed group ids in that order. */
export type Groups = "all" | string[]

/**
 * Element is one block of the landing page (design/layout.md). Every element carries signals, not attributes: exceptions instead of
 * defaults, people's work apart from automation.
 */
export type Element =
  | { kind: "telemetry" }
  | { kind: "fleet-map"; groups: Groups }
  | { kind: "products"; groups: Groups; view: ProductsView }
  | { kind: "latest-commits"; count: number }
  | { kind: "heading"; text: string; note?: string }

/** defaultLayout is the landing page without a stored layout: the fleet at a glance, then the products, then people's commits. */
export const defaultLayout: Element[] = [
  { kind: "telemetry" },
  { kind: "fleet-map", groups: "all" },
  { kind: "products", groups: "all", view: "cards" },
  { kind: "latest-commits", count: 10 },
]

/** pick is the groups an element selects, in its order; ids no group has are left out. */
export function pick<G extends { id: string }>(all: G[], groups: Groups): G[] {
  if (groups === "all") return all
  const by = new Map(all.map((g) => [g.id, g]))
  return groups.flatMap((id) => by.get(id) ?? [])
}
