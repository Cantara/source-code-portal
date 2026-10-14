import { test as base, expect } from "@playwright/test"

/**
 * test fails on any CSP violation, uncaught page error or console error: the CSP check, which also catches a dependency that starts
 * injecting <style> or inline script.
 */
export const test = base.extend<{ guard: undefined }>({
  guard: [
    async ({ page }, use) => {
      const problems: string[] = []
      await page.addInitScript(() => {
        document.addEventListener("securitypolicyviolation", (e) => {
          console.error(`CSP violation: ${e.violatedDirective} ${e.blockedURI}`)
        })
      })
      page.on("pageerror", (e) => problems.push(`page error: ${e.message}`))
      page.on("console", (m) => {
        if (m.type() === "error" && !/status of 404/.test(m.text())) problems.push(`console: ${m.text()}`)
      })
      await use(undefined)
      expect(problems).toEqual([])
    },
    { auto: true },
  ],
})

export { expect }
