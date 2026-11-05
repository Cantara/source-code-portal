import { describe, expect, it } from "vitest"
import { asWritten, countdown, shown, when } from "./announcement"
import { juleol, now } from "./fixture"

describe("announcement times", () => {
  it("are shown as written, in the offset they carry, never converted", () => {
    expect(asWritten("2026-11-26T18:00:00+01:00")).toEqual({ year: 2026, month: 11, day: 26, hour: 18, minute: 0 })
    expect(when(juleol)).toBe("tor. 26. nov. 18:00–23:00")
    expect(when({ ...juleol, lang: undefined, endsAt: "2026-11-27T01:00:00+01:00" })).toBe("Thu, Nov 26 18:00–Fri, Nov 27 01:00")
  })
  it("count down to the start, are now while they last, then nothing", () => {
    expect(countdown(juleol, now)).toBe("in 6 days")
    expect(countdown(juleol, Date.parse("2026-11-26T19:00:00+01:00"))).toBe("now")
    expect(countdown(juleol, Date.parse("2026-11-27T00:00:00+01:00"))).toBeUndefined()
  })
  it("go when their time is over or the reader dismissed this revision", () => {
    expect(shown([juleol], now)).toHaveLength(1)
    expect(shown([juleol], Date.parse("2026-11-26T23:00:00+01:00"))).toHaveLength(0)
    expect(shown([juleol], now, ["juleol:abc123"])).toHaveLength(0)
    expect(shown([juleol], now, ["juleol:older"])).toHaveLength(1)
  })
})
