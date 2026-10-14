import { test as base, expect } from "@playwright/test"

type Violation = { directive: string; blocked: string; source: string }
/** Every test fails on any CSP violation, console error or page error (the CSP gate of ADR-010 / risk W11). */
export const test = base.extend<{ gate: void; allowConsole: RegExp[] }>({
  // Tests that deliberately trigger an error boundary list the console messages they expect.
  allowConsole: [[], { option: true }],
  gate: [
    async ({ page, allowConsole }, use) => {
      const problems: string[] = []
      await page.addInitScript(() => {
        ;(window as unknown as { __csp: Violation[] }).__csp = []
        document.addEventListener("securitypolicyviolation", (e) => {
          ;(window as unknown as { __csp: Violation[] }).__csp.push({
            directive: e.effectiveDirective,
            blocked: e.blockedURI,
            source: e.sourceFile,
          })
        })
      })
      page.on("console", (m) => {
        if (m.type() === "error" && !m.text().includes("Failed to load resource") && !allowConsole.some((r) => r.test(m.text())))
          problems.push(`console.error: ${m.text()}`)
      })
      page.on("pageerror", (e) => problems.push(`pageerror: ${e.message}`))
      await use()
      const csp = await page.evaluate(() => (window as unknown as { __csp?: Violation[] }).__csp ?? []).catch(() => [])
      for (const v of csp) problems.push(`csp violation: ${v.directive} ${v.blocked} ${v.source}`)
      expect(problems, "no CSP violations, console errors or page errors").toEqual([])
    },
    { auto: true },
  ],
})
export { expect }
