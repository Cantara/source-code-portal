import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Repo } from "@/lib/api/client"
import { product, repo, rules } from "@/test/server"
import { FindingList, HealthScore } from "./health/findings"
import { activity, byActivity } from "./repo/health"
import { shipped } from "./repo/packages"
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
    expect(activity(at(30), now, rules)).toBe("active")
    expect(activity(at(31), now, rules)).toBe("recent")
    expect(activity(at(182), now, rules)).toBe("recent")
    expect(activity(at(183), now, rules)).toBe("quiet")
    expect(activity(at(730), now, rules)).toBe("quiet")
    expect(activity(at(731), now, rules)).toBe("dormant")
    expect(activity(at(1, { archived: true }), now, rules)).toBe("archived")
    expect(
      activity(
        at(1, { lastCommitAt: null, lastAuthoredAt: null, pushedAt: new Date(now - 400 * day).toISOString() }),
        now,
        rules,
      ),
    ).toBe("quiet")
  })

  it("orders newest first with archived last", () => {
    const list = [at(400), at(1, { archived: true }), at(2)].sort(byActivity)
    expect(list.map((r) => r.name)).toEqual(["r2", "r400", "r1"])
  })
})

describe("findings on a repository's page", () => {
  it("are the targets of the marks and end with their next step on GitHub", () => {
    const { container } = render(
      <FindingList
        findings={[{ code: "unreleased", penalty: 20, message: "Unreleased changes" }]}
        github="https://github.com/acme/core"
      />,
    )
    expect(container.querySelector("#finding-unreleased")).toHaveTextContent("Unreleased changes")
    expect(screen.getByRole("link", { name: "Draft a release ↗" })).toHaveAttribute(
      "href",
      "https://github.com/acme/core/releases/new",
    )
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
    expect(activity(bots, now, rules)).toBe("dormant")
  })
})

describe("health", () => {
  it("shows a score with its level in words and the findings with their repository", () => {
    render(<HealthScore health={product} />)
    expect(screen.getByText("74")).toBeInTheDocument()
    expect(screen.getByText("Watch")).toBeInTheDocument()
    render(<FindingList findings={product.findings} />)
    // The repository and the finding's mark open the finding on the repository's page, where its next step is.
    expect(screen.getByRole("link", { name: /^agent: No change for 3 years/ })).toHaveAttribute(
      "href",
      "/repos/acme/agent#finding-dormant",
    )
    expect(screen.getByRole("link", { name: /^Dormant in agent, high/ })).toHaveAttribute(
      "href",
      "/repos/acme/agent#finding-dormant",
    )
    expect(screen.getByText(/No change for 3 years/)).toBeInTheDocument()
    // The kind and weight are named for screen readers; the points show in the hint, not the row.
    expect(screen.getByText(/Dormant, high, costs 30 points/)).toBeInTheDocument()
    expect(screen.queryByText("−30")).toBeNull()
  })
})

describe("shipped", () => {
  const pkg = {
    ecosystem: "maven",
    name: "core",
    version: "2.0.0",
    url: "https://repo/core",
    registry: "",
    publishedAt: "2026-11-05T00:00:00Z",
  }
  const gh = { tag: "v1.0.0", url: "https://github.com/acme/core/releases/v1.0.0", publishedAt: "2026-10-01T00:00:00Z" }
  const base = repo("core")

  it("is the newer of the GitHub release and the newest package, as /releases says", () => {
    expect(shipped({ ...base, release: gh, packages: [pkg] })).toMatchObject({ from: "Maven", version: "2.0.0", name: "core" })
    expect(shipped({ ...base, release: { ...gh, publishedAt: "2026-11-06T00:00:00Z" }, packages: [pkg] })).toMatchObject({
      from: "GitHub",
      version: "v1.0.0",
    })
    expect(shipped({ ...base, release: gh, packages: [{ ...pkg, publishedAt: null }] })?.from).toBe("GitHub")
    expect(shipped({ ...base, release: null, packages: [pkg] })?.from).toBe("Maven")
    expect(shipped({ ...base, release: null, packages: [] })).toBeUndefined()
  })
})
