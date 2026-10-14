import { queryOptions, keepPreviousData } from "@tanstack/react-query"
import { api, unwrap } from "@/lib/api/client"
import { qk } from "@/lib/api/query-keys"
import type { GroupSearch } from "./search"

export const groupQuery = (groupId: string) =>
  queryOptions({
    queryKey: qk.catalog.group(groupId),
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/groups/{groupId}", { params: { path: { groupId } }, signal })),
    staleTime: 10_000,
  })

export const groupReposQuery = (groupId: string, s: GroupSearch) =>
  queryOptions({
    queryKey: ["catalog", "group-repos", groupId, s] as const,
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/api/v1/groups/{groupId}/repos", {
          params: {
            path: { groupId },
            query: {
              ...(s.q ? { q: s.q } : {}), ...(s.lifecycle ? { lifecycle: s.lifecycle } : {}), ...(s.type ? { type: s.type } : {}),
              ...(s.tag ? { tag: s.tag } : {}), archived: s.archived, sort: s.sort,
            },
          },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
    staleTime: 10_000,
  })

/** Every visible repository (the palette's repository list; one page, the API default limit is 1,000 and a catalogue larger than that is out of scope for v1). */
export const allReposQuery = () =>
  queryOptions({
    queryKey: ["catalog", "repos", "all"] as const,
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/repos", { params: { query: { sort: "name" } }, signal })),
    staleTime: 60_000,
  })
