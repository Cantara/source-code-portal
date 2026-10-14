/// <reference types="vitest/config" />
import { fileURLToPath } from "node:url"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// The Go server (`scp serve`, :9090) serves the API in development; Vite proxies to it and serves the UI with hot reload.
const backend = "http://localhost:9090"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: {
    proxy: {
      "/api": backend,
      "/badges": backend,
      "/llms.txt": backend,
      "/llms-full.txt": backend,
    },
  },
  build: { outDir: "dist", assetsDir: "assets", sourcemap: false },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
})
