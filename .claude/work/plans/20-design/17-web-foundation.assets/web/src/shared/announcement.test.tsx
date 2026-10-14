import { screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import { AnnouncementBanner } from "./announcement"
import { STORAGE } from "@/lib/storage-keys"
import { renderI18n as render } from "@/test/render"

const a = { id: "h1", text: "Maintenance on Friday", level: "warning" as const, dismissible: true }

describe("AnnouncementBanner", () => {
  it("renders plain text only (markup in the text is not interpreted)", () => {
    render(<AnnouncementBanner announcement={{ ...a, text: "<b>bold</b> now" }} />)
    expect(screen.getByText("<b>bold</b> now")).toBeInTheDocument()
    expect(document.querySelector("b")).toBeNull()
  })
  it("dismissal is remembered per id; a changed announcement shows again", async () => {
    const { unmount } = render(<AnnouncementBanner announcement={a} />)
    await userEvent.click(screen.getByRole("button", { name: /Dismiss announcement/ }))
    expect(screen.queryByText(a.text)).toBeNull()
    expect(window.localStorage.getItem(STORAGE.announcementDismissed)).toBe("h1")
    unmount()
    render(<AnnouncementBanner announcement={a} />)
    expect(screen.queryByText(a.text)).toBeNull()
    unmount()
    render(<AnnouncementBanner announcement={{ ...a, id: "h2" }} />)
    expect(screen.getByText(a.text)).toBeInTheDocument()
  })
  it("a non-dismissible announcement has no dismiss button", () => {
    render(<AnnouncementBanner announcement={{ ...a, dismissible: false }} />)
    expect(screen.queryByRole("button")).toBeNull()
  })
  it("a new announcement id arriving while mounted shows again (V-06)", async () => {
    const { rerender } = render(<AnnouncementBanner announcement={a} />)
    await userEvent.click(screen.getByRole("button", { name: /Dismiss announcement/ }))
    expect(screen.queryByText(a.text)).toBeNull()
    rerender(<AnnouncementBanner announcement={{ ...a, id: "h2", text: "Another notice" }} />)
    expect(screen.getByText("Another notice")).toBeInTheDocument()
  })
})
