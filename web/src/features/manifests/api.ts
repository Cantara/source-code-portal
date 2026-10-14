import { queryOptions } from "@tanstack/react-query"
import { unwrap, type Api } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"

export const manifestsQuery = (api: Api, owner: string, name: string) =>
  queryOptions({
    queryKey: keys.manifests(owner, name),
    queryFn: ({ signal }) => unwrap(api.GET("/repos/{owner}/{name}/manifests", { params: { path: { owner, name } }, signal })),
  })
