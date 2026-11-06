"use client"

import { useQuery } from "@tanstack/react-query"
import { usePathname, useRouter } from "next/navigation"
import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react"
import { SearchIcon } from "@/components/ui/icons"
import { api } from "@/lib/api/client"
import { kinds, maxQuery, suggestQuery } from "./api"
import { Parts, plain } from "./parts"

/** debounce is how long the box waits after a key before it asks for suggestions. */
const debounce = 150

/**
 * SearchBox is the header's search: a GET form to `/search`, so it works without JavaScript; with it, it navigates in the page. With it, `/` or Ctrl/⌘ K focuses the
 * box and it is a combobox: as one types it suggests repositories, groups and documents by their names (arrows move, Enter opens
 * the one chosen or else searches, Escape closes the list).
 */
export function SearchBox() {
  const router = useRouter()
  const path = usePathname()
  const input = useRef<HTMLInputElement>(null)
  const [value, setValue] = useState("")
  const [asked, setAsked] = useState("")
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const id = useId()

  useEffect(() => {
    const t = setTimeout(() => setAsked(value.trim()), debounce)
    return () => clearTimeout(t)
  }, [value])

  useEffect(() => {
    const focus = (e: globalThis.KeyboardEvent) => {
      if (e.defaultPrevented || !input.current) return // the page's own shortcut took it, or there is no box (on /search)
      const target = e.target as HTMLElement | null
      const typing = target?.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target?.tagName ?? "")
      if (
        (e.key === "/" && !typing && !e.metaKey && !e.ctrlKey && !e.altKey) ||
        (e.key.toLowerCase() === "k" && (e.metaKey || e.ctrlKey))
      ) {
        e.preventDefault()
        input.current?.focus()
        input.current?.select()
      }
    }
    document.addEventListener("keydown", focus)
    return () => document.removeEventListener("keydown", focus)
  }, [])

  const { data } = useQuery({ ...suggestQuery(api, asked), enabled: asked.length >= 2 })
  const hits = asked.length >= 2 && asked === value.trim() ? (data?.hits ?? []) : []
  const shown = open && hits.length > 0

  const go = (url: string) => {
    setOpen(false)
    setActive(-1)
    router.push(url)
  }
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      if (!hits.length) return
      e.preventDefault()
      setOpen(true)
      const step = e.key === "ArrowDown" ? 1 : -1
      // -1 is the box itself: the arrows go round the suggestions and back to it.
      const n = hits.length + 1
      setActive((a) => ((((a + 1 + step) % n) + n) % n) - 1)
    } else if (e.key === "Escape") {
      if (shown) e.preventDefault()
      setOpen(false)
      setActive(-1)
    } else if (e.key === "Enter" && shown && active >= 0 && active < hits.length) {
      const hit = hits[active]
      if (hit) {
        e.preventDefault()
        go(hit.url)
      }
    }
  }

  if (path === "/search") return null // the page has its own form
  return (
    <form
      action="/search"
      role="search"
      className="relative"
      onSubmit={(e) => {
        e.preventDefault()
        const q = value.trim()
        if (q) go(`/search?${new URLSearchParams({ q }).toString()}`)
      }}
    >
      <label htmlFor={`${id}-q`} className="sr-only">
        Search the portal
      </label>
      <SearchIcon className="pointer-events-none absolute top-1/2 left-2 size-4 -translate-y-1/2 text-muted-foreground" />
      <input
        ref={input}
        id={`${id}-q`}
        type="search"
        name="q"
        required
        maxLength={maxQuery}
        value={value}
        placeholder="Search"
        autoComplete="off"
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={shown}
        aria-controls={`${id}-list`}
        aria-activedescendant={shown && active >= 0 && active < hits.length ? `${id}-${active}` : undefined}
        aria-keyshortcuts="/ Control+K Meta+K"
        onChange={(e) => {
          setValue(e.target.value)
          setOpen(true)
          setActive(-1)
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onKeyDown={onKeyDown}
        className="h-8 w-40 rounded-md border border-input bg-background/60 pr-2 pl-7 text-sm transition-[width] focus:w-64 sm:w-48"
      />
      <ul
        id={`${id}-list`}
        role="listbox"
        aria-label="Suggestions"
        hidden={!shown}
        className="absolute top-full right-0 z-50 mt-1.5 w-80 max-w-[calc(100vw-2rem)] rounded-lg border border-border bg-popover/95 p-1 text-sm text-popover-foreground shadow-xl backdrop-blur-md"
      >
        {hits.map((h, i) => (
          <li
            key={h.url}
            id={`${id}-${i}`}
            role="option"
            aria-selected={i === active}
            aria-label={`${plain(h.title)}, ${kinds.find((k) => k.kind === h.kind)?.one ?? h.kind}`}
            // A press on an option opens it before the box loses the focus.
            onMouseDown={(e) => {
              e.preventDefault()
              go(h.url)
            }}
            className="flex cursor-pointer flex-col rounded-md px-2 py-1.5 aria-selected:bg-accent hover:bg-accent"
          >
            <span className="truncate font-medium">
              <Parts parts={h.title} />
            </span>
            <span className="truncate text-xs text-muted-foreground">
              {kinds.find((k) => k.kind === h.kind)?.one}
              {h.repo && h.kind !== "repo" && ` · ${h.repo}`}
            </span>
          </li>
        ))}
      </ul>
    </form>
  )
}
