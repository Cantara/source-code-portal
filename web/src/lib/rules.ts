import type { Schemas } from "./api/client"

/**
 * The rules the portal judges by, as the server applies them (`GET /api/v1/rules`, internal/health rules.go): the window of the
 * activity counts, the levels, the severities, the activities and every kind of finding. The pages say what these say and hold no
 * thresholds of their own, so the words follow the server (P-62). Pure: a server page passes them in, the client reads them from
 * the provider (rules-context.tsx).
 */
export type Rules = Schemas["Rules"]
export type ActivityLevel = Schemas["ActivityLevel"]
export type Severity = Schemas["SeverityRule"]["severity"]
export type FindingKind = Schemas["FindingKind"]

const day = 24 * 3600 * 1000

/** activityOf is how recently a last change (null: none read) at now falls, by the rules' ages; archived whatever its age. */
export function activityOf(at: string | null, archived: boolean, now: number, rules: Rules): ActivityLevel {
  if (archived) return "archived"
  if (at === null) return "dormant"
  const age = now - new Date(at).getTime()
  for (const a of rules.activity) if (a.withinDays !== null && age <= a.withinDays * day) return a.level
  return "dormant"
}

/** activities are the activities in their order (archived last), as the rules name them. */
export const activities = (rules: Rules) => rules.activity

/** activityRule is one activity's rule; a level the rules do not name keeps its own name. */
export function activityRule(level: ActivityLevel, rules: Rules): Schemas["ActivityRule"] {
  return rules.activity.find((a) => a.level === level) ?? { level, label: level, about: "", withinDays: null }
}

/** age is a span of days in a few letters: 30d, 6mo, 2y. */
export function age(days: number): string {
  if (days >= 730) return `${Math.floor(days / 365)}y`
  if (days >= 60) return `${Math.floor(days / 30)}mo`
  return `${days}d`
}

/** span is an activity's range in a few letters: ≤30d, ≤6mo, ≤2y, then >2y for the one past the last bound; none when archived. */
export function span(level: ActivityLevel, rules: Rules): string {
  const r = activityRule(level, rules)
  if (r.withinDays !== null) return `≤${age(r.withinDays)}`
  if (level === "archived") return ""
  const bounds = rules.activity.flatMap((a) => (a.withinDays === null ? [] : [a.withinDays]))
  return bounds.length > 0 ? `>${age(Math.max(...bounds))}` : ""
}

/** severityOf is a penalty's severity: the first whose start it reaches. */
export function severityOf(penalty: number, rules: Rules): Severity {
  for (const s of [...rules.severities].sort((a, b) => b.from - a.from)) if (penalty >= s.from) return s.severity
  return "low"
}

/** severityLabel is a severity's name. */
export const severityLabel = (s: Severity, rules: Rules) => rules.severities.find((x) => x.severity === s)?.label ?? s

/** kindOf is a kind of finding as the rules tell it; a kind they do not name is named by its code. */
export function kindOf(code: string, rules: Rules): FindingKind {
  return (
    rules.findings.find((k) => k.code === code) ?? {
      code,
      label: code,
      about: "",
      penalties: [],
      action: { label: "", path: "" },
    }
  )
}

/** levelLabel is a health level's name. */
export const levelLabel = (level: Schemas["HealthLevel"], rules: Rules) =>
  rules.levels.find((l) => l.level === level)?.label ?? level

/** levelRanges are the levels with the scores each covers: "80–100", "50–79", "0–49". */
export function levelRanges(rules: Rules): { level: Schemas["HealthLevel"]; label: string; range: string }[] {
  const sorted = [...rules.levels].sort((a, b) => b.from - a.from)
  return sorted.map((l, i) => ({
    level: l.level,
    label: l.label,
    range: `${l.from}–${i === 0 ? 100 : (sorted[i - 1]?.from ?? 100) - 1}`,
  }))
}

/** window is the activity counts' window in words: "30 days". */
export const window = (rules: Rules) => `${rules.windowDays} days`
