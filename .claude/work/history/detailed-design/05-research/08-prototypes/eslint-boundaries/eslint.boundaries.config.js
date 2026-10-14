// Verified 2026-10-13 (E11) with eslint 10.12.0, eslint-plugin-boundaries 7.2.0, typescript-eslint 8.71.2,
// eslint-import-resolver-typescript 4.4.5. Reconstructed from the experiment transcript after the
// working copy was overwritten by the jsx-a11y experiment; the policy block is unchanged.
// Observed results on the sample tree in ./src:
//   features/b/impl.ts   importing "@/features/a/internal"   -> error (cross-feature deep import)
//   shared/sh.ts         importing "@/features/b"            -> error (shared may not depend on features)
//   features/b/impl.ts   importing "@/features/a" (index.ts) -> allowed
import tseslint from "typescript-eslint"
import boundaries from "eslint-plugin-boundaries"

export default tseslint.config({
  files: ["src/**/*.{ts,tsx}"],
  extends: [tseslint.configs.recommended],
  plugins: { boundaries },
  settings: {
    "import/resolver": { typescript: { project: "./tsconfig.json" } },
    "boundaries/elements": [
      { type: "feature", pattern: "src/features/*", capture: ["featureName"] },
      // folder patterns (not "src/shared/*"): file-level patterns left files unclassified in the experiment
      { type: "shared", pattern: "src/shared" },
      { type: "lib", pattern: "src/lib" },
    ],
  },
  rules: {
    "boundaries/dependencies": [
      2,
      {
        default: "disallow",
        policies: [
          {
            from: { element: { type: "feature" } },
            allow: {
              to: [
                // a feature may import itself ...
                { element: { type: "feature", captured: { featureName: "{{from.element.captured.featureName}}" } } },
                { element: { type: "shared" } },
                { element: { type: "lib" } },
                // ... and other features only through their public index.ts
                { element: { type: "feature", fileInternalPath: "index.ts" } },
              ],
            },
          },
          { from: { element: { type: "shared" } }, allow: { to: [{ element: { type: "shared" } }, { element: { type: "lib" } }] } },
          { from: { element: { type: "lib" } }, allow: { to: [{ element: { type: "lib" } }] } },
        ],
      },
    ],
  },
})
