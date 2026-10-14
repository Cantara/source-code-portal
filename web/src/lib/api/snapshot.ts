import { queryOptions, useQuery } from "@tanstack/react-query"
import { api, unwrap } from "./client"

export const snapshotKey = ["snapshot"] as const

/** snapshotQuery polls the snapshot every 30 s while the page is visible; its version changes with every new snapshot. */
export const snapshotQuery = queryOptions({
  queryKey: snapshotKey,
  queryFn: ({ signal }) => unwrap(api.GET("/snapshot", { signal })),
  refetchInterval: 30_000,
  refetchIntervalInBackground: false,
  staleTime: 0,
})

/** useSnapshotVersion is the version of the last snapshot seen, 0 before the first answer. */
export function useSnapshotVersion(): number {
  return useQuery(snapshotQuery).data?.version ?? 0
}
