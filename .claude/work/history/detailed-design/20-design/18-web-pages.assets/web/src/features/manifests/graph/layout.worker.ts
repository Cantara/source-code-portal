/// <reference lib="webworker" />
import { layout, type LayoutRequest, type LayoutResponse } from "./layout"

self.onmessage = (m: MessageEvent<LayoutRequest>) => {
  const { key, input } = m.data
  try {
    const t0 = performance.now()
    const positions = layout(input)
    const res: LayoutResponse = { type: "layout", key, positions, ms: performance.now() - t0 }
    ;(self as DedicatedWorkerGlobalScope).postMessage(res, [positions.buffer]) // transfer, no copy
  } catch (e) {
    const err: LayoutResponse = { type: "error", key, message: e instanceof Error ? e.message : "layout failed" }
    ;(self as DedicatedWorkerGlobalScope).postMessage(err)
  }
}
