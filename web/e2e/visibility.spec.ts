import AxeBuilder from "@axe-core/playwright"
import { expect, settled, test } from "./fixtures"

// cmd/scp/e2e_test.go: the fake provider signs in Ada Lovelace, an administrator; acme/tool is a public repository of the group Other.

// @writes: who sees what is the server's, so this runs after the readers, one test at a time (playwright.config.ts).
test(
  "an admin shows a repository to signed-in viewers only, reviews it, and undoes it; every section of the admin page passes axe in both themes",
  { tag: "@writes" },
  async ({ page, browser }) => {
    await page.goto("/signin")
    await page.getByRole("link", { name: "Sign in with single sign-on" }).click()
    await expect(page.getByRole("button", { name: "Ada Lovelace" })).toBeVisible()
    await page.goto("/admin")
    const nav = page.getByRole("navigation", { name: "Admin sections" })
    await expect(page.getByRole("heading", { name: "Snapshot" })).toBeVisible()
    // Every section passes axe in both themes; the side menu leads to each and marks the one shown.
    for (const [link, heading] of [
      ["Overview", "Snapshot"],
      ["GitHub", "Sources"],
      ["Announcements", "Announcements"],
      ["Assistant", "Assistant"],
      ["Who sees what", "Who sees what"],
    ] as const) {
      await nav.getByRole("link", { name: new RegExp(`^${link}`) }).click()
      await expect(nav.getByRole("link", { name: new RegExp(`^${link}`) })).toHaveAttribute("aria-current", "page")
      await expect(page.getByRole("heading", { name: heading, exact: true })).toBeVisible()
      for (const scheme of ["light", "dark"] as const) {
        await page.emulateMedia({ colorScheme: scheme })
        const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze()
        expect(result.violations.map((v) => `${link} ${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
      }
    }
    await expect(page).toHaveURL(/\/admin#visibility$/)
    const card = page.getByRole("region", { name: "Who sees what" })
    const tool = card.getByRole("radiogroup", { name: "Who sees acme/tool" })
    await expect(tool.getByRole("radio", { name: "Everyone" })).toHaveAttribute("aria-checked", "true")
    await expect(
      card.getByRole("radiogroup", { name: "Who sees acme/vault" }).getByRole("radio", { name: "Everyone" }),
    ).toBeDisabled()

    await tool.getByRole("radio", { name: "Signed in" }).click()
    await expect(card.getByText("changed", { exact: true })).toBeVisible()
    await card.getByRole("button", { name: "Review and save" }).click()
    const review = page.getByRole("dialog", { name: "Review 1 change" })
    await expect(review.getByText("Leave the public view · 1")).toBeVisible()
    await review.getByRole("button", { name: "Save 1 change" }).click()
    await expect(card.getByText("Saved 1 change.")).toBeVisible()

    const anonymous = settled(await browser.newPage({ reducedMotion: "reduce" }))
    await expect
      .poll(async () => (await anonymous.goto("/repos/acme/tool"))?.status(), { message: "the public loses the repository" })
      .toBe(404)
    const ada = settled(await page.context().newPage())
    await ada.goto("/repos/acme/tool")
    await expect(ada.getByText("Not public", { exact: true })).toBeVisible()
    await ada.close()

    await card.getByRole("button", { name: "Undo" }).click()
    await expect(tool.getByRole("radio", { name: "Everyone" })).toHaveAttribute("aria-checked", "true")
    await expect
      .poll(async () => (await anonymous.goto("/repos/acme/tool"))?.status(), { message: "the public has it again" })
      .toBe(200)
    await anonymous.close()

    // The overview offers scp.yaml with what is in force written into it; only an admin may download it.
    await nav.getByRole("link", { name: /^Overview/ }).click()
    await expect(page.getByRole("link", { name: "Download scp.yaml" })).toBeVisible()
    const file = await page.request.get("/api/v1/admin/config.yaml")
    expect(file.status()).toBe(200)
    expect(file.headers()["content-disposition"]).toContain('filename="scp.yaml"')
    expect(await file.text()).toContain("announcements:")
    const outsider = await browser.newContext()
    expect((await outsider.request.get("/api/v1/admin/config.yaml")).status()).toBe(401)
    await outsider.close()
  },
)
