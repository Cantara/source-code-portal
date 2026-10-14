# Source Code Portal

An organisational dashboard for GitHub: repositories grouped into systems, with their latest commits, rendered README and docs, status
badges and an API that scripts and LLM agents can read. One static Go binary with the web UI embedded, kept fresh by webhooks and
scheduled sync.

Status: under development towards v1.0. The plan is in [`.claude/work/plan.md`](.claude/work/plan.md).

## Run

```sh
make build                     # web UI and bin/scp (needs Go 1.27, Node 24, pnpm 12)
bin/scp serve                  # public :9090, admin :9091 (/livez)
bin/scp config check -c deploy/config.example.yaml
make image                     # container image scp:dev
```

Configuration is one YAML file (`--config`) with `SCP_*` environment overrides; `scp config check` prints the effective settings.

## Develop

```sh
make check                     # the pull request gate: generated code, tidy modules, lint, tests
make dev                       # Go server on :9090 and the web UI with hot reload on :5173
go test -race ./internal/config/
```

## Licence

Apache-2.0, see [LICENSE](LICENSE).
