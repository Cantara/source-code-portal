"use client"

import { useQuery } from "@tanstack/react-query"
import { useState, type ReactNode } from "react"
import { api, type Commit, type Schemas } from "@/lib/api/client"
import { useNow } from "@/lib/clock"
import { cn } from "@/lib/cn"
import {
  columnCookie,
  columnDefaults,
  columnsValue,
  forget,
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
import { ProblemAlert } from "@/shared/problem-alert"
import { meQuery } from "@/lib/api/queries"
import { useFeatures } from "@/lib/rules-context"
import type { Notice } from "@/entities/announcement/announcement"
import { Announcements } from "./announcements"
import { dashboardQuery, pageQuery, workQuery } from "./api"
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
import { sectionLabels, type Section } from "./side"
import { Brand, Freshness } from "./present-status"
import { Stream } from "./present-stream"
import { Bar, Resizer, columns } from "./present-controls"

/** A commit list asks for at most this many when the page shows only some products, and keeps theirs. */
const scoped = 200

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
