"use client"

import type { ReactNode } from "react"
import type { Schemas } from "@/lib/api/client"
import { useNow } from "@/lib/clock"
import { cn } from "@/lib/cn"

/**
 * The insights' graphs (design/insights.md, IN3): inline SVG drawn from the API's numbers, in the theme's tokens. One hue light to
 * dark for magnitude, the emphasis colour for what the card is about and grey for the rest, the fleet map's activity colours for
 * activity. Every mark carries a title (its hover text and its name for assistive technology), every chart a text alternative.
 */

type Product = Schemas["InsightProduct"]

/** Scroll keeps a wide chart readable on a phone: it scrolls sideways, as a region a keyboard can reach. */
function Scroll({ label, min, children }: { label: string; min: number; children: ReactNode }) {
  return (
    <div
      role="region"
      aria-label={label}
      tabIndex={0}
      className="mt-3 overflow-x-auto rounded-md focus-visible:ring-2 focus-visible:ring-ring"
    >
      <div className={cn("w-full", min >= 900 ? "min-w-[900px]" : min >= 700 ? "min-w-[700px]" : "min-w-[560px]")}>
        {children}
      </div>
    </div>
  )
}

/** Legend names what the colours of a chart mean, once, beside the chart. */
function Legend({ items }: { items: { label: string; className: string }[] }) {
  return (
    <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-label="Legend">
      {items.map((i) => (
        <li key={i.label} className="flex items-center gap-1.5">
          <svg width="10" height="10" aria-hidden="true">
            <rect width="10" height="10" rx="2" className={i.className} />
          </svg>
          {i.label}
        </li>
      ))}
    </ul>
  )
}

const month = (d: Date) => d.toLocaleString("en", { month: "short", timeZone: "UTC" })

/** monday is the start of d's week (Monday, UTC). */
function monday(d: Date) {
  const t = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()))
  t.setUTCDate(t.getUTCDate() - ((t.getUTCDay() + 6) % 7))
  return t
}

/** depth is the sequential step of a count: 0 is empty, then four steps of the primary hue, darker for more. */
const depth = (n: number, top: number) => (n === 0 ? 0 : Math.min(4, Math.ceil((n / Math.max(top, 1)) * 4)))
const steps = [
  "fill-muted-foreground opacity-[0.12]",
  "fill-primary opacity-25",
  "fill-primary opacity-50",
  "fill-primary opacity-75",
  "fill-primary",
]

const short = (s: string, n: number) => (s.length > n ? `${s.slice(0, n - 1)}…` : s)

/**
 * ReleaseRhythm is a heatmap of the stable releases of the last 52 weeks: a row per product (the busiest first), a column per week,
 * the depth of a cell the releases of that week, the row's total and median interval at its end. It shows who ships, how steadily and
 * where it went quiet.
 */
export function ReleaseRhythm({ products }: { products: Product[] }) {
  const now = useNow()
  const rows = [...products]
    .filter((p) => p.releases > 0)
    .sort((a, b) => b.releases - a.releases || a.name.localeCompare(b.name))
    .slice(0, 12)
  if (rows.length === 0) return null
  const weeks = rows[0]?.weekly.length ?? 52
  const top = Math.max(1, ...rows.flatMap((p) => p.weekly))
  const left = 150
  const cell = 11
  const pitch = 13
  const rowH = 20
  const head = 18
  const width = left + weeks * pitch + 96
  const height = head + rows.length * rowH + 4
  const last = monday(new Date(now))
  const starts = Array.from({ length: weeks }, (_, i) => {
    const d = new Date(last)
    d.setUTCDate(d.getUTCDate() - 7 * (weeks - 1 - i))
    return d
  })
  return (
    <div>
      <Scroll label="Release rhythm, scrolls sideways" min={900}>
        <svg
          role="img"
          aria-label={`Stable releases per week over the last ${weeks} weeks, per product: ${rows
            .map((p) => `${p.name} ${p.releases}`)
            .join(", ")}`}
          viewBox={`0 0 ${width} ${height}`}
          className="h-auto w-full"
        >
          {starts.map((d, i) =>
            i === 0 || d.getUTCMonth() !== (starts[i - 1] as Date).getUTCMonth() ? (
              <text key={d.toISOString()} x={left + i * pitch} y={10} className="fill-muted-foreground text-[9px]">
                {month(d)}
              </text>
            ) : null,
          )}
          {rows.map((p, r) => {
            const y = head + r * rowH
            return (
              <g key={p.group}>
                <text x={left - 8} y={y + 12} textAnchor="end" className="fill-foreground text-[11px]">
                  {short(p.name, 22)}
                </text>
                {p.weekly.map((n, i) => (
                  <rect key={i} x={left + i * pitch} y={y} width={cell} height={cell + 3} rx={2} className={steps[depth(n, top)]}>
                    <title>{`${p.name}, week of ${(starts[i] as Date).toISOString().slice(0, 10)}: ${n === 0 ? "no stable release" : n === 1 ? "1 stable release" : `${n} stable releases`}`}</title>
                  </rect>
                ))}
                <text x={left + weeks * pitch + 8} y={y + 12} className="fill-muted-foreground text-[10px]">
                  {p.releases} · {p.medianDays === null || p.medianDays === undefined ? "–" : `${p.medianDays} d`}
                </text>
              </g>
            )
          })}
        </svg>
      </Scroll>
      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <span className="flex items-center gap-1" aria-hidden="true">
          Fewer
          <svg width="64" height="11" aria-hidden="true">
            {steps.map((c, i) => (
              <rect key={i} x={i * 13} width="11" height="11" rx="2" className={c} />
            ))}
          </svg>
          More
        </span>
        <span>At the right of a row: its stable releases held, and the median days between its last five.</span>
      </div>
    </div>
  )
}

