import { queryOptions } from "@tanstack/react-query"
import { api, unwrap } from "@/lib/api/client"
import { qk } from "@/lib/api/query-keys"

/** Rows in the dashboard "latest commits" panel: one named constant, never a literal repeated at each use (GR-14). */
export const DASHBOARD_COMMITS = 10

export const groupsQuery = () =>
  queryOptions({
    queryKey: qk.catalog.groups({ sort: "config" }),
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/groups", { params: { query: { sort: "config" } }, signal })),
    staleTime: 10_000,
  })

export const latestCommitsQuery = () =>
  queryOptions({
    queryKey: qk.commits.list({ scope: "latest", limit: DASHBOARD_COMMITS }),
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/commits", { params: { query: { limit: DASHBOARD_COMMITS } }, signal })),
    staleTime: 10_000,
  })
