import { act, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { HeaderStatus } from "./header-status"
import { setDisplayZone } from "@/lib/format"
import { renderI18n as render } from "@/test/render"

describe("HeaderStatus", () => {
  it("shows the repository count (singular and plural) and the last full sync as a <time>", () => {
    const { container, rerender } = render(<HeaderStatus site={{ repoCount: 42, lastFullSync: "2026-10-14T09:30:00Z" }} />)
    expect(screen.getByText(/42 repositories/)).toBeInTheDocument()
    expect(container.querySelector("time")).toHaveAttribute("datetime", "2026-10-14T09:30:00Z")
    rerender(<HeaderStatus site={{ repoCount: 1, lastFullSync: null }} />)
    expect(screen.getByText(/1 repository/)).toBeInTheDocument()
  })
  it("says so when nothing has been synced (never shows render time as Updated)", () => {
    render(<HeaderStatus site={{ repoCount: 0, lastFullSync: null }} />)
    expect(screen.getByText(/Not synced yet/)).toBeInTheDocument()
  })
  it("renders the exact time in the display zone of the site", () => {
    const { container } = render(<HeaderStatus site={{ repoCount: 3, lastFullSync: "2026-10-14T09:30:00Z" }} />)
    act(() => setDisplayZone("Europe/Oslo"))
    expect(container.querySelector("time")?.getAttribute("title")).toMatch(/11:30/)
    act(() => setDisplayZone("UTC"))
    expect(container.querySelector("time")?.getAttribute("title")).toMatch(/9:30/)
  })
})
