const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 365 * 24 * 3600],
  ["month", 30 * 24 * 3600],
  ["week", 7 * 24 * 3600],
  ["day", 24 * 3600],
  ["hour", 3600],
  ["minute", 60],
]

const rtf = new Intl.RelativeTimeFormat("en", { numeric: "auto" })
const exact = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" })
const num = new Intl.NumberFormat("en")

/** relative is "3 days ago" for at relative to now. */
export function relative(at: string | Date, now: number): string {
  const seconds = Math.round((new Date(at).getTime() - now) / 1000)
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) return rtf.format(Math.trunc(seconds / size), unit)
  }
  return "just now"
}

/** exactUTC is the instant in UTC, for titles. */
export function exactUTC(at: string | Date): string {
  return exact.format(new Date(at)) + " UTC"
}

const day = new Intl.DateTimeFormat("en", { dateStyle: "full", timeZone: "UTC" })

/** dayUTC is the day of the instant in UTC, "Wednesday, October 14, 2026". */
export function dayUTC(at: string | Date): string {
  return day.format(new Date(at))
}

export function count(n: number): string {
  return num.format(n)
}

/** plural is "1 repository" or "3 repositories". */
export function plural(n: number, one: string, many: string): string {
  return `${count(n)} ${n === 1 ? one : many}`
}

const short: [string, number][] = [
  ["y", 365 * 24 * 3600],
  ["mo", 30 * 24 * 3600],
  ["w", 7 * 24 * 3600],
  ["d", 24 * 3600],
  ["h", 3600],
  ["m", 60],
]

/** ago is the compact age of at relative to now: "3d", "5mo", "2y"; "now" under a minute. */
export function ago(at: string | Date, now: number): string {
  const seconds = Math.max(0, Math.round((now - new Date(at).getTime()) / 1000))
  for (const [unit, size] of short) {
    if (seconds >= size) return `${Math.floor(seconds / size)}${unit}`
  }
  return "now"
}
