import { fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { apply } from "@/lib/theme"
import { ThemeMenu } from "./shell"

afterEach(() => apply("system"))

describe("ThemeMenu", () => {
  it("shows the chosen mode, opens on the chosen item, and a choice closes it with focus back on the button", () => {
    apply("dark")
    render(<ThemeMenu />)
    const button = screen.getByRole("button", { name: "Theme: Dark" })
    expect(screen.queryByRole("menu")).toBeNull()
    fireEvent.click(button)
    expect(button).toHaveAttribute("aria-expanded", "true")
    const dark = screen.getByRole("menuitemradio", { name: "Dark" })
    expect(dark).toHaveAttribute("aria-checked", "true")
    expect(dark).toHaveFocus()
    expect(screen.getAllByRole("menuitemradio").filter((i) => i.getAttribute("aria-checked") === "true")).toHaveLength(1)
    fireEvent.click(screen.getByRole("menuitemradio", { name: "Light" }))
    expect(screen.queryByRole("menu")).toBeNull()
    expect(screen.getByRole("button", { name: "Theme: Light" })).toHaveFocus()
    expect(document.documentElement).not.toHaveClass("dark")
  })

  it("moves with the arrows, wraps, and Escape or a click elsewhere closes it", () => {
    render(<ThemeMenu />)
    const button = screen.getByRole("button", { name: "Theme: System" })
    fireEvent.keyDown(button, { key: "ArrowDown" })
    const menu = screen.getByRole("menu", { name: "Theme" })
    expect(screen.getByRole("menuitemradio", { name: "System" })).toHaveFocus()
    fireEvent.keyDown(menu, { key: "ArrowUp" })
    expect(screen.getByRole("menuitemradio", { name: "Light" })).toHaveFocus()
    fireEvent.keyDown(menu, { key: "Home" })
    expect(screen.getByRole("menuitemradio", { name: "System" })).toHaveFocus()
    fireEvent.keyDown(menu, { key: "Escape" })
    expect(screen.queryByRole("menu")).toBeNull()
    expect(button).toHaveFocus()
    fireEvent.click(button)
    fireEvent.pointerDown(document.body)
    expect(screen.queryByRole("menu")).toBeNull()
  })
})
