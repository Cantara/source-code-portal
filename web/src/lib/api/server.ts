import "server-only"
import createClient from "openapi-fetch"
import { headers } from "next/headers"
import { cache } from "react"
import type { Api } from "./client"
import type { paths } from "./schema"

// The headers the Go server puts on a proxied page request that the API calls of the page carry back: the viewer with its token,
// and the request id.
const forwarded = ["x-scp-token", "x-scp-viewer", "x-request-id"]

/** serverApi is the API client of the current request: it reaches the Go server at SCP_API as the page's viewer. */
export const serverApi = cache(async (): Promise<Api> => {
  const h = await headers()
  const pass: Record<string, string> = {}
  for (const k of forwarded) {
    const v = h.get(k)
    if (v) pass[k] = v
  }
  return createClient<paths>({ baseUrl: `${process.env.SCP_API ?? "http://127.0.0.1:9090"}/api/v1`, headers: pass })
})

/** origin is the portal's public origin, for canonical and sitemap URLs: the Go server's X-Scp-Origin, else this request's host. */
export const origin = cache(async (): Promise<string> => {
  const h = await headers()
  return h.get("x-scp-origin") ?? `http://${h.get("host") ?? "localhost"}`
})

/** privateViewer reports whether the page is rendered for a viewer who sees private repositories. */
export async function privateViewer(): Promise<boolean> {
  return (await headers()).get("x-scp-viewer") === "private"
}

/** renderedAt is the instant the request renders at, on the clock's 30 s tick grid; the browser hydrates against it (lib/clock.ts). */
export const renderedAt = cache((): number => Math.floor(Date.now() / 30_000) * 30_000)
