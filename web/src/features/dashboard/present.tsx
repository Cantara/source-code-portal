"use client"

import { useInfiniteQuery, useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useEffect, useRef, useState, useSyncExternalStore, type KeyboardEvent, type PointerEvent, type ReactNode } from "react"
import { api, type Commit, type Schemas } from "@/lib/api/client"
import { snapshotQuery } from "@/lib/api/snapshot"
import { useNow } from "@/lib/clock"
import { cn } from "@/lib/cn"
import { useConnectionFailing, useLive } from "@/lib/connection"
import { ago } from "@/lib/format"
import { addressPath } from "@/lib/paths"
import {
  by,
  columnCookie,
  columnDefaults,
  columnsValue,
  columnWidths,
  forget,
  profiles,
  remember,
  sameWork,
  presentSectionsCookie,
  sectionKinds,
  sectionsValue,
  type Columns,
  type Profile,
  type SectionKind,
  type Work,
} from "@/lib/preferences"
import { PanelRightIcon } from "@/components/ui/icons"
import { Mark } from "@/components/ui/mark"
import { AiMark } from "@/entities/commit/ai-mark"
import { InitialsAvatar } from "@/shared/initials-avatar"
import { ProblemAlert } from "@/shared/problem-alert"
import { meQuery } from "@/lib/api/queries"
import { useFeatures } from "@/lib/rules-context"
import { apply, useTheme, type Choice } from "@/lib/theme"
import type { Notice } from "@/entities/announcement/announcement"
import { DiffStat } from "@/entities/commit/diffstat"
import { Announcements } from "./announcements"
import { dashboardQuery, moreQuery, morePages, pageQuery, workQuery } from "./api"
import { dedupe } from "./latest"
import { useProfile } from "./profile"
import { ShowPicker, useShow } from "./show"
import { Designer } from "./design"
import { useShift, useWall } from "./wall"
import { FleetMap } from "./fleet-map"
import { defaultPresent, pick, type Element } from "./layout"
import { TelemetryStrip } from "./telemetry"
import { ProductTiles } from "./tiles"
import { Pulls } from "./pulls"
import { Issues } from "./issues"
import { sectionLabels, SectionFold, type Section } from "./side"

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

/** idleTop is how long a scrolled column waits untouched before it goes back to the newest commits (a screen on a wall). */
const idleTop = 60_000

/**
 * Stream is the newest commits of the work that counts: the author's avatar while the column is wide enough, then the repository
 * with who and when on one line and the subject on the next, the whole width its own. The column scrolls: at its end it reads
 * the next pages of the same commits (keep: the products the address names), and a minute untouched it goes back to the top and
 * lets the older ones go, so a wall shows the newest again. A commit that arrives while the page is open slides in at the top
 * (not with reduced motion).
 */
