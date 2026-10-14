import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { App } from "./app"

describe("App", () => {
  it("renders the dashboard at /", async () => {
    render(<App initialPath="/" />)
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
  })

  it("renders the not-found page for an unknown path", async () => {
    render(<App initialPath="/no/such/page" />)
    expect(await screen.findByRole("heading", { name: "Page not found" })).toBeInTheDocument()
  })
})
