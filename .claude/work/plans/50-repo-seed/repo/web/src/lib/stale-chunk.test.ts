import { describe, expect, it, vi } from "vitest"
import { onPreloadError, RELOAD_GUARD_MS } from "./stale-chunk"
import { STORAGE } from "./storage-keys"

const event = () => new Event("vite:preloadError", { cancelable: true })

describe("onPreloadError (stale chunk after a deploy)", () => {
  it("reloads once, cancels the event and remembers the time", () => {
    const reload = vi.fn()
    const e = event()
    expect(onPreloadError(e, reload, 1_000_000)).toBe(true)
    expect(reload).toHaveBeenCalledTimes(1)
    expect(e.defaultPrevented).toBe(true)
    expect(window.localStorage.getItem(STORAGE.reloadGuard)).toBe("1000000")
  })
  it("does not reload again within 30 s and leaves the event alone so Vite rethrows the real error", () => {
    window.localStorage.setItem(STORAGE.reloadGuard, "1000000")
    const reload = vi.fn()
    const e = event()
    expect(onPreloadError(e, reload, 1_000_000 + RELOAD_GUARD_MS)).toBe(false)
    expect(reload).not.toHaveBeenCalled()
    expect(e.defaultPrevented).toBe(false)
  })
  it("reloads again after the guard window", () => {
    window.localStorage.setItem(STORAGE.reloadGuard, "1000000")
    const reload = vi.fn()
    expect(onPreloadError(event(), reload, 1_000_000 + RELOAD_GUARD_MS + 1)).toBe(true)
    expect(reload).toHaveBeenCalledTimes(1)
  })
  it("never reloads when the time cannot be remembered (blocked storage would loop forever)", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("denied", "SecurityError")
    })
    const reload = vi.fn()
    const e = event()
    expect(onPreloadError(e, reload, 5_000_000)).toBe(false)
    expect(reload).not.toHaveBeenCalled()
    expect(e.defaultPrevented).toBe(false)
    vi.restoreAllMocks()
  })
})
