import { queryOptions } from "@tanstack/react-query"
import { unwrap, type Api } from "@/lib/api/client"

export const groupQuery = (api: Api, group: string) =>
  queryOptions({
    queryKey: ["group", group],
    queryFn: ({ signal }) => unwrap(api.GET("/groups/{group}", { params: { path: { group } }, signal })),
  })

export const groupCommitsQuery = (api: Api, group: string) =>
  queryOptions({
    queryKey: ["commits", "latest", "group", group],
    queryFn: ({ signal }) => unwrap(api.GET("/commits", { params: { query: { group, limit: 10 } }, signal })),
  })
