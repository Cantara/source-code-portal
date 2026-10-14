import { useSuspenseQuery } from "@tanstack/react-query"
import { createFileRoute, Link } from "@tanstack/react-router"
import { Trans } from "@lingui/react/macro"
import { t } from "@lingui/core/macro"
import { groupsLoader, groupsQuery } from "./api"
import { dashboardSearch } from "./search"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { FreshnessBadge } from "@/shared/freshness-badge"
import { PanelBoundary } from "@/shared/panel-boundary"

function Groups() {
  const { data } = useSuspenseQuery(groupsQuery())
  const { q } = Route.useSearch()
  const items = q ? data.items.filter((g) => g.displayName.toLowerCase().includes(q.toLowerCase())) : data.items
  return (
    <>
      <FreshnessBadge freshness={data.freshness} />
      <ul className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {items.map((g) => (
          <li key={g.id}>
            <Card>
              <CardHeader>
                <CardTitle>
                  <Link to="/groups/$groupId" params={{ groupId: g.id }}>
                    {g.displayName}
                  </Link>
                </CardTitle>
              </CardHeader>
              <CardContent>
                <p>{g.description}</p>
                <FreshnessBadge freshness={g.freshness} />
                <p className="text-sm text-muted-foreground">{t`${g.repoCount} repositories`}</p>
                <div className="flex flex-wrap gap-1">
                  {g.badges.map((b) => (
                    <img key={b.kind} src={b.url} alt={b.label} height={20} />
                  ))}
                </div>
              </CardContent>
            </Card>
          </li>
        ))}
      </ul>
    </>
  )
}

export const Route = createFileRoute("/dashboard")({
  validateSearch: dashboardSearch,
  loader: ({ context }) => groupsLoader(context.queryClient),
  head: () => ({ meta: [{ title: "Dashboard" }] }),
  pendingComponent: () => <Skeleton className="h-40 w-full" />,
  component: () => (
    <>
      <title>{t`Dashboard`}</title>
      <h1 className="text-2xl font-semibold">
        <Trans>Dashboard</Trans>
      </h1>
      <PanelBoundary>
        <Groups />
      </PanelBoundary>
    </>
  ),
})
