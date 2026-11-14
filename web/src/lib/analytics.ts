/**
 * The deployment's analytics (design/analytics.md, P-113): the site Plausible knows the portal as and the portal's own address the
 * tracker posts its events to, which Go passes to this server as the JSON in SCP_ANALYTICS. Without it: no tracker.
 */
export interface Analytics {
  /** domain is the site as Plausible knows it. */
  domain: string
  /** path is the portal's own address for events, which forwards them to Plausible. */
  path: string
}

const site = /^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?(\/[a-z0-9._~/-]*)?$/i

/** parseAnalytics reads SCP_ANALYTICS defensively: anything else than the expected shape is no tracker. */
export function parseAnalytics(raw: string | undefined): Analytics | undefined {
  let v: unknown
  try {
    v = raw ? JSON.parse(raw) : undefined
  } catch {
    return undefined
  }
  const o = v && typeof v === "object" ? (v as Record<string, unknown>) : {}
  const { domain, path } = o
  if (typeof domain !== "string" || !site.test(domain)) return undefined
  if (typeof path !== "string" || !path.startsWith("/") || path.startsWith("//") || path.length > 64) return undefined
  return { domain, path }
}

let held: Analytics | undefined | null = null

/** analytics is this server's tracker setting, read once. */
export function analytics(): Analytics | undefined {
  if (held === null) held = parseAnalytics(process.env.SCP_ANALYTICS)
  return held
}

// The query parameters Plausible reads a visit's source from; every other one (a search, a login in a filter) stays with the portal.
const campaign = /^(utm_[a-z]+|ref|source|via)$/

/** scrub is a page's address as the analytics sees it: the origin and the path, with the campaign parameters and nothing else. */
export function scrub(address: string): string {
  try {
    const u = new URL(address)
    const keep = new URLSearchParams([...u.searchParams].filter(([k]) => campaign.test(k)))
    return u.origin + u.pathname + (keep.size > 0 ? "?" + keep.toString() : "")
  } catch {
    return address
  }
}

/** counted says whether a page address is a visit: the presentation page reloads itself on a wall and is not one. */
export function counted(address: string): boolean {
  try {
    return !new URL(address).pathname.startsWith("/present")
  } catch {
    return true
  }
}
