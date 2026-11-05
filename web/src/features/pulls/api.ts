import type { PullFilter } from "@/lib/api/queries"
import { pageNumber, pageSize } from "@/lib/paging"
import { pullStates, type PullState } from "@/entities/pull/pull"

/** defaultPullSize is the page size of /pulls until the reader chooses one. */
export const defaultPullSize = 50

/** PullsState is /pulls's URL: its filter, the page and its size. */
export interface PullsState {
  filter: PullFilter
  page: number
  size: number
}

const sorts = ["waiting", "newest", "updated"] as const

/** pullsState reads /pulls's URL; size falls back to the reader's remembered choice. */
export function pullsState(s: Record<string, unknown>, size: number): PullsState {
  const f: PullFilter = {}
  if (typeof s.group === "string" && s.group !== "") f.group = s.group.slice(0, 100)
  if (typeof s.repo === "string" && s.repo !== "") f.repo = s.repo.slice(0, 200).toLowerCase()
  if (typeof s.state === "string" && pullStates.some((x) => x.state === s.state)) f.state = s.state as PullState
  if (s.by === "people" || s.by === "automation") f.by = s.by
  if (s.drafts === "true") f.drafts = true
  if (typeof s.sort === "string" && (sorts as readonly string[]).includes(s.sort) && s.sort !== "waiting")
    f.sort = s.sort as PullFilter["sort"]
  return { filter: f, page: pageNumber(s.page), size: pageSize(s.per, size) }
}

/** pullsSearch is the query string of a state; the size only when it differs from the reader's default. */
export function pullsSearch(st: PullsState, defaultSize: number): string {
  const q = new URLSearchParams()
  const f = st.filter
  if (f.group) q.set("group", f.group)
  if (f.repo) q.set("repo", f.repo)
  if (f.state) q.set("state", f.state)
  if (f.by) q.set("by", f.by)
  if (f.drafts) q.set("drafts", "true")
  if (f.sort && f.sort !== "waiting") q.set("sort", f.sort)
  if (st.page > 1) q.set("page", String(st.page))
  if (st.size !== defaultSize) q.set("per", String(st.size))
  return q.size > 0 ? `?${q}` : ""
}

/** repoPulls is what a repository's panel reads: its ten open pull requests whose turn it is first, drafts included. */
export const repoPulls = (owner: string, name: string): PullFilter => ({
  repo: `${owner}/${name}`.toLowerCase(),
  drafts: true,
  size: 10,
})
