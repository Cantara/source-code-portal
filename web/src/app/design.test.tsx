import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { http as raw, HttpResponse } from "msw"
import { describe, expect, it } from "vitest"
import { DashboardView } from "@/features/dashboard/view"
import type { StoredLayout } from "@/features/dashboard/api"
import { setLocation } from "@/test/navigation"
import { http, me, server } from "@/test/server"

function show() {
  setLocation("/")
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 10_000 } } })
  return render(
    <QueryClientProvider client={qc}>
      <DashboardView />
    </QueryClientProvider>,
  )
}

const stored = (over: Partial<StoredLayout> = {}): StoredLayout => ({
  page: "dashboard",
  elements: [{ kind: "telemetry" }, { kind: "products", view: "cards" }],
  revision: "r1",
  stored: true,
  writable: true,
  problem: "",
  ...over,
})

const admin = () => server.use(http.get("/me", ({ response }) => response(200).json(me({ signedIn: true, admin: true }))))

describe("design mode", () => {
  it("is offered to admins only", async () => {
    show()
    expect(await screen.findByRole("link", { name: "IAM" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Design this page" })).not.toBeInTheDocument()
  })

  it("adds, moves and sets up elements, previews them and saves the layout with its revision", async () => {
    admin()
    let saved: unknown
    server.use(
      raw.get("*/api/v1/admin/layouts/dashboard", () => HttpResponse.json(stored())),
      raw.put("*/api/v1/admin/layouts/dashboard", async ({ request }) => {
        saved = await request.json()
        return HttpResponse.json(stored({ revision: "r2", elements: (saved as StoredLayout).elements }))
      }),
    )
    const user = userEvent.setup()
    show()
    await user.click(await screen.findByRole("button", { name: "Design this page" }))
    const list = within(await screen.findByRole("region", { name: "Elements" }))
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
    await user.selectOptions(list.getByRole("combobox", { name: "New element" }), "heading")
    await user.click(list.getByRole("button", { name: "Add" }))
    const heading = list.getByRole("textbox", { name: "Heading" })
    await user.clear(heading)
    await user.type(heading, "Our products")
    await user.click(list.getByRole("button", { name: "Move Heading up" }))
    await user.selectOptions(list.getByRole("combobox", { name: "Show as" }), "table")
    const preview = within(screen.getByRole("region", { name: "Preview" }))
    expect(preview.getByRole("heading", { name: "Our products" })).toBeInTheDocument()
    expect(preview.getByRole("table", { name: /Products/ })).toBeInTheDocument()
    expect(screen.getByText("Unsaved changes")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText("Saved: every viewer sees this layout")).toBeInTheDocument()
    expect(saved).toEqual({
      revision: "r1",
      elements: [{ kind: "telemetry" }, { kind: "heading", text: "Our products" }, { kind: "products", view: "table" }],
    })
  })

  it("refuses to overwrite a newer layout and offers to load it", async () => {
    admin()
    let reads = 0
    server.use(
      raw.get("*/api/v1/admin/layouts/dashboard", () =>
        HttpResponse.json(reads++ === 0 ? stored() : stored({ revision: "r9", elements: [{ kind: "fleet-map" }] })),
      ),
      raw.put("*/api/v1/admin/layouts/dashboard", () =>
        HttpResponse.json(
          { code: "conflict", status: 412, detail: "Someone saved this layout since you opened it; reload to see theirs." },
          { status: 412 },
        ),
      ),
    )
    const user = userEvent.setup()
    show()
    await user.click(await screen.findByRole("button", { name: "Design this page" }))
    const list = within(await screen.findByRole("region", { name: "Elements" }))
    await user.click(list.getByRole("button", { name: "Remove Telemetry" }))
    await user.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText(/Someone saved this layout/)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Load the saved layout" }))
    expect(await list.findByText("Fleet map", { selector: "span" })).toBeInTheDocument()
    expect(screen.getByText("The stored layout")).toBeInTheDocument()
  })

  it("says that saving needs a layout directory", async () => {
    admin()
    server.use(raw.get("*/api/v1/admin/layouts/dashboard", () => HttpResponse.json(stored({ writable: false, stored: false }))))
    const user = userEvent.setup()
    show()
    await user.click(await screen.findByRole("button", { name: "Design this page" }))
    expect(await screen.findByText(/set layout.dir/)).toBeInTheDocument()
  })
})
