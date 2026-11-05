import { describe, expect, it } from "vitest"
import { issuesPath, issuesSearch, issuesState, repoIssues } from "./api"

describe("issuesState", () => {
  it("keeps the known filters of the address and writes them back", () => {
    const st = issuesState(
      { group: "iam", repo: "Acme/Core", state: "triage", kind: "bug", by: "people", sort: "newest", page: "2", per: "25" },
      50,
    )
    expect(st).toEqual({
      filter: { group: "iam", repo: "acme/core", state: "triage", kind: "bug", by: "people", sort: "newest" },
      page: 2,
      size: 25,
    })
    expect(issuesSearch(st, 50)).toBe("?group=iam&repo=acme%2Fcore&state=triage&kind=bug&by=people&sort=newest&page=2&per=25")
  })

  it("drops what it does not know and the defaults", () => {
    const st = issuesState({ state: "closed", kind: "chore", by: "bots", sort: "waiting", page: "x" }, 50)
    expect(st).toEqual({ filter: {}, page: 1, size: 50 })
    expect(issuesSearch(st, 50)).toBe("")
  })

  it("reads a repository's panel and links a repository or a group", () => {
    expect(repoIssues("Acme", "Core")).toEqual({ repo: "acme/core", size: 10 })
    expect(issuesPath({ repo: "Acme/Core" })).toBe("/issues?repo=acme%2Fcore")
    expect(issuesPath({ group: "iam" })).toBe("/issues?group=iam")
  })
})
