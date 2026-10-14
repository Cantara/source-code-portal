import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import "./index.css"
import App from "./App.tsx"
import { CSPProvider } from "@base-ui/react/csp-provider"
import { ThemeProvider } from "@/components/theme-provider.tsx"

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider disableTransitionOnChange={false}>
      <CSPProvider disableStyleElements><App /></CSPProvider>
    </ThemeProvider>
  </StrictMode>
)
