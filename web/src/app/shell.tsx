"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react"
import {
  BookIcon,
  CheckIcon,
  ChevronDownIcon,
  LogInIcon,
  LogOutIcon,
  MonitorIcon,
  MoonIcon,
  ShieldIcon,
  SlidersIcon,
  SunIcon,
  PresentIcon,
} from "@/components/ui/icons"
import { cn } from "@/lib/cn"
import type { Schemas } from "@/lib/api/client"
import { apply, useTheme, type Choice } from "@/lib/theme"

const themes = [
  { value: "system", label: "System", Icon: MonitorIcon },
  { value: "dark", label: "Dark", Icon: MoonIcon },
  { value: "light", label: "Light", Icon: SunIcon },
] as const

/** The header's icon controls: one size, one hover, and the same mark for the current page and an open menu. */
const tool =
  "flex h-8 min-w-8 items-center justify-center gap-1 rounded-md px-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground aria-[current=page]:bg-accent aria-[current=page]:text-foreground aria-expanded:bg-accent aria-expanded:text-foreground"

const panel =
  "absolute top-full right-0 z-50 mt-1.5 rounded-lg border border-border bg-popover/95 p-1 text-sm text-popover-foreground shadow-xl backdrop-blur-md"

const item =
  "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:bg-accent focus-visible:text-foreground"

/**
 * useMenu is a header menu's behaviour: its button opens it (or an arrow key), the checked item or else the first takes the focus;
 * arrows, Home and End move, Escape closes it back to the button, and Tab or a press elsewhere closes it.
 */
