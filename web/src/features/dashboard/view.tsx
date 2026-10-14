"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { useMemo } from "react"
import { Card, CardTitle } from "@/components/ui/card"
import { api, type Repo, type Schemas } from "@/lib/api/client"
import { useNow } from "@/lib/clock"
import { cn } from "@/lib/cn"
import { count, plural } from "@/lib/format"
import { byActivity, pulse, type Pulse } from "@/entities/repo/health"
import { groupPath, repoPath } from "@/lib/paths"
import { CommitList } from "@/entities/commit/commit-list"
import { EmptyState } from "@/shared/empty-state"
import { ActivityLegend, ActivityLight, HealthBadges, HealthStrip } from "@/entities/repo/health-badges"
import { PageHeader } from "@/shared/page-header"
import { PanelBoundary } from "@/shared/panel-boundary"
import { ProblemAlert } from "@/shared/problem-alert"
import { RelativeTime } from "@/shared/relative-time"
import { reposQuery } from "@/lib/api/queries"
import { dashboardQuery } from "./api"

type Group = Schemas["DashboardGroup"]

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

function percent(part: number, whole: number): string {
  return whole === 0 ? "–" : `${Math.round((part / whole) * 100)}%`
}

/** Telemetry is the fleet at a glance: counts, activity and how much of the catalogue is current. */
function Telemetry({ p, data }: { p: Pulse; data: Schemas["Dashboard"] }) {
  const f = data.freshness
  const checked = f.fresh + f.stale + f.error + f.unknown
  return (
    <section aria-label="Telemetry" className="flex flex-col gap-3">
      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        <Tile label="Repositories" value={count(p.repos)} detail={plural(data.groups.length, "product", "products")} />
        <Tile label="Active" value={count(p.active)} detail="a commit in the last 30 days" level="lvl-active" />
        <Tile label="Released" value={percent(p.released, p.repos)} detail={`${count(p.released)} with a release`} />
        <Tile label="Licensed" value={percent(p.licensed, p.repos)} detail={`${count(p.licensed)} with a licence`} />
        <Tile label="Open issues" value={count(p.issues)} detail="issues and pull requests" />
        <Tile
          label="In sync"
          value={percent(f.fresh, checked)}
          detail={f.error > 0 ? `${count(f.error)} failing to update` : "the catalogue is current"}
          level={f.error > 0 ? "lvl-danger" : "lvl-active"}
        />
      </dl>
      <ActivityLegend pulse={p} />
    </section>
  )
}

/** RepoRow is one repository inside a product card: activity light, name, description and its health badges. */
function RepoRow({ repo, now }: { repo: Repo; now: number }) {
  return (
    <li className="grid break-inside-avoid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-2 border-b border-border/60 py-2 last:border-b-0">
      <ActivityLight repo={repo} now={now} decorative />
      <div className="min-w-0">
        <Link href={repoPath(repo.owner, repo.name)} className="block truncate font-medium hover:text-primary hover:underline">
          {repo.name}
        </Link>
        {repo.description && (
          <p className="truncate text-xs text-muted-foreground" title={repo.description}>
            {repo.description}
          </p>
        )}
        <HealthBadges repo={repo} now={now} className="mt-1" />
      </div>
    </li>
  )
}

