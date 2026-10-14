import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import "./styles/index.css"
import { App } from "./app/providers"
import { BUILD_ID } from "./lib/build"
import { onPreloadError } from "./lib/stale-chunk"

// A deploy replaces every hashed chunk; reload once, never in a loop (lib/stale-chunk.ts).
window.addEventListener("vite:preloadError", (event) => onPreloadError(event))

document.documentElement.dataset["build"] = BUILD_ID // visible to support and to the e2e job; the skew check of design 17 replaces nothing here

const root = document.getElementById("root")
if (!root) throw new Error("#root missing")
createRoot(root, {
  onUncaughtError: (error, info) => console.error("uncaught", error, info.componentStack),
  onCaughtError: (error, info) => console.error("caught", error, info.componentStack),
  onRecoverableError: (error) => console.warn("recoverable", error),
}).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
