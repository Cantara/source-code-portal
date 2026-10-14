import { resolve } from "node:path"
import { defineConfig } from "vite"
import { appPlugins } from "./vite.plugins.ts"

const buildId = process.env["BUILD_ID"] ?? "dev"
if (process.env["RELEASE"] === "1" && buildId === "dev")
  throw new Error("RELEASE=1 needs BUILD_ID (the git SHA also passed to go build -ldflags)")

// Dev server only: the Go binary to proxy to (make dev runs it on :9090 against cmd/fakegithub).
const api = process.env["SCP_DEV_API"] ?? "http://127.0.0.1:9090"
// Prefixes first; the last entry is a regular expression (Vite treats a key starting with ^ as one; verified against a running stand-in): server-answered files that live
// under SPA-looking prefixes (/repos/{repo}/readme.md, /groups/{id}/llms.txt, /repos/{repo}/docs/x.md) would otherwise get Vite's index.html.
const PROXIED = [
  "/api",
  "/badges",
  "/media",
  "/schemas",
  "/llms.txt",
  "/llms-full.txt",
  "/sitemap.xml",
  "/robots.txt",
  "/csp-reports",
  "^/(repos|groups)/.+\\.(md|txt)$",
]

export default defineConfig({
  plugins: appPlugins({ router: true, css: true }),
  resolve: { alias: { "@": resolve(import.meta.dirname, "./src") } },
  define: { __BUILD_ID__: JSON.stringify(buildId) },
  build: {
    target: "baseline-widely-available",
    sourcemap: false,
    assetsInlineLimit: 0, // no data: URIs: img-src/font-src stay 'self'
    cssCodeSplit: true,
    manifest: true,
    // hex hashes: [0-9a-f]{8} never contain "-" or "_", so "the last dash starts the hash" holds for webapp's hashedName rule
    rolldownOptions: { output: { hashCharacters: "hex" } },
  },
  server: { host: "127.0.0.1", port: 5173, strictPort: true, proxy: Object.fromEntries(PROXIED.map((p) => [p, api])) },
})
