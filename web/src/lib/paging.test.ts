import { describe, expect, it } from "vitest"
import { firstPageOf, pageItems, pageNumber, pageSize } from "./paging"

describe("paging", () => {
  it("shows the first, the last and two either side of the current page", () => {
    expect(pageItems(1, 1)).toEqual([1])
    expect(pageItems(1, 3)).toEqual([1, 2, 3])
    expect(pageItems(1, 12)).toEqual([1, 2, 3, "gap", 12])
    expect(pageItems(6, 12)).toEqual([1, "gap", 4, 5, 6, 7, 8, "gap", 12])
    expect(pageItems(4, 12)).toEqual([1, 2, 3, 4, 5, 6, "gap", 12]) // a gap of one page shows it
    expect(pageItems(12, 12)).toEqual([1, "gap", 10, 11, 12])
    expect(pageItems(9, 12)).toEqual([1, "gap", 7, 8, 9, 10, 11, 12])
  })

  it("reads page numbers and sizes from the URL, falling back on anything else", () => {
    expect(["3", "0", "-1", "1.5", "abc", "100001", undefined, "100000"].map(pageNumber)).toEqual([3, 1, 1, 1, 1, 1, 1, 100000])
    expect(["25", "200", "30", "x", undefined, 50].map((v) => pageSize(v, 100))).toEqual([25, 200, 100, 100, 100, 50])
  })

  it("keeps the first item in view when the page size changes", () => {
    expect(firstPageOf(1, 50, 100)).toBe(1)
    expect(firstPageOf(3, 50, 100)).toBe(2) // item 101 is on page 2 of 100
    expect(firstPageOf(2, 100, 25)).toBe(5) // item 101 is on page 5 of 25
  })
})
