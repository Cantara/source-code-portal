"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useEffect, useRef, useState, type ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { ChevronDownIcon, PanelLeftIcon, SearchIcon } from "@/components/ui/icons"
import { api } from "@/lib/api/client"
import { groupsQuery, meQuery } from "@/lib/api/queries"
import { cn } from "@/lib/cn"
import { count } from "@/lib/format"
import { firstPageOf, pageSizes, rememberSize, type PagedList } from "@/lib/paging"
import { filtersCookie, remember, type Filters } from "@/lib/preferences"

/**
 * FilterLayout puts a list page's filters in a panel at the left of its list, every choice in view at once (design/filters.md): the
 * panel is sticky while the list scrolls, its button hides it and a rail shows it again, kept in `scp-filters` for every list page. A
 * reader who never chose has it open on a wide screen and folded to its rail on a narrow one, where it would push the list down.
 */
export function FilterLayout({
  initial,
  label,
  clear,
  filters,
  children,
}: {
  initial: Filters
  /** the panel's name for assistive technology ("Filter pull requests") */
  label: string
  /** where "Clear" goes when a filter is set; none when none is */
  clear?: string
  filters: ReactNode
  children: ReactNode
}) {
  const [panel, setPanel] = useState(initial)
  const save = (open: boolean) => {
    setPanel({ open, chosen: true })
    remember(filtersCookie, open ? "open" : "closed")
  }
  const shown = panel.open
  return (
    <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:gap-5">
      {shown && (
        <aside
          aria-label={label}
          className={cn(
            "panel flex shrink-0 flex-col gap-1 p-3 lg:sticky lg:top-20 lg:max-h-[calc(100dvh-6rem)] lg:w-64 lg:overflow-y-auto",
            !panel.chosen && "max-lg:hidden",
          )}
        >
          <div className="flex items-center gap-2 pb-1">
            <p className="min-w-0 flex-1 text-sm font-semibold">Filters</p>
            {clear !== undefined && (
              <Link href={clear} className="text-xs text-muted-foreground hover:text-foreground hover:underline">
                Clear
              </Link>
            )}
            <button
              type="button"
              onClick={() => save(false)}
              aria-expanded={true}
              aria-label="Hide the filters"
              title="Hide the filters"
              className="grid size-7 shrink-0 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            >
              <PanelLeftIcon open className="size-4" />
            </button>
          </div>
          {filters}
        </aside>
      )}
      {(!shown || !panel.chosen) && (
        <aside aria-label={label} className={cn("shrink-0 lg:sticky lg:top-20", shown && "lg:hidden")}>
          <button
            type="button"
            onClick={() => save(true)}
            aria-expanded={false}
            aria-label="Show the filters"
            title="Show the filters"
            className="panel flex w-full items-center gap-2 px-3 py-2 text-xs font-medium tracking-widest text-muted-foreground uppercase transition-colors hover:text-foreground lg:w-10 lg:flex-col lg:px-0 lg:py-3"
          >
            <PanelLeftIcon open={false} className="size-4 shrink-0" />
            <span className="lg:[writing-mode:vertical-rl]">Filters</span>
          </button>
        </aside>
      )}
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  )
}

/**
 * FilterForm is the panel's choices as a GET form: with JavaScript each choice applies as it is made (the controls call their
 * onPick), and the form never submits; without it the form is the way, and its Apply button is shown.
 */
export function FilterForm({ action, label, children }: { action: string; label: string; children: ReactNode }) {
  return (
    <form action={action} aria-label={label} onSubmit={(e) => e.preventDefault()} className="flex flex-col text-sm">
      {children}
      <noscript>
        <Button type="submit" variant="outline" className="mt-3 w-full">
          Apply
        </Button>
      </noscript>
    </form>
  )
}

/** FilterGroup is a titled part of the panel that folds with its chevron; every one starts open so every choice is in view. */
export function FilterGroup({ title, open = true, children }: { title: string; open?: boolean; children: ReactNode }) {
  return (
    <details open={open} className="group border-t border-border py-2 first:border-t-0">
      <summary className="flex min-h-8 cursor-pointer list-none items-center justify-between gap-2 rounded text-sm font-semibold [&::-webkit-details-marker]:hidden">
        {title}
        <ChevronDownIcon className="size-4 text-muted-foreground transition-transform group-open:rotate-180" />
      </summary>
      <div className="mt-1 flex flex-col">{children}</div>
    </details>
  )
}

