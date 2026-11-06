import type { ReleaseFilter } from "@/lib/api/queries"
import { pageNumber, pageSize } from "@/lib/paging"

/** defaultReleaseSize is the page size of /releases until the reader chooses one. */
export const defaultReleaseSize = 50

/** ReleasesState is /releases's URL: its filter, the page and its size. */
export interface ReleasesState {
  filter: ReleaseFilter
  page: number
  size: number
}

export const releaseVariants = [
  { value: "latest", label: "Latest per repository" },
  { value: "all", label: "Every release" },
  { value: "stable", label: "Stable" },
  { value: "pre", label: "Pre-releases" },
] as const

export const releaseSorts = [
  { value: "newest", label: "Newest first" },
  { value: "oldest", label: "Oldest first" },
  { value: "repo", label: "Repository" },
  { value: "group", label: "Product" },
] as const

/** releaseWindows are the windows of the form; the address may carry any `Nd` or RFC 3339 time the API accepts. */
export const releaseWindows = [
  { value: "", label: "Any time" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "90d", label: "Last 90 days" },
  { value: "365d", label: "Last year" },
] as const

const since = /^(\d{1,5}d|\d{4}-\d{2}-\d{2}T[\d:.]+(Z|[+-]\d{2}:\d{2}))$/

/** releasesState reads /releases's URL; size falls back to the reader's remembered choice. */
export function releasesState(s: Record<string, unknown>, size: number): ReleasesState {
  const f: ReleaseFilter = {}
  if (typeof s.group === "string" && s.group !== "") f.group = s.group.slice(0, 100)
  if (typeof s.repo === "string" && s.repo !== "") f.repo = s.repo.slice(0, 200).toLowerCase()
  const variant = releaseVariants.find((x) => x.value === s.variant)
  if (variant && variant.value !== "latest") f.variant = variant.value
  if (typeof s.since === "string" && since.test(s.since)) f.since = s.since
  const sort = releaseSorts.find((x) => x.value === s.sort)
  if (sort && sort.value !== "newest") f.sort = sort.value
  return { filter: f, page: pageNumber(s.page), size: pageSize(s.per, size) }
}

/** releasesSearch is the query string of a state; the size only when it differs from the reader's default. */
export function releasesSearch(st: ReleasesState, defaultSize: number): string {
  const q = new URLSearchParams()
  const f = st.filter
  if (f.group) q.set("group", f.group)
  if (f.repo) q.set("repo", f.repo)
  if (f.variant && f.variant !== "latest") q.set("variant", f.variant)
  if (f.since) q.set("since", f.since)
  if (f.sort && f.sort !== "newest") q.set("sort", f.sort)
  if (st.page > 1) q.set("page", String(st.page))
  if (st.size !== defaultSize) q.set("per", String(st.size))
  return q.size > 0 ? `?${q}` : ""
}

/** repoReleases is what a repository's panel reads: its ten newest releases, pre-releases included. */
export const repoReleases = (owner: string, name: string): ReleaseFilter => ({
  repo: `${owner}/${name}`.toLowerCase(),
  variant: "all",
  size: 10,
})
