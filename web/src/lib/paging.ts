/** pageSizes are the page sizes a reader can choose. */
export const pageSizes = [25, 50, 100, 200] as const

/** PagedList names a list whose page size the reader's browser remembers, in the cookie `scp-per-<list>`. */
export type PagedList = "commits" | "history"

export const sizeCookie = (list: PagedList) => `scp-per-${list}`

/** pageSize is v when it is one of the page sizes, else fallback. */
export function pageSize(v: unknown, fallback: number): number {
  const n = typeof v === "string" ? Number(v) : v
  return pageSizes.includes(n as (typeof pageSizes)[number]) ? (n as number) : fallback
}

/** pageNumber is v as a page number (1 to 100000), else 1. */
export function pageNumber(v: unknown): number {
  const n = typeof v === "string" && /^\d{1,6}$/.test(v) ? Number(v) : NaN
  return Number.isInteger(n) && n >= 1 && n <= 100_000 ? n : 1
}

/** rememberSize keeps the reader's page size for a list for a year. */
export function rememberSize(list: PagedList, size: number) {
  const secure = location.protocol === "https:" ? "; Secure" : ""
  document.cookie = `${sizeCookie(list)}=${size}; Path=/; Max-Age=31536000; SameSite=Lax${secure}`
}

/** firstPageOf is the page of the new size that holds the first item of page `page` of the old size, so the reader stays put. */
export function firstPageOf(page: number, oldSize: number, newSize: number): number {
  return Math.floor(((page - 1) * oldSize) / newSize) + 1
}

/**
 * pageItems are the page numbers a pager shows: the first and the last, the current with two either side, and "gap" where pages
 * are left out; a gap of one page shows that page instead.
 */
export function pageItems(current: number, pages: number): (number | "gap")[] {
  const shown = new Set([1, pages])
  for (let n = current - 2; n <= current + 2; n++) if (n >= 1 && n <= pages) shown.add(n)
  const sorted = [...shown].sort((a, b) => a - b)
  const out: (number | "gap")[] = []
  sorted.forEach((n, i) => {
    const prev = sorted[i - 1]
    if (prev !== undefined && n - prev === 2) out.push(n - 1)
    else if (prev !== undefined && n - prev > 2) out.push("gap")
    out.push(n)
  })
  return out
}