/** Option is one choice: its value, its words, and optionally how many it holds and the dot that marks it. */
export interface Option {
  value: string
  label: string
  count?: number
  dot?: string
  /** said on hover and to a screen reader */
  about?: string
}

/**
 * Choices are one-of-many choices as radios, each with its count: "All" is the option whose value is "". A choice that holds none
 * is dimmed and cannot be picked, unless it is the current one.
 */
export function Choices({
  name,
  label,
  value,
  options,
  onPick,
  scroll = false,
}: {
  name: string
  /** the group's name for assistive technology */
  label: string
  value: string
  options: Option[]
  onPick: (value: string) => void
  /** a long list keeps to its height and scrolls inside */
  scroll?: boolean
}) {
  return (
    <div role="radiogroup" aria-label={label} className={cn("flex flex-col", scroll && "max-h-56 overflow-y-auto")}>
      {options.map((o) => {
        const none = o.count === 0 && o.value !== value
        return (
          <label
            key={o.value}
            title={o.about}
            className={cn(
              "flex min-h-8 cursor-pointer items-center gap-2 rounded px-1 hover:bg-accent has-[:checked]:font-medium has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring",
              none && "cursor-default text-muted-foreground opacity-50 hover:bg-transparent",
            )}
          >
            <input
              type="radio"
              name={name}
              value={o.value}
              checked={value === o.value}
              disabled={none}
              onChange={() => onPick(o.value)}
              className="size-4 shrink-0 accent-primary"
            />
            {o.dot && <span aria-hidden="true" className={cn("size-2 shrink-0 rounded-full", o.dot)} />}
            <span className="min-w-0 flex-1 truncate">{o.label}</span>{" "}
            {o.count !== undefined && (
              <span className="font-mono text-xs text-muted-foreground tabular-nums">{count(o.count)}</span>
            )}
          </label>
        )
      })}
    </div>
  )
}

/** Toggle is an on-off choice as a switch: it names the value it sends when on and is absent from the form when off. */
export function Toggle({
  name,
  value = "true",
  label,
  checked,
  onPick,
}: {
  name: string
  value?: string
  label: string
  checked: boolean
  onPick: (on: boolean) => void
}) {
  return (
    <label className="flex min-h-8 cursor-pointer items-center justify-between gap-2 rounded px-1 hover:bg-accent has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring">
      <span className="min-w-0 flex-1">{label}</span>
      <input
        type="checkbox"
        role="switch"
        name={name}
        value={value}
        checked={checked}
        onChange={(e) => onPick(e.target.checked)}
        className="peer sr-only"
      />
      <span
        aria-hidden="true"
        className="relative h-5 w-9 shrink-0 rounded-full bg-muted-foreground/40 transition-colors peer-checked:bg-primary after:absolute after:top-0.5 after:left-0.5 after:size-4 after:rounded-full after:bg-background after:transition-transform peer-checked:after:translate-x-4"
      />
    </label>
  )
}

/**
 * TextFilter is a text choice with a search glyph: it applies on Enter, when the field is left and a moment after the last key, so
 * typing is not interrupted. It follows the page when the address changes (Clear).
 */
export function TextFilter({
  name,
  label,
  value,
  placeholder,
  id,
  onPick,
}: {
  name: string
  label: string
  value: string
  placeholder?: string
  id?: string
  onPick: (value: string) => void
}) {
  const [text, setText] = useState(value)
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  // Follows the address (Clear, back) while it is not what was typed: state adjusted as the value changes, not in an effect.
  const [seen, setSeen] = useState(value)
  if (seen !== value) {
    setSeen(value)
    setText(value)
  }
  useEffect(() => () => clearTimeout(timer.current), [])
  const apply = (v: string) => {
    clearTimeout(timer.current)
    if (v.trim() !== value.trim()) onPick(v.trim())
  }
  return (
    <label className="relative flex flex-col">
      <span className="sr-only">{label}</span>
      <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
      <input
        id={id}
        type="search"
        name={name}
        value={text}
        placeholder={placeholder ?? label}
        autoComplete="off"
        onChange={(e) => {
          setText(e.target.value)
          clearTimeout(timer.current)
          timer.current = setTimeout(() => apply(e.target.value), 500)
        }}
        onBlur={(e) => apply(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), apply(e.currentTarget.value))}
        className="min-h-9 w-full rounded-md border border-input bg-background pr-2 pl-8"
      />
    </label>
  )
}

