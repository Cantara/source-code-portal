import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query"
import { api, unwrap } from "@/lib/api/client"

/** The log stops offering "Load more" at this many rows and points at the filters instead (DOM size, reading value). */
export const MAX_LOG_ROWS = 1000

export interface CommitScope { readonly repo?: string | undefined; readonly group?: string | undefined }

const q = (s: CommitScope, limit: number, cursor?: string) => ({
  ...(s.repo ? { repo: s.repo } : {}), ...(s.group ? { group: s.group } : {}), limit, ...(cursor ? { cursor } : {}),
})

/** Below this scroll offset (px) the log follows the head again; above it no refetch may insert rows under the reader (design 4.4). */
export const FOLLOW_SCROLL_Y = 200

/**
 * The log: keyset pages, "Load more" button (no infinite scroll). Key root `commits` = invalidation topic.
 * `follow` is the V-13 mechanism: unit 17 invalidates EVERY query on a snapshot change and refetches every active one every 120 s (`SWEEP_MS`), and an
 * infinite query refetches all loaded pages from the first, so the newest commits would be inserted above the row being read. A DISABLED query is not
 * refetched by invalidation (it is only marked stale) and keeps its data; the component passes `follow = window.scrollY <= FOLLOW_SCROLL_Y`, so the
 * refetch happens when the reader is back at the top (enabled flips to true on a stale query) or on the explicit click of the "N new commits" pill (`refetch()`).
 */
export const commitLogQuery = (s: CommitScope, pageSize = 50, follow = true) =>
  infiniteQueryOptions({
    enabled: follow,
    queryKey: ["commits", "log", { repo: s.repo ?? null, group: s.group ?? null, pageSize }] as const,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => unwrap(api.GET("/api/v1/commits", { params: { query: q(s, pageSize, pageParam) }, signal })),
    getNextPageParam: (last) => last.page.nextCursor ?? undefined,
    staleTime: 10_000,
  })

/** Head probe for the "new commits" pill: only the newest 50, polled with the snapshot version, never rendered as rows. */
export const commitHeadQuery = (s: CommitScope) =>
  queryOptions({
    queryKey: ["commits", "head", { repo: s.repo ?? null, group: s.group ?? null }] as const,
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/commits", { params: { query: q(s, 50) }, signal })),
    staleTime: 10_000,
  })
