import { render } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { SafeHtml } from "./safe-html"

describe("SafeHtml", () => {
  it("renders server HTML verbatim (the sanitiser is the server's job, ADR-005)", () => {
    const { container } = render(<SafeHtml html={'<h2 id="doc-x">X</h2><p>hi</p>'} className="prose" />)
    expect(container.querySelector("h2#doc-x")).not.toBeNull()
    expect(container.firstElementChild).toHaveClass("prose")
  })
})
