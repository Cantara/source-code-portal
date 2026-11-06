import { describe, expect, it } from "vitest"
import { releasesSearch, releasesState, repoReleases } from "./api"

describe("releasesState", () => {
  it("keeps the known filters of the address and writes them back", () => {
    const st = releasesState(
      { group: "iam", repo: "Acme/Core", variant: "all", since: "30d", sort: "repo", page: "2", per: "25" },
      50,
    )
    expect(st).toEqual({
      filter: { group: "iam", repo: "acme/core", variant: "all", since: "30d", sort: "repo" },
      page: 2,
      size: 25,
    })
    expect(releasesSearch(st, 50)).toBe("?group=iam&repo=acme%2Fcore&variant=all&since=30d&sort=repo&page=2&per=25")
  })

  it("takes an RFC 3339 window and drops what it does not know and the defaults", () => {
    expect(releasesState({ since: "2026-10-01T00:00:00Z" }, 50).filter).toEqual({ since: "2026-10-01T00:00:00Z" })
    const st = releasesState({ variant: "latest", since: "last week", sort: "newest", page: "x" }, 50)
    expect(st).toEqual({ filter: {}, page: 1, size: 50 })
    expect(releasesSearch(st, 50)).toBe("")
  })

  it("reads a repository's panel with every release", () => {
    expect(repoReleases("Acme", "Core")).toEqual({ repo: "acme/core", variant: "all", size: 10 })
  })
})
