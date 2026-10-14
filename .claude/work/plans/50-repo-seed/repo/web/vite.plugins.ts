import react, { reactCompilerPreset } from "@vitejs/plugin-react"
import babel from "@rolldown/plugin-babel"
import { lingui } from "@lingui/vite-plugin"
import { tanstackRouter } from "@tanstack/router-plugin/vite"
import tailwindcss from "@tailwindcss/vite"
import type { PluginOption } from "vite"

/** The plugin chain shared by vite and vitest: tests run against the React-Compiler-compiled, Lingui-transformed code. */
export function appPlugins(opts: { router: boolean; css: boolean }): PluginOption[] {
  return [
    ...(opts.router
      ? [
          tanstackRouter({
            target: "react",
            autoCodeSplitting: true,
            routesDirectory: "./src",
            virtualRouteConfig: "./routes.ts",
            generatedRouteTree: "./src/app/routeTree.gen.ts",
          }),
        ]
      : []),
    react(),
    babel({ plugins: ["@lingui/babel-plugin-lingui-macro"], presets: [reactCompilerPreset()] }),
    lingui(),
    ...(opts.css ? [tailwindcss()] : []),
  ]
}
