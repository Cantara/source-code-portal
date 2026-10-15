import js from "@eslint/js"
import reactHooks from "eslint-plugin-react-hooks"
import globals from "globals"
import tseslint from "typescript-eslint"

const innerHTML = {
  selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
  message: "Render server HTML through shared/safe-html.tsx.",
}

/** layer bans the given import patterns (static and dynamic) and every relative escape for files; except may set markup. */
function layer(files, banned, except = []) {
  const patterns = ["../*", "./../*", ...banned]
  const message = "Import only from your own folder (./) and the layers below (see eslint.config.js)."
  const dynamic = patterns.map((p) => ({
    selector: `ImportExpression[source.value=/^${p.replace(/[.*/@]/g, (c) => (c === "*" ? ".*" : `\\${c}`))}/]`,
    message,
  }))
  return [
    {
      files: [files],
      ignores: except,
      rules: {
        "no-restricted-imports": ["error", { patterns: [{ group: patterns, message }] }],
        "no-restricted-syntax": ["error", innerHTML, ...dynamic],
      },
    },
    ...except.map((f) => ({
      files: [f],
      rules: {
        "no-restricted-imports": ["error", { patterns: [{ group: patterns, message }] }],
        "no-restricted-syntax": ["error", ...dynamic],
      },
    })),
  ]
}

export default tseslint.config(
  { ignores: [".next", "bundle", "next-env.d.ts", "playwright-report", "test-results"] },
  {
    files: ["**/*.{ts,tsx,js}"],
    extends: [js.configs.recommended, tseslint.configs.strict, reactHooks.configs.flat.recommended],
    languageOptions: { globals: globals.browser },
    rules: {
      // Only shared/safe-html.tsx sets markup, and only the server-sanitised html field; lib/seo.tsx sets a JSON-LD data block.
      "no-restricted-syntax": ["error", innerHTML],
    },
  },
  // Configuration, build scripts and the Playwright tests run in Node.
  { files: ["*.{ts,js,mjs}", "scripts/**", "e2e/**"], languageOptions: { globals: globals.node } },
  // Layers, bottom up: lib, components/ui, shared (generic UI), entities (repository and commit pieces), features, app. A layer
  // imports only those below it; a feature imports itself (./) and the layers below, never another feature; only app/ composes.
  // Imports go through @/ or ./, so a relative escape cannot dodge the rules, and dynamic imports obey them too.
  ...layer("src/lib/**", ["@/components/*", "@/shared/*", "@/entities/*", "@/features/*", "@/app/*"], ["src/lib/seo.tsx"]),
  ...layer("src/components/**", ["@/shared/*", "@/entities/*", "@/features/*", "@/app/*"]),
  ...layer("src/shared/**", ["@/entities/*", "@/features/*", "@/app/*"], ["src/shared/safe-html.tsx"]),
  ...layer("src/entities/**", ["@/features/*", "@/app/*"]),
  ...layer("src/features/**", ["@/features/*", "@/app/*"]),
  ...layer("src/app/**", []),
)
