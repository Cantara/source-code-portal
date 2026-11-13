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

/** querySerializer writes array parameters as the contract has them: comma-separated in one value (work=hand,ai). */
export const querySerializer = { array: { style: "form", explode: false } } as const

/** forksCookie is the reader's choice to see the forks no source shows and no group names (Settings, the switch on the dashboard). */
export const forksCookie = "scp-forks"

/** asking is the request with forks=show added to its URL: the API leaves forks out unless asked, and the URL tells caches apart. */
export function asking(request: Request): Request {
  const url = new URL(request.url)
  url.searchParams.set("forks", "show")
  return new Request(url, request)
}

/** forksShown reports whether this browser asks for the forks (`scp-forks=show`); false on the server, which reads its own cookies. */
export function forksShown(): boolean {
  return typeof document !== "undefined" && document.cookie.split("; ").includes(`${forksCookie}=show`)
}

/** apiBase is where /api/v1 is: this page's origin (the tests' own without one). */
const apiBase = () => `${globalThis.location?.origin ?? "http://localhost"}/api/v1`

/**
 * api is the browser's client. The base is absolute (the tests resolve against jsdom's location) and fetch is looked up per request
 * (the tests intercept it). On the server it is never called: the pages hand their data to the browser's cache. The providers add
 * the build check (lib/build.ts).
 */
export const api: Api = createClient<paths>({
  baseUrl: apiBase(),
  fetch: (r) => globalThis.fetch(forksShown() ? asking(r) : r),
  querySerializer,
})

/** Get reads a path of /api/v1 outside the contract (the operator's status) as JSON, failing as unwrap does. */
export type Get = <T>(path: string, signal?: AbortSignal) => Promise<T>

/** request is a JSON request outside the typed client: its answer's data, or the problem it failed with (see unwrap). */
function request<T>(url: string, init: RequestInit): Promise<T> {
  return unwrap<T>(
    globalThis.fetch(url, init).then(async (response) => {
      const body: unknown = await response.json().catch(() => undefined)
      return response.ok ? { data: body as T, response } : { error: body, response }
    }),
  )
}

/** getter is a Get against base with headers, asking for the forks when forks says so. */
export function getter(base: string, headers: Record<string, string> = {}, forks: () => boolean = () => false): Get {
  return <T>(path: string, signal?: AbortSignal) =>
    request<T>(`${base}${path}${forks() ? `${path.includes("?") ? "&" : "?"}forks=show` : ""}`, {
      headers: { Accept: "application/json", ...headers },
      signal,
    })
}

/** get is the browser's Get. */
export const get: Get = (path, signal) => getter(apiBase(), {}, forksShown)(path, signal)

/** send writes to a path of /api/v1 outside the contract (the admin's layouts, a person's tokens) with a JSON body, failing as unwrap does. */
export function send<T>(method: "POST" | "PUT" | "DELETE", path: string, body?: unknown): Promise<T> {
  return request<T>(`${apiBase()}${path}`, {
    method,
    headers: { Accept: "application/json", ...(body === undefined ? {} : { "Content-Type": "application/json" }) },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

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
