import { defineConfig, devices } from "@playwright/test"

// The suite runs against the real server with the SPA embedded (-tags spa, so `pnpm build` first) and a fake GitHub:
// cmd/scp/e2e_test.go serves both on E2E_SCP_ADDR until Playwright stops it.
const addr = "127.0.0.1:9190"

export default defineConfig({
  testDir: "e2e",
  globalSetup: "./e2e/global-setup.ts",
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: { baseURL: `http://${addr}`, trace: "retain-on-failure" },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"], viewport: { width: 1280, height: 800 } } },
    {
      name: "phone",
      use: { ...devices["Desktop Chrome"], viewport: { width: 360, height: 740 }, isMobile: true, hasTouch: true },
    },
  ],
  webServer: {
    command: "go test -tags spa -count=1 -timeout 0 -run '^TestServeForPlaywright$' ./cmd/scp",
    cwd: "..",
    env: { E2E_SCP_ADDR: addr },
    url: `http://${addr}/api/v1/snapshot`,
    timeout: 180_000,
    reuseExistingServer: !process.env.CI,
    stdout: "pipe",
  },
})
