"use client"

import { Fragment, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react"
import { CardTitle } from "@/components/ui/card"
import { PanelRightIcon } from "@/components/ui/icons"
import { cn } from "@/lib/cn"
import {
  remember,
  sectionsCookie,
  sectionsValue,
  sideCookie,
  sideValue,
  sideWidths,
  type SectionKind,
  type Side,
} from "@/lib/preferences"

// The panel's widths as classes, one per rem: the server renders the remembered width, and the content security policy allows no
// style attribute. Tailwind finds them here as written.
const widths: Record<number, string> = {
  18: "lg:w-[18rem]",
  19: "lg:w-[19rem]",
  20: "lg:w-[20rem]",
  21: "lg:w-[21rem]",
  22: "lg:w-[22rem]",
  23: "lg:w-[23rem]",
  24: "lg:w-[24rem]",
  25: "lg:w-[25rem]",
  26: "lg:w-[26rem]",
  27: "lg:w-[27rem]",
  28: "lg:w-[28rem]",
  29: "lg:w-[29rem]",
  30: "lg:w-[30rem]",
  31: "lg:w-[31rem]",
  32: "lg:w-[32rem]",
  33: "lg:w-[33rem]",
  34: "lg:w-[34rem]",
  35: "lg:w-[35rem]",
  36: "lg:w-[36rem]",
  37: "lg:w-[37rem]",
  38: "lg:w-[38rem]",
  39: "lg:w-[39rem]",
  40: "lg:w-[40rem]",
}

/** sectionLabels are the sections' names, as their buttons say them. */
export const sectionLabels: Record<SectionKind, string> = {
  "latest-commits": "Latest commits",
  pulls: "Pull requests",
  issues: "Issues",
}

const clamp = (w: number) => Math.min(Math.max(Math.round(w), sideWidths.min), sideWidths.max)

/** Section is what a side panel's element gets (design/layout.md, L5): open or folded, the way to fold it, and the panel's button. */
export interface Section {
  open: boolean
  toggle: () => void
  /** the section's name, as its button says it ("Hide issues") */
  label: string
  /** always in a column of fixed height (the presentation page), not only on a wide screen */
  fixed?: boolean
}

/** sectionCard is a section's card: open, its share of the panel's height on a wide screen; folded, its header alone. */
export const sectionCard = (s: Section | undefined) =>
  s
    ? cn("flex flex-col", !s.open ? "flex-none" : s.fixed ? "min-h-0 flex-1 basis-0" : "lg:min-h-0 lg:flex-1 lg:basis-0")
    : undefined

/** sectionBody is a section's list: on a wide screen it scrolls inside the section, below the header that stays. */
export const sectionBody = (s: Section | undefined) =>
  s
    ? s.fixed
      ? "min-h-0 flex-1 overflow-y-auto -mr-2 pr-2"
      : "lg:min-h-0 lg:flex-1 lg:overflow-y-auto lg:-mr-2 lg:pr-2"
    : undefined

/** SectionTitle is an element's title. */
export function SectionTitle({ id, children }: { id: string; children: ReactNode }) {
  return (
    <CardTitle id={id} className="min-w-0 flex-1">
      {children}
    </CardTitle>
  )
}

/**
 * SectionFold is a section's button at its header's end: it hides the section (folds it to its header, the open ones take its
 * space) and shows it again. Hiding the last open one folds the whole panel to its rail.
 */
export function SectionFold({ id, section }: { id: string; section?: Section }) {
  if (!section) return null
  const say = `${section.open ? "Hide" : "Show"} ${section.label.toLowerCase()}`
  return (
    <button
      type="button"
      onClick={section.toggle}
      aria-expanded={section.open}
      aria-controls={section.open ? `${id}-body` : undefined}
      aria-label={say}
      title={say}
      className="grid size-7 shrink-0 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
    >
      <PanelRightIcon open={section.open} className="size-4" />
    </button>
  )
}

/**
 * SidePanel puts the sections (the latest commits, the pull requests, the issues) at the right of main (the products) on a wide
 * screen: sticky while the products scroll and as tall as the window below the header, the open sections sharing its height
 * equally, each with its inner scroll. Each section's button hides it and shows it again (kept in `scp-side-sections`); hiding the
 * last open one folds the panel to a rail, whose button shows them all again. Its edge is dragged (or moved with the arrow keys) to
 * its width; the rail and the width are kept in `scp-side-commits`. On a narrow screen the sections follow main at their own height
 * and still hide.
 */
export function SidePanel({
  main,
  sections,
  initial,
}: {
  main: ReactNode
  sections: { kind: SectionKind; render: (s: Section) => ReactNode }[]
  initial: Side
}) {
  const [side, setSide] = useState(initial)
  const root = useRef<HTMLDivElement>(null)
  const label = "Side panel"
  const save = (s: Side) => {
    setSide(s)
    remember(sideCookie, sideValue(s))
  }
  const toggle = (kind: SectionKind) => {
    const folded = side.folded.includes(kind) ? side.folded.filter((k) => k !== kind) : [...side.folded, kind]
    // The last open one hidden: the whole panel folds to its rail, and comes back with every section open.
    const all = sections.every((x) => folded.includes(x.kind))
    const next = all ? { ...side, open: false, folded: [] } : { ...side, folded }
    remember(sectionsCookie, sectionsValue(next.folded))
    save(next)
  }
  const resizeTo = (width: number) => setSide((s) => ({ ...s, width: clamp(width) }))

  const drag = (e: PointerEvent<HTMLDivElement>) => {
    if (!e.currentTarget.hasPointerCapture(e.pointerId)) return
    const right = root.current?.getBoundingClientRect().right ?? 0
    const rem = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16
    resizeTo((right - e.clientX) / rem)
  }
  const key = (e: KeyboardEvent) => {
    const to = { ArrowLeft: side.width + 1, ArrowRight: side.width - 1, Home: sideWidths.max, End: sideWidths.min }[e.key]
    if (to === undefined) return
    e.preventDefault()
    save({ ...side, width: clamp(to) })
  }

  const anyOpen = sections.some((x) => !side.folded.includes(x.kind))

  return (
    <div ref={root} className="flex flex-col gap-6 lg:flex-row lg:items-start lg:gap-0">
      <div className="min-w-0 flex-1">{main}</div>
      {side.open ? (
        <>
          <div
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize the side panel"
            aria-valuemin={sideWidths.min}
            aria-valuemax={sideWidths.max}
            aria-valuenow={side.width}
            tabIndex={0}
            onPointerDown={(e) => e.currentTarget.setPointerCapture(e.pointerId)}
            onPointerMove={drag}
            onPointerUp={(e) => {
              e.currentTarget.releasePointerCapture(e.pointerId)
              save(side)
            }}
            onDoubleClick={() => save({ ...side, width: sideWidths.initial })}
            onKeyDown={key}
            title="Drag to resize; double-click for the usual width"
            className="group hidden w-6 shrink-0 cursor-col-resize touch-none self-stretch justify-center outline-none lg:flex"
          >
            <span className="my-2 w-px rounded-full bg-border transition-colors group-hover:w-0.5 group-hover:bg-primary group-focus-visible:w-0.5 group-focus-visible:bg-primary group-active:bg-primary" />
          </div>
          <aside
            aria-label={label}
            className={cn(
              "flex min-w-0 shrink-0 flex-col gap-4 lg:sticky lg:top-20 lg:max-h-[calc(100dvh-6rem)] lg:overflow-y-auto",
              anyOpen && "lg:h-[calc(100dvh-6rem)]",
              widths[side.width],
            )}
          >
            {sections.map((x) => (
              <Fragment key={x.kind}>
                {x.render({ open: !side.folded.includes(x.kind), toggle: () => toggle(x.kind), label: sectionLabels[x.kind] })}
              </Fragment>
            ))}
          </aside>
        </>
      ) : (
        <aside aria-label={label} className="shrink-0 lg:sticky lg:top-20 lg:ml-4">
          <button
            type="button"
            onClick={() => save({ ...side, open: true })}
            aria-expanded={false}
            aria-label="Show the side panel"
            title="Show the side panel"
            className="panel flex w-full items-center gap-2 px-3 py-2 text-xs font-medium tracking-widest text-muted-foreground uppercase transition-colors hover:text-foreground lg:w-10 lg:flex-col lg:px-0 lg:py-3"
          >
            <PanelRightIcon open={false} className="size-4 shrink-0" />
            <span className="lg:[writing-mode:vertical-rl]">{label}</span>
          </button>
        </aside>
      )}
    </div>
  )
}
