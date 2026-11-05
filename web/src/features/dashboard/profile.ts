"use client"

import { useEffect, useState, useSyncExternalStore } from "react"
import { paint } from "@/lib/theme"
import { forget, profileCookie, profileOf, remember, wallCookie, type Profile } from "@/lib/preferences"

/** size is the window's size as one value the store compares, "2560x1440". */
const size = () => `${window.innerWidth}x${window.innerHeight}`

function onResize(changed: () => void) {
  window.addEventListener("resize", changed)
  return () => window.removeEventListener("resize", changed)
}

/**
 * useProfile is the presentation page's profile (P-64): found from the window as it is and as it changes (a tablet turned, a
 * window resized), a wall when this screen was told it is one. The server renders the profile the page last found (guess, the
 * cookie scp-present-profile), so the first paint has that profile's column; the page remembers what it finds for the next.
 * ask is the address's `?profile=`: wall tells this screen it is on a wall, auto forgets it.
 */
export function useProfile(guess: Profile, told: boolean, ask?: string) {
  const [wall, setWall] = useState(ask === "wall" || (ask !== "auto" && told))
  const seen = useSyncExternalStore(onResize, size, () => null)
  const [w = 0, h = 0] = seen ? seen.split("x").map(Number) : []
  const profile: Profile = seen ? profileOf(w, h, wall) : wall ? "wall" : guess
  useEffect(() => {
    if (wall) remember(wallCookie, "1")
    else forget(wallCookie)
    paint() // a wall is dark unless the reader chose; a tablet or a desk follows the system
  }, [wall])
  useEffect(() => {
    if (seen) remember(profileCookie, profile)
  }, [seen, profile])
  return { profile, wall, setWall, size: seen ? seen.replace("x", "×") : undefined }
}
