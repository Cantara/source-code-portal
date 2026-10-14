import tseslint from "typescript-eslint"
import jsxA11y from "eslint-plugin-jsx-a11y"
import reactHooks from "eslint-plugin-react-hooks"
export default tseslint.config(
  { files: ["src/**/*.{ts,tsx}"], extends: [tseslint.configs.recommended, jsxA11y.flatConfigs.recommended, reactHooks.configs.flat.recommended] },
)
