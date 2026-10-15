import "server-only"
import { HydrationBoundary, type QueryClient } from "@tanstack/react-query"
import type { Metadata } from "next"
import { cookies } from "next/headers"
import type { ReactNode } from "react"
import type { Api } from "./api/client"
import { origin, serverApi } from "./api/server"
import { serverQueryClient, state } from "./load"
import { JsonLd, ld, type Thing } from "./seo"

/** Search is a page's query string as Next.js passes it. */
export type Search = Record<string, string | string[] | undefined>

/** Ctx is what a route module works with on the server: the viewer's API client, the request's query cache, params and query. */
export interface Ctx<P> {
  api: Api
  qc: QueryClient
  params: P
  search: Search
  /** the portal's own cookies (`scp-*`): the reader's preferences */
  cookies: Record<string, string>
}

/**
 * Route is a page of a feature: load fills the query cache (and may answer not-found or redirect), metadata names the page, render
 * draws it from the cache.
 */
export interface Route<P> {
  load: (ctx: Ctx<P>) => Promise<unknown>
  metadata: (ctx: Ctx<P>) => Promise<Metadata> | Metadata
  render: (ctx: Ctx<P>) => ReactNode
  /** structured is the page's JSON-LD nodes, beside the site's, with URLs under base; a page without it has the site's only */
  structured?: (ctx: Ctx<P>, base: string) => Promise<Thing[]> | Thing[]
}

/** Panel is a block one feature contributes to another feature's page, composed in app/. */
export interface Panel<P> {
  id: string
  column: "main" | "side"
  prefetch: (ctx: Ctx<P>) => Promise<unknown>
  render: (params: P) => ReactNode
}

type Props<P> = { params: Promise<P>; searchParams: Promise<Search> }

async function context<P>({ params, searchParams }: Props<P>): Promise<Ctx<P>> {
  const [api, p, search, jar] = await Promise.all([serverApi(), params, searchParams, cookies()])
  const own = Object.fromEntries(jar.getAll().flatMap((c) => (c.name.startsWith("scp-") ? [[c.name, c.value]] : [])))
  return { api, qc: serverQueryClient(), params: p, search, cookies: own }
}

/** page turns a route module into the exports of an app/ page file. */
export function page<P>(route: Route<P>) {
  return {
    async Page(props: Props<P>) {
      const ctx = await context(props)
      await route.load(ctx)
      const base = await origin()
      const graph = [ld.website(base), ...((await route.structured?.(ctx, base)) ?? [])]
      return (
        <>
          <JsonLd graph={graph} />
          <HydrationBoundary state={state(ctx.qc)}>{route.render(ctx)}</HydrationBoundary>
        </>
      )
    },
    async generateMetadata(props: Props<P>): Promise<Metadata> {
      return route.metadata(await context(props))
    },
  }
}
