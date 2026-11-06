import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { keys } from "@/lib/api/queries"
import { snapshotKey } from "@/lib/api/snapshot"
import { http, server, snapshot } from "@/test/server"
import { refreshEvery, retryFirst, silenceLimit, useSnapshotSync } from "./sync"

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

/** FakeSource stands in for EventSource: every one made is kept, and the test plays the server's part. */
class FakeSource {
  static all: FakeSource[] = []
  static readonly CLOSED = 2
  readyState = 0
  closed = false
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  private handlers = new Map<string, (e: MessageEvent<string>) => void>()
  constructor(readonly url: string) {
    FakeSource.all.push(this)
  }
  addEventListener(type: string, h: (e: MessageEvent<string>) => void) {
    this.handlers.set(type, h)
  }
  close() {
    this.closed = true
    this.readyState = FakeSource.CLOSED
  }
  open() {
    this.readyState = 1
    this.onopen?.()
  }
  say(type: string, data = "{}") {
    this.handlers.get(type)?.(new MessageEvent(type, { data }))
  }
}

describe("the live stream keeps itself open", () => {
  beforeEach(() => {
    FakeSource.all = []
    vi.stubGlobal("EventSource", FakeSource)
    server.use(http.get("/snapshot", ({ response }) => response(200).json(snapshot(1))))
  })
  afterEach(() => vi.unstubAllGlobals())

  const mount = async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrap = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    const hook = renderHook(() => useSnapshotSync(), { wrapper: wrap })
    await act(async () => {})
    return hook
  }
  const live = () => FakeSource.all.filter((s) => !s.closed)

  it("opens it again after silence, and a ping is proof of life", async () => {
    const hook = await mount()
    expect(FakeSource.all).toHaveLength(1)
    await act(async () => FakeSource.all[0]?.open())
    await act(async () => void vi.advanceTimersByTime(silenceLimit - 1))
    await act(async () => FakeSource.all[0]?.say("ping"))
    await act(async () => void vi.advanceTimersByTime(silenceLimit - 1))
    expect(FakeSource.all).toHaveLength(1) // the ping started the count again
    await act(async () => void vi.advanceTimersByTime(1))
    expect(FakeSource.all).toHaveLength(2)
    expect(live()).toHaveLength(1)
    hook.unmount()
    expect(live()).toHaveLength(0)
  })

  it("opens it again, with a growing wait, after the browser gave up on it", async () => {
    const hook = await mount()
    const first = FakeSource.all[0]
    await act(async () => {
      first?.close() // an error answer: the browser does not retry
      first?.onerror?.()
    })
    expect(FakeSource.all).toHaveLength(1)
    await act(async () => void vi.advanceTimersByTime(retryFirst))
    expect(FakeSource.all).toHaveLength(2)
    await act(async () => {
      FakeSource.all[1]?.close()
      FakeSource.all[1]?.onerror?.()
    })
    await act(async () => void vi.advanceTimersByTime(retryFirst))
    expect(FakeSource.all).toHaveLength(2) // the second wait is twice the first
    await act(async () => void vi.advanceTimersByTime(retryFirst))
    expect(FakeSource.all).toHaveLength(3)
    hook.unmount()
  })

  it("opens it at once when the tab comes back", async () => {
    const hook = await mount()
    await act(async () => FakeSource.all[0]?.open())
    await act(async () => void document.dispatchEvent(new Event("visibilitychange")))
    expect(FakeSource.all).toHaveLength(2)
    expect(live()).toHaveLength(1)
    hook.unmount()
  })
})
