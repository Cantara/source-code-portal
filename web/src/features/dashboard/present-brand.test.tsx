import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LookProvider } from "@/lib/look-context"
import { Brand } from "./present-status"

describe("the presentation page's brand", () => {
  it("names the deployment, and says SCP without a name", () => {
    const { rerender } = render(<Brand />)
    expect(screen.getByRole("link")).toHaveAttribute("title", "Source Code Portal: the dashboard")
    expect(screen.getByText("SCP")).toBeInTheDocument()
    rerender(
      <LookProvider look={{ name: "Acme Code", logo: "/theme/logo.svg?v=1" }}>
        <Brand />
      </LookProvider>,
    )
    expect(screen.getByRole("link")).toHaveAttribute("title", "Acme Code: the dashboard")
    expect(screen.getByText("Acme Code, the dashboard")).toBeInTheDocument()
    expect(screen.queryByText("SCP")).toBeNull()
  })
})
