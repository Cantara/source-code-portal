import type { components } from "./schema"

export type Problem = components["schemas"]["Problem"]
export type FieldError = components["schemas"]["FieldError"]

/** Codes minted by the client for failures that never produced a problem document. */
export type ClientCode =
  | "client-network" // fetch rejected (DNS, reset, CORS, offline)
  | "client-aborted" // AbortController
  | "client-unparseable" // non-JSON error body (proxy HTML 502, empty 500)
  | "client-contract" // 2xx without a body or with an unexpected shape

const UUID_URN = /^urn:uuid:([0-9a-f-]{36})$/i

/** The single error type thrown by every query function. Features never see raw fetch errors. */
export class ProblemError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId: string | null
  readonly retryAfterSeconds: number | null
  readonly fieldErrors: readonly FieldError[]
  readonly detail: string | null

  constructor(init: {
    status: number
    code: string
    title: string
    detail?: string | null | undefined
    requestId?: string | null | undefined
    retryAfterSeconds?: number | null | undefined
    fieldErrors?: readonly FieldError[] | undefined
  }) {
    super(init.title)
    this.name = "ProblemError"
    this.status = init.status
    this.code = init.code
    this.detail = init.detail ?? null
    this.requestId = init.requestId ?? null
    this.retryAfterSeconds = init.retryAfterSeconds ?? null
    this.fieldErrors = init.fieldErrors ?? []
  }

  get isClient(): boolean {
    return this.status === 0
  }
}

export function isProblem(e: unknown): e is ProblemError {
  return e instanceof ProblemError
}

function isProblemBody(b: unknown): b is Problem {
  return typeof b === "object" && b !== null && typeof (b as Problem).code === "string" && typeof (b as Problem).status === "number"
}

/** Build a ProblemError from an error response. `body` is the already-parsed JSON (or undefined). */
export function problemFromResponse(response: Response, body: unknown): ProblemError {
  const header = response.headers.get("X-Request-Id")
  const retryHeader = Number.parseInt(response.headers.get("Retry-After") ?? "", 10)
  if (isProblemBody(body)) {
    const m = body.instance ? UUID_URN.exec(body.instance) : null
    return new ProblemError({
      status: body.status,
      code: body.code,
      title: body.title,
      detail: body.detail,
      requestId: m?.[1] ?? header,
      retryAfterSeconds: body.retryAfterSeconds ?? (Number.isFinite(retryHeader) ? retryHeader : null),
      fieldErrors: body.errors,
    })
  }
  return new ProblemError({
    status: response.status,
    code: "client-unparseable",
    title: `Unexpected response (${response.status})`,
    requestId: header,
    retryAfterSeconds: Number.isFinite(retryHeader) ? retryHeader : null,
  })
}

export function problemFromThrown(e: unknown): ProblemError {
  if (isProblem(e)) return e
  // by name, not instanceof: the AbortError may come from another realm (jsdom/undici in tests, iframes)
  if (typeof e === "object" && e !== null && (e as { name?: unknown }).name === "AbortError") {
    return new ProblemError({ status: 0, code: "client-aborted", title: "Request aborted" })
  }
  return new ProblemError({ status: 0, code: "client-network", title: e instanceof Error ? e.message : "Network error" })
}

/** Transient = worth retrying automatically (5xx except 501, 429, network). */
export function isTransient(e: unknown): boolean {
  if (!isProblem(e)) return true
  if (e.code === "client-aborted" || e.code === "client-contract") return false
  if (e.status === 0 || e.status === 429) return true
  return e.status >= 500 && e.status !== 501
}
