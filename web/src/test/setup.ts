import "@testing-library/jest-dom/vitest"
import { cleanup } from "@testing-library/react"
import { afterAll, afterEach, beforeAll, vi } from "vitest"
import { rules, server } from "./server"

// Components read the rules the root layout passes down; a test renders them without it, so they read the server's (rules.json).
vi.mock("@/lib/rules-context", async (actual) => ({
  ...(await actual<typeof import("@/lib/rules-context")>()),
  useRules: () => rules,
}))

// jsdom has no layout; the router calls scrollTo on navigation.
window.scrollTo = () => {}
window.matchMedia ??= ((query: string) => ({
  matches: false,
  media: query,
  onchange: null,
  addListener: () => {},
  removeListener: () => {},
  addEventListener: () => {},
  removeEventListener: () => {},
  dispatchEvent: () => false,
})) as typeof window.matchMedia

beforeAll(() => server.listen({ onUnhandledFrame: "error" }))
afterEach(() => {
  cleanup()
  server.resetHandlers()
})
afterAll(() => server.close())

// jsdom lays nothing out, so it has no scrolling.
Element.prototype.scrollIntoView ??= () => {}
