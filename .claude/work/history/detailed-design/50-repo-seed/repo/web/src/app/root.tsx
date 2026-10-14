import { createRootRouteWithContext, Link, Outlet } from "@tanstack/react-router"
import { Trans } from "@lingui/react/macro"
import type { RouterContext } from "./router"

// Seed shell: header, skip link and the not-found page. The full layout (site title, nav from /api/v1/site, theme toggle, connection
// banner, route announcer, announcement) is design 17 section 5 and design 18.
function Shell() {
  return (
    <>
      <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-background focus:p-2">
        <Trans>Skip to content</Trans>
      </a>
      <header className="border-b">
        <div className="mx-auto flex max-w-6xl items-center gap-x-4 p-3">
          <Link to="/dashboard" className="font-semibold">
            <Trans>Source Code Portal</Trans>
          </Link>
        </div>
      </header>
      <main id="main" className="mx-auto max-w-6xl p-4">
        <Outlet />
      </main>
    </>
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

export const Route = createRootRouteWithContext<RouterContext>()({ component: Shell, notFoundComponent: NotFound })
