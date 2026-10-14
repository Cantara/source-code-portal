import { afterEach, describe, expect, it } from "vitest"
import { readPersisted, writePersisted } from "./persist"

// Runs in real Chromium (vitest browser project): the seed's proof that `pnpm test:browser` works. Real storage, not jsdom's.
describe("persist in a real browser", () => {
  afterEach(() => window.localStorage.clear())
  it("round-trips a value through the real localStorage", () => {
    expect(writePersisted("scp-test", "x")).toBe(true)
    expect(readPersisted("scp-test")).toBe("x")
    expect(writePersisted("scp-test", null)).toBe(true)
    expect(readPersisted("scp-test")).toBeNull()
  })
})
