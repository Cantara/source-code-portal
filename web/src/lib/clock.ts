import { createContext, createElement, useContext, useSyncExternalStore, type ReactNode } from "react"

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

// The server's instant on the tick grid, so repeated reads in one render agree (layout.tsx passes the same instant).
const serverNow = () => Math.floor(Date.now() / period) * period

// The instant the server rendered at: hydration renders against it, so a level or label that depends on the time cannot differ
// between the server's HTML and the first client render.
const RenderedAt = createContext<number | undefined>(undefined)

/** NowProvider passes the server's render instant to the browser's first render. */
export function NowProvider({ at, children }: { at: number; children: ReactNode }) {
  return createElement(RenderedAt, { value: at }, children)
}

/** useNow is the time of the last tick, in milliseconds; the server's render instant during server rendering and hydration. */
export function useNow(): number {
  const at = useContext(RenderedAt)
  return useSyncExternalStore(
    subscribe,
    () => now,
    () => at ?? serverNow(),
  )
}
