import { useSyncExternalStore } from "react"

// One ticker for every relative time: it fires every 30 s while the page is visible.
const period = 30_000
let now = Date.now()
const listeners = new Set<() => void>()
let timer: ReturnType<typeof setInterval> | undefined

function tick() {
  now = Date.now()
  for (const l of listeners) l()
}

function visibility() {
  if (document.visibilityState === "visible") {
    tick()
    timer ??= setInterval(tick, period)
  } else {
    clearInterval(timer)
    timer = undefined
  }
}

function subscribe(l: () => void) {
  if (listeners.size === 0) {
    document.addEventListener("visibilitychange", visibility)
    visibility()
  }
  listeners.add(l)
  return () => {
    listeners.delete(l)
    if (listeners.size === 0) {
      document.removeEventListener("visibilitychange", visibility)
      clearInterval(timer)
      timer = undefined
    }
  }
}

/** useNow is the time of the last tick, in milliseconds. */
export function useNow(): number {
  return useSyncExternalStore(subscribe, () => now)
}
