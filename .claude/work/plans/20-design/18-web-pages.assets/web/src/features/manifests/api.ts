import { infiniteQueryOptions, keepPreviousData, queryOptions } from "@tanstack/react-query"
import { api, unwrap } from "@/lib/api/client"
import { ProblemError } from "@/lib/api/problem"
import type { OrgDepsSearch, PackageSearch, RepoDepsSearch } from "./search"

export const repoManifestsQuery = (repo: string) =>
  queryOptions({
    queryKey: ["manifests", "repo", repo] as const,
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/repos/{repo}/manifests", { params: { path: { repo } }, signal })),
    staleTime: 30_000,
  })

export const repoDepsQuery = (repo: string, s: RepoDepsSearch, pageSize = 100) =>
  infiniteQueryOptions({
    queryKey: ["manifests", "deps", repo, s, pageSize] as const,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      unwrap(
        api.GET("/api/v1/repos/{repo}/dependencies", {
          params: {
            path: { repo },
            query: {
              ...(s.scope ? { scope: s.scope } : {}), relationship: s.relationship,
              ...(s.internal !== undefined ? { internal: s.internal } : {}), ...(s.ecosystem ? { ecosystem: s.ecosystem } : {}),
              ...(s.unit ? { unit: s.unit } : {}), ...(s.q ? { q: s.q } : {}), sort: s.sort, limit: pageSize,
              ...(pageParam ? { cursor: pageParam } : {}),
            },
          },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.page.nextCursor ?? undefined,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  })

/** The operation answers application/json or (format=mermaid) text/plain; this call always asks for json. A string body is a contract violation:
 *  a ProblemError `client-contract` (-> server-error page, manual retry), NOT a bare TypeError (V-03: problemView would have called that "offline"). */
export function assertGraphBody<T>(d: T | string): T {
  if (typeof d === "string") throw new ProblemError({ status: 0, code: "client-contract", title: "dependency graph: expected JSON, got text" })
  return d
}

export const graphQuery = (s: OrgDepsSearch) =>
  queryOptions({
    queryKey: ["manifests", "graph", { group: s.group ?? null, focus: s.focus ?? null, depth: s.depth, ecosystem: s.ecosystem ?? null, scope: s.scope ?? null, intra: s.intra }] as const,
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/api/v1/dependencies/graph", {
          params: {
            query: {
              ...(s.group ? { group: s.group } : {}), ...(s.scope ? { scope: s.scope } : {}), ...(s.ecosystem ? { ecosystem: s.ecosystem } : {}),
              intra: s.intra, internal: true, ...(s.focus ? { focus: s.focus, depth: s.depth } : {}), format: "json",
            },
          },
          signal,
        }),
      ).then(assertGraphBody),
    placeholderData: keepPreviousData,
    staleTime: 60_000,
  })

export const versionsQuery = (s: OrgDepsSearch, pageSize = 50) =>
  infiniteQueryOptions({
    queryKey: ["manifests", "versions", { group: s.group ?? null, ecosystem: s.ecosystem ?? null, q: s.q ?? null, origin: s.origin, minVersions: s.minVersions }, pageSize] as const,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      unwrap(
        api.GET("/api/v1/dependencies/versions", {
          params: {
            query: {
              ...(s.group ? { group: s.group } : {}), ...(s.ecosystem ? { ecosystem: s.ecosystem } : {}),
              ...(s.origin !== "all" ? { internal: s.origin === "internal" } : {}), ...(s.q ? { q: s.q } : {}),
              minVersions: s.minVersions, limit: pageSize, ...(pageParam ? { cursor: pageParam } : {}),
            },
          },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.page.nextCursor ?? undefined,
    placeholderData: keepPreviousData,
    staleTime: 60_000,
  })

export const licensesQuery = (group: string | undefined) =>
  queryOptions({
    queryKey: ["manifests", "licenses", { group: group ?? null }] as const,
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/dependencies/licenses", { params: { query: group ? { group } : {} }, signal })),
    staleTime: 60_000,
  })

export const componentQuery = (purl: string) =>
  queryOptions({
    queryKey: ["manifests", "component", purl] as const,
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/components", { params: { query: { purl } }, signal })),
    staleTime: 60_000,
  })

export const dependentsQuery = (s: PackageSearch & { purl: string }, pageSize = 50) =>
  infiniteQueryOptions({
    queryKey: ["manifests", "dependents", s.purl, { scope: s.scope ?? null, group: s.group ?? null }, pageSize] as const,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      unwrap(
        api.GET("/api/v1/components/dependents", {
          params: { query: { purl: s.purl, ...(s.scope ? { scope: s.scope } : {}), ...(s.group ? { group: s.group } : {}), limit: pageSize, ...(pageParam ? { cursor: pageParam } : {}) } },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.page.nextCursor ?? undefined,
    staleTime: 60_000,
  })
