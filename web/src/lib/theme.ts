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

/** apply stores the choice and sets the dark class and colour scheme on the document. */
export function apply(choice: Choice) {
  try {
    if (choice === "system") window.localStorage.removeItem(themeKey)
    else window.localStorage.setItem(themeKey, choice)
  } catch {
    // storage blocked: the choice lasts for this page only
  }
  const dark = isDark(choice, window.matchMedia("(prefers-color-scheme: dark)").matches)
  document.documentElement.classList.toggle("dark", dark)
  document.documentElement.style.colorScheme = dark ? "dark" : "light"
  current = choice
  for (const l of listeners) l()
}

let current: Choice | undefined

export function useTheme(): Choice {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => (current ??= stored()),
  )
}
