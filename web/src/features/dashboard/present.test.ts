import { describe, expect, it } from "vitest"
import type { Schemas } from "@/lib/api/client"
import { freshness } from "./present"
import { byNeed } from "./tiles"
import { columnOf } from "@/lib/preferences"

type Group = Schemas["DashboardGroup"]

const group = (id: string, level: Schemas["HealthLevel"] | null, score: number, last: string | null): Group =>
  ({
    id,
    name: id,
    health: level ? { level, score } : null,
    activity: { lastAuthoredAt: last },
  }) as unknown as Group

describe("byNeed", () => {
  it("puts attention first, then watch by score, then the rest by their last change, the unscored last", () => {
    const order = byNeed([
      group("quiet", "good", 90, "2026-01-01T00:00:00Z"),
      group("unscored", null, 0, "2026-11-01T00:00:00Z"),
      group("watch-high", "watch", 70, null),
      group("busy", "good", 80, "2026-11-04T00:00:00Z"),
      group("attention", "attention", 40, null),
      group("watch-low", "watch", 60, null),
    ]).map((g) => g.id)
    expect(order).toEqual(["attention", "watch-low", "watch-high", "busy", "quiet", "unscored"])
  })
})

describe("freshness", () => {
  const now = 10 * 60 * 60_000
  it("is live while the stream is open", () => {
    expect(freshness(true, false, now, now).word).toBe("Live")
  })
  it("polls, then is stale past 2 minutes and offline past 10 without the stream", () => {
    expect(freshness(false, false, now - 60_000, now).word).toBe("Polling")
    expect(freshness(false, false, now - 3 * 60_000, now).word).toBe("Stale")
    expect(freshness(false, false, now - 11 * 60_000, now).word).toBe("Offline")
  })
  it("is offline at once while the server fails", () => {
    expect(freshness(true, true, now, now).word).toBe("Offline")
  })
})

describe("columnOf", () => {
  it("reads the commit column's width on the 5% grid within 20% to 60%", () => {
    expect(columnOf(undefined)).toBe(30)
    expect(columnOf("45")).toBe(45)
    expect(columnOf("43")).toBe(45)
    expect(columnOf("5")).toBe(20)
    expect(columnOf("90")).toBe(60)
    expect(columnOf("wide")).toBe(30)
  })
})
