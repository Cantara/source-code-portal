import type { Schemas } from "@/lib/api/client"
import type { Work } from "@/lib/preferences"

type Activity = Schemas["Activity"]

/** counted is the work that counts of an activity, in the last 30 days. */
export function counted(a: Activity, w: Work): number {
  return w === "hand" ? a.hand : w === "ai" ? a.ai : a.hand + a.ai
}

/** noun names the counted commits: "commits", "commits by hand", "commits with AI". */
export function noun(n: number, w: Work): string {
  const c = n === 1 ? "commit" : "commits"
  return w === "hand" ? `${c} by hand` : w === "ai" ? `${c} with AI` : c
}

/** series is what a sparkline draws for the lens: both stacked (with AI at the foot), or the one the lens counts. */
export function series(a: Activity, w: Work): { values: number[]; part?: number[]; ai?: boolean } {
  if (w === "hand") return { values: a.weeklyHand }
  if (w === "ai") return { values: a.weeklyAi, ai: true }
  return { values: a.weeklyHand.map((n, i) => n + (a.weeklyAi[i] ?? 0)), part: a.weeklyAi }
}

/** weekly is a sparkline's label: what the bars count, by week. */
export function weekly(a: Activity, w: Work): string {
  const head = "the last 12 weeks, the oldest first"
  if (w === "hand") return `Commits by hand per week, ${head}: ${a.weeklyHand.join(", ")}`
  if (w === "ai") return `Commits with AI per week, ${head}: ${a.weeklyAi.join(", ")}`
  return `Commits per week, ${head}: by hand ${a.weeklyHand.join(", ")}; with AI ${a.weeklyAi.join(", ")}`
}

/** share is the part of the team's commits made with an AI, as a percentage (-1 without commits). */
export function share(a: Activity): number {
  const all = a.hand + a.ai
  return all === 0 ? -1 : Math.round((a.ai / all) * 100)
}

/** assistants is " (Claude, Copilot)", or "" without any. */
export const assistants = (a: Activity) => (a.assistants.length > 0 ? ` (${a.assistants.join(", ")})` : "")
