/** Only web URLs become links: `javascript:`, `data:` and relative or malformed values render as plain text. */
export function isWebUrl(href: string): boolean {
  try {
    const p = new URL(href).protocol
    return p === "https:" || p === "http:"
  } catch {
    return false
  }
}
