import { useSyncExternalStore } from "react"

/** BUILD_ID is the build this bundle belongs to; the server sends its own as X-SCP-Build. */
export const BUILD_ID: string = import.meta.env.VITE_BUILD_ID ?? "dev"

let newer = false
const listeners = new Set<() => void>()

/** noteBuild records the server's build; a different one means a new version is deployed. */
export function noteBuild(server: string | null) {
  if (!server || server === BUILD_ID || BUILD_ID === "dev" || newer) return
  newer = true
  for (const l of listeners) l()
}

/** useNewVersion is true once the server reports a build other than this bundle's. */
export function useNewVersion(): boolean {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => newer,
  )
}

/** reloadOnStaleChunk reloads the page once when a lazy chunk of an older build is gone. */
export function reloadOnStaleChunk(win: Window = window) {
  win.addEventListener("vite:preloadError", (event) => {
    const key = "scp-reloaded-at"
    const last = Number(win.sessionStorage.getItem(key) ?? "0")
    if (Date.now() - last < 60_000) return // reloaded a minute ago: let the error surface instead of looping
    win.sessionStorage.setItem(key, String(Date.now()))
    event.preventDefault()
    win.location.reload()
  })
}
