@AGENTS.md

# Claude Code notes

- Run `make arch` after adding or moving a package, and `make arch-fix` after editing `internal/archtest/rules.go`; never edit the generated
  block between the BEGIN/END markers of `.golangci.yml` by hand.
- Prefer the `make` targets over ad-hoc commands: CI runs the same ones. `make check` is the gate at the current bootstrap step (it needs git, Node 24.21, pnpm 12.10.1, golangci-lint v2.14.0).
- Before designing anything, find its unit in `docs/design/20-design/` (map: `docs/design/README.md`; the order of work is `docs/design/40-implementation-plan.md`); each unit lists its tasks, acceptance
  criteria, Go signatures and the verification notes. Implement against the document; record disagreements as proposals, not as silent changes.
- Library and version facts move fast (Go, oapi-codegen, shadcn/ui, Vite, pnpm, GitHub API): verify against the primary source and write the
  date into the pull request or the ADR before relying on memory.
- Do not create report or summary markdown files in the repository; put durable knowledge in `docs/` (ADR, design notes) and keep pull request
  descriptions factual.
