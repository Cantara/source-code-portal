import { resolve } from "node:path"
import { defineConfig } from "vitest/config"
import { playwright } from "@vitest/browser-playwright"
import { appPlugins } from "./vite.plugins.ts"

const root = {
  plugins: appPlugins({ router: false, css: true }),
  resolve: { alias: { "@": resolve(import.meta.dirname, "./src") } },
  define: { __BUILD_ID__: JSON.stringify("test-build") },
}
export default defineConfig({
  ...root,
  test: {
    projects: [
      {
        extends: true,
        test: {
          name: "unit",
          environment: "jsdom",
          include: ["src/**/*.test.{ts,tsx}"],
          exclude: ["src/**/*.browser.test.tsx"],
          setupFiles: ["src/test/setup.ts"],
          css: false,
        },
      },
      // build tooling and lint-rule tests: plain Node, no DOM, no MSW
      { extends: false, test: { name: "tooling", environment: "node", include: ["scripts/**/*.test.ts"] } },
      {
        extends: true,
        test: {
          name: "browser",
          include: ["src/**/*.browser.test.tsx"],
          setupFiles: ["src/test/setup-browser.ts"],
          browser: {
            enabled: true,
            headless: true,
            provider: playwright({ launchOptions: { channel: process.env["PW_CHANNEL"] ?? undefined } }),
            instances: [{ browser: "chromium" }],
          },
        },
      },
    ],
  },
})