/** ProductChoices is every product of the portal as a choice, "All products" first. */
export function ProductChoices({ value, onPick }: { value: string; onPick: (group: string) => void }) {
  const groups = useQuery(groupsQuery(api))
  const options: Option[] = [{ value: "", label: "All products" }]
  for (const g of groups.data?.groups ?? []) options.push({ value: g.id, label: g.name })
  // The chosen product is listed even before the products arrive, so the choice never looks unmade.
  if (value && !options.some((o) => o.value === value)) options.push({ value, label: value })
  return (
    <FilterGroup title="Product">
      <Choices name="group" label="Product" value={value} options={options} onPick={onPick} scroll />
    </FilterGroup>
  )
}

/**
 * MineToggles are what is the reader's own, as switches: each sets one field of the address to the reader's GitHub login (known
 * when the portal's credential for them says so, `/api/v1/me`). Nothing shows to a reader without a login.
 */
export function MineToggles({
  fields,
  current,
  onPick,
}: {
  fields: { name: string; label: string }[]
  current: Record<string, string | undefined>
  onPick: (patch: Record<string, string>) => void
}) {
  const me = useQuery(meQuery(api)).data?.login
  if (!me) return null
  return (
    <FilterGroup title="Mine">
      {fields.map((f) => (
        <Toggle
          key={f.name}
          name={f.name}
          value={me}
          label={f.label}
          checked={current[f.name]?.toLowerCase() === me.toLowerCase()}
          onPick={(on) => onPick({ [f.name]: on ? me : "" })}
        />
      ))}
    </FilterGroup>
  )
}

/** SizeChoices is how many items a page shows, from the sizes a reader can choose; the form's field `per`. */
export function SizeChoices({ value, onPick }: { value: number; onPick: (n: number) => void }) {
  return (
    <FilterGroup title="Per page">
      <Choices
        name="per"
        label="Per page"
        value={String(value)}
        options={pageSizes.map((n) => ({ value: String(n), label: String(n) }))}
        onPick={(v) => onPick(Number(v))}
      />
    </FilterGroup>
  )
}

/**
 * useListNav is a list page's address as its panel and pager use it: its state read from the query string (parse) and written back
 * (serialize); pick changes some fields and goes to the first page, pickSize keeps the reader's first item in view and remembers the
 * size, href is a page's address and clear is where the unfiltered list is, or none when nothing is filtered.
 */
export function useListNav<S extends { page: number; size: number }>({
  list,
  base,
  parse,
  serialize,
  defaultSize,
}: {
  list: PagedList
  /** the page's own path, `/pulls` */
  base: string
  parse: (fields: Record<string, unknown>, size: number) => S
  serialize: (st: S, defaultSize: number) => string
  defaultSize: number
}) {
  const fields = Object.fromEntries(useSearchParams())
  const path = usePathname()
  const router = useRouter()
  const st = parse(fields, defaultSize)
  const href = (n: number) => path + serialize({ ...st, page: n }, defaultSize)
  return {
    st,
    path,
    href,
    pick: (patch: Record<string, string>) => {
      const next = parse({ ...fields, ...patch, page: "1" }, defaultSize)
      router.push(base + serialize({ ...next, page: 1 }, defaultSize))
    },
    pickSize: (n: number) => {
      rememberSize(list, n)
      window.history.pushState(
        null,
        "",
        path + serialize({ ...st, size: n, page: firstPageOf(st.page, st.size, n) }, defaultSize),
      )
    },
    clear: serialize({ ...st, page: 1, size: defaultSize }, defaultSize) === "" ? undefined : base,
  }
}
