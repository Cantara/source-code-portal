import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { http, HttpResponse } from "msw"
import { describe, expect, it } from "vitest"
import { server } from "@/test/server"
import { changes, type Doc, type Effective, type Item, VisibilityCard, type Visibility } from "./visibility"

const eff = (over: Partial<Effective> = {}): Effective => ({
  audience: "everyone",
  search: true,
  why: "default",
  rule: -1,
  product: "",
  capped: false,
  ...over,
})
const item = (address: string, over: Partial<Item> = {}): Item => ({
  address,
  private: false,
  fork: false,
  setting: null,
  ...eff(),
  ...over,
})

function body(over: Partial<Visibility> = {}): Visibility {
  return {
    groups: {},
    repos: { "acme/old": { audience: "no-one", search: true }, "acme/gone": { audience: "signed-in", search: true } },
    rules: [],
    tree: [
      {
        id: "pay",
        name: "Payments",
        setting: null,
        ...eff(),
        repos: [item("acme/pay-api"), item("acme/pay-vault", { private: true, audience: "signed-in", capped: true })],
      },
    ],
    ungrouped: [item("acme/old", { audience: "no-one", why: "repository", setting: { audience: "no-one", search: true } })],
    missing: ["acme/gone"],
    ruleMatches: [],
    counts: { everyone: 1, signedIn: 1, noOne: 1, searchOff: 0, private: 1 },
    revision: "r1",
    writable: true,
    problem: "",
    ...over,
  }
}

function card() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <VisibilityCard />
    </QueryClientProvider>,
  )
}

/** previewOf answers a preview as the portal would for the repositories' own choices (no products or rules). */
function previewOf(d: Doc): Visibility {
  const b = body()
  const apply = (r: Item): Item => {
    const st = d.repos[r.address]
    if (!st) return { ...r, setting: null, ...eff(r.private ? { audience: "signed-in", capped: true } : {}) }
    return { ...r, setting: st, ...eff({ audience: st.audience, search: st.search, why: "repository" }) }
  }
  return {
    ...b,
    ...d,
    tree: b.tree.map((g) => ({ ...g, repos: g.repos.map(apply) })),
    ungrouped: b.ungrouped.map(apply),
  }
}

describe("who sees what", () => {
  it("lists every repository by product with its audience, where it comes from, and the private ones locked out of everyone", async () => {
    server.use(http.get("*/api/v1/admin/visibility", () => HttpResponse.json(body())))
    card()
    const list = await screen.findByRole("list", { name: "Products and repositories" })
    expect(within(list).getByText("Payments")).toBeInTheDocument()
    const vault = screen.getByRole("radiogroup", { name: "Who sees acme/pay-vault" })
    expect(within(vault).getByRole("radio", { name: "Everyone" })).toBeDisabled()
    expect(within(vault).getByRole("radio", { name: "Signed in" })).toHaveAttribute("aria-checked", "true")
    expect(screen.getByText("default; private on GitHub")).toBeInTheDocument()
    const old = screen.getByRole("radiogroup", { name: "Who sees acme/old" })
    expect(within(old).getByRole("radio", { name: "No one" })).toHaveAttribute("aria-checked", "true")
    expect(screen.getByText("acme/gone")).toBeInTheDocument()
    expect(screen.getAllByText("never offered to search engines")).toHaveLength(2)
    await userEvent.click(screen.getByRole("button", { name: "Private on GitHub 1" }))
    expect(screen.queryByText("acme/pay-api")).not.toBeInTheDocument()
    expect(screen.getByText("acme/pay-vault")).toBeInTheDocument()
  })

  it("previews a change, reviews it before saving, saves it with the revision read and undoes it", async () => {
    const saved: unknown[] = []
    let current = body()
    server.use(
      http.get("*/api/v1/admin/visibility", () => HttpResponse.json(current)),
      http.post("*/api/v1/admin/visibility/preview", async ({ request }) =>
        HttpResponse.json(previewOf((await request.json()) as Doc)),
      ),
      http.put("*/api/v1/admin/visibility", async ({ request }) => {
        const b = (await request.json()) as Doc & { revision: string }
        saved.push(b)
        current = { ...previewOf(b), revision: `r${saved.length + 1}` }
        return HttpResponse.json(current)
      }),
    )
    card()
    const api = await screen.findByRole("radiogroup", { name: "Who sees acme/pay-api" })
    await userEvent.click(within(api).getByRole("radio", { name: "Signed in" }))
    await screen.findByText("changed")
    await userEvent.click(screen.getByRole("button", { name: "Review and save" }))
    const dialog = screen.getByRole("dialog", { name: "Review 1 change" })
    expect(within(dialog).getByText("Leave the public view · 1")).toBeInTheDocument()
    expect(within(dialog).getByText(/Visitors no longer see it/)).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole("button", { name: "Save 1 change" }))
    await screen.findByText(/Saved 1 change/)
    expect(saved[0]).toMatchObject({ revision: "r1", repos: { "acme/pay-api": { audience: "signed-in", search: true } } })
    await userEvent.click(screen.getByRole("button", { name: "Undo" }))
    await waitFor(() => expect(saved).toHaveLength(2))
    expect(saved[1]).toMatchObject({ revision: "r2" })
    expect((saved[1] as Doc).repos["acme/pay-api"]).toBeUndefined()
  })

  it("sets several selected items at once", async () => {
    const previews: Doc[] = []
    server.use(
      http.get("*/api/v1/admin/visibility", () => HttpResponse.json(body())),
      http.post("*/api/v1/admin/visibility/preview", async ({ request }) => {
        const d = (await request.json()) as Doc
        previews.push(d)
        return HttpResponse.json(previewOf(d))
      }),
    )
    card()
    await userEvent.click(await screen.findByRole("checkbox", { name: "Select acme/pay-api" }))
    await userEvent.click(screen.getByRole("checkbox", { name: "Select acme/pay-vault" }))
    const bar = screen.getByRole("group", { name: "Selected" })
    await userEvent.click(within(bar).getByRole("button", { name: "Everyone" }))
    await waitFor(() => expect(previews.length).toBeGreaterThan(0))
    // The private one is never set to everyone.
    expect(previews.at(-1)?.repos["acme/pay-vault"]).toBeUndefined()
    await userEvent.click(within(bar).getByRole("button", { name: "Keep from search engines" }))
    await waitFor(() => expect(previews.at(-1)?.repos["acme/pay-api"]).toEqual({ audience: "everyone", search: false }))
  })
})

describe("changes", () => {
  it("lists each item once and counts what comes back into the public view or the sitemap", () => {
    const saved = body()
    const repos = Array.from({ length: 5 }, (_, i) => item(`acme/r${i}`, { audience: "no-one" }))
    saved.ungrouped = repos
    const preview = { ...saved, ungrouped: repos.map((r) => ({ ...r, audience: "everyone" as const, search: true })) }
    const list = changes(saved, preview)
    expect(list).toHaveLength(5)
    expect(list.filter((c) => c.back)).toHaveLength(5)
    expect(list[0]?.effect).toMatch(/Visitors see it again/)
  })
})
