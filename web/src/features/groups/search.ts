export type SortKey = "name" | "pushed"

export interface GroupSearch {
  q?: string
  sort?: SortKey
  desc?: boolean
}

/** validateGroupSearch keeps the known view state of the group page from the URL. */
export function validateGroupSearch(s: Record<string, unknown>): GroupSearch {
  const out: GroupSearch = {}
  if (typeof s.q === "string" && s.q !== "") out.q = s.q.slice(0, 100)
  if (s.sort === "name" || s.sort === "pushed") out.sort = s.sort
  if (s.desc === true || s.desc === "true") out.desc = true
  return out
}