/** Bar is one row of RankedBars. */
export interface Bar {
  label: string
  value: number
  /** the number as written beside the bar */
  text: string
  /** an explanation shown on hover */
  hint?: string
  /** drawn in the emphasis colour; the rest are grey */
  emphasis?: boolean
}

/** RankedBars is ranked horizontal bars on one baseline: the rows' labels at the left, the value written at the end of each bar. */
export function RankedBars({ bars, label, max }: { bars: Bar[]; label: string; max?: number }) {
  if (bars.length === 0) return null
  const top = max ?? Math.max(1, ...bars.map((b) => b.value))
  const left = 150
  const track = 230
  const rowH = 28
  const height = bars.length * rowH + 4
  return (
    <svg
      role="img"
      aria-label={`${label}: ${bars.map((b) => `${b.label} ${b.text}`).join(", ")}`}
      viewBox={`0 0 ${left + track + 80} ${height}`}
      width={left + track + 80}
      height={height}
      className="mt-3 h-auto max-w-full"
    >
      {bars.map((b, i) => {
        const w = Math.max((b.value / top) * track, 2)
        const y = i * rowH + 2
        return (
          <g key={b.label}>
            <title>{b.hint ?? `${b.label}: ${b.text}`}</title>
            <text x={left - 8} y={y + 16} textAnchor="end" className="fill-foreground text-[12.5px]">
              {short(b.label, 22)}
            </text>
            <rect
              x={left}
              y={y + 3}
              width={w}
              height={14}
              rx={3}
              className={b.emphasis ? "fill-primary" : "fill-muted-foreground opacity-45"}
            />
            <text x={left + w + 8} y={y + 16} className="fill-foreground text-[12px]">
              {b.text}
            </text>
          </g>
        )
      })}
    </svg>
  )
}

/**
 * VersionLadder is the versions a runtime is named at, oldest to newest: a dot per version sized by the repositories on it, the count
 * above it and the version under it. The oldest is the emphasis: the stragglers are what to see.
 */
export function VersionLadder({ runtime }: { runtime: Schemas["InsightRuntime"] }) {
  const v = runtime.versions
  if (v.length === 0) return null
  const pitch = 78
  const top = Math.max(...v.map((x) => x.repos))
  const width = 60 + v.length * pitch
  return (
    <div className="mt-3">
      <p className="text-sm font-medium">
        {runtime.name} <span className="font-normal text-muted-foreground">in {runtime.repos} repositories</span>
      </p>
      <Scroll label={`Versions of ${runtime.name}`} min={Math.max(560, width)}>
        <svg
          role="img"
          aria-label={`Versions of ${runtime.name}, oldest first: ${v.map((x) => `${x.version} in ${x.repos}`).join(", ")}`}
          viewBox={`0 0 ${width} 92`}
          width={width}
          height={92}
          className="h-auto max-w-full"
        >
          <line x1={30} x2={width - 30} y1={42} y2={42} className="stroke-border" strokeWidth={2} />
          {v.map((x, i) => {
            const r = 5 + 11 * Math.sqrt(x.repos / top)
            const cx = 30 + pitch / 2 + i * pitch - 8
            return (
              <g key={x.version}>
                <title>{`${runtime.name} ${x.version}: ${x.repos === 1 ? "1 repository" : `${x.repos} repositories`}${i === 0 && v.length > 1 ? " (the oldest)" : ""}`}</title>
                <circle
                  cx={cx}
                  cy={42}
                  r={r}
                  className={i === 0 && v.length > 1 ? "fill-primary stroke-background" : "fill-muted-foreground opacity-50"}
                  strokeWidth={2}
                />
                <text x={cx} y={42 - r - 6} textAnchor="middle" className="fill-foreground text-[11px]">
                  {x.repos}
                </text>
                <text x={cx} y={82} textAnchor="middle" className="fill-muted-foreground text-[10px]">
                  {short(x.version, 10)}
                </text>
              </g>
            )
          })}
        </svg>
      </Scroll>
    </div>
  )
}

