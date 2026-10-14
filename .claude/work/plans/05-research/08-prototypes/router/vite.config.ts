import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import { tanstackRouter } from "@tanstack/router-plugin/vite"
export default defineConfig({
  plugins: [tanstackRouter({ target: "react", autoCodeSplitting: true, routesDirectory: "./src", virtualRouteConfig: "./routes.ts", generatedRouteTree: "./src/routeTree.gen.ts" }), react()],
})
