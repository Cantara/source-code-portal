import type { NextConfig } from "next"

// The Go server (`scp serve`) answers everything but the pages. In production it runs this app as its child and proxies the pages to
// it; under `next dev` the app proxies the Go addresses instead, so the UI reloads in place against a running `scp serve`.
const api = process.env.SCP_API ?? "http://127.0.0.1:9090"
const build = process.env.BUILD_ID ?? "dev"

const config: NextConfig = {
  output: "standalone",
  poweredByHeader: false,
  agentRules: false,
  reactStrictMode: true,
  deploymentId: build === "dev" ? undefined : build,
  env: { NEXT_PUBLIC_BUILD_ID: build },
  images: { unoptimized: true },
  outputFileTracingExcludes: { "*": ["**/node_modules/sharp/**", "**/node_modules/@img/**"] }, // image optimisation is off: no native code
  async rewrites() {
    if (process.env.NODE_ENV !== "development") return []
    return [
      { source: "/api/:path*", destination: `${api}/api/:path*` },
      { source: "/badges/:path*", destination: `${api}/badges/:path*` },
      { source: "/images/:path*", destination: `${api}/images/:path*` },
      { source: "/llms.txt", destination: `${api}/llms.txt` },
      { source: "/llms-full.txt", destination: `${api}/llms-full.txt` },
      { source: "/repos/:owner/:name/:path(.*\\.md)", destination: `${api}/repos/:owner/:name/:path` },
    ]
  },
}

export default config