/**
 * DependencyMatrix is the libraries most depended on (rows) against the repositories that use most of them (columns): a filled cell
 * where the repository depends on the library. A full row is a library everything leans on, a column filled in many rows a
 * repository tied to many libraries.
 */
export function DependencyMatrix({ matrix, retired }: { matrix: Schemas["InsightMatrix"]; retired: Set<string> }) {
  if (matrix.libraries.length === 0 || matrix.repos.length === 0) return null
  const name = (r: string) => r.slice(r.indexOf("/") + 1)
  const left = 190
  const cell = 18
  const head = 110
  const width = left + matrix.repos.length * cell + 90
  const height = head + matrix.libraries.length * cell + 6
  const uses = matrix.repos.map((r) => new Set(r.uses))
  return (
    <div>
      <Scroll label="Dependency matrix, scrolls sideways" min={matrix.repos.length > 14 ? 700 : 560}>
        <svg
          role="img"
          aria-label={`Which of the ${matrix.repos.length} repositories that use most of the libraries depend on which of the ${matrix.libraries.length} most depended on libraries`}
          viewBox={`0 0 ${width} ${height}`}
          width={width}
          height={height}
          className="h-auto max-w-full"
        >
          {matrix.repos.map((r, c) => (
            <text
              key={r.repo}
              transform={`translate(${left + c * cell + cell / 2 + 4} ${head - 6}) rotate(-55)`}
              className="fill-muted-foreground text-[10px]"
            >
              {short(name(r.repo), 18)}
            </text>
          ))}
          {matrix.libraries.map((lib, i) => {
            const y = head + i * cell
            return (
              <g key={lib}>
                <text x={left - 8} y={y + 13} textAnchor="end" className="fill-foreground text-[11px]">
                  {short(name(lib), 24)}
                  {retired.has(lib) ? " (retired)" : ""}
                </text>
                {matrix.repos.map((r, c) => {
                  const on = (uses[c] as Set<number>).has(i)
                  return (
                    <rect
                      key={r.repo}
                      x={left + c * cell + 1}
                      y={y + 1}
                      width={cell - 3}
                      height={cell - 3}
                      rx={3}
                      className={on ? "fill-primary" : "fill-muted-foreground opacity-[0.12]"}
                    >
                      <title>{`${r.repo} ${on ? "depends on" : "does not depend on"} ${lib}`}</title>
                    </rect>
                  )
                })}
              </g>
            )
          })}
        </svg>
      </Scroll>
      <p className="mt-1 text-xs text-muted-foreground">
        A filled square is a repository that depends on the library; a full row is a library everything leans on.
      </p>
    </div>
  )
}

/**
 * Concentration is how much of the people's recent commits the busiest author wrote, in bands of 10%, over the repositories with
 * enough commits held: the shape of where knowledge sits. The bands at or over the threshold are the emphasis. It never says who.
 */
