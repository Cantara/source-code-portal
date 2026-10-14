import src from "../../public/theme-init.js?raw"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { STORAGE } from "./storage-keys"
import { apply, applyServerDefault, getPreference, setPreference } from "./theme"

function matchMedia(dark: boolean) {
  vi.stubGlobal("matchMedia", (q: string) => ({ matches: q.includes("dark") && dark, addEventListener() {}, removeEventListener() {} }))
}
beforeEach(() => {
  document.documentElement.className = ""
  document.documentElement.removeAttribute("style")
  matchMedia(false)
})

describe("theme", () => {
  it("defaults to system and follows prefers-color-scheme", () => {
    matchMedia(true)
    apply()
    expect(document.documentElement.classList.contains("dark")).toBe(true)
    expect(document.documentElement.style.colorScheme).toBe("dark")
  })
  it("a stored choice wins over the server default and the OS", () => {
    setPreference("light")
    matchMedia(true)
    applyServerDefault("dark")
    expect(getPreference()).toBe("light")
    expect(document.documentElement.classList.contains("dark")).toBe(false)
  })
  it("the server default applies only without a stored choice and is remembered for the next pre-paint", () => {
    applyServerDefault("dark")
    expect(window.localStorage.getItem(STORAGE.themeDefault)).toBe("dark")
    expect(document.documentElement.classList.contains("dark")).toBe(true)
  })
  it("survives blocked storage", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked")
    })
    expect(getPreference()).toBe("system")
  })
})

describe("public/theme-init.js", () => {
  it("uses the same storage keys as the app", () => {
    expect(src).toContain(`'${STORAGE.theme}'`)
    expect(src).toContain(`'${STORAGE.themeDefault}'`)
  })
  it("is CSP-clean: no eval, no inline-style string writes, no network", () => {
    expect(src).not.toMatch(/eval\(|new Function|innerHTML|setAttribute\(\s*['"]style|cssText|fetch\(|XMLHttpRequest/)
  })
  it("agrees with theme.ts for every combination of stored choice, server default and OS", () => {
    for (const stored of [null, "light", "dark", "system"])
      for (const def of [null, "light", "dark", "system"])
        for (const os of [false, true]) {
          window.localStorage.clear()
          if (stored) window.localStorage.setItem(STORAGE.theme, stored)
          if (def) window.localStorage.setItem(STORAGE.themeDefault, def)
          matchMedia(os)
          document.documentElement.className = ""
          // The script is evaluated on purpose: this test proves it makes the same decision as theme.ts.
          // eslint-disable-next-line @typescript-eslint/no-implied-eval, @typescript-eslint/no-unsafe-call, no-new-func
          new Function(src)()
          const fromScript = document.documentElement.classList.contains("dark")
          document.documentElement.className = ""
          apply()
          expect(fromScript, `stored=${stored} default=${def} os=${os}`).toBe(document.documentElement.classList.contains("dark"))
        }
  })
})
