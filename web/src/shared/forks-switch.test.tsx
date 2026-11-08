import { fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ForksSwitch } from "./forks-switch"

const reload = vi.fn()

beforeEach(() => {
  reload.mockReset()
  Object.defineProperty(window, "location", { value: { ...window.location, reload, protocol: "http:" }, writable: true })
})

afterEach(() => {
  document.cookie = "scp-forks=; Path=/; Max-Age=0"
})

describe("ForksSwitch", () => {
  it("is absent where no fork is hidden", () => {
    const { container } = render(<ForksSwitch hidden={0} />)
    expect(container).toBeEmptyDOMElement()
  })

  it("offers the hidden forks, remembers the choice in a cookie and reads the page again", () => {
    render(<ForksSwitch hidden={3} />)
    const button = screen.getByRole("button", { name: "Show forks (3)" })
    expect(button).toHaveAttribute("aria-pressed", "false")
    fireEvent.click(button)
    expect(document.cookie).toContain("scp-forks=show")
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it("says the forks are shown and takes the choice back", () => {
    document.cookie = "scp-forks=show; Path=/"
    render(<ForksSwitch hidden={3} />)
    const button = screen.getByRole("button", { name: "Showing forks" })
    expect(button).toHaveAttribute("aria-pressed", "true")
    fireEvent.click(button)
    expect(document.cookie).not.toContain("scp-forks=show")
    expect(reload).toHaveBeenCalledTimes(1)
  })
})
