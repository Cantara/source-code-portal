/** login reads a GitHub login from an address field: 1 to 39 letters, digits and hyphens, else nothing. */
export function login(v: unknown): string | undefined {
  return typeof v === "string" && /^[A-Za-z0-9-]{1,39}$/.test(v) ? v : undefined
}
