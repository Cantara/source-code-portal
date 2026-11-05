import type { ReactNode } from "react"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { count, plural } from "@/lib/format"
import { levels } from "@/entities/health/health"
import { activityAt } from "@/entities/repo/health"
import { by } from "@/lib/preferences"
import { assistants } from "./work"

function percent(part: number, whole: number): string {
  return whole === 0 ? "–" : `${Math.round((part / whole) * 100)}%`
}

/** activeMembers counts the grouped repositories a person changed in the last 30 days, each once. */
function activeMembers(groups: Schemas["DashboardGroup"][], now: number): number {
  const active = new Set<string>()
  for (const g of groups)
    for (const m of g.members) if (activityAt(m.lastAuthoredAt, m.archived, now) === "active") active.add(m.address)
  return active.size
}

/** Signal is one indicator of the fleet, the same on the dashboard (a tile) and on the presentation page (a figure in the strip). */
export type Signal = {
  key: "people" | "ai" | "automation" | "active" | "findings" | "sync"
  label: string
  value: string
  detail: string
  /** a level's colour: an LED beside the value */
  level?: string
  /** a kind of work's colour, as the sparklines draw it */
  dot?: string
  /** where a narrowing strip drops it (container queries); none: it always stays */
  drop?: string
}

/** kindDots are the kinds of work's colours, as the sparklines and the cards draw them (no inline style: the CSP forbids it). */
const kindDots = { hand: "bg-primary", ai: "bg-[var(--ai)]", automation: "bg-muted-foreground" } as const

/**
 * signals are the fleet's indicators in their order: the three kinds of work (people by hand, with AI or an agent, automation), the
 * repositories they changed, what needs a look, and whether the catalogue is current. One list, so the dashboard and the
 * presentation page say the same things in the same words.
 */
export function signals(data: Schemas["Dashboard"], now: number): Signal[] {
  const a = data.activity
  const f = data.freshness
  const checked = f.fresh + f.stale + f.error + f.unknown
  const h = data.health
  const attention = data.groups.filter((g) => g.health?.level === "attention").length
  const watch = data.groups.filter((g) => g.health?.level === "watch").length
  const products = plural(data.groups.length, "product", "products")
  return [
    {
      key: "people",
      label: "People",
      value: count(a.hand),
      detail: `commits by hand in 30 days, by ${plural(a.authorCount, "person", "people")}`,
      dot: kindDots.hand,
      drop: "@max-xl:hidden",
    },
    {
      key: "ai",
      label: "AI",
      value: count(a.ai),
      detail: `commits with AI or by an agent in 30 days${assistants(a)}`,
      dot: kindDots.ai,
      drop: "@max-xl:hidden",
    },
    {
      key: "automation",
      label: "Automation",
      value: count(a.automation + a.sweeps),
      detail:
        a.sweeps > 0
          ? `CI, dependency and release bots in 30 days; ${plural(a.sweeps, "commit", "commits")} in sweeps`
          : "CI, dependency and release bots in 30 days",
      dot: kindDots.automation,
      drop: "@max-3xl:hidden",
    },
    {
      key: "active",
      label: "Active",
      value: count(activeMembers(data.groups, now)),
      detail:
        `of ${plural(data.repoCount, "repository", "repositories")} changed ${by(data.work)} in 30 days` +
        (data.ungroupedCount > 0 ? `; ${count(data.ungroupedCount)} in no product` : ""),
      drop: "@max-3xl:hidden",
    },
    {
      key: "findings",
      label: "Findings",
      value: h ? count(h.findingCount) : "–",
      detail:
        attention > 0
          ? `${count(attention)} of ${products} need attention`
          : watch > 0
            ? `${count(watch)} of ${products} to watch`
            : `none of ${products} needs attention`,
      level: h ? levels[h.level].className : undefined,
    },
    {
      key: "sync",
      label: "In sync",
      value: percent(f.fresh, checked),
      detail: f.error > 0 ? `${count(f.error)} failing to update` : "the catalogue is current",
      level: f.error > 0 ? "lvl-danger" : "lvl-active",
    },
  ]
}

/** Mark is a signal's colour before its value: a level's LED, or a kind of work's dot. */
function SignalMark({ s, large }: { s: Signal; large?: boolean }) {
  if (s.level) return <span aria-hidden="true" className={cn("led", large && "led-live")} />
  if (s.dot) return <span aria-hidden="true" className={cn("inline-block size-2.5 shrink-0 rounded-xs", s.dot)} />
  return null
}

/**
 * Telemetry is the fleet at a glance, in signals: the three kinds of work, the repositories they changed, what needs a look, and
 * whether the catalogue is current; each a tile with what it counts.
 */
export function Telemetry({ data, now }: { data: Schemas["Dashboard"]; now: number }) {
  return (
    <section aria-label="Telemetry">
      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        {signals(data, now).map((s) => (
          <div key={s.key} className={cn("panel flex flex-col gap-1 p-3", s.level)}>
            <dt className="text-[0.6875rem] font-medium tracking-widest text-muted-foreground uppercase">{s.label}</dt>
            <dd className="flex items-center gap-2 font-mono text-2xl font-semibold tabular-nums sm:text-3xl">
              <SignalMark s={s} large />
              {s.value}
            </dd>
            <dd className="text-xs text-muted-foreground">{s.detail}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

/**
 * TelemetryStrip is the same signals in one line, for the presentation page: each its value and label, what it counts in its hint.
 * start opens the line (the brand), end closes it (the data's age). As its container narrows it keeps what matters most: the
 * repositories changed and automation go first, then people and AI; the findings and the sync always stay.
 */
export function TelemetryStrip({
  data,
  now,
  start,
  end,
}: {
  data: Schemas["Dashboard"]
  now: number
  start?: ReactNode
  end?: ReactNode
}) {
  return (
    <section aria-label="Telemetry" className="panel flex items-center gap-x-6 px-4 py-2">
      {start}
      <dl className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-5 gap-y-1">
        {signals(data, now).map((s) => (
          <div
            key={s.key}
            title={`${s.label}: ${s.value}, ${s.detail}`}
            className={cn("flex items-baseline gap-1.5", s.level, s.drop)}
          >
            <dt className="order-last text-sm text-muted-foreground">{s.label}</dt>
            <dd className="flex items-baseline gap-1.5">
              <span className="self-center">
                <SignalMark s={s} />
              </span>
              <span className="font-mono text-xl font-semibold tabular-nums">{s.value}</span>
            </dd>
          </div>
        ))}
      </dl>
      {end}
    </section>
  )
}
