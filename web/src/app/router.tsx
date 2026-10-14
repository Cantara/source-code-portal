import type { QueryClient } from "@tanstack/react-query"
import { createMemoryHistory, createRootRouteWithContext, createRoute, createRouter, Link, Outlet } from "@tanstack/react-router"

export interface RouterContext {
  queryClient: QueryClient
}

function Shell() {
  return (
    <>
      <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-background focus:p-2">
        Skip to content
      </a>
      <header className="border-b border-border">
        <div className="mx-auto flex max-w-6xl items-center gap-x-4 p-3">
          <Link to="/" className="font-semibold">
            Source Code Portal
          </Link>
        </div>
      </header>
      <main id="main" className="mx-auto max-w-6xl p-4">
        <Outlet />
      </main>
    </>
  )
}

function Dashboard() {
  return <h1 className="text-2xl font-semibold">Dashboard</h1>
}

function NotFound() {
  return (
    <section>
      <h1 className="text-2xl font-semibold">Page not found</h1>
      <p>
        <Link to="/" className="underline">
          Back to the dashboard
        </Link>
      </p>
    </section>
  )
}

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: Shell, notFoundComponent: NotFound })
const dashboardRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: Dashboard })
const routeTree = rootRoute.addChildren([dashboardRoute])

// createAppRouter builds the router; initialPath selects an in-memory history for tests.
export function createAppRouter(queryClient: QueryClient, initialPath?: string) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: "intent",
    ...(initialPath ? { history: createMemoryHistory({ initialEntries: [initialPath] }) } : {}),
  })
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
}
