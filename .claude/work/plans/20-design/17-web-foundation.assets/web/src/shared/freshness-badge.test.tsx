import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { FreshnessBadge } from "./freshness-badge"
import type { components } from "@/lib/api/schema"

const base: components["schemas"]["Freshness"] = {
  status: "fresh",
  kind: "k",
  asOf: null,
  fetchedAt: null,
  checkedAt: null,
  staleSince: null,
  lastErrorCode: null,
}
describe("FreshnessBadge", () => {
  it.each([
    ["fresh", "Up to date"],
    ["stale", "Delayed"],
    ["error", "Update failing"],
    ["unknown", "Not loaded yet"],
  ] as const)("%s -> text %s (never colour alone)", (status, text) => {
    render(<FreshnessBadge freshness={{ ...base, status }} />)
    expect(screen.getByText(text)).toBeInTheDocument()
  })
  it("treats an unknown enum value as unknown, not as a crash (open enum)", () => {
    render(<FreshnessBadge freshness={{ ...base, status: "future-state" as never }} />)
    expect(screen.getByText("Unknown")).toBeInTheDocument()
  })
  it("renders a relative time with the exact time as datetime when asOf is present", () => {
    render(<FreshnessBadge freshness={{ ...base, asOf: "2026-10-14T09:00:00Z" }} />)
    expect(document.querySelector("time")).toHaveAttribute("datetime", "2026-10-14T09:00:00Z")
  })
})
