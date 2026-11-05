"use client"

import Link from "next/link"
import { useLayoutEffect, useRef, useState } from "react"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { ago, count, plural } from "@/lib/format"
import { groupPath } from "@/lib/paths"
import { levels } from "@/entities/health/health"
import { FindingMark } from "@/entities/health/findings"
import { levelLabel, window } from "@/lib/rules"
import { useRules } from "@/lib/rules-context"
import { Hint } from "@/shared/hint"
import { healthCell } from "./fleet-map"
import { Sparkline } from "@/shared/sparkline"
import { qualifier, type Work } from "@/lib/preferences"
import { counted, noun, series, weekly } from "./work"

type Group = Schemas["DashboardGroup"]

/** tileFindings is how many findings a wide tile offers (the dashboard carries 5); two lines show as many as fit. */
const tileFindings = 5

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
  const rules = useRules()
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
          // The level as the fleet map draws it (watch round, attention outlined) and how many repositories are at it.
          <Hint
            heading={levelLabel(h.level, rules)}
            swatch={levels[h.level].className}
            meta={at > 0 ? plural(at, "repository", "repositories") : undefined}
            render={(t) => (
              <span {...t} className="flex shrink-0 cursor-help items-center gap-1.5 font-mono text-sm text-(--lvl-fg)">
                <span aria-hidden="true" className={`cell ${healthCell(h.level)}`} />
                {at > 0 && <span aria-hidden="true">{count(at)}</span>}
                <span className="sr-only">
                  {levelLabel(h.level, rules)}
                  {at > 0 && `: ${plural(at, "repository", "repositories")}`}
                </span>
              </span>
            )}
          />
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
        // The signal and the repository, as the dashboard draws a finding: its mark (the kind's icon on its severity's tile, the
        // whole story in its hint, a link to the finding and its next step on the repository's page) and the repository's name.
        // Two lines at most, the most costly first; the rest are on the product's page.
        <ul className="flex max-h-[3.25rem] flex-wrap gap-x-3 gap-y-1 overflow-hidden text-[0.9375rem]">
          {h.findings.slice(0, tileFindings).map((f) => (
            <li key={f.repo + f.code} className="flex min-w-0 max-w-full items-center gap-1.5">
              <FindingMark finding={f} label={<span className="truncate font-medium">{f.repo.split("/")[1]}</span>} />
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}

function Narrow({ g, work: w, now }: { g: Group; work: Work; now: number }) {
  const rules = useRules()
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
          {g.health && g.health.level !== "good" && (
            <span className="text-(--lvl-fg)">{levelLabel(g.health.level, rules)} · </span>
          )}
          {a.lastAuthoredAt ? `${ago(a.lastAuthoredAt, now)} ago` : "no change read"}
          {g.health ? "" : " · not scored"}
        </span>
        <Sparkline parts={series(a, w)} label={weekly(a, w)} className="h-4 w-14 shrink-0" />
      </p>
    </li>
  )
}

/** people is "ada, bo and 3 others": the people of an activity by login. */
function people(a: Group["activity"]): string {
  const names = a.authors.map((p) => p.login || p.name)
  const more = a.authorCount - names.length
  const parts = more > 0 ? [...names, plural(more, "other", "others")] : names
  return parts.length > 1 ? `${parts.slice(0, -1).join(", ")} and ${parts.at(-1)}` : (parts[0] ?? "")
}

/**
 * Tall is a good product with room to spare: two rows of the grid for what its card on the dashboard says first, the work that
 * counts and its last change, its twelve weeks across the tile, and the people behind it.
 */
function Tall({ g, work: w, now }: { g: Group; work: Work; now: number }) {
  const rules = useRules()
  const a = g.activity
  return (
    <li
      className={cn(
        "panel row-span-2 flex min-w-0 flex-col justify-between gap-1 p-3",
        g.health && levels[g.health.level].className,
      )}
    >
      <h3 className="flex min-w-0 items-center gap-2 text-[0.9375rem] leading-tight font-semibold">
        <span aria-hidden="true" className="led" />
        <Link href={groupPath(g.id)} className="truncate hover:underline">
          {g.name}
        </Link>
      </h3>
      <p className="flex items-baseline justify-between gap-2 text-sm text-muted-foreground">
        <span className="min-w-0 truncate">
          <span className="font-mono text-base font-semibold text-foreground tabular-nums">{count(counted(a, w))}</span>{" "}
          {noun(counted(a, w), w)}
        </span>
        <span className="shrink-0 font-mono text-xs">{a.lastAuthoredAt ? ago(a.lastAuthoredAt, now) : "–"}</span>
      </p>
      <Sparkline parts={series(a, w)} label={weekly(a, w)} className="h-6 w-full" />
      <p className="truncate text-sm">
        {a.authorCount > 0 ? people(a) : <span className="text-muted-foreground">no one in {window(rules)}</span>}
      </p>
    </li>
  )
}

/** Grid is the tiles' grid as the browser laid it out: its columns, and the rows its box holds at the fixed row height. */
type Grid = { cols: number; rows: number }

function gridOf(grid: HTMLElement, box: HTMLElement): Grid {
  const style = getComputedStyle(grid)
  const cols = Math.max(1, style.gridTemplateColumns.split(" ").filter(Boolean).length)
  const row = parseFloat(style.gridAutoRows) || 68
  const gap = parseFloat(style.rowGap) || 0
  return { cols, rows: Math.max(1, Math.floor((box.clientHeight + gap) / (row + gap))) }
}

/** Plan is how the tiles are drawn: the products shown, each wide or not, and the ones folded into the last tile. */
export type Plan = { tiles: { g: Group; wide: boolean; tall: boolean }[]; folded: Group[] }

/**
 * plan is the tiles with steps given up by the products that need a look, the least important first: the mildest at watch lose
 * their wide tile, then they fold, the mildest first, then those at attention lose theirs (a product at attention never folds);
 * and the first good of the rest shown, the others folded; the first tall of those shown take two rows, richer.
 */
export function plan(ordered: Group[], steps: number, good: number, tall = 0): Plan {
  const tiles = ordered.map((g) => ({ g, wide: needs(g), tall: false, folded: false }))
  const at = (level: string) => tiles.filter((t) => t.g.health?.level === level).reverse()
  const moves: (() => void)[] = [
    ...at("watch").map((t) => () => (t.wide = false)),
    ...at("watch").map((t) => () => (t.folded = true)),
    ...at("attention").map((t) => () => (t.wide = false)),
  ]
  for (const m of moves.slice(0, steps)) m()
  tiles
    .filter((t) => !needs(t.g))
    .forEach((t, i) => {
      t.folded = i >= good
      t.tall = !t.folded && i < tall
    })
  return { tiles: tiles.filter((t) => !t.folded), folded: tiles.filter((t) => t.folded).map((t) => t.g) }
}

/**
 * fits says whether a plan's tiles, the fold tile last, fit the grid, placed as the browser places them (grid-auto-flow: row dense:
 * each tile at the first free place from the top left): a wide tile takes two columns (one on a single column) and two rows, a tall
 * one two rows.
 */
export function fits(p: Plan, grid: Grid): boolean {
  const { cols, rows } = grid
  const used = Array.from({ length: rows }, () => new Array<boolean>(cols).fill(false))
  const spans = [
    ...p.tiles.map((t) => (t.wide ? [Math.min(2, cols), 2] : t.tall ? [1, 2] : [1, 1])),
    ...(p.folded.length > 0 ? [[1, 1]] : []),
  ]
  for (const [w = 1, h = 1] of spans) {
    let placed = false
    for (let r = 0; r + h <= rows && !placed; r++)
      for (let c = 0; c + w <= cols && !placed; c++) {
        let free = true
        for (let y = r; y < r + h && free; y++) for (let x = c; x < c + w && free; x++) free = !used[y]?.[x]
        if (!free) continue
        for (let y = r; y < r + h; y++) for (let x = c; x < c + w; x++) (used[y] as boolean[])[x] = true
        placed = true
      }
    if (!placed) return false
  }
  return true
}

/** Choice is a plan's settings: the steps given up by the products that need a look, how many good ones show, how many tall. */
type Choice = { steps: number; good: number; tall: number }

/**
 * choose is what the grid shows: the fewest steps that let the products needing a look fit, then as many of the others as still
 * fit beside them, so the space a narrowed or folded tile left is used again; when every product shows, the room left makes good
 * ones richer.
 */
export function choose(ordered: Group[], grid: Grid): Choice {
  const rest = ordered.filter((g) => !needs(g)).length
  const max = ordered.reduce((n, g) => n + (g.health?.level === "watch" ? 2 : g.health?.level === "attention" ? 1 : 0), 0)
  let steps = 0
  while (steps < max && !fits(plan(ordered, steps, 0), grid)) steps++
  let good = rest
  while (good > 0 && !fits(plan(ordered, steps, good), grid)) good--
  // Room to spare once every product shows: the good ones take richer tiles, the most recent first, while they all still fit.
  let tall = 0
  if (good === rest) while (tall < good && fits(plan(ordered, steps, good, tall + 1), grid)) tall++
  return { steps, good, tall }
}

/** foldText says what the last tile holds: "7 more good", "3 more to watch, 7 good". */
export function foldText(folded: Group[]): string {
  const watch = folded.filter((g) => g.health?.level === "watch").length
  const good = folded.length - watch
  if (watch === 0) return `${count(good)} more${folded.every((g) => g.health?.level === "good") ? " good" : ""}`
  return good > 0 ? `${count(watch)} more to watch, ${count(good)} good` : `${count(watch)} more to watch`
}

/**
 * ProductTiles are the products compact, for a glance (design/present.md): a product at watch or attention takes a wide tile with
 * its costliest findings, the others a one-line tile. With fold, the tiles keep to the height they are given: the page places them
 * as the browser will (choose) and gives up detail in order of importance until they fit, then checks what it drew; what folds is
 * named in the last tile.
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
  const all = { steps: 0, good: ordered.length, tall: 0 }
  const [choice, setChoice] = useState<Choice>(all)
  const key = ordered.map((g) => `${g.id}:${g.health?.level ?? ""}`).join(",")

  useLayoutEffect(() => {
    if (!fold || !box.current || !grid.current) return
    const b = box.current
    const g = grid.current
    const measure = () => setChoice(choose(ordered, gridOf(g, b)))
    // Drawn and still cut off (a font or a rounding the placement did not foresee): one good fewer, or one more step.
    const check = () => {
      if (g.scrollHeight > b.clientHeight + 1)
        setChoice((c) =>
          c.tall > 0 ? { ...c, tall: c.tall - 1 } : c.good > 0 ? { ...c, good: c.good - 1 } : { ...c, steps: c.steps + 1 },
        )
    }
    measure()
    const sized = new ResizeObserver(measure)
    const drawn = new ResizeObserver(check)
    sized.observe(b)
    drawn.observe(g)
    return () => {
      sized.disconnect()
      drawn.disconnect()
    }
    // ordered is recomputed each render; key names what it holds.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fold, key])

  const { tiles, folded } = fold ? plan(ordered, choice.steps, choice.good, choice.tall) : plan(ordered, 0, ordered.length)
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
          {tiles.map(({ g, wide, tall }) =>
            wide ? (
              <Wide key={g.id} g={g} work={work} now={now} />
            ) : tall ? (
              <Tall key={g.id} g={g} work={work} now={now} />
            ) : (
              <Narrow key={g.id} g={g} work={work} now={now} />
            ),
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
