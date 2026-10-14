import { queryOptions, useSuspenseQuery } from "@tanstack/react-query"
import { api, unwrap } from "./client"
import { qk } from "./query-keys"

export const siteQuery = () =>
  queryOptions({
    queryKey: qk.site(),
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/site", { signal })),
    staleTime: 60_000,
  })

/** The shell and feature code read the site through this hook (unit 18 asks for it); the shell itself uses the plain query so it renders without it. */
export function useSite() {
  return useSuspenseQuery(siteQuery())
}
