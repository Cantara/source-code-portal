import { act, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ProblemAlert } from "./problem-alert"
import { ProblemError } from "@/lib/api/problem"
import { renderI18n as render } from "@/test/render"

describe("ProblemAlert", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it("shows the localised title and the request id, not the raw detail", () => {
    const e = new ProblemError({ status: 500, code: "internal", title: "x", detail: "stack trace here", requestId: "req-1" })
    render(<ProblemAlert error={e} />)
    expect(screen.getByText("Something went wrong on our side")).toBeInTheDocument()
    expect(screen.getByText(/req-1/)).toBeInTheDocument()
    expect(screen.queryByText(/stack trace here/)).toBeNull()
  })
  it("auto-retries a not-ready problem after the countdown and keeps the countdown out of the accessibility tree", () => {
    const onRetry = vi.fn()
    const e = new ProblemError({ status: 503, code: "not-ready", title: "t", retryAfterSeconds: 3 })
    const { container } = render(<ProblemAlert error={e} onRetry={onRetry} />)
    expect(container.querySelector("[aria-hidden=true]:not(svg)")).not.toBeNull()
    for (let i = 0; i < 4; i++) act(() => void vi.advanceTimersByTime(1000)) // one render per tick, as in the browser
    expect(onRetry).toHaveBeenCalledTimes(1)
  })
  it("does not auto-retry a 404", () => {
    const onRetry = vi.fn()
    render(<ProblemAlert error={new ProblemError({ status: 404, code: "not-found", title: "t" })} onRetry={onRetry} />)
    for (let i = 0; i < 60; i++) act(() => void vi.advanceTimersByTime(1000))
    expect(onRetry).not.toHaveBeenCalled()
  })
  it("never renders the server's English title, even for a code this build does not know (V-04)", () => {
    render(<ProblemAlert error={new ProblemError({ status: 418, code: "teapot", title: "Upstream said: boom" })} />)
    expect(screen.queryByText(/Upstream said/)).toBeNull()
    expect(screen.getByText("Unexpected problem")).toBeInTheDocument()
  })
  it("a persisting problem is retried once per countdown, not on every render (V-05)", () => {
    let calls = 0
    const e = new ProblemError({ status: 503, code: "not-ready", title: "t", retryAfterSeconds: 3 })
    const { rerender } = render(<ProblemAlert error={e} onRetry={() => void calls++} />)
    for (let i = 0; i < 12; i++) {
      act(() => void vi.advanceTimersByTime(1000))
      rerender(<ProblemAlert error={e} onRetry={() => void calls++} />) // a parent re-render hands over a fresh closure
    }
    expect(calls).toBe(4) // at 3, 6, 9 and 12 s
  })
})
