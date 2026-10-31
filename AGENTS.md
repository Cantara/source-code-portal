# AGENTS.md

Instructions for coding agents working in this repository (Source Code Portal, Go module `github.com/cantara/source-code-portal`).

- Plan and design: `.claude/work/plan.md` (binding), `.claude/work/design/*.md`, decisions in `.claude/work/decisions.md`.
- The agent drives: milestones in order, the next one started when the last is green; the specs cover the requirements, the owner
  accepts the agent's choices. Decide, record in `decisions.md` and continue; never ask the owner.
- Layout: `cmd/scp` (Cobra CLI and all wiring), `internal/<package>` (created with its first code), `api/openapi.yaml` (the `/api/v1`
  contract), `web/` (Next.js web UI; `pnpm build` makes `web/bundle`, which `scp serve` runs with Node via `web.dir`), `deploy/` (Dockerfile, example config), `docs/` (user documentation).
- Greenfield: change code, contract, config keys and the store schema in place; no compatibility layers or extra migrations.
- Rules: features (`catalog`, `commits`, `docs`, `badges`, `webhook`, `llms`, `manifests`) never import each other, enforced by
  `cmd/scp/boundary_test.go`; request handlers read only the current snapshot; no globals or `init()`; config keys, metrics and
  packages arrive with the code that uses them.
- Gate: `make check` (needs golangci-lint 2.14.0, Go 1.27.3, Node 24, pnpm 12 and the Playwright Chromium: `pnpm -C web exec playwright install chromium`). Single test: `go test -race -run TestName ./internal/<pkg>/`.
- Tests never touch the network: loopback listeners only, the fake GitHub in `internal/github/githubtest` and recorded fixtures.
- Commits: Conventional Commits, directly on `main`. Generated files are regenerated with `make gen`, never edited by hand.
