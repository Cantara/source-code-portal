import type { Schemas } from "@/lib/api/client"

/** Element is one block of the landing page (design/layout.md), as the API serves it. */
export type Element = Schemas["LayoutElement"]
export type Kind = Element["kind"]

export type { ProductsView } from "@/lib/preferences"

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
    about: "Every repository as a tile by product, coloured by its last change or by its health.",
  },
  {
    kind: "announcements",
    label: "Announcements",
    about: "The team's announcements for this page while they last, written in Admin; nothing when there are none.",
  },
  { kind: "products", label: "Products", about: "The products as distilled cards, one sortable table or compact tiles." },
  {
    kind: "pulls",
    label: "Pull requests",
    about: "The open pull requests whose turn it is first, the ones waiting for a review at the top; all of them on /pulls.",
  },
  {
    kind: "issues",
    label: "Issues",
    about: "The open issues that wait first, the ones needing triage at the top; all of them on /issues.",
  },
  {
    kind: "latest-commits",
    label: "Latest commits",
    about: "The newest commits of the work that counts; automation on request.",
  },
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
    case "pulls":
    case "issues":
      return { kind, count: 5 }
    case "heading":
      return { kind, text: "A heading" }
    default:
      return { kind }
  }
}

/** defaultLayout is the landing page before its layout is read: the built-in one the API serves without a stored layout. */
export const defaultLayout: Element[] = [
  { kind: "announcements" },
  { kind: "telemetry" },
  { kind: "fleet-map" },
  { kind: "products", view: "cards" },
  { kind: "latest-commits", count: 10 },
  { kind: "pulls", count: 5 },
  { kind: "issues", count: 5 },
]

/** defaultPresent is the presentation page before its layout is read (design/present.md): the API's built-in one. */
export const defaultPresent: Element[] = [
  { kind: "telemetry" },
  { kind: "announcements" },
  { kind: "fleet-map", color: "health" },
  { kind: "products", view: "tiles" },
  { kind: "latest-commits", count: 30 },
  { kind: "pulls", count: 8 },
  { kind: "issues", count: 8 },
]

/** pick is the groups an element selects (none: every group), in its order; ids no group has are left out. */
export function pick<G extends { id: string }>(all: G[], groups: string[] | undefined): G[] {
  if (!groups) return all
  const by = new Map(all.map((g) => [g.id, g]))
  return groups.flatMap((id) => by.get(id) ?? [])
}
