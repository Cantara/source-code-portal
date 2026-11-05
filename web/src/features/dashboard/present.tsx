"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useState, type ReactNode } from "react"
import { api, type Commit, type Schemas } from "@/lib/api/client"
import { snapshotQuery } from "@/lib/api/snapshot"
import { useNow } from "@/lib/clock"
import { cn } from "@/lib/cn"
import { useConnectionFailing, useLive } from "@/lib/connection"
import { ago } from "@/lib/format"
import { addressPath } from "@/lib/paths"
import { by, type Work } from "@/lib/preferences"
import { AiMark } from "@/entities/commit/ai-mark"
import { ProblemAlert } from "@/shared/problem-alert"
import { dashboardQuery, pageQuery, workQuery } from "./api"
import { FleetMap } from "./fleet-map"
import { defaultPresent, pick, type Element } from "./layout"
import { TelemetryStrip } from "./telemetry"
import { ProductTiles } from "./tiles"

/** stale and offline are how long since the data was last known current before the page says so (design/present.md). */
const stale = 2 * 60_000
const offline = 10 * 60_000

/** A commit list asks for at most this many when the page shows only some products, and keeps theirs. */
const scoped = 200

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

/** Freshness says at all times how old the page's data is, so a screen on a wall is never silently wrong. */
function Freshness() {
  const now = useNow()
  const live = useLive()
  const failing = useConnectionFailing()
  const { data, dataUpdatedAt } = useQuery(snapshotQuery(api))
  const checked = live ? now : dataUpdatedAt || now
  const s = freshness(live, failing, checked, now)
  return (
    <p role="status" className={cn(s.level, "flex items-center gap-2 text-[0.9375rem]")} title={s.about}>
      <span aria-hidden="true" className="led led-live" />
      <span className="font-mono font-semibold tracking-widest text-(--lvl-fg) uppercase">{s.word}</span>
      {data?.builtAt && (
        <span className="text-muted-foreground" suppressHydrationWarning>
          data from {ago(data.builtAt, now) === "now" ? "just now" : `${ago(data.builtAt, now)} ago`}
        </span>
      )}
    </p>
  )
}

/**
 * Stream is the newest commits of the work that counts, as many as the column holds: repository and time, then author and subject.
 * A commit that arrives while the page is open slides in at the top (not with reduced motion).
 */
function Stream({ commits, work }: { commits: Commit[]; work: Work }) {
  const now = useNow()
  // The commits of the first render; a row's animation runs once, when it mounts.
  const [shown] = useState(() => new Set(commits.map((c) => c.repo + c.sha)))
  const rows = commits.map((c) => ({ c, id: c.repo + c.sha, fresh: !shown.has(c.repo + c.sha) }))
  return (
    <section aria-labelledby="stream" className="panel flex min-h-0 flex-col overflow-hidden">
      <div className="flex items-baseline justify-between gap-2 border-b border-border px-3 py-2">
        <h2 id="stream" className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
          Latest commits
        </h2>
        <span className="text-xs text-muted-foreground">{by(work)}</span>
      </div>
      <ol className="min-h-0 flex-1 divide-y divide-border overflow-hidden">
        {rows.map(({ c, id, fresh }) => (
          <li key={id} className={cn("flex flex-col gap-0.5 px-3 py-2", fresh && "arrive")}>
            <p className="flex items-baseline justify-between gap-2 text-[0.9375rem]">
              <Link href={addressPath(c.repo)} className="truncate font-semibold hover:underline">
                {c.repo.split("/")[1]}
              </Link>
              <time
                dateTime={c.committedAt}
                className="shrink-0 font-mono text-sm text-muted-foreground"
                suppressHydrationWarning
              >
                {ago(c.committedAt, now)}
              </time>
            </p>
            <p className="flex min-w-0 items-baseline gap-1.5 text-[0.9375rem]">
              <span className="shrink-0 text-muted-foreground">{c.authorLogin || c.authorName}</span>
              <AiMark commit={c} />
              <span className="truncate">{c.subject}</span>
            </p>
          </li>
        ))}
      </ol>
    </section>
  )
}

/** within is the groups an element selects (ids; none: every group), kept to those the address names (`?groups=`). */
function within(all: Schemas["DashboardGroup"][], ids: string[] | undefined, only: string[] | undefined) {
  const g = pick(all, ids)
  return only ? g.filter((x) => only.includes(x.id)) : g
}

/**
 * PresentView is the presentation page (design/present.md): the layout's elements on one screen that never scrolls, for a tablet
 * or a screen on a wall. The latest commits take the right column at full height (below the rest in portrait); the other elements
 * stack at the left in their order, the products filling what is left and folding the good ones that do not fit. only is the
 * groups the address names.
 */
export function PresentView({ work, only }: { work: Work; only?: string[] }) {
  const now = useNow()
  const { data, error } = useQuery(dashboardQuery(api, work))
  const { data: page } = useQuery(pageQuery(api, "present"))
  const layout = page?.elements ?? defaultPresent
  const stream = layout.find((e) => e.kind === "latest-commits")
  const count = stream?.count ?? 30
  const { data: log } = useQuery({ ...workQuery(api, work, only ? scoped : count), enabled: !!stream })
  if (!data) return <div className="p-4">{error && <ProblemAlert error={error} />}</div>

  const scope = only ? { ...data, groups: within(data.groups, undefined, only) } : data
  const members = new Set(scope.groups.flatMap((g) => g.members.map((m) => m.address)))
  const commits = (log?.commits ?? data.latest).filter((c) => !only || members.has(c.repo)).slice(0, count)
  const groups = (el: Element) => within(data.groups, el.groups, only)
  const left: ReactNode[] = []
  let strip = false
  layout.forEach((el, i) => {
    switch (el.kind) {
      case "telemetry":
        left.push(<TelemetryStrip key={i} data={scope} end={strip ? undefined : <Freshness />} />)
        strip = true
        break
      case "fleet-map":
        left.push(<FleetMap key={i} groups={groups(el)} work={data.work} now={now} color={el.color ?? "health"} />)
        break
      case "products":
        left.push(<ProductTiles key={i} groups={groups(el)} work={data.work} now={now} fold />)
        break
      case "heading":
        left.push(
          <div key={i}>
            <h2 className="text-lg font-semibold">{el.text}</h2>
            {el.note && <p className="text-[0.9375rem] text-muted-foreground">{el.note}</p>}
          </div>,
        )
        break
      case "latest-commits":
        break
    }
  })
  return (
    <div className="present fixed inset-0 grid grid-cols-1 grid-rows-[minmax(0,1fr)_minmax(0,38%)] gap-3 overflow-hidden p-3 text-[0.9375rem] landscape:grid-cols-[minmax(0,1fr)_minmax(18rem,30%)] landscape:grid-rows-1">
      <h1 className="sr-only">Presentation</h1>
      <div className="flex min-h-0 min-w-0 flex-col gap-3 overflow-hidden">
        {!strip && (
          <div className="flex justify-end">
            <Freshness />
          </div>
        )}
        {left}
      </div>
      {stream && <Stream commits={commits} work={data.work} />}
    </div>
  )
}
