import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it } from "vitest"
import { juleol } from "@/entities/announcement/fixture"
import { dismissedOf, dismissedValue } from "@/lib/preferences"
import { Announcements } from "./announcements"

afterEach(() => {
  document.cookie = "scp-announcements=; Max-Age=0; Path=/"
})

describe("Announcements", () => {
  it("draws the card in its language with its link, and keeps a dismissal in the cookie", async () => {
    const user = userEvent.setup()
    render(<Announcements list={[juleol]} />)
    const region = screen.getByRole("region", { name: "Announcements" })
    expect(screen.getByRole("heading", { name: juleol.title })).toHaveAttribute("lang", "nb")
    expect(region).toHaveTextContent("Event")
    expect(region).toHaveTextContent("Rebel Makerspace")
    const link = screen.getByRole("link", { name: /Meld deg på her/ })
    expect(link).toHaveAttribute("href", "https://example.org/pamelding")
    expect(link).toHaveAttribute("rel", "noopener noreferrer")
    expect(screen.queryByRole("img", { name: /QR code/ })).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: `Dismiss “${juleol.title}”` }))
    expect(screen.queryByRole("region", { name: "Announcements" })).not.toBeInTheDocument()
    expect(document.cookie).toContain("scp-announcements=juleol%3Aabc123")
  })
  it("on a wall cannot be dismissed and shows the link's QR code", () => {
    render(<Announcements list={[juleol]} dismissed={["juleol:abc123"]} wall />)
    expect(screen.getByRole("img", { name: "QR code for Meld deg på her" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Dismiss/ })).not.toBeInTheDocument()
  })
  it("draws nothing without an announcement to show", () => {
    const { container } = render(<Announcements list={[juleol]} dismissed={["juleol:abc123"]} />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe("the dismissal cookie", () => {
  it("keeps valid pairs, the newest 20", () => {
    expect(dismissedOf("a:1f,<x>:2,b:2e")).toEqual(["a:1f", "b:2e"])
    const many = Array.from({ length: 25 }, (_, i) => `n${i}:a`)
    expect(dismissedValue(many, "last:b").split(",")).toHaveLength(20)
    expect(dismissedValue(["a:1", "b:2"], "a:1")).toBe("b:2,a:1")
  })
})
