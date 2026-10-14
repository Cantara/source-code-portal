import { createRouter } from "@tanstack/react-router"
import type { QueryClient } from "@tanstack/react-query"
import { routeTree } from "./routeTree.gen"

export type RouterContext = { queryClient: QueryClient }

export function createAppRouter(queryClient: QueryClient) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0, // TanStack Query owns freshness
    scrollRestoration: true,
    defaultPendingMs: 150, // show a pending component only after 150 ms and keep it 300 ms: a fast response never flashes a skeleton
    defaultPendingMinMs: 300,
  })
}
declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
}
