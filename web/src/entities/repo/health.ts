import type { Repo } from "@/lib/api/client"
import { activityOf, type ActivityLevel, type Rules } from "@/lib/rules"

/** Activity is how recently the work that counts changed a repository; the rules (lib/rules.ts) say the ages each covers. */
export type Activity = ActivityLevel

/** lastChange is the instant activity is measured from. */
export function lastChange(r: Repo): string {
  return r.lastAuthoredAt ?? r.lastCommitAt ?? r.pushedAt
}

/** activity classifies a repository relative to now by the server's rules. */
export function activity(r: Repo, now: number, rules: Rules): Activity {
  return activityOf(lastChange(r), r.archived, now, rules)
}

/** byActivity orders repositories newest change first, archived last. */
export function byActivity(a: Repo, b: Repo): number {
  if (a.archived !== b.archived) return a.archived ? 1 : -1
  return new Date(lastChange(b)).getTime() - new Date(lastChange(a)).getTime()
}
