"use client"

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api/client"
import { snapshotQuery } from "@/lib/api/snapshot"
import { useNow } from "@/lib/clock"
import { cn } from "@/lib/cn"
import { useConnectionFailing, useLive } from "@/lib/connection"
import { ago } from "@/lib/format"
import { Mark } from "@/components/ui/mark"

/** stale and offline are how long since the data was last known current before the page says so (design/present.md). */
const stale = 2 * 60_000
const offline = 10 * 60_000

type State = { level: string; word: string; about: string }

/**
 * freshness is how current the page's data is: live while the event stream is open, else as old as the last snapshot read; stale
 * past 2 minutes, offline past 10 or while the server fails. The colour and a word, never the colour alone.
 */
export function freshness(live: boolean, failing: boolean, checked: number, now: number): State {
  const age = now - checked
  if (failing || (!live && age > offline))
    return { level: "lvl-danger", word: "Offline", about: "The portal cannot be reached; this is the last data that loaded." }
  if (!live && age > stale) return { level: "lvl-quiet", word: "Stale", about: "No new data for a few minutes." }
  return live
    ? { level: "lvl-active", word: "Live", about: "New data shows within seconds." }
    : { level: "lvl-recent", word: "Polling", about: "Checking for new data every 30 seconds." }
}

/**
 * Brand is the portal's mark and name at the top left, the header being left out: the name gives way to the mark alone as the
 * left column narrows. A whole page load to the dashboard, as the bar's link.
 */
export function Brand() {
  return (
    <a
      href="/"
      title="Source Code Portal: the dashboard"
      className="flex shrink-0 items-center gap-2 font-semibold tracking-tight"
    >
      <Mark />
      <span aria-hidden="true" className="font-mono tracking-widest @max-md:hidden">
        SCP
      </span>
      <span className="sr-only">Source Code Portal, the dashboard</span>
    </a>
  )
}

/** Freshness says at all times how old the page's data is, so a screen on a wall is never silently wrong. */
export function Freshness() {
  const now = useNow()
  const live = useLive()
  const failing = useConnectionFailing()
  const { data, dataUpdatedAt } = useQuery(snapshotQuery(api))
  const checked = live ? now : dataUpdatedAt || now
  const s = freshness(live, failing, checked, now)
  return (
    // Two short lines at the strip's end: the word, then the data's age.
    <p role="status" className={cn(s.level, "flex shrink-0 flex-col items-end leading-tight")} title={s.about}>
      <span className="flex items-center gap-2">
        <span aria-hidden="true" className="led led-live" />
        <span className="font-mono text-sm font-semibold tracking-widest text-(--lvl-fg) uppercase">{s.word}</span>
      </span>
      {data?.builtAt && (
        <span className="text-xs text-muted-foreground" suppressHydrationWarning>
          <span className="sr-only">data from </span>
          {ago(data.builtAt, now) === "now" ? "now" : `${ago(data.builtAt, now)} ago`}
        </span>
      )}
    </p>
  )
}
