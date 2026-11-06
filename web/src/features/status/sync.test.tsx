import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { keys } from "@/lib/api/queries"
import { snapshotKey } from "@/lib/api/snapshot"
import { http, server, snapshot } from "@/test/server"
import { refreshEvery, useSnapshotSync } from "./sync"

beforeEach(() => vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] }))
afterEach(() => vi.useRealTimers())

describe("a run of new snapshots refreshes the page's data once", () => {
  it("at once for the first, then once at the end of the interval, and never who the viewer is", async () => {
    // The page's own read of the snapshot answers with what was last published.
    let current = 1
    server.use(http.get("/snapshot", ({ response }) => response(200).json(snapshot(current))))
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const refetched = vi.spyOn(qc, "invalidateQueries")
    qc.setQueryData(snapshotKey, snapshot(1))
    qc.setQueryData(keys.me(), { signedIn: false })
    const wrap = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    const hook = renderHook(() => useSnapshotSync(), { wrapper: wrap })
    await act(async () => {})
    expect(hook.result.current?.version).toBe(1)
    const publish = (v: number) =>
      act(async () => {
        current = v
        qc.setQueryData(snapshotKey, snapshot(v))
        await vi.advanceTimersByTimeAsync(0) // the query cache tells its observers on a timer
      })

    await publish(2)
    expect(refetched).toHaveBeenCalledTimes(1)
    await publish(3)
    await publish(4)
    expect(refetched).toHaveBeenCalledTimes(1)
    await act(async () => void vi.advanceTimersByTime(refreshEvery - 1))
    expect(refetched).toHaveBeenCalledTimes(1)
    await act(async () => void vi.advanceTimersByTime(1))
    expect(refetched).toHaveBeenCalledTimes(2)

    const stays = refetched.mock.calls.map(([f]) => f?.predicate)
    for (const key of [keys.me(), snapshotKey]) {
      const held = qc.getQueryCache().find({ queryKey: key })
      expect(held).toBeDefined()
      for (const p of stays) expect(held && p?.(held)).toBe(false)
    }
  })
})
