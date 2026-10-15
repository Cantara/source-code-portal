import createClient, { type Client } from "openapi-fetch"
import type { components, paths } from "./schema"

export type Schemas = components["schemas"]
export type Freshness = Schemas["Freshness"]
export type Commit = Schemas["Commit"]
export type Repo = Schemas["Repo"]

/** ProblemError is every failed request: an RFC 9457 problem from the API, or a network failure (status 0, code "network"). */
export class ProblemError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId: string
  readonly retryAfter: number // seconds; 0 when not sent

  constructor(status: number, code: string, detail: string, requestId = "", retryAfter = 0) {
    super(detail)
    this.name = "ProblemError"
    this.status = status
    this.code = code
    this.requestId = requestId
    this.retryAfter = retryAfter
  }

  /** retryable failures are worth another attempt: network, rate limit and server errors. */
  get retryable(): boolean {
    return this.status === 0 || this.status === 429 || this.status >= 500
  }
}

/** Api is a typed client of /api/v1: the browser's (api) or one per server request (lib/api/server.ts). */
export type Api = Client<paths>

/**
 * api is the browser's client. The base is absolute (the tests resolve against jsdom's location) and fetch is looked up per request
 * (the tests intercept it). On the server it is never called: the pages hand their data to the browser's cache. The providers add
 * the build check (lib/build.ts).
 */
export const api: Api = createClient<paths>({
  baseUrl: `${globalThis.location?.origin ?? "http://localhost"}/api/v1`,
  fetch: (r) => globalThis.fetch(r),
})

/** Get reads a path of /api/v1 outside the contract (the operator's status) as JSON, failing as unwrap does. */
export type Get = <T>(path: string, signal?: AbortSignal) => Promise<T>

/** getter is a Get against base with headers. */
export function getter(base: string, headers: Record<string, string> = {}): Get {
  return <T>(path: string, signal?: AbortSignal) =>
    unwrap<T>(
      globalThis
        .fetch(`${base}${path}`, { headers: { Accept: "application/json", ...headers }, signal })
        .then(async (response) => {
          const body: unknown = await response.json().catch(() => undefined)
          return response.ok ? { data: body as T, response } : { error: body, response }
        }),
    )
}

/** get is the browser's Get. */
export const get: Get = (path, signal) => getter(`${globalThis.location?.origin ?? "http://localhost"}/api/v1`)(path, signal)

interface Result<T> {
  data?: T
  error?: unknown
  response: Response
}

/** unwrap returns the data of a request or throws a ProblemError. */
export async function unwrap<T>(request: Promise<Result<T>>): Promise<T> {
  let result: Result<T>
  try {
    result = await request
  } catch (e) {
    if (e instanceof DOMException && e.name === "AbortError") throw e
    throw new ProblemError(0, "network", "The portal could not be reached.")
  }
  const { data, error, response } = result
  if (response.ok && data !== undefined) return data
  const p = (error ?? {}) as Partial<Schemas["Problem"]>
  const requestId = (p.instance ?? "").replace(/^urn:scp:request:/, "") || (response.headers.get("X-Request-Id") ?? "")
  const retryAfter = Number(response.headers.get("Retry-After") ?? "0") || 0
  throw new ProblemError(
    response.status,
    p.code ?? `http-${response.status}`,
    p.detail ?? response.statusText,
    requestId,
    retryAfter,
  )
}
