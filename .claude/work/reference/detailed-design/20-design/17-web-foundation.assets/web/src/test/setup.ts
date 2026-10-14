import "@testing-library/jest-dom/vitest"
import { cleanup } from "@testing-library/react"
import { afterAll, afterEach, beforeAll } from "vitest"
import { initI18n } from "@/lib/i18n"
import { serverStatus } from "@/lib/server-status"
import { server } from "./server"

initI18n()
beforeAll(() => server.listen({ onUnhandledFrame: "error" })) // MSW 3 name (v2: onUnhandledRequest)
afterEach(() => {
  cleanup()
  server.resetHandlers()
  serverStatus.reset()
  window.localStorage.clear()
})
afterAll(() => server.close())