function Stream({
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
  const [more, setMore] = useState(false)
  const q = useInfiniteQuery({ ...moreQuery(api, work), enabled: more })
  const read = more ? (q.data?.pages.flatMap((p) => p.commits) ?? []) : []
  const top = new Set(commits.map((c) => c.repo + c.sha))
  const rows = dedupe([...commits, ...read.filter(keep)]).map((c) => {
    const id = c.repo + c.sha
    return { c, id, fresh: top.has(id) && !shown.has(id) }
  })
  const list = useRef<HTMLOListElement>(null)
  const end = useRef<HTMLLIElement>(null)
  const visible = useRef(false)
  const next = useRef(() => {})
  const pages = q.data?.pages.length ?? 0
  useEffect(() => {
    next.current = () => {
      if (!more) setMore(true)
      else if (q.hasNextPage && !q.isFetching && pages < morePages) void q.fetchNextPage()
    }
  })
  useEffect(() => {
    const root = list.current
    const el = end.current
    if (!root || !el || typeof IntersectionObserver === "undefined") return
    const seen = new IntersectionObserver(
      (es) => {
        visible.current = es.some((e) => e.isIntersecting)
        if (visible.current) next.current()
      },
      { root, rootMargin: "0px 0px 300px 0px" },
    )
    seen.observe(el)
    return () => seen.disconnect()
  }, [])
  useEffect(() => {
    if (more && !q.isFetching && visible.current) next.current()
  }, [more, q.isFetching, pages])
  // Back to the newest after a minute untouched.
  const idle = useRef<number | undefined>(undefined)
  const scrolled = () => {
    window.clearTimeout(idle.current)
    if ((list.current?.scrollTop ?? 0) === 0) return
    idle.current = window.setTimeout(() => {
      list.current?.scrollTo({ top: 0, behavior: "smooth" })
      setMore(false)
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
            {more && q.isFetching
              ? "Loading more…"
              : more && (pages >= morePages || (q.isSuccess && !q.hasNextPage))
                ? "The rest is under All commits"
                : ""}
          </li>
        </ol>
      )}
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
  column = columnDefaults["handheld-landscape"],
  usual = column,
  profile,
  onColumn,
  shown = data.work,
  picker,
  notices = [],
  hidden = [],
  onHide,
  onShowAll,
}: {
  layout: Element[]
  data: Schemas["Dashboard"]
  commits: Commit[]
  only?: string[]
  frame: string
  /** the side column's width in percent of the screen, in landscape */
  column?: number
  /** resizes the column as its edge is dragged; kept when the drag ends (keep) */
  onColumn?: (width: number, keep: boolean) => void
  /** the profile's usual width of the side column, and the profile (data-profile on the frame) */
  usual?: number
  profile?: Profile
  /** the kinds the side column shows, and the label that chooses them */
  shown?: Work
  picker?: ReactNode
  /** the announcements the server gave this page */
  notices?: Notice[]
  /** the side column's hidden sections, and the ways to hide one and to show them all */
  hidden?: SectionKind[]
  onHide?: (kind: SectionKind) => void
  onShowAll?: () => void
}) {
  const now = useNow()
  const features = useFeatures()
  const scope = only ? { ...data, groups: within(data.groups, undefined, only) } : data
  const groups = (el: Element) => within(data.groups, el.groups, only)
  // The older commits the column reads as it scrolls keep to the products the address names, as the newest do.
  const members = new Set(scope.groups.flatMap((g) => g.members.map((m) => m.address)))
  const keep = (c: Commit) => !only || members.has(c.repo)
  // One product named in the address: its pull requests; more than one: the fleet's, as the strip counts them.
  const one = only?.length === 1 ? scope.groups[0] : undefined
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
      case "announcements":
        left.push(<Announcements key={i} list={notices} wall />)
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
      case "pulls":
      case "issues":
        break
    }
  })
  // The latest commits, the pull requests and the issues are the side column's sections, in the layout's order (design/present.md).
  const on = (e: Element) => (e.kind !== "pulls" || features.pulls) && (e.kind !== "issues" || features.issues)
  const kinds = layout.filter((e) => (sectionKinds as readonly string[]).includes(e.kind) && on(e))
  const allHidden = kinds.length > 0 && kinds.every((e) => hidden.includes(e.kind as SectionKind))
  const section = (e: Element): Section => {
    const kind = e.kind as SectionKind
    return { open: !hidden.includes(kind), toggle: () => onHide?.(kind), label: sectionLabels[kind], fixed: true }
  }
  const draw = (e: Element) => {
    const sec = section(e)
    switch (e.kind) {
      case "latest-commits":
        return (
          <Stream
            key={`stream-${shown.join(",")}-${sec.open}`}
            commits={commits.slice(0, e.count ?? 30)}
            work={shown}
            keep={keep}
            picker={picker}
            section={sec}
          />
        )
      case "pulls":
        return (
          <Pulls
            key="pulls"
            work={data.work}
            group={one?.id}
            counts={one?.pulls ?? data.pulls}
            n={e.count ?? 8}
            section={sec}
            column
          />
        )
      case "issues":
        return <Issues key="issues" work={data.work} group={one?.id} n={e.count ?? 8} section={sec} column />
    }
    return null
  }
  return (
    <div
      data-profile={profile}
      data-split={profile !== "handheld-portrait" || undefined}
      className={cn(
        "grid grid-cols-1 gap-3 overflow-hidden p-3 text-[0.9375rem] split:grid-rows-1 split:gap-x-0",
        kinds.length === 0
          ? "split:grid-cols-1"
          : allHidden
            ? "grid-rows-[minmax(0,1fr)_auto] split:grid-cols-[minmax(0,1fr)_auto] split:gap-x-3"
            : cn("grid-rows-[minmax(0,1fr)_minmax(0,38%)]", columns[column]),
        frame,
      )}
    >
      {/* A container: as the side column widens, the elements keep the signals that matter most (container queries). */}
      <div className="@container flex min-h-0 min-w-0 flex-col gap-3 overflow-hidden">
        {!strip && (
          <div className="flex items-center justify-between gap-4">
            <Brand />
            <Freshness />
          </div>
        )}
        {left}
      </div>
      {kinds.length > 0 && !allHidden && <Resizer width={column} usual={usual} onChange={onColumn} />}
      {kinds.length > 0 && !allHidden && (
        <aside aria-label="Side column" className="@container flex min-h-0 min-w-0 flex-col gap-3 overflow-hidden">
          {kinds.map((e) => (
            // Each section's share is its wrapper's, padding apart, so the open ones are equal (design/present.md).
            <div
              key={e.kind}
              className={
                hidden.includes(e.kind as SectionKind) ? "flex flex-none flex-col" : "flex min-h-0 flex-1 basis-0 flex-col"
              }
            >
              {draw(e)}
            </div>
          ))}
        </aside>
      )}
      {allHidden && (
        <aside aria-label="Side column">
          <button
            type="button"
            onClick={onShowAll}
            aria-expanded={false}
            aria-label="Show the side column"
            title="Show the side column"
            className="panel flex h-full w-full items-center gap-2 px-3 py-2 text-xs font-medium tracking-widest text-muted-foreground uppercase transition-colors hover:text-foreground split:w-10 split:flex-col split:px-0 split:py-3"
          >
            <PanelRightIcon open={false} className="size-4 shrink-0" />
            <span className="split:[writing-mode:vertical-rl]">Side column</span>
          </button>
        </aside>
      )}
    </div>
  )
}

