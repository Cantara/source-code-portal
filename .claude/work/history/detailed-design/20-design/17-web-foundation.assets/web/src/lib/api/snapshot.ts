import { queryOptions, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query"
import { useEffect, useRef } from "react"
import { api, unwrap } from "./client"
import { qk, topicRoots, type Topic } from "./query-keys"
import { serverStatus } from "@/lib/server-status"

export const SNAPSHOT_POLL_MS = 30_000

export const snapshotQuery = () =>
  queryOptions({
    queryKey: qk.snapshot(),
    queryFn: ({ signal }) => unwrap(api.GET("/api/v1/snapshot", { signal })),
    refetchInterval: SNAPSHOT_POLL_MS,
    staleTime: 0,
    retry: false,
  })

export function invalidateAll(qc: QueryClient) {
  return qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== "snapshot" })
}
export function invalidateTopics(qc: QueryClient, topics: readonly Topic[]) {
  const roots = new Set(topics.flatMap((t) => topicRoots[t]))
  return qc.invalidateQueries({ predicate: (q) => roots.has(String(q.queryKey[0])) })
}

/** Mounted once in the shell. Polls the snapshot version (browser ETag makes it a 304 when unchanged) and invalidates on change. */
export function useSnapshotSync(): void {
  const qc = useQueryClient()
  const q = useQuery(snapshotQuery())
  const last = useRef<number | null>(null)
  const version = q.data?.version
  useEffect(() => {
    if (q.data) {
      serverStatus.reportReadiness(q.data.readiness)
      serverStatus.reportBuild(q.data.buildId)
    }
    if (version === undefined) return
    if (last.current !== null && last.current !== version) void invalidateAll(qc)
    last.current = version
  }, [version, q.data, qc])
}
