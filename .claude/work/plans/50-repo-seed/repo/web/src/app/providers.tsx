import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { CSPProvider } from "@base-ui/react/csp-provider"
import { RouterProvider } from "@tanstack/react-router"
import { I18nProvider } from "@lingui/react"
import { useState } from "react"
import { createAppRouter } from "./router"
import { i18n, initI18n } from "@/lib/i18n"

initI18n()

// Seed shell: the real query client (retry policy, problem handling, snapshot sync) is design 17 section 4; replace this one then.
export function App() {
  const [queryClient] = useState(() => new QueryClient())
  const [router] = useState(() => createAppRouter(queryClient))
  return (
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <CSPProvider disableStyleElements>
          <RouterProvider router={router} />
        </CSPProvider>
      </QueryClientProvider>
    </I18nProvider>
  )
}