/** ProductCard is one group with all its repositories, newest change first; large groups span more columns. */
function ProductCard({ group, repos, wide, now }: { group: Group; repos: Repo[]; wide: boolean; now: number }) {
  const p = useMemo(() => pulse(repos, now), [repos, now])
  const name = group.id ? (
    <Link href={groupPath(group.id)} className="hover:text-primary hover:underline">
      {group.name}
    </Link>
  ) : (
    group.name
  )
  return (
    <li
      className={cn("panel panel-live flex min-w-0 flex-col p-4", wide ? "col-span-full" : repos.length > 14 && "md:col-span-2")}
    >
      <div className="flex items-start justify-between gap-3">
        <h3 className="text-base leading-tight font-semibold">{name}</h3>
        <span className="font-mono text-xs whitespace-nowrap text-muted-foreground">
          <span className="text-base font-semibold text-foreground tabular-nums">{count(group.repoCount)}</span> repos
        </span>
      </div>
      {group.description && <p className="mt-1 text-sm text-muted-foreground">{group.description}</p>}
      <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
        <HealthStrip repos={repos} pulse={p} now={now} />
        <span className="text-xs text-muted-foreground">
          {count(p.active)} active
          {group.lastActivity && (
            <>
              {" "}
              · last commit <RelativeTime at={group.lastActivity} />
            </>
          )}
        </span>
      </div>
      {repos.length === 0 ? (
        <p className="mt-3 text-sm text-muted-foreground">No repositories match this group.</p>
      ) : (
        <ul className={cn("mt-2", repos.length > 14 && "md:columns-2 md:gap-6", wide && "xl:columns-3 2xl:columns-4")}>
          {repos.map((r) => (
            <RepoRow key={r.address} repo={r} now={now} />
          ))}
        </ul>
      )}
    </li>
  )
}

/** DashboardView is mission control: telemetry, every product with its repositories and their health, and the latest commits. */
export function DashboardView() {
  const { data, error } = useQuery(dashboardQuery(api))
  const { data: list } = useQuery(reposQuery(api))
  const now = useNow()
  const repos = useMemo(() => [...(list?.repos ?? [])].sort(byActivity), [list])
  const p = useMemo(() => pulse(repos, now), [repos, now])
  const members = useMemo(() => {
    const m = new Map<string, Repo[]>()
    for (const r of repos)
      for (const g of r.groups) {
        const list = m.get(g)
        if (list) list.push(r)
        else m.set(g, [r])
      }
    return m
  }, [repos])
  const ungrouped = useMemo(() => repos.filter((r) => r.groups.length === 0), [repos])
  return (
    <>
      <PageHeader
        title="Dashboard"
        meta={
          data && (
            <span className="font-mono text-xs text-muted-foreground">
              {plural(data.groups.length, "product", "products")} · {plural(data.repoCount, "repository", "repositories")}
            </span>
          )
        }
      />
      {error && !data && <ProblemAlert error={error} />}
      {data && (
        <div className="flex flex-col gap-6">
          {list && <Telemetry p={p} data={data} />}
          <div className="grid gap-6 2xl:grid-cols-[minmax(0,1fr)_24rem]">
            <section aria-labelledby="products" className="min-w-0">
              <h2 id="products" className="mb-3 text-xs font-medium tracking-widest text-muted-foreground uppercase">
                Products
              </h2>
              {data.groups.length === 0 && ungrouped.length === 0 ? (
                <EmptyState title="No groups configured">
                  {plural(data.repoCount, "repository", "repositories")} in the catalogue.
                </EmptyState>
              ) : (
                <ul className="grid grid-flow-dense grid-cols-[repeat(auto-fill,minmax(min(100%,22rem),1fr))] gap-4">
                  {data.groups.map((g) => (
                    <ProductCard
                      key={g.id}
                      group={g}
                      repos={members.get(g.id) ?? []}
                      wide={g.repoCount > 40 || (g.repoCount === data.repoCount && data.groups.length > 1)}
                      now={now}
                    />
                  ))}
                  {ungrouped.length > 0 && (
                    <ProductCard
                      group={{
                        id: "",
                        name: "In no group",
                        description: "Repositories no group selects.",
                        defaultRepo: null,
                        repoCount: ungrouped.length,
                        lastActivity: null,
                      }}
                      repos={ungrouped}
                      wide={ungrouped.length > 40}
                      now={now}
                    />
                  )}
                </ul>
              )}
            </section>
            <PanelBoundary>
              <Card aria-labelledby="latest" className="self-start 2xl:sticky 2xl:top-20">
                <div className="flex items-baseline justify-between">
                  <CardTitle id="latest">Latest commits</CardTitle>
                  <Link href="/commits" className="text-sm hover:text-primary hover:underline">
                    All commits
                  </Link>
                </div>
                <CommitList commits={data.latest} />
              </Card>
            </PanelBoundary>
          </div>
        </div>
      )}
    </>
  )
}
