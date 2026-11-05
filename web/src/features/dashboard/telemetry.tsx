import type { ReactNode } from "react"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { count, plural } from "@/lib/format"
import { levels } from "@/entities/health/health"
import { activityAt } from "@/entities/repo/health"
import { by } from "@/lib/preferences"
import { assistants } from "./work"

function Tile({ label, value, detail, level }: { label: string; value: string; detail?: string; level?: string }) {
  return (
    <div className={cn("panel flex flex-col gap-1 p-3", level)}>
      <dt className="text-[0.6875rem] font-medium tracking-widest text-muted-foreground uppercase">{label}</dt>
      <dd className="flex items-center gap-2 font-mono text-2xl font-semibold tabular-nums sm:text-3xl">
        {level && <span aria-hidden="true" className="led led-live" />}
        {value}
      </dd>
      {detail && <dd className="text-xs text-muted-foreground">{detail}</dd>}
    </div>
  )
}

/** work says people's commits: by hand and with AI, in 30 days. */
function work(a: Schemas["Activity"]): string {
  return `${count(a.hand)} by hand, ${count(a.ai)} with AI${assistants(a)} in 30 days`
}

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

/**
 * Telemetry is the fleet at a glance, in signals: who works on it and how much, what automation does beside them, what needs a
 * look, and whether the catalogue is current.
 */
export function Telemetry({ data, now }: { data: Schemas["Dashboard"]; now: number }) {
  const a = data.activity
  const f = data.freshness
  const checked = f.fresh + f.stale + f.error + f.unknown
  const h = data.health
  const attention = data.groups.filter((g) => g.health?.level === "attention").length
  const watch = data.groups.filter((g) => g.health?.level === "watch").length
  return (
    <section aria-label="Telemetry">
      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        <Tile
          label="People"
          value={count(a.authorCount)}
          detail={work(a)}
          level={a.authorCount > 0 ? "lvl-active" : "lvl-dormant"}
        />
        <Tile
          label="Active"
          value={count(activeMembers(data.groups, now))}
          detail={`of ${plural(data.repoCount, "repository", "repositories")} changed ${by(data.work)} in 30 days`}
        />
        <Tile
          label="Automation"
          value={count(a.automation)}
          detail={
            a.sweeps > 0
              ? `CI, dependency and release bots in 30 days; ${plural(a.sweeps, "commit", "commits")} in sweeps`
              : "CI, dependency and release bots in 30 days"
          }
        />
        <Tile
          label="Findings"
          value={h ? count(h.findingCount) : "–"}
          detail={
            attention > 0
              ? `${plural(attention, "product needs", "products need")} attention`
              : watch > 0
                ? `${plural(watch, "product", "products")} to watch`
                : "no product needs attention"
          }
          level={h ? levels[h.level].className : undefined}
        />
        <Tile
          label="Products"
          value={count(data.groups.length)}
          detail={
            data.ungroupedCount > 0
              ? `${plural(data.ungroupedCount, "repository", "repositories")} in no group`
              : "every repository has a home"
          }
        />
        <Tile
          label="In sync"
          value={percent(f.fresh, checked)}
          detail={f.error > 0 ? `${count(f.error)} failing to update` : "the catalogue is current"}
          level={f.error > 0 ? "lvl-danger" : "lvl-active"}
        />
      </dl>
    </section>
  )
}

/** Figure is one of the strip's numbers with what it counts. */
function Figure({ value, label, level, className }: { value: string; label: string; level?: string; className?: string }) {
  return (
    <div className={cn("flex items-baseline gap-1.5", level, className)}>
      <dt className="sr-only">{label}</dt>
      <dd className="flex items-baseline gap-1.5">
        {level && <span aria-hidden="true" className="led self-center" />}
        <span className="font-mono text-xl font-semibold tabular-nums">{value}</span>
        <span className="text-[0.9375rem] text-muted-foreground" aria-hidden="true">
          {label}
        </span>
      </dd>
    </div>
  )
}

/**
 * TelemetryStrip is the telemetry in one line, for the presentation page: repositories, products, the products at attention and
 * at watch, the people of the last 30 days and their share with AI; end is what closes the line (the data's age). As its container
 * narrows it keeps what matters most: the people and the share with AI go first, then the products, then the repositories; the
 * products that need a look and the data's age always stay.
 */
export function TelemetryStrip({ data, end }: { data: Schemas["Dashboard"]; end?: ReactNode }) {
  const a = data.activity
  const attention = data.groups.filter((g) => g.health?.level === "attention").length
  const watch = data.groups.filter((g) => g.health?.level === "watch").length
  const all = a.hand + a.ai
  return (
    <section aria-label="Telemetry" className="panel flex flex-wrap items-center justify-between gap-x-6 gap-y-1 px-4 py-2">
      <dl className="flex flex-wrap items-baseline gap-x-6 gap-y-1">
        <Figure
          value={count(data.repoCount)}
          label={data.repoCount === 1 ? "repository" : "repositories"}
          className="@max-md:hidden"
        />
        <Figure
          value={count(data.groups.length)}
          label={data.groups.length === 1 ? "product" : "products"}
          className="@max-xl:hidden"
        />
        {attention > 0 && <Figure value={count(attention)} label="need attention" level={levels.attention.className} />}
        {watch > 0 && <Figure value={count(watch)} label="to watch" level={levels.watch.className} />}
        {attention === 0 && watch === 0 && <Figure value="0" label="need attention" level={levels.good.className} />}
        <Figure
          value={count(a.authorCount)}
          label={a.authorCount === 1 ? "person in 30 days" : "people in 30 days"}
          className="@max-3xl:hidden"
        />
        {all > 0 && <Figure value={percent(a.ai, all)} label="with AI" className="@max-3xl:hidden" />}
      </dl>
      {end}
    </section>
  )
}
