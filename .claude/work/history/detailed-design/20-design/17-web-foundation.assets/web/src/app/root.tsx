import { createRootRouteWithContext, Link, Outlet, useRouter } from "@tanstack/react-router"
import { Trans } from "@lingui/react/macro"
import { t } from "@lingui/core/macro"
import type { RouterContext } from "./router"
import { ConnectionBanner } from "@/shared/connection-banner"
import { ProblemAlert } from "@/shared/problem-alert"
import { RouteAnnouncer } from "@/shared/route-announcer"
import { ThemeToggle } from "@/shared/theme-toggle"
import { TooltipProvider } from "@/components/ui/tooltip"
import { useQuery } from "@tanstack/react-query"
import { useEffect } from "react"
import { useSnapshotSync } from "@/lib/api/snapshot"
import { siteQuery } from "@/lib/api/site"
import { setDisplayZone } from "@/lib/format"
import { applyServerDefault } from "@/lib/theme"
import { AnnouncementBanner } from "@/shared/announcement"
import { ExternalLink } from "@/shared/external-link"
import { HeaderStatus } from "@/shared/header-status"

function Shell() {
  useSnapshotSync()
  const { data: site } = useQuery(siteQuery())
  const defaultTheme = site?.defaultTheme
  useEffect(() => {
    if (defaultTheme) applyServerDefault(defaultTheme)
  }, [defaultTheme])
  const zone = site?.displayTimeZone
  useEffect(() => {
    if (zone) setDisplayZone(zone)
  }, [zone])
  return (
    <TooltipProvider>
      <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-background focus:p-2">
        <Trans>Skip to content</Trans>
      </a>
      <header className="border-b">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-4 gap-y-2 p-3">
          <Link to="/dashboard" className="font-semibold">
            {site?.title ?? t`Source Code Portal`}
          </Link>
          <nav aria-label={t`Primary`} className="flex flex-wrap gap-x-3 gap-y-1 text-sm">
            <Link to="/dashboard" activeProps={{ "aria-current": "page" }}>
              <Trans>Dashboard</Trans>
            </Link>
            <Link to="/commits" search={{}} activeProps={{ "aria-current": "page" }}>
              <Trans>Commits</Trans>
            </Link>
            {site?.features.manifests ? (
              <Link to="/dependencies" activeProps={{ "aria-current": "page" }}>
                <Trans>Dependencies</Trans>
              </Link>
            ) : null}
            {site?.nav.map((n) =>
              n.kind === "page" && n.page ? (
                <Link key={n.label} to="/pages/$slug" params={{ slug: n.page }} activeProps={{ "aria-current": "page" }}>
                  {n.label}
                </Link>
              ) : n.url ? (
                <ExternalLink key={n.label} href={n.url}>
                  {n.label}
                </ExternalLink>
              ) : null,
            )}
          </nav>
          <div className="ml-auto flex items-center gap-3">
            {site ? <HeaderStatus site={site} /> : null}
            <ThemeToggle />
          </div>
        </div>
      </header>
      <ConnectionBanner />
      {site?.announcement ? <AnnouncementBanner announcement={site.announcement} /> : null}
      <main id="main" className="mx-auto max-w-6xl p-4">
        <Outlet />
      </main>
      <RouteAnnouncer />
    </TooltipProvider>
  )
}

function NotFound() {
  return (
    <section>
      <h1 className="text-2xl font-semibold">
        <Trans>Page not found</Trans>
      </h1>
      <p>
        <Link to="/dashboard">
          <Trans>Back to the dashboard</Trans>
        </Link>
      </p>
    </section>
  )
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Shell,
  notFoundComponent: NotFound,
  errorComponent: function RootError({ error, reset }) {
    const router = useRouter()
    return (
      <main className="mx-auto max-w-6xl p-4">
        <h1 className="sr-only">
          <Trans>Error</Trans>
        </h1>
        <ProblemAlert
          error={error}
          onRetry={() => {
            reset()
            void router.invalidate()
          }}
        />
      </main>
    )
  },
})
