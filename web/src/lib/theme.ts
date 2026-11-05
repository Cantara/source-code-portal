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

/**
 * onWall reports whether this page is the presentation page on a screen told it is on a wall (`?profile=wall`, or the cookie
 * scp-present-wall unless `?profile=auto`), as public/theme-init.js reads it.
 */
export function onWall(): boolean {
  if (window.location.pathname !== "/present") return false
  const ask = new URLSearchParams(window.location.search).get("profile")
  return ask === "wall" || (ask !== "auto" && /(?:^|;\s*)scp-present-wall=1(?:;|$)/.test(document.cookie))
}

/**
 * paint sets the dark class and colour scheme on the document from the current choice and the system preference; a wall is dark
 * unless the reader chose (design/present.md), as public/theme-init.js paints it.
 */
export function paint() {
  const wall = onWall()
  const dark = isDark(current ?? stored(), wall || window.matchMedia(systemQuery).matches)
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
