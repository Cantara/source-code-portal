import { readFileSync } from "node:fs"
import { HttpResponse, http as raw } from "msw"
import { act, renderHook } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { http, server } from "@/test/server"
import { api, ProblemError, unwrap } from "./api/client"
import { relative } from "./format"
import { retryDelay } from "./query"
import { apply, isDark, stored, themeKey, useTheme } from "./theme"

describe("unwrap", () => {
  it("returns the data of a success", async () => {
    expect((await unwrap(api.GET("/snapshot"))).version).toBe(1)
  })

  it("turns a problem into a ProblemError with the request id", async () => {
    server.use(
      http.get("/dashboard", ({ response }) =>
        response.untyped(
          HttpResponse.json(
            {
              type: "about:blank",
              title: "Busy",
              status: 503,
              code: "queue-full",
              detail: "try later",
              instance: "urn:scp:request:r9",
            },
            { status: 503, headers: { "Retry-After": "7" } },
          ),
        ),
      ),
    )
    const e = await unwrap(api.GET("/dashboard")).catch((e: unknown) => e)
    expect(e).toBeInstanceOf(ProblemError)
    expect(e).toMatchObject({
      status: 503,
      code: "queue-full",
      message: "try later",
      requestId: "r9",
      retryAfter: 7,
      retryable: true,
    })
    expect(retryDelay(0, e)).toBe(7000)
  })

  it("turns a network failure into a retryable ProblemError", async () => {
    server.use(raw.get("*/api/v1/groups", () => HttpResponse.error()))
    const e = await unwrap(api.GET("/groups")).catch((e: unknown) => e)
    expect(e).toMatchObject({ status: 0, code: "network", retryable: true })
    expect(retryDelay(1, e)).toBe(2000)
  })

  it("does not retry a 404", () => {
    expect(new ProblemError(404, "not-found", "").retryable).toBe(false)
  })
})

describe("format", () => {
  it("shows times relative to now", () => {
    const now = Date.parse("2026-10-14T12:00:00Z")
    expect(relative("2026-10-14T11:59:50Z", now)).toBe("just now")
    expect(relative("2026-10-14T09:00:00Z", now)).toBe("3 hours ago")
    expect(relative("2026-10-11T12:00:00Z", now)).toBe("3 days ago")
  })
})

describe("theme", () => {
  const script = readFileSync("public/theme-init.js", "utf8")

  it("resolves every stored value the way public/theme-init.js does", () => {
    for (const value of [null, "light", "dark", "system", "bogus"]) {
      for (const systemDark of [false, true]) {
        window.localStorage.clear()
        if (value !== null) window.localStorage.setItem(themeKey, value)
        window.matchMedia = ((q: string) => ({ matches: systemDark, media: q })) as unknown as typeof window.matchMedia
        document.documentElement.className = ""
        new Function(script)()
        expect(document.documentElement.classList.contains("dark"), `${value} system dark ${systemDark}`).toBe(
          isDark(stored(), systemDark),
        )
      }
    }
  })

  it("follows the system preference while system is chosen", () => {
    let systemDark = false
    const changed = new Set<() => void>()
    window.matchMedia = ((q: string) => ({
      get matches() {
        return systemDark
      },
      media: q,
      addEventListener: (_: string, l: () => void) => changed.add(l),
      removeEventListener: (_: string, l: () => void) => changed.delete(l),
    })) as unknown as typeof window.matchMedia
    const os = (dark: boolean) => {
      systemDark = dark
      for (const l of changed) l()
    }
    const dark = () => document.documentElement.classList.contains("dark")
    const { unmount } = renderHook(() => useTheme())
    act(() => apply("system"))
    os(true)
    expect(dark()).toBe(true)
    os(false)
    expect(dark()).toBe(false)
    act(() => apply("dark"))
    os(false)
    expect(dark()).toBe(true)
    act(() => apply("light"))
    os(true)
    expect(dark()).toBe(false)
    act(() => apply("system"))
    expect(dark()).toBe(true)
    unmount()
    expect(changed.size).toBe(0)
  })

  it("falls back to system when storage throws", () => {
    expect(
      stored({
        getItem: () => {
          throw new Error("blocked")
        },
      }),
    ).toBe("system")
  })
})

describe("docs", () => {
  it("addresses a document without its extension and drops its leading h1", async () => {
    const { docAddress, withoutLeadingH1 } = await import("./docs")
    expect([docAddress("docs/setup.md"), docAddress("docs/a.b/c.markdown"), docAddress("LICENSE")]).toEqual([
      "docs/setup",
      "docs/a.b/c",
      "LICENSE",
    ])
    expect(withoutLeadingH1('<h1 id="doc-x">X <code>y</code></h1>\n<p>body</p><h1>Later</h1>')).toBe("<p>body</p><h1>Later</h1>")
    expect(withoutLeadingH1("<p>no title</p>")).toBe("<p>no title</p>")
  })
})
