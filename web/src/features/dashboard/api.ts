import { queryOptions } from "@tanstack/react-query"
import { unwrap, type Api } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"

export const dashboardQuery = (api: Api) =>
  queryOptions({ queryKey: keys.dashboard(), queryFn: ({ signal }) => unwrap(api.GET("/dashboard", { signal })) })

/** latestAllQuery is the newest commits of every kind, for the dashboard's "include automation". */
export const latestAllQuery = (api: Api, limit: number) =>
  queryOptions({
    queryKey: keys.commitLog("", "", true, limit, 1),
    queryFn: ({ signal }) => unwrap(api.GET("/commits", { params: { query: { limit } }, signal })),
  })
