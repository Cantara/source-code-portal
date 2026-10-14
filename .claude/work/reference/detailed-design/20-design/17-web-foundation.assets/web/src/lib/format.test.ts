import { act, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { formatDateTime, formatRelative, useNow } from "./format"
import { i18n } from "@/lib/i18n"

const now = Date.parse("2026-10-14T10:00:00Z")
describe("formatRelative", () => {
  it.each([
    ["2026-10-14T09:59:40Z", "now"],
    ["2026-10-14T09:55:00Z", "5 minutes ago"],
    ["2026-10-14T07:00:00Z", "3 hours ago"],
    ["2026-10-13T10:00:00Z", "yesterday"],
    ["2026-10-14T10:20:00Z", "in 20 minutes"],
  ])("%s -> %s", (iso, want) => expect(formatRelative(iso, now, "en")).toBe(want))
})

describe("locale (V-08)", () => {
  it("follows the UI locale (i18n.locale), not the browser's", () => {
    expect(formatRelative("2026-10-14T09:55:00Z", now)).toBe("5 minutes ago")
    i18n.load("nb", {})
    i18n.activate("nb")
    try {
      expect(formatRelative("2026-10-14T09:55:00Z", now)).toBe("for 5 minutter siden")
      expect(formatDateTime("2026-10-14T09:55:00Z", "UTC")).toContain("okt")
    } finally {
      i18n.activate("en")
    }
  })
})

describe("useNow (one shared 30 s ticker)", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date("2026-10-14T10:00:00Z"))
  })
  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
  })
  it("advances every 30 s while visible, not while the tab is hidden, and catches up on visibilitychange", () => {
    const { result } = renderHook(() => useNow())
    const t0 = result.current
    act(() => void vi.advanceTimersByTime(30_000))
    expect(result.current - t0).toBe(30_000)
    vi.spyOn(document, "hidden", "get").mockReturnValue(true)
    act(() => void vi.advanceTimersByTime(60_000))
    expect(result.current - t0).toBe(30_000)
    vi.spyOn(document, "hidden", "get").mockReturnValue(false)
    act(() => void document.dispatchEvent(new Event("visibilitychange")))
    expect(result.current - t0).toBe(90_000)
  })
  it("stops its timer when the last subscriber unmounts", () => {
    const { unmount } = renderHook(() => useNow())
    expect(vi.getTimerCount()).toBe(1)
    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
