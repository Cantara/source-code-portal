import { render, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

type Config = {
  domain: string
  endpoint: string
  transformRequest: (p: { n: string; u: string; d: string }) => { u: string } | null
}
const init = vi.fn<(c: Config) => void>()
vi.mock("@plausible-analytics/tracker", () => ({ init }))

describe("the tracker", () => {
  beforeEach(() => {
    init.mockClear()
    vi.resetModules()
  })

  it("starts once, posting to the portal's own address, without a page's query", async () => {
    const { Tracker } = await import("./analytics")
    const { unmount } = render(<Tracker domain="scp.example.org" path="/_v/m" />)
    await waitFor(() => expect(init).toHaveBeenCalledTimes(1))
    const call = init.mock.calls.at(0)
    if (!call) throw new Error("init was not called")
    const [config] = call
    expect(config).toMatchObject({ domain: "scp.example.org", endpoint: "/_v/m" })
    expect(
      config.transformRequest({ n: "pageview", u: "https://scp.example.org/pulls?author=ann", d: "scp.example.org" })?.u,
    ).toBe("https://scp.example.org/pulls")
    expect(config.transformRequest({ n: "pageview", u: "https://scp.example.org/present", d: "scp.example.org" })).toBeNull()
    unmount()
    render(<Tracker domain="scp.example.org" path="/_v/m" />)
    await new Promise((r) => setTimeout(r, 10))
    expect(init).toHaveBeenCalledTimes(1)
  })
})
