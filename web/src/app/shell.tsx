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
        className="min-h-8 rounded-md border border-border bg-card px-2 text-sm"
        aria-label="Theme"
      >
        <option value="system">System</option>
        <option value="light">Light</option>
        <option value="dark">Dark</option>
      </select>
    </label>
  )
}

const nav =
  "rounded-md px-2.5 py-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground aria-[current=page]:bg-accent aria-[current=page]:font-medium aria-[current=page]:text-foreground"

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

/** Mark is the portal's logo: a node graph in a hexagon. */
export function Mark() {
  return (
    <svg viewBox="0 0 32 32" aria-hidden="true" className="size-7 text-primary drop-shadow-[0_0_6px_var(--glow)]">
      <path d="M16 2.5 27.7 9.25v13.5L16 29.5 4.3 22.75V9.25Z" fill="none" stroke="currentColor" strokeWidth="1.6" />
      <path d="M16 9v7m0 0-6 4m6-4 6 4" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      <circle cx="16" cy="9" r="2.2" fill="currentColor" />
      <circle cx="10" cy="20" r="2.2" fill="currentColor" />
      <circle cx="22" cy="20" r="2.2" fill="currentColor" />
    </svg>
  )
}
