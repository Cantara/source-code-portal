"use client"

import Link from "next/link"
import type { Schemas } from "@/lib/api/client"
import { ago, count } from "@/lib/format"
import { addressPath } from "@/lib/paths"
import { by, every, qualifier, type Work } from "@/lib/preferences"
import { Hint } from "@/shared/hint"
import { Toggletip } from "@/shared/toggletip"
import { levels } from "@/entities/health/health"
import { activities, activityAt, type Activity } from "@/entities/repo/health"

type Group = Schemas["DashboardGroup"]

const label = Object.fromEntries(activities.map((a) => [a.level, a.label])) as Record<Activity, string>

function tally(members: Group["members"], now: number): Record<Activity, number> {
  const t: Record<Activity, number> = { active: 0, recent: 0, quiet: 0, dormant: 0, archived: 0 }
  for (const m of members) t[activityAt(m.lastAuthoredAt, m.archived, now)]++
  return t
}

/** ages say what each level means, of the last change of the work that counts. */
const ages: Record<Activity, string> = {
  active: "in the last 30 days",
  recent: "in the last 6 months",
  quiet: "in the last 2 years",
  dormant: "more than 2 years ago, or none among the commits read",
  archived: "archived on GitHub, whatever its age",
}

/** short is each level's range in a few words, for its hint. */
const short: Record<Activity, string> = {
  active: "within 30 days",
  recent: "within 6 months",
  quiet: "within 2 years",
  dormant: "over 2 years ago, or none read",
  archived: "",
}

/** plural is a count of repositories in words. */
function plural(n: number): string {
  return `${count(n)} ${n === 1 ? "repository" : "repositories"}`
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
 * FleetMap is every repository as one tile, grouped by product and coloured by the last change of the work that counts, so big products look
 * big and quiet ones look cold. Each product's counts are its text for screen readers; the tiles are for the eye and the pointer
 * (a Hint names each one with its activity and health the moment it is pointed at or tapped; a click opens it), while the
 * products below carry the keyboard's links. The legend's levels explain themselves the same way, from the keyboard too.
 */
export function FleetMap({ groups, work = every, now }: { groups: Group[]; work?: Work; now: number }) {
  const all = new Map<string, Group["members"][number]>()
  for (const g of groups) for (const m of g.members) all.set(m.address, m)
  const total = tally([...all.values()], now)
  return (
    <section aria-labelledby="fleet" className="panel relative z-10 flex flex-col gap-3 p-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <div className="flex items-center gap-1.5">
          <h2 id="fleet" className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
            Fleet
          </h2>
          <Toggletip label="About the fleet map">
            <p>
              Each tile is one repository, grouped by product and coloured by the age of its last change {by(work)} (Settings
              choose the work that counts). Bots, release-plugin commits and sweeps (the same subject in more than 10 repositories
              on one day) never count.
            </p>
            <ul className="mt-2 flex flex-col gap-0.5">
              {activities.map((a) => (
                <li key={a.level} className={`lvl-${a.level} flex items-center gap-1.5`}>
                  <span aria-hidden="true" className="cell" />
                  <span>
                    <span className="font-medium">{a.label}</span>: {ages[a.level]}
                  </span>
                </li>
              ))}
            </ul>
            <p className="mt-2">
              A repository in several products is counted once in the totals. Select a tile to open its repository.
            </p>
          </Toggletip>
        </div>
        <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-label="Activity of every repository">
          {activities.map((a) => (
            <Hint
              key={a.level}
              heading={a.label}
              swatch={`lvl-${a.level}`}
              meta={plural(total[a.level])}
              render={(t) => (
                <li
                  {...t}
                  tabIndex={0}
                  className={`lvl-${a.level} flex cursor-help items-center gap-1.5 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring`}
                >
                  <span aria-hidden="true" className="cell" />
                  {a.label}
                  {a.span && <span>{a.span}</span>}
                  <span className="font-mono text-foreground">{count(total[a.level])}</span>
                </li>
              )}
            >
              {a.level === "archived" ? "Archived on GitHub" : `Last change${qualifier(work)} ${short[a.level]}`}
            </Hint>
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
                return (
                  <Hint
                    key={m.address}
                    heading={m.name}
                    rows={[
                      {
                        swatch: `lvl-${level}`,
                        label: label[level],
                        value: m.lastAuthoredAt ? `${ago(m.lastAuthoredAt, now)} ago` : "none read",
                      },
                      ...(m.level ? [{ swatch: levels[m.level].className, label: "Health", value: levels[m.level].label }] : []),
                    ]}
                    render={(t) => (
                      <Link
                        {...t}
                        href={addressPath(m.address)}
                        tabIndex={-1}
                        className={`lvl-${level} cell transition-transform hover:scale-150 motion-reduce:transition-none`}
                      />
                    )}
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
