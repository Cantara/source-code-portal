import { useSyncExternalStore } from "react"

export type Choice = "system" | "light" | "dark"
export const themeKey = "scp-theme"

/** stored reads the choice like public/theme-init.js: anything but light or dark, or no storage, is system. */
export function stored(storage: Pick<Storage, "getItem"> | undefined = safeStorage()): Choice {
  try {
    const v = storage?.getItem(themeKey)
    return v === "light" || v === "dark" ? v : "system"
  } catch {
    return "system"
  }
}

/** isDark resolves a choice with the system preference. */
export function isDark(choice: Choice, systemDark: boolean): boolean {
  return choice === "dark" || (choice === "system" && systemDark)
}

function safeStorage(): Storage | undefined {
  try {
    return window.localStorage
  } catch {
    return undefined
  }
}

const listeners = new Set<() => void>()
const systemQuery = "(prefers-color-scheme: dark)"

/** apply stores the choice and paints it. */
export function apply(choice: Choice) {
  try {
    if (choice === "system") window.localStorage.removeItem(themeKey)
    else window.localStorage.setItem(themeKey, choice)
  } catch {
    // storage blocked: the choice lasts for this page only
  }
  current = choice
  paint()
  for (const l of listeners) l()
}

/** paint sets the dark class and colour scheme on the document from the current choice and the system preference. */
function paint() {
  const dark = isDark(current ?? stored(), window.matchMedia(systemQuery).matches)
  document.documentElement.classList.toggle("dark", dark)
  document.documentElement.style.colorScheme = dark ? "dark" : "light"
}

let current: Choice | undefined

/** useTheme is the current choice; while it is mounted, the system choice follows the system preference as it changes. */
export function useTheme(): Choice {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      const media = window.matchMedia(systemQuery)
      media.addEventListener("change", paint)
      return () => {
        listeners.delete(l)
        media.removeEventListener("change", paint)
      }
    },
    () => (current ??= stored()),
    () => "system",
  )
}
