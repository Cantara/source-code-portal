import { queryOptions } from "@tanstack/react-query"
import { api, unwrap } from "@/lib/api/client"
import { qk } from "@/lib/api/query-keys"

/** The composite repository resource: header, metadata, docs index, statuses, technology summary, latest commits. */
export const repoQuery = (repo: string) =>
  queryOptions({
    queryKey: qk.repo(repo),
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/repos/{repo}", { params: { path: { repo } }, signal })),
    staleTime: 10_000,
  })

export const docQuery = (repo: string, docId: string) =>
  queryOptions({
    queryKey: qk.docs.get(repo, docId),
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/repos/{repo}/docs/{docId}", { params: { path: { repo, docId } }, signal })),
    staleTime: 30_000,
  })
