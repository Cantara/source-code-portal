import { useSyncExternalStore } from "react"
import { i18n } from "@/lib/i18n"

// The UI language decides the format, not the browser's: an English UI never shows "for 5 minutter siden" to a Norwegian browser.
const uiLocale = () => i18n.locale || "en"
const rtfs = new Map<string, Intl.RelativeTimeFormat>()
function rtf(locale: string): Intl.RelativeTimeFormat {
  let f = rtfs.get(locale)
  if (!f) rtfs.set(locale, (f = new Intl.RelativeTimeFormat(locale, { numeric: "auto" })))
  return f
}
const UNITS: readonly [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 31_536_000],
  ["month", 2_592_000],
  ["day", 86_400],
  ["hour", 3_600],
  ["minute", 60],
  ["second", 1],
]
export function formatRelative(iso: string, now: number, locale: string = uiLocale()): string {
  const seconds = Math.round((Date.parse(iso) - now) / 1000)
  const abs = Math.abs(seconds)
  const f = rtf(locale)
  if (abs < 45) return f.format(0, "second") // "now"
  for (const [unit, size] of UNITS) if (abs >= size) return f.format(Math.trunc(seconds / size), unit)
  return f.format(0, "second")
}
export function formatDateTime(iso: string, timeZone: string, locale: string = uiLocale()): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short", timeZone }).format(new Date(iso))
}

/** One shared 30 s ticker for every RelativeTime on the page. */
const tickListeners = new Set<() => void>()
let now = Date.now()
let timer: ReturnType<typeof setInterval> | null = null
function startTicker() {
  if (timer !== null) return
  now = Date.now() // the module-level value is as old as the page load; refresh it for the first subscriber
  timer = setInterval(() => {
    if (document.hidden) return // a hidden tab renders nothing; catch up on visibilitychange
    tick()
  }, 30_000)
  document.addEventListener("visibilitychange", onVisible)
}
function tick() {
  now = Date.now()
  tickListeners.forEach((l) => l())
}
function onVisible() {
  if (!document.hidden) tick()
}
function stopTicker() {
  if (timer !== null) clearInterval(timer)
  timer = null
  document.removeEventListener("visibilitychange", onVisible)
}
export function useNow(): number {
  return useSyncExternalStore(
    (l) => {
      tickListeners.add(l)
      startTicker()
      return () => {
        tickListeners.delete(l)
        if (tickListeners.size === 0) stopTicker()
      }
    },
    () => now,
    () => now,
  )
}

/** The portal's display zone (`Site.displayTimeZone`, GR-38). Set once by the shell; every RelativeTime reads it. */
let displayZone = "UTC"
const zoneListeners = new Set<() => void>()
export function setDisplayZone(zone: string): void {
  if (zone === displayZone) return
  displayZone = zone
  zoneListeners.forEach((l) => l())
}
export function useDisplayZone(): string {
  return useSyncExternalStore(
    (l) => {
      zoneListeners.add(l)
      return () => zoneListeners.delete(l)
    },
    () => displayZone,
    () => "UTC",
  )
}
