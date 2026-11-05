import { describe, expect, it } from "vitest"
import type { Schemas } from "@/lib/api/client"
import { freshness } from "./present"
import { byNeed, choose, fits, foldText, plan, type Plan } from "./tiles"
import { columnDefaults, columnsOf, columnsValue, profileNamed, profileOf } from "@/lib/preferences"

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

describe("profiles", () => {
  it("are told from the window: upright is handheld, on its side a tablet up to 1600 px, a desk's monitor beyond", () => {
    expect(profileOf(820, 1180, false)).toBe("handheld-portrait")
    expect(profileOf(1376, 1032, false)).toBe("handheld-landscape")
    expect(profileOf(2560, 1440, false)).toBe("desk")
    expect(profileOf(2560, 1440, true)).toBe("wall")
    expect(profileNamed("desk")).toBe("desk")
    expect(profileNamed("tv")).toBeUndefined()
  })

  it("keep a commit column of their own, on the 5% grid within 20% to 60%, the desk's 60% until dragged", () => {
    expect(columnsOf(undefined)).toEqual(columnDefaults)
    expect(columnsOf(undefined).desk).toBe(60)
    const c = columnsOf("handheld-landscape:43,desk:90,tv:20,wall:wide")
    expect(c["handheld-landscape"]).toBe(45)
    expect(c.desk).toBe(60)
    expect(c.wall).toBe(30)
    expect(columnsValue({ ...columnDefaults, "handheld-landscape": 45 })).toBe("handheld-landscape:45")
    expect(columnsValue(columnDefaults)).toBe("")
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
  const show = (p: Plan) => ({ tiles: p.tiles.map((t) => `${t.g.id}${t.wide ? "+" : ""}`), folded: p.folded.map((g) => g.id) })
  it("narrows then folds the mildest at watch, then narrows attention; attention never folds; good ones as many as asked", () => {
    expect(show(plan(ordered, 0, 2))).toEqual({ tiles: ["a1+", "w1+", "w2+", "g1", "g2"], folded: [] })
    expect(show(plan(ordered, 0, 1))).toEqual({ tiles: ["a1+", "w1+", "w2+", "g1"], folded: ["g2"] })
    expect(show(plan(ordered, 1, 0))).toEqual({ tiles: ["a1+", "w1+", "w2"], folded: ["g1", "g2"] })
    expect(show(plan(ordered, 3, 2))).toEqual({ tiles: ["a1+", "w1", "g1", "g2"], folded: ["w2"] })
    expect(show(plan(ordered, 99, 0))).toEqual({ tiles: ["a1"], folded: ["w1", "w2", "g1", "g2"] })
  })
  it("names what the last tile holds", () => {
    expect(foldText(plan(ordered, 0, 0).folded)).toBe("2 more good")
    expect(foldText(plan(ordered, 3, 0).folded)).toBe("1 more to watch, 2 good")
  })
  it("places tiles as the dense grid does: wide tiles that cannot pair leave a column for the narrow ones", () => {
    // 3 columns, 4 rows: two wide tiles stack in columns 1 and 2; column 3 holds the narrow ones and the fold.
    const grid = { cols: 3, rows: 4 }
    expect(fits(plan(ordered, 1, 2), grid)).toBe(true) // a1+ w1+ stacked, w2 g1 g2 in column 3, nothing folded
    expect(fits(plan(ordered, 0, 0), grid)).toBe(false) // three wide tiles need six rows
    expect(choose(ordered, grid)).toEqual({ steps: 1, good: 2 })
    // 3×2: a1 wide, then column 3 holds w1 and the fold tile (w2, g1, g2 folded).
    expect(choose(ordered, { cols: 3, rows: 2 })).toEqual({ steps: 3, good: 0 })
  })
})
