import type { Repo } from "@/lib/api/client"

const day = 24 * 3600 * 1000

/** Activity is how recently a repository changed: by its newest commit, else its last push. */
export type Activity = "active" | "recent" | "quiet" | "dormant" | "archived"

/** activities in display order, with their labels. */
export const activities: { level: Activity; label: string; within: string }[] = [
  { level: "active", label: "Active", within: "a commit in the last 30 days" },
  { level: "recent", label: "Recent", within: "a commit in the last 6 months" },
  { level: "quiet", label: "Quiet", within: "a commit in the last 2 years" },
  { level: "dormant", label: "Dormant", within: "no commit for 2 years" },
  { level: "archived", label: "Archived", within: "archived on GitHub" },
]

/** lastChange is the instant activity is measured from. */
export function lastChange(r: Repo): string {
  return r.lastCommitAt ?? r.pushedAt
}

/** activity classifies a repository relative to now. */
export function activity(r: Repo, now: number): Activity {
  if (r.archived) return "archived"
  const age = now - new Date(lastChange(r)).getTime()
  if (age <= 30 * day) return "active"
  if (age <= 182 * day) return "recent"
  if (age <= 730 * day) return "quiet"
  return "dormant"
}

/** byActivity orders repositories newest change first, archived last. */
export function byActivity(a: Repo, b: Repo): number {
  if (a.archived !== b.archived) return a.archived ? 1 : -1
  return new Date(lastChange(b)).getTime() - new Date(lastChange(a)).getTime()
}

/** Pulse is the fleet summary the telemetry strip shows. */
export type Pulse = {
  repos: number
  active: number
  released: number
  licensed: number
  issues: number
  levels: Record<Activity, number>
}

/** pulse summarises repositories. */
export function pulse(repos: Repo[], now: number): Pulse {
  const levels: Record<Activity, number> = { active: 0, recent: 0, quiet: 0, dormant: 0, archived: 0 }
  let released = 0
  let licensed = 0
  let issues = 0
  for (const r of repos) {
    levels[activity(r, now)]++
    if (r.release) released++
    if (r.license && r.license !== "NOASSERTION") licensed++
    issues += r.openIssues
  }
  return { repos: repos.length, active: levels.active, released, licensed, issues, levels }
}
