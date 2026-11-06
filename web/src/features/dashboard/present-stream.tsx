"use client"

import Link from "next/link"
import { useEffect, useRef, useState, type ReactNode } from "react"
import { api, type Commit } from "@/lib/api/client"
import { useNow } from "@/lib/clock"
import { cn } from "@/lib/cn"
import { ago } from "@/lib/format"
import { addressPath } from "@/lib/paths"
import { by, type Work } from "@/lib/preferences"
import { AiMark } from "@/entities/commit/ai-mark"
import { InitialsAvatar } from "@/shared/initials-avatar"
import { DiffStat } from "@/shared/diffstat"
import { moreQuery } from "./api"
import { commitKey } from "./latest"
import { useMore } from "./more"
import { SectionFold, type Section } from "./side"

/** idleTop is how long a scrolled column waits untouched before it goes back to the newest commits (a screen on a wall). */
const idleTop = 60_000

/**
 * Stream is the newest commits of the work that counts: the author's avatar while the column is wide enough, then the repository
 * with who and when on one line and the subject on the next, the whole width its own. The column scrolls: at its end it reads
 * the next pages of the same commits (keep: the products the address names), and a minute untouched it goes back to the top and
 * lets the older ones go, so a wall shows the newest again. A commit that arrives while the page is open slides in at the top
 * (not with reduced motion).
 */
export function Stream({
  commits,
  work,
  keep,
  picker,
  section,
}: {
  commits: Commit[]
  /** the kinds the column shows */
  work: Work
  keep: (c: Commit) => boolean
  /** the label that chooses the kinds (ShowPicker) */
  picker?: ReactNode
  /** its section of the side column */
  section?: Section
}) {
  const now = useNow()
  // The commits of the first render; a row's animation runs once, when it mounts, and only for one that arrived at the top.
  const [shown] = useState(() => new Set(commits.map((c) => c.repo + c.sha)))
  const list = useRef<HTMLOListElement>(null)
  const end = useRef<HTMLLIElement>(null)
  const more = useMore(moreQuery(api, work), commits, (p) => p.commits.filter(keep), commitKey, end, section?.open !== false)
  const top = new Set(commits.map((c) => c.repo + c.sha))
  const rows = more.items.map((c) => {
    const id = c.repo + c.sha
    return { c, id, fresh: top.has(id) && !shown.has(id) }
  })
  // Back to the newest after a minute untouched.
  const idle = useRef<number | undefined>(undefined)
  const scrolled = () => {
    window.clearTimeout(idle.current)
    if ((list.current?.scrollTop ?? 0) === 0) return
    idle.current = window.setTimeout(() => {
      list.current?.scrollTo({ top: 0, behavior: "smooth" })
      more.reset()
    }, idleTop)
  }
  useEffect(() => () => window.clearTimeout(idle.current), [])
  return (
    <section
      aria-labelledby="stream"
      className={cn("panel flex min-h-0 flex-col overflow-hidden", section && (section.open ? "flex-1 basis-0" : "flex-none"))}
    >
      <div
        className={cn("flex items-center justify-between gap-2 px-3 py-2", section?.open !== false && "border-b border-border")}
      >
        <h2
          id="stream"
          className="shrink-0 text-xs font-medium tracking-widest whitespace-nowrap text-muted-foreground uppercase"
        >
          Latest commits
        </h2>
        <span className="flex min-w-0 items-center gap-1">
          {section?.open !== false && (picker ?? <span className="text-xs text-muted-foreground">{by(work)}</span>)}
          <SectionFold id="stream" section={section} />
        </span>
      </div>
      {/* A container: the avatars show while the column is wide enough for them. */}
      {section?.open !== false && (
        <ol
          ref={list}
          onScroll={scrolled}
          tabIndex={0}
          id="stream-body"
          aria-label="Latest commits, newest first"
          className="@container min-h-0 flex-1 divide-y divide-border overflow-y-auto overscroll-contain outline-none focus-visible:ring-2 focus-visible:ring-ring [scrollbar-width:thin]"
        >
          {rows.map(({ c, id, fresh }) => {
            const who = c.authorLogin || c.authorName
            return (
              <li key={id} className={cn("flex items-center gap-2.5 px-3 py-2", fresh && "arrive")}>
                <span className="@max-[17rem]:hidden">
                  <InitialsAvatar name={c.authorName || c.authorLogin} />
                </span>
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <p className="flex items-baseline gap-2 text-[0.9375rem]">
                    <Link prefetch={false} href={addressPath(c.repo)} className="min-w-0 truncate font-semibold hover:underline">
                      {c.repo.split("/")[1]}
                    </Link>
                    <span className="ml-auto flex max-w-[55%] shrink-0 items-baseline gap-1.5 text-sm text-muted-foreground">
                      <span className="truncate">{who}</span>
                      <span aria-hidden="true">·</span>
                      <time dateTime={c.committedAt} className="shrink-0 font-mono" suppressHydrationWarning>
                        {ago(c.committedAt, now)}
                      </time>
                    </span>
                  </p>
                  <p className="flex min-w-0 items-center gap-1.5 text-[0.9375rem]">
                    <AiMark commit={c} />
                    {c.url ? (
                      <a href={c.url} className="truncate hover:underline" rel="noreferrer" title={c.subject}>
                        {c.subject}
                      </a>
                    ) : (
                      <span className="truncate" title={c.subject}>
                        {c.subject}
                      </span>
                    )}
                    <DiffStat stats={c.stats} className="ml-auto pl-2 @max-[22rem]:hidden" />
                  </p>
                </div>
              </li>
            )
          })}
          <li ref={end} aria-hidden="true" className="px-3 py-2 text-center text-xs text-muted-foreground">
            {more.loading ? "Loading more…" : more.done ? "The rest is under All commits" : ""}
          </li>
        </ol>
      )}
    </section>
  )
}
