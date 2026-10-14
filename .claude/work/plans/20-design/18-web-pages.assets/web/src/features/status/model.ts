import type { components } from "@/lib/api/schema"
type Warning = components["schemas"]["StatusWarning"]
type Rate = components["schemas"]["RateInfo"]
type Status = components["schemas"]["Status"]

const SEV: Readonly<Record<string, number>> = { critical: 0, warning: 1, info: 2 }
/** Server order is already "most severe first"; re-sorting is a stable guard so a reordered response cannot bury a critical warning. */
export function sortWarnings(ws: readonly Warning[]): Warning[] {
  return ws.map((w, i) => [w, i] as const).sort((a, b) => (SEV[a[0].severity] ?? 3) - (SEV[b[0].severity] ?? 3) || a[1] - b[1]).map(([w]) => w)
}

/** Remaining share of a rate-limit window, 0..1; limit 0 (unknown) is reported as null, never as 0 % remaining. */
export function rateShare(r: Rate): number | null {
  return r.limit > 0 ? Math.min(1, Math.max(0, r.remaining / r.limit)) : null
}

export type HealthTone = "ok" | "degraded" | "down"
/** One header light for the whole page: down = a gate failed or the store is degraded; degraded = a warning or critical entry exists. */
export function overallTone(s: Pick<Status, "readiness" | "store" | "warnings">): HealthTone {
  if (s.readiness.state === "shutting-down" || s.readiness.gates.some((g) => !g.ok) || s.store.state !== "ok") return "down"
  if (s.readiness.state === "degraded" || s.warnings.some((w) => w.severity !== "info")) return "degraded"
  return "ok"
}

/** Key shown in the retry dialog is exactly the engine key; the UI never builds one. */
export const canRetry = (key: string): boolean => /^[a-z][a-z0-9._-]*\/\S+$/.test(key) && key.length <= 512
