"use client"

import type { ReactNode } from "react"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { count, plural } from "@/lib/format"
import { by, has, kinds, type WorkKind } from "@/lib/preferences"
import { activityRule, kindOf, levelLabel, window, type Rules } from "@/lib/rules"
import { useRules } from "@/lib/rules-context"
import { useNow } from "@/lib/clock"
import Link from "next/link"
import { CloudCheckIcon, CloudOffIcon, CommitIcon, FlagIcon, GridIcon, PullIcon, PulseIcon } from "@/components/ui/icons"
import { levels } from "@/entities/health/health"
import { Hint, type HintRow } from "@/shared/hint"
import { assistants } from "./work"

/** lower starts a label in lower case for the middle of a phrase: "With AI" is "with AI". */
const lower = (s: string) => s.charAt(0).toLowerCase() + s.slice(1)

function percent(part: number, whole: number): string {
  return whole === 0 ? "–" : `${Math.round((part / whole) * 100)}%`
}

/** Signal is one indicator of the fleet, the same on the dashboard (a tile) and on the presentation page (an icon in the strip). */
export type Signal = {
  key: WorkKind | "active" | "findings" | "pulls" | "products" | "sync"
  /** what it counts, in words: "Commits by hand" */
  label: string
  value: string
  detail: string
  icon: ReactNode
  /** its parts, for the hint: at most three rows */
  rows: HintRow[]
  /** a level's colour class, which paints the icon */
  level?: string
  /** a kind of work's colour, as the sparklines draw it */
  tone?: string
  /** where a narrowing strip drops it (container queries); none: it always stays */
  drop?: string
  /** the page that lists what it counts */
  href?: string
}

/** tones are the kinds of work's colours, as the sparklines and the cards draw them (no inline style: the CSP forbids it). */
const tones: Record<WorkKind, string> = { hand: "text-primary", ai: "text-[var(--ai)]", automation: "text-muted-foreground" }
const dots: Record<WorkKind, string> = { hand: "bg-primary", ai: "bg-[var(--ai)]", automation: "bg-muted-foreground" }
/** commitsDrop is where a narrowing strip drops the commits: the three kinds together, the last of what goes. */
const commitsDrop = "@max-xl:hidden"

/**
 * signals are the fleet's indicators in their order: the commits of each kind of work (by hand, with AI or by an agent, by
 * automation; each says whether Settings count it), the repositories the work that counts changed, what needs a look, the
 * products by their health, and whether the catalogue is current. The window and the names of the levels come from the server's rules, so they say what it does today.
 */
