import { describe, expect, it } from "vitest"
import { parseLook, productName } from "./look"

describe("the deployment's look", () => {
  it("is the built-in one without a theme or with one that cannot be read", () => {
    for (const raw of [undefined, "", "not json", "[1]", "null", '"x"']) {
      expect(parseLook(raw)).toEqual({ name: productName, css: undefined, logo: undefined, favicon: undefined })
    }
  })

  it("takes the name and the addresses the portal serves", () => {
    const raw = JSON.stringify({
      name: "  Acme Code  ",
      css: "/theme/theme.css?v=ab12",
      logo: "/theme/logo.svg?v=1",
      favicon: "/theme/favicon.svg?v=2",
    })
    expect(parseLook(raw)).toEqual({
      name: "Acme Code",
      css: "/theme/theme.css?v=ab12",
      logo: "/theme/logo.svg?v=1",
      favicon: "/theme/favicon.svg?v=2",
    })
  })

  it("links nothing but the portal's own /theme/ files and bounds the name", () => {
    const l = parseLook(
      JSON.stringify({
        name: "x".repeat(100),
        css: "https://evil.example/a.css",
        logo: "//evil.example/l.svg",
        favicon: "/other/f.svg",
      }),
    )
    expect(l.name).toHaveLength(60)
    expect([l.css, l.logo, l.favicon]).toEqual([undefined, undefined, undefined])
    expect(parseLook(JSON.stringify({ name: 7, css: 3 }))).toEqual({
      name: productName,
      css: undefined,
      logo: undefined,
      favicon: undefined,
    })
  })
})
