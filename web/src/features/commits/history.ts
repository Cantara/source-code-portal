import { keepPreviousData, queryOptions, type QueryClient } from "@tanstack/react-query"
import { unwrap, type Api, type Schemas } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"
import { snapshotQuery } from "@/lib/api/snapshot"
import { pageNumber, pageSize } from "@/lib/paging"

export type HistoryCommit = Schemas["HistoryCommit"]
export type Label = Schemas["Label"]

/** defaultHistorySize is the navigator's page size until the reader chooses one. */
export const defaultHistorySize = 100

/** HistoryFilter is what the navigator lists. */
export interface HistoryFilter {
  ref?: string
  author?: string
  q?: string
  automation?: false
}

/** HistoryState is the navigator's URL: the filter, the page and its size, and the selected commit. */
export interface HistoryState {
  filter: HistoryFilter
  /** absent: the first page, or the page holding sha */
  page?: number
  size: number
  sha?: string
}

const text = (v: unknown, max: number) => (typeof v === "string" && v.trim() !== "" ? v.trim().slice(0, max) : undefined)

/** historyState reads the navigator's URL; size falls back to the reader's remembered choice. */
export function historyState(s: Record<string, unknown>, size: number): HistoryState {
  const filter: HistoryFilter = {}
  const ref = text(s.ref, 255)
  const author = text(s.author, 100)
  const q = text(s.q, 200)
  if (ref) filter.ref = ref
  if (author) filter.author = author
  if (q) filter.q = q
  if (s.automation === "false") filter.automation = false
  const sha = text(s.sha, 40)?.toLowerCase()
  return {
    filter,
    page: s.page === undefined ? undefined : pageNumber(s.page),
    size: pageSize(s.per, size),
    sha: sha && /^[0-9a-f]{4,40}$/.test(sha) ? sha : undefined,
  }
}

/** search is the query string of a navigator state, in a fixed order; the size only when it differs from the reader's default. */
export function search(st: HistoryState, defaultSize: number): string {
  const q = new URLSearchParams()
  const f = st.filter
  if (f.ref) q.set("ref", f.ref)
  if (f.author) q.set("author", f.author)
  if (f.q) q.set("q", f.q)
  if (f.automation === false) q.set("automation", "false")
  if (st.page !== undefined && st.page > 1) q.set("page", String(st.page))
  if (st.size !== defaultSize) q.set("per", String(st.size))
  if (st.sha) q.set("sha", st.sha)
  return q.size > 0 ? `?${q}` : ""
}

/** Request is what the API is asked for: a page, or with a selected commit and no page, the page holding it. */
export interface Request {
  page: number
  at?: string
}

export function request(st: HistoryState): Request {
  return st.page === undefined && st.sha ? { page: 1, at: st.sha } : { page: st.page ?? 1 }
}

/**
 * historyQuery is one page of a repository's history. It notes the snapshot version it was read under; a page past the first stays
 * as it is on a new snapshot (meta.keepPaged) and offers to reload.
 */
export const historyQuery = (
  api: Api,
  qc: QueryClient,
  owner: string,
  name: string,
  f: HistoryFilter,
  size: number,
  r: Request,
) =>
  queryOptions({
    queryKey: keys.history(owner, name, { ...f, size, page: r.page, at: r.at }),
    queryFn: async ({ signal }) => {
      const version = (await qc.query({ ...snapshotQuery(api), staleTime: "static" }).catch(() => undefined))?.version ?? 0
      const page = await unwrap(
        api.GET("/repos/{owner}/{name}/history", {
          params: { path: { owner, name }, query: { ...f, limit: size, page: r.page, ...(r.at ? { at: r.at } : {}) } },
          signal,
        }),
      )
      return { ...page, version }
    },
    placeholderData: keepPreviousData,
    meta: { keepPaged: true },
  })
