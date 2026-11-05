import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Repo } from "@/lib/api/client"
import { product, repo, rules } from "@/test/server"
import { FindingList, HealthScore } from "./health/findings"
import { HealthBadges } from "./repo/health-badges"
import { activity, byActivity } from "./repo/health"
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
    expect(screen.getByRole("link", { name: "agent" })).toHaveAttribute("href", "/repos/acme/agent#finding-dormant")
    expect(screen.getByRole("link", { name: /^Dormant in agent, high/ })).toHaveAttribute(
      "href",
      "/repos/acme/agent#finding-dormant",
    )
    expect(screen.getByText(/No change for 3 years/)).toBeInTheDocument()
    // The kind and weight are named for screen readers; the points show in the hint, not the row.
    expect(screen.getByText(/Dormant, high, costs 30 points/)).toBeInTheDocument()
    expect(screen.queryByText("−30")).toBeNull()
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

  it("shows the licence short and links it and the issues to their pages", () => {
    const { rerender } = render(<HealthBadges repo={repo("A", { openIssues: 1 })} now={now} />)
    const apl = screen.getByRole("link", { name: "APL2 licence, Apache-2.0" })
    expect(apl).toHaveTextContent("APL2")
    expect(apl).toHaveAttribute("href", "https://spdx.org/licenses/Apache-2.0.html")
    expect(screen.getByRole("link", { name: "1 issue open on GitHub" })).toHaveAttribute(
      "href",
      "https://github.com/acme/A/issues",
    )
    rerender(<HealthBadges repo={repo("A", { license: "GPL-3.0-or-later", openIssues: 4 })} now={now} />)
    expect(screen.getByRole("link", { name: "GPL3 licence, GPL-3.0-or-later" })).toHaveTextContent("GPL3")
    expect(screen.getByRole("link", { name: "4 issues open on GitHub" })).toBeInTheDocument()
    rerender(<HealthBadges repo={repo("A", { license: "Zlib" })} now={now} />)
    expect(screen.getByRole("link", { name: "Zlib licence, Zlib" })).toBeInTheDocument()
    rerender(<HealthBadges repo={repo("A", { license: "NOASSERTION" })} now={now} />)
    expect(screen.getByText("no licence")).toBeInTheDocument()
  })

  it("shows the newest published package where there is no GitHub release", () => {
    const pkg = { ecosystem: "maven", name: "net.example:lib", version: "3.0.21", publishedAt: "2026-10-01T00:00:00Z" }
    const withPackage = repo("A", {
      release: null,
      packages: [{ ...pkg, url: "https://repo.example/net/example/lib/3.0.21/", registry: "example" }],
    })
    const { rerender } = render(<HealthBadges repo={withPackage} now={now} />)
    expect(screen.getByRole("link", { name: /^3\.0\.21 · Maven/ })).toHaveAttribute(
      "href",
      "https://repo.example/net/example/lib/3.0.21/",
    )
    expect(screen.queryByText("no release")).toBeNull()
    rerender(<HealthBadges repo={repo("A")} now={now} />)
    expect(screen.getByRole("link", { name: /Release/ })).toHaveTextContent("v1.0.0")
    rerender(<HealthBadges repo={repo("A", { release: null })} now={now} />)
    expect(screen.getByText("no release")).toBeInTheDocument()
  })
})
