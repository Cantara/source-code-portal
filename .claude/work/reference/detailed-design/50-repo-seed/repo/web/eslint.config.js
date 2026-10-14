import js from "@eslint/js"
import globals from "globals"
import tseslint from "typescript-eslint"
import reactHooks from "eslint-plugin-react-hooks"
import reactRefresh from "eslint-plugin-react-refresh"
import jsxA11y from "eslint-plugin-jsx-a11y"
import boundaries from "eslint-plugin-boundaries"
import importX from "eslint-plugin-import-x"
import lingui from "eslint-plugin-lingui"

// Feature rank (web mirror of the Go rank table, architecture 12.2): a feature may import another feature only through its
// index.ts and only in the direction listed here. Everything else fails the boundaries rule.
export const FEATURE_MAY_IMPORT = {
  dashboard: ["groups", "commits"],
  groups: ["commits"],
  commits: [],
  docs: [],
  manifests: [],
  pages: [],
  status: [],
}

const PATH_LITERAL = "^\\/(api|repos|groups|commits|badges|media|llms|dependencies|packages|pages|status|dashboard)(\\/|$|\\?)"

// Typed route references (<Link to>, redirect({ to }), createFileRoute) are compile-checked by the router, so they are allowed;
// every other string that looks like a portal path is a defect (D-16: no hard-coded URL strings).
const TYPED_ROUTE_CONTEXT =
  ":not(JSXAttribute[name.name='to'] > Literal):not(JSXAttribute[name.name='to'] > JSXExpressionContainer > Literal):not(Property[key.name='to'] > Literal):not(CallExpression[callee.name='createFileRoute'] > Literal):not(CallExpression[callee.name='redirect'] Property[key.name='to'] > Literal)"
function urlSelectors({ allowApi }) {
  const re = allowApi ? PATH_LITERAL.replace("(api|", "(") : PATH_LITERAL
  return [
    {
      selector: `Literal[value=/${re}/]${TYPED_ROUTE_CONTEXT}`,
      message: "No hard-coded URLs: use typed routes (<Link to>) or lib/routes.ts. Endpoint paths belong to features/*/api.ts and lib/api.",
    },
    { selector: `TemplateElement[value.raw=/${re}/]`, message: "No hard-coded URLs: use typed routes or lib/routes.ts." },
  ]
}

