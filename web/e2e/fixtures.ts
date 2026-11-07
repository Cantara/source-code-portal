import { test as base, expect, type Page, type Response } from "@playwright/test"

/**
 * hydrated waits until the page's script runs (the root layout marks <html data-hydrated>): before that, a click on a control that
 * needs it is lost.
 */
export async function hydrated(page: Page) {
  await page.locator("html[data-hydrated]").waitFor({ state: "attached" })
}

type Navigation = (...args: never[]) => Promise<Response | null>

/**
 * settled makes page's goto, reload and goBack return once a page they load answers to clicks, so no test acts on a page whose script
 * has not run. A page from browser.newPage() is passed through it too; one without JavaScript is left alone.
 */
export function settled(page: Page): Page {
  for (const name of ["goto", "reload", "goBack"] as const) {
    const navigate = (page[name] as Navigation).bind(page)
    Object.assign(page, {
      [name]: async (...args: never[]) => {
        const res = await navigate(...args)
        if (res?.headers()["content-type"]?.startsWith("text/html")) await hydrated(page)
        return res
      },
    })
  }
  return page
}

/**
 * test fails on any CSP violation, uncaught page error or console error: the CSP check, which also catches a dependency that starts
 * injecting <style> or inline script.
 */
export const test = base.extend<{ guard: undefined }>({
  // A reader who has not chosen has the filter panel folded on a phone; the tests that use a list's filters have it open.
  context: async ({ context, baseURL }, provide) => {
    if (baseURL) await context.addCookies([{ name: "scp-filters", value: "open", url: baseURL }])
    await provide(context)
  },
  page: async ({ page, javaScriptEnabled }, provide) => {
    await provide(javaScriptEnabled === false ? page : settled(page))
  },
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
