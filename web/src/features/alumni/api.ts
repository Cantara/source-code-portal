import type { AlumniFilter } from "@/lib/api/queries"
import { pageNumber, pageSize } from "@/lib/paging"

/** defaultAlumniSize is the page size of /alumni until the reader chooses one. */
export const defaultAlumniSize = 50

/** AlumniState is /alumni's URL: its filter, the page and its size. */
export interface AlumniState {
  filter: AlumniFilter
  page: number
  size: number
}

export const alumniSorts = [
  { value: "active", label: "Last active first" },
  { value: "name", label: "Name" },
  { value: "group", label: "Product" },
  { value: "used", label: "Most depended on first" },
] as const

/** alumniState reads /alumni's URL; size falls back to the reader's remembered choice. */
export function alumniState(s: Record<string, unknown>, size: number): AlumniState {
  const f: AlumniFilter = {}
  if (typeof s.group === "string" && s.group !== "") f.group = s.group.slice(0, 100)
  if (typeof s.q === "string" && s.q.trim() !== "") f.q = s.q.trim().slice(0, 100)
  if (s.reason === "archived" || s.reason === "deprecated") f.reason = s.reason
  if (s.used === "true") f.used = true
  const sort = alumniSorts.find((x) => x.value === s.sort)
  if (sort && sort.value !== "active") f.sort = sort.value
  return { filter: f, page: pageNumber(s.page), size: pageSize(s.per, size) }
}

/** alumniSearch is the query string of a state; the size only when it differs from the reader's default. */
export function alumniSearch(st: AlumniState, defaultSize: number): string {
  const q = new URLSearchParams()
  const f = st.filter
  if (f.group) q.set("group", f.group)
  if (f.q) q.set("q", f.q)
  if (f.reason) q.set("reason", f.reason)
  if (f.used) q.set("used", "true")
  if (f.sort && f.sort !== "active") q.set("sort", f.sort)
  if (st.page > 1) q.set("page", String(st.page))
  if (st.size !== defaultSize) q.set("per", String(st.size))
  return q.size > 0 ? `?${q}` : ""
}
