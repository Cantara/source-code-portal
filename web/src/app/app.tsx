import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider } from "@tanstack/react-router"
import { useState } from "react"
import { createAppRouter } from "./router"

export function App({ initialPath }: { initialPath?: string }) {
  const [queryClient] = useState(() => new QueryClient())
  const [router] = useState(() => createAppRouter(queryClient, initialPath))
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}
