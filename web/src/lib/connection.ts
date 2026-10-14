import { useSyncExternalStore } from "react"
import { ProblemError } from "./api/client"

// The connection is failing after a network, 429 or 5xx failure and recovers on the next successful request.
let failing = false
const listeners = new Set<() => void>()

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
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => failing,
  )
}
