import type { Page } from "@playwright/test"
import { expect, test } from "./fixtures"

// cmd/scp/e2e_test.go: the fake provider signs in Ada Lovelace, an admin in staff; staff sees the private repository vault.
const token = "e2e-token-0123456789abcdef0123456789abcdef"

/** openAccount opens the account menu, pressing again while the page is not yet hydrated (a busy run renders it before its script runs). */
async function openAccount(page: Page) {
  const menu = page.getByRole("menu", { name: "Account" })
  await expect(async () => {
    if (!(await menu.isVisible())) await page.getByRole("button", { name: "Ada Lovelace" }).click()
    await expect(menu).toBeVisible({ timeout: 1000 })
  }).toPass()
}

test("signing in through the provider returns to the page and shows the private repositories", async ({ page }) => {
  await page.goto("/groups/other")
  await expect(page.getByRole("heading", { level: 1, name: "Other", exact: true })).toBeVisible()
  await expect(page.getByRole("link", { name: "vault", exact: true })).toHaveCount(0)
  await page.getByRole("link", { name: "Sign in", exact: true }).click()
  await expect(page).toHaveURL(/\/signin\?return_to=%2Fgroups%2Fother$/)
  await page.getByRole("link", { name: "Sign in with single sign-on" }).click()
  await expect(page).toHaveURL(/\/groups\/other$/)
  await expect(page.getByRole("button", { name: "Ada Lovelace" })).toBeVisible()
  await expect(page.getByRole("link", { name: "vault", exact: true })).toBeVisible()

  await openAccount(page)
  await page.getByRole("menu", { name: "Account" }).getByRole("menuitem", { name: "Admin" }).click()
  await expect(page.getByRole("heading", { level: 1, name: "Admin", exact: true })).toBeVisible()
  await expect(page.getByText("Signed in as Ada Lovelace through single sign-on, an administrator.")).toBeVisible()
  await expect(page.getByRole("heading", { name: "Snapshot" })).toBeVisible()

  await openAccount(page)
  await page.getByRole("menuitem", { name: "Sign out" }).click() // through the provider's end session back to the dashboard
  await expect(page.getByRole("heading", { level: 1, name: "Dashboard", exact: true })).toBeVisible()
  await page.goto("/groups/other")
  await expect(page.getByRole("link", { name: "Sign in", exact: true })).toBeVisible()
  await expect(page.getByRole("link", { name: "vault", exact: true })).toHaveCount(0)
})

test("a token signs in a person without the provider; a wrong one is explained", async ({ page }) => {
  await page.goto("/signin?return_to=/groups/other")
  await page.getByLabel("Token").fill("not-a-token")
  await page.getByRole("button", { name: "Sign in with the token" }).click()
  await expect(page.getByRole("alert").filter({ hasText: "That token" })).toHaveText("That token is not valid.")
  await page.goto("/signin?return_to=/groups/other")
  await page.getByLabel("Token").fill(token)
  await page.getByRole("button", { name: "Sign in with the token" }).click()
  await expect(page).toHaveURL(/\/groups\/other$/)
  await expect(page.getByRole("button", { name: "e2e" })).toBeVisible()
  await expect(page.getByRole("link", { name: "vault", exact: true })).toBeVisible()
  await page.getByRole("button", { name: "e2e" }).click()
  await expect(page.getByText("Signed in as e2e through a token.")).toBeVisible()
  await expect(page.getByRole("menuitem", { name: "Admin" })).toHaveCount(0)
})

test("the admin page asks an anonymous viewer to sign in", async ({ page }) => {
  await page.goto("/admin")
  await expect(page.getByText("Sign in as an administrator to see the portal's status.")).toBeVisible()
})

test("an API client reads with a bearer token; a wrong token is refused", async ({ request }) => {
  const ok = await request.get("/api/v1/repos", { headers: { Authorization: `Bearer ${token}` } })
  expect(ok.status()).toBe(200)
  expect(ok.headers()["cache-control"]).toMatch(/^private/)
  expect(JSON.stringify(await ok.json())).toContain("acme/vault")
  const bad = await request.get("/api/v1/repos", { headers: { Authorization: "Bearer wrong" } })
  expect(bad.status()).toBe(401)
  expect((await bad.json()).code).toBe("unauthenticated")
})

