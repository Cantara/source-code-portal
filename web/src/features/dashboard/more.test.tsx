import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { fresh, http, issue, issueCounts, pull, pullCounts, server } from "@/test/server"
import { Issues } from "./issues"
import { Pulls } from "./pulls"
import type { Section } from "./side"

// jsdom lays nothing out: the stand-in reports the end of the list in view as soon as it is watched.
class InView {
  constructor(private readonly seen: IntersectionObserverCallback) {}
  observe(el: Element) {
    this.seen([{ isIntersecting: true, target: el } as IntersectionObserverEntry], this as unknown as IntersectionObserver)
  }
  disconnect() {}
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const section: Section = { open: true, toggle: () => {}, label: "Section", fixed: true }
const client = () => new QueryClient({ defaultOptions: { queries: { retry: false } } })
const page = (number: number) => ({ number, size: 25, total: 4, pages: 2 })

describe("the side sections list the newest first and read more at their end", () => {
  it("Pulls reads the next pages of the same list, each pull request once, and ends with a link to every one", async () => {
    vi.stubGlobal("IntersectionObserver", InView)
    const asked: string[] = []
    const counts = pullCounts({ review: 4 })
    server.use(
      http.get("/pulls", ({ query, response }) => {
        asked.push(`${query.get("sort")}:${query.get("limit")}:${query.get("page") ?? ""}`)
        // The element's own two first, then page 1 repeats them, page 2 is the last.
        const n = Number(query.get("page") ?? 0)
        const pulls = n === 0 ? [pull(1), pull(2)] : n === 1 ? [pull(1), pull(2), pull(3)] : [pull(4)]
        return response(200).json({ pulls, counts, page: page(Math.max(n, 1)), freshness: fresh })
      }),
    )
    render(
      <QueryClientProvider client={client()}>
        <Pulls work={["hand", "ai"]} counts={counts} n={2} section={section} />
      </QueryClientProvider>,
    )
    expect(await screen.findByText("Change 4")).toBeInTheDocument()
    expect(screen.getAllByText("Change 1")).toHaveLength(1)
    expect(asked).toEqual(["newest:2:", "newest:25:1", "newest:25:2"])
    expect(await screen.findByRole("link", { name: "Every pull request, with filters" })).toHaveAttribute(
      "href",
      "/pulls?by=people",
    )
  })

  it("Issues reads more only when its first page was full", async () => {
    vi.stubGlobal("IntersectionObserver", InView)
    const asked: string[] = []
    server.use(
      http.get("/issues", ({ query, response }) => {
        asked.push(`${query.get("sort")}:${query.get("limit")}:${query.get("page") ?? ""}`)
        const n = Number(query.get("page") ?? 0)
        const issues = n === 0 ? [issue(1), issue(2)] : n === 1 ? [issue(1), issue(2), issue(3)] : [issue(4)]
        return response(200).json({ issues, counts: issueCounts({ triage: 4 }), page: page(Math.max(n, 1)), freshness: fresh })
      }),
    )
    const { unmount } = render(
      <QueryClientProvider client={client()}>
        <Issues work={["hand", "ai"]} n={2} section={section} />
      </QueryClientProvider>,
    )
    expect(await screen.findByText("Problem 4")).toBeInTheDocument()
    expect(screen.getAllByText("Problem 1")).toHaveLength(1)
    expect(asked).toEqual(["newest:2:", "newest:25:1", "newest:25:2"])
    unmount()
    // Two of at most five: nothing more to read.
    asked.length = 0
    render(
      <QueryClientProvider client={client()}>
        <Issues work={["hand", "ai"]} n={5} section={section} />
      </QueryClientProvider>,
    )
    expect(await screen.findByText("Problem 2")).toBeInTheDocument()
    expect(asked).toEqual(["newest:5:"])
  })

  it("reads the next page only while the list's end is near, not on what the observer once said", async () => {
    vi.stubGlobal("IntersectionObserver", InView)
    // The first page made the list long: the end is far below the section's bottom.
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (this: Element) {
      const top = this.getAttribute("aria-live") ? 5000 : 0
      return { top, bottom: top, left: 0, right: 0, width: 0, height: 0, x: 0, y: top, toJSON: () => ({}) }
    })
    const asked: string[] = []
    server.use(
      http.get("/pulls", ({ query, response }) => {
        asked.push(query.get("page") ?? "")
        const n = Number(query.get("page") ?? 0)
        const pulls = n === 0 ? [pull(1), pull(2)] : [pull(1), pull(2), pull(3)]
        return response(200).json({ pulls, counts: pullCounts({ review: 4 }), page: page(Math.max(n, 1)), freshness: fresh })
      }),
    )
    render(
      <QueryClientProvider client={client()}>
        <Pulls work={["hand", "ai"]} counts={pullCounts({ review: 4 })} n={2} section={section} />
      </QueryClientProvider>,
    )
    expect(await screen.findByText("Change 3")).toBeInTheDocument()
    expect(asked).toEqual(["", "1"])
  })
})
