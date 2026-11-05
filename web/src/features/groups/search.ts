/** SortKey is a column of the group's repository table. */
export const sortKeys = ["name", "release", "from", "licence", "issues", "last", "findings", "health"] as const
export type SortKey = (typeof sortKeys)[number]

export interface GroupSearch {
  q?: string
  sort?: SortKey
  desc?: boolean
}

/** validateGroupSearch keeps the known view state of the group page from the URL. */
export function validateGroupSearch(s: Record<string, unknown>): GroupSearch {
  const out: GroupSearch = {}
  if (typeof s.q === "string" && s.q !== "") out.q = s.q.slice(0, 100)
  if (typeof s.sort === "string" && (sortKeys as readonly string[]).includes(s.sort)) out.sort = s.sort as SortKey
  if (s.desc === true || s.desc === "true") out.desc = true
  return out
}
