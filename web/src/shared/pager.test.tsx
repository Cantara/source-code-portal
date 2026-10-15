import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { Pager } from "./pager"

const href = (n: number) => `/commits?page=${n}`

describe("Pager", () => {
  it("links the pages around the current one, the first and the last, with gaps between", () => {
    render(<Pager page={{ number: 6, size: 50, total: 1000, pages: 20 }} href={href} />)
    const nav = screen.getByRole("navigation", { name: "Pages" })
    expect(within(nav).getByText("251–300 of 1,000 commits")).toBeInTheDocument()
    const pages = within(nav)
      .getAllByRole("link", { name: /^Page / })
      .map((a) => a.textContent)
    expect(pages).toEqual(["1", "4", "5", "6", "7", "8", "20"])
    expect(within(nav).getByRole("link", { current: "page" })).toHaveAttribute("href", "/commits?page=6")
    expect(within(nav).getByRole("link", { name: "Previous" })).toHaveAttribute("href", "/commits?page=5")
    expect(within(nav).getByRole("link", { name: "Next" })).toHaveAttribute("href", "/commits?page=7")
  })

  it("disables what leads nowhere and hides the numbers of a single page", () => {
    const { rerender } = render(<Pager page={{ number: 1, size: 50, total: 120, pages: 3 }} href={href} />)
    expect(screen.getByText("Previous")).toHaveAttribute("aria-disabled", "true")
    rerender(<Pager page={{ number: 1, size: 50, total: 7, pages: 1 }} href={href} />)
    expect(screen.queryByRole("list")).not.toBeInTheDocument()
    expect(screen.getByText("1–7 of 7 commits")).toBeInTheDocument()
  })

  it("loads a page in place with onPage, and leaves a modified click to the browser", async () => {
    const user = userEvent.setup()
    const onPage = vi.fn()
    render(<Pager page={{ number: 1, size: 50, total: 120, pages: 3 }} href={href} onPage={onPage} />)
    await user.click(screen.getByRole("link", { name: "Page 3" }))
    expect(onPage).toHaveBeenCalledWith(3)
    await user.keyboard("{Control>}")
    await user.click(screen.getByRole("link", { name: "Next" }))
    await user.keyboard("{/Control}")
    expect(onPage).toHaveBeenCalledTimes(1)
  })
})
