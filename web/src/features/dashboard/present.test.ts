import { describe, expect, it } from "vitest"
import type { Schemas } from "@/lib/api/client"
import { freshness } from "./present"
import { byNeed, foldText, plan } from "./tiles"
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

describe("plan", () => {
  const ordered = byNeed([
    group("a1", "attention", 30, null),
    group("w1", "watch", 50, null),
    group("w2", "watch", 70, null),
    group("g1", "good", 90, "2026-11-04T00:00:00Z"),
    group("g2", "good", 90, "2026-11-01T00:00:00Z"),
  ])
  const at = (steps: number) => {
    const p = plan(ordered, steps)
    return { tiles: p.tiles.map((t) => `${t.g.id}${t.wide ? "+" : ""}`), folded: p.folded.map((g) => g.id) }
  }
  it("folds the good first, then narrows and folds the mildest at watch, then narrows attention; attention never folds", () => {
    expect(at(0)).toEqual({ tiles: ["a1+", "w1+", "w2+", "g1", "g2"], folded: [] })
    expect(at(2)).toEqual({ tiles: ["a1+", "w1+", "w2+"], folded: ["g1", "g2"] })
    expect(at(3)).toEqual({ tiles: ["a1+", "w1+", "w2"], folded: ["g1", "g2"] })
    expect(at(5)).toEqual({ tiles: ["a1+", "w1"], folded: ["w2", "g1", "g2"] })
    expect(at(99)).toEqual({ tiles: ["a1"], folded: ["w1", "w2", "g1", "g2"] })
  })
  it("names what the last tile holds", () => {
    expect(foldText(plan(ordered, 2).folded)).toBe("2 more good")
    expect(foldText(plan(ordered, 5).folded)).toBe("1 more to watch, 2 good")
  })
})
