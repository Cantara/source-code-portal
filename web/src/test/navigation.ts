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

// Next.js follows history.replaceState into useSearchParams; so does the stand-in.
const replaceState = window.history.replaceState.bind(window.history)
window.history.replaceState = (data, unused, url) => {
  replaceState(data, unused, url)
  if (url !== undefined && url !== null) setLocation(String(url))
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

/** NavigationSignal is what notFound and permanentRedirect throw in tests, as Next.js throws its own signals. */
export class NavigationSignal extends Error {
  constructor(
    readonly kind: "not-found" | "redirect",
    readonly to = "",
  ) {
    super(kind === "redirect" ? `redirect to ${to}` : "not found")
  }
}

export function notFound(): never {
  throw new NavigationSignal("not-found")
}

export function permanentRedirect(to: string): never {
  throw new NavigationSignal("redirect", to)
}
