import { infiniteQueryOptions, type QueryClient } from "@tanstack/react-query"
import { unwrap, type Api } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"
import { snapshotQuery } from "@/lib/api/snapshot"

export const pageSize = 50

export interface Filter {
  group?: string
  repo?: string
}

/**
 * commitLogQuery pages through commits. Each page notes the snapshot version it was read under; with more than one page loaded the
 * log is kept on a new snapshot (meta.keepWhenPaged) and the page offers to reload it.
 */
export const commitLogQuery = (api: Api, qc: QueryClient, f: Filter) =>
  infiniteQueryOptions({
    queryKey: keys.commitLog(f.group, f.repo),
    queryFn: async ({ pageParam, signal }) => {
      const version = (await qc.query({ ...snapshotQuery(api), staleTime: "static" }).catch(() => undefined))?.version ?? 0
      const page = await unwrap(
        api.GET("/commits", {
          params: { query: { ...f, limit: pageSize, ...(pageParam ? { cursor: pageParam } : {}) } },
          signal,
        }),
      )
      return { ...page, version }
    },
    initialPageParam: "",
    getNextPageParam: (last) => last.page.nextCursor ?? undefined,
    meta: { keepWhenPaged: true },
  })

/** commitFilter keeps the known filters of the commit log from the URL. */
export function commitFilter(s: Record<string, unknown>): Filter {
  const out: Filter = {}
  if (typeof s.group === "string" && s.group !== "") out.group = s.group.slice(0, 100)
  if (typeof s.repo === "string" && s.repo !== "") out.repo = s.repo.slice(0, 200)
  return out
}
