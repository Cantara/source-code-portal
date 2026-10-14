import { queryOptions } from "@tanstack/react-query"
import { api, unwrap } from "@/lib/api/client"

export const repoQuery = (owner: string, name: string) =>
  queryOptions({
    queryKey: ["repo", owner.toLowerCase(), name.toLowerCase()],
    queryFn: ({ signal }) => unwrap(api.GET("/repos/{owner}/{name}", { params: { path: { owner, name } }, signal })),
  })

export const docListQuery = (owner: string, name: string) =>
  queryOptions({
    queryKey: ["docs", owner.toLowerCase(), name.toLowerCase()],
    queryFn: ({ signal }) => unwrap(api.GET("/repos/{owner}/{name}/docs", { params: { path: { owner, name } }, signal })),
  })

export const docQuery = (owner: string, name: string, docId: string) =>
  queryOptions({
    queryKey: ["doc", owner.toLowerCase(), name.toLowerCase(), docId],
    queryFn: ({ signal }) =>
      unwrap(api.GET("/repos/{owner}/{name}/docs/{docId}", { params: { path: { owner, name, docId } }, signal })),
  })

export const manifestsQuery = (owner: string, name: string) =>
  queryOptions({
    queryKey: ["manifests", owner.toLowerCase(), name.toLowerCase()],
    queryFn: ({ signal }) => unwrap(api.GET("/repos/{owner}/{name}/manifests", { params: { path: { owner, name } }, signal })),
  })
