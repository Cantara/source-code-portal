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

/**
 * Work is the work that counts, the reader's lens: one or more kinds. People's work is by hand or with AI; automation (CI,
 * dependency and release bots, and sweeps) is maintenance. The dashboard counts people's work until the reader chooses (P-43).
 */
export type Work = Schemas["Work"]
export type WorkKind = Schemas["WorkKind"]

export const workCookie = "scp-work"

/** team is people's work, by hand and with AI: the default. */
export const team: Work = ["hand", "ai"]

export const kinds: { value: WorkKind; label: string; about: string }[] = [
  { value: "hand", label: "By hand", about: "People's commits with no trace of an AI." },
  {
    value: "ai",
    label: "With AI",
    about: "People's commits with an AI co-author, and a coding agent's. Some tools leave no trace, so this is a lower bound.",
  },
  {
    value: "automation",
    label: "By automation",
    about: "Maintenance: CI, dependency and release bots, and sweeps (one change landing in more than 10 repositories on a day).",
  },
]

/** workOf is the kinds v names (a cookie's "hand,ai"), once each and in order; people's work when it names none. */
export function workOf(v: unknown): Work {
  const named = typeof v === "string" ? v.split(",") : Array.isArray(v) ? v : []
  const w = kinds.map((k) => k.value).filter((k) => named.includes(k))
  return w.length > 0 ? w : team
}

/** has reports whether the lens counts kind k. */
export const has = (w: Work, k: WorkKind) => w.includes(k)

/** isTeam reports whether the lens is people's work exactly, by hand and with AI. */
export const isTeam = (w: Work) => w.length === 2 && has(w, "hand") && has(w, "ai")

const named: Record<WorkKind, string> = { hand: "by hand", ai: "with AI", automation: "by automation" }

/**
 * by is how a sentence names the work that counts: "Last change by the team", "by hand", "with AI", "by automation", "by hand or
 * automation", "by the team or automation".
 */
export function by(w: Work): string {
  const people = has(w, "hand") && has(w, "ai") ? ["by the team"] : w.filter((k) => k !== "automation").map((k) => named[k])
  if (!has(w, "automation")) return people.join(" or ")
  return people.length === 0 ? named.automation : `${people.join(" or ")} or automation`
}

/** ProductsView is how the products element draws the groups: distilled cards or one sortable table. */
export type ProductsView = "cards" | "table"

export const productsViews: ProductsView[] = ["cards", "table"]

/** viewCookie remembers the reader's choice of products view. */
export const viewCookie = "scp-view-products"

/** productsView is v when it names a view, else fallback. */
export function productsView(v: unknown, fallback: ProductsView): ProductsView {
  return productsViews.includes(v as ProductsView) ? (v as ProductsView) : fallback
}
