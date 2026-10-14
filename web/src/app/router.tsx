import type { QueryClient } from "@tanstack/react-query"
import { createMemoryHistory, createRootRouteWithContext, createRoute, createRouter, notFound } from "@tanstack/react-router"
import { docQuery } from "@/features/docs/api"
import { groupQuery } from "@/features/groups/api"
import { validateGroupSearch } from "@/features/groups/search"
import { repoQuery } from "@/features/repos/api"
import { ProblemError } from "@/lib/api/client"
import { NotFound } from "./not-found"
import { Root } from "./root"

export interface RouterContext {
  queryClient: QueryClient
}

/** ensure loads a page's main query; a 404 problem becomes the not-found page, other failures show in the page itself. */
async function ensure(load: Promise<unknown>) {
  try {
    await load
  } catch (e) {
    if (e instanceof ProblemError && e.status === 404) throw notFound()
  }
}

function validateCommitSearch(s: Record<string, unknown>): { group?: string; repo?: string } {
  const out: { group?: string; repo?: string } = {}
  if (typeof s.group === "string" && s.group !== "") out.group = s.group
  if (typeof s.repo === "string" && s.repo !== "") out.repo = s.repo
  return out
}

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: Root, notFoundComponent: NotFound })

const dashboard = createRoute({ getParentRoute: () => rootRoute, path: "/" }).lazy(() =>
  import("@/features/dashboard/route").then((m) => m.dashboardRoute),
)
const commits = createRoute({ getParentRoute: () => rootRoute, path: "/commits", validateSearch: validateCommitSearch }).lazy(
  () => import("@/features/commits/route").then((m) => m.allCommitsRoute),
)
const group = createRoute({
  getParentRoute: () => rootRoute,
  path: "/groups/$group",
  validateSearch: validateGroupSearch,
  loader: ({ context, params }) => ensure(context.queryClient.ensureQueryData(groupQuery(params.group))),
}).lazy(() => import("@/features/groups/route").then((m) => m.groupRoute))
const groupCommits = createRoute({
  getParentRoute: () => rootRoute,
  path: "/groups/$group/commits",
  loader: ({ context, params }) => ensure(context.queryClient.ensureQueryData(groupQuery(params.group))),
}).lazy(() => import("@/features/commits/route").then((m) => m.groupCommitsRoute))
const repo = createRoute({
  getParentRoute: () => rootRoute,
  path: "/repos/$owner/$name",
  loader: ({ context, params }) => ensure(context.queryClient.ensureQueryData(repoQuery(params.owner, params.name))),
}).lazy(() => import("@/features/repos/route").then((m) => m.repoRoute))
const repoCommits = createRoute({
  getParentRoute: () => rootRoute,
  path: "/repos/$owner/$name/commits",
  loader: ({ context, params }) => ensure(context.queryClient.ensureQueryData(repoQuery(params.owner, params.name))),
}).lazy(() => import("@/features/commits/route").then((m) => m.repoCommitsRoute))
const doc = createRoute({
  getParentRoute: () => rootRoute,
  path: "/repos/$owner/$name/docs/$",
  loader: ({ context, params }) =>
    ensure(context.queryClient.ensureQueryData(docQuery(params.owner, params.name, params._splat ?? ""))),
}).lazy(() => import("@/features/docs/route").then((m) => m.docRoute))

export const routeTree = rootRoute.addChildren([dashboard, commits, group, groupCommits, repo, repoCommits, doc])

// createAppRouter builds the router; initialPath selects an in-memory history for tests.
export function createAppRouter(queryClient: QueryClient, initialPath?: string) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
    scrollRestoration: true,
    ...(initialPath ? { history: createMemoryHistory({ initialEntries: [initialPath] }) } : {}),
  })
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
}
