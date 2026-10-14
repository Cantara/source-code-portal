import { defineConfig } from "@hey-api/openapi-ts"
export default defineConfig({
  input: "../api/openapi.yaml",
  output: "src/gen-hey",
  plugins: ["@hey-api/typescript", "zod", { name: "@hey-api/sdk", validator: false }, "@hey-api/client-fetch"],
})
