import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router"
import { QueryClient } from "@tanstack/react-query"
import { I18nProvider } from "@lingui/react"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { i18n } from "@/lib/i18n"
import { routeTree } from "./routeTree.gen"

function at(path: string) {
  const router = createRouter({
    routeTree,
    context: { queryClient: new QueryClient() },
    history: createMemoryHistory({ initialEntries: [path] }),
  })
  return render(
    <I18nProvider i18n={i18n}>
      <RouterProvider router={router} />
    </I18nProvider>,
  )
}

describe("route tree (seed shell)", () => {
  it("renders the dashboard stub with a level-one heading", async () => {
    at("/dashboard")
    expect(await screen.findByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument()
  })
  it("renders the not-found page for an unknown path", async () => {
    at("/nope/never")
    expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeInTheDocument()
  })
})
