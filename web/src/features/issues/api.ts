import type { IssueFilter } from "@/lib/api/queries"
import { pageNumber, pageSize } from "@/lib/paging"
import { issueKinds, issueStates, type IssueKind, type IssueState } from "@/entities/issue/issue"

/** defaultIssueSize is the page size of /issues until the reader chooses one. */
export const defaultIssueSize = 50

/** IssuesState is /issues's URL: its filter, the page and its size. */
export interface IssuesState {
  filter: IssueFilter
  page: number
  size: number
}

const sorts = ["waiting", "newest", "updated"] as const

/** issuesState reads /issues's URL; size falls back to the reader's remembered choice. */
export function issuesState(s: Record<string, unknown>, size: number): IssuesState {
  const f: IssueFilter = {}
  if (typeof s.group === "string" && s.group !== "") f.group = s.group.slice(0, 100)
  if (typeof s.repo === "string" && s.repo !== "") f.repo = s.repo.slice(0, 200).toLowerCase()
  if (typeof s.state === "string" && issueStates.some((x) => x.state === s.state)) f.state = s.state as IssueState
  if (typeof s.kind === "string" && issueKinds.some((x) => x.kind === s.kind)) f.kind = s.kind as IssueKind
  if (s.by === "people" || s.by === "automation") f.by = s.by
  if (typeof s.sort === "string" && (sorts as readonly string[]).includes(s.sort) && s.sort !== "waiting")
    f.sort = s.sort as IssueFilter["sort"]
  return { filter: f, page: pageNumber(s.page), size: pageSize(s.per, size) }
}

/** issuesSearch is the query string of a state; the size only when it differs from the reader's default. */
export function issuesSearch(st: IssuesState, defaultSize: number): string {
  const q = new URLSearchParams()
  const f = st.filter
  if (f.group) q.set("group", f.group)
  if (f.repo) q.set("repo", f.repo)
  if (f.state) q.set("state", f.state)
  if (f.kind) q.set("kind", f.kind)
  if (f.by) q.set("by", f.by)
  if (f.sort && f.sort !== "waiting") q.set("sort", f.sort)
  if (st.page > 1) q.set("page", String(st.page))
  if (st.size !== defaultSize) q.set("per", String(st.size))
  return q.size > 0 ? `?${q}` : ""
}

/** repoIssues is what a repository's panel reads: its ten open issues, what waits first. */
export const repoIssues = (owner: string, name: string): IssueFilter => ({ repo: `${owner}/${name}`.toLowerCase(), size: 10 })

/** issuesPath is /issues for a repository or a group. */
export const issuesPath = (f: { repo?: string; group?: string }) =>
  `/issues${f.repo ? `?repo=${encodeURIComponent(f.repo.toLowerCase())}` : f.group ? `?group=${encodeURIComponent(f.group)}` : ""}`
