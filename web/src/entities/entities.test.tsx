import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Repo } from "@/lib/api/client"
import { product, repo } from "@/test/server"
import { FindingList, HealthScore } from "./health/health"
import { HealthBadges } from "./repo/health-badges"
import { activity, byActivity, pulse } from "./repo/health"
import { VisibilityBadge } from "./repo/visibility-badge"

const day = 24 * 3600 * 1000
const now = Date.parse("2026-10-15T00:00:00Z")
const at = (days: number, over: Partial<Repo> = {}): Repo => ({
  ...repo(`r${days}`),
  lastCommitAt: new Date(now - days * day).toISOString(),
  lastAuthoredAt: new Date(now - days * day).toISOString(),
  ...over,
})

describe("health", () => {
  it("classifies activity at its thresholds", () => {
    expect(activity(at(30), now)).toBe("active")
    expect(activity(at(31), now)).toBe("recent")
    expect(activity(at(182), now)).toBe("recent")
    expect(activity(at(183), now)).toBe("quiet")
    expect(activity(at(730), now)).toBe("quiet")
    expect(activity(at(731), now)).toBe("dormant")
    expect(activity(at(1, { archived: true }), now)).toBe("archived")
    expect(
      activity(at(1, { lastCommitAt: null, lastAuthoredAt: null, pushedAt: new Date(now - 400 * day).toISOString() }), now),
    ).toBe("quiet")
  })

  it("orders newest first with archived last, and summarises", () => {
    const list = [at(400), at(1, { archived: true }), at(2)].sort(byActivity)
    expect(list.map((r) => r.name)).toEqual(["r2", "r400", "r1"])
    const p = pulse([at(2), at(400, { release: null, license: "NOASSERTION", openIssues: 5 })], now)
    expect(p).toMatchObject({ repos: 2, active: 1, released: 1, licensed: 1, issues: 5 })
    expect(p.levels).toMatchObject({ active: 1, quiet: 1 })
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

describe("activity", () => {
  it("is measured from the newest commit by a person", () => {
    const bots = at(1, { lastAuthoredAt: new Date(now - 800 * day).toISOString() })
    expect(activity(bots, now)).toBe("dormant")
  })
})

describe("health", () => {
  it("shows a score with its level in words and the findings with their repository", () => {
    render(<HealthScore health={product} />)
    expect(screen.getByText("74")).toBeInTheDocument()
    expect(screen.getByText("Watch")).toBeInTheDocument()
    render(<FindingList findings={product.findings} />)
    expect(screen.getByRole("link", { name: "agent" })).toHaveAttribute("href", "/repos/acme/agent")
    expect(screen.getByText(/No change for 3 years/)).toBeInTheDocument()
    expect(screen.getByText("−30")).toBeInTheDocument()
  })

  it("adds a health chip to a repository only when it is not good", () => {
    const { container, rerender } = render(<HealthBadges repo={repo("A")} now={now} />)
    expect(container.textContent).not.toMatch(/attention/)
    const dormant = repo("B", {
      health: { score: 40, level: "attention", findings: [{ code: "dormant", penalty: 30, message: "No change for 3 years" }] },
    })
    rerender(<HealthBadges repo={dormant} now={now} />)
    expect(container.textContent).toMatch(/40 needs attention/)
    expect(container.textContent).toMatch(/No change for 3 years/)
  })
})
