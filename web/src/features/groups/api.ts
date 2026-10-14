import { queryOptions } from "@tanstack/react-query"
import { unwrap, type Api } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"

export const groupCommitsQuery = (api: Api, group: string) =>
  queryOptions({
    queryKey: keys.latestCommits(group),
    queryFn: ({ signal }) => unwrap(api.GET("/commits", { params: { query: { group, limit: 10 } }, signal })),
  })
