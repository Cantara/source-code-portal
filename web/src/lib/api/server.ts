import "server-only"
import createClient from "openapi-fetch"
import { headers } from "next/headers"
import { cache } from "react"
import { getter, type Api, type Get } from "./client"
import type { paths } from "./schema"

// The headers the Go server puts on a proxied page request that the API calls of the page carry back: the viewer (its kind, name
// and how it was identified) with the web UI's token, and the request id.
const forwarded = ["x-scp-token", "x-scp-viewer", "x-scp-name", "x-scp-via", "x-request-id"]

const base = () => `${process.env.SCP_API ?? "http://127.0.0.1:9090"}/api/v1`

const pass = cache(async (): Promise<Record<string, string>> => {
  const h = await headers()
  const out: Record<string, string> = {}
  for (const k of forwarded) {
    const v = h.get(k)
    if (v) out[k] = v
  }
  return out
})

/** serverApi is the API client of the current request: it reaches the Go server at SCP_API as the page's viewer. */
export const serverApi = cache(async (): Promise<Api> => createClient<paths>({ baseUrl: base(), headers: await pass() }))

/** serverGet is the current request's Get, as the page's viewer. */
export const serverGet = cache(async (): Promise<Get> => getter(base(), await pass()))

/** origin is the portal's public origin, for canonical and sitemap URLs: the Go server's X-Scp-Origin, else this request's host. */
export const origin = cache(async (): Promise<string> => {
  const h = await headers()
  return h.get("x-scp-origin") ?? `http://${h.get("host") ?? "localhost"}`
})

/** privateViewer reports whether the page is rendered for a viewer who sees private repositories. */
export async function privateViewer(): Promise<boolean> {
  const v = (await headers()).get("x-scp-viewer")
  return v === "private" || v === "admin"
}

/** renderedAt is the instant the request renders at, on the clock's 30 s tick grid; the browser hydrates against it (lib/clock.ts). */
export const renderedAt = cache((): number => Math.floor(Date.now() / 30_000) * 30_000)
