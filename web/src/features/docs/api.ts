import { queryOptions } from "@tanstack/react-query"
import { unwrap, type Api } from "@/lib/api/client"

export const docListQuery = (api: Api, owner: string, name: string) =>
  queryOptions({
    queryKey: ["docs", owner.toLowerCase(), name.toLowerCase()],
    queryFn: ({ signal }) => unwrap(api.GET("/repos/{owner}/{name}/docs", { params: { path: { owner, name } }, signal })),
  })

export const docQuery = (api: Api, owner: string, name: string, docId: string) =>
  queryOptions({
    queryKey: ["doc", owner.toLowerCase(), name.toLowerCase(), docId],
    queryFn: ({ signal }) =>
      unwrap(api.GET("/repos/{owner}/{name}/docs/{docId}", { params: { path: { owner, name, docId } }, signal })),
  })
