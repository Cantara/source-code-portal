import { describe, expect, it } from "vitest"
import { ProblemError } from "@/lib/api/problem"
import { AVATAR_COLOURS, avatarColour, fnv1a, initialsOf, isBot } from "./avatar"
import { ageSeconds, causeId, needsBanner, toneOf, worst } from "./freshness"
import { problemView, retryDelayMs } from "./problem-view"
import { freshCatalog, staleManifests } from "@/test/fixtures"

describe("avatar", () => {
  it.each([
    ["Kari Nordmann", "kari-n", "KN"], ["Dependabot", null, "D"], ["dependabot[bot]", "dependabot[bot]", "D"], [null, "ole-b", "O"],
    ["  ", null, "?"], [null, null, "?"], ["Åse Ødegård", null, "ÅØ"], ["jean-luc picard", null, "JP"], ["李 雷", null, "李雷"],
    ["👩\u200D💻 Dev", null, "👩\u200D💻D"],
  ])("initialsOf(%s, %s) = %s", (n, l, want) => expect(initialsOf(n as string | null, l as string | null)).toBe(want))
  it("bot detection and stable colour", () => {
    expect(isBot("renovate[bot]")).toBe(true); expect(isBot("kari")).toBe(false)
    const c = avatarColour("Kari Nordmann", "Kari-N")
    expect(c).toBe(avatarColour("Someone Else", "kari-n")) // the login is the key, case-insensitive
    expect(c).toBeGreaterThanOrEqual(1); expect(c).toBeLessThanOrEqual(AVATAR_COLOURS)
    expect(fnv1a("a")).toBe(0xe40c292c) // FNV-1a 32 of "a"
  })
})

describe("freshness", () => {
  it("worst / tone / banner", () => {
    expect(worst([])).toBe("unknown"); expect(worst([freshCatalog, staleManifests])).toBe("stale")
    expect(worst([{ ...freshCatalog, status: "error" }, staleManifests])).toBe("error")
    expect(worst([{ ...freshCatalog, status: "future-status" as "fresh" }])).toBe("unknown") // open enum
    expect(toneOf("stale")).toBe("warning"); expect(needsBanner("unknown")).toBe(false); expect(needsBanner("error")).toBe(true)
  })
  it("age and cause", () => {
    expect(ageSeconds(freshCatalog, new Date("2026-10-14T09:41:51Z"))).toBe(60)
    expect(ageSeconds(freshCatalog, new Date("2026-10-14T09:00:00Z"))).toBe(0) // clock skew never goes negative
    expect(ageSeconds({ ...freshCatalog, asOf: null, fetchedAt: null }, new Date())).toBeNull()
    expect(causeId("github.rate_limited")).toBe("freshness.cause.rate-limited")
    expect(causeId("github.weird")).toBe("freshness.cause.github")
    expect(causeId("zzz")).toBe("freshness.cause.unknown"); expect(causeId(null)).toBe("freshness.cause.unknown")
    // the closed set unit 09 emits for commit feeds, and the dotted 404 of unit 07 (V-26): a sentence, never "Unknown cause"
    expect(causeId("unavailable")).toBe("freshness.cause.unavailable"); expect(causeId("forbidden")).toBe("freshness.cause.forbidden")
    expect(causeId("not_found")).toBe("freshness.cause.not-found"); expect(causeId("github.not_found")).toBe("freshness.cause.not-found")
    expect(causeId("parse.failed")).toBe("freshness.cause.parse"); expect(causeId("permanent")).toBe("freshness.cause.unknown")
    // closed snake_case vocabulary (CD-ER-01)
    expect(causeId("rate_limited")).toBe("freshness.cause.rate-limited"); expect(causeId("auth")).toBe("freshness.cause.credentials")
    expect(causeId("suspended")).toBe("freshness.cause.suspended"); expect(causeId("transient")).toBe("freshness.cause.unavailable")
  })
})

describe("problemView", () => {
  const pe = (code: string, status: number, extra: Partial<{ requestId: string; retryAfterSeconds: number }> = {}) => new ProblemError({ code, status, title: "x", ...extra })
  it("maps every documented code", () => {
    const cases: [string, number, string, string][] = [
      ["not-found", 404, "not-found", "none"], ["repo-renamed", 308, "not-found", "none"], ["feature-disabled", 404, "disabled", "none"],
      ["not-ready", 503, "not-ready", "auto"], ["queue-full", 503, "unavailable", "manual"], ["upstream-unavailable", 503, "unavailable", "manual"],
      ["rate-limited", 429, "rate-limited", "auto"], ["forbidden", 403, "forbidden", "none"], ["unauthorized", 401, "unauthorized", "none"],
      ["validation-failed", 400, "bad-request", "none"], ["invalid-cursor", 400, "bad-request", "none"], ["internal", 500, "server-error", "manual"],
    ]
    for (const [code, status, kind, retry] of cases) { const v = problemView(pe(code, status)); expect([v.kind, v.retry]).toEqual([kind, retry]) }
  })
  it("classifies unknown codes by status and never throws", () => {
    expect(problemView(pe("zzz", 404)).kind).toBe("not-found"); expect(problemView(pe("zzz", 418)).kind).toBe("bad-request")
    expect(problemView(pe("zzz", 502)).kind).toBe("server-error"); expect(problemView(pe("zzz", 503)).kind).toBe("unavailable")
    expect(problemView(new TypeError("Cannot read properties of undefined")).kind).toBe("server-error") // a code bug is not "offline" (V-03)
    expect(problemView(new Error("boom")).kind).toBe("server-error"); expect(problemView(undefined).kind).toBe("server-error")
    expect(problemView(pe("x", 0)).kind).toBe("offline")
  })
  it("maps the client-minted codes of lib/api/problem.ts", () => {
    expect(problemView(pe("client-network", 0)).kind).toBe("offline")
    expect(problemView(pe("client-unparseable", 502)).kind).toBe("unavailable")
    expect(problemView(pe("client-contract", 0)).kind).toBe("server-error")
  })
  it("carries request id, retry-after and i18n ids", () => {
    const v = problemView(pe("not-ready", 503, { requestId: "r-1", retryAfterSeconds: 15 }))
    expect(v).toMatchObject({ requestId: "r-1", retryAfterSeconds: 15, titleId: "problem.not-ready.title", bodyId: "problem.not-ready.body", httpStatus: 503 })
  })
  it("retry delay honours Retry-After (capped) else jittered backoff", () => {
    expect(retryDelayMs(0, 15, () => 0.5)).toBe(15000); expect(retryDelayMs(0, 900, () => 0.5)).toBe(120000)
    expect(retryDelayMs(0, null, () => 0.5)).toBe(2000); expect(retryDelayMs(1, null, () => 0)).toBe(3200); expect(retryDelayMs(9, null, () => 1)).toBe(36000)
  })
})
