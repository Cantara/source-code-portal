import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { http, HttpResponse } from "msw"
import { describe, expect, it } from "vitest"
import { server } from "@/test/server"
import { IndexingCard, matches, toggled, type Indexing } from "./indexing"

const body: Indexing = {
  groups: [],
  repos: ["acme/old"],
  groupRows: [
    { id: "pay", name: "Payments", repos: 2, excluded: false },
    { id: "id", name: "Identity", repos: 1, excluded: false },
  ],
  repoRows: [
    { address: "acme/pay-api", groups: ["pay"], excluded: false, via: false },
    { address: "acme/old", groups: [], excluded: true, via: false },
  ],
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

  it("finds a repository by the words of its address or its products", () => {
    const r = { address: "acme/pay-api", groups: ["pay"] }
    expect(matches(r, "")).toBe(true)
    expect(matches(r, "PAY api")).toBe(true)
    expect(matches(r, "identity")).toBe(false)
  })

  it("saves the whole list with the revision it was read at", async () => {
    let sent: unknown
    server.use(
      http.get("*/api/v1/admin/indexing", () => HttpResponse.json(body)),
      http.put("*/api/v1/admin/indexing", async ({ request }) => {
        sent = await request.json()
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
    await userEvent.click(save)
    await waitFor(() => expect(sent).toEqual({ revision: "r1", groups: ["pay"], repos: ["acme/old"] }))
    await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toBeDisabled())
  })
})