function useMenu() {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLUListElement>(null)
  const id = useId()
  const items = () => [...(menu.current?.querySelectorAll<HTMLElement>('[role^="menuitem"]') ?? [])]

  useEffect(() => {
    if (!open) return
    const all = items()
    ;(all.find((i) => i.getAttribute("aria-checked") === "true") ?? all[0])?.focus()
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
  const buttonProps = {
    ref: button,
    type: "button" as const,
    "aria-haspopup": "menu" as const,
    "aria-expanded": open,
    "aria-controls": open ? id : undefined,
    onClick: () => setOpen((o) => !o),
    onKeyDown: (e: KeyboardEvent) => {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault()
        setOpen(true)
      }
    },
  }
  const menuProps = {
    ref: menu,
    id,
    role: "menu" as const,
    onKeyDown: (e: KeyboardEvent<HTMLUListElement>) => {
      const all = items()
      const at = all.indexOf(document.activeElement as HTMLElement)
      const to = { ArrowDown: at + 1, ArrowUp: at - 1, Home: 0, End: all.length - 1 }[e.key]
      if (to !== undefined) {
        e.preventDefault()
        all[(to + all.length) % all.length]?.focus()
      } else if (e.key === "Escape") {
        e.preventDefault()
        close()
      } else if (e.key === "Tab") setOpen(false)
    },
  }
  return { open, root, close, dismiss: () => setOpen(false), buttonProps, menuProps }
}

/** ThemeMenu picks system, dark or light (lib/theme.ts, public/theme-init.js): the chosen mode's icon opens the three, it checked. */
export function ThemeMenu() {
  const theme = useTheme()
  const { open, root, close, buttonProps, menuProps } = useMenu()
  const chosen = themes.find((t) => t.value === theme) ?? themes[0]
  return (
    <div ref={root} className="relative">
      <button {...buttonProps} aria-label={`Theme: ${chosen.label}`} title={`Theme: ${chosen.label}`} className={tool}>
        <chosen.Icon className="size-[1.125rem]" />
        <ChevronDownIcon className="size-3" />
      </button>
      {open && (
        <ul {...menuProps} aria-label="Theme" className={cn(panel, "flex w-36 flex-col")}>
          {themes.map(({ value, label, Icon }) => (
            <li key={value} role="none">
              <button
                type="button"
                role="menuitemradio"
                aria-checked={theme === value}
                tabIndex={-1}
                onClick={() => {
                  apply(value as Choice)
                  close()
                }}
                className={cn(item, "aria-checked:text-foreground")}
              >
                <Icon className="size-4" />
                <span className="flex-1">{label}</span>
                {theme === value && <CheckIcon className="size-4 text-primary" />}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

/** PortalNav is the reader's own corner of the header: the Guide, the theme and Settings, as icons named for assistive technology. */
export function PortalNav() {
  const path = usePathname()
  return (
    <nav aria-label="Portal" className="flex items-center gap-0.5">
      <Link href="/guide" className={tool} title="Guide" aria-current={path === "/guide" ? "page" : undefined}>
        <BookIcon className="size-[1.125rem]" />
        <span className="sr-only">Guide</span>
      </Link>
      {/* A whole page load: the presentation page draws its own screen, dark unless the reader chose. */}
      <a href="/present" className={tool} title="Present">
        <PresentIcon className="size-[1.125rem]" />
        <span className="sr-only">Present</span>
      </a>
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

const via: Record<string, string> = { proxy: "the proxy", oidc: "single sign-on", token: "a token" }

/** Divider parts the reader's tools from the viewer's own. */
function Divider() {
  return <span aria-hidden="true" className="h-5 w-px bg-border" />
}

/**
 * ViewerMenu is the header's viewer, after the reader's tools: the signed-in viewer's initial and name open a menu that says who they
 * are and how they signed in, with Admin for an administrator and Sign out (a viewer named by the proxy signs out there); without a
 * viewer, Sign in back to this page when the portal has sign-in. On a phone the initial alone.
 */
export function ViewerMenu({ me }: { me: Schemas["Me"] | undefined }) {
  const path = usePathname()
  const { open, root, dismiss, buttonProps, menuProps } = useMenu()
  if (!me) return null
  if (!me.signedIn) {
    if ((!me.signIn.oidc && !me.signIn.token) || path === "/signin") return null
    return (
      <>
        <Divider />
        <Link href={`/signin?return_to=${encodeURIComponent(path)}`} className={cn(tool, "px-2 text-sm")} title="Sign in">
          <LogInIcon className="size-[1.125rem]" />
          <span className="sr-only sm:not-sr-only">Sign in</span>
        </Link>
      </>
    )
  }
  return (
    <>
      <Divider />
      <div ref={root} className="relative">
        <button
          {...buttonProps}
          title={`Signed in as ${me.name}`}
          aria-current={path.startsWith("/admin") ? "page" : undefined}
          className={cn(tool, "gap-2 text-sm sm:pr-1")}
        >
          <span
            aria-hidden="true"
            className="grid size-6 shrink-0 place-items-center rounded-full bg-primary/15 text-xs font-semibold text-primary uppercase"
          >
            {[...me.name.trim()][0] ?? "?"}
          </span>
          <span className="sr-only max-w-40 truncate sm:not-sr-only">{me.name}</span>
          <ChevronDownIcon className="hidden size-3 sm:block" />
        </button>
        {open && (
          <div className={cn(panel, "w-56")}>
            <p className="px-2 pt-1.5 pb-2 text-xs text-muted-foreground">
              Signed in as <span className="font-medium text-foreground">{me.name}</span> through {via[me.via] ?? me.via}
              {me.admin ? ", an administrator" : ""}.
            </p>
            <ul {...menuProps} aria-label="Account" className="flex flex-col border-t border-border pt-1">
              {me.admin && (
                <li role="none">
                  <Link href="/admin" role="menuitem" tabIndex={-1} onClick={dismiss} className={item}>
                    <ShieldIcon className="size-4" />
                    Admin
                  </Link>
                </li>
              )}
              {me.via !== "proxy" && (
                <li role="none">
                  <form method="post" action="/auth/logout">
                    <button type="submit" role="menuitem" tabIndex={-1} className={item}>
                      <LogOutIcon className="size-4" />
                      Sign out
                    </button>
                  </form>
                </li>
              )}
            </ul>
          </div>
        )}
      </div>
    </>
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

/** bare are the pages drawn without the header: the presentation page fills the screen on its own (design/present.md). */
const bare = ["/present"]

/** Chrome is the header and what goes with it, left out on a bare page. */
export function Chrome({ children }: { children: ReactNode }) {
  return bare.includes(usePathname()) ? null : children
}
