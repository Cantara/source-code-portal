import { useSyncExternalStore } from "react"
import { ProblemError } from "./api/client"
import { subscriber } from "./subscribe"

// The connection is failing after a network, 429 or 5xx failure and recovers on the next successful request.
let failing = false
const listeners = new Set<() => void>()
const subscribe = subscriber(listeners)

function set(v: boolean) {
  if (failing === v) return
  failing = v
  for (const l of listeners) l()
}

export function noteFailure(e: unknown) {
  if (e instanceof ProblemError && e.retryable) set(true)
}

export function noteSuccess() {
  set(false)
}

export function useConnectionFailing(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => failing,
    () => false,
  )
}

// The live stream (/api/v1/events) is open; the header shows it.
let live = false
const liveListeners = new Set<() => void>()
const subscribeLive = subscriber(liveListeners)

export function setLive(v: boolean) {
  if (live === v) return
  live = v
  for (const l of liveListeners) l()
}

export function useLive(): boolean {
  return useSyncExternalStore(
    subscribeLive,
    () => live,
    () => false,
  )
}
