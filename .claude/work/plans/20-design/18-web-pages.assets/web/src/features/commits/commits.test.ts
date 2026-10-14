// @vitest-environment jsdom
// (api.ts reaches lib/api/client.ts, which reads window.location at load)
import { describe, expect, it } from "vitest"
import { commitA, commitB } from "@/test/fixtures"
import { commitKey, countNewer, matchesCommit } from "./filter"
import { dayKey, dayLabel, groupByDay, previousDay } from "./groupByDay"
import { commitsSearch } from "./search"

describe("dayKey golden vectors (design 09 section 4.9)", () => {
  const v: [string, string, string][] = [
    ["2026-03-29T23:30:00Z", "Europe/Oslo", "2026-03-30"],
    ["2026-10-25T00:30:00Z", "Europe/Oslo", "2026-10-25"],
    ["2026-10-25T01:30:00Z", "Europe/Oslo", "2026-10-25"],
    ["2026-10-25T23:30:00Z", "Europe/Oslo", "2026-10-26"],
    ["2026-01-01T00:00:00Z", "America/Los_Angeles", "2025-12-31"],
    ["2026-06-15T10:00:00Z", "Pacific/Kiritimati", "2026-06-16"],
    ["2026-06-15T18:30:00Z", "Asia/Kolkata", "2026-06-16"],
    ["2026-03-08T09:59:59Z", "America/Los_Angeles", "2026-03-08"],
    ["2026-03-08T10:00:00Z", "America/Los_Angeles", "2026-03-08"],
  ]
  it.each(v)("%s in %s is %s", (iso, zone, want) => expect(dayKey(iso, zone)).toBe(want))
  it("rejects an invalid zone", () => expect(() => dayKey("2026-01-01T00:00:00Z", "Mars/Olympus")).toThrow(RangeError))
})

describe("groupByDay", () => {
  const items = [{ committedAt: "2026-10-14T22:30:00Z" }, { committedAt: "2026-10-14T21:30:00Z" }, { committedAt: "2026-10-14T05:00:00Z" }]
  it("splits at local midnight, not UTC midnight", () => {
    expect(groupByDay(items, "Europe/Oslo").map((g) => [g.day, g.items.length])).toEqual([["2026-10-15", 1], ["2026-10-14", 2]])
    expect(groupByDay(items, "UTC").map((g) => [g.day, g.items.length])).toEqual([["2026-10-14", 3]])
  })
  it("is pure: input untouched, empty in empty out", () => {
    const copy = structuredClone(items)
    groupByDay(items, "UTC")
    expect(items).toEqual(copy)
    expect(groupByDay([], "UTC")).toEqual([])
  })
  it("appending a page merges the boundary day (same function over the concatenation)", () => {
    const p1 = [{ committedAt: "2026-10-14T10:00:00Z" }], p2 = [{ committedAt: "2026-10-14T09:00:00Z" }, { committedAt: "2026-10-13T09:00:00Z" }]
    expect(groupByDay([...p1, ...p2], "UTC").map((g) => g.items.length)).toEqual([2, 1])
  })
})

describe("labels", () => {
  const now = new Date("2026-03-30T12:00:00Z")
  it("today / yesterday / date by key arithmetic, across DST and month ends", () => {
    expect(dayLabel("2026-03-30", now, "Europe/Oslo")).toEqual({ kind: "today" })
    expect(dayLabel("2026-03-29", now, "Europe/Oslo")).toEqual({ kind: "yesterday" }) // the 23-hour day
    expect(dayLabel("2026-03-28", now, "Europe/Oslo")).toEqual({ kind: "date", day: "2026-03-28" })
    expect(previousDay("2026-03-01")).toBe("2026-02-28")
    expect(previousDay("2026-01-01")).toBe("2025-12-31")
    expect(previousDay("2024-03-01")).toBe("2024-02-29")
  })
})

describe("commit filter and new-commit count", () => {
  it("matches subject, author, login, repo, sha prefix, case-insensitively; null author is fine", () => {
    expect(matchesCommit(commitA, "VAT")).toBe(true)
    expect(matchesCommit(commitA, "kari-n")).toBe(true)
    expect(matchesCommit(commitA, "3f2a9c1")).toBe(true)
    expect(matchesCommit(commitB, "dependabot")).toBe(true)
    expect(matchesCommit(commitB, "nomatch")).toBe(false)
    expect(matchesCommit(commitB, "  ")).toBe(true)
  })
  it("countNewer", () => {
    expect(countNewer([commitA, commitB], commitB)).toBe(1)
    expect(countNewer([commitA, commitB], commitA)).toBe(0)
    expect(countNewer([commitA], commitB)).toBe("many")
    expect(countNewer([], commitB)).toBe(0)
    expect(countNewer([commitA], undefined)).toBe(0)
  })
  it("countNewer compares repo@sha, not the sha alone (GR-07, V-06): a mirror with the same sha must not hide new commits", () => {
    const mirror = { ...commitA, repo: "billing-service-mirror" } // same sha in another repository, newer in the merged list
    expect(countNewer([mirror, commitA, commitB], commitA)).toBe(1)
    expect(commitKey(mirror)).not.toBe(commitKey(commitA))
  })
})

describe("search schema", () => {
  it("bad values degrade to absent instead of failing the route", () => {
    expect(commitsSearch.parse({ repo: "../x", group: "A B", q: 5 })).toEqual({})
    expect(commitsSearch.parse({ repo: "core-lib", group: "platform" })).toEqual({ repo: "core-lib", group: "platform" })
  })
})

describe("commit log follow switch (V-13)", () => {
  it("is a disabled query while the reader has scrolled away, so invalidation cannot insert rows", async () => {
    const { commitLogQuery } = await import("./api")
    expect(commitLogQuery({ repo: "a" }).enabled).toBe(true)
    expect(commitLogQuery({ repo: "a" }, 50, false).enabled).toBe(false)
    expect(commitLogQuery({ repo: "a" }, 50, false).queryKey).toEqual(commitLogQuery({ repo: "a" }).queryKey) // same cache entry, only the flag differs
  })
})
