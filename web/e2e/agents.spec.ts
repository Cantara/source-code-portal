import type { Page } from "@playwright/test"
import { expect, test } from "./fixtures"

// WebMCP (design/agents.md, W3) with a draft `document.modelContext` installed before the portal loads: the tools it registers, run
// as a browser agent runs them. cmd/scp/e2e_test.go: the token signs in a member of staff, who sees the private repository vault.
const token = "e2e-token-0123456789abcdef0123456789abcdef"

declare global {
  interface Window {
    agentTools: Map<string, { execute: (input: unknown) => Promise<unknown>; annotations?: Record<string, boolean> }>
  }
}

async function withModelContext(page: Page) {
  await page.addInitScript(() => {
    window.agentTools = new Map()
    Object.defineProperty(document, "modelContext", {
      value: {
        registerTool(tool: { name: string; execute: (input: unknown) => Promise<unknown> }, o?: { signal?: AbortSignal }) {
          if (window.agentTools.has(tool.name)) return Promise.reject(new DOMException("taken", "InvalidStateError"))
          window.agentTools.set(tool.name, tool)
          o?.signal?.addEventListener("abort", () => window.agentTools.delete(tool.name))
          return Promise.resolve()
        },
      },
    })
  })
}

const names = (page: Page) => page.evaluate(() => [...window.agentTools.keys()].sort())
const run = (page: Page, name: string, input: unknown) =>
  page.evaluate(([n, i]) => window.agentTools.get(n as string)?.execute(i), [name, input] as const) as Promise<
    Record<string, unknown>
  >

test("the pages offer a browser's agent the portal's tools, which answer from the reader's view", async ({ page }) => {
  await withModelContext(page)
  await page.goto("/repos/acme/whydah-core")
  await expect
    .poll(() => names(page))
    .toEqual([
      "current_page",
      "get_document",
      "get_group",
      "get_repository",
      "list_groups",
      "open_page",
      "recent_commits",
      "repository_health",
      "repository_packages",
      "search_repositories",
      "set_preferences",
    ])
  expect(await page.evaluate(() => window.agentTools.get("repository_health")?.annotations)).toEqual({
    readOnlyHint: true,
    untrustedContentHint: true,
  })

  expect(await run(page, "current_page", {})).toMatchObject({ kind: "repository", owner: "acme", name: "whydah-core" })
  const health = await run(page, "repository_health", { owner: "acme", name: "whydah-core" })
  expect(health.isError).toBe(false)
  expect(health.text).toMatch(/^<untrusted-content id="[0-9a-f]+" source="\/repos\/acme\/whydah-core">/)
  expect(health.text).toContain("Health")
  expect(health.source).toMatch(/\/repos\/acme\/whydah-core$/)

  // The public's agent finds no trace of the private repository.
  const hidden = await run(page, "search_repositories", { query: "vault" })
  expect(hidden.text).toContain("0 repositories match")
  expect(await run(page, "repository_health", { owner: "acme", name: "vault" })).toMatchObject({ isError: true })

  // The page's own tools: the reader's preferences and going to a page of the portal, never another site.
  expect(await run(page, "set_preferences", { work: ["hand"], products: "table" })).toEqual({
    set: { work: ["hand"], products: "table" },
  })
  expect((await page.context().cookies()).find((c) => c.name === "scp-work")?.value).toBe("hand")
  expect(await run(page, "open_page", { path: "https://evil.example/" })).toMatchObject({ isError: true })
  await run(page, "open_page", { path: "/groups/whydah" })
  await expect(page).toHaveURL(/\/groups\/whydah$/)
  await expect(page.getByRole("heading", { level: 1, name: "Whydah", exact: true })).toBeVisible()
  await expect.poll(() => run(page, "current_page", {})).toMatchObject({ kind: "group", group: "whydah" })
})

test("a signed-in reader's agent sees what the reader sees", async ({ page }) => {
  await withModelContext(page)
  await page.goto("/signin")
  await page.getByLabel("Token").fill(token)
  await page.getByRole("button", { name: "Sign in with the token" }).click()
  await expect(page).not.toHaveURL(/\/signin/)
  await expect.poll(() => names(page)).toContain("search_repositories")
  const found = await run(page, "search_repositories", { query: "vault" })
  expect(found.text).toContain("acme/vault")
})

test("without WebMCP the pages register nothing and ask for no tools", async ({ page }) => {
  const asked: string[] = []
  page.on("request", (r) => {
    if (r.url().includes("/api/v1/tools")) asked.push(r.url())
  })
  await page.goto("/")
  await expect(page.getByRole("heading", { level: 1, name: "Dashboard", exact: true })).toBeVisible()
  expect(await page.evaluate(() => "modelContext" in document)).toBe(false)
  expect(asked).toEqual([])
})
