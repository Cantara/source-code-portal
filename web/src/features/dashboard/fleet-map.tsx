"use client"

import Link from "next/link"
import type { Schemas } from "@/lib/api/client"
import { ago, count } from "@/lib/format"
import { addressPath } from "@/lib/paths"
import { by, every, qualifier, type Work } from "@/lib/preferences"
import { Hint } from "@/shared/hint"
import { Toggletip } from "@/shared/toggletip"
import { levels } from "@/entities/health/health"
import { activityRule, levelLabel, span, type ActivityLevel, type Rules } from "@/lib/rules"
import { useRules } from "@/lib/rules-context"

type Group = Schemas["DashboardGroup"]
type Member = Group["members"][number]
type Level = Schemas["HealthLevel"]

/** Color is what colours the tiles: the age of the last change (the default) or the health level. */
export type Color = "activity" | "health"

const healthLevels: Level[] = ["attention", "watch", "good"]

/** healthCell is a tile's classes by health: the level's colour and its shape, so colour is never alone; grey when not scored. */
export function healthCell(level: Level | null): string {
  if (!level) return "lvl-dormant"
  return `${levels[level].className} ${level === "good" ? "" : `cell-${level}`}`
}

function healthTally(members: Member[]): Record<Level | "none", number> {
  const t = { attention: 0, watch: 0, good: 0, none: 0 }
  for (const m of members) t[m.level ?? "none"]++
  return t
}

function healthSummary(members: Member[], rules: Rules): string {
  const t = healthTally(members)
  return (
    [...healthLevels.map((l) => [t[l], levelLabel(l, rules).toLowerCase()] as const), [t.none, "not scored"] as const]
      .filter(([n]) => n > 0)
      .map(([n, l]) => `${count(n)} ${l}`)
      .join(", ") || "no repositories"
  )
}

/** tally counts the members at each activity the server gave them. */
function tally(members: Member[]): Record<ActivityLevel, number> {
  const t: Record<ActivityLevel, number> = { active: 0, recent: 0, quiet: 0, dormant: 0, archived: 0 }
  for (const m of members) t[m.activity]++
  return t
}

/** sentence is a rule's words as a sentence: "Changed in the last 30 days". */
const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

/** plural is a count of repositories in words. */
function plural(n: number): string {
  return `${count(n)} ${n === 1 ? "repository" : "repositories"}`
}

