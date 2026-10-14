# ADR-019: Frontend is a React + shadcn/ui + TypeScript SPA embedded in the Go binary (Svelte 5 considered)

Status: Accepted (owner directive 2026-10-13, D-16). Date: 2026-10-14. Supersedes 04 D-05 (rendering, assets, live panel) and the "no Node" statement in 04 D-11 (A-16).

## Context
The portal needs searchable tables, an org dependency graph, live status and accessible theming while keeping one static binary and a strict security posture. The owner first considered Svelte 5, then decided on React + shadcn/ui + TypeScript. Node is a build-time tool only.

## Decision
React 19.3 (+ React Compiler), TypeScript 7 (TypeScript 6 alias for tools), Vite 8, Tailwind CSS 4.3, shadcn/ui on **Base UI** (default since 2026-07; Radix remains supported via `-b radix`), TanStack Router (virtual file routes, Zod-validated search params) and Query 5, TanStack Table 9, sigma 3 + graphology, Lingui, Vitest 5 + Testing Library + MSW 3, Playwright + axe, `eslint-plugin-boundaries`, pnpm 12 with a 7-day release cooldown, Node 24.21 (26 LTS after 2026-10-28). Structure is package-by-feature mirroring the backend; boundary rules are lint-enforced (`features/x` imports other features only through `index.ts`). Typed routes replace the Go route-crawl; no production Zod response validation (build-id skew check instead). Output embedded per ADR-010. Accessibility: WCAG 2.2 AA, token overrides, axe in CI. Bundle budgets and details in architecture section 12.

## Alternatives considered (evidence-based, not an open question)
Svelte 5 (+ shadcn-svelte, SvelteKit or plain Vite): hello-world 12.0 KB vs React's 65.8 KB gzip (research 08); Svelte is the more admired and React by far the more used framework in the Stack Overflow survey (the exact percentages originally quoted for a 2026 survey could not be verified on 2026-10-14: only 2025 figures were found, about 44.7% React and 7.2% Svelte usage, with sources disagreeing on attribution, so no percentages are asserted here); shadcn-svelte is a community port, no typed router comparable to TanStack Router was evaluated, SvelteKit SPA mode is documented as a performance negative and assistant/LLM familiarity is lower (Web-Bench result is secondary and dated). The 54 KB runtime gap is small against the budgets of an internal portal. React Router 8 (typed routes only in framework mode); templ/HTMX (rejected by the owner for the target UX); Radix instead of Base UI (kept as escape hatch).

## Consequences
Positive: typed routes and API, large ecosystem, accessible primitives, one binary at runtime. Negative: a Node toolchain in CI and Docker builds, supply-chain exposure (cooldown, exact pins, SHA-pinned Actions), shadcn components are owned code needing three CSP patches and contrast fixes, a bundle budget to police, first paint needs JS (no SSR; crawler and LLM surfaces are server-generated).

## Evidence
`05-research/08-frontend-stack.md` sections 1-2, 4, 5, 8, 9; React 19.3.0 and TanStack Router 1.170.44 verified 2026-10-14 at https://registry.npmjs.org/react/latest and https://registry.npmjs.org/@tanstack/react-router/latest; shadcn Base UI default verified at https://ui.shadcn.com/docs/changelog.

## Exit condition
Revisit only if the bundle budget cannot be met or the React/Base UI/TanStack supply chain becomes untenable.

## Amendment 2026-10-14 (review revision)

Reviewer finding m13: the survey figures were unverifiable and have been removed; the decision does not depend on them. Presentation choice recorded in the architecture review for the owner to confirm: commit authors render as initials in v1 (no avatar fetch, no third-party request), tracked as risk W19.
