# AGENTS.md

Instructions for coding agents working in this repository (Source Code Portal, Go module `github.com/cantara/source-code-portal`).

- Plan and design: `.claude/work/plan.md` (binding), `.claude/work/design/*.md`, decisions in `.claude/work/decisions.md`.
- Layout: `cmd/scp` (Cobra CLI and all wiring), `internal/<package>` (created with its first code), `web/` (React SPA, embedded with the
  `spa` build tag), `deploy/` (Dockerfile, example config), `docs/` (user documentation).
- Rules: features (`catalog`, `commits`, `docs`, `badges`, `webhook`, `llms`) never import each other, enforced by
  `cmd/scp/boundary_test.go`; request handlers read only the current snapshot; no globals or `init()`; config keys, metrics and
  packages arrive with the code that uses them.
- Gate: `make check` (needs golangci-lint 2.14.0, Go 1.27.2, Node 24, pnpm 12). Single test: `go test -race -run TestName ./internal/<pkg>/`.
- Tests never touch the network: loopback listeners only, a fake GitHub server and recorded fixtures.
- Commits: Conventional Commits. Generated files are regenerated with `make gen`, never edited by hand.
