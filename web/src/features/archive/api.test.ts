import { describe, expect, it } from "vitest"
import { archiveSearch, archiveState } from "./api"

describe("archiveState", () => {
  it("keeps the known filters of the address and writes them back", () => {
    const st = archiveState(
      { group: "iam", q: "  Old ", reason: "archived", used: "true", sort: "used", page: "2", per: "25" },
      50,
    )
    expect(st).toEqual({ filter: { group: "iam", q: "Old", reason: "archived", used: true, sort: "used" }, page: 2, size: 25 })
    expect(archiveSearch(st, 50)).toBe("?group=iam&q=Old&reason=archived&used=true&sort=used&page=2&per=25")
  })

  it("drops what it does not know and the defaults", () => {
    const st = archiveState({ reason: "gone", used: "yes", sort: "active", q: "  ", page: "x" }, 50)
    expect(st).toEqual({ filter: {}, page: 1, size: 50 })
    expect(archiveSearch(st, 50)).toBe("")
  })
})
