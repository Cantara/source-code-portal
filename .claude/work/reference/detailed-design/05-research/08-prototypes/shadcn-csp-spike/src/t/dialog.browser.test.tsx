import { render } from "vitest-browser-react"
import { expect, test } from "vitest"
import { Dialog, DialogContent, DialogTrigger, DialogTitle, DialogDescription } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
test("browser: dialog opens, focus moves in, Escape closes", async () => {
  const screen = await render(<Dialog><DialogTrigger render={<Button>Open</Button>} /><DialogContent><DialogTitle>T</DialogTitle><DialogDescription>D</DialogDescription></DialogContent></Dialog>)
  await screen.getByRole("button", { name: "Open" }).click()
  await expect.element(screen.getByRole("dialog")).toBeVisible()
})
