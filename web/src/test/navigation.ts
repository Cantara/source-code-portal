import { useSyncExternalStore } from "react"

// A stand-in for next/navigation in component tests: one in-memory location that router.push and router.replace change.
let location = new URL("http://localhost/")
const listeners = new Set<() => void>()

/** setLocation sets the address the views see. */
export function setLocation(path: string) {
  location = new URL(path, "http://localhost/")
  for (const l of listeners) l()
}

function useLocation(): URL {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => location,
  )
}

export function usePathname(): string {
  return useLocation().pathname
}

export function useSearchParams(): URLSearchParams {
  return useLocation().searchParams
}

const router = {
  push: setLocation,
  replace: setLocation,
  refresh: () => {},
  back: () => {},
  forward: () => {},
  prefetch: () => {},
}

export function useRouter() {
  return router
}
