// Pure day grouping of a UTC-ordered commit list in an explicit IANA zone (design 09 section 4.9; GR-38).
const formatters = new Map<string, Intl.DateTimeFormat>()
function fmt(zone: string): Intl.DateTimeFormat {
  let f = formatters.get(zone)
  if (!f) {
    f = new Intl.DateTimeFormat("en-CA", { timeZone: zone, year: "numeric", month: "2-digit", day: "2-digit" })
    formatters.set(zone, f)
  }
  return f
}

/** "2026-06-16" for the calendar day of `iso` in `zone`. Throws RangeError for an invalid zone (validated at startup by the API). */
export function dayKey(iso: string, zone: string): string {
  return fmt(zone).format(new Date(iso))
}

export interface DayGroup<T> { readonly day: string; readonly items: readonly T[] }

/** Single pass: the API order is strictly time-descending, so equal days are adjacent. Does not mutate `items`. */
export function groupByDay<T extends { readonly committedAt: string }>(items: readonly T[], zone: string): DayGroup<T>[] {
  const out: { day: string; items: T[] }[] = []
  for (const it of items) {
    const day = dayKey(it.committedAt, zone)
    const last = out[out.length - 1]
    if (last && last.day === day) last.items.push(it)
    else out.push({ day, items: [it] })
  }
  return out
}

/** Previous calendar day of "YYYY-MM-DD" by date arithmetic on the three numbers (never "minus 24 h": DST days are 23 or 25 h). */
export function previousDay(day: string): string {
  const [y, m, d] = day.split("-").map(Number) as [number, number, number]
  const t = new Date(Date.UTC(y, m - 1, d - 1))
  return `${t.getUTCFullYear().toString().padStart(4, "0")}-${String(t.getUTCMonth() + 1).padStart(2, "0")}-${String(t.getUTCDate()).padStart(2, "0")}`
}

export type DayLabel = { kind: "today" } | { kind: "yesterday" } | { kind: "date"; day: string }
export function dayLabel(day: string, now: Date, zone: string): DayLabel {
  const today = dayKey(now.toISOString(), zone)
  if (day === today) return { kind: "today" }
  if (day === previousDay(today)) return { kind: "yesterday" }
  return { kind: "date", day }
}
