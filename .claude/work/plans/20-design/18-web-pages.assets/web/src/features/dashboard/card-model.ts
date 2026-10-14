import type { components } from "@/lib/api/schema"
type GroupSummary = components["schemas"]["GroupSummary"]
type Badge = components["schemas"]["Badge"]

export type CardState =
  | { readonly kind: "ok"; readonly headline: string; readonly blurb: string | null }
  | { readonly kind: "degraded"; readonly reason: "missing" | "unset"; readonly blurb: string | null }

/**
 * A group whose default repository is missing or unset still gets a card (GR-29: absence is a typed state, never a silently dropped card).
 * The configured description wins over the GitHub description of the default repository (GR-12: configuration is authoritative).
 */
export function cardState(g: GroupSummary): CardState {
  const blurb = g.description ?? g.defaultRepo?.description ?? null
  if (g.defaultRepoState === "ok" && g.defaultRepo) return { kind: "ok", headline: g.defaultRepo.title ?? g.defaultRepo.name, blurb }
  return { kind: "degraded", reason: g.defaultRepoState === "unset" ? "unset" : "missing", blurb }
}

/** The repository-count badge first, then the others in API order; at most 5 shown, the rest behind the group page. */
export function cardBadges(g: GroupSummary, max = 5): Badge[] {
  const repos = g.badges.filter((b) => b.kind === "repos")
  const rest = g.badges.filter((b) => b.kind !== "repos")
  return [...repos, ...rest].slice(0, max)
}

export function matchesGroup(g: GroupSummary, q: string | undefined): boolean {
  const n = (q ?? "").trim().toLowerCase()
  if (n === "") return true
  return [g.id, g.displayName, g.description ?? "", g.defaultRepo?.title ?? "", g.defaultRepo?.name ?? ""].some((s) => s.toLowerCase().includes(n))
}
