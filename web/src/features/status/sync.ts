import { useQuery, useQueryClient, type Query } from "@tanstack/react-query"
import { useEffect, useRef, useState } from "react"
import { api } from "@/lib/api/client"
import { setLive } from "@/lib/connection"
import type { components } from "@/lib/api/schema"
import { snapshotKey, snapshotQuery } from "@/lib/api/snapshot"
import { keys } from "@/lib/api/queries"

type SnapshotInfo = components["schemas"]["SnapshotInfo"]

/** pollInterval is how often the snapshot is read while the event stream is not open. */
export const pollInterval = 30_000

/** refreshEvery is the least time between two refreshes of the page's data: a run of snapshots (sync jobs finishing) reads it once. */
export const refreshEvery = 15_000

/** keep says whether a query stays as it is on a new snapshot: the snapshot itself, who the viewer is (no snapshot changes that), a page of a paged list past the first, which offers to reload. */
export function keep(q: Query): boolean {
  if (q.queryKey[0] === snapshotKey[0] || q.queryKey[0] === keys.me()[0]) return true
  const number = (q.state.data as { page?: { number?: number } } | undefined)?.page?.number
  return q.meta?.keepPaged === true && (number ?? 1) > 1
}

/**
 * useLiveSnapshot listens to /api/v1/events: every snapshot event replaces the snapshot query's data. It reports whether the
 * stream is open; the browser reconnects after an error.
 */
function useLiveSnapshot(): boolean {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  useEffect(() => setLive(open), [open])
  useEffect(() => {
    if (typeof EventSource === "undefined") return
    const source = new EventSource("/api/v1/events")
    source.onopen = () => setOpen(true)
    source.onerror = () => setOpen(false)
    source.addEventListener("snapshot", (e) => {
      qc.setQueryData(snapshotKey, JSON.parse((e as MessageEvent<string>).data) as SnapshotInfo)
    })
    return () => source.close()
  }, [qc])
  return open
}

/**
 * useSnapshotSync keeps the snapshot current: live while the event stream is open, else by polling. A new version invalidates
 * every other query, so the active ones refetch: at once when the last refresh is older than refreshEvery, else once at its end,
 * so a burst of snapshots is one refresh, not one each.
 */
export function useSnapshotSync() {
  const qc = useQueryClient()
  const live = useLiveSnapshot()
  const { data } = useQuery({
    ...snapshotQuery(api),
    refetchInterval: live ? false : pollInterval,
    refetchIntervalInBackground: false,
  })
  const seen = useRef<number | undefined>(undefined)
  const last = useRef(-Infinity)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const version = data?.version
  useEffect(() => {
    if (version === undefined) return
    const was = seen.current
    seen.current = version
    if (was === undefined || was === version || timer.current !== undefined) return // a refresh is already waiting: it reads the newest
    const refresh = () => {
      timer.current = undefined
      last.current = Date.now()
      void qc.invalidateQueries({ predicate: (q) => !keep(q) })
    }
    const wait = last.current + refreshEvery - Date.now()
    if (wait <= 0) refresh()
    else timer.current = setTimeout(refresh, wait)
  }, [qc, version])
  useEffect(() => () => clearTimeout(timer.current), [])
  return data
}
