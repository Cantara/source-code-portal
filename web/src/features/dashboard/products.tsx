"use client"

import Link from "next/link"
import { useState, type ReactNode } from "react"
import type { Schemas } from "@/lib/api/client"
import { cn } from "@/lib/cn"
import { ago, count, plural } from "@/lib/format"
import { addressPath, groupPath } from "@/lib/paths"
import { Author } from "@/entities/commit/author"
import { FindingList, HealthScore, levels } from "@/entities/health/health"
import { EmptyState } from "@/shared/empty-state"
import { RelativeTime } from "@/shared/relative-time"
import { Sparkline } from "@/shared/sparkline"
import { by, remember, viewCookie, type Work } from "@/lib/preferences"
import { assistants, counted, noun, series, share, weekly } from "./work"
import type { ProductsView } from "./layout"

type Group = Schemas["DashboardGroup"]

/** cardFindings is how many findings a card shows, the most costly first; the product's page has the rest. */
const cardFindings = 3

/** names is "ada, bo and 3 others" for the people of an activity, each by login (the full name on hover). */
function names(a: Schemas["Activity"]): ReactNode {
  const more = a.authorCount - a.authors.length
  const people = a.authors.map((p, i) => <Author key={i} name={p.name} login={p.login} />)
  const parts: ReactNode[] = more > 0 ? [...people, plural(more, "other", "others")] : people
  return parts.map((p, i) => (
    <span key={i}>
      {i > 0 && (i === parts.length - 1 ? " and " : ", ")}
      {p}
    </span>
  ))
}

/** Trend is an activity's sparkline through the lens. */
function Trend({ a, w, className }: { a: Schemas["Activity"]; w: Work; className?: string }) {
  const s = series(a, w)
  return <Sparkline values={s.values} part={s.part} label={weekly(a, w)} className={cn(s.ai && "text-[var(--ai)]", className)} />
}

/** AiDot marks the commits with AI in text, in the colour the sparklines draw them. */
const AiDot = () => <span aria-hidden="true" className="mr-1 inline-block size-2 rounded-xs bg-[var(--ai)]" />

/** Shared is what most members have in common, said once: the licence, the language and how many are released. */
function Shared({ group }: { group: Group }) {
  const s = group.summary
  const facts = [s.license, s.language, `${count(s.released)} of ${count(group.members.length)} released`].filter(Boolean)
  return <p className="text-xs text-muted-foreground">{facts.join(" · ")}</p>
}

/**
 * ProductCard is one product distilled to what an engineer acts on, in the same five rows on every card: the product and its
 * level, people's work against automation's, who did it, what they changed last, and the exceptions. The members are on the
 * product's page.
 */
