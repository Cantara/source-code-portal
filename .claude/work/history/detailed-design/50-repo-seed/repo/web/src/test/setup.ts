import "@testing-library/jest-dom/vitest"
import { cleanup } from "@testing-library/react"
import { afterEach } from "vitest"
import { initI18n } from "@/lib/i18n"

initI18n()
window.scrollTo = () => undefined // jsdom does not implement it; the router restores scroll positions
afterEach(() => {
  cleanup()
  window.localStorage.clear()
})
// Design 17 adds the MSW server here (server.listen({ onUnhandledFrame: "error" }) in beforeAll, resetHandlers in afterEach) once lib/api exists.
