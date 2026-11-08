import { render } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { Logo } from "./logo"

describe("the logo", () => {
  it("draws the deployment's logo as a decorative picture, else the portal's mark", () => {
    const { container, rerender } = render(<Logo />)
    expect(container.querySelector("svg")).not.toBeNull()
    rerender(<Logo src="/theme/logo.svg?v=1" />)
    const img = container.querySelector("img")
    expect(img?.getAttribute("src")).toBe("/theme/logo.svg?v=1")
    expect(img?.getAttribute("alt")).toBe("")
    expect(container.querySelector("svg")).toBeNull()
  })
})