function ProductCard({ group, work: w, now }: { group: Group; work: Work; now: number }) {
  const a = group.activity
  const h = group.health
  const findings = h?.findings.slice(0, cardFindings) ?? []
  const more = h ? h.findingCount - findings.length : 0
  return (
    <li className="panel panel-live flex min-w-0 flex-col gap-3 p-4">
      <div className="flex items-start justify-between gap-3">
        <h3 className="text-base leading-tight font-semibold">
          <Link href={groupPath(group.id)} className="hover:text-primary hover:underline">
            {group.name}
          </Link>
        </h3>
        {h && h.level !== "good" ? (
          <HealthScore health={h} />
        ) : (
          h && (
            <span className="font-mono text-xs text-muted-foreground" title="Health score: good">
              <span className="sr-only">Health </span>
              {h.score}
            </span>
          )
        )}
      </div>
      <div className="flex items-end justify-between gap-3">
        <p className="text-sm">
          <span className="font-mono text-lg font-semibold tabular-nums">{count(counted(a, w))}</span> {noun(counted(a, w), w)}
          {w === "ai" && assistants(a)}
          {w === "all" && (
            <span className="block text-xs text-muted-foreground">
              {count(a.hand)} by hand · {a.ai > 0 && <AiDot />}
              {count(a.ai)} with AI{assistants(a)}
            </span>
          )}
          <span className="block text-xs text-muted-foreground">
            {w === "hand" && (
              <>
                {a.ai > 0 && <AiDot />}
                {count(a.ai)} with AI ·{" "}
              </>
            )}
            {w === "ai" && <>{count(a.hand)} by hand · </>}
            {count(a.automation)} by automation{a.sweeps > 0 && `, ${count(a.sweeps)} in sweeps`} · 30 days
          </span>
        </p>
        <Trend a={a} w={w} />
      </div>
      <p className="text-sm">
        {a.authorCount > 0 ? (
          <span className={cn(levels.good.className, "inline-flex items-center gap-1.5")}>
            <span aria-hidden="true" className="led" />
            <span className="text-foreground">{names(a)}</span>
          </span>
        ) : (
          <span className="text-muted-foreground">No commit {by[w]} in 30 days</span>
        )}
        <span className="block text-xs text-muted-foreground">
          {a.lastAuthoredAt ? (
            <>
              Last change {by[w]} <RelativeTime at={a.lastAuthoredAt} />
            </>
          ) : (
            `No change ${by[w]} read`
          )}
        </span>
      </p>
      {a.recent.length > 0 && (
        <ul aria-label={`Changed last ${by[w]}`} className="flex flex-wrap gap-x-3 gap-y-1 text-xs">
          {a.recent.map((r) => (
            <li key={r.repo} className="flex min-w-0 items-baseline gap-1">
              <Link href={addressPath(r.repo)} className="truncate font-medium hover:underline">
                {r.repo.split("/")[1]}
              </Link>
              <time dateTime={r.at} className="font-mono text-muted-foreground" suppressHydrationWarning>
                {ago(r.at, now)}
              </time>
            </li>
          ))}
        </ul>
      )}
      <div className="mt-auto flex flex-col gap-1 border-t border-border/60 pt-2">
        {findings.length > 0 ? (
          <FindingList findings={findings} className="text-xs" />
        ) : (
          <p className="text-xs text-muted-foreground">No exceptions.</p>
        )}
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <Shared group={group} />
          <Link href={groupPath(group.id)} className="text-xs whitespace-nowrap text-muted-foreground hover:underline">
            {more > 0 ? `${plural(more, "more finding", "more findings")} · ` : ""}
            {plural(group.repoCount, "repository", "repositories")}
            <span className="sr-only"> of {group.name}</span>
          </Link>
        </div>
      </div>
    </li>
  )
}

type Column = {
  key: string
  label: string
  numeric?: boolean
  value: (g: Group) => number | string
  cell: (g: Group) => ReactNode
}

const columns = (w: Work): Column[] => [
  {
    key: "name",
    label: "Product",
    value: (g) => g.name.toLowerCase(),
    cell: (g) => (
      <Link href={groupPath(g.id)} className="font-medium hover:text-primary hover:underline">
        {g.name}
      </Link>
    ),
  },
  { key: "repos", label: "Repositories", numeric: true, value: (g) => g.repoCount, cell: (g) => count(g.repoCount) },
  {
    key: "commits",
    label: w === "all" ? "Commits, 30 days" : `${w === "hand" ? "By hand" : "With AI"}, 30 days`,
    numeric: true,
    value: (g) => counted(g.activity, w),
    cell: (g) => (
      <span className="inline-flex items-center justify-end gap-2">
        {count(counted(g.activity, w))}
        <Trend a={g.activity} w={w} className="h-4 w-12" />
      </span>
    ),
  },
  {
    key: "ai",
    label: "Share with AI",
    numeric: true,
    value: (g) => share(g.activity),
    cell: (g) => (share(g.activity) < 0 ? "–" : `${share(g.activity)}%`),
  },
  {
    key: "automation",
    label: "By automation",
    numeric: true,
    value: (g) => g.activity.automation,
    cell: (g) => count(g.activity.automation),
  },
  {
    key: "authors",
    label: "People",
    numeric: true,
    value: (g) => g.activity.authorCount,
    cell: (g) => count(g.activity.authorCount),
  },
  {
    key: "last",
    label: `Last change ${by[w]}`,
    value: (g) => g.activity.lastAuthoredAt ?? "",
    cell: (g) => <RelativeTime at={g.activity.lastAuthoredAt} />,
  },
  {
    key: "findings",
    label: "Findings",
    numeric: true,
    value: (g) => g.health?.findingCount ?? 0,
    cell: (g) => count(g.health?.findingCount ?? 0),
  },
  {
    key: "health",
    label: "Health",
    numeric: true,
    value: (g) => g.health?.score ?? -1,
    cell: (g) => (g.health ? <HealthScore health={g.health} className="justify-end" /> : "–"),
  },
]

