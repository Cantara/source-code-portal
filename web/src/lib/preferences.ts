import type { Schemas } from "./api/client"

/**
 * The reader's preferences, kept in the portal's own cookies (`scp-*`) so the server renders the first page with them (Ctx.cookies)
 * and nothing is stored per person (P-38, P-42). The settings page sets them all; a few are also set where they are used.
 */

/** remember keeps a preference cookie for a year. */
export function remember(name: string, value: string) {
  const secure = location.protocol === "https:" ? "; Secure" : ""
  document.cookie = `${name}=${encodeURIComponent(value)}; Path=/; Max-Age=31536000; SameSite=Lax${secure}`
}

/** forget drops a preference cookie: the portal's default applies again. */
export function forget(name: string) {
  document.cookie = `${name}=; Path=/; Max-Age=0; SameSite=Lax`
}

/** Work is the work that counts, the reader's lens on people's work: all of it, by hand only, or with an AI only. */
export type Work = Schemas["Work"]

export const workCookie = "scp-work"

export const works: { value: Work; label: string; about: string }[] = [
  { value: "all", label: "Both", about: "Every commit by the team, by hand and with AI. Automation is counted apart." },
  { value: "hand", label: "By hand", about: "Only commits with no trace of an AI." },
  {
    value: "ai",
    label: "With AI",
    about: "Only commits with an AI co-author, or by a coding agent. Some tools leave no trace, so this is a lower bound.",
  },
]

/** workOf is v when it names a lens, else all. */
export function workOf(v: unknown): Work {
  return v === "hand" || v === "ai" ? v : "all"
}

/** by is how a sentence names the work that counts: "Last change by the team", "by hand", "with AI". */
export const by: Record<Work, string> = { all: "by the team", hand: "by hand", ai: "with AI" }

/** ProductsView is how the products element draws the groups: distilled cards or one sortable table. */
export type ProductsView = "cards" | "table"

export const productsViews: ProductsView[] = ["cards", "table"]

/** viewCookie remembers the reader's choice of products view. */
export const viewCookie = "scp-view-products"

/** productsView is v when it names a view, else fallback. */
export function productsView(v: unknown, fallback: ProductsView): ProductsView {
  return productsViews.includes(v as ProductsView) ? (v as ProductsView) : fallback
}
