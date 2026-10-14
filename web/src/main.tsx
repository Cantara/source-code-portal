import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { App } from "./app/app"
import { reloadOnStaleChunk } from "./lib/build"
import "./styles/index.css"
import "./styles/docs.css"

reloadOnStaleChunk()
const root = document.getElementById("root")
if (!root) throw new Error("#root missing")
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
