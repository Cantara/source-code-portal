import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { blocks, DiffStat, said } from "./diffstat"

describe("DiffStat", () => {
  it("shares five blocks out as GitHub does", () => {
    expect(blocks({ additions: 12, deletions: 3 })).toEqual(["add", "add", "add", "add", "del"])
    expect(blocks({ additions: 0, deletions: 40 })).toEqual(["del", "del", "del", "del", "del"])
    expect(blocks({ additions: 5, deletions: 5 })).toEqual(["add", "add", "add", "del", "del"])
    expect(blocks({ additions: 0, deletions: 0 })).toEqual(["none", "none", "none", "none", "none"])
  })

  it("says the change in words, the files when GitHub tells them", () => {
    expect(said({ additions: 12, deletions: 1, files: 2 })).toBe("12 additions and 1 deletion in 2 files")
    expect(said({ additions: 1, deletions: 0 })).toBe("1 addition and 0 deletions")
  })

  it("draws +a −d for a reader and the words for a screen reader, nothing without stats", () => {
    const { container, rerender } = render(<DiffStat stats={{ additions: 1240, deletions: 3, files: 9 }} />)
    expect(container).toHaveTextContent("+1,240")
    expect(container).toHaveTextContent("−3")
    expect(screen.getByText("1,240 additions and 3 deletions in 9 files")).toHaveClass("sr-only")
    rerender(<DiffStat stats={undefined} />)
    expect(container).toBeEmptyDOMElement()
  })
})
