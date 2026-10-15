import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { crumbs } from "@/lib/crumbs"
import { Breadcrumbs } from "./breadcrumbs"

describe("Breadcrumbs", () => {
  it("links the pages above and marks the page itself", () => {
    render(<Breadcrumbs trail={crumbs.doc("acme", "Core", "docs/setup.md", "Setup")} />)
    const nav = screen.getByRole("navigation", { name: "Breadcrumb" })
    expect(nav.querySelectorAll("a")).toHaveLength(2)
    expect(screen.getByRole("link", { name: "Dashboard" })).toHaveAttribute("href", "/")
    expect(screen.getByRole("link", { name: "acme/Core" })).toHaveAttribute("href", "/repos/acme/core")
    expect(screen.getByText("Setup")).toHaveAttribute("aria-current", "page")
  })
})
