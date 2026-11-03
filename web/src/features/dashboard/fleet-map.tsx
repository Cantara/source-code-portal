import Link from "next/link"
import type { Schemas } from "@/lib/api/client"
import { ago, count } from "@/lib/format"
import { addressPath } from "@/lib/paths"
import { Toggletip } from "@/shared/toggletip"
import { activities, activityAt, type Activity } from "@/entities/repo/health"

type Group = Schemas["DashboardGroup"]

const label = Object.fromEntries(activities.map((a) => [a.level, a.label])) as Record<Activity, string>

function tally(members: Group["members"], now: number): Record<Activity, number> {
  const t: Record<Activity, number> = { active: 0, recent: 0, quiet: 0, dormant: 0, archived: 0 }
  for (const m of members) t[activityAt(m.lastAuthoredAt, m.archived, now)]++
  return t
}

function summary(t: Record<Activity, number>): string {
  return (
    activities
      .filter((a) => t[a.level] > 0)
      .map((a) => `${count(t[a.level])} ${a.label.toLowerCase()}`)
      .join(", ") || "no repositories"
  )
}

/**
 * FleetMap is every repository as one tile, grouped by product and coloured by the last change by a person, so big products look
 * big and quiet ones look cold. Each product's counts are its text for screen readers; the tiles are for the eye (and the mouse:
 * each opens its repository, its name and age in the title), while the products below carry the keyboard's links.
 */
export function FleetMap({ groups, now }: { groups: Group[]; now: number }) {
  const all = new Map<string, Group["members"][number]>()
  for (const g of groups) for (const m of g.members) all.set(m.address, m)
  const total = tally([...all.values()], now)
  return (
    <section aria-labelledby="fleet" className="panel flex flex-col gap-3 p-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <div className="flex items-center gap-1.5">
          <h2 id="fleet" className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
            Fleet
          </h2>
          <Toggletip label="About the fleet map">
            <p>
              Each tile is one repository, grouped by product and coloured by the age of its last commit by a person. Bots,
              release-plugin commits and sweeps (the same subject in more than 10 repositories on one day) do not count.
            </p>
            <ul className="mt-2 flex flex-col gap-0.5">
              {activities.map((a) => (
                <li key={a.level} className={`lvl-${a.level} flex items-center gap-1.5`}>
                  <span aria-hidden="true" className="cell" />
                  <span className="font-medium">{a.label}</span>: {a.within}
                </li>
              ))}
            </ul>
            <p className="mt-2">
              Dormant also covers a repository with no commit by a person among those read. A repository in several products is
              counted once in the totals. Select a tile to open its repository.
            </p>
          </Toggletip>
        </div>
        <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-label="Activity of every repository">
          {activities.map((a) => (
            <li key={a.level} className={`lvl-${a.level} flex items-center gap-1.5`} title={a.within}>
              <span aria-hidden="true" className="cell" />
              {a.label}
              {a.span && <span className="font-mono">{a.span}</span>}
              <span className="font-mono text-foreground">{count(total[a.level])}</span>
            </li>
          ))}
        </ul>
      </div>
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,14rem),1fr))] gap-x-6 gap-y-3">
        {groups.map((g) => (
          <li key={g.id} className="min-w-0">
            <p className="flex items-baseline justify-between gap-2 text-xs">
              <span className="truncate font-medium">{g.name}</span>
              <span className="font-mono text-muted-foreground tabular-nums">{count(g.members.length)}</span>
            </p>
            <p className="sr-only">{summary(tally(g.members, now))}</p>
            <div aria-hidden="true" className="mt-1 flex flex-wrap gap-0.5">
              {g.members.map((m) => {
                const level = activityAt(m.lastAuthoredAt, m.archived, now)
                const age = m.lastAuthoredAt ? `last change by a person ${ago(m.lastAuthoredAt, now)} ago` : "no change read"
                return (
                  <Link
                    key={m.address}
                    href={addressPath(m.address)}
                    tabIndex={-1}
                    className={`lvl-${level} cell transition-transform hover:scale-150 motion-reduce:transition-none`}
                    title={`${m.name}: ${label[level]}, ${age}`}
                  />
                )
              })}
            </div>
          </li>
        ))}
      </ul>
    </section>
  )
}
