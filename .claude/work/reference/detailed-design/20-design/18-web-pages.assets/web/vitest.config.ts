import { defineConfig } from "vitest/config"
import { resolve } from "node:path"
export default defineConfig({
  define: { __BUILD_ID__: JSON.stringify("test") },
  resolve: { alias: { "@": resolve(import.meta.dirname, "src") } },
  test: { environment: "node", include: ["src/**/*.test.{ts,tsx}"] },
})