function restrictedSyntax({ allowApi, urls = true }) {
  return [
    ...(urls ? urlSelectors({ allowApi }) : []),
    { selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']", message: "Only shared/safe-html.tsx may set innerHTML." },
    { selector: "MemberExpression[property.name='innerHTML']", message: "Only shared/safe-html.tsx may set innerHTML." },
    { selector: "AssignmentExpression[left.property.name='outerHTML']", message: "No outerHTML." },
    { selector: "CallExpression[callee.property.name='insertAdjacentHTML']", message: "No insertAdjacentHTML." },
    {
      selector:
        "JSXOpeningElement[name.name='SafeHtml'] > JSXAttribute[name.name='html'] > JSXExpressionContainer > :not(MemberExpression[property.name='html'])",
      message: "<SafeHtml html> accepts only an API field named `html` (server-sanitised).",
    },
    { selector: "MemberExpression[object.name='document'][property.name='cookie']", message: "No cookies." },
    {
      selector: "JSXOpeningElement[name.name='AvatarImage']",
      message: "No remote avatars in v1 (risk W19, CSP img-src 'self'): show initials with AvatarFallback.",
    },
  ]
}

const featureRules = Object.entries(FEATURE_MAY_IMPORT).map(([name, others]) => ({
  from: { element: { type: "feature", captured: { featureName: name } } },
  allow: {
    to: [
      { element: { type: "feature", captured: { featureName: name } } },
      { element: { type: "ui" } },
      { element: { type: "shared" } },
      { element: { type: "lib" } },
      ...others.map((o) => ({ element: { type: "feature", captured: { featureName: o }, fileInternalPath: "index.ts" } })),
    ],
  },
}))

export default tseslint.config(
  {
    ignores: ["dist", ".build", "node_modules", "playwright-report", "test-results", "src/lib/api/schema.d.ts", "src/app/routeTree.gen.ts"],
  },
  js.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    extends: [tseslint.configs.recommendedTypeChecked],
    languageOptions: { parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname }, globals: { ...globals.browser } },
    rules: {
      "@typescript-eslint/no-floating-promises": "error",
      // `throw redirect(...)` and `throw notFound()` are the router's documented control flow
      "@typescript-eslint/only-throw-error": [
        "error",
        { allow: [{ from: "package", package: "@tanstack/router-core", name: ["Redirect", "NotFoundError"] }] },
      ],
      "@typescript-eslint/consistent-type-imports": ["error", { fixStyle: "inline-type-imports" }],
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
    },
  },
  {
    files: ["scripts/**/*.ts", "*.config.ts", "e2e/**/*.ts", "routes.ts"],
    languageOptions: { globals: { ...globals.node } },
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: { boundaries, "import-x": importX, "react-refresh": reactRefresh },
    extends: [reactHooks.configs.flat.recommended, jsxA11y.flatConfigs.recommended],
    settings: {
      "import/resolver": { typescript: { project: "./tsconfig.app.json" } },
      "boundaries/ignore": ["src/main.tsx"], // the entry point wires app + lib + styles; nothing imports it
      "boundaries/elements": [
        { type: "feature", pattern: "src/features/*", capture: ["featureName"] },
        { type: "ui", pattern: "src/components/ui" },
        { type: "shared", pattern: "src/shared" },
        { type: "lib", pattern: ["src/lib", "src/locales"] },
        { type: "app", pattern: "src/app" },
        { type: "styles", pattern: "src/styles" },
      ],
    },
    rules: {
      "boundaries/dependencies": [
        2,
        {
          default: "disallow",
          policies: [
            ...featureRules,
            {
              from: { element: { type: "shared" } },
              allow: { to: [{ element: { type: "shared" } }, { element: { type: "ui" } }, { element: { type: "lib" } }] },
            },
            { from: { element: { type: "ui" } }, allow: { to: [{ element: { type: "ui" } }, { element: { type: "lib" } }] } },
            { from: { element: { type: "lib" } }, allow: { to: [{ element: { type: "lib" } }] } },
            {
              from: { element: { type: "app" } },
              allow: {
                to: [
                  { element: { type: "app" } },
                  { element: { type: "feature" } },
                  { element: { type: "shared" } },
                  { element: { type: "ui" } },
                  { element: { type: "lib" } },
                ],
              },
            },
          ],
        },
      ],
      "import-x/no-cycle": "error",
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true, allowExportNames: ["Route"] }],
      "no-restricted-syntax": ["error", ...restrictedSyntax({ allowApi: false })],
      "no-restricted-globals": [
        "error",
        { name: "fetch", message: "Use lib/api (openapi-fetch)." },
        { name: "XMLHttpRequest", message: "Use lib/api." },
        { name: "EventSource", message: "Use lib/api/events (SSE)." },
        { name: "localStorage", message: "Use lib/persist." },
        { name: "sessionStorage", message: "Use lib/persist." },
        { name: "eval" },
      ],
      "no-restricted-imports": [
        "error",
        {
          paths: [
            { name: "zod", message: "Use zod/mini (bundle budget)." },
            { name: "react-router", message: "TanStack Router only." },
          ],
        },
      ],
      "no-console": ["error", { allow: ["error", "warn"] }],
      "no-new-func": "error",
    },
  },
  // the allowed places for the restricted things
  { files: ["src/features/*/api.ts"], rules: { "no-restricted-syntax": ["error", ...restrictedSyntax({ allowApi: true })] } },
  { files: ["eslint.config.js"], extends: [tseslint.configs.disableTypeChecked] },
  { files: ["src/lib/persist.ts"], rules: { "no-restricted-globals": "off" } },
  // overrides drop only what the file legitimately needs, never the whole ban list (an HTML sink or document.cookie stays banned)
  {
    files: ["src/shared/safe-html.tsx"],
    rules: {
      "no-restricted-syntax": [
        "error",
        ...restrictedSyntax({ allowApi: true, urls: false }).filter((r) => !/innerHTML|dangerously/i.test(r.message)),
      ],
    },
  },
  {
    files: ["src/lib/routes.ts", "src/lib/api/**"],
    rules: { "no-restricted-syntax": ["error", ...restrictedSyntax({ allowApi: true, urls: false })] },
  },
  {
    files: ["src/lib/api/**"],
    rules: { "no-restricted-globals": ["error", { name: "XMLHttpRequest" }, { name: "localStorage" }, { name: "eval" }] },
  },
  // shadcn-owned code: formatting and boundary rules apply, product rules do not
  {
    files: ["src/components/ui/**"],
    extends: [tseslint.configs.disableTypeChecked],
    rules: {
      "react-refresh/only-export-components": "off",
      "no-restricted-syntax": ["error", ...restrictedSyntax({ allowApi: true, urls: false })],
      "jsx-a11y/label-has-associated-control": "off",
      "jsx-a11y/click-events-have-key-events": "off",
      "jsx-a11y/no-noninteractive-element-interactions": "off",
    },
  },
  // route modules export `Route` next to local components by design (TanStack file routes)
  { files: ["src/app/root.tsx", "src/features/*/route.tsx"], rules: { "react-refresh/only-export-components": "off" } },
  // the pre-paint script is plain browser JS served from public/; it must never throw, hence the empty catches
  {
    files: ["public/**/*.js"],
    languageOptions: { globals: { ...globals.browser } },
    rules: { "no-empty": ["error", { allowEmptyCatch: true }], "no-unused-vars": ["error", { caughtErrors: "none" }] },
  },
  // tests may use fixtures and literals
  {
    files: ["**/*.test.{ts,tsx}", "src/test/**"],
    rules: {
      "no-restricted-syntax": "off",
      "no-restricted-globals": "off",
      "@typescript-eslint/no-unsafe-assignment": "off",
      "react-refresh/only-export-components": "off",
    },
  },
  // user-visible strings are localised (Lingui); shadcn code and tests are exempt
  {
    files: ["src/features/**/*.tsx", "src/shared/**/*.tsx", "src/app/**/*.tsx"],
    ignores: ["**/*.test.tsx"],
    plugins: { lingui },
    rules: {
      "lingui/no-unlocalized-strings": [
        "error",
        {
          ignore: ["^[^a-zA-Z]*$", "^[a-z-]+$", "^[A-Z]{3,4}$", "^main h1$"],
          ignoreNames: ["className", "type", "id", "to", "href", "src", "role", "key", "data-testid", "variant", "size", "name", "kind"],
          ignoreFunctions: [
            "console.*",
            "cn",
            "cva",
            "createFileRoute",
            "createRootRouteWithContext",
            "setAttribute",
            "Error",
            "querySelector",
          ],
        },
      ],
    },
  },
)
