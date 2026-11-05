import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import type { ReactNode } from "react"
import type { Schemas } from "@/lib/api/client"
import { usePathname, useSearchParams } from "next/navigation"
import { setLocation } from "@/test/navigation"
import { http, server } from "@/test/server"
import { SearchBox } from "./box"
import { searchHref, searchState } from "./api"
import { SearchView } from "./view"

/** Where shows the in-memory location the views navigate. */
function Where() {
  const q = useSearchParams().toString()
  return <output aria-label="Location">{usePathname() + (q ? `?${q}` : "")}</output>
}

function show(view: ReactNode, path: string) {
  setLocation(path)
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{view}</QueryClientProvider>)
}

const hit = (over: Partial<Schemas["SearchHit"]>): Schemas["SearchHit"] => ({
  kind: "doc",
  title: [{ text: "README › " }, { text: "Session", mark: true }],
  repo: "acme/whydah-core",
  url: "/repos/acme/whydah-core#session",
  snippet: [{ text: "Set the " }, { text: "session", mark: true }, { text: " timeout." }],
  ...over,
})

const result = (over: Partial<Schemas["SearchResult"]> = {}): Schemas["SearchResult"] => ({
  query: "sesion",
  counts: { repo: 1, group: 0, doc: 2, dep: 0, package: 0, commit: 0 },
  corrections: [{ from: "sesion", to: "session" }],
  hits: [hit({}), hit({ kind: "repo", title: [{ text: "acme/whydah-core" }], url: "/repos/acme/whydah-core", snippet: [] })],
  page: { number: 1, size: 25, total: 3, pages: 2 },
  ...over,
})

describe("search", () => {
  it("reads and writes its address", () => {
    expect(searchState({ q: "  oauth ", in: "doc", page: "2" })).toEqual({ q: "oauth", in: "doc", page: 2 })
    expect(searchState({ q: ["x"], in: "nothing", page: "-1" })).toEqual({ q: "", page: 1 })
    expect(searchHref({ q: "a b", in: "commit", page: 3 })).toBe("/search?q=a+b&in=commit&page=3")
    expect(searchHref({ q: "a", page: 1 })).toBe("/search?q=a")
  })

  it("shows the kinds with their counts, the hits with their matches marked, the correction and the pages", async () => {
    let asked: URLSearchParams | undefined
    server.use(
      http.get("/search", ({ request, response }) => {
        asked = new URL(request.url).searchParams
        return response(200).json(result())
      }),
    )
    show(<SearchView />, "/search?q=sesion&page=1")
    expect(await screen.findByRole("link", { name: /Session/ })).toHaveAttribute("href", "/repos/acme/whydah-core#session")
    expect(asked?.get("q")).toBe("sesion")
    expect(asked?.get("limit")).toBe("25")
    const tabs = screen.getByRole("navigation", { name: "Kinds" })
    expect(within(tabs).getByRole("link", { name: "All 3" })).toHaveAttribute("aria-current", "page")
    expect(within(tabs).getByRole("link", { name: "Documents 2" })).toHaveAttribute("href", "/search?q=sesion&in=doc")
    expect(within(tabs).queryByRole("link", { name: /Groups/ })).toBeNull()
    expect(screen.getAllByText("session", { selector: "mark" })).toHaveLength(1)
    expect(screen.getByText(/Nothing holds/)).toHaveTextContent("Nothing holds sesion (searched for session).")
    expect(screen.getByRole("navigation", { name: "Pages of hits" })).toBeInTheDocument()
    expect(screen.getByRole("searchbox", { name: "Search the portal" })).toHaveValue("sesion")
  })

  it("explains the query language before a search and when nothing is found, and shows a bad query's problem", async () => {
    show(<SearchView />, "/search")
    expect(screen.getByRole("heading", { name: "How to search" })).toBeInTheDocument()

    server.use(
      http.get("/search", ({ response }) =>
        response(200).json(
          result({ hits: [], corrections: [], counts: { repo: 0, group: 0, doc: 0, dep: 0, package: 0, commit: 0 } }),
        ),
      ),
    )
    show(<SearchView />, "/search?q=nothing")
    expect(await screen.findByText(/Nothing found for/)).toBeInTheDocument()

    server.use(
      http.get("/search", ({ response }) =>
        response(400).json({
          type: "urn:scp:problem:bad-query",
          title: "Bad Request",
          status: 400,
          detail: "more than 12 words",
          code: "bad-query",
          instance: "x",
        }),
      ),
    )
    show(<SearchView />, "/search?q=a+b")
    expect(await screen.findByRole("alert")).toHaveTextContent("more than 12 words")
  })

  it("suggests as one types, opens a suggestion by the keyboard, and searches on Enter", async () => {
    const user = userEvent.setup()
    server.use(
      http.get("/search/suggest", ({ response }) =>
        response(200).json({
          hits: [
            hit({
              kind: "repo",
              title: [{ text: "acme/" }, { text: "whydah", mark: true }, { text: "-core" }],
              url: "/repos/acme/whydah-core",
            }),
            hit({}),
          ],
        }),
      ),
    )
    show(
      <>
        <SearchBox />
        <Where />
      </>,
      "/",
    )
    const box = screen.getByRole("combobox", { name: "Search the portal" })
    await user.keyboard("/")
    expect(box).toHaveFocus()
    await user.type(box, "whyd")
    const list = await screen.findByRole("listbox", { name: "Suggestions" })
    expect(within(list).getAllByRole("option")).toHaveLength(2)
    expect(box).toHaveAttribute("aria-expanded", "true")
    await user.keyboard("{ArrowDown}")
    expect(box).toHaveAttribute("aria-activedescendant", within(list).getAllByRole("option")[0]?.id)
    await user.keyboard("{Enter}")
    expect(screen.getByRole("status", { name: "Location" })).toHaveTextContent("/repos/acme/whydah-core")
    expect(screen.getByRole("listbox", { hidden: true })).not.toBeVisible()

    setLocation("/")
    await user.clear(box)
    await user.type(box, "oauth{Enter}")
    await waitFor(() => expect(screen.getByRole("status", { name: "Location" })).toHaveTextContent("/search?q=oauth"))
  })

  it("leaves the search page to its own form", () => {
    show(<SearchBox />, "/search?q=x")
    expect(screen.queryByRole("combobox")).toBeNull()
  })
})