test("a signed-in viewer asks the assistant; its answer streams with what it read", async ({ page }) => {
  // cmd/scp/e2e_test.go: the fake model reads the repository named by the question's last word and answers with what it read.
  await page.goto("/ask")
  await expect(page.getByRole("link", { name: "Ask", exact: true })).toHaveCount(0)
  await page.getByRole("link", { name: "Sign in", exact: true }).first().click()
  await page.getByLabel("Token").fill(token)
  await page.getByRole("button", { name: "Sign in with the token" }).click()
  await expect(page).toHaveURL(/\/ask$/)
  await expect(page.getByRole("link", { name: "Ask", exact: true })).toHaveAttribute("aria-current", "page")
  await page.getByLabel("Your question").fill("Tell me about vault")
  await page.getByRole("button", { name: "Ask" }).click()
  await expect(page.getByText("Reading acme/vault")).toBeVisible()
  await expect(page.getByRole("heading", { name: "acme/vault" })).toBeVisible()
  await expect(page.getByRole("button", { name: "New conversation" })).toBeVisible()
})

// @writes: the layouts are the server's, so the design mode runs after the readers, one test at a time (playwright.config.ts).
test(
  "an admin designs the dashboard in the design mode and every viewer gets it",
  { tag: "@writes" },
  async ({ page, browser }) => {
    await page.goto("/signin")
    await page.getByRole("link", { name: "Sign in with single sign-on" }).click()
    await expect(page.getByRole("button", { name: "Ada Lovelace" })).toBeVisible()
    await page.goto("/")
    await page.getByRole("button", { name: "Design this page" }).click()
    const list = page.getByRole("region", { name: "Elements" })
    await list.getByRole("combobox", { name: "New element" }).selectOption("heading")
    await list.getByRole("button", { name: "Add" }).click()
    await list.getByRole("textbox", { name: "Heading" }).fill("Designed in the e2e")
    await list.getByRole("button", { name: "Move Heading up" }).click()
    await expect(
      page.getByRole("region", { name: "Preview" }).getByRole("heading", { name: "Designed in the e2e" }),
    ).toBeVisible()
    await page.getByRole("button", { name: "Save" }).click()
    await expect(page.getByText("Saved: every viewer sees this layout")).toBeVisible()

    const anonymous = await browser.newPage()
    await anonymous.goto(page.url())
    await expect(anonymous.getByRole("heading", { name: "Designed in the e2e" })).toBeVisible()
    await expect(anonymous.getByRole("button", { name: "Design this page" })).toHaveCount(0)
    await anonymous.close()

    await page.getByRole("button", { name: "Back to the built-in layout" }).click()
    await expect(page.getByText("The built-in layout, for every viewer")).toBeVisible()
    await page.getByRole("button", { name: "Done" }).click()
    await expect(page.getByRole("heading", { name: "Designed in the e2e" })).toHaveCount(0)
  },
)

test("an admin designs the presentation page from its bar", { tag: "@writes" }, async ({ page }) => {
  await page.goto("/signin")
  await page.getByRole("link", { name: "Sign in with single sign-on" }).click()
  await expect(page.getByRole("button", { name: "Ada Lovelace" })).toBeVisible()
  await page.goto("/present")
  await page.mouse.click(10, 10)
  await page.getByRole("navigation", { name: "Presentation" }).getByRole("button", { name: "Design this page" }).click()
  const list = page.getByRole("region", { name: "Elements" })
  await list.getByRole("combobox", { name: "New element" }).selectOption("heading")
  await list.getByRole("button", { name: "Add" }).click()
  await list.getByRole("textbox", { name: "Heading" }).fill("On the wall")
  await expect(page.getByRole("region", { name: "Preview" }).getByRole("heading", { name: "On the wall" })).toBeVisible()
  await page.getByRole("button", { name: "Save" }).click()
  await expect(page.getByText("Saved: every viewer sees this layout")).toBeVisible()
  await page.getByRole("button", { name: "Back to the built-in layout" }).click()
  await expect(page.getByText("The built-in layout, for every viewer")).toBeVisible()
  await page.getByRole("button", { name: "Done" }).click()
  await expect(page.getByRole("region", { name: "Latest commits" })).toBeVisible()
})
