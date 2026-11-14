import { describe, expect, it } from "vitest"
import { counted, parseAnalytics, scrub } from "./analytics"

describe("the deployment's analytics", () => {
  it("is no tracker without a site or with settings that cannot be read", () => {
    for (const raw of [
      undefined,
      "",
      "not json",
      "[1]",
      "null",
      '{"path":"/_v/m"}',
      '{"domain":"not a domain","path":"/_v/m"}',
      '{"domain":"scp.example.org"}',
      '{"domain":"scp.example.org","path":"//evil.example/m"}',
      '{"domain":"scp.example.org","path":"https://evil.example/m"}',
    ]) {
      expect(parseAnalytics(raw)).toBeUndefined()
    }
  })

  it("takes the site and the portal's own address for events", () => {
    expect(parseAnalytics('{"domain":"scp.example.org","path":"/_v/m"}')).toEqual({ domain: "scp.example.org", path: "/_v/m" })
    expect(parseAnalytics('{"domain":"scp.example.org/app","path":"/_v/m"}')?.domain).toBe("scp.example.org/app")
  })

  it("sends a page's address without its query, but the campaign's", () => {
    expect(scrub("https://scp.example.org/pulls?author=ann&q=secret#top")).toBe("https://scp.example.org/pulls")
    expect(scrub("https://scp.example.org/?utm_source=news&author=ann&ref=x")).toBe(
      "https://scp.example.org/?utm_source=news&ref=x",
    )
    expect(scrub("not an address")).toBe("not an address")
  })

  it("does not count the presentation page", () => {
    expect(counted("https://scp.example.org/present")).toBe(false)
    expect(counted("https://scp.example.org/present?profile=wall")).toBe(false)
    expect(counted("https://scp.example.org/presentation-tools")).toBe(false)
    expect(counted("https://scp.example.org/repos/acme/core")).toBe(true)
  })
})
