"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useEffect, useRef } from "react"
import { apply, useTheme, type Choice } from "@/lib/theme"

/** ThemeSelect picks system, light or dark (lib/theme.ts, public/theme-init.js). */
export function ThemeSelect() {
  const theme = useTheme()
  return (
    <label className="flex items-center gap-2 text-sm">
      <span className="sr-only sm:not-sr-only">Theme</span>
      <select
        value={theme}
        onChange={(e) => apply(e.target.value as Choice)}
        className="min-h-8 rounded-md border border-input bg-background px-2"
        aria-label="Theme"
      >
        <option value="system">System</option>
        <option value="light">Light</option>
        <option value="dark">Dark</option>
      </select>
    </label>
  )
}

const nav = "rounded-md px-2 py-1 hover:bg-accent aria-[current=page]:font-semibold aria-[current=page]:underline"

/** MainNav is the header navigation; the current section is marked. */
export function MainNav() {
  const path = usePathname()
  return (
    <nav aria-label="Main" className="flex gap-1 text-sm">
      <Link href="/" className={nav} aria-current={path === "/" ? "page" : undefined}>
        Dashboard
      </Link>
      <Link href="/commits" className={nav} aria-current={path === "/commits" ? "page" : undefined}>
        Commits
      </Link>
    </nav>
  )
}

/** FocusOnNavigation moves focus to the new page's h1 after a client navigation, so screen readers start at the page. */
export function FocusOnNavigation() {
  const path = usePathname()
  const first = useRef(path)
  useEffect(() => {
    if (path === first.current) return
    first.current = path
    document.querySelector<HTMLElement>("main h1")?.focus()
  }, [path])
  return null
}
