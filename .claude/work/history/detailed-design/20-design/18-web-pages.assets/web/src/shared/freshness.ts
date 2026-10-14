import type { components } from "@/lib/api/schema"
type Freshness = components["schemas"]["Freshness"]

export type FreshnessStatus = Freshness["status"]
export type Tone = "success" | "warning" | "danger" | "muted"

const RANK: Readonly<Record<FreshnessStatus, number>> = { fresh: 0, unknown: 1, stale: 2, error: 3 }

/** Open enum on the wire: an unknown status is treated as `unknown`. */
export function normaliseStatus(s: string): FreshnessStatus {
  return s === "fresh" || s === "stale" || s === "error" ? s : "unknown"
}

export function toneOf(s: FreshnessStatus): Tone {
  switch (s) {
    case "fresh": return "success"
    case "stale": return "warning"
    case "error": return "danger"
    case "unknown": return "muted"
  }
}

/** The worst of several freshness objects (header summary, composite pages). Empty input is `unknown`. */
export function worst(fs: readonly Freshness[]): FreshnessStatus {
  let w: FreshnessStatus = fs.length === 0 ? "unknown" : "fresh"
  for (const f of fs) {
    const s = normaliseStatus(f.status)
    if (RANK[s] > RANK[w]) w = s
  }
  return w
}

/** Whole seconds since the data was true upstream (asOf), falling back to fetchedAt; null when never fetched. Never negative. */
export function ageSeconds(f: Freshness, now: Date): number | null {
  const t = f.asOf ?? f.fetchedAt
  if (t === null || t === undefined) return null
  return Math.max(0, Math.floor((now.getTime() - Date.parse(t)) / 1000))
}

/** A banner is shown only for stale or error; `unknown` shows a quiet placeholder, never a banner. */
export const needsBanner = (s: FreshnessStatus): boolean => s === "stale" || s === "error"

/**
 * One closed snake_case vocabulary (cross-design review CD-ER-01, 20-design/92-error-catalogue.md section 3): `not_found`, `forbidden`, `auth`,
 * `permission_missing`, `suspended`, `rate_limited`, `unavailable`, `timeout`, `transient`, `permanent`, `gone`, `no_data`, `config`, `protocol`,
 * `too_large`, `aborted`, `history_rewritten`, `canceled`, `internal`. The dotted spellings stay below as tolerated input from a server of an earlier release, never produced by the
 * contract. Unknown codes read as the generic cause; the code itself is shown in a tooltip.
 */
const EXACT_CAUSES: Readonly<Record<string, string>> = {
  not_found: "freshness.cause.not-found", "github.not_found": "freshness.cause.not-found", gone: "freshness.cause.not-found",
  forbidden: "freshness.cause.forbidden", "github.forbidden": "freshness.cause.forbidden", permission_missing: "freshness.cause.forbidden",
  unavailable: "freshness.cause.unavailable", timeout: "freshness.cause.unavailable", transient: "freshness.cause.unavailable", aborted: "freshness.cause.unavailable",
  rate_limited: "freshness.cause.rate-limited",
  auth: "freshness.cause.credentials",
  suspended: "freshness.cause.suspended",
  history_rewritten: "freshness.cause.github", no_data: "freshness.cause.github", protocol: "freshness.cause.github",
}
const ERROR_PREFIXES: ReadonlyArray<readonly [string, string]> = [
  ["github.rate_limited", "freshness.cause.rate-limited"],
  ["github.unauthorized", "freshness.cause.credentials"],
  ["github.suspended", "freshness.cause.suspended"],
  ["github.", "freshness.cause.github"],
  ["parse.", "freshness.cause.parse"],
  ["descriptor.", "freshness.cause.descriptor"],
]
/** Lingui id of the human cause for lastErrorCode; unknown codes get the generic id (the code itself is shown in a tooltip). */
export function causeId(code: string | null | undefined): string {
  if (!code) return "freshness.cause.unknown"
  const exact = EXACT_CAUSES[code]
  if (exact !== undefined) return exact
  for (const [p, id] of ERROR_PREFIXES) if (code.startsWith(p)) return id
  return "freshness.cause.unknown"
}
