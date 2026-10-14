# Operator guide

Source Code Portal (SCP) is one static binary, `scp`, with the web UI embedded. It reads the public repositories of one or more GitHub
organisations, keeps a local SQLite cache and serves the portal. One instance serves up to 1,000 repositories.

- Public listener `:9090`: the web UI, the JSON API `/api/v1`, badges, `/llms.txt` and the GitHub webhook. Put it behind your proxy.
- Admin listener `:9091`: `/livez`, `/readyz`, `/metrics`, `/api/v1/status` and optional profiling. Keep it inside the cluster.

## 1. Connect GitHub

SCP needs read access to the organisations' public repositories. Use a GitHub App (recommended: its own rate limit of up to 12,500
requests an hour per installation) or a personal access token (5,000 an hour).

**GitHub App.** Create an App in the organisation settings (Developer settings, GitHub Apps):
1. Repository permissions: *Contents* read-only and *Metadata* read-only. Nothing else.
2. Webhook: active, URL `https://<portal>/webhooks/github`, a random secret (for example `openssl rand -hex 32`), and subscribe to the
   events *Push*, *Release* and *Repository*. Installation events are sent to Apps without subscribing.
3. Generate a private key (a `.pem` file) and note the App ID.
4. Install the App on each organisation, on all repositories.

**Token.** A fine-grained token with public repository read access, or a classic token without scopes, in a file. For updates within
seconds, add an organisation webhook (Settings, Webhooks) with content type `application/json`, the same secret and the events *Pushes*,
*Releases* and *Repositories*.

Without a webhook the portal still refreshes: it lists every organisation every five minutes and notices pushes from that listing;
commits, documents, descriptors and manifests are also re-read on their own schedule.

## 2. Configure

The configuration is one YAML file given with `--config` (or `-c`). Every key can be overridden by an environment variable
`SCP_<KEY>` with dots as underscores, for example `SCP_SERVER_PUBLIC_URL`; a set `SCP_*` variable that names no key is an error, as is
an unknown key in the file. Secrets are always read from files.

```yaml
server:
  public_url: https://scp.example.org  # absolute links in /llms.txt and Markdown; https also turns on HSTS
  trusted_proxies: [10.0.0.0/8]        # proxies whose X-Forwarded-For names the client
store:
  path: /data/scp.db
github:
  sources:
    - id: github
      orgs: [Cantara]
      app: { id: 123456, private_key_file: /run/secrets/scp-app.pem }
      # token_file: /run/secrets/github-token     (instead of app)
catalog:
  groups:
    - id: whydah
      name: Whydah
      description: Identity and access management
      default_repo: Whydah-UserAdminService
      match: [{ prefix: whydah- }, { topic: iam }]
    - { id: other, name: Other, default_repo: source-code-portal, match: [{ catch_all: true }] }
webhook:
  secret_file: /run/secrets/webhook-secret
```

`scp config check -c scp.yaml` validates the file exactly as `serve` does, lists every problem with its key, and prints the effective
settings with secrets redacted. It exits 0 when the configuration is valid and 2 when it is not.

### Keys

| Key | Default | Meaning |
|---|---|---|
| `server.public_addr` | `:9090` | public listener (also `--public-addr`) |
| `server.admin_addr` | `:9091` | admin listener (also `--admin-addr`) |
| `server.shutdown_grace` | `20s` | time in-flight requests and jobs get after SIGTERM |
| `server.public_url` | empty | absolute base URL of the public listener, without query; empty gives root-relative links |
| `server.trusted_proxies` | `[]` | CIDRs allowed to set `X-Forwarded-For` |
| `log.level` | `info` | `debug`, `info`, `warn`, `error` (also `--log-level`) |
| `log.format` | `json` | `json`, or `text` for local use |
| `store.path` | `/data/scp.db` | the SQLite cache |
| `sync.concurrency` | `4` | GitHub jobs running at once |
| `github.sources[]` | required | one entry per GitHub host and credential |
| `github.sources[].id` | required | short id, `[a-z0-9-]` |
| `github.sources[].base_url` | `https://api.github.com/` | REST base ending in `/`; GitHub Enterprise Server: `https://<host>/api/v3/` |
| `github.sources[].orgs` | required | organisations read with this credential; an organisation belongs to one source |
| `github.sources[].app.id` | | GitHub App ID |
| `github.sources[].app.private_key_file` | | the App's private key (PEM) |
| `github.sources[].app.installations` | all | account logins whose installations are used |
| `github.sources[].token_file` | | a token file, instead of `app` |
| `github.sources[].include_forks` | `false` | list forks |
| `github.sources[].include_archived` | `true` | list archived repositories |
| `catalog.reconcile_interval` | `5m` | how often every organisation is listed (1m to 24h) |
| `catalog.groups[]` | none | groups in dashboard order (at most 500) |
| `catalog.groups[].id` | required | URL id, `[a-z0-9-]`, at most 63 characters |
| `catalog.groups[].name` | required | display name, at most 120 characters |
| `catalog.groups[].description` | empty | at most 500 characters |
| `catalog.groups[].default_repo` | required | the group's main repository name; a missing one is shown on the group, the group stays |
| `catalog.groups[].sources` | all | source ids the group draws from |
| `catalog.groups[].match[]` | required | 1 to 64 matchers; a repository in any matcher is a member |
| `webhook.secret_file` | empty | webhook secret; the receiver is served only when set |
| `webhook.previous_secret_file` | empty | the old secret, accepted too while you rotate |
| `ops.pprof` | `false` | serve `/debug/pprof/` on the admin listener |

