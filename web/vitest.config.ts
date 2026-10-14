import { fileURLToPath } from "node:url"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vitest/config"

const src = (p: string) => fileURLToPath(new URL(`./src/${p}`, import.meta.url))

// Component and route tests in jsdom; next/navigation and next/link are replaced by small stand-ins (src/test/navigation.ts,
// link.tsx), and server-only by an empty module so route modules load.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "next/navigation": src("test/navigation.ts"),
      "next/link": src("test/link.tsx"),
      "server-only": src("test/server-only.ts"),
      "@": src(""),
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
})
