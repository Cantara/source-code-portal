import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { http as raw, HttpResponse } from "msw"
import type { ReactNode } from "react"
import { describe, expect, it } from "vitest"
import { AdminView } from "@/features/admin/view"
import type { AdminStatus } from "@/features/admin/api"
import { safeReturn, SignInView } from "@/features/signin/view"
import { setLocation } from "@/test/navigation"
import { http, me, server } from "@/test/server"
import { ViewerMenu } from "./shell"

function show(view: ReactNode, path = "/") {
  setLocation(path)
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{view}</QueryClientProvider>)
}

const status: AdminStatus = {
  ready: true,
  alive: true,
  snapshot: { version: 42, builtAt: "2026-10-13T10:00:00Z", ageSeconds: 3, catalogued: true, repos: 7 },
  queue: { waiting: 2, running: 1 },
  sources: [{ id: "gh", repos: 7, freshness: "fresh", checkedAt: "2026-10-13T10:00:00Z", rateRemaining: 0.93 }],
  webhook: { enabled: false, deliveries: {}, lastDelivery: null },
  cluster: { enabled: false, connected: false, leader: true },
  warnings: ["webhook: not configured; pushes show after the next scheduled listing"],
}

describe("sign-in", () => {
  it("offers the provider and the token, both returning to the page", async () => {
    show(<SignInView returnTo="/groups/iam?q=x" />, "/signin")
    expect(await screen.findByRole("link", { name: "Sign in with single sign-on" })).toHaveAttribute(
      "href",
      "/auth/login?return_to=%2Fgroups%2Fiam%3Fq%3Dx",
    )
    const form = screen.getByRole("button", { name: "Sign in with the token" }).closest("form")
    expect(form).toHaveAttribute("action", "/auth/token")
    expect(form).toHaveAttribute("method", "post")
    expect(form?.querySelector("input[name=return_to]")).toHaveValue("/groups/iam?q=x")
    expect(screen.getByLabelText("Token")).toHaveAttribute("type", "password")
  })

  it("explains a failed attempt and shows only the ways the portal has", async () => {
    server.use(http.get("/me", ({ response }) => response(200).json(me({ signIn: { oidc: true, token: false } }))))
    show(<SignInView error="expired" />, "/signin")
    expect(await screen.findByRole("alert")).toHaveTextContent("took too long")
    expect(screen.queryByLabelText("Token")).not.toBeInTheDocument()
  })

  it("tells a signed-in viewer who they are", async () => {
    server.use(http.get("/me", ({ response }) => response(200).json(me({ signedIn: true, name: "Ada", via: "oidc" }))))
    show(<SignInView returnTo="https://evil.example/" />, "/signin")
    expect(await screen.findByText("Ada")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Continue" })).toHaveAttribute("href", "/")
  })

  it("keeps only the portal's own paths to return to", () => {
    for (const [to, want] of [
      ["/repos/acme/core", "/repos/acme/core"],
      ["//evil.example", "/"],
      ["/\\evil.example", "/"],
      ["https://evil.example", "/"],
      ["/auth/logout", "/"],
      [undefined, "/"],
    ] as const)
      expect(safeReturn(to)).toBe(want)
  })
})

describe("viewer menu", () => {
  it("offers sign-in back to the page", () => {
    show(<ViewerMenu me={me()} />, "/groups/iam")
    expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/signin?return_to=%2Fgroups%2Fiam")
  })

  it("shows the name, the admin page and sign-out", () => {
    show(<ViewerMenu me={me({ signedIn: true, name: "Ada", via: "oidc", admin: true, private: true })} />)
    expect(screen.getByText("Ada")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Admin" })).toHaveAttribute("href", "/admin")
    expect(screen.getByRole("button", { name: "Sign out" }).closest("form")).toHaveAttribute("action", "/auth/logout")
  })

  it("leaves sign-out to the proxy, and sign-in out where the portal has none", () => {
    const { unmount } = show(<ViewerMenu me={me({ signedIn: true, name: "ann", via: "proxy", private: true })} />)
    expect(screen.queryByRole("button", { name: "Sign out" })).not.toBeInTheDocument()
    unmount()
    show(<ViewerMenu me={me({ signIn: { oidc: false, token: false } })} />)
    expect(screen.queryByRole("link", { name: "Sign in" })).not.toBeInTheDocument()
  })
})

describe("admin", () => {
  it("shows the status to an administrator", async () => {
    server.use(
      http.get("/me", ({ response }) => response(200).json(me({ signedIn: true, name: "Ada", via: "oidc", admin: true }))),
      raw.get("*/api/v1/admin/status", () => HttpResponse.json(status)),
    )
    show(<AdminView />, "/admin")
    expect(await screen.findByText("42")).toBeInTheDocument()
    expect(screen.getByText("1 warning")).toBeInTheDocument()
    expect(screen.getByText("93%")).toBeInTheDocument()
    expect(screen.getByText(/Signed in as Ada through single sign-on, an administrator/)).toBeInTheDocument()
  })

  it.each([
    [401, "Sign in as an administrator", true],
    [403, "not an administrator", false],
  ])("answers %i with what to do", async (code, text, signIn) => {
    server.use(
      http.get("/me", ({ response }) =>
        response(200).json(code === 401 ? me() : me({ signedIn: true, name: "Bo", via: "oidc", private: true })),
      ),
      raw.get("*/api/v1/admin/status", () =>
        HttpResponse.json({ code: code === 401 ? "unauthenticated" : "forbidden", status: code }, { status: code }),
      ),
    )
    show(<AdminView />, "/admin")
    expect(await screen.findByText(new RegExp(text))).toBeInTheDocument()
    expect(!!screen.queryByRole("link", { name: "Sign in" })).toBe(signIn)
  })

  it("asks to sign in again when the session ended meanwhile", async () => {
    server.use(
      http.get("/me", ({ response }) => response(200).json(me({ signedIn: true, name: "Ada", via: "oidc", admin: true }))),
      raw.get("*/api/v1/admin/status", () => HttpResponse.json({ code: "unauthenticated", status: 401 }, { status: 401 })),
    )
    show(<AdminView />, "/admin")
    expect(await screen.findByText(/Sign in as an administrator/)).toBeInTheDocument()
  })
})
