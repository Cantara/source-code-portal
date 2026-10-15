import "server-only"
import { dehydrate, QueryClient, type QueryExecuteOptions, type QueryKey } from "@tanstack/react-query"
import { notFound, permanentRedirect } from "next/navigation"
import { cache } from "react"
import { ProblemError, type Api } from "./api/client"
import { repoQuery } from "./api/queries"
import { repoPath } from "./paths"

/** newServerQueryClient is the query cache of one server render. */
export const newServerQueryClient = () => new QueryClient({ defaultOptions: { queries: { staleTime: 10_000, retry: false } } })

/** serverQueryClient is the query cache of one server request, shared by its metadata and its page. */
export const serverQueryClient = cache(newServerQueryClient)

/**
 * load fetches a page's main query on the server: a 404 problem shows the not-found page, any other failure gives undefined (the
 * page renders without the data and the browser tries again).
 */
export async function load<T, K extends QueryKey>(
  qc: QueryClient,
  options: QueryExecuteOptions<T, Error, T, T, K>,
): Promise<T | undefined> {
  try {
    return await qc.query(options)
  } catch (e) {
    if (e instanceof ProblemError && e.status === 404) notFound()
    return undefined
  }
}

/**
 * peek fetches a query for a page's metadata; any failure gives undefined. Metadata never decides the status: the page does, so the
 * not-found page renders on the server.
 */
export async function peek<T, K extends QueryKey>(
  qc: QueryClient,
  options: QueryExecuteOptions<T, Error, T, T, K>,
): Promise<T | undefined> {
  return qc.query(options).catch(() => undefined)
}

/** prefetch loads a panel's query on the server; a failure leaves it to the browser. */
export async function prefetch<T, K extends QueryKey>(
  qc: QueryClient,
  options: QueryExecuteOptions<T, Error, T, T, K>,
): Promise<void> {
  await qc.query(options).catch(() => undefined)
}

/** state is what the server loaded, for HydrationBoundary. */
export function state(qc: QueryClient) {
  return dehydrate(qc)
}

/** Here is the address of a page under a repository, built from the repository's current owner and name. */
export type Here = (owner: string, name: string) => string

/**
 * loadRepo loads the repository of a page: unknown is the not-found page, and a renamed or transferred repository (the API followed
 * the alias) answers 308 to the same page at its current address. Addresses are case-insensitive, as on GitHub: another casing
 * renders, and the page's canonical is the lower-case address.
 */
export async function loadRepo(api: Api, qc: QueryClient, owner: string, name: string, here: Here = repoPath) {
  const data = await load(qc, repoQuery(api, owner, name))
  if (data && data.repo.address !== `${owner}/${name}`.toLowerCase()) permanentRedirect(here(data.repo.owner, data.repo.name))
  return data
}
