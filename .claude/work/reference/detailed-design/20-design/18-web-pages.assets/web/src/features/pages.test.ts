import { describe, expect, it } from "vitest"
import { groupMissing, groupOk } from "@/test/fixtures"
import { cardBadges, cardState, matchesGroup } from "./dashboard/card-model"
import { dashboardSearch } from "./dashboard/search"
import { groupSearch } from "./groups/search"
import { orgDepsSearch, packageSearch, repoDepsSearch } from "./manifests/search"
import { canRetry, overallTone, rateShare, sortWarnings } from "./status/model"
import type { components } from "@/lib/api/schema"

describe("dashboard card model", () => {
  it("a group with a missing default repository still has a card (GR-29) and keeps its configured description (GR-12)", () => {
    expect(cardState(groupMissing)).toEqual({ kind: "degraded", reason: "missing", blurb: "Invoicing and payments." })
    expect(cardState(groupOk)).toEqual({ kind: "ok", headline: "Core Library", blurb: "Shared libraries and infrastructure services." })
    expect(cardState({ ...groupOk, defaultRepoState: "unset" })).toMatchObject({ kind: "degraded", reason: "unset" })
    expect(cardState({ ...groupMissing, description: null })).toEqual({ kind: "degraded", reason: "missing", blurb: null })
  })
  it("repository count badge first, capped", () => {
    expect(cardBadges(groupOk).map((b) => b.kind)).toEqual(["repos", "issues"]); expect(cardBadges(groupOk, 1)).toHaveLength(1)
  })
  it("filter", () => { expect(matchesGroup(groupOk, "core")).toBe(true); expect(matchesGroup(groupOk, "zzz")).toBe(false); expect(matchesGroup(groupOk, undefined)).toBe(true) })
})

describe("route search schemas never throw and apply the documented defaults", () => {
  it("dashboard / group", () => {
    expect(dashboardSearch.parse({})).toEqual({}); expect(dashboardSearch.parse({ q: 3 })).toEqual({})
    expect(groupSearch.parse({})).toEqual({ archived: "include", sort: "name" })
    expect(groupSearch.parse({ sort: "bogus", archived: "x", lifecycle: ["production", "nope"] })).toEqual({ archived: "include", sort: "name" })
    expect(groupSearch.parse({ sort: "-pushedAt", lifecycle: ["production"], tag: ["java"] })).toMatchObject({ sort: "-pushedAt", lifecycle: ["production"], tag: ["java"] })
  })
  it("manifests", () => {
    expect(repoDepsSearch.parse({})).toEqual({ relationship: "direct", sort: "ecosystem" })
    expect(orgDepsSearch.parse({})).toEqual({ view: "graph", depth: 1, intra: false, origin: "all", minVersions: 1 })
    expect(orgDepsSearch.parse({ view: "nope", depth: 9, minVersions: 0, group: "A B" })).toEqual({ view: "graph", depth: 1, intra: false, origin: "all", minVersions: 1 })
    expect(orgDepsSearch.parse({ view: "versions", depth: 3, ecosystem: ["maven"] })).toMatchObject({ view: "versions", depth: 3, ecosystem: ["maven"] })
    expect(packageSearch.parse({ purl: "pkg:maven/org.acme/core-lib" })).toEqual({ purl: "pkg:maven/org.acme/core-lib" })
    expect(packageSearch.parse({ purl: "maven/core" })).toEqual({}); expect(packageSearch.parse({ purl: "pkg:x/" + "a".repeat(1100) })).toEqual({})
  })
})

describe("status model", () => {
  const w = (code: string, severity: "info" | "warning" | "critical"): components["schemas"]["StatusWarning"] => ({ code, severity, message: code, since: null })
  it("critical first, stable", () => expect(sortWarnings([w("a", "info"), w("b", "critical"), w("c", "warning"), w("d", "critical")]).map((x) => x.code)).toEqual(["b", "d", "c", "a"]))
  it("rate share is null for an unknown limit", () => {
    expect(rateShare({ resource: "core", limit: 0, remaining: 0, resetAt: null })).toBeNull(); expect(rateShare({ resource: "core", limit: 5000, remaining: 2500, resetAt: null })).toBe(0.5)
    expect(rateShare({ resource: "core", limit: 5000, remaining: 9000, resetAt: null })).toBe(1)
  })
  const base = { readiness: { state: "ready" as const, readySince: null, gates: [{ name: "store" as const, ok: true, detail: "" }], coverage: { have: 1, total: 1, ratio: 1, coreKinds: [] } }, store: { state: "ok" as const, reason: null, lastBackupAt: null, rebuildsTotal: 0, sizeBytes: null }, warnings: [] as components["schemas"]["StatusWarning"][] }
  it("overall tone", () => {
    expect(overallTone(base)).toBe("ok"); expect(overallTone({ ...base, warnings: [w("x", "info")] })).toBe("ok"); expect(overallTone({ ...base, warnings: [w("x", "warning")] })).toBe("degraded")
    expect(overallTone({ ...base, readiness: { ...base.readiness, gates: [{ name: "store", ok: false, detail: "" }] } })).toBe("down")
    expect(overallTone({ ...base, store: { ...base.store, state: "degraded" } })).toBe("down")
  })
  it("retry key syntax", () => { expect(canRetry("docs.refresh/acme/acme/billing-service")).toBe(true); expect(canRetry("catalog.reconcile/_")).toBe(true); expect(canRetry("nokind")).toBe(false); expect(canRetry("a/ b")).toBe(false) })
})
