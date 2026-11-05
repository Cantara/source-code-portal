import { expect, test } from "./fixtures"
import { screens } from "./screens"

// cmd/scp/e2e_test.go configures two announcements: an event in six days on both pages for everyone, with a link, and a notice on
// the dashboard for signed-in viewers alone. The fake provider signs in Ada Lovelace, an administrator.
const title = "Juleølsmaking på Rebel Makerspace"

test("the dashboard shows the event above the fleet, keeps the signed-in notice from the public, and keeps a dismissal", async ({
  page,
}) => {
  await page.goto("/")
  const region = page.getByRole("region", { name: "Announcements" })
  await expect(region.getByRole("heading", { name: title })).toBeVisible()
  await expect(region.getByRole("heading", { name: title })).toHaveAttribute("lang", "nb")
  await expect(region).toContainText(/in [56] days/) // six days from the server's start, read a moment later
  await expect(region.getByRole("link", { name: /Meld deg på her/ })).toHaveAttribute("href", "https://example.org/pamelding")
  await expect(page.getByText("The vault moves to the new cluster")).toHaveCount(0)
  await region.getByRole("button", { name: `Dismiss “${title}”` }).click()
  await expect(page.getByRole("region", { name: "Announcements" })).toHaveCount(0)
  await page.reload()
  await expect(page.getByRole("region", { name: "Fleet" })).toBeVisible()
  await expect(page.getByRole("region", { name: "Announcements" })).toHaveCount(0)
})

test("the presentation page shows the event, never dismissible, with the link's QR code on a desk", async ({ page }) => {
  const { width, height } = screens.xdrFull
  await page.setViewportSize({ width, height })
  await page.goto("/present")
  const region = page.getByRole("region", { name: "Announcements" })
  await expect(region.getByRole("heading", { name: title })).toBeVisible()
  await expect(region.getByRole("img", { name: "QR code for Meld deg på her" })).toBeVisible()
  await expect(region.getByRole("button", { name: /Dismiss/ })).toHaveCount(0)
})

test("an admin writes, previews, publishes and removes an announcement", async ({ page, browser }, info) => {
  test.skip(info.project.name !== "desktop", "one announcements file serves both projects")
  await page.goto("/signin")
  await page.getByRole("link", { name: "Sign in with single sign-on" }).click()
  await expect(page.getByRole("button", { name: "Ada Lovelace" })).toBeVisible()
  await page.goto("/")
  await expect(page.getByRole("region", { name: "Announcements" }).getByText("The vault moves to the new cluster")).toBeVisible()

  await page.goto("/admin")
  const card = page.getByRole("region", { name: "Announcements" })
  await expect(card.getByText(title)).toBeVisible()
  await expect(card.getByText("from the configuration").first()).toBeVisible()
  await card.getByRole("button", { name: "New announcement" }).click()
  const form = page.getByRole("region", { name: "New announcement" })
  await form.getByLabel("Title", { exact: true }).fill("Hackathon on Friday")
  await form.getByLabel("Text", { exact: true }).fill("Bring a project.")
  await form.getByLabel("Tone", { exact: true }).selectOption("info")
  await form.getByLabel("Shown until", { exact: true }).fill("2099-01-01T12:00")
  await form.getByRole("checkbox", { name: "Present (/present)" }).uncheck()
  await expect(form.getByRole("heading", { name: "Hackathon on Friday" })).toBeVisible() // the preview
  await expect(form.getByRole("button", { name: "Save" })).toBeEnabled()
  await form.getByRole("checkbox", { name: /Published/ }).check()
  await form.getByRole("button", { name: "Save" }).click()
  await expect(card.getByRole("listitem").filter({ hasText: "Hackathon on Friday" })).toContainText("Live")

  const anonymous = await browser.newPage()
  await anonymous.goto("/")
  await expect(
    anonymous.getByRole("region", { name: "Announcements" }).getByRole("heading", { name: "Hackathon on Friday" }),
  ).toBeVisible()
  await anonymous.goto("/present")
  await expect(anonymous.getByRole("region", { name: "Announcements" }).getByRole("heading", { name: title })).toBeVisible()
  await expect(anonymous.getByRole("heading", { name: "Hackathon on Friday" })).toHaveCount(0)
  await anonymous.close()

  page.once("dialog", (d) => void d.accept())
  await card.getByRole("button", { name: "Remove Hackathon on Friday" }).click()
  await expect(card.getByText("Hackathon on Friday")).toHaveCount(0)
})
