import "server-only"
import { HydrationBoundary, type QueryClient } from "@tanstack/react-query"
import type { Metadata } from "next"
import type { ReactNode } from "react"
import type { Api } from "./api/client"
import { serverApi } from "./api/server"
import { serverQueryClient, state } from "./load"

/** Search is a page's query string as Next.js passes it. */
export type Search = Record<string, string | string[] | undefined>

/** Ctx is what a route module works with on the server: the viewer's API client, the request's query cache, params and query. */
export interface Ctx<P> {
  api: Api
  qc: QueryClient
  params: P
  search: Search
}

/**
 * Route is a page of a feature: load fills the query cache (and may answer not-found or redirect), metadata names the page, render
 * draws it from the cache.
 */
export interface Route<P> {
  load: (ctx: Ctx<P>) => Promise<unknown>
  metadata: (ctx: Ctx<P>) => Promise<Metadata> | Metadata
  render: (ctx: Ctx<P>) => ReactNode
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
  const [api, p, search] = await Promise.all([serverApi(), params, searchParams])
  return { api, qc: serverQueryClient(), params: p, search }
}

/** page turns a route module into the exports of an app/ page file. */
export function page<P>(route: Route<P>) {
  return {
    async Page(props: Props<P>) {
      const ctx = await context(props)
      await route.load(ctx)
      return <HydrationBoundary state={state(ctx.qc)}>{route.render(ctx)}</HydrationBoundary>
    },
    async generateMetadata(props: Props<P>): Promise<Metadata> {
      return route.metadata(await context(props))
    },
  }
}
