import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it } from "vitest"
import { SettingsView } from "@/features/settings/view"

const clear = () => {
  for (const c of document.cookie.split("; ")) {
    const name = c.split("=")[0]
    if (name) document.cookie = `${name}=; Path=/; Max-Age=0`
  }
}

describe("settings", () => {
  afterEach(clear)

  it("keeps each preference in its cookie at once, and Default forgets a page size", async () => {
    const user = userEvent.setup()
    render(
      <SettingsView initial={{ work: ["hand", "ai"], view: "cards", commits: 0, history: 0, pulls: 0, issues: 0, alumni: 0 }} />,
    )
    expect(screen.getByRole("heading", { level: 1, name: "Settings" })).toBeInTheDocument()
    const work = screen.getByRole("group", { name: "Count, at least one" })
    expect(work).toHaveTextContent("lower bound")
    expect(screen.getByRole("checkbox", { name: /^By hand/ })).toBeChecked()
    expect(screen.getByRole("checkbox", { name: /^By automation/ })).not.toBeChecked()
    await user.click(screen.getByRole("checkbox", { name: /^By automation/ }))
    expect(document.cookie).toContain(`scp-work=${encodeURIComponent("hand,ai,automation")}`)
    await user.click(screen.getByRole("checkbox", { name: /^By hand/ }))
    await user.click(screen.getByRole("checkbox", { name: /^With AI/ }))
    expect(document.cookie).toContain("scp-work=automation")
    // The last kind ticked stays: something always counts.
    expect(screen.getByRole("checkbox", { name: /^By automation/ })).toBeDisabled()
    await user.click(screen.getByRole("radio", { name: "Table" }))
    expect(document.cookie).toContain("scp-view-products=table")
    const commits = screen.getByRole("combobox", { name: "Commit timelines" })
    await user.selectOptions(commits, "100")
    expect(document.cookie).toContain("scp-per-commits=100")
    await user.selectOptions(commits, "Default")
    expect(document.cookie).not.toContain("scp-per-commits")
    expect(screen.getByText(/Saved\. Commit timelines: the default\./)).toBeInTheDocument()
  })
})
