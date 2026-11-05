"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useEffect, useRef, useState, useSyncExternalStore, type KeyboardEvent, type PointerEvent, type ReactNode } from "react"
import { api, type Commit, type Schemas } from "@/lib/api/client"
import { snapshotQuery } from "@/lib/api/snapshot"
import { useNow } from "@/lib/clock"
import { cn } from "@/lib/cn"
import { useConnectionFailing, useLive } from "@/lib/connection"
import { ago } from "@/lib/format"
import { addressPath } from "@/lib/paths"
import { by, columnCookie, columnWidths, remember, type Work } from "@/lib/preferences"
import { Mark } from "@/components/ui/mark"
import { AiMark } from "@/entities/commit/ai-mark"
import { InitialsAvatar } from "@/shared/initials-avatar"
import { ProblemAlert } from "@/shared/problem-alert"
import { meQuery } from "@/lib/api/queries"
import { apply, useTheme, type Choice } from "@/lib/theme"
import { dashboardQuery, pageQuery, workQuery } from "./api"
import { Designer } from "./design"
import { useShift, useWall } from "./wall"
import { FleetMap } from "./fleet-map"
import { defaultPresent, pick, type Element } from "./layout"
import { TelemetryStrip } from "./telemetry"
import { ProductTiles } from "./tiles"

/** stale and offline are how long since the data was last known current before the page says so (design/present.md). */
const stale = 2 * 60_000
const offline = 10 * 60_000

/** barFor is how long a tap shows the controls. */
const barFor = 5_000

const themes: Choice[] = ["dark", "light", "system"]

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

/**
 * Brand is the portal's mark and name at the top left, the header being left out: the name gives way to the mark alone as the
 * left column narrows. A whole page load to the dashboard, as the bar's link.
 */
function Brand() {
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
function Freshness() {
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
        <span className="font-mono font-semibold tracking-widest text-(--lvl-fg) uppercase">{s.word}</span>
      </span>
      {data?.builtAt && (
        <span className="text-sm text-muted-foreground" suppressHydrationWarning>
          data from {ago(data.builtAt, now) === "now" ? "just now" : `${ago(data.builtAt, now)} ago`}
        </span>
      )}
    </p>
  )
}

