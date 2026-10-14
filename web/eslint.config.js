import js from "@eslint/js"
import reactHooks from "eslint-plugin-react-hooks"
import globals from "globals"
import tseslint from "typescript-eslint"

export default tseslint.config(
  { ignores: [".next", "bundle", "next-env.d.ts", "playwright-report", "test-results"] },
  {
    files: ["**/*.{ts,tsx,js}"],
    extends: [js.configs.recommended, tseslint.configs.strict, reactHooks.configs.flat.recommended],
    languageOptions: { globals: globals.browser },
    rules: {
      // Only shared/safe-html.tsx sets markup, and only the server-sanitised html field.
      "no-restricted-syntax": [
        "error",
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message: "Render server HTML through shared/safe-html.tsx.",
        },
      ],
    },
  },
  // Configuration, build scripts and the Playwright tests run in Node.
  { files: ["*.{ts,js,mjs}", "scripts/**", "e2e/**"], languageOptions: { globals: globals.node } },
  { files: ["src/shared/safe-html.tsx"], rules: { "no-restricted-syntax": "off" } },
  {
    // Features never import features: they share through shared/, lib/ and typed links.
    files: ["src/features/**"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            { group: ["@/features/*", "../*"], message: "A feature imports only itself (./), shared/, lib/ and components/." },
          ],
        },
      ],
    },
  },
  {
    files: ["src/shared/**", "src/lib/**", "src/components/**"],
    rules: {
      "no-restricted-imports": [
        "error",
        { patterns: [{ group: ["@/features/*", "@/app/*"], message: "Shared code imports no feature." }] },
      ],
    },
  },
)
