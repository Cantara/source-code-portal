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
          {h.findings.slice(0, tileFindings).map((f, i) => (
            // A narrow container keeps the costliest finding only.
            <li key={f.repo + f.code} className={cn("truncate", i > 0 && "@max-xl:hidden")}>
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
          {g.health && g.health.level !== "good" && <span className="text-(--lvl-fg)">{levels[g.health.level].label} · </span>}
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

/** Plan is how the tiles are drawn: the products shown, each wide or not, and the ones folded into the last tile. */
export type Plan = { tiles: { g: Group; wide: boolean }[]; folded: Group[] }

/**
 * plan is the tiles after giving up steps of detail, the least important first, so what matters most stays (design/present.md):
 * the good products fold from the end; then the products at watch lose their wide tile from the mildest; then they fold, the mildest
 * first; then those at attention lose their wide tile. A product at attention never folds.
 */
export function plan(ordered: Group[], steps: number): Plan {
  const tiles = ordered.map((g) => ({ g, wide: needs(g), folded: false }))
  const at = (level: string) => tiles.filter((t) => (t.g.health?.level ?? "none") === level).reverse()
  const rest = tiles.filter((t) => !needs(t.g)).reverse()
  const moves: (() => void)[] = [
    ...rest.map((t) => () => (t.folded = true)),
    ...at("watch").map((t) => () => (t.wide = false)),
    ...at("watch").map((t) => () => (t.folded = true)),
    ...at("attention").map((t) => () => (t.wide = false)),
  ]
  for (const m of moves.slice(0, steps)) m()
  return { tiles: tiles.filter((t) => !t.folded), folded: tiles.filter((t) => t.folded).map((t) => t.g) }
}

/** steps is how many steps of plan there are: once each product could give up all it can. */
const stepsOf = (ordered: Group[]) =>
  ordered.reduce((n, g) => n + (g.health?.level === "watch" ? 2 : g.health?.level === "attention" ? 1 : 1), 0)

/** cost is the cells a plan takes, the fold tile included. */
const cost = (p: Plan, wide: number) => p.tiles.reduce((n, t) => n + (t.wide ? wide : 1), 0) + (p.folded.length > 0 ? 1 : 0)

/** foldText says what the last tile holds: "7 more good", "3 more to watch, 7 good". */
export function foldText(folded: Group[]): string {
  const watch = folded.filter((g) => g.health?.level === "watch").length
  const good = folded.length - watch
  if (watch === 0) return `${count(good)} more${folded.every((g) => g.health?.level === "good") ? " good" : ""}`
  return good > 0 ? `${count(watch)} more to watch, ${count(good)} good` : `${count(watch)} more to watch`
}

/**
 * ProductTiles are the products compact, for a glance (design/present.md): a product at watch or attention takes a wide tile with
 * its costliest findings, the others a one-line tile. With fold, the tiles keep to the height they are given, giving up detail in
 * order of importance (plan) until they fit, and measuring again after they are drawn; what folds is named in the last tile.
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
  const [steps, setSteps] = useState(0)
  const max = stepsOf(ordered)
  const key = ordered.map((g) => `${g.id}:${g.health?.level ?? ""}`).join(",")

  // From the box's size, the fewest steps whose plan fits.
  useLayoutEffect(() => {
    if (!fold || !box.current || !grid.current) return
    const b = box.current
    const g = grid.current
    const measure = () => {
      const c = capacity(g, b)
      let s = 0
      while (s < max && cost(plan(ordered, s), c.wide) > c.cells) s++
      setSteps(s)
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(b)
    return () => ro.disconnect()
    // ordered is recomputed each render; key names what it holds.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fold, key, max])

  // The estimate counts cells; dense packing can leave a hole. Drawn and still too tall: one more step.
  useLayoutEffect(() => {
    if (!fold || !box.current || !grid.current) return
    if (grid.current.scrollHeight > box.current.clientHeight + 1 && steps < max) setSteps(steps + 1)
  }, [fold, steps, max])

  const { tiles, folded } = fold ? plan(ordered, steps) : plan(ordered, 0)
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
          {tiles.map(({ g, wide }) =>
            wide ? <Wide key={g.id} g={g} work={work} now={now} /> : <Narrow key={g.id} g={g} work={work} now={now} />,
          )}
          {folded.length > 0 && (
            <li
              className={cn(
                "panel flex items-center px-3 py-2",
                folded.some(needs) ? levels.watch.className : levels.good.className,
              )}
            >
              <Link href="/" className="text-[0.9375rem] font-medium hover:underline">
                {foldText(folded)}
                <span className="sr-only">: {folded.map((g) => g.name).join(", ")}</span>
              </Link>
            </li>
          )}
        </ul>
      </div>
    </section>
  )
}
