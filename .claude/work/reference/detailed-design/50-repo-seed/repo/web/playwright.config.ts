import { defineConfig } from "@playwright/test"

const base = process.env["SCP_E2E_URL"] ?? "http://127.0.0.1:9090"
export default defineConfig({
  testDir: "./e2e",
  testMatch: /.*\.spec\.ts/,
  fullyParallel: true,
  forbidOnly: !!process.env["CI"],
  retries: process.env["CI"] ? 1 : 0,
  reporter: [["list"]],
  use: { baseURL: base, channel: process.env["PW_CHANNEL"] ?? undefined, trace: "retain-on-failure" },
  projects: [
    { name: "desktop", use: { viewport: { width: 1280, height: 800 } } },
    { name: "phone", use: { viewport: { width: 360, height: 740 } } },
  ],
})
