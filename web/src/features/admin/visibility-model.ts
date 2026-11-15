// The model of who sees what (design/visibility.md, design/config-baseline.md): what the admin's list reads, and the pure rules that
// say an item's state, where it comes from and what a change does.

/** Audience is who sees an item (design/visibility.md). */
export type Audience = "everyone" | "signed-in" | "no-one"
export type Setting = { audience: Audience; search: boolean }
export type Rule = Setting & { pattern: string }
/** Doc is the stored choices: on products by id, on repositories by address, and the rules. */
export type Doc = { groups: Record<string, Setting>; repos: Record<string, Setting>; rules: Rule[] }
export type Effective = {
  audience: Audience
  search: boolean
  why: "repository" | "rule" | "product" | "default"
  rule: number
  product: string
  /** GitHub's private flag lowered everyone to signed in. */
  capped: boolean
}
export type Item = Effective & { address: string; private: boolean; fork: boolean; setting: Setting | null }
export type Node = Effective & { id: string; name: string; setting: Setting | null; repos: Item[] }

/** Visibility is what `GET /api/v1/admin/visibility` answers, outside the contract (design/visibility.md). */
export type Visibility = Doc & {
  /** scp.yaml's `visibility:`: what the store's changes are made against (design/config-baseline.md). */
  configured: Doc
  tree: Node[]
  ungrouped: Item[]
  /** Addresses the choices name that the catalogue does not hold: renamed, deleted or not listed yet. */
  missing: string[]
  ruleMatches: number[]
  counts: { everyone: number; signedIn: number; noOne: number; searchOff: number; private: number }
  revision: string
  writable: boolean
  problem: string
}

export const audiences: { value: Audience; label: string; on: string }[] = [
  { value: "everyone", label: "Everyone", on: "bg-sky-100 text-sky-950 dark:bg-sky-950 dark:text-sky-100" },
  { value: "signed-in", label: "Signed in", on: "bg-amber-100 text-amber-950 dark:bg-amber-950 dark:text-amber-100" },
  { value: "no-one", label: "No one", on: "bg-rose-100 text-rose-950 dark:bg-rose-950 dark:text-rose-100" },
]

/** describe is an effective state in words. */
export function describe(e: { audience: Audience; search: boolean }): string {
  if (e.audience === "everyone") return e.search ? "Everyone, offered to search engines" : "Everyone, kept from search engines"
  return e.audience === "signed-in" ? "Signed in only" : "No one"
}

const alike = (a: Setting | undefined | null, b: Setting | undefined | null) =>
  !!a && !!b && a.audience === b.audience && a.search === b.search

/** differs reports whether an item's own choice is not what scp.yaml has for it. */
export function differs(own: Setting | null, file: Setting | undefined): boolean {
  return own ? !alike(own, file) : !!file
}

/** sameRules reports whether two lists of rules are the same. */
export function sameRules(a: Rule[], b: Rule[]): boolean {
  return a.length === b.length && a.every((r, i) => r.pattern === b[i]?.pattern && alike(r, b[i]))
}

/** origin says where an item's choice is kept: scp.yaml, or changed here (kept in the store until the file has it). */
export function origin(own: Setting | null, file: Setting | undefined, why: string): string {
  if (!differs(own, file)) return own ? "set in scp.yaml" : why
  if (!file) return own ? "set here" : why
  return `changed here; scp.yaml: ${describe(file)}`
}

/** whyOf says where an item's audience comes from. */
export function whyOf(e: Effective, rules: Rule[]): string {
  const base = (() => {
    switch (e.why) {
      case "repository":
        return "set here"
      case "rule":
        return `rule ${rules[e.rule]?.pattern ?? ""}`.trim()
      case "product":
        return e.product ? "from its product" : "set here"
      default:
        return "default"
    }
  })()
  return e.capped ? `${base}; private on GitHub` : base
}

/** matches says whether a text contains the filter's words, each of them. */
export function matches(text: string, filter: string): boolean {
  const hay = text.toLowerCase()
  return filter
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((w) => hay.includes(w))
}

/** Change is one item whose result a save changes. */
export type Change = {
  key: string
  name: string
  product: boolean
  from: Effective
  to: Effective
  effect: string
  back: boolean
}

const same = (a: Effective, b: Effective) => a.audience === b.audience && a.search === b.search

/** effect says what a change does, in the reader's words. */
function effect(from: Effective, to: Effective): string {
  if (to.audience === "no-one") return "Hidden from everyone, you included. The portal still reads it from GitHub."
  if (to.audience === "signed-in")
    return from.audience === "everyone"
      ? "Visitors no longer see it, and it leaves the sitemap. Signed-in viewers still do."
      : "Signed-in viewers see it again; visitors do not."
  if (from.audience !== "everyone")
    return to.search
      ? "Visitors see it again, and search engines are offered it."
      : "Visitors see it again; search engines are not offered it."
  return to.search ? "Back in the sitemap: search engines are offered it." : "Leaves the sitemap; its pages say noindex."
}

/** changes lists what a save of the preview changes against what is saved: products first, then repositories, each once. */
export function changes(saved: Visibility, preview: Visibility): Change[] {
  const out: Change[] = []
  const savedGroups = new Map(saved.tree.map((g) => [g.id, g]))
  for (const g of preview.tree) {
    const before = savedGroups.get(g.id)
    if (before && !same(before, g)) {
      out.push({
        key: "g:" + g.id,
        name: g.name,
        product: true,
        from: before,
        to: g,
        effect: effect(before, g),
        back: comesBack(before, g),
      })
    }
  }
  const all = (v: Visibility) => new Map([...v.tree.flatMap((g) => g.repos), ...v.ungrouped].map((r) => [r.address, r]))
  const before = all(saved)
  for (const [address, r] of [...all(preview)].sort(([a], [b]) => a.localeCompare(b))) {
    const b = before.get(address)
    if (b && !same(b, r)) {
      out.push({
        key: "r:" + address,
        name: address,
        product: false,
        from: b,
        to: r,
        effect: effect(b, r),
        back: comesBack(b, r),
      })
    }
  }
  return out
}

/** comesBack is whether a change brings an item back into the public view or the sitemap. */
const comesBack = (from: Effective, to: Effective) =>
  to.audience === "everyone" && (from.audience !== "everyone" || (to.search && !from.search))

/** The number of items coming back at once that the review warns about in words. */
export const warnAt = 5

export type Filter = "all" | "everyone" | "signed-in" | "no-one" | "private" | "search-off" | "changed"

export const clone = (d: Doc): Doc => ({ groups: { ...d.groups }, repos: { ...d.repos }, rules: d.rules.map((r) => ({ ...r })) })