/** columns are the landscape grid at each width of the side column, as written classes (the CSP allows no style attribute). */
const columns: Record<number, string> = {
  20: "split:grid-cols-[minmax(0,1fr)_0.75rem_20%]",
  25: "split:grid-cols-[minmax(0,1fr)_0.75rem_25%]",
  30: "split:grid-cols-[minmax(0,1fr)_0.75rem_30%]",
  35: "split:grid-cols-[minmax(0,1fr)_0.75rem_35%]",
  40: "split:grid-cols-[minmax(0,1fr)_0.75rem_40%]",
  45: "split:grid-cols-[minmax(0,1fr)_0.75rem_45%]",
  50: "split:grid-cols-[minmax(0,1fr)_0.75rem_50%]",
  55: "split:grid-cols-[minmax(0,1fr)_0.75rem_55%]",
  60: "split:grid-cols-[minmax(0,1fr)_0.75rem_60%]",
}

const clampColumn = (w: number) =>
  Math.min(columnWidths.max, Math.max(columnWidths.min, Math.round(w / columnWidths.step) * columnWidths.step))

/** A press that moves less than this is a tap, not a drag; two taps within doubleTap bring back the usual width. */
const slop = 4
const doubleTap = 400

/**
 * Resizer is the side column's edge in split: dragged (a finger or a pointer) or moved with the arrow keys, in steps of 5% of
 * the screen from 20% to 60%; a double tap or click brings back the profile's usual width. Taps are told apart here, as a touch on a handle
 * that takes the whole gesture (touch-action none) sends no dblclick in every browser, and a tap never moves the column.
 */
function Resizer({
  width,
  usual,
  onChange,
}: {
  width: number
  /** the profile's width, which a double tap brings back */
  usual: number
  onChange?: (width: number, keep: boolean) => void
}) {
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
      aria-label="Resize the side column"
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
          onChange?.(usual, true)
          lastTap.current = Number.NEGATIVE_INFINITY
        } else lastTap.current = e.timeStamp
      }}
      onPointerCancel={() => {
        press.current = null
      }}
      onKeyDown={key}
      title="Drag to resize; double-tap for the usual width"
      // As wide as the gap it replaces, a hairline to see; the hit area reaches past it for a finger.
      className="group relative hidden cursor-col-resize touch-none justify-center outline-none before:absolute before:inset-y-0 before:-inset-x-2 before:content-[''] split:flex"
    >
      <span className="my-6 w-px rounded-full bg-border transition-all group-hover:w-0.5 group-hover:bg-primary group-focus-visible:w-0.5 group-focus-visible:bg-primary group-active:w-0.5 group-active:bg-primary" />
    </div>
  )
}

