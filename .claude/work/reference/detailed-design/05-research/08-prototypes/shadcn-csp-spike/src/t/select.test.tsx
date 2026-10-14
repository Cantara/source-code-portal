import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { expect, test } from "vitest"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
test("jsdom: tabs switch", async () => {
  render(<Tabs defaultValue="a"><TabsList><TabsTrigger value="a">A</TabsTrigger><TabsTrigger value="b">B</TabsTrigger></TabsList><TabsContent value="a">pa</TabsContent><TabsContent value="b">pb</TabsContent></Tabs>)
  await userEvent.click(screen.getByRole("tab", { name: "B" }))
  expect(await screen.findByText("pb")).toBeTruthy()
})
