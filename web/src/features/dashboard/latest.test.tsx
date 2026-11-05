import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { commit, fresh, http, server } from "@/test/server"
import { LatestCommits } from "./latest"

// jsdom lays nothing out: the stand-in reports the end of the list in view as soon as it is watched.
class InView {
  constructor(private readonly seen: IntersectionObserverCallback) {}
  observe(el: Element) {
    this.seen([{ isIntersecting: true, target: el } as IntersectionObserverEntry], this as unknown as IntersectionObserver)
  }
  disconnect() {}
}

afterEach(() => vi.unstubAllGlobals())

describe("LatestCommits", () => {
  it("reads the next pages of the same lens at its end, each commit once, and stops at the last", async () => {
    vi.stubGlobal("IntersectionObserver", InView)
    const asked: string[] = []
    server.use(
      http.get("/commits", ({ query, response }) => {
        asked.push(`${query.getAll("work").join(",")}:${query.get("page")}`)
        const page = Number(query.get("page"))
        // Page 1 repeats the two already shown (the dashboard's), page 2 is the last.
        const commits = page === 1 ? [commit(1), commit(2), commit(3)] : [commit(4)]
        return response(200).json({ commits, page: { number: page, size: 50, total: 4, pages: 2 }, freshness: fresh })
      }),
    )
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <LatestCommits counted={[commit(1), commit(2)]} work={["hand", "ai"]} count={10} />
      </QueryClientProvider>,
    )
    expect(await screen.findByText("Commit 4")).toBeInTheDocument()
    expect(screen.getAllByText("Commit 1")).toHaveLength(1)
    expect(screen.getByText("Commit 3")).toBeInTheDocument()
    expect(asked).toEqual(["hand,ai:1", "hand,ai:2"])
    expect(await screen.findByRole("link", { name: "Every commit, with filters" })).toHaveAttribute("href", "/commits")
  })
})