export function signals(data: Schemas["Dashboard"], rules: Rules, now = Date.now()): Signal[] {
  const a = data.activity
  const f = data.freshness
  const checked = f.fresh + f.stale + f.error + f.unknown
  const h = data.health
  const days = window(rules)
  const members = new Map<string, Schemas["ActivityLevel"]>()
  for (const g of data.groups) for (const m of g.members) members.set(m.address, m.activity)
  const at = (l: Schemas["ActivityLevel"]) => [...members.values()].filter((v) => v === l).length
  const products = (l: Schemas["HealthLevel"]) => data.groups.filter((g) => g.health?.level === l).length
  const ofProducts = plural(data.groups.length, "product", "products")
  const commits = (k: WorkKind, n: number, detail: string, rows: HintRow[]): Signal => ({
    key: k,
    label: `Commits ${lower(kinds.find((x) => x.value === k)?.label ?? k)}`,
    value: count(n),
    detail,
    icon: <CommitIcon kind={k} />,
    rows: [...rows, { label: "In the work that counts", value: has(data.work, k) ? "yes" : "no, see Settings" }],
    tone: tones[k],
    drop: commitsDrop,
  })
  return [
    commits("hand", a.hand, `in the last ${days}, by ${plural(a.authorCount, "person", "people")}`, [
      { swatch: dots.hand, label: "People", value: count(a.authorCount) },
    ]),
    commits("ai", a.ai, `in the last ${days}, with AI or by an agent${assistants(a)}`, [
      { swatch: dots.ai, label: "Assistants", value: a.assistants.join(", ") || "none named" },
    ]),
    commits(
      "automation",
      a.automation + a.sweeps,
      `in the last ${days}, by CI, dependency and release bots` +
        (a.sweeps > 0 ? `, ${plural(a.sweeps, "commit", "commits")} of them in sweeps` : ""),
      [
        { swatch: dots.automation, label: "By bots", value: count(a.automation) },
        { swatch: dots.automation, label: "In sweeps", value: count(a.sweeps) },
      ],
    ),
    {
      key: "active",
      label: "Active repositories",
      value: count(at("active")),
      detail: `of ${plural(members.size, "repository", "repositories")}, changed ${by(data.work)} in the last ${days}`,
      icon: <PulseIcon />,
      rows: (["active", "recent", "quiet"] as const).map((l) => ({
        swatch: `lvl-${l}`,
        label: activityRule(l, rules).label,
        value: count(at(l)),
      })),
      level: "lvl-active",
      drop: "@max-3xl:hidden",
    },
    {
      key: "findings",
      label: "Findings",
      value: h ? count(h.findingCount) : "–",
      detail:
        products("attention") > 0
          ? `${count(products("attention"))} of ${ofProducts} need attention`
          : products("watch") > 0
            ? `${count(products("watch"))} of ${ofProducts} to watch`
            : `none of ${ofProducts} needs attention`,
      icon: <FlagIcon />,
      // The most costly first, as the dashboard weighs them: each its kind and repository.
      rows: (h?.findings ?? []).slice(0, 3).map((x) => ({
        swatch: levels[h?.level ?? "good"].className,
        label: `${kindOf(x.code, rules).label}: ${x.repo.split("/")[1] ?? x.repo}`,
        value: `−${x.penalty}`,
      })),
      level: h ? levels[h.level].className : undefined,
    },
    ...(rules.features.pulls ? [pullsSignal(data, now)] : []),
    {
      key: "products",
      label: "Products",
      value: count(data.groups.length),
      detail:
        `holding ${plural(members.size, "repository", "repositories")}` +
        (data.ungroupedCount > 0 ? `; ${count(data.ungroupedCount)} more in none` : ""),
      icon: <GridIcon />,
      rows: (["attention", "watch", "good"] as const).map((l) => ({
        swatch: levels[l].className,
        label: levelLabel(l, rules),
        value: count(products(l)),
      })),
      tone: "text-primary",
      drop: "@max-2xl:hidden",
    },
    {
      key: "sync",
      label: "In sync",
      value: percent(f.fresh, checked),
      detail: f.error > 0 ? `${count(f.error)} failing to update from GitHub` : "the catalogue is current with GitHub",
      icon: f.error > 0 ? <CloudOffIcon /> : <CloudCheckIcon />,
      rows: [
        { swatch: "lvl-active", label: "Current", value: count(f.fresh) },
        { swatch: "lvl-quiet", label: "Due for a check", value: count(f.stale + f.unknown) },
        { swatch: "lvl-danger", label: "Failing", value: count(f.error) },
      ],
      level: f.error > 0 ? "lvl-danger" : "lvl-active",
    },
  ]
}

/**
 * pullsSignal is the open pull requests of the work that counts (automation's only when it counts): how many, how many wait for a
 * review and how long the oldest has been idle, and in its parts the ones ready to merge and the authors'. The health findings tell
 * when one waits too long (reviews).
 */
function pullsSignal(data: Schemas["Dashboard"], now: number): Signal {
  const p = data.pulls
  const st = p.states
  return {
    key: "pulls",
    label: "Pull requests",
    value: count(p.open),
    detail:
      st.review > 0
        ? `open, ${count(st.review)} waiting for a review${p.oldestReview ? `, the oldest idle ${idle(p.oldestReview, now)}` : ""}`
        : p.open > 0
          ? "open, none waiting for a review"
          : "none open",
    icon: <PullIcon />,
    rows: [
      { swatch: "bg-primary", label: "Need review", value: count(st.review) },
      { swatch: "bg-[var(--active)]", label: "Ready to merge", value: count(st.ready) },
      {
        swatch: "bg-[var(--quiet)]",
        label: "With their authors",
        value: count(st.changes + st.failing + st.conflicts + st.draft),
      },
    ],
    tone: "text-primary",
    drop: "@max-2xl:hidden",
    href: "/pulls",
  }
}