/**
 * Stream is the newest commits of the work that counts, as many as the column holds: the author's avatar while the column is wide
 * enough, then the repository with who and when on one line and the subject on the next, the whole width its own.
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
      {/* A container: the avatars show while the column is wide enough for them. */}
      <ol className="@container min-h-0 flex-1 divide-y divide-border overflow-hidden">
        {rows.map(({ c, id, fresh }) => {
          const who = c.authorLogin || c.authorName
          return (
            <li key={id} className={cn("flex items-center gap-2.5 px-3 py-2", fresh && "arrive")}>
              <span className="@max-[17rem]:hidden">
                <InitialsAvatar name={c.authorName || c.authorLogin} />
              </span>
              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                <p className="flex items-baseline gap-2 text-[0.9375rem]">
                  <Link href={addressPath(c.repo)} className="min-w-0 truncate font-semibold hover:underline">
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
                <p className="flex min-w-0 items-baseline gap-1.5 text-[0.9375rem]">
                  <AiMark commit={c} />
                  <span className="truncate">{c.subject}</span>
                </p>
              </div>
            </li>
          )
        })}
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
 * Screen draws a layout's elements on the presentation page's grid within frame (the whole viewport, or the design mode's preview).
 * The latest commits take the right column at full height (below the rest in portrait); the other elements stack at the left in
 * their order, the products filling what is left and folding the good ones that do not fit. only is the groups the address names.
 */
function Screen({
  layout,
  data,
  commits,
  only,
  frame,
  column = columnWidths.initial,
  onColumn,
}: {
  layout: Element[]
  data: Schemas["Dashboard"]
  commits: Commit[]
  only?: string[]
  frame: string
  /** the commit column's width in percent of the screen, in landscape */
  column?: number
  /** resizes the column as its edge is dragged; kept when the drag ends (keep) */
  onColumn?: (width: number, keep: boolean) => void
}) {
  const now = useNow()
  const scope = only ? { ...data, groups: within(data.groups, undefined, only) } : data
  const groups = (el: Element) => within(data.groups, el.groups, only)
  const left: ReactNode[] = []
  let strip = false
  layout.forEach((el, i) => {
    switch (el.kind) {
      case "telemetry":
        left.push(
          <TelemetryStrip key={i} data={scope} start={strip ? undefined : <Brand />} end={strip ? undefined : <Freshness />} />,
        )
        strip = true
        break
      case "fleet-map":
        left.push(<FleetMap key={i} groups={groups(el)} work={data.work} now={now} color={el.color ?? "health"} dense />)
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
  const stream = layout.find((e) => e.kind === "latest-commits")
  return (
    <div
      className={cn(
        "grid grid-cols-1 grid-rows-[minmax(0,1fr)_minmax(0,38%)] gap-3 overflow-hidden p-3 text-[0.9375rem] landscape:grid-rows-1 landscape:gap-x-0",
        stream ? columns[column] : "landscape:grid-cols-1",
        frame,
      )}
    >
      {/* A container: as the commit column widens, the elements keep the signals that matter most (container queries). */}
      <div className="@container flex min-h-0 min-w-0 flex-col gap-3 overflow-hidden">
        {!strip && (
          <div className="flex items-center justify-between gap-4">
            <Brand />
            <Freshness />
          </div>
        )}
        {left}
      </div>
      {stream && <Resizer width={column} onChange={onColumn} />}
      {stream && <Stream commits={commits.slice(0, stream.count ?? 30)} work={data.work} />}
    </div>
  )
}

/** columns are the landscape grid at each width of the commit column, as written classes (the CSP allows no style attribute). */
const columns: Record<number, string> = {
  20: "landscape:grid-cols-[minmax(0,1fr)_0.75rem_20%]",
  25: "landscape:grid-cols-[minmax(0,1fr)_0.75rem_25%]",
  30: "landscape:grid-cols-[minmax(0,1fr)_0.75rem_30%]",
  35: "landscape:grid-cols-[minmax(0,1fr)_0.75rem_35%]",
  40: "landscape:grid-cols-[minmax(0,1fr)_0.75rem_40%]",
  45: "landscape:grid-cols-[minmax(0,1fr)_0.75rem_45%]",
  50: "landscape:grid-cols-[minmax(0,1fr)_0.75rem_50%]",
  55: "landscape:grid-cols-[minmax(0,1fr)_0.75rem_55%]",
  60: "landscape:grid-cols-[minmax(0,1fr)_0.75rem_60%]",
}

const clampColumn = (w: number) =>
  Math.min(columnWidths.max, Math.max(columnWidths.min, Math.round(w / columnWidths.step) * columnWidths.step))

/** A press that moves less than this is a tap, not a drag; two taps within doubleTap bring back the usual width. */
const slop = 4
const doubleTap = 400

/**
 * Resizer is the commit column's edge in landscape: dragged (a finger or a pointer) or moved with the arrow keys, in steps of 5% of
 * the screen from 20% to 60%; a double tap or click brings back the usual width. Taps are told apart here, as a touch on a handle
 * that takes the whole gesture (touch-action none) sends no dblclick in every browser, and a tap never moves the column.
 */
function Resizer({ width, onChange }: { width: number; onChange?: (width: number, keep: boolean) => void }) {
  const press = useRef<{ x: number; moved: boolean } | null>(null)
  const lastTap = useRef(Number.NEGATIVE_INFINITY)
  const at = (e: PointerEvent<HTMLDivElement>) => {
    const frame = e.currentTarget.parentElement?.getBoundingClientRect()
    return frame ? clampColumn(((frame.right - e.clientX) / frame.width) * 100) : width
  }
  const key = (e: KeyboardEvent<HTMLDivElement>) => {
    const to = {
      ArrowLeft: width + columnWidths.step,
      ArrowRight: width - columnWidths.step,
      Home: columnWidths.max,
      End: columnWidths.min,
    }[e.key]
    if (to === undefined || !onChange) return
    e.preventDefault()
    onChange(clampColumn(to), true)
  }
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize latest commits"
      aria-valuemin={columnWidths.min}
      aria-valuemax={columnWidths.max}
      aria-valuenow={width}
      aria-valuetext={`${width}% of the screen`}
      tabIndex={onChange ? 0 : -1}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId)
        press.current = { x: e.clientX, moved: false }
      }}
      onPointerMove={(e) => {
        const p = press.current
        if (!p || !e.currentTarget.hasPointerCapture(e.pointerId)) return
        if (!p.moved && Math.abs(e.clientX - p.x) < slop) return
        p.moved = true
        onChange?.(at(e), false)
      }}
      onPointerUp={(e) => {
        e.currentTarget.releasePointerCapture(e.pointerId)
        const p = press.current
        press.current = null
        if (p?.moved) {
          onChange?.(at(e), true)
          lastTap.current = Number.NEGATIVE_INFINITY
          return
        }
        if (e.timeStamp - lastTap.current < doubleTap) {
          onChange?.(columnWidths.initial, true)
          lastTap.current = Number.NEGATIVE_INFINITY
        } else lastTap.current = e.timeStamp
      }}
      onPointerCancel={() => {
        press.current = null
      }}
      onKeyDown={key}
      title="Drag to resize; double-tap for the usual width"
      // As wide as the gap it replaces, a hairline to see; the hit area reaches past it for a finger.
      className="group relative hidden cursor-col-resize touch-none justify-center outline-none before:absolute before:inset-y-0 before:-inset-x-2 before:content-[''] landscape:flex"
    >
      <span className="my-6 w-px rounded-full bg-border transition-all group-hover:w-0.5 group-hover:bg-primary group-focus-visible:w-0.5 group-focus-visible:bg-primary group-active:w-0.5 group-active:bg-primary" />
    </div>
  )
}