/** Bar is the controls a tap or a key shows for a few seconds: back to the dashboard, the theme, full screen, the design mode. */
function Bar({
  profile,
  size,
  wall,
  onWall,
  onDesign,
}: {
  profile: Profile
  /** the window's size in CSS pixels, so two people can tell they look at the same screen */
  size?: string
  wall: boolean
  onWall: (wall: boolean) => void
  onDesign?: () => void
}) {
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
      {/* The profile this screen is in, and the wall it can be told it is (a wall has a desk's pixels). */}
      <span className="px-2 text-xs font-medium tracking-widest text-muted-foreground uppercase">
        {profiles.find((x) => x.value === profile)?.label}
        {size && <span className="ml-1.5 font-mono font-normal tracking-normal normal-case">{size}</span>}
      </span>
      <button type="button" className={button} aria-pressed={wall} onClick={() => onWall(!wall)}>
        {wall ? "Not a wall" : "On a wall"}
      </button>
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
  columns: initial = columnDefaults,
  profile: guess = "handheld-landscape",
  wall: told = false,
  ask,
  show,
  hidden: hiddenAtFirst = [],
}: {
  work: Work
  only?: string[]
  /** the side column's width per profile (the cookie `scp-present-column`) */
  columns?: Columns
  /** the profile the page last found on this screen (the cookie `scp-present-profile`), for the first paint */
  profile?: Profile
  /** whether this screen was told it is on a wall (the cookie `scp-present-wall`), and the address's `?profile=` */
  wall?: boolean
  ask?: string
  /** the kinds the reader shows in the side column (the cookie `scp-latest-work`), if any */
  show?: Work
  /** the side column's hidden sections (the cookie `scp-present-sections`) */
  hidden?: SectionKind[]
}) {
  const s = useShow(show, work)
  const [hidden, setHidden] = useState(hiddenAtFirst)
  const keepHidden = (next: SectionKind[]) => {
    setHidden(next)
    const v = sectionsValue(next)
    if (v) remember(presentSectionsCookie, v)
    else forget(presentSectionsCookie)
  }
  // Hiding the last open section folds the column to its rail; showing it brings every section back (design/present.md).
  const hide = (kind: SectionKind) => keepHidden(hidden.includes(kind) ? hidden.filter((k) => k !== kind) : [...hidden, kind])
  const p = useProfile(guess, told, ask)
  const [columns, setColumns] = useState(initial)
  const column = columns[p.profile]
  const resize = (w: number, keep: boolean) => {
    const next = { ...columns, [p.profile]: w }
    setColumns(next)
    if (!keep) return
    const v = columnsValue(next)
    if (v) remember(columnCookie, v)
    else forget(columnCookie)
  }
  const { data, error } = useQuery(dashboardQuery(api, work))
  const { data: page } = useQuery(pageQuery(api, "present"))
  const { data: me } = useQuery(meQuery(api))
  const [designing, setDesigning] = useState(false)
  const layout = page?.elements ?? defaultPresent
  const count = Math.max(30, ...layout.map((e) => (e.kind === "latest-commits" ? (e.count ?? 30) : 0)))
  const { data: log } = useQuery(workQuery(api, s.shown, only ? scoped : count))
  useWall()
  const shift = useShift()
  if (!data) return <div className="p-4">{error && <ProblemAlert error={error} />}</div>

  const members = new Set(within(data.groups, undefined, only).flatMap((g) => g.members.map((m) => m.address)))
  const commits = (log?.commits ?? (sameWork(s.shown, work) ? data.latest : [])).filter((c) => !only || members.has(c.repo))
  const picker = <ShowPicker shown={s.shown} follows={s.follows} activity={data.activity} onChange={s.set} />
  return (
    <>
      <h1 className="sr-only">Presentation</h1>
      {designing ? (
        <div className="fixed inset-0 overflow-auto bg-background p-4">
          <Designer
            page="present"
            groups={data.groups}
            preview={(draft) => (
              <Screen
                layout={draft}
                data={data}
                commits={commits}
                only={only}
                frame="relative h-[40rem]"
                column={column}
                profile={p.profile}
                notices={page?.announcements}
              />
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
            usual={columnDefaults[p.profile]}
            profile={p.profile}
            onColumn={resize}
            shown={s.shown}
            picker={picker}
            notices={page?.announcements}
            hidden={hidden}
            onHide={hide}
            onShowAll={() => keepHidden([])}
          />
          <Bar
            profile={p.profile}
            size={p.size}
            wall={p.wall}
            onWall={p.setWall}
            onDesign={me?.admin ? () => setDesigning(true) : undefined}
          />
        </>
      )}
    </>
  )
}
