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

/**
 * commitsCookie remembers which kinds of work the latest commits show (the dashboard's panel and the presentation page's column);
 * without it they show the work that counts.
 */
export const commitsCookie = "scp-latest-work"

/** commitsOf is the kinds a cookie names, or none: the latest commits follow the work that counts. */
export function commitsOf(v: unknown): Work | undefined {
  return typeof v === "string" && v !== "" ? workOf(v) : undefined
}

/** sameWork reports whether two lenses count the same kinds. */
export const sameWork = (a: Work, b: Work) => a.length === b.length && a.every((k) => b.includes(k))

/**
 * Profile is the kind of screen the presentation page is on, named for where it sits (design/present.md, P-64): a tablet held
 * upright or on its side, a big monitor at a desk (the commit log beside the fleet), a screen on a wall read from across a room.
 * The page tells the first three from the window (profileOf); a wall has the pixels of a desk, so a screen is told it is one.
 */
export type Profile = "handheld-portrait" | "handheld-landscape" | "desk" | "wall"

export const profiles: { value: Profile; label: string }[] = [
  { value: "handheld-portrait", label: "Handheld portrait" },
  { value: "handheld-landscape", label: "Handheld landscape" },
  { value: "desk", label: "Desk" },
  { value: "wall", label: "Wall" },
]

/** deskFrom is the width (CSS pixels) from which a screen on its side is a desk's monitor: wider than the largest tablet. */
export const deskFrom = 1600

/** handheldUpTo is the width below which an upright screen is a tablet held upright (an iPad Pro 13" is 1032 wide). */
export const handheldUpTo = 1100

/**
 * profileOf is the profile of a window of width by height: a wall when the screen was told it is one; an upright window wider than
 * a tablet is a desk's (half a 27" monitor, 1280 by 1328), split as a desk is, whichever way it stands.
 */
export function profileOf(width: number, height: number, wall: boolean): Profile {
  if (wall) return "wall"
  if (height > width) return width < handheldUpTo ? "handheld-portrait" : "desk"
  return width >= deskFrom ? "desk" : "handheld-landscape"
}

/** profileCookie remembers the last profile the page found, so the server renders that profile's column at once. */
export const profileCookie = "scp-present-profile"

/** profileNamed is the profile a cookie names, or none. */
export function profileNamed(v: unknown): Profile | undefined {
  return profiles.find((p) => p.value === v)?.value
}

/** wallCookie remembers that this screen is on a wall (`?profile=wall`, or the tap bar); `?profile=auto` forgets it. */
export const wallCookie = "scp-present-wall"

/**
 * columnCookie remembers the presentation page's commit column per profile, its width in percent of the screen
 * ("handheld-landscape:30,desk:60"): a width dragged on the desk's monitor leaves the tablet's alone.
 */
export const columnCookie = "scp-present-column"
export const columnWidths = { min: 20, max: 60, step: 5 } as const

/** Columns are the commit column's width per profile. */
export type Columns = Record<Profile, number>

/** columnDefaults are each profile's width until it is dragged: the commit log takes most of a desk, a third elsewhere. */
export const columnDefaults: Columns = { "handheld-portrait": 30, "handheld-landscape": 30, desk: 60, wall: 30 }

/** columnOf is a width on the 5% grid within 20% to 60%; fallback when it is no number. */
export function columnOf(v: unknown, fallback: number): number {
  const n = typeof v === "string" && /^\d{1,3}$/.test(v) ? Number(v) : NaN
  if (Number.isNaN(n)) return fallback
  const stepped = Math.round(n / columnWidths.step) * columnWidths.step
  return Math.min(Math.max(stepped, columnWidths.min), columnWidths.max)
}

/** columnsOf reads the cookie's widths by profile; a profile it does not name keeps its default. */
export function columnsOf(v: unknown): Columns {
  const out = { ...columnDefaults }
  for (const part of typeof v === "string" ? v.split(",") : []) {
    const [name, width] = part.split(":")
    const p = profileNamed(name)
    if (p) out[p] = columnOf(width, columnDefaults[p])
  }
  return out
}

/** columnsValue is the cookie's value: only the widths that differ from their defaults. */
export const columnsValue = (c: Columns) =>
  profiles
    .filter((p) => c[p.value] !== columnDefaults[p.value])
    .map((p) => `${p.value}:${c[p.value]}`)
    .join(",")

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
