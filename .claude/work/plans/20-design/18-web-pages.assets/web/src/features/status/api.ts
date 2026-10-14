import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"
import { api, unwrap } from "@/lib/api/client"
import { qk } from "@/lib/api/query-keys"

/** Admin listener only. Polled every 10 s while the tab is visible (TanStack default: refetchIntervalInBackground=false); ETag = body hash, so an idle portal answers 304. */
export const STATUS_POLL_MS = 10_000
export const statusQuery = () =>
  queryOptions({
    queryKey: qk.status(),
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/status", { signal })),
    refetchInterval: STATUS_POLL_MS,
    staleTime: 5_000,
  })

/** POST /api/v1/admin/jobs/retry: needs Content-Type: application/json (openapi-fetch sets it for a JSON body) and X-SCP-Request: 1. */
export function useRetryJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (key: string) => unwrap(api.POST("/api/v1/admin/jobs/retry", { params: { header: { "X-SCP-Request": "1" } }, body: { key } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.status() }),
  })
}
