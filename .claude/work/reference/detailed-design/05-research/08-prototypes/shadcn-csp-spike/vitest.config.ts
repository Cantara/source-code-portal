import { defineConfig } from "vitest/config"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import { resolve } from "node:path"
import { playwright } from "@vitest/browser-playwright"
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": resolve(import.meta.dirname, "./src") } },
  test: {
    projects: [
      { extends: true, test: { name: "unit", environment: "jsdom", include: ["src/**/*.test.{ts,tsx}"], exclude: ["src/**/*.browser.test.tsx"] } },
      { extends: true, test: { name: "browser", include: ["src/**/*.browser.test.tsx"], browser: { enabled: true, headless: true, provider: playwright({ launchOptions: { channel: "chrome" } }), instances: [{ browser: "chromium" }] } } },
    ],
  },
})
