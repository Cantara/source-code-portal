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

/** silenceLimit is how long a stream may be silent before it is taken for dead: the server pings every 25 s. */
export const silenceLimit = 65_000

/** The wait before the stream is opened again after the browser gave up on it: doubled from the first, to the last. */
export const retryFirst = 2_000
export const retryLast = 60_000

/**
 * useLiveSnapshot listens to /api/v1/events: every snapshot event replaces the snapshot query's data. It reports whether the
 * stream is open, and keeps it so: the browser reconnects after a drop, but not after an error answer (a full server, a proxy's 502),
 * and nothing tells it a stream that went silent without closing (a proxy that holds the connection, a sleeping laptop). So the
 * server's ping is the proof of life: no word for silenceLimit and the stream is opened again; a closed one is opened again with a
 * growing wait; coming back to the tab or the network opens it at once, and the first event is the current snapshot. Meanwhile the
 * snapshot is polled (useSnapshotSync).
 */
function useLiveSnapshot(): boolean {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  useEffect(() => setLive(open), [open])
  useEffect(() => {
    if (typeof EventSource === "undefined") return
    let source: EventSource | undefined
    let timer: ReturnType<typeof setTimeout> | undefined
    let failures = 0
    let stopped = false
    const wait = (ms: number, then: () => void) => {
      clearTimeout(timer)
      timer = setTimeout(then, ms)
    }
    const close = () => {
      clearTimeout(timer)
      source?.close()
      source = undefined
    }
    const connect = () => {
      close()
      if (stopped) return
      const s = new EventSource("/api/v1/events")
      source = s
      const alive = () => wait(silenceLimit, connect)
      s.onopen = () => {
        failures = 0
        setOpen(true)
        alive()
      }
      s.onerror = () => {
        setOpen(false)
        if (s.readyState === EventSource.CLOSED) wait(Math.min(retryLast, retryFirst * 2 ** failures++), connect)
        else alive() // the browser is retrying: if it never gets through, start over
      }
      s.addEventListener("snapshot", (e) => {
        alive()
        qc.setQueryData(snapshotKey, JSON.parse((e as MessageEvent<string>).data) as SnapshotInfo)
      })
      s.addEventListener("ping", alive)
    }
    const back = () => {
      if (document.visibilityState === "visible") connect()
    }
    connect()
    document.addEventListener("visibilitychange", back)
    window.addEventListener("online", back)
    return () => {
      stopped = true
      close()
      document.removeEventListener("visibilitychange", back)
      window.removeEventListener("online", back)
    }
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
