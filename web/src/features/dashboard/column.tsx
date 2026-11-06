"use client"

import Link from "next/link"
import type { RefObject } from "react"
import { useNow } from "@/lib/clock"
import { cn } from "@/lib/cn"
import { ago } from "@/lib/format"
import { addressPath } from "@/lib/paths"
import { InitialsAvatar } from "@/shared/initials-avatar"
import { SectionFold, type Section } from "./side"

/**
 * ColumnSection is a section of the presentation page's side column drawn as its latest commits are (design/present.md): a panel
 * with a small uppercase label, the count that links its page and the hide button in one header line, then its rows edge to edge,
 * scrolling inside it. end closes the list: the next page is read as it nears the view (more.tsx), then the link to every one.
 */
export function ColumnSection({
  id,
  title,
  all,
  section,
  rows,
  end,
  loading,
  done,
  empty,
  rest,
}: {
  id: string
  title: string
  /** the count's link: the page with every one */
  all: { href: string; text: string }
  section?: Section
  rows: Row[]
  end: RefObject<HTMLDivElement | null>
  loading: boolean
  done: boolean
  empty: string
  /** the words at the end once nothing more is read */
  rest: string
}) {
  const open = section?.open !== false
  return (
    <section
      aria-labelledby={id}
      className={cn("panel flex min-h-0 flex-col overflow-hidden", section && (open ? "flex-1 basis-0" : "flex-none"))}
    >
      <div className={cn("flex items-center justify-between gap-2 px-3 py-2", open && "border-b border-border")}>
        <h2 id={id} className="shrink-0 text-xs font-medium tracking-widest whitespace-nowrap text-muted-foreground uppercase">
          {title}
        </h2>
        <span className="flex min-w-0 items-center gap-1">
          {open && (
            <Link
              href={all.href}
              className="truncate text-xs whitespace-nowrap text-muted-foreground hover:text-foreground hover:underline"
            >
              {all.text}
            </Link>
          )}
          <SectionFold id={id} section={section} />
        </span>
      </div>
      {open && (
        <div
          id={`${id}-body`}
          tabIndex={0}
          className="@container min-h-0 flex-1 overflow-y-auto overscroll-contain outline-none focus-visible:ring-2 focus-visible:ring-ring [scrollbar-width:thin]"
        >
          <ol aria-label={`${title}, newest first`} className="divide-y divide-border">
            {rows.map((r) => (
              <ColumnRow key={`${r.repo}#${r.number}`} r={r} />
            ))}
            {rows.length === 0 && <li className="px-3 py-2 text-sm text-muted-foreground">{empty}</li>}
          </ol>
          <div ref={end} aria-live="polite" className="px-3 py-2 text-center text-xs text-muted-foreground">
            {loading ? "Loading more…" : null}
            {done ? (
              <Link href={all.href} className="hover:text-primary hover:underline">
                {rest}
              </Link>
            ) : null}
          </div>
        </div>
      )}
    </section>
  )
}

/** Row is a pull request or an issue as the side column draws it: where it is, who opened it and when, its state and its title. */
export interface Row {
  repo: string
  number: number
  url: string
  title: string
  author: string
  bot: boolean
  /** when it was opened: the column lists the newest first */
  at: string
  /** its state's words, what it waits for and the colour of its dot */
  state: { label: string; waits: string; dot: string }
}

/** ColumnRow is one row in the two lines of a latest commit: the repository, who and when; then the state's dot and the title. */
function ColumnRow({ r }: { r: Row }) {
  const now = useNow()
  return (
    <li className="flex items-center gap-2.5 px-3 py-2">
      <span className="@max-[17rem]:hidden">
        <InitialsAvatar name={r.author} />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <p className="flex items-baseline gap-2 text-[0.9375rem]">
          <Link href={addressPath(r.repo)} className="min-w-0 truncate font-semibold hover:underline">
            {r.repo.split("/")[1]}
            <span className="font-normal text-muted-foreground">#{r.number}</span>
          </Link>
          <span className="ml-auto flex max-w-[55%] shrink-0 items-baseline gap-1.5 text-sm text-muted-foreground">
            <span className="truncate">
              {r.author}
              {r.bot && <span className="sr-only"> (automation)</span>}
            </span>
            <span aria-hidden="true">·</span>
            <time dateTime={r.at} title="opened" className="shrink-0 font-mono" suppressHydrationWarning>
              {ago(r.at, now)}
            </time>
          </span>
        </p>
        <p className="flex min-w-0 items-center gap-1.5 text-[0.9375rem]">
          <span
            aria-hidden="true"
            title={`${r.state.label}: ${r.state.waits}`}
            className={cn("size-2 shrink-0 rounded-full", r.state.dot)}
          />
          <span className="sr-only">{r.state.label}: </span>
          <a href={r.url} rel="noreferrer" className="truncate hover:underline" title={r.title}>
            {r.title}
          </a>
        </p>
      </div>
    </li>
  )
}
