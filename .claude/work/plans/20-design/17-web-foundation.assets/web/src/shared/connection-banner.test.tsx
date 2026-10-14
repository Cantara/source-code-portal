import { act, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { ConnectionBanner } from "./connection-banner"
import { serverStatus } from "@/lib/server-status"
import { renderI18n as render } from "@/test/render"

describe("ConnectionBanner", () => {
  it("is silent when everything is fine, but the live region exists", () => {
    render(<ConnectionBanner />)
    const region = screen.getByTestId("connection-banner")
    expect(region).toHaveAttribute("role", "status")
    expect(region).toBeEmptyDOMElement()
  })
  it("precedence: build skew over unreachable over warming", () => {
    render(<ConnectionBanner />)
    act(() => serverStatus.reportReadiness("warming"))
    expect(screen.getByText(/still loading data from GitHub/)).toBeInTheDocument()
    act(() => serverStatus.reportFailure())
    expect(screen.getByText(/Cannot refresh right now/)).toBeInTheDocument()
    expect(screen.queryByText(/still loading data/)).toBeNull()
    act(() => serverStatus.reportBuild("other", "mine"))
    expect(screen.getByText(/new version of the portal/)).toBeInTheDocument()
    expect(screen.queryByText(/Cannot refresh/)).toBeNull()
  })
  it("never reports skew for a dev bundle", () => {
    render(<ConnectionBanner />)
    act(() => serverStatus.reportBuild("anything", "dev"))
    expect(screen.queryByText(/new version/)).toBeNull()
  })
  it("recovers when a request succeeds again", () => {
    render(<ConnectionBanner />)
    act(() => serverStatus.reportFailure())
    expect(screen.getByText(/Cannot refresh right now/)).toBeInTheDocument()
    act(() => serverStatus.reportOk())
    expect(screen.queryByText(/Cannot refresh/)).toBeNull()
  })
})
