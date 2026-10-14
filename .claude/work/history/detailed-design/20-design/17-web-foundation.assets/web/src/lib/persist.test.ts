import { afterEach, describe, expect, it, vi } from "vitest"
import { readPersisted, writePersisted } from "./persist"

afterEach(() => vi.restoreAllMocks())

describe("persist (blocked or full storage must never throw)", () => {
  it("round-trips and removes", () => {
    writePersisted("k", "v")
    expect(readPersisted("k")).toBe("v")
    writePersisted("k", null)
    expect(readPersisted("k")).toBeNull()
  })
  it("read returns null when the accessor throws (SecurityError in a private window)", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("denied", "SecurityError")
    })
    expect(readPersisted("k")).toBeNull()
  })
  it("write swallows QuotaExceededError", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("full", "QuotaExceededError")
    })
    expect(() => writePersisted("k", "v")).not.toThrow()
  })
})
