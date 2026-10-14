import { QueryClient } from "@tanstack/react-query"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import { commit, fresh, http, server, snapshot } from "@/test/server"
import { App } from "./app"

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 10_000 } } })
}

describe("pages", () => {
  it("renders the dashboard with groups, health and the latest commits", async () => {
    render(<App initialPath="/" queryClient={client()} />)
    expect(await screen.findByRole("heading", { level: 1, name: "Dashboard" })).toBeInTheDocument()
    expect(await screen.findByRole("link", { name: "IAM" })).toHaveAttribute("href", "/groups/iam")
    expect(screen.getByText("2 up to date · 1 delayed")).toBeInTheDocument()
    expect(screen.getByText("Commit 1")).toBeInTheDocument()
    expect(screen.getByText("1 repository is in no group.")).toBeInTheDocument()
  })

  it("renders the not-found page for an unknown path and for a 404 problem", async () => {
    render(<App initialPath="/no/such/page" queryClient={client()} />)
    expect(await screen.findByRole("heading", { name: "Page not found" })).toBeInTheDocument()
    cleanupAndRender("/groups/nope")
    expect(await screen.findByRole("heading", { name: "Page not found" })).toBeInTheDocument()
  })

  it("filters and sorts the repositories of a group through the URL", async () => {
    const user = userEvent.setup()
    render(<App initialPath="/groups/iam" queryClient={client()} />)
    await screen.findByRole("heading", { level: 1, name: "IAM" })
    const table = screen.getByRole("table")
    const names = () =>
      within(table)
        .getAllByRole("row")
        .slice(1)
        .map((r) => within(r).getAllByRole("cell")[0]?.textContent)
    expect(names()).toEqual(["acme/Agent", "acme/Core"])
    await user.click(screen.getByRole("button", { name: /Repository/ }))
    expect(names()).toEqual(["acme/Core", "acme/Agent"])
    expect(screen.getByRole("columnheader", { name: /Repository/ })).toHaveAttribute("aria-sort", "descending")
    await user.type(screen.getByRole("searchbox", { name: "Filter by name" }), "agent")
    expect(names()).toEqual(["acme/Agent"])
  })

  it("renders a repository with its descriptor, badges, README and documents", async () => {
    render(<App initialPath="/repos/acme/core" queryClient={client()} />)
    expect(await screen.findByRole("heading", { level: 1, name: "Core identity" })).toBeInTheDocument()
    expect(await screen.findByText("Hello from the README.")).toBeInTheDocument()
    expect(screen.getByText("@acme/iam")).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "last commit badge" })).toHaveAttribute(
      "src",
      "/badges/repos/acme/core/last-commit.svg",
    )
    expect(await screen.findByRole("link", { name: "Setup" })).toHaveAttribute("href", "/repos/acme/core/docs/docs/setup")
  })

  it("shows a repository's technology, dependencies and internal links", async () => {
    render(<App initialPath="/repos/acme/core" queryClient={client()} />)
    const tech = await screen.findByRole("region", { name: "Technology" })
    expect(await within(tech).findByText("Maven")).toBeInTheDocument()
    expect(within(tech).getByText("java 21")).toBeInTheDocument()
    expect(within(tech).getByRole("link", { name: "acme/lib" })).toHaveAttribute("href", "/repos/acme/lib")
    expect(within(tech).getByRole("link", { name: "acme/sso" })).toHaveAttribute("href", "/repos/acme/sso")
    const deps = screen.getByRole("region", { name: "Dependencies" })
    expect(within(deps).getByRole("link", { name: "no.acme:lib" })).toHaveAttribute("href", "/repos/acme/lib")
    expect(within(deps).getByText("org.junit.jupiter:junit-jupiter")).toBeInTheDocument()
  })

  it("renders a document with its outline", async () => {
    render(<App initialPath="/repos/acme/core/docs/docs/setup.md" queryClient={client()} />)
    expect(await screen.findByRole("heading", { level: 1, name: "Setup" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Install" })).toHaveAttribute("href", "#install")
    expect(screen.getByRole("link", { name: "docs/setup.md on GitHub" })).toBeInTheDocument()
  })

  it("shows a problem with its request id when a panel fails", async () => {
    server.use(
      http.get("/dashboard", ({ response }) =>
        response.untyped(
          Response.json(
            { type: "about:blank", title: "Internal", status: 500, code: "internal", instance: "urn:scp:request:abc" },
            { status: 500 },
          ),
        ),
      ),
    )
    render(<App initialPath="/" queryClient={client()} />)
    expect(await screen.findByText("Something went wrong on the server")).toBeInTheDocument()
    expect(screen.getByText("Request id: abc")).toBeInTheDocument()
  })
})

describe("commit log", () => {
  function pages() {
    server.use(
      http.get("/commits", ({ query, response }) => {
        const second = query.get("cursor") === "c2"
        const commits = Array.from({ length: second ? 5 : 50 }, (_, i) => commit(second ? 50 + i : i))
        return response(200).json({ commits, page: { nextCursor: second ? null : "c2", limit: 50 }, freshness: fresh })
      }),
    )
  }

  it("loads more pages and offers new commits after a snapshot change", async () => {
    pages()
    const user = userEvent.setup()
    const qc = client()
    render(<App initialPath="/commits" queryClient={qc} />)
    await screen.findByText("Commit 0")
    await waitFor(() => expect(qc.getQueryData(["snapshot"])).toBeDefined())
    await user.click(screen.getByRole("button", { name: "Load more" }))
    expect(await screen.findByText("Commit 54")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument()

    server.use(http.get("/snapshot", ({ response }) => response(200).json(snapshot(2))))
    await qc.refetchQueries({ queryKey: ["snapshot"] })
    const button = await screen.findByRole("button", { name: "New commits" })
    expect(screen.getByText("Commit 54")).toBeInTheDocument() // the loaded log stays as it is
    await user.click(button)
    await waitFor(() => expect(screen.queryByText("Commit 54")).not.toBeInTheDocument())
    await waitFor(() => expect(screen.getAllByRole("listitem")[0]).toHaveFocus())
  })

  it("refetches a one-page view when the snapshot changes", async () => {
    let n = 0
    server.use(
      http.get("/dashboard", ({ response }) =>
        response(200).json({
          groups: [],
          repoCount: ++n,
          ungroupedCount: 0,
          latest: [],
          freshness: { fresh: 0, stale: 0, unknown: 0, error: 0 },
        }),
      ),
    )
    const qc = client()
    render(<App initialPath="/" queryClient={qc} />)
    expect(await screen.findByText("1 repository in the catalogue.")).toBeInTheDocument()
    await waitFor(() => expect(qc.getQueryData(["snapshot"])).toBeDefined())
    server.use(http.get("/snapshot", ({ response }) => response(200).json(snapshot(2, false))))
    await qc.refetchQueries({ queryKey: ["snapshot"] })
    expect(await screen.findByText("2 repositories in the catalogue.")).toBeInTheDocument()
    expect(screen.getByText(/loading its catalogue/)).toBeInTheDocument()
  })
})

let current: ReturnType<typeof render> | undefined
function cleanupAndRender(path: string) {
  current?.unmount()
  document.body.innerHTML = ""
  current = render(<App initialPath={path} queryClient={client()} />)
}

describe("route tree", () => {
  it("has exactly the page shapes the server answers with the shell (internal/httpapi/spa.go shellShape)", async () => {
    const { routeTree } = await import("./router")
    const paths = (routeTree.children ?? []).map((r) => r.fullPath as string).sort()
    expect(paths).toEqual([
      "/",
      "/commits",
      "/groups/$group",
      "/groups/$group/commits",
      "/repos/$owner/$name",
      "/repos/$owner/$name/commits",
      "/repos/$owner/$name/docs/$",
    ])
  })
})
