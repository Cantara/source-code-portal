import type { Schemas } from "./api/client"
import { docPath, groupCommitsPath, groupPath, repoCommitsPath, repoPath } from "./paths"

/** Crumb is one step of a page's trail: its name and its path. The page header draws the trail, and the JSON-LD BreadcrumbList is it. */
export interface Crumb {
  name: string
  path: string
}

const dashboard: Crumb = { name: "Dashboard", path: "/" }

/** Parent is the group a repository's trail passes through: the first of its groups (RepoDetail.group); none when it is in none. */
type Parent = Schemas["GroupRef"] | null | undefined

/** The trails of the pages: the dashboard, then each page above, the page itself last. The dashboard has none. */
export const crumbs = {
  commits: (): Crumb[] => [dashboard, { name: "Commits", path: "/commits" }],
  group: (id: string, name: string): Crumb[] => [dashboard, { name, path: groupPath(id) }],
  groupCommits: (id: string, name: string): Crumb[] => [
    ...crumbs.group(id, name),
    { name: "Commits", path: groupCommitsPath(id) },
  ],
  repo: (owner: string, name: string, group: Parent): Crumb[] => [
    ...(group ? crumbs.group(group.id, group.name) : [dashboard]),
    { name: `${owner}/${name}`, path: repoPath(owner, name) },
  ],
  repoCommits: (owner: string, name: string, group: Parent): Crumb[] => [
    ...crumbs.repo(owner, name, group),
    { name: "Commits", path: repoCommitsPath(owner, name) },
  ],
  doc: (owner: string, name: string, group: Parent, path: string, title: string): Crumb[] => [
    ...crumbs.repo(owner, name, group),
    { name: title, path: docPath(owner, name, path) },
  ],
}