/** idle is how long ago a time was, in whole days. */
function idle(at: string, now: number): string {
  const days = Math.floor((now - Date.parse(at)) / 86_400_000)
  return days < 1 ? "less than a day" : plural(days, "day", "days")
}

/** SignalHint is a signal's depth the moment it is pointed at, focused or tapped: what it counts and its parts. */
function SignalHint({ s, render }: { s: Signal; render: Parameters<typeof Hint>[0]["render"] }) {
  return (
    <Hint heading={s.label} meta={s.value} swatch={s.level} rows={s.rows} render={render}>
      {s.detail}
    </Hint>
  )
}

/** iconClass paints a signal's icon: its level's colour, or its kind of work's. */
const iconClass = (s: Signal) => (s.level ? "text-[var(--lvl)]" : s.tone)

/**
 * Telemetry is the fleet at a glance, in signals: the commits of each kind of work, the repositories they changed, what needs a
 * look, and whether the catalogue is current; each a tile with what it counts, its parts in its hint.
 */
export function Telemetry({ data }: { data: Schemas["Dashboard"] }) {
  const rules = useRules()
  const now = useNow()
  return (
    <section aria-label="Telemetry">
      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4 xl:grid-cols-8">
        {signals(data, rules, now).map((s) => (
          <SignalHint
            key={s.key}
            s={s}
            render={(t) => (
              <div
                {...t}
                tabIndex={0}
                className={cn(
                  "panel flex cursor-help flex-col gap-1 p-3 outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  s.level,
                )}
              >
                <dt className="text-[0.6875rem] font-medium tracking-widest text-muted-foreground uppercase">
                  {s.href ? (
                    <Link href={s.href} className="hover:text-foreground hover:underline">
                      {s.label}
                    </Link>
                  ) : (
                    s.label
                  )}
                </dt>
                <dd className="flex items-center gap-2 font-mono text-2xl font-semibold tabular-nums sm:text-3xl">
                  <span className={cn("[&_svg]:size-6", iconClass(s))}>{s.icon}</span>
                  {s.value}
                </dd>
                <dd className="text-xs text-muted-foreground">{s.detail}</dd>
              </div>
            )}
          />
        ))}
      </dl>
    </section>
  )
}

/**
 * TelemetryStrip is the same signals in one line, for the presentation page: each its icon and value, what it counts and its parts
 * in its hint, so the line stays short. start opens the line (the brand), end closes it (the data's age). As its container narrows
 * it keeps what matters most: the active repositories go first, then the products, then the commits (the three kinds together,
 * never one without the others); the findings and the sync always stay.
 */
export function TelemetryStrip({ data, start, end }: { data: Schemas["Dashboard"]; start?: ReactNode; end?: ReactNode }) {
  const rules = useRules()
  const now = useNow()
  return (
    <section aria-label="Telemetry" className="panel flex items-center gap-x-4 px-3 py-1.5">
      {start}
      <dl className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3.5 gap-y-1">
        {signals(data, rules, now).map((s) => (
          <SignalHint
            key={s.key}
            s={s}
            render={(t) => (
              <div
                {...t}
                tabIndex={0}
                className={cn(
                  "flex cursor-help items-center gap-1 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  s.level,
                  s.drop,
                )}
              >
                <dt className="sr-only">{s.label}</dt>
                <dd className="flex items-center gap-1">
                  <span className={cn("[&_svg]:size-4", iconClass(s))}>{s.icon}</span>
                  <span className="font-mono text-lg font-semibold tabular-nums">{s.value}</span>
                </dd>
              </div>
            )}
          />
        ))}
      </dl>
      {end}
    </section>
  )
}
