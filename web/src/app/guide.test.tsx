import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import shots from "@/features/guide/shots.json"
import { GuideView } from "@/features/guide/view"

describe("guide", () => {
  it("has one h1, a contents entry per section, every picture in both themes and a callout per row of the card", () => {
    const { container } = render(<GuideView version="2.0.1" />)
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1)
    const contents = within(screen.getByRole("navigation", { name: "Guide" })).getAllByRole("link")
    expect(contents).toHaveLength(container.querySelectorAll("section[id]").length)
    for (const img of container.querySelectorAll("img")) {
      const name = (img.getAttribute("src") ?? "").replace(/^\/guide\/|-(light|dark)\.webp$/g, "")
      expect(Object.keys(shots.sizes)).toContain(name)
      expect(img).toHaveAttribute("alt")
    }
    expect(container.querySelectorAll("img").length % 2).toBe(0)
    expect(container.querySelectorAll("svg[role=img] circle")).toHaveLength(Object.keys(shots.callouts).length)
    expect(screen.getByLabelText("Run the container").textContent).toMatch(
      /--cap-drop=ALL \\\n {2}-p .*source-code-portal:2\.0\.1 serve/s,
    )
  })

  it("leaves the version to fill in on a development build", () => {
    render(<GuideView version="dev" />)
    expect(screen.getByLabelText("Run the container")).toHaveTextContent("source-code-portal:<version> serve")
  })
})
