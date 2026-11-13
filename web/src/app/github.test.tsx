import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { http as raw, HttpResponse } from "msw"
import { describe, expect, it } from "vitest"
import type { Tokens } from "@/features/github/api"
import { GitHubView } from "@/features/github/view"
import { StatusBanners } from "@/features/status/banners"
import { RulesProvider } from "@/lib/rules-context"
import { rules, server, snapshot } from "@/test/server"

const create =
  "https://ghe.example/settings/personal-access-tokens/new?name=x&target_name=acme&expires_in=90&contents=read&metadata=read"

const none: Tokens = {
  sources: [{ source: "ghe", webUrl: "https://ghe.example/", owners: [{ owner: "acme", createUrl: create, token: null }] }],
  permissions: ["contents", "metadata", "pull_requests", "issues", "statuses"],
}

type Token = NonNullable<Tokens["sources"][number]["owners"][number]["token"]>

const token = (over: Partial<Token> = {}): Token => ({
  source: "ghe",
  owner: "acme",
  login: "ada_octo",
  createdAt: "2026-11-01T00:00:00Z",
  state: "ok",
  ...over,
})

const linked = (over: Partial<Token> = {}): Tokens => ({
  ...none,
  sources: [
    {
      source: "ghe",
      webUrl: "https://ghe.example/",
      owners: [
        {
          owner: "acme",
          createUrl: create,
          token: token(over),
        },
      ],
    },
  ],
})

/** card is the card a heading names. */
function card(heading: HTMLElement): HTMLElement {
  const c = heading.parentElement
  if (!c) throw new Error("a heading outside a card")
  return c
}

const view = () =>
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <GitHubView />
    </QueryClientProvider>,
  )

describe("your GitHub access", () => {
  it("leads a reader without a token through GitHub's form to a linked token", async () => {
    let state = none
    let posted: unknown
    server.use(
      raw.get("*/api/v1/me/github/tokens", () => HttpResponse.json(state)),
      raw.post("*/api/v1/me/github/tokens", async ({ request }) => {
        posted = await request.json()
        const listedAt = new Date().toISOString()
        state = linked({ listedAt })
        return HttpResponse.json(token({ listedAt }), { status: 201 })
      }),
    )
    const user = userEvent.setup()
    view()
    expect(screen.getByRole("heading", { level: 1, name: "Your GitHub access" })).toBeInTheDocument()
    const region = card(await screen.findByRole("heading", { name: "acme" }))
    expect(within(region).getByText(/No token linked/)).toBeInTheDocument()
    expect(within(region).getByRole("link", { name: /Open GitHub's form/ })).toHaveAttribute("href", create)
    expect(region).toHaveTextContent("Contents, Metadata, Pull requests, Issues, Commit statuses")
    const field = within(region).getByLabelText(/Paste the token here/)
    expect(field).toHaveAttribute("type", "password")
    await user.type(field, "github_pat_x")
    await user.click(within(region).getByRole("button", { name: "Link" }))
    expect(await within(region).findByText(/Linked as/)).toHaveTextContent("ada_octo")
    expect(posted).toEqual({ source: "ghe", owner: "acme", token: "github_pat_x" })
    expect(within(region).getByText(/Your access was read/)).toBeInTheDocument()
    expect(field).toHaveValue("")
  })

  it("says why GitHub refused a token, and warns of one that ends soon", async () => {
    server.use(
      raw.get("*/api/v1/me/github/tokens", () => HttpResponse.json(none)),
      raw.post("*/api/v1/me/github/tokens", () =>
        HttpResponse.json(
          {
            code: "not-yours",
            status: 422,
            title: "Unprocessable",
            detail: "The token belongs to the GitHub user bo_octo, who is not you.",
          },
          { status: 422, headers: { "Content-Type": "application/problem+json" } },
        ),
      ),
    )
    const user = userEvent.setup()
    view()
    const region = card(await screen.findByRole("heading", { name: "acme" }))
    await user.type(within(region).getByLabelText(/Paste the token here/), "github_pat_bo")
    await user.click(within(region).getByRole("button", { name: "Link" }))
    expect(await within(region).findByRole("alert")).toHaveTextContent("bo_octo, who is not you")

    server.use(
      raw.get("*/api/v1/me/github/tokens", () =>
        HttpResponse.json(linked({ expiresAt: new Date(Date.now() + 3 * 24 * 3600 * 1000).toISOString() })),
      ),
    )
    const again = view()
    expect(await again.findByText(/The token ends soon/)).toBeInTheDocument()
    again.unmount()
    server.use(raw.get("*/api/v1/me/github/tokens", () => HttpResponse.json(linked({ state: "unauthorized" }))))
    view()
    expect(await screen.findByText(/GitHub refused the token: it expired or was revoked/)).toBeInTheDocument()
  })
})

describe("the status banner in a portal read with people's tokens", () => {
  it("leads a reader to their GitHub access while there is no catalogue, not to wait for one", async () => {
    server.use(raw.get("*/api/v1/snapshot", () => HttpResponse.json(snapshot(1, false))))
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <RulesProvider rules={{ ...rules, features: { ...rules.features, people: true } }}>
          <StatusBanners />
        </RulesProvider>
      </QueryClientProvider>,
    )
    expect(await screen.findByRole("link", { name: "Link your GitHub access" })).toHaveAttribute("href", "/github")
    expect(screen.queryByText(/loading its catalogue/)).not.toBeInTheDocument()
  })
})
