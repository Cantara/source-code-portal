import { useSyncExternalStore } from "react"

/** BUILD_ID is the build this bundle belongs to; the server sends its own as X-SCP-Build. */
export const BUILD_ID: string = process.env.NEXT_PUBLIC_BUILD_ID ?? "dev"

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
    () => false,
  )
}
