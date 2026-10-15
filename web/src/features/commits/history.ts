import { infiniteQueryOptions, type QueryClient } from "@tanstack/react-query"
import { unwrap, type Api, type Schemas } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"
import { snapshotQuery } from "@/lib/api/snapshot"

export type HistoryCommit = Schemas["HistoryCommit"]
export type Label = Schemas["Label"]

export const historyPageSize = 100

/** HistoryFilter is the navigator's filter in the URL; cursor is where the first page starts (the "Older commits" link). */
export interface HistoryFilter {
  ref?: string
  author?: string
  q?: string
  automation?: false
  cursor?: string
}

const text = (v: unknown, max: number) => (typeof v === "string" && v.trim() !== "" ? v.trim().slice(0, max) : undefined)

/** historyFilter keeps the navigator's known filters from the URL. */
export function historyFilter(s: Record<string, unknown>): HistoryFilter {
  const out: HistoryFilter = {}
  const ref = text(s.ref, 255)
  const author = text(s.author, 100)
  const q = text(s.q, 200)
  const cursor = text(s.cursor, 500)
  if (ref) out.ref = ref
  if (author) out.author = author
  if (q) out.q = q
  if (s.automation === "false") out.automation = false
  if (cursor) out.cursor = cursor
  return out
}

/** search is the query string of a filter (and a selected commit), in a fixed order. */
export function search(f: HistoryFilter, sha?: string): string {
  const q = new URLSearchParams()
  if (f.ref) q.set("ref", f.ref)
  if (f.author) q.set("author", f.author)
  if (f.q) q.set("q", f.q)
  if (f.automation === false) q.set("automation", "false")
  if (f.cursor) q.set("cursor", f.cursor)
  if (sha) q.set("sha", sha)
  return q.size > 0 ? `?${q}` : ""
}

/** historyQuery pages through a repository's history from the filter's cursor, kept on a new snapshot once paged (as the log). */
export const historyQuery = (api: Api, qc: QueryClient, owner: string, name: string, f: HistoryFilter) =>
  infiniteQueryOptions({
    queryKey: keys.history(owner, name, { ...f }),
    queryFn: async ({ pageParam, signal }) => {
      const version = (await qc.query({ ...snapshotQuery(api), staleTime: "static" }).catch(() => undefined))?.version ?? 0
      const query = { ref: f.ref, author: f.author, q: f.q, automation: f.automation }
      const page = await unwrap(
        api.GET("/repos/{owner}/{name}/history", {
          params: {
            path: { owner, name },
            query: { ...query, limit: historyPageSize, ...(pageParam ? { cursor: pageParam } : {}) },
          },
          signal,
        }),
      )
      return { ...page, version }
    },
    initialPageParam: f.cursor ?? "",
    getNextPageParam: (last) => last.page.nextCursor ?? undefined,
    meta: { keepWhenPaged: true },
  })
