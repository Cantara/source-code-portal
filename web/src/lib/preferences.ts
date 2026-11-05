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

/** every is each kind of work, by hand, with AI and by automation: the default. */
export const every: Work = ["hand", "ai", "automation"]

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

/** workOf is the kinds v names (a cookie's "hand,ai"), once each and in order; every kind when it names none. */
export function workOf(v: unknown): Work {
  const named = typeof v === "string" ? v.split(",") : Array.isArray(v) ? v : []
  const w = kinds.map((k) => k.value).filter((k) => named.includes(k))
  return w.length > 0 ? w : every
}

/** has reports whether the lens counts kind k. */
export const has = (w: Work, k: WorkKind) => w.includes(k)

/** isEvery reports whether the lens counts every kind, the default: a label then needs no qualifier. */
export const isEvery = (w: Work) => w.length === kinds.length

/** qualifier is the lens as the end of a label, " by hand": nothing under the default, every kind, where the label needs none. */
export const qualifier = (w: Work) => (isEvery(w) ? "" : ` ${by(w)}`)

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

/** columnCookie remembers the presentation page's commit column: its width in percent of the screen (design/present.md). */
export const columnCookie = "scp-present-column"
export const columnWidths = { min: 20, max: 60, step: 5, initial: 30 } as const

/** columnOf is the column's width a cookie names, on the 5% grid within 20% to 60%; the initial width otherwise. */
export function columnOf(v: unknown): number {
  const n = typeof v === "string" && /^\d{1,3}$/.test(v) ? Number(v) : NaN
  if (Number.isNaN(n)) return columnWidths.initial
  const stepped = Math.round(n / columnWidths.step) * columnWidths.step
  return Math.min(Math.max(stepped, columnWidths.min), columnWidths.max)
}

/** Side is the dashboard's side panel of the latest commits beside the products: open or folded to a rail, and its width (rem). */
export type Side = { open: boolean; width: number }

export const sideCookie = "scp-side-commits"
export const sideWidths = { min: 18, max: 40, initial: 26 } as const

/** sideOf reads the panel's cookie ("open:26", "closed:30"); anything else is open at the initial width. */
export function sideOf(v: unknown): Side {
  const m = typeof v === "string" ? /^(open|closed):(\d{1,2})$/.exec(v) : null
  const width = m ? Math.min(Math.max(Number(m[2]), sideWidths.min), sideWidths.max) : sideWidths.initial
  return { open: m?.[1] !== "closed", width }
}

/** sideValue is the panel's cookie value. */
export function sideValue(s: Side): string {
  return `${s.open ? "open" : "closed"}:${s.width}`
}
