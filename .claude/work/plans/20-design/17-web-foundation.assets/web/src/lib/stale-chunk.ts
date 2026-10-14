import { readPersisted, writePersisted } from "./persist"
import { STORAGE } from "./storage-keys"

export const RELOAD_GUARD_MS = 30_000

/**
 * A deploy replaces every hashed chunk; an open tab then fails to load a lazy route and Vite dispatches `vite:preloadError`.
 * Reload once. Never loop: the reload time is remembered in storage, and when it cannot be remembered (blocked storage)
 * the page does not reload at all. Returns true when the event was handled by a reload.
 *
 * `preventDefault()` is called only when reloading: Vite rethrows the original error unless the event is cancelled, and a
 * cancelled event makes the failed `import()` resolve to `undefined`, which would surface as an unrelated TypeError.
 */
export function onPreloadError(event: Event, reload: () => void = () => window.location.reload(), now = Date.now()): boolean {
  const last = Number(readPersisted(STORAGE.reloadGuard) ?? "0")
  if (now - last <= RELOAD_GUARD_MS) return false
  if (!writePersisted(STORAGE.reloadGuard, String(now))) return false
  event.preventDefault()
  reload()
  return true
}