/** ProductTable is every product in one row, sortable by any column, so many products compare at once. */
function ProductTable({ groups, work }: { groups: Group[]; work: Work }) {
  const [sort, setSort] = useState<{ key: string; dir: 1 | -1 }>({ key: "", dir: 1 })
  const cols = columns(work)
  const col = cols.find((c) => c.key === sort.key)
  const rows = col
    ? [...groups].sort((a, b) => {
        const x = col.value(a)
        const y = col.value(b)
        return (x < y ? -1 : x > y ? 1 : 0) * sort.dir
      })
    : groups
  return (
    <div className="panel overflow-x-auto">
      <table className="w-full text-sm">
        <caption className="sr-only">Products, sortable by every column</caption>
        <thead>
          <tr className="border-b border-border text-xs text-muted-foreground">
            {cols.map((c) => (
              <th
                key={c.key}
                scope="col"
                aria-sort={sort.key === c.key ? (sort.dir === 1 ? "ascending" : "descending") : undefined}
                className={cn("px-3 py-2 font-medium", c.numeric ? "text-right" : "text-left")}
              >
                <button
                  type="button"
                  className="inline-flex items-center gap-1 hover:text-foreground"
                  onClick={() =>
                    setSort((s) =>
                      s.key === c.key ? { key: c.key, dir: s.dir === 1 ? -1 : 1 } : { key: c.key, dir: c.numeric ? -1 : 1 },
                    )
                  }
                >
                  {c.label}
                  <span aria-hidden="true">{sort.key === c.key ? (sort.dir === 1 ? "▲" : "▼") : ""}</span>
                </button>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((g) => (
            <tr key={g.id} className="border-b border-border/60 last:border-b-0">
              {cols.map((c, i) =>
                i === 0 ? (
                  <th key={c.key} scope="row" className="px-3 py-2 text-left font-normal">
                    {c.cell(g)}
                  </th>
                ) : (
                  <td key={c.key} className={cn("px-3 py-2 font-mono tabular-nums", c.numeric ? "text-right" : "text-left")}>
                    {c.cell(g)}
                  </td>
                ),
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/** Products are the groups as distilled cards or as one table, the reader's choice remembered in a cookie. */
export function Products({
  groups,
  repoCount,
  view: initial,
  work,
  now,
}: {
  groups: Group[]
  repoCount: number
  view: ProductsView
  work: Work
  now: number
}) {
  const [view, setView] = useState(initial)
  const choose = (v: ProductsView) => {
    setView(v)
    remember(viewCookie, v)
  }
  return (
    <section aria-labelledby="products" className="min-w-0">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h2 id="products" className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
          Products
        </h2>
        <div role="group" aria-label="Show products as" className="inline-flex rounded-md border border-border p-0.5 text-xs">
          {(["cards", "table"] as const).map((v) => (
            <button
              key={v}
              type="button"
              aria-pressed={view === v}
              onClick={() => choose(v)}
              className={cn(
                "rounded px-2 py-1 capitalize",
                view === v ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {v}
            </button>
          ))}
        </div>
      </div>
      {groups.length === 0 ? (
        <EmptyState title="No groups configured">{plural(repoCount, "repository", "repositories")} in the catalogue.</EmptyState>
      ) : view === "table" ? (
        <ProductTable groups={groups} work={work} />
      ) : (
        <ul className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,20rem),1fr))] gap-4">
          {groups.map((g) => (
            <ProductCard key={g.id} group={g} work={work} now={now} />
          ))}
        </ul>
      )}
    </section>
  )
}
