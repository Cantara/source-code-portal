import type { Schemas } from "@/lib/api/client"
import { by, has, isTeam, type Work } from "@/lib/preferences"

type Activity = Schemas["Activity"]

/** maintenance is the commits by automation and of sweeps in the last 30 days. */
export const maintenance = (a: Activity) => a.automation + a.sweeps

/** counted is the work that counts of an activity, in the last 30 days. */
export function counted(a: Activity, w: Work): number {
  return (has(w, "hand") ? a.hand : 0) + (has(w, "ai") ? a.ai : 0) + (has(w, "automation") ? maintenance(a) : 0)
}

/** noun names the counted commits: "commits" for people's work (with automation or not), else "commits by hand", "with AI", .... */
export function noun(n: number, w: Work): string {
  const c = n === 1 ? "commit" : "commits"
  return has(w, "hand") && has(w, "ai") ? c : `${c} ${by(w)}`
}

/** Part is one kind's weekly counts in a stacked sparkline, with its fill. */
export type Part = { values: number[]; fill: string }

/** fills are the colours the kinds are drawn in, here and in the text's dots. */
export const fills = { hand: "currentColor", ai: "var(--ai)", automation: "var(--muted-foreground)" } as const

/** series is what a sparkline stacks for the lens, the foot first: with AI, by hand, by automation, each when it counts. */
export function series(a: Activity, w: Work): Part[] {
  const parts: Part[] = []
  if (has(w, "ai")) parts.push({ values: a.weeklyAi, fill: fills.ai })
  if (has(w, "hand")) parts.push({ values: a.weeklyHand, fill: fills.hand })
  if (has(w, "automation")) parts.push({ values: a.weeklyAutomation, fill: fills.automation })
  return parts
}

/** weekly is a sparkline's label: what the bars count, by week. */
export function weekly(a: Activity, w: Work): string {
  const head = `Commits ${isTeam(w) ? "" : `${by(w)} `}per week, the last 12 weeks, the oldest first:`
  const parts = [
    has(w, "hand") && `by hand ${a.weeklyHand.join(", ")}`,
    has(w, "ai") && `with AI ${a.weeklyAi.join(", ")}`,
    has(w, "automation") && `by automation ${a.weeklyAutomation.join(", ")}`,
  ].filter(Boolean)
  return `${head} ${parts.join("; ")}`
}

/** share is the part of the team's commits made with an AI, as a percentage (-1 without commits). */
export function share(a: Activity): number {
  const all = a.hand + a.ai
  return all === 0 ? -1 : Math.round((a.ai / all) * 100)
}

/** assistants is " (Claude, Copilot)", or "" without any. */
export const assistants = (a: Activity) => (a.assistants.length > 0 ? ` (${a.assistants.join(", ")})` : "")
