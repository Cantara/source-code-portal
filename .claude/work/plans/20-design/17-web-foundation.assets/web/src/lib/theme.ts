import { useSyncExternalStore } from "react"
import { readPersisted, writePersisted } from "./persist"
import { STORAGE } from "./storage-keys"

export type ThemePreference = "light" | "dark" | "system"
const isPref = (v: string | null): v is ThemePreference => v === "light" || v === "dark" || v === "system"

const listeners = new Set<() => void>()
const notify = () => listeners.forEach((l) => l())

export function getPreference(): ThemePreference {
  const stored = readPersisted(STORAGE.theme)
  if (isPref(stored)) return stored
  const def = readPersisted(STORAGE.themeDefault)
  return def === "light" || def === "dark" ? def : "system"
}
export function resolve(pref: ThemePreference): "light" | "dark" {
  if (pref !== "system") return pref
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"
}
/** Applies the class and color-scheme exactly as public/theme-init.js does (CSSOM writes only: CSP-safe). */
export function apply(pref: ThemePreference = getPreference()): void {
  const r = resolve(pref)
  const root = document.documentElement
  root.classList.toggle("dark", r === "dark")
  root.style.colorScheme = r
  root.setAttribute("data-theme", pref)
  notify()
}
export function setPreference(pref: ThemePreference): void {
  writePersisted(STORAGE.theme, pref)
  apply(pref)
}
/** Called with api.Site.defaultTheme. Remembered so the next page load is flash-free. */
export function applyServerDefault(def: "system" | "light" | "dark"): void {
  writePersisted(STORAGE.themeDefault, def)
  if (readPersisted(STORAGE.theme) === null) apply()
}
export function watchSystemTheme(): () => void {
  const mq = window.matchMedia("(prefers-color-scheme: dark)")
  const h = () => {
    if (getPreference() === "system") apply()
  }
  mq.addEventListener("change", h)
  return () => mq.removeEventListener("change", h)
}
export function useThemePreference(): ThemePreference {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    getPreference,
    () => "system",
  )
}
