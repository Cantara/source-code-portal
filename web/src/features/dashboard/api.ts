import { queryOptions } from "@tanstack/react-query"
import { unwrap, type Api } from "@/lib/api/client"

export const dashboardQuery = (api: Api) =>
  queryOptions({
    queryKey: ["dashboard"],
    queryFn: ({ signal }) => unwrap(api.GET("/dashboard", { signal })),
  })

export const reposQuery = (api: Api) =>
  queryOptions({
    queryKey: ["repos"],
    queryFn: ({ signal }) => unwrap(api.GET("/repos", { signal })),
  })
