"use client"

import Link from "next/link"
import { useLayoutEffect, useRef, useState } from "react"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { ago, count, plural } from "@/lib/format"
import { groupPath } from "@/lib/paths"
import { levels } from "@/entities/health/health"
import { Sparkline } from "@/shared/sparkline"
import { qualifier, type Work } from "@/lib/preferences"
import { counted, noun, series, weekly } from "./work"

type Group = Schemas["DashboardGroup"]

/** tileFindings is how many findings a wide tile names; the product's page has the rest. */
const tileFindings = 2

const rank = { attention: 0, watch: 1, good: 2 } as const

/** needs is whether a product takes a wide tile: it is at watch or attention. */
const needs = (g: Group) => !!g.health && g.health.level !== "good"

/**
 * byNeed orders products for a glance: attention first, then watch (the lower score first), then the rest by their last change,
 * the newest first, and the unscored last.
 */
export function byNeed(groups: Group[]): Group[] {
  return [...groups].sort((a, b) => {
    const la = a.health ? rank[a.health.level] : 3
    const lb = b.health ? rank[b.health.level] : 3
    if (la !== lb) return la - lb
    if (la < 2 && a.health && b.health && a.health.score !== b.health.score) return a.health.score - b.health.score
    return (b.activity.lastAuthoredAt ?? "").localeCompare(a.activity.lastAuthoredAt ?? "")
  })
}

function Wide({ g, work: w, now }: { g: Group; work: Work; now: number }) {
  const h = g.health
  const a = g.activity
  const at = h ? (h.level === "attention" ? h.attention : h.watch) : 0
  return (
    <li
      className={cn(
        "panel flex min-w-0 flex-col justify-between gap-1 p-3 sm:col-span-2 row-span-2",
        h && levels[h.level].className,
      )}
    >
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="flex min-w-0 items-center gap-2 text-lg leading-tight font-semibold">
          <span aria-hidden="true" className="led led-live" />
          <Link href={groupPath(g.id)} className="truncate hover:underline">
            {g.name}
          </Link>
        </h3>
        {h && (
          <span className="chip shrink-0 text-xs">
            {levels[h.level].label}
            {at > 0 && ` · ${plural(at, "repository", "repositories")}`}
          </span>
        )}
      </div>
      <div className="flex items-end justify-between gap-3 text-[0.9375rem] text-muted-foreground">
        <span>
          <span className="font-mono text-xl font-semibold text-foreground tabular-nums">{count(counted(a, w))}</span>{" "}
          {noun(counted(a, w), w)} · {a.lastAuthoredAt ? `last ${ago(a.lastAuthoredAt, now)} ago` : `none${qualifier(w)} read`}
        </span>
        <Sparkline parts={series(a, w)} label={weekly(a, w)} className="h-6 w-20 shrink-0" />
      </div>
      {h && h.findings.length > 0 && (
        <ul className="flex flex-col text-[0.9375rem]">
          {h.findings.slice(0, tileFindings).map((f) => (
            <li key={f.repo + f.code} className="truncate">
              <span className="font-medium">{f.repo.split("/")[1]}</span>{" "}
              <span className="text-muted-foreground">{f.message}</span>
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}

function Narrow({ g, work: w, now }: { g: Group; work: Work; now: number }) {
  const a = g.activity
  return (
    <li
      className={cn("panel flex min-w-0 flex-col justify-center gap-0.5 px-3 py-2", g.health && levels[g.health.level].className)}
    >
      <h3 className="flex min-w-0 items-center gap-2 text-[0.9375rem] leading-tight font-semibold">
        <span aria-hidden="true" className="led" />
        <Link href={groupPath(g.id)} className="truncate hover:underline">
          {g.name}
        </Link>
      </h3>
      <p className="flex items-center justify-between gap-2 text-sm text-muted-foreground">
        <span className="truncate">
          {a.lastAuthoredAt ? `${ago(a.lastAuthoredAt, now)} ago` : "no change read"}
          {g.health ? "" : " · not scored"}
        </span>
        <Sparkline parts={series(a, w)} label={weekly(a, w)} className="h-4 w-14 shrink-0" />
      </p>
    </li>
  )
}

/**
 * capacity is how many grid cells the grid holds: its columns from the template the browser laid out, its rows from the height it
 * has and the fixed row height. A wide tile takes two columns (one on a single column) and two rows.
 */
function capacity(grid: HTMLElement, box: HTMLElement): { cells: number; wide: number } {
  const style = getComputedStyle(grid)
  const cols = Math.max(1, style.gridTemplateColumns.split(" ").filter(Boolean).length)
  const row = parseFloat(style.gridAutoRows) || 72
  const gap = parseFloat(style.rowGap) || 0
  const rows = Math.max(1, Math.floor((box.clientHeight + gap) / (row + gap)))
  return { cells: cols * rows, wide: Math.min(2, cols) * 2 }
}

/**
 * ProductTiles are the products compact, for a glance (design/present.md): a product at watch or attention takes a wide tile with
 * its costliest findings, the others a one-line tile. With fold, the tiles keep to the height they are given: the good ones that do
 * not fit fold from the end into one tile, "N more good"; a product that needs a look never folds.
 */
export function ProductTiles({
  groups,
  work,
  now,
  fold = false,
  heading = true,
}: {
  groups: Group[]
  work: Work
  now: number
  fold?: boolean
  /** false inside the products element, which has its own heading */
  heading?: boolean
}) {
  const ordered = byNeed(groups)
  const box = useRef<HTMLDivElement>(null)
  const grid = useRef<HTMLUListElement>(null)
  const [shown, setShown] = useState(ordered.length)
  const wide = ordered.filter(needs).length

  useLayoutEffect(() => {
    if (!fold || !box.current || !grid.current) return
    const b = box.current
    const g = grid.current
    const measure = () => {
      const c = capacity(g, b)
      const left = c.cells - wide * c.wide
      const rest = ordered.length - wide
      setShown(wide + (rest <= left ? rest : Math.max(0, left - 1)))
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(b)
    return () => ro.disconnect()
  }, [fold, wide, ordered.length])

  const folded = ordered.slice(shown)
  return (
    <section
      aria-labelledby={heading ? "products" : undefined}
      aria-label={heading ? undefined : "Product tiles"}
      className={cn("flex min-w-0 flex-col gap-2", fold && "min-h-0 flex-1")}
    >
      {heading && (
        <h2 id="products" className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
          Products
        </h2>
      )}
      <div ref={box} className={cn(fold && "min-h-0 flex-1 overflow-hidden")}>
        <ul
          ref={grid}
          className="grid auto-rows-[4.25rem] grid-flow-row-dense grid-cols-[repeat(auto-fill,minmax(min(100%,12rem),1fr))] gap-2"
        >
          {ordered
            .slice(0, shown)
            .map((g) =>
              needs(g) ? <Wide key={g.id} g={g} work={work} now={now} /> : <Narrow key={g.id} g={g} work={work} now={now} />,
            )}
          {folded.length > 0 && (
            <li className="panel lvl-active flex items-center px-3 py-2">
              <Link href="/" className="text-[0.9375rem] font-medium hover:underline">
                {count(folded.length)} more{folded.every((g) => g.health?.level === "good") ? " good" : ""}
                <span className="sr-only">: {folded.map((g) => g.name).join(", ")}</span>
              </Link>
            </li>
          )}
        </ul>
      </div>
    </section>
  )
}
