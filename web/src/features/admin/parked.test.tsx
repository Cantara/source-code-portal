import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { http, HttpResponse } from "msw"
import { describe, expect, it } from "vitest"
import { server } from "@/test/server"
import { ParkedCard, type Parked } from "./parked"

const body: Parked = {
  repos: ["acme/old", "acme/gone"],
  tree: [
    {
      id: "pay",
      name: "Payments",
      repos: [
        { address: "acme/pay-api", private: false, fork: false, parked: false },
        { address: "acme/pay-vault", private: true, fork: false, parked: false },
      ],
    },
  ],
  ungrouped: [{ address: "acme/old", private: false, fork: false, parked: true }],
  missing: ["acme/gone"],
  revision: "r1",
  writable: true,
  problem: "",
}

function card() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <ParkedCard />
    </QueryClientProvider>,
  )
}

describe("the parked repositories", () => {
  it("lists every repository by product with its state, the private ones too, and the parked ones the catalogue lost", async () => {
    server.use(http.get("*/api/v1/admin/parked", () => HttpResponse.json(body)))
    card()
    expect(await screen.findByText("1 of 3 repositories parked.")).toBeInTheDocument()
    expect(screen.getByRole("checkbox", { name: /acme\/old/ })).toBeChecked()
    expect(screen.getByRole("checkbox", { name: /acme\/pay-vault.*private/ })).not.toBeChecked()
    expect(screen.getByText(/not in the catalogue now/)).toBeInTheDocument()
    expect(screen.getByRole("checkbox", { name: /acme\/gone/ })).toBeChecked()
  })

  it("saves the whole list with the revision it was read at", async () => {
    let sent: unknown
    server.use(
      http.get("*/api/v1/admin/parked", () => HttpResponse.json(body)),
      http.put("*/api/v1/admin/parked", async ({ request }) => {
        sent = await request.json()
        return HttpResponse.json({ ...body, repos: ["acme/gone", "acme/old", "acme/pay-api"], revision: "r2" })
      }),
    )
    card()
    const save = await screen.findByRole("button", { name: "Save" })
    expect(save).toBeDisabled()
    await userEvent.click(screen.getByRole("checkbox", { name: /acme\/pay-api/ }))
    expect(screen.getByText("2 of 3 repositories parked.")).toBeInTheDocument()
    await userEvent.click(save)
    await waitFor(() => expect(sent).toEqual({ revision: "r1", repos: ["acme/gone", "acme/old", "acme/pay-api"] }))
    await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toBeDisabled())
  })

  it("offers to load the saved list when someone saved in between", async () => {
    server.use(
      http.get("*/api/v1/admin/parked", () => HttpResponse.json(body)),
      http.put("*/api/v1/admin/parked", () =>
        HttpResponse.json(
          {
            type: "about:blank",
            title: "conflict",
            status: 412,
            code: "conflict",
            detail: "Someone saved the list since you opened it",
          },
          { status: 412, headers: { "Content-Type": "application/problem+json" } },
        ),
      ),
    )
    card()
    await userEvent.click(await screen.findByRole("checkbox", { name: /acme\/pay-api/ }))
    await userEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByRole("button", { name: "Load the saved list" })).toBeInTheDocument()
  })
})
