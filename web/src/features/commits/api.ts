import { keepPreviousData, queryOptions, type QueryClient } from "@tanstack/react-query"
import { unwrap, type Api } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"
import { snapshotQuery } from "@/lib/api/snapshot"
import { pageNumber, pageSize } from "@/lib/paging"

/** defaultLogSize is the timeline's page size until the reader chooses one. */
export const defaultLogSize = 50

export interface Filter {
  group?: string
  repo?: string
  /** false leaves out the commits of automation */
  automation?: false
}

/** LogState is a timeline's URL: its filter, the page and its size. */
export interface LogState {
  filter: Filter
  page: number
  size: number
}

/**
 * commitLogQuery is one page of commits. It notes the snapshot version it was read under; a page past the first stays as it is on
 * a new snapshot (meta.keepPaged) and offers to reload.
 */
export const commitLogQuery = (api: Api, qc: QueryClient, { filter: f, page, size }: LogState) =>
  queryOptions({
    queryKey: keys.commitLog(f.group, f.repo, f.automation ?? true, size, page),
    queryFn: async ({ signal }) => {
      const version = (await qc.query({ ...snapshotQuery(api), staleTime: "static" }).catch(() => undefined))?.version ?? 0
      const data = await unwrap(
        api.GET("/commits", {
          params: { query: { group: f.group, repo: f.repo, automation: f.automation, limit: size, page } },
          signal,
        }),
      )
      return { ...data, version }
    },
    placeholderData: keepPreviousData,
    meta: { keepPaged: true },
  })

/** commitFilter keeps the known filters of the commit log from the URL. */
export function commitFilter(s: Record<string, unknown>): Filter {
  const out: Filter = {}
  if (typeof s.group === "string" && s.group !== "") out.group = s.group.slice(0, 100)
  if (typeof s.repo === "string" && s.repo !== "") out.repo = s.repo.slice(0, 200)
  if (s.automation === "false") out.automation = false
  return out
}

/** logState reads a timeline's URL; size falls back to the reader's remembered choice. */
export function logState(s: Record<string, unknown>, size: number): LogState {
  return { filter: commitFilter(s), page: pageNumber(s.page), size: pageSize(s.per, size) }
}

/** logSearch is the query string of a timeline state; the size only when it differs from the reader's default. */
export function logSearch(st: LogState, defaultSize: number): string {
  const q = new URLSearchParams()
  const f = st.filter
  if (f.group) q.set("group", f.group)
  if (f.repo) q.set("repo", f.repo)
  if (f.automation === false) q.set("automation", "false")
  if (st.page > 1) q.set("page", String(st.page))
  if (st.size !== defaultSize) q.set("per", String(st.size))
  return q.size > 0 ? `?${q}` : ""
}
