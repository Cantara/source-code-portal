import { QueryClientProvider } from "@tanstack/react-query"
import { CSPProvider } from "@base-ui/react/csp-provider"
import { RouterProvider } from "@tanstack/react-router"
import { I18nProvider } from "@lingui/react"
import { useEffect, useState } from "react"
import { createQueryClient } from "@/lib/api/query-client"
import { createAppRouter } from "./router"
import { i18n, initI18n } from "@/lib/i18n"
import { apply, watchSystemTheme } from "@/lib/theme"

initI18n()

export function App() {
  const [queryClient] = useState(createQueryClient)
  const [router] = useState(() => createAppRouter(queryClient))
  useEffect(() => {
    apply()
    return watchSystemTheme()
  }, [])
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
