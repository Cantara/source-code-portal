import { describe, expect, it } from "vitest"
import { docConfig, docInstall, docReadme } from "@/test/fixtures"
import { fromDoc, fromGroup, fromRepo, pushRecent, rank, score, type SearchItem } from "./search-model"
import { groupOk } from "@/test/fixtures"
import type { components } from "@/lib/api/schema"

const repo = (slug: string, title: string, summary?: string): components["schemas"]["RepoSummary"] => ({
  slug, name: slug, owner: "acme", title: { value: title, source: "descriptor" }, ...(summary ? { summary: { value: summary, source: "descriptor" as const } } : {}),
  archived: false, fork: false, visibility: "public", defaultBranch: "main", htmlUrl: `https://github.com/acme/${slug}`, tags: [{ value: "java", source: "github-topics" }], groups: [],
  stats: { openIssues: 0, stars: 0, pushedAt: null, headSha: null, lastCommitAt: null }, badges: [], uiUrl: `/repos/${slug}`, apiUrl: `/api/v1/repos/${slug}`, markdownUrl: null,
  freshness: { status: "fresh", kind: "catalog.stats", asOf: null, fetchedAt: null, checkedAt: null, staleSince: null, lastErrorCode: null },
})
const go: SearchItem[] = [{ key: "go:dash", kind: "go", title: "Dashboard", hint: "", haystack: ["dashboard"], target: { to: "/dashboard" } }, { key: "go:deps", kind: "go", title: "Dependencies", hint: "", haystack: ["dependencies"], target: { to: "/dependencies" } }]

describe("score tiers", () => {
  it("exact > prefix > word prefix > substring > subsequence; identifier field beats description", () => {
    const s = (q: string, h: string[]) => score(q, h)
    expect(s("core", ["core"])).toBeGreaterThan(s("core", ["core-lib"])); expect(s("core", ["core-lib"])).toBeGreaterThan(s("lib", ["core-lib"]))
    expect(s("lib", ["core-lib"])).toBeGreaterThan(s("ore", ["core-lib"])); expect(s("ore", ["core-lib"])).toBeGreaterThan(s("cl", ["core-lib"])); expect(s("cl", ["core-lib"])).toBeGreaterThan(0)
    expect(s("zz", ["core-lib"])).toBe(0); expect(s("core", ["x", "core"])).toBeLessThan(s("core", ["core", "x"]))
    expect(s("cl", ["x", "core-lib"])).toBe(0) // subsequence only on the identifier field
  })
})

describe("rank", () => {
  const items = [...go, fromGroup(groupOk), fromRepo(repo("core-lib", "Core Library", "Shared building blocks")), fromRepo(repo("billing-service", "Billing Service")), fromDoc("billing-service", docReadme), fromDoc("billing-service", docInstall), fromDoc("billing-service", docConfig)]
  it("empty query shows only the shortcuts and the recents", () => {
    expect(rank("", items).map((g) => g.kind)).toEqual(["go"])
    expect(rank("", items, [items[3]!]).map((g) => g.kind)).toEqual(["go", "repo"])
  })
  it("groups in a fixed order, deterministic", () => {
    const r = rank("co", items); expect(r.map((g) => g.kind)).toEqual(["group", "repo", "doc"].filter((k) => r.some((g) => g.kind === k)))
    expect(JSON.stringify(rank("co", items))).toBe(JSON.stringify(rank("co", [...items].reverse())))
  })
  it("README doc links to the repo page, other docs to the splat route", () => {
    expect(fromDoc("billing-service", docReadme).target).toEqual({ to: "/repos/$repo", params: { repo: "billing-service" } })
    expect(fromDoc("billing-service", docInstall).target).toEqual({ to: "/repos/$repo/docs/$", params: { repo: "billing-service", _splat: "docs/install" } })
  })
  it("caps per kind", () => {
    const many = Array.from({ length: 30 }, (_, i) => fromRepo(repo(`svc-${i}`, `Service ${i}`)))
    expect(rank("svc", many)[0]!.items).toHaveLength(8)
  })
  it("recent list is MRU, unique, bounded", () => { expect(pushRecent(["a", "b", "c"], "b", 3)).toEqual(["b", "a", "c"]); expect(pushRecent(["a", "b", "c"], "d", 3)).toEqual(["d", "a", "b"]) })
})
