import { describe, expect, it } from "vitest"
import { pullsSearch, pullsState, repoPulls } from "./api"

describe("pullsState", () => {
  it("keeps the known filters of the address and writes them back", () => {
    const st = pullsState(
      { group: "iam", repo: "Acme/Core", state: "review", by: "people", drafts: "true", sort: "newest", page: "2", per: "25" },
      50,
    )
    expect(st).toEqual({
      filter: { group: "iam", repo: "acme/core", state: "review", by: "people", drafts: true, sort: "newest" },
      page: 2,
      size: 25,
    })
    expect(pullsSearch(st, 50)).toBe("?group=iam&repo=acme%2Fcore&state=review&by=people&drafts=true&sort=newest&page=2&per=25")
  })

  it("keeps a login for what is a person's and drops one that is not a login", () => {
    const st = pullsState({ author: "octo-cat", reviewer: "Ann" }, 50)
    expect(st.filter).toEqual({ author: "octo-cat", reviewer: "Ann" })
    expect(pullsSearch(st, 50)).toBe("?author=octo-cat&reviewer=Ann")
    expect(pullsState({ author: "a b", reviewer: "x/../y" }, 50).filter).toEqual({})
  })

  it("drops what it does not know and the defaults", () => {
    const st = pullsState({ state: "merged", by: "bots", sort: "waiting", page: "x" }, 50)
    expect(st).toEqual({ filter: {}, page: 1, size: 50 })
    expect(pullsSearch(st, 50)).toBe("")
  })

  it("reads a repository's panel with its drafts", () => {
    expect(repoPulls("Acme", "Core")).toEqual({ repo: "acme/core", drafts: true, size: 10 })
  })
})
