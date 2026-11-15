import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { http, HttpResponse } from "msw"
import { describe, expect, it } from "vitest"
import { server } from "@/test/server"
import { IndexingCard, lines, matches, toggled, type Indexing } from "./indexing"

const body: Indexing = {
  groups: [],
  repos: ["acme/old"],
  exclude: ["^acme/legacy-"],
  include: [],
  tree: [
    {
      id: "pay",
      name: "Payments",
      excluded: false,
      repos: [
        { address: "acme/pay-api", why: "" },
        { address: "acme/legacy-pay", why: "pattern" },
      ],
    },
  ],
  ungrouped: [{ address: "acme/old", why: "named" }],
  excludeMatches: [1],
  includeMatches: [],
  private: 2,
  revision: "r1",
  writable: true,
  problem: "",
}

describe("what the admin keeps out of search engines", () => {
  it("keeps a set sorted, with an id in it or not", () => {
    expect(toggled(["b"], "a", true)).toEqual(["a", "b"])
    expect(toggled(["a", "b"], "a", false)).toEqual(["b"])
    expect(toggled(["a"], "a", true)).toEqual(["a"])
  })

  it("reads a text box's patterns as the portal does", () => {
    expect(lines(" a \n\nb\na\n")).toEqual(["a", "b"])
    expect(lines("")).toEqual([])
  })

  it("finds a repository by the words of its address", () => {
    expect(matches("acme/pay-api", "")).toBe(true)
    expect(matches("acme/pay-api", "PAY api")).toBe(true)
    expect(matches("acme/pay-api", "identity")).toBe(false)
  })

  it("lists public products with their repositories, and says how many are private without listing them", async () => {
    server.use(http.get("*/api/v1/admin/indexing", () => HttpResponse.json(body)))
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <IndexingCard />
      </QueryClientProvider>,
    )
    expect(await screen.findByRole("checkbox", { name: /acme\/pay-api/ })).not.toBeChecked()
    expect(screen.getByRole("checkbox", { name: /acme\/legacy-pay/ })).toBeChecked()
    expect(screen.getByRole("checkbox", { name: /acme\/legacy-pay/ })).toBeDisabled()
    expect(screen.getByRole("checkbox", { name: /acme\/old/ })).toBeChecked()
    expect(screen.getByText(/2 on this portal/)).toBeInTheDocument()
    expect(screen.getByText("1 of 3 public repositories are in the sitemap, 2 kept out.")).toBeInTheDocument()
    expect(screen.getByText("in the sitemap")).toBeInTheDocument()
    expect(screen.getByText("kept out by a rule")).toBeInTheDocument()
    expect(screen.getByText(/: 1 public repository/)).toBeInTheDocument()
  })

  it("previews a change and saves the whole list with the revision it was read at", async () => {
    let sent: unknown
    server.use(
      http.get("*/api/v1/admin/indexing", () => HttpResponse.json(body)),
      http.put("*/api/v1/admin/indexing", async ({ request }) => {
        const b = (await request.json()) as { dryRun?: boolean }
        if (b.dryRun) return HttpResponse.json({ ...body, excludeMatches: [1, 3] })
        sent = b
        return HttpResponse.json({ ...body, groups: ["pay"], revision: "r2" })
      }),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <IndexingCard />
      </QueryClientProvider>,
    )
    const save = await screen.findByRole("button", { name: "Save" })
    expect(save).toBeDisabled()
    await userEvent.click(screen.getByRole("checkbox", { name: /Payments/ }))
    expect(save).toBeEnabled()
    await userEvent.type(screen.getByLabelText(/Keep out repositories whose address matches/), "\ntmp$")
    await waitFor(() => expect(screen.getByText(/: 3 public repositories/)).toBeInTheDocument())
    await userEvent.click(save)
    await waitFor(() =>
      expect(sent).toEqual({
        revision: "r1",
        groups: ["pay"],
        repos: ["acme/old"],
        exclude: ["^acme/legacy-", "tmp$"],
        include: [],
      }),
    )
    await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toBeDisabled())
  })

  it("shows the portal's reason when a pattern is refused, and does not save it", async () => {
    server.use(
      http.get("*/api/v1/admin/indexing", () => HttpResponse.json(body)),
      http.put("*/api/v1/admin/indexing", () =>
        HttpResponse.json(
          { title: "Validation failed", detail: 'exclude[1]: "a(b" is not a regular expression' },
          { status: 422, headers: { "Content-Type": "application/problem+json" } },
        ),
      ),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <IndexingCard />
      </QueryClientProvider>,
    )
    await screen.findByRole("button", { name: "Save" })
    await userEvent.type(screen.getByLabelText(/Keep out repositories whose address matches/), "\na(b")
    expect(await screen.findByText(/is not a regular expression/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
  })
})
