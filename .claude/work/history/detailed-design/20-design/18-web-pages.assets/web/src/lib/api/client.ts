import createClient, { type Middleware } from "openapi-fetch"
import type { paths } from "./schema"
import { problemFromResponse, problemFromThrown, ProblemError } from "./problem"
import { serverStatus } from "@/lib/server-status"

const middleware: Middleware = {
  onResponse({ response }) {
    serverStatus.reportBuild(response.headers.get("X-SCP-Build"))
    return undefined
  },
}

export function createApi(baseUrl: string = window.location.origin) {
  const client = createClient<paths>({ baseUrl })
  client.use(middleware)
  return client
}
export const api = createApi()

type Result<T> = { data?: T | undefined; error?: unknown; response: Response }

/**
 * Turn an openapi-fetch result into data or a thrown ProblemError.
 * The browser HTTP cache performs ETag revalidation (Cache-Control: no-cache + ETag), so a 304 is never visible here.
 */
export async function unwrap<T>(p: Promise<Result<T>>): Promise<T> {
  let r: Result<T>
  try {
    r = await p
  } catch (e) {
    serverStatus.reportFailure()
    throw problemFromThrown(e)
  }
  if (r.error !== undefined || !r.response.ok) {
    if (r.response.status >= 500 || r.response.status === 429) serverStatus.reportFailure()
    throw problemFromResponse(r.response, r.error)
  }
  serverStatus.reportOk()
  if (r.data === undefined) throw new ProblemError({ status: 0, code: "client-contract", title: "Empty response body" })
  return r.data
}
