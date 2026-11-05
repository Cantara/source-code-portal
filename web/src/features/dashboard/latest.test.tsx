import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import userEvent from "@testing-library/user-event"
import { activity, commit, fresh, http, server } from "@/test/server"
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
        <LatestCommits counted={[commit(1), commit(2)]} work={["hand", "ai"]} activity={activity()} count={10} />
      </QueryClientProvider>,
    )
    expect(await screen.findByText("Commit 4")).toBeInTheDocument()
    expect(screen.getAllByText("Commit 1")).toHaveLength(1)
    expect(screen.getByText("Commit 3")).toBeInTheDocument()
    expect(asked).toEqual(["hand,ai:1", "hand,ai:2"])
    expect(await screen.findByRole("link", { name: "Every commit, with filters" })).toHaveAttribute("href", "/commits")
  })

  it("shows the kinds the reader checks in its dialog, one at least, remembered, and back to the work that counts", async () => {
    const user = userEvent.setup()
    const asked: string[] = []
    server.use(
      http.get("/commits", ({ query, response }) => {
        asked.push(query.getAll("work").join(","))
        return response(200).json({ commits: [commit(9)], page: { number: 1, size: 50, total: 1, pages: 1 }, freshness: fresh })
      }),
    )
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <LatestCommits counted={[commit(1)]} work={["hand", "ai"]} activity={activity({ hand: 4, ai: 2 })} count={10} />
      </QueryClientProvider>,
    )
    const label = screen.getByRole("button", { name: "Show commits by the team" })
    expect(label).toHaveAttribute("aria-expanded", "false")
    await user.click(label)
    const dialog = screen.getByRole("dialog", { name: "Show in the latest commits" })
    expect(screen.getByRole("checkbox", { name: /By hand/ })).toHaveFocus()
    expect(screen.getByRole("checkbox", { name: /With AI/ })).toBeChecked()
    expect(screen.getByRole("checkbox", { name: /By automation/ })).not.toBeChecked()
    expect(dialog).toHaveTextContent("last 30 days")
    // Automation too: the list reads its own commits, and the choice is remembered.
    await user.click(screen.getByRole("checkbox", { name: /By automation/ }))
    expect(await screen.findByText("Commit 9")).toBeInTheDocument()
    expect(asked).toContain("hand,ai,automation")
    expect(document.cookie).toContain("scp-latest-work=hand%2Cai%2Cautomation")
    // One kind stays: the last checked cannot be unchecked.
    await user.click(screen.getByRole("checkbox", { name: /By hand/ }))
    await user.click(screen.getByRole("checkbox", { name: /With AI/ }))
    expect(screen.getByRole("checkbox", { name: /By automation/ })).toBeDisabled()
    // Back to the work that counts: the dashboard's commits again, the cookie gone.
    await user.click(screen.getByRole("button", { name: "Same as the work that counts" }))
    expect(await screen.findByText("Commit 1")).toBeInTheDocument()
    expect(document.cookie).not.toContain("scp-latest-work")
    await user.keyboard("{Escape}")
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    expect(label).toHaveFocus()
  })
})
