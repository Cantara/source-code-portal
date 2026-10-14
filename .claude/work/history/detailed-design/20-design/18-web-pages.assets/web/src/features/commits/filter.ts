import type { components } from "@/lib/api/schema"
type Commit = components["schemas"]["Commit"]

/** Case-insensitive match on subject, author name, author login, repo slug and sha prefix. Empty query keeps everything. */
export function matchesCommit(c: Commit, q: string): boolean {
  const n = q.trim().toLowerCase()
  if (n === "") return true
  return (
    c.subject.toLowerCase().includes(n) ||
    (c.authorName ?? "").toLowerCase().includes(n) ||
    (c.authorLogin ?? "").toLowerCase().includes(n) ||
    c.repo.toLowerCase().includes(n) ||
    c.sha.startsWith(n)
  )
}

/** Identity of a commit in a list: `repo@sha` (GR-07: a fork or mirror shares shas, so the sha alone is not a key). */
export const commitKey = (c: Pick<Commit, "repo" | "sha">): string => `${c.repo}@${c.sha}`

/** Number of commits strictly newer than the newest loaded one: drives the "N new commits" pill (head query, limit 50). Compared by `repo@sha` (V-06). */
export function countNewer(head: readonly Commit[], loadedFirst: Pick<Commit, "repo" | "sha"> | undefined): number | "many" {
  if (loadedFirst === undefined) return 0
  const want = commitKey(loadedFirst)
  const i = head.findIndex((c) => commitKey(c) === want)
  if (i === -1) return head.length === 0 ? 0 : "many" // the loaded head fell out of the window (or was rewritten)
  return i
}
