import { queryOptions } from "@tanstack/react-query"
import { unwrap, type Api } from "./client"

const lc = (s: string) => s.toLowerCase()

/**
 * keys are the query keys of the resources several features read; a feature's own queries build theirs here too, so the server's
 * cache and the browser's always agree on a key.
 */
export const keys = {
  dashboard: () => ["dashboard"] as const,
  repos: () => ["repos"] as const,
  repo: (owner: string, name: string) => ["repo", lc(owner), lc(name)] as const,
  docs: (owner: string, name: string) => ["docs", lc(owner), lc(name)] as const,
  doc: (owner: string, name: string, id: string) => ["doc", lc(owner), lc(name), id] as const,
  manifests: (owner: string, name: string) => ["manifests", lc(owner), lc(name)] as const,
  groups: () => ["groups"] as const,
  group: (group: string) => ["group", group] as const,
  commitLog: (group = "", repo = "") => ["commits", "log", group, repo] as const,
  latestCommits: (group: string) => ["commits", "latest", group] as const,
}

export const reposQuery = (api: Api) =>
  queryOptions({ queryKey: keys.repos(), queryFn: ({ signal }) => unwrap(api.GET("/repos", { signal })) })

export const repoQuery = (api: Api, owner: string, name: string) =>
  queryOptions({
    queryKey: keys.repo(owner, name),
    queryFn: ({ signal }) => unwrap(api.GET("/repos/{owner}/{name}", { params: { path: { owner, name } }, signal })),
  })

export const docListQuery = (api: Api, owner: string, name: string) =>
  queryOptions({
    queryKey: keys.docs(owner, name),
    queryFn: ({ signal }) => unwrap(api.GET("/repos/{owner}/{name}/docs", { params: { path: { owner, name } }, signal })),
  })

export const docQuery = (api: Api, owner: string, name: string, docId: string) =>
  queryOptions({
    queryKey: keys.doc(owner, name, docId),
    queryFn: ({ signal }) =>
      unwrap(api.GET("/repos/{owner}/{name}/docs/{docId}", { params: { path: { owner, name, docId } }, signal })),
  })

export const groupsQuery = (api: Api) =>
  queryOptions({ queryKey: keys.groups(), queryFn: ({ signal }) => unwrap(api.GET("/groups", { signal })) })

export const groupQuery = (api: Api, group: string) =>
  queryOptions({
    queryKey: keys.group(group),
    queryFn: ({ signal }) => unwrap(api.GET("/groups/{group}", { params: { path: { group } }, signal })),
  })
