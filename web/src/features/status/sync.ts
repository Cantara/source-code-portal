import { useQuery, useQueryClient, type Query } from "@tanstack/react-query"
import { useEffect, useRef } from "react"
import { snapshotKey, snapshotQuery } from "@/lib/api/snapshot"

/** keep says whether a query stays as it is on a new snapshot: a paged log with more than one page loaded. */
export function keep(q: Query): boolean {
  if (q.queryKey[0] === snapshotKey[0]) return true
  const pages = (q.state.data as { pages?: unknown[] } | undefined)?.pages
  return q.meta?.keepWhenPaged === true && (pages?.length ?? 0) > 1
}

/** useSnapshotSync polls the snapshot; a new version invalidates every other query, so the active ones refetch. */
export function useSnapshotSync() {
  const qc = useQueryClient()
  const { data } = useQuery(snapshotQuery)
  const seen = useRef<number | undefined>(undefined)
  const version = data?.version
  useEffect(() => {
    if (version === undefined) return
    if (seen.current !== undefined && seen.current !== version) {
      void qc.invalidateQueries({ predicate: (q) => !keep(q) })
    }
    seen.current = version
  }, [qc, version])
  return data
}