/** Bar is the controls a tap or a key shows for a few seconds: back to the dashboard, the theme, full screen, the design mode. */
function Bar({ onDesign }: { onDesign?: () => void }) {
  const [shown, setShown] = useState(false)
  const [within, setWithin] = useState(false)
  const choice = useTheme()
  const [full, setFull] = useState(false)
  // Full screen where the browser has it (not the iPhone), known only in the browser: the server renders without it.
  const canFull = useSyncExternalStore(
    () => () => undefined,
    () => document.fullscreenEnabled,
    () => false,
  )
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    const wake = () => {
      setShown(true)
      clearTimeout(timer)
      timer = setTimeout(() => setShown(false), barFor)
    }
    const screen = () => setFull(!!document.fullscreenElement)
    document.addEventListener("pointerdown", wake)
    document.addEventListener("keydown", wake)
    document.addEventListener("fullscreenchange", screen)
    return () => {
      clearTimeout(timer)
      document.removeEventListener("pointerdown", wake)
      document.removeEventListener("keydown", wake)
      document.removeEventListener("fullscreenchange", screen)
    }
  }, [])
  const next = themes[(themes.indexOf(choice) + 1) % themes.length] ?? "system"
  const button = "rounded-md px-2.5 py-1.5 text-sm hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring outline-none"
  return (
    <nav
      aria-label="Presentation"
      onFocus={() => setWithin(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) setWithin(false)
      }}
      className={cn(
        "fixed bottom-3 left-1/2 z-50 flex -translate-x-1/2 items-center gap-1 rounded-lg border border-border bg-popover/95 p-1 text-popover-foreground shadow-xl backdrop-blur-md transition-opacity motion-reduce:transition-none",
        shown || within ? "opacity-100" : "pointer-events-none opacity-0",
      )}
    >
      {/* A whole page load: the dashboard draws its own theme and header. */}
      <a href="/" className={button}>
        Dashboard
      </a>
      <button type="button" className={button} onClick={() => apply(next)}>
        Theme: {choice === "system" ? "auto" : choice}
      </button>
      {canFull && (
        <button
          type="button"
          className={button}
          onClick={() => void (full ? document.exitFullscreen() : document.documentElement.requestFullscreen())}
        >
          {full ? "Leave full screen" : "Full screen"}
        </button>
      )}
      {onDesign && (
        <button type="button" className={button} onClick={onDesign}>
          Design this page
        </button>
      )}
    </nav>
  )
}

/**
 * PresentView is the presentation page (design/present.md): the layout's elements on one screen that never scrolls, for a tablet
 * or a screen on a wall. It keeps the screen awake, reloads on a new release and once an hour, and shifts by a pixel or two now and
 * then against burn-in; a tap shows its controls. only is the groups the address names.
 */
export function PresentView({
  work,
  only,
  column: initial = columnWidths.initial,
}: {
  work: Work
  only?: string[]
  /** the reader's commit column width (the cookie `scp-present-column`) */
  column?: number
}) {
  const [column, setColumn] = useState(initial)
  const resize = (w: number, keep: boolean) => {
    setColumn(w)
    if (keep) remember(columnCookie, String(w))
  }
  const { data, error } = useQuery(dashboardQuery(api, work))
  const { data: page } = useQuery(pageQuery(api, "present"))
  const { data: me } = useQuery(meQuery(api))
  const [designing, setDesigning] = useState(false)
  const layout = page?.elements ?? defaultPresent
  const count = Math.max(30, ...layout.map((e) => (e.kind === "latest-commits" ? (e.count ?? 30) : 0)))
  const { data: log } = useQuery(workQuery(api, work, only ? scoped : count))
  useWall()
  const shift = useShift()
  if (!data) return <div className="p-4">{error && <ProblemAlert error={error} />}</div>

  const members = new Set(within(data.groups, undefined, only).flatMap((g) => g.members.map((m) => m.address)))
  const commits = (log?.commits ?? data.latest).filter((c) => !only || members.has(c.repo))
  return (
    <>
      <h1 className="sr-only">Presentation</h1>
      {designing ? (
        <div className="fixed inset-0 overflow-auto bg-background p-4">
          <Designer
            page="present"
            groups={data.groups}
            preview={(draft) => (
              <Screen layout={draft} data={data} commits={commits} only={only} frame="relative h-[40rem]" column={column} />
            )}
            onClose={() => setDesigning(false)}
          />
        </div>
      ) : (
        <>
          <Screen
            layout={layout}
            data={data}
            commits={commits}
            only={only}
            frame={cn("fixed inset-0", shift)}
            column={column}
            onColumn={resize}
          />
          <Bar onDesign={me?.admin ? () => setDesigning(true) : undefined} />
        </>
      )}
    </>
  )
}
