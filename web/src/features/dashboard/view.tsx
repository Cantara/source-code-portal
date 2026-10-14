"use client"

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { Card, CardTitle } from "@/components/ui/card"
import { api, type Schemas } from "@/lib/api/client"
import { count, plural } from "@/lib/format"
import { groupPath } from "@/lib/paths"
import { CommitList } from "@/shared/commit-list"
import { EmptyState } from "@/shared/empty-state"
import { PageHeader } from "@/shared/page-header"
import { PanelBoundary } from "@/shared/panel-boundary"
import { ProblemAlert } from "@/shared/problem-alert"
import { GroupBadge, RepoBadges } from "@/shared/repo-badges"
import { RelativeTime } from "@/shared/relative-time"
import { dashboardQuery } from "./api"

function Health({ counts }: { counts: Schemas["FreshnessCounts"] }) {
  const parts = [
    [counts.fresh, "up to date"],
    [counts.stale, "delayed"],
    [counts.error, "failing to update"],
    [counts.unknown, "not checked yet"],
  ] as const
  return (
    <p className="text-sm text-muted-foreground">
      {parts
        .filter(([n]) => n > 0)
        .map(([n, label]) => `${count(n)} ${label}`)
        .join(" · ") || "No repositories yet"}
    </p>
  )
}

/** DashboardView is the dashboard: groups, health and the latest commits. */
export function DashboardView() {
  const { data, error } = useQuery(dashboardQuery(api))
  return (
    <>
      <PageHeader title="Dashboard">{data && <Health counts={data.freshness} />}</PageHeader>
      {error && !data && <ProblemAlert error={error} />}
      {data && (
        <div className="grid gap-6 lg:grid-cols-3">
          <section aria-labelledby="groups" className="lg:col-span-2">
            <h2 id="groups" className="mb-3 text-lg font-semibold">
              Groups
            </h2>
            {data.groups.length === 0 ? (
              <EmptyState title="No groups configured">
                {plural(data.repoCount, "repository", "repositories")} in the catalogue.
              </EmptyState>
            ) : (
              <ul className="grid gap-3 sm:grid-cols-2">
                {data.groups.map((g) => (
                  <li key={g.id}>
                    <Card className="h-full">
                      <div className="flex items-start justify-between gap-2">
                        <h3 className="font-semibold">
                          <Link href={groupPath(g.id)} className="hover:underline">
                            {g.name}
                          </Link>
                        </h3>
                        <GroupBadge group={g.id} />
                      </div>
                      {g.description && <p className="mt-1 text-sm text-muted-foreground">{g.description}</p>}
                      {g.defaultRepo && <RepoBadges address={g.defaultRepo} className="mt-3" />}
                      <p className="mt-2 text-xs text-muted-foreground">
                        Last activity <RelativeTime at={g.lastActivity} />
                      </p>
                    </Card>
                  </li>
                ))}
              </ul>
            )}
            {data.ungroupedCount > 0 && (
              <p className="mt-3 text-sm text-muted-foreground">
                {plural(data.ungroupedCount, "repository is", "repositories are")} in no group.
              </p>
            )}
          </section>
          <PanelBoundary>
            <Card aria-labelledby="latest">
              <div className="flex items-baseline justify-between">
                <CardTitle id="latest">Latest commits</CardTitle>
                <Link href="/commits" className="text-sm hover:underline">
                  All commits
                </Link>
              </div>
              <CommitList commits={data.latest} />
            </Card>
          </PanelBoundary>
        </div>
      )}
    </>
  )
}
