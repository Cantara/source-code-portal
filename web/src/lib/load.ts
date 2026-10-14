import { dehydrate, QueryClient, type FetchQueryOptions, type QueryKey } from "@tanstack/react-query"
import { notFound } from "next/navigation"
import { cache } from "react"
import { ProblemError } from "./api/client"

/** serverQueryClient is the query cache of one server request, shared by its metadata and its page. */
export const serverQueryClient = cache(
  () => new QueryClient({ defaultOptions: { queries: { staleTime: 10_000, retry: false } } }),
)

/**
 * load fetches a page's main query on the server: a 404 problem shows the not-found page, any other failure gives undefined (the
 * page renders without the data and the browser tries again).
 */
export async function load<T, K extends QueryKey>(options: FetchQueryOptions<T, Error, T, K>): Promise<T | undefined> {
  try {
    return await serverQueryClient().fetchQuery(options)
  } catch (e) {
    if (e instanceof ProblemError && e.status === 404) notFound()
    return undefined
  }
}

/**
 * peek fetches a query for a page's metadata; any failure gives undefined. Metadata never decides the status: the page does, so the
 * not-found page renders on the server.
 */
export async function peek<T, K extends QueryKey>(options: FetchQueryOptions<T, Error, T, K>): Promise<T | undefined> {
  return serverQueryClient()
    .fetchQuery(options)
    .catch(() => undefined)
}

/** prefetch loads a panel's query on the server; a failure leaves it to the browser. */
export async function prefetch<T, K extends QueryKey>(options: FetchQueryOptions<T, Error, T, K>): Promise<void> {
  await serverQueryClient().prefetchQuery(options)
}

/** state is what the server loaded, for HydrationBoundary. */
export function state() {
  return dehydrate(serverQueryClient())
}
