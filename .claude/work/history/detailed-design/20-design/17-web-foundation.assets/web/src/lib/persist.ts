export function readPersisted(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}
/** Returns false when the value could not be stored (private window, blocked site data, full quota); never throws. */
export function writePersisted(key: string, value: string | null): boolean {
  try {
    if (value === null) window.localStorage.removeItem(key)
    else window.localStorage.setItem(key, value)
    return true
  } catch {
    return false // the page works without storage
  }
}
