import { queryOptions } from "@tanstack/react-query"
import { unwrap, type Api } from "@/lib/api/client"
import { keys } from "@/lib/api/queries"

export const groupCommitsQuery = (api: Api, group: string) =>
  queryOptions({
    queryKey: keys.latestCommits(group),
    queryFn: ({ signal }) => unwrap(api.GET("/commits", { params: { query: { group, limit: 10 } }, signal })),
  })

/** groupPulls is what the group's pull requests panel reads: the five whose turn it is first. */
export const groupPulls = (group: string) => ({ group, size: 5 })

/** groupReleases is what the group's releases panel reads: the latest stable release of each repository, the five newest. */
export const groupReleases = (group: string) => ({ group, size: 5 })

/** groupRetired is what the group's retired link reads: one row, for the count of its retired repositories. */
export const groupRetired = (group: string) => ({ group, size: 1 })

/** groupIssues is what the group's issues panel reads: the five that wait longest. */
export const groupIssues = (group: string) => ({ group, size: 5 })
