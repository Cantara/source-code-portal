import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { count, plural } from "@/lib/format"
import { levels } from "@/entities/health/health"
import { activityAt } from "@/entities/repo/health"

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

/** withAI is "43 with AI (Claude, Copilot)": the commits by people that name an AI, and the assistants. */
function withAI(a: Schemas["Activity"]): string {
  return `${count(a.ai)} with AI${a.assistants.length > 0 ? ` (${a.assistants.join(", ")})` : ""}`
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
          detail={`${plural(a.people, "commit", "commits")} by people in 30 days${a.ai > 0 ? `, ${withAI(a)}` : ""}`}
          level={a.authorCount > 0 ? "lvl-active" : "lvl-dormant"}
        />
        <Tile
          label="Active"
          value={count(activeMembers(data.groups, now))}
          detail={`of ${plural(data.repoCount, "repository", "repositories")} changed by a person in 30 days`}
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