function summary(t: Record<ActivityLevel, number>, rules: Rules): string {
  return (
    rules.activity
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
export function FleetMap({
  groups,
  work = every,
  now,
  color = "activity",
  dense = false,
}: {
  groups: Group[]
  work?: Work
  now: number
  color?: Color
  /** one strip of tiles without the products' names, for the presentation page (by health only) */
  dense?: boolean
}) {
  const rules = useRules()
  const all = new Map<string, Member>()
  for (const g of groups) for (const m of g.members) all.set(m.address, m)
  if (color === "health") return <HealthMap groups={groups} members={[...all.values()]} work={work} now={now} dense={dense} />
  const total = tally([...all.values()])
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
              {rules.activity.map((a) => (
                <li key={a.level} className={`lvl-${a.level} flex items-center gap-1.5`}>
                  <span aria-hidden="true" className="cell" />
                  <span>
                    <span className="font-medium">{a.label}</span>: {a.about}
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
          {rules.activity.map((a) => (
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
                  {span(a.level, rules) && <span>{span(a.level, rules)}</span>}
                  <span className="font-mono text-foreground">{count(total[a.level])}</span>
                </li>
              )}
            >
              {a.level === "archived" ? sentence(a.about) : `${sentence(a.about)}${qualifier(work)}`}
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
            <p className="sr-only">{summary(tally(g.members), rules)}</p>
            <div aria-hidden="true" className="mt-1 flex flex-wrap gap-0.5">
              {g.members.map((m) => {
                const level = m.activity
                return (
                  <Hint
                    key={m.address}
                    heading={m.name}
                    rows={[
                      {
                        swatch: `lvl-${level}`,
                        label: activityRule(level, rules).label,
                        value: m.lastAuthoredAt ? `${ago(m.lastAuthoredAt, now)} ago` : "none read",
                      },
                      ...(m.level
                        ? [{ swatch: levels[m.level].className, label: "Health", value: levelLabel(m.level, rules) }]
                        : []),
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

/**
 * HealthMap is the fleet map coloured by health: every repository one tile by product, attention outlined and watch round besides
 * their colours, so the fleet's health reads at a glance and from across a room. The legend counts each level, once per repository;
 * in a narrow container (the presentation page with a wide commit column) only the legend stays, the tiles' room going to the products.
 */
function HealthMap({
  groups,
  members,
  work,
  now,
  dense,
}: {
  groups: Group[]
  members: Member[]
  work: Work
  now: number
  dense: boolean
}) {
  const rules = useRules()
  const total = healthTally(members)
  const tile = (m: Member) => (
    <Hint
      key={m.address}
      heading={m.name}
      rows={[
        ...(m.level
          ? [{ swatch: levels[m.level].className, label: "Health", value: levelLabel(m.level, rules) }]
          : [{ swatch: "lvl-dormant", label: "Health", value: "not scored" }]),
        {
          swatch: `lvl-${m.activity}`,
          label: `Last change${qualifier(work)}`,
          value: m.lastAuthoredAt ? `${ago(m.lastAuthoredAt, now)} ago` : "none read",
        },
      ]}
      render={(t) => <Link {...t} href={addressPath(m.address)} tabIndex={-1} className={`cell ${healthCell(m.level)}`} />}
    />
  )
  return (
    <section aria-labelledby="fleet" className="panel relative z-10 flex flex-col gap-2 p-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 id="fleet" className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
          Fleet health
        </h2>
        <ul className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted-foreground" aria-label="Health of every repository">
          {healthLevels.map((l) => (
            <li key={l} className={`${levels[l].className} flex items-center gap-1.5`}>
              <span aria-hidden="true" className={`cell ${healthCell(l)}`} />
              {levelLabel(l, rules)}
              <span className="font-mono text-foreground tabular-nums">{count(total[l])}</span>
            </li>
          ))}
        </ul>
      </div>
      {dense ? (
        // One strip of clusters: each product its tiles in a hairline box under its name (cut to the tiles' width, whole in its
        // hint), so the fleet reads as its products at a glance.
        <>
          <ul className="sr-only">
            {groups.map((g) => (
              <li key={g.id}>
                {g.name}: {healthSummary(g.members, rules)}
              </li>
            ))}
          </ul>
          <div aria-hidden="true" className="flex flex-wrap gap-1.5">
            {groups.map((g) => (
              <span key={g.id} className="flex flex-col gap-0.5 rounded-sm border border-border/70 px-1 pt-0.5 pb-1">
                <Hint
                  heading={g.name}
                  meta={`${count(g.members.length)} ${g.members.length === 1 ? "repository" : "repositories"}`}
                  rows={healthLevels
                    .map((l) => ({ l, n: g.members.filter((m) => m.level === l).length }))
                    .filter(({ n }) => n > 0)
                    .map(({ l, n }) => ({ swatch: levels[l].className, label: levelLabel(l, rules), value: count(n) }))}
                  render={(t) => (
                    <span
                      {...t}
                      className="w-0 min-w-full cursor-help truncate text-[0.625rem] leading-none text-muted-foreground"
                    >
                      {g.name}
                    </span>
                  )}
                />
                <span className="flex flex-wrap gap-1">{g.members.map(tile)}</span>
              </span>
            ))}
          </div>
        </>
      ) : (
        <ul className="flex flex-wrap gap-x-5 gap-y-2 @max-lg:hidden">
          {groups.map((g) => (
            <li key={g.id} className="min-w-0 max-w-full">
              <p className="flex items-baseline justify-between gap-2 text-xs">
                <span className="truncate font-medium">{g.name}</span>
                <span className="font-mono text-muted-foreground tabular-nums">{count(g.members.length)}</span>
              </p>
              <p className="sr-only">{healthSummary(g.members, rules)}</p>
              <div aria-hidden="true" className="mt-1 flex max-w-[20rem] flex-wrap gap-1">
                {g.members.map(tile)}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
