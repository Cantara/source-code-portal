import { expect, test } from "./fixtures"

// cmd/scp/e2e_test.go: the fake provider signs in Ada Lovelace, an admin in staff; staff sees the private repository vault.
const token = "e2e-token-0123456789abcdef0123456789abcdef"

test("signing in through the provider returns to the page and shows the private repositories", async ({ page }) => {
  await page.goto("/groups/other")
  await expect(page.getByRole("heading", { level: 1, name: "Other", exact: true })).toBeVisible()
  await expect(page.getByRole("link", { name: "acme/vault" })).toHaveCount(0)
  await page.getByRole("link", { name: "Sign in", exact: true }).click()
  await expect(page).toHaveURL(/\/signin\?return_to=%2Fgroups%2Fother$/)
  await page.getByRole("link", { name: "Sign in with single sign-on" }).click()
  await expect(page).toHaveURL(/\/groups\/other$/)
  await expect(page.getByText("Ada Lovelace", { exact: true })).toBeVisible()
  await expect(page.getByRole("link", { name: "acme/vault" })).toBeVisible()

  await page.getByRole("link", { name: "Admin", exact: true }).click()
  await expect(page.getByRole("heading", { level: 1, name: "Admin", exact: true })).toBeVisible()
  await expect(page.getByText("Signed in as Ada Lovelace through single sign-on, an administrator.")).toBeVisible()
  await expect(page.getByRole("heading", { name: "Snapshot" })).toBeVisible()

  await page.getByRole("button", { name: "Sign out" }).click() // through the provider's end session back to the dashboard
  await expect(page.getByRole("heading", { level: 1, name: "Dashboard", exact: true })).toBeVisible()
  await page.goto("/groups/other")
  await expect(page.getByRole("link", { name: "Sign in", exact: true })).toBeVisible()
  await expect(page.getByRole("link", { name: "acme/vault" })).toHaveCount(0)
})

test("a token signs in a person without the provider; a wrong one is explained", async ({ page }) => {
  await page.goto("/signin?return_to=/groups/other")
  await page.getByLabel("Token").fill("not-a-token")
  await page.getByRole("button", { name: "Sign in with the token" }).click()
  await expect(page.getByRole("alert")).toHaveText("That token is not valid.")
  await page.goto("/signin?return_to=/groups/other")
  await page.getByLabel("Token").fill(token)
  await page.getByRole("button", { name: "Sign in with the token" }).click()
  await expect(page).toHaveURL(/\/groups\/other$/)
  await expect(page.getByText("e2e", { exact: true })).toBeVisible()
  await expect(page.getByRole("link", { name: "acme/vault" })).toBeVisible()
  await expect(page.getByRole("link", { name: "Admin", exact: true })).toHaveCount(0)
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
