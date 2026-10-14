// Mapping of API problem codes to what the user sees (GR-16). The code is the key; `detail` is never shown as the message.
import { ProblemError } from "@/lib/api/problem"

export type ProblemKind =
  | "not-found" | "disabled" | "not-ready" | "unavailable" | "rate-limited"
  | "forbidden" | "unauthorized" | "bad-request" | "server-error" | "offline" | "skew"

export type RetryMode = "auto" | "manual" | "none"

export interface ProblemView {
  readonly kind: ProblemKind
  /** Lingui message id of the title and the body (catalogue keys `problem.<kind>.title|body`). */
  readonly titleId: string
  readonly bodyId: string
  readonly retry: RetryMode
  /** Seconds before the automatic retry, from Retry-After when present. */
  readonly retryAfterSeconds: number | null
  /** What the status of the HTML response should be considered: the SPA never sets it, tests assert it. */
  readonly httpStatus: number
  readonly requestId: string | null
  readonly code: string
}

const KIND_BY_CODE: Readonly<Record<string, ProblemKind>> = {
  "not-found": "not-found",
  "repo-renamed": "not-found", // only reachable when a redirect could not be followed
  "feature-disabled": "disabled",
  "not-ready": "not-ready",
  "queue-full": "unavailable",
  "upstream-unavailable": "unavailable",
  "rate-limited": "rate-limited",
  forbidden: "forbidden",
  unauthorized: "unauthorized",
  "validation-failed": "bad-request",
  "invalid-cursor": "bad-request",
  "method-not-allowed": "bad-request",
  "not-acceptable": "bad-request",
  "payload-too-large": "bad-request",
  "unsupported-media-type": "bad-request",
  internal: "server-error",
  // codes minted by the client (lib/api/problem.ts ClientCode): no problem document existed
  "client-network": "offline",
  "client-aborted": "offline", // normally swallowed by TanStack Query; never rendered as a page
  "client-unparseable": "unavailable", // a proxy answered with HTML or an empty body
  "client-contract": "server-error", // 2xx without a body
}

const RETRY: Readonly<Record<ProblemKind, RetryMode>> = {
  "not-found": "none", disabled: "none", "not-ready": "auto", unavailable: "manual", "rate-limited": "auto",
  forbidden: "none", unauthorized: "none", "bad-request": "none", "server-error": "manual", offline: "auto", skew: "manual",
}

const FALLBACK_STATUS: Readonly<Record<ProblemKind, number>> = {
  "not-found": 404, disabled: 404, "not-ready": 503, unavailable: 503, "rate-limited": 429,
  forbidden: 403, unauthorized: 401, "bad-request": 400, "server-error": 500, offline: 0, skew: 0,
}

/** Total function: any thrown value becomes a ProblemView. Unknown codes are classified by HTTP status, never shown raw. */
export function problemView(e: unknown): ProblemView {
  if (e instanceof ProblemError) {
    let kind: ProblemKind | undefined = KIND_BY_CODE[e.code]
    if (kind === undefined) {
      if (e.status === 0) kind = "offline"
      else if (e.status === 404) kind = "not-found"
      else if (e.status === 429) kind = "rate-limited"
      else if (e.status === 401) kind = "unauthorized"
      else if (e.status === 403) kind = "forbidden"
      else if (e.status === 503) kind = "unavailable"
      else if (e.status >= 400 && e.status < 500) kind = "bad-request"
      else kind = "server-error"
    }
    return build(kind, e.code, e.status, e.requestId, e.retryAfterSeconds)
  }
  // `unwrap()` (lib/api/client.ts) converts every fetch rejection into a ProblemError `client-network`, so a bare TypeError reaching this point is
  // a bug in our own code (`Cannot read properties of undefined`): it is a server-error page, never "You are offline" with an automatic retry (V-03).
  return build("server-error", "client-error", 0, null, null)
}

function build(kind: ProblemKind, code: string, status: number, requestId: string | null, retryAfter: number | null): ProblemView {
  return {
    kind, code, requestId, retryAfterSeconds: retryAfter, retry: RETRY[kind],
    titleId: `problem.${kind}.title`, bodyId: `problem.${kind}.body`,
    httpStatus: status || FALLBACK_STATUS[kind],
  }
}

/** Delay before the nth automatic retry: honours Retry-After (capped at 120 s), else 2, 4, 8, 16, 30 s with +-20 % jitter (injected). */
export function retryDelayMs(attempt: number, retryAfterSeconds: number | null, rnd: () => number): number {
  if (retryAfterSeconds !== null) return Math.min(retryAfterSeconds, 120) * 1000
  const base = Math.min(30, 2 ** (attempt + 1))
  return Math.round(base * 1000 * (0.8 + 0.4 * rnd()))
}
