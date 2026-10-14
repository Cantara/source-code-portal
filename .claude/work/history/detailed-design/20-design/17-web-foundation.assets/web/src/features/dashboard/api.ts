import { queryOptions, type QueryClient } from "@tanstack/react-query"
import { api, unwrap } from "@/lib/api/client"
import { qk } from "@/lib/api/query-keys"

/** Route loaders fail fast (retry: false): the errorComponent shows the problem and owns the automatic retry. */
export const groupsLoader = (qc: QueryClient) => qc.ensureQueryData({ ...groupsQuery(), retry: false })

export const groupsQuery = () =>
  queryOptions({
    queryKey: qk.catalog.groups(),
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/groups", { signal })),
  })
