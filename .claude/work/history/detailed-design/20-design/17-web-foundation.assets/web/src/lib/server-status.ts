import { useSyncExternalStore } from "react"

export type ServerStatus = {
  /** true while the last poll succeeded */
  reachable: boolean
  /** the server answered with a build id different from the one compiled into this bundle */
  buildSkew: boolean
  /** the server reported readiness "warming" or "degraded" (snapshot endpoint) */
  readiness: "warming" | "ready" | "degraded" | "unknown"
  lastOkAt: number | null
}

let state: ServerStatus = { reachable: true, buildSkew: false, readiness: "unknown", lastOkAt: null }
const listeners = new Set<() => void>()
function set(next: Partial<ServerStatus>) {
  state = { ...state, ...next }
  for (const l of listeners) l()
}
export const serverStatus = {
  get: (): ServerStatus => state,
  subscribe: (l: () => void) => {
    listeners.add(l)
    return () => listeners.delete(l)
  },
  reportOk(now = Date.now()) {
    set({ reachable: true, lastOkAt: now })
  },
  reportFailure() {
    set({ reachable: false })
  },
  reportBuild(serverBuild: string | null, bundleBuild = __BUILD_ID__) {
    if (serverBuild && bundleBuild !== "dev" && serverBuild !== bundleBuild) set({ buildSkew: true })
  },
  reportReadiness(r: ServerStatus["readiness"]) {
    set({ readiness: r })
  },
  reset() {
    state = { reachable: true, buildSkew: false, readiness: "unknown", lastOkAt: null }
    for (const l of listeners) l()
  },
}
export function useServerStatus(): ServerStatus {
  return useSyncExternalStore(serverStatus.subscribe, serverStatus.get, serverStatus.get)
}
