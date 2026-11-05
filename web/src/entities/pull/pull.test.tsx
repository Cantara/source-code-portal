import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { pull } from "@/test/server"
import { PullList, PullStateChip, stateOf } from "./pull"

describe("PullList", () => {
  it("says whose turn each is, where it is, who opened it and what it changes", () => {
    render(
      <PullList
        pulls={[
          pull(12, { title: "Bridge the session", labels: [{ name: "enhancement", color: "0e8a16" }] }),
          pull(9, { title: "Update slf4j", author: "renovate[bot]", bot: true, state: "ready", waitsFor: "maintainer" }),
        ]}
      />,
    )
    expect(screen.getByRole("link", { name: "Bridge the session" })).toHaveAttribute(
      "href",
      "https://github.com/acme/core/pull/12",
    )
    expect(screen.getByText("Needs review")).toBeInTheDocument()
    expect(screen.getByText(", waits for a review")).toHaveClass("sr-only")
    expect(screen.getByRole("link", { name: "acme/core #12" })).toHaveAttribute("href", "/repos/acme/core")
    expect(screen.getByText("enhancement")).toBeInTheDocument()
    expect(screen.getByText("(automation)")).toBeInTheDocument()
    expect(screen.getByText("Ready to merge")).toBeInTheDocument()
    expect(screen.getAllByText("+12")).toHaveLength(2)
  })

  it("keeps two short lines when compact and says when there are none", () => {
    const { container, rerender } = render(<PullList pulls={[pull(1)]} compact showRepo={false} />)
    expect(container).toHaveTextContent("#1")
    expect(container).not.toHaveTextContent("+12")
    rerender(<PullList pulls={[]} empty="Nothing waits" />)
    expect(screen.getByText("Nothing waits")).toBeInTheDocument()
  })

  it("names every state with whose turn it is", () => {
    render(<PullStateChip state="failing" />)
    expect(screen.getByTitle("Failing checks: waits for its author")).toBeInTheDocument()
    expect(stateOf("unknown").label).toBe("Not read yet")
  })
})
