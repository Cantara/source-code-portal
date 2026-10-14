# Source Code Portal

An organisational dashboard for a GitHub organisation: repositories grouped into systems, per-repository documentation (Markdown and
AsciiDoc), commit logs, local status badges, package-manifest and dependency intelligence, and an LLM-readable surface (`/llms.txt`,
OpenAPI). One static Go binary with the React single-page application embedded; state is a read-only snapshot refreshed in the
background and persisted in an embedded SQLite file.

The design is complete and lives in [`docs/design/`](docs/design/README.md);
this repository is built from it.

## Status

Bootstrap step 1 (design 16 section 4.12): the skeleton, the architecture test and the pipeline exist; features are not implemented yet.

## Quick start

```
make check          # generated files current, lint (golangci-lint v2.14.0), architecture test, race tests, web checks, govulncheck, licences
make build-go       # bin/scp-placeholder: Go only, placeholder web page
make web build      # pnpm build, then bin/scp with the SPA embedded (needs Node 24.21 and pnpm 12.10.1)
bin/scp version
```

Requirements: Go 1.27.2, GNU make, golangci-lint v2.14.0 (binary), Node 24.21 and pnpm 12.10.1 for `web/`, Docker for the image.

## Layout

`cmd/scp` composition root and CLI, `internal/` packages by feature (each has a `doc.go` with its responsibility and allowed imports),
`internal/archtest` the architecture test, `web/` the SPA (and the Go `embed` package), `api/` the OpenAPI contract, `deploy/` Dockerfiles and
examples, `tools/` pinned dev tools (a nested module), `docs/` documentation. Start with [`AGENTS.md`](AGENTS.md).

## Licence

Apache License 2.0, see [`LICENSE`](LICENSE) and [`THIRD_PARTY.md`](THIRD_PARTY.md).
