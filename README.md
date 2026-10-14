# Source Code Portal

An organisational dashboard for GitHub: repositories grouped into systems, with their latest commits, rendered README and docs, status
badges and an API that scripts and LLM agents can read. One static Go binary with the web UI embedded, kept fresh by webhooks and
scheduled sync.

Documentation: [operator guide](docs/operator-guide.md), [repository guide](docs/repository-guide.md), [API guide](docs/api.md).

```sh
docker run --rm -p 9090:9090 -v scp-data:/data -v "$PWD/scp.yaml:/etc/scp/scp.yaml:ro" -v "$PWD/secrets:/run/secrets:ro" \
  ghcr.io/cantara/source-code-portal:2.0.0 serve --config /etc/scp/scp.yaml
```

Binaries for Linux (amd64, arm64) and macOS (arm64) are on the [releases page](https://github.com/Cantara/source-code-portal/releases).

## Build from source

```sh
make build                     # web UI and bin/scp (needs Go 1.27, Node 24, pnpm 12)
cp deploy/config.example.yaml scp.yaml   # then set your organisations and the App key or token file
bin/scp config check -c scp.yaml
bin/scp serve -c scp.yaml      # public :9090 (UI, /api/v1, /badges, /webhooks/github), admin :9091 (/livez, /readyz)
make image                     # container image scp:dev
```

Configuration is one YAML file (`--config`) with `SCP_*` environment overrides; at least one GitHub source (`github.sources`, a GitHub
App or a token file) is required. `deploy/config.example.yaml` shows every key; `scp config check` prints the effective settings.
Point an organisation webhook (content type `application/json`, the secret in `webhook.secret_file`) at `/webhooks/github` for
updates within seconds; without it the portal still refreshes on its schedule.

## Develop

```sh
make check                     # the gate: generated code, tidy modules, lint, tests, Playwright
make dev                       # Go server (scp.yaml) on :9090 and the web UI with hot reload on :5173
go test -race ./internal/config/
```

## Licence

Apache-2.0, see [LICENSE](LICENSE).
