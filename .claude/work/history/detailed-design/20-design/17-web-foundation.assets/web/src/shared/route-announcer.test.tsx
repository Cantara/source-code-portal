import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router"
import { act, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { RouteAnnouncer } from "./route-announcer"

function setup() {
  const root = createRootRoute({
    component: () => (
      <>
        <main>
          <Outlet />
        </main>
        <RouteAnnouncer />
      </>
    ),
  })
  const page = (path: string, title: string) =>
    createRoute({
      getParentRoute: () => root,
      path,
      validateSearch: (s: Record<string, unknown>) => ({ q: typeof s["q"] === "string" ? s["q"] : "" }),
      component: () => (
        <>
          <title>{title}</title>
          <h1>{title}</h1>
          <input aria-label="filter" />
        </>
      ),
    })
  const router = createRouter({
    routeTree: root.addChildren([page("/a", "Page A"), page("/b", "Page B")]),
    history: createMemoryHistory({ initialEntries: ["/a"] }),
  })
  return router
}

// The test router has its own route ids; the app router's typed registration does not know them.
const go = (router: unknown, opts: object) => (router as { navigate(o: object): Promise<void> }).navigate(opts)

describe("RouteAnnouncer", () => {
  it("does not steal focus on first load, focuses the h1 after navigating to another page, and ignores search-only changes (V-07)", async () => {
    const router = setup()
    render(<RouterProvider router={router} />)
    await screen.findByRole("heading", { name: "Page A" })
    expect(document.activeElement).toBe(document.body) // first load

    const input = screen.getByLabelText("filter")
    input.focus()
    await act(async () => void (await go(router, { to: "/a", search: { q: "abc" } })))
    expect(document.activeElement).toBe(input) // typing in a URL-bound filter keeps focus

    await act(async () => void (await go(router, { to: "/b" })))
    expect(document.activeElement).toBe(await screen.findByRole("heading", { name: "Page B" }))
  })
})
