"use client"

import { useEffect, useRef, useState } from "react"
import { useNewVersion } from "@/lib/build"
import { useConnectionFailing } from "@/lib/connection"

/** reloadEvery is how often the page loads itself again: a page request reseals a signed-in viewer's session (design/auth.md). */
const reloadEvery = 60 * 60_000

/** checkEvery is how often a due reload is tried; never while the portal cannot be reached, as a reload would lose the page. */
const checkEvery = 60_000

/** shiftEvery is how often the page moves against burn-in. */
const shiftEvery = 10 * 60_000

/** offsets are the burn-in shifts, at most 2 pixels each way, as classes (the CSP allows no style attribute). */
const offsets = [
  "",
  "translate-x-px",
  "translate-x-0.5 translate-y-px",
  "translate-y-0.5",
  "-translate-x-px translate-y-px",
  "-translate-x-0.5",
  "-translate-y-px",
]

/**
 * useWall keeps a screen on a wall going on its own: the screen awake while the page is visible (the Screen Wake Lock API, where the
 * browser has it), the page reloaded when a new release is deployed and once an hour, never while the portal cannot be reached.
 */
export function useWall() {
  const newer = useNewVersion()
  const failing = useConnectionFailing()

  useEffect(() => {
    if (!("wakeLock" in navigator)) return
    let lock: WakeLockSentinel | undefined
    let gone = false
    const ask = () => {
      if (document.visibilityState !== "visible" || (lock && !lock.released)) return // one lock at a time: a second would be orphaned
      navigator.wakeLock.request("screen").then(
        (l) => {
          if (gone) void l.release()
          else lock = l
        },
        () => undefined, // refused (no gesture yet, low battery): the screen sleeps as usual
      )
    }
    ask()
    document.addEventListener("visibilitychange", ask)
    // Safari grants the lock only after a user gesture: the first tap asks again.
    document.addEventListener("pointerdown", ask, { once: true })
    return () => {
      gone = true
      document.removeEventListener("visibilitychange", ask)
      document.removeEventListener("pointerdown", ask)
      void lock?.release()
    }
  }, [])

  useEffect(() => {
    if (newer && !failing) window.location.reload()
  }, [newer, failing])

  const down = useRef(failing)
  useEffect(() => {
    down.current = failing
  }, [failing])
  useEffect(() => {
    const start = Date.now()
    const t = setInterval(() => {
      if (Date.now() - start >= reloadEvery && !down.current) window.location.reload()
    }, checkEvery)
    return () => clearInterval(t)
  }, [])
}

/** useShift is the page's burn-in offset, a class that changes every 10 minutes (OLED tablets and TVs). */
export function useShift(): string {
  const [i, setI] = useState(0)
  useEffect(() => {
    const t = setInterval(() => setI((n) => (n + 1) % offsets.length), shiftEvery)
    return () => clearInterval(t)
  }, [])
  return offsets[i] ?? ""
}
