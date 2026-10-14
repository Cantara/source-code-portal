import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { FreshnessBadge, freshnessLabel } from "./freshness-badge"
import { colour, initials } from "./initials-avatar"
import { ProblemAlert, problemTitle } from "./problem-alert"
import { ProblemError } from "@/lib/api/client"
import { VisibilityBadge } from "./visibility-badge"

describe("shared", () => {
  it("labels freshness, anything unexpected as Unknown", () => {
    expect(["fresh", "stale", "error", "unknown", "weird", undefined].map(freshnessLabel)).toEqual([
      "Up to date",
      "Delayed",
      "Update failing",
      "Unknown",
      "Unknown",
      "Unknown",
    ])
    render(<FreshnessBadge freshness={{ status: "stale", fetchedAt: null, checkedAt: "2026-10-14T08:00:00Z" }} />)
    expect(screen.getByText("Delayed")).toHaveAttribute("title", "Checked Oct 14, 2026, 8:00 AM UTC")
  })

  it("titles problems by code and always shows the request id", () => {
    expect(["not-found", "not-ready", "queue-full", "internal", "else"].map(problemTitle)).toEqual([
      "Not found",
      "The portal is still loading",
      "The portal is busy",
      "Something went wrong on the server",
      "Unexpected problem",
    ])
    render(<ProblemAlert error={new ProblemError(404, "not-found", "No such group", "r1")} />)
    expect(screen.getByRole("alert")).toHaveTextContent("Not foundNo such groupRequest id: r1")
  })

  it("derives initials and a stable colour from a name", () => {
    expect([initials("Ada Lovelace"), initials("ada"), initials("  "), initials("Øyvind von Ås")]).toEqual(["AL", "A", "?", "ØÅ"])
    expect(colour("Ada Lovelace")).toBe(colour("Ada Lovelace"))
  })
})

describe("VisibilityBadge", () => {
  it("marks private and internal repositories only", () => {
    const { rerender, container } = render(<VisibilityBadge visibility="private" />)
    expect(screen.getByText("Private")).toBeInTheDocument()
    rerender(<VisibilityBadge visibility="internal" />)
    expect(screen.getByText("Internal")).toBeInTheDocument()
    rerender(<VisibilityBadge visibility="public" />)
    expect(container).toBeEmptyDOMElement()
  })
})
