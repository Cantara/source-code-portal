import { QueryClientProvider, type QueryClient } from "@tanstack/react-query"
import { RouterProvider } from "@tanstack/react-router"
import { useState } from "react"
import { createQueryClient } from "@/lib/query"
import { createAppRouter } from "./router"

export function App({ initialPath, queryClient }: { initialPath?: string; queryClient?: QueryClient }) {
  const [qc] = useState(() => queryClient ?? createQueryClient())
  const [router] = useState(() => createAppRouter(qc, initialPath))
  return (
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}
