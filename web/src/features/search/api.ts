import { keepPreviousData, queryOptions } from "@tanstack/react-query"
import { unwrap, type Api, type Schemas } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"
import { pageNumber } from "@/lib/paging"

export type Kind = Schemas["SearchKind"]
export type Hit = Schemas["SearchHit"]
export type TextPart = Schemas["TextPart"]

/** kinds are what the search finds, in the order of its tabs: the plural for a tab, the singular for a hit. */
export const kinds: { kind: Kind; tab: string; one: string }[] = [
  { kind: "repo", tab: "Repositories", one: "Repository" },
  { kind: "group", tab: "Groups", one: "Group" },
  { kind: "doc", tab: "Documents", one: "Document" },
  { kind: "dep", tab: "Dependencies", one: "Manifest" },
  { kind: "package", tab: "Packages", one: "Package" },
  { kind: "commit", tab: "Commits", one: "Commit" },
  { kind: "pull", tab: "Pull requests", one: "Pull request" },
  { kind: "issue", tab: "Issues", one: "Issue" },
  { kind: "release", tab: "Releases", one: "Release" },
]

/** searchSize is the hits on a page. */
export const searchSize = 25

/** maxQuery is the longest query the portal searches, in characters (internal/search.MaxQuery). */
export const maxQuery = 256

/** SearchState is the search page's URL: the query, the kind it shows and the page. */
export interface SearchState {
  q: string
  in?: Kind
  page: number
}

const isKind = (v: unknown): v is Kind => kinds.some((k) => k.kind === v)

/** searchState reads the search page's URL. */
export function searchState(s: Record<string, unknown>): SearchState {
  const q = typeof s.q === "string" ? s.q.trim().slice(0, 1024) : ""
  return { q, page: pageNumber(s.page), ...(isKind(s.in) && { in: s.in }) }
}

/** searchHref is the search page of a state, its parameters in a fixed order; page 1 and every kind are left out. */
export function searchHref(st: SearchState): string {
  const p = new URLSearchParams({ q: st.q })
  if (st.in) p.set("in", st.in)
  if (st.page > 1) p.set("page", String(st.page))
  return `/search?${p.toString()}`
}

/** searchQuery is one page of a query's hits. */
export const searchQuery = (api: Api, st: SearchState) =>
  queryOptions({
    queryKey: keys.search(st.q, st.in ?? "", st.page),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/search", {
          params: { query: { q: st.q, page: st.page, limit: searchSize, ...(st.in && { in: st.in }) } },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
    retry: false,
  })

/** suggestQuery is what the search box offers for a query being typed. */
export const suggestQuery = (api: Api, q: string) =>
  queryOptions({
    queryKey: keys.suggest(q),
    queryFn: ({ signal }) => unwrap(api.GET("/search/suggest", { params: { query: { q } }, signal })),
    staleTime: 30_000,
    retry: false,
  })
