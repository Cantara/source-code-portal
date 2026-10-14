import { act, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { PanelBoundary } from "./panel-boundary"
import { ProblemError } from "@/lib/api/problem"
import { renderI18n as render } from "@/test/render"

function Flaky({ fail }: { fail: { current: boolean } }) {
  if (fail.current) throw new ProblemError({ status: 503, code: "upstream-unavailable", title: "x", requestId: "req-9" })
  return <p>panel ok</p>
}

describe("PanelBoundary", () => {
  it("one failing panel renders a problem alert and leaves its sibling alone; Try again re-renders it", async () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined) // React logs the caught error
    const fail = { current: true }
    render(
      <>
        <p>sibling</p>
        <PanelBoundary>
          <Flaky fail={fail} />
        </PanelBoundary>
      </>,
    )
    expect(screen.getByText("sibling")).toBeInTheDocument()
    expect(screen.getByText("GitHub data is temporarily unavailable")).toBeInTheDocument()
    expect(screen.getByText(/req-9/)).toBeInTheDocument()
    fail.current = false
    await userEvent.click(screen.getByRole("button", { name: /Try again/ }))
    expect(screen.getByText("panel ok")).toBeInTheDocument()
    vi.restoreAllMocks()
  })
  it("a child that keeps failing is retried on the countdown, not in a loop (V-05)", () => {
    vi.useFakeTimers()
    vi.spyOn(console, "error").mockImplementation(() => undefined)
    let attempts = 0
    function Always(): never {
      attempts++
      throw new ProblemError({ status: 503, code: "not-ready", title: "x", retryAfterSeconds: 3 })
    }
    render(
      <PanelBoundary>
        <Always />
      </PanelBoundary>,
    )
    const first = attempts
    for (let i = 0; i < 12; i++) act(() => void vi.advanceTimersByTime(1000))
    expect(Math.round((attempts - first) / 2)).toBeLessThanOrEqual(4) // React may render a failing child twice per attempt
    vi.useRealTimers()
    vi.restoreAllMocks()
  })
})
