import { describe, expect, it } from "vitest"
import { alumniSearch, alumniState } from "./api"

describe("alumniState", () => {
  it("keeps the known filters of the address and writes them back", () => {
    const st = alumniState(
      { group: "iam", q: "  Old ", reason: "archived", used: "true", sort: "used", page: "2", per: "25" },
      50,
    )
    expect(st).toEqual({ filter: { group: "iam", q: "Old", reason: "archived", used: true, sort: "used" }, page: 2, size: 25 })
    expect(alumniSearch(st, 50)).toBe("?group=iam&q=Old&reason=archived&used=true&sort=used&page=2&per=25")
  })

  it("drops what it does not know and the defaults", () => {
    const st = alumniState({ reason: "gone", used: "yes", sort: "active", q: "  ", page: "x" }, 50)
    expect(st).toEqual({ filter: {}, page: 1, size: 50 })
    expect(alumniSearch(st, 50)).toBe("")
  })
})