Matchers (each has exactly one field) see the repository name, except `topic`:

| Matcher | Selects |
|---|---|
| `exact: name` | the repository with that name (case-insensitive) |
| `prefix: whydah-` | names starting with the prefix (case-insensitive) |
| `glob: "*-service"` | the whole name against `*` and `?` (case-insensitive) |
| `regexp: "^scp-(api\|web)$"` | the whole name against an RE2 expression (case-sensitive unless `(?i)`) |
| `topic: iam` | repositories with that GitHub topic |
| `catch_all: true` | every repository |

A repository can be in several groups. Repositories in no group are counted on the dashboard and listed by the API.

## 3. Run

**Binary.** Download the archive for your platform from the GitHub release, check it and run it:

```sh
sha256sum -c scp_2.0.0_checksums.txt --ignore-missing
tar xzf scp_2.0.0_linux_amd64.tar.gz
./scp config check -c scp.yaml && ./scp serve -c scp.yaml
```

**Container.** The image `ghcr.io/cantara/source-code-portal` (linux/amd64 and linux/arm64) runs as UID 65532 on a read-only root file
system; `/data` is the one writable path.

```sh
docker run -d --name scp --read-only --cap-drop=ALL \
  -p 9090:9090 -p 127.0.0.1:9091:9091 \
  -v scp-data:/data -v "$PWD/scp.yaml:/etc/scp/scp.yaml:ro" -v "$PWD/secrets:/run/secrets:ro" \
  ghcr.io/cantara/source-code-portal:2.0.0 serve --config /etc/scp/scp.yaml
```

Images and checksum files are signed keyless by the release workflow. Verify an image with cosign:

```sh
cosign verify ghcr.io/cantara/source-code-portal:2.0.0 \
  --certificate-identity https://github.com/Cantara/source-code-portal/.github/workflows/release.yml@refs/tags/v2.0.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

**Kubernetes.** One replica with a persistent volume (the cache is one SQLite file; use block or local storage, not NFS):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: scp }
spec:
  replicas: 1
  strategy: { type: Recreate }
  selector: { matchLabels: { app: scp } }
  template:
    metadata: { labels: { app: scp } }
    spec:
      securityContext: { runAsNonRoot: true, fsGroup: 65532, seccompProfile: { type: RuntimeDefault } }
      containers:
        - name: scp
          image: ghcr.io/cantara/source-code-portal:2.0.0
          args: [serve, --config, /etc/scp/scp.yaml]
          ports: [{ name: public, containerPort: 9090 }, { name: admin, containerPort: 9091 }]
          securityContext: { readOnlyRootFilesystem: true, allowPrivilegeEscalation: false, capabilities: { drop: [ALL] } }
          startupProbe: { httpGet: { path: /readyz, port: admin }, periodSeconds: 10, failureThreshold: 90 }
          readinessProbe: { httpGet: { path: /readyz, port: admin }, periodSeconds: 10 }
          livenessProbe: { httpGet: { path: /livez, port: admin }, periodSeconds: 20, failureThreshold: 3 }
          resources: { requests: { cpu: 100m, memory: 128Mi }, limits: { memory: 512Mi } }
          volumeMounts:
            - { name: data, mountPath: /data }
            - { name: config, mountPath: /etc/scp, readOnly: true }
            - { name: secrets, mountPath: /run/secrets, readOnly: true }
      volumes:
        - { name: data, persistentVolumeClaim: { claimName: scp-data } }
        - { name: config, configMap: { name: scp-config } }
        - { name: secrets, secret: { secretName: scp-secrets } }
```

