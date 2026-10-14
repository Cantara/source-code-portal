# Decisions

The decisions in force, newest last. The full log of how the portal got here (M0..M11) is `history/decisions-m0-m11.md`. The owner's
standing instruction: the agent decides as it goes, records here and reports; decisions are not put to the owner as questions.

| ID | Decision | Recorded in |
|---|---|---|
| — | Repository `Cantara/source-code-portal`, branch `main`, module `github.com/cantara/source-code-portal`, image `ghcr.io/cantara/source-code-portal`. `Cantara/SourceCodePortal` (Java) is read-only reference | `.claude/CLAUDE.md` |
| — | Planning and design under `.claude/work/`, finished work in `.claude/work/history/`; `docs/` is user documentation only; documents describe what is built | `plan.md` section 5 |
| A-34 | CLI on Cobra; configuration with Viper (one instance per run, `UnmarshalExact`), `SCP_*` overrides | `design/build-ops.md` |
| A-35 | Hazelcast is the distributed-state backend for several instances | `design/cluster.md` |
| P-9 | Greenfield: code, contract, config keys, the store schema and documents change in place, no compatibility layers or extra migrations; commits go directly to `main`; `.claude/CLAUDE.md` is kept current in the same commit | `.claude/CLAUDE.md`, `AGENTS.md` |
| P-11 | The Go module path has no `/v2`: SCP is an application, not a library | `go.mod` |
| P-13 | The web UI is Next.js with server-side rendering, every page complete HTML | `design/web.md` |
| P-15 | Licence: Apache License 2.0, as for the Java version | `LICENSE` |
| P-16 | Semantic versioning; published tags are never moved. `v2.0.0` is published; the next release is `v2.1.0` | `plan.md` section 4, `CHANGELOG.md` |
| P-17 | Dependencies are the latest release of the best-in-class choice. TypeScript 7 is `tsc` (`@typescript/native`); the `typescript` name holds the 6.0 API (`@typescript/typescript6`) for typescript-eslint and Next.js | `plan.md` section 3, `web/package.json` |
| P-18 | Search engines are planned from the krantho study and built after the feature modules | `plan.md` section 4, `design/seo.md` |
| P-19 | `.claude/work/` holds only what is built and in progress; finished milestone plans, the full decision log, the earlier detailed design and the UI brief live in `.claude/work/history/` | `plan.md`, `README.md` |
| P-20 | Before `v2.1.0`, the code is reshaped so features own their data (M12, `design/feature-modules.md`), following the backend and frontend package-by-feature reviews of 2026-10-15 | `plan.md` section 4 |
