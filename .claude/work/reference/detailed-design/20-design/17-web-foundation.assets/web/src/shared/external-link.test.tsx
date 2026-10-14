import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { isWebUrl } from "@/lib/url"
import { ExternalLink } from "./external-link"

describe("ExternalLink", () => {
  it.each([
    ["https://example.org/a", true],
    ["http://example.org", true],
    ["javascript:alert(1)", false],
    ["data:text/html,x", false],
    ["/relative", false],
    ["not a url", false],
  ])("isWebUrl(%s) = %s", (href, want) => {
    expect(isWebUrl(href)).toBe(want)
  })
  it("renders a safe new-tab link for a web URL", () => {
    render(<ExternalLink href="https://example.org/a">Docs</ExternalLink>)
    const a = screen.getByRole("link", { name: /Docs/ })
    expect(a).toHaveAttribute("target", "_blank")
    expect(a.getAttribute("rel")).toContain("noopener")
  })
  it("renders a javascript: URL as text, not a link", () => {
    render(<ExternalLink href="javascript:alert(1)">Evil</ExternalLink>)
    expect(screen.queryByRole("link")).toBeNull()
    expect(screen.getByText("Evil")).toBeInTheDocument()
  })
})
