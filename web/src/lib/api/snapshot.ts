import { queryOptions, useQuery } from "@tanstack/react-query"
import { api, unwrap } from "./client"

export const snapshotKey = ["snapshot"] as const

/** snapshotQuery reads the snapshot; its version identifies the data. The shell keeps it current (features/status/sync.ts). */
export const snapshotQuery = queryOptions({
  queryKey: snapshotKey,
  queryFn: ({ signal }) => unwrap(api.GET("/snapshot", { signal })),
  staleTime: 0,
})

/** useSnapshotVersion is the version of the last snapshot seen, 0 before the first answer. */
export function useSnapshotVersion(): number {
  return useQuery(snapshotQuery).data?.version ?? 0
}
