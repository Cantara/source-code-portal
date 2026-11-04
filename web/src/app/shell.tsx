"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react"
import { buttonClass } from "@/components/ui/button"
import { BookIcon, CheckIcon, ChevronDownIcon, MonitorIcon, MoonIcon, SlidersIcon, SunIcon } from "@/components/ui/icons"
import { cn } from "@/lib/cn"
import type { Schemas } from "@/lib/api/client"
import { apply, useTheme, type Choice } from "@/lib/theme"

const themes = [
  { value: "system", label: "System", Icon: MonitorIcon },
  { value: "dark", label: "Dark", Icon: MoonIcon },
  { value: "light", label: "Light", Icon: SunIcon },
] as const

/**
 * ThemeMenu picks system, dark or light (lib/theme.ts, public/theme-init.js): a button with the chosen mode's icon opens a menu of the
 * three, the chosen one checked. Arrows, Home and End move in it, Enter or a click chooses, Escape, Tab or a click elsewhere closes it.
 */
export function ThemeMenu() {
  const theme = useTheme()
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLUListElement>(null)
  const id = useId()
  const chosen = themes.find((t) => t.value === theme) ?? themes[0]

  useEffect(() => {
    if (!open) return
    menu.current?.querySelector<HTMLElement>('[aria-checked="true"]')?.focus()
    const away = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener("pointerdown", away)
    return () => document.removeEventListener("pointerdown", away)
  }, [open])

  const close = () => {
    setOpen(false)
    button.current?.focus()
  }
  const choose = (value: Choice) => {
    apply(value)
    close()
  }
  const move = (e: KeyboardEvent<HTMLUListElement>) => {
    const items = [...(menu.current?.querySelectorAll<HTMLElement>('[role="menuitemradio"]') ?? [])]
    const at = items.indexOf(document.activeElement as HTMLElement)
    const to = { ArrowDown: at + 1, ArrowUp: at - 1, Home: 0, End: items.length - 1 }[e.key]
    if (to !== undefined) {
      e.preventDefault()
      items[(to + items.length) % items.length]?.focus()
    } else if (e.key === "Escape") {
      e.preventDefault()
      close()
    } else if (e.key === "Tab") setOpen(false)
  }

  return (
    <div ref={root} className="relative">
      <button
        ref={button}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-label={`Theme: ${chosen.label}`}
        title={`Theme: ${chosen.label}`}
        onClick={() => setOpen((o) => !o)}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" || e.key === "ArrowUp") {
            e.preventDefault()
            setOpen(true)
          }
        }}
        className={cn(
          "flex h-8 items-center gap-0.5 rounded-md px-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
          open && "bg-accent text-foreground",
        )}
      >
        <chosen.Icon className="size-[1.125rem]" />
        <ChevronDownIcon className="size-3" />
      </button>
      {open && (
        <ul
          ref={menu}
          id={id}
          role="menu"
          aria-label="Theme"
          onKeyDown={move}
          className="absolute top-full right-0 z-50 mt-1.5 flex w-36 flex-col rounded-lg border border-border bg-popover/95 p-1 text-sm text-popover-foreground shadow-xl backdrop-blur-md"
        >
          {themes.map(({ value, label, Icon }) => (
            <li key={value} role="none">
              <button
                type="button"
                role="menuitemradio"
                aria-checked={theme === value}
                tabIndex={-1}
                onClick={() => choose(value)}
                className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:bg-accent focus-visible:text-foreground aria-checked:text-foreground"
              >
                <Icon className="size-4" />
                <span className="flex-1 text-left">{label}</span>
                {theme === value && <CheckIcon className="size-4 text-primary" />}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

const tool =
  "grid size-8 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground aria-[current=page]:bg-accent aria-[current=page]:text-foreground"

/** PortalNav is the reader's own corner of the header: the Guide, the theme and Settings, as icons named for assistive technology. */
export function PortalNav() {
  const path = usePathname()
  return (
    <nav aria-label="Portal" className="flex items-center gap-0.5">
      <Link href="/guide" className={tool} title="Guide" aria-current={path === "/guide" ? "page" : undefined}>
        <BookIcon className="size-[1.125rem]" />
        <span className="sr-only">Guide</span>
      </Link>
      <ThemeMenu />
      <Link href="/settings" className={tool} title="Settings" aria-current={path === "/settings" ? "page" : undefined}>
        <SlidersIcon className="size-[1.125rem]" />
        <span className="sr-only">Settings</span>
      </Link>
    </nav>
  )
}

const nav =
  "rounded-md px-2.5 py-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground aria-[current=page]:bg-accent aria-[current=page]:font-medium aria-[current=page]:text-foreground"

/** MainNav is the header navigation, with Ask when the viewer may use the assistant; the current section is marked. */
export function MainNav({ ask = false }: { ask?: boolean }) {
  const path = usePathname()
  return (
    <nav aria-label="Main" className="flex gap-1 text-sm">
      <Link href="/" className={nav} aria-current={path === "/" ? "page" : undefined}>
        Dashboard
      </Link>
      <Link href="/commits" className={nav} aria-current={path === "/commits" ? "page" : undefined}>
        Commits
      </Link>
      {ask && (
        <Link href="/ask" className={nav} aria-current={path === "/ask" ? "page" : undefined}>
          Ask
        </Link>
      )}
    </nav>
  )
}

/**
 * ViewerMenu is the header's viewer: the signed-in name with Admin and Sign out, or Sign in back to this page when the portal has
 * sign-in. A viewer named by the proxy signs out there.
 */
export function ViewerMenu({ me }: { me: Schemas["Me"] | undefined }) {
  const path = usePathname()
  if (!me) return null
  if (me.signedIn)
    return (
      <div className="flex items-center gap-1 text-sm">
        <span className="max-w-40 truncate px-1" title={`Signed in as ${me.name}`}>
          {me.name}
        </span>
        {me.admin && (
          <Link href="/admin" className={nav} aria-current={path === "/admin" ? "page" : undefined}>
            Admin
          </Link>
        )}
        {me.via !== "proxy" && (
          <form method="post" action="/auth/logout">
            <button type="submit" className={buttonClass("ghost", "min-h-8 px-2.5")}>
              Sign out
            </button>
          </form>
        )}
      </div>
    )
  if ((!me.signIn.oidc && !me.signIn.token) || path === "/signin") return null
  return (
    <Link href={`/signin?return_to=${encodeURIComponent(path)}`} className={nav}>
      Sign in
    </Link>
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
