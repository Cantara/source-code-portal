// Excerpt of web/vite.config.ts (design 16 section 4.11): the dev server on :5173 forwards everything that is not the SPA to the
// portal on :9090, so the browser sees one origin and the SPA code is identical in development and in production.
// The list is the reserved-prefix list of the route table (routes.Table.ReservedPrefixes) plus the server-only text routes.
import { defineConfig } from "vite";

const portal = process.env.SCP_DEV_TARGET ?? "http://127.0.0.1:9090";
const forward = Object.fromEntries(
  ["/api", "/badges", "/media", "/schemas", "/webhooks", "/.well-known", "/llms.txt", "/llms-full.txt", "/sitemap.xml", "/robots.txt", "/livez", "/readyz"].map((p) => [
    p,
    { target: portal, changeOrigin: false, xfwd: true },
  ]),
);

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: forward, // SSE (/api/v1/events, v1.1) passes through; no response buffering in http-proxy
  },
});
