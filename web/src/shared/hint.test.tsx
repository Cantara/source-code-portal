import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { Hint } from "./hint"

function Tiles() {
  return (
    <>
      {["alpha", "beta"].map((name) => (
        <Hint
          key={name}
          heading={name}
          swatch="lvl-recent"
          meta="Recent"
          render={(t) => (
            <a {...t} href={`/repos/acme/${name}`}>
              {name} tile
            </a>
          )}
        >
          Last change 1mo ago.
        </Hint>
      ))}
    </>
  )
}

describe("Hint", () => {
  it("shows at once on hover, describes its trigger, and closes on leaving", () => {
    render(<Tiles />)
    const tile = screen.getByText("alpha tile")
    expect(screen.queryByRole("tooltip")).toBeNull()
    fireEvent.pointerEnter(tile, { pointerType: "mouse" })
    const tip = screen.getByRole("tooltip")
    expect(tip).toHaveTextContent("alphaRecentLast change 1mo ago.")
    expect(tile).toHaveAttribute("aria-describedby", tip.id)
    fireEvent.pointerLeave(tile, { pointerType: "mouse" })
    expect(screen.queryByRole("tooltip")).toBeNull()
  })

  it("shows one at a time, from the keyboard too, and Escape closes it", () => {
    render(<Tiles />)
    fireEvent.focus(screen.getByText("alpha tile"))
    fireEvent.pointerEnter(screen.getByText("beta tile"), { pointerType: "mouse" })
    expect(screen.getAllByRole("tooltip")).toHaveLength(1)
    expect(screen.getByRole("tooltip")).toHaveTextContent("beta")
    fireEvent.keyDown(document, { key: "Escape" })
    expect(screen.queryByRole("tooltip")).toBeNull()
  })

  it("on a tap shows the hint first, and the second tap follows the link", () => {
    render(<Tiles />)
    const tile = screen.getByText("alpha tile")
    fireEvent.pointerDown(tile, { pointerType: "touch" })
    fireEvent.focus(tile)
    expect(screen.queryByRole("tooltip")).toBeNull() // the focus a tap brings waits for its click
    expect(fireEvent.click(tile)).toBe(false) // the default (navigation) prevented
    expect(screen.getByRole("tooltip")).toHaveTextContent("alpha")
    expect(fireEvent.click(tile)).toBe(true)
  })
})
