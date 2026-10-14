/// <reference types="vitest/config" />
import { fileURLToPath } from "node:url"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// The Go server (`scp serve`, :9090) serves the API in development; Vite proxies to it and serves the UI with hot reload.
const backend = process.env.SCP_DEV_API ?? "http://127.0.0.1:9090"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  define: { "import.meta.env.VITE_BUILD_ID": JSON.stringify(process.env.BUILD_ID ?? "dev") },
  server: {
    host: "127.0.0.1",
    proxy: {
      "/api": backend,
      "/badges": backend,
      "/llms.txt": backend,
      "/llms-full.txt": backend,
      "^/(repos|groups)/.+\\.(md|txt)$": backend,
    },
  },
  build: { outDir: "dist", assetsDir: "assets", sourcemap: false, assetsInlineLimit: 0 },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
})