export function Concentration({ bands, threshold }: { bands: Schemas["InsightBand"][]; threshold: number }) {
  const top = Math.max(1, ...bands.map((b) => b.repos))
  const w = 34
  const base = 112
  return (
    <div>
      <div className="mt-3">
        <svg
          role="img"
          aria-label={`Repositories by the share of the people's recent commits the busiest author wrote: ${bands
            .map((b) => `${b.from} to ${b.to} percent ${b.repos}`)
            .join(", ")}`}
          viewBox={`0 0 ${bands.length * w + 20} 140`}
          width={bands.length * w + 20}
          height={140}
          className="h-auto max-w-full"
        >
          {bands.map((b, i) => {
            const h = (b.repos / top) * 80
            const hot = b.from >= threshold
            return (
              <g key={b.from}>
                <title>{`${b.repos === 1 ? "1 repository" : `${b.repos} repositories`} where the busiest author wrote ${b.from} to ${b.to}% of the people's commits`}</title>
                <rect
                  x={10 + i * w + 4}
                  y={base - h}
                  width={w - 8}
                  height={Math.max(h, b.repos > 0 ? 2 : 1)}
                  rx={3}
                  className={hot ? "fill-primary" : "fill-muted-foreground opacity-45"}
                />
                {b.repos > 0 && (
                  <text x={10 + i * w + w / 2} y={base - h - 6} textAnchor="middle" className="fill-foreground text-[11px]">
                    {b.repos}
                  </text>
                )}
                <text x={10 + i * w + w / 2} y={base + 14} textAnchor="middle" className="fill-muted-foreground text-[10px]">
                  {b.from}%
                </text>
              </g>
            )
          })}
          <text x={bands.length * w + 10} y={10} textAnchor="end" className="fill-primary text-[10px]">
            One person wrote {threshold}%+ →
          </text>
        </svg>
      </div>
    </div>
  )
}

const bandClass = {
  active: "fill-[var(--active)]",
  recent: "fill-[var(--recent)]",
  quiet: "fill-[var(--quiet)]",
  dormant: "fill-[var(--dormant)]",
} as const

/**
 * ActivityBars is each product's repositories split by how recently they changed (the fleet map's bands and colours): a bar per
 * product, the fleet on top. The segments' counts are written on them where they fit.
 */
export function ActivityBars({ total, products }: { total: Schemas["InsightAgeing"]; products: Schemas["InsightActivity"][] }) {
  const rows = [
    { group: "", name: "The fleet", active: total.active, recent: total.recent, quiet: total.quiet, dormant: total.dormant },
    ...products,
  ]
  const widest = Math.max(1, ...rows.map((r) => r.active + r.recent + r.quiet + r.dormant))
  const left = 130
  const track = 270
  const rowH = 28
  return (
    <div>
      <div className="mt-3">
        <svg
          role="img"
          aria-label={`Repositories by activity: ${rows.map((r) => `${r.name} ${r.active} active, ${r.recent} recent, ${r.quiet} quiet, ${r.dormant} dormant`).join("; ")}`}
          viewBox={`0 0 ${left + track + 50} ${rows.length * rowH + 4}`}
          width={left + track + 50}
          height={rows.length * rowH + 4}
          className="h-auto max-w-full"
        >
          {rows.map((r, i) => {
            const y = i * rowH + 2
            const n = r.active + r.recent + r.quiet + r.dormant
            let x = left
            return (
              <g key={r.group || "all"}>
                <text
                  x={left - 8}
                  y={y + 16}
                  textAnchor="end"
                  className={cn("text-[12px]", i === 0 ? "fill-foreground font-semibold" : "fill-foreground")}
                >
                  {short(r.name, 18)}
                </text>
                {(["active", "recent", "quiet", "dormant"] as const).map((k) => {
                  const w = (r[k] / widest) * track
                  if (r[k] === 0) return null
                  const at = x
                  x += w
                  return (
                    <g key={k}>
                      <title>{`${r.name}: ${r[k]} ${k}`}</title>
                      <rect x={at} y={y + 3} width={Math.max(w - 2, 1)} height={16} rx={3} className={bandClass[k]} />
                      {w > 22 && (
                        <text x={at + (w - 2) / 2} y={y + 15} textAnchor="middle" className="fill-white text-[10px] font-medium">
                          {r[k]}
                        </text>
                      )}
                    </g>
                  )
                })}
                <text x={left + (n / widest) * track + 8} y={y + 15} className="fill-muted-foreground text-[10px]">
                  {n}
                </text>
              </g>
            )
          })}
        </svg>
      </div>
      <Legend
        items={[
          { label: "Active: changed in the last 30 days", className: "fill-[var(--active)]" },
          { label: "Recent", className: "fill-[var(--recent)]" },
          { label: "Quiet", className: "fill-[var(--quiet)]" },
          { label: "Dormant", className: "fill-[var(--dormant)]" },
        ]}
      />
    </div>
  )
}