Expose only the `public` port through your ingress. The ingress must pass `X-Forwarded-For` from an address in
`server.trusted_proxies` and keep request bodies up to 25 MiB for `/webhooks/github`.

## 4. Lifecycle

**Start.** A start loads the cache and serves it at once, then lists every organisation. On the first start (empty cache) the pages
say the portal is loading and fill in as data arrives; `/readyz` turns 200 once every source has a complete listing. A cold start of
1,000 repositories needs about 5,500 GitHub requests; with a token it can wait for the hourly rate-limit reset to finish.

**GitHub outages.** When GitHub fails or the rate limit runs out, the portal keeps serving the last good data, marks the affected
items *Delayed* or *Update failing*, and retries with backoff. `/readyz` stays 200.

**Stop.** SIGTERM or SIGINT: `/readyz` answers 503, in-flight requests and jobs get `server.shutdown_grace`, the cache is closed and the
process exits 0.

**Upgrade.** Replace the binary or the image tag and restart. The cache holds only what GitHub can give again: if it is lost, deleted or
unreadable, the portal rebuilds it like a first start. There is nothing to back up beyond the configuration and the secrets.

**Rotating the webhook secret.** Set the new secret in `webhook.secret_file` and the old one in `webhook.previous_secret_file`,
restart, change the secret on GitHub, then remove the previous one.

## 5. Monitor

| Endpoint (admin) | Answers |
|---|---|
| `/livez` | 200 while the process and its sync loop run; restart on failure |
| `/readyz` | 200 once a catalogue is loaded, 503 before and during shutdown |
| `/metrics` | Prometheus exposition |
| `/api/v1/status` | JSON for people: readiness, snapshot version and age, queue, per-source freshness and rate limit left, webhook deliveries, warnings |
| `/debug/pprof/` | Go profiles, when `ops.pprof` is true |

Metrics:

| Metric | Labels | Meaning |
|---|---|---|
| `scp_http_requests_total` | `listener`, `pattern`, `method`, `code` | requests served |
| `scp_http_request_duration_seconds` | `listener`, `pattern` | time to serve a request |
| `scp_webhook_deliveries_total` | `event`, `result` | webhook deliveries: `accepted`, `ignored`, `duplicate`, `ping`, `invalid_signature`, `malformed`, `too_large`, `unsupported_media_type` |
| `scp_sync_jobs_total` | `kind`, `result` | finished GitHub jobs: `ok`, `not_modified`, `error`, `rate_limited` |
| `scp_sync_job_duration_seconds` | `kind` | time a job ran |
| `scp_sync_queue_depth`, `scp_sync_running` | | jobs due and waiting, jobs running |
| `scp_github_requests_total` | `source`, `code` | GitHub responses by status |
| `scp_github_rate_remaining_ratio` | `source` | smallest fraction of the rate limit left |
| `scp_snapshot_age_seconds` | | time since the served data last changed |
| `scp_items` | `kind`, `freshness` | repositories by data kind and freshness |
| `scp_build_info` | `version`, `commit`, `goversion` | the running build |

Useful alerts: `scp_github_rate_remaining_ratio < 0.1` for 15 minutes; `scp_items{freshness="error"}` above zero for an hour;
an increase of `scp_webhook_deliveries_total{result="invalid_signature"}` (a wrong secret or a sender that is not GitHub).

Logs are JSON lines on stderr (`log.format: text` for people): one line per request with method, route, status and duration, startup
steps, and job failures. Secrets are never logged.

## 6. Troubleshooting

| Symptom | Look at |
|---|---|
| `config check` exits 2 | every problem is listed with its key; unknown keys and `SCP_*` variables are errors |
| `/readyz` stays 503 | `/api/v1/status` sources and warnings; the log for `401`/`403` (credential, App not installed, missing permission) |
| A repository is missing | it must be public, not a fork (unless `include_forks`), and in a configured organisation; a deleted or private repository disappears after two listings |
| Pushes show only after minutes | the webhook: GitHub's delivery log for the App or organisation, `scp_webhook_deliveries_total` by result |
| Pages show *Delayed* | `scp_github_rate_remaining_ratio`; with a token, consider a GitHub App |
