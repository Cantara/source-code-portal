# Operator guide

Source Code Portal (SCP) is one static binary, `scp`, with the web UI embedded. It reads the repositories of one or more GitHub
organisations, keeps a local SQLite cache and serves the portal. One instance serves up to 1,000 repositories; several instances
can serve one portal together through Hazelcast (section 3.1).

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
| `github.sources[].include_private` | `false` | list private and internal repositories too (section 2.1) |
| `catalog.reconcile_interval` | `5m` | how often every organisation is listed (1m to 24h) |
| `catalog.groups[]` | none | groups in dashboard order (at most 500) |
| `catalog.groups[].id` | required | URL id, `[a-z0-9-]`, at most 63 characters |
| `catalog.groups[].name` | required | display name, at most 120 characters |
| `catalog.groups[].description` | empty | at most 500 characters |
| `catalog.groups[].default_repo` | required | the group's main repository name; a missing one is shown on the group, the group stays |
| `catalog.groups[].sources` | all | source ids the group draws from |
| `catalog.groups[].match[]` | required | 1 to 64 matchers; a repository in any matcher is a member |
| `access.user_header` | `X-Forwarded-User` | header in which the authenticating proxy names the user |
| `access.groups_header` | `X-Forwarded-Groups` | header with the user's groups, comma-separated |
| `access.private_groups` | `[]` | groups whose members see private repositories; `"*"` is every authenticated user |
| `webhook.secret_file` | empty | webhook secret; the receiver is served only when set |
| `webhook.previous_secret_file` | empty | the old secret, accepted too while you rotate |
| `cluster.addresses` | `[]` | Hazelcast members `host:port`; set to run as one of several instances (section 3.1) |
| `cluster.name` | `dev` | the Hazelcast cluster name |
| `cluster.namespace` | `scp` | prefix of the Hazelcast objects, `[a-z0-9-]`, so several portals can share one Hazelcast cluster |
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

### 2.1 Private repositories

The portal can show private and internal repositories to people your authenticating proxy (oauth2-proxy, an OIDC ingress, an SSO
gateway) lets through, and keep them from everyone else. The portal does not log anyone in; it believes the proxy.

1. Give the credential access: a GitHub App installed on the repositories (its *Contents* and *Metadata* read permissions cover private
   ones), or a token that can read them.
2. Set `include_private: true` on the source, the proxy's address in `server.trusted_proxies`, and the permitted groups:

```yaml
server:
  trusted_proxies: [10.0.0.0/8]
access:
  private_groups: [engineering]       # or ["*"] for every authenticated user
github:
  sources:
    - { id: github, orgs: [Cantara], app: { id: 123456, private_key_file: /run/secrets/scp-app.pem }, include_private: true }
```

3. Have the proxy pass the user in `X-Forwarded-User` and the groups in `X-Forwarded-Groups` (or name other headers with
   `access.user_header` and `access.groups_header`).

A request whose user is in one of `access.private_groups` sees everything; any other request sees the public repositories only, on
every page, in the API, badges, `/llms.txt` and the Markdown answers. The two headers are believed only from an address in
`server.trusted_proxies` and dropped from any other. Answers to a permitted user are marked `Cache-Control: private`, and every answer
varies on the two headers. `config check` refuses `include_private` without `access.private_groups` and `server.trusted_proxies`.
Images in private READMEs load from GitHub and show for users signed in there.

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
`server.trusted_proxies`, keep request bodies up to 25 MiB for `/webhooks/github`, and pass `/api/v1/events` unbuffered with a read
timeout above 30 seconds (the stream sends a comment every 25 seconds; nginx honours the `X-Accel-Buffering: no` it sends).

**Outbound.** Besides GitHub, the portal fetches the external images documents show (`https` on port 443, public addresses only, at
most 5 MiB, cached in memory for an hour). Where it may not reach the internet those images show their alt text.

### 3.1 Several instances

Two or more instances serve one portal when they share a Hazelcast cluster (Hazelcast 5, the open-source edition is enough). One
instance leads: it alone calls GitHub, and every instance applies what it records, so all of them serve the same data with the same
ETags and the load balancer may send any request, webhooks included, to any of them. A webhook delivery is de-duplicated across the
instances. When the leader stops, another takes over within a few seconds (within about a minute when it crashed: Hazelcast's
`hazelcast.client.max.no.heartbeat.seconds`).

```yaml
cluster:
  addresses: ["hazelcast-0.hazelcast:5701", "hazelcast-1.hazelcast:5701"]
  name: dev          # the server's cluster-name
```

- Give every instance the same configuration and secrets; only `store.path` is each instance's own. A new instance fills its cache from
  the cluster, so in Kubernetes the cache may be an `emptyDir`: drop the claim and `Recreate` from the Deployment above, set `replicas`
  and use the default rolling update.
- Hazelcast holds the portal's data (private repositories included when they are admitted) and has no authentication in the
  open-source edition: run it on a private network that only the instances reach.
- When Hazelcast is unreachable every instance keeps serving its last good data and stays ready, but nothing is refreshed until the
  connection is back; `/api/v1/status` warns.
- Each instance reads the images it serves itself, and its event stream announces its own snapshots; equal data has the same version on
  every instance.

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
| `/api/v1/status` | JSON for people: readiness, snapshot version and age, queue, per-source freshness and rate limit left, webhook deliveries, the cluster role, warnings |
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
| `scp_cluster_leader` | | 1 on the instance that leads the cluster, 0 on the others (only with `cluster.addresses`) |
| `scp_event_streams` | | open `/api/v1/events` streams (at most 1,000 per instance; beyond that browsers poll) |

Useful alerts: `scp_github_rate_remaining_ratio < 0.1` for 15 minutes; `scp_items{freshness="error"}` above zero for an hour;
an increase of `scp_webhook_deliveries_total{result="invalid_signature"}` (a wrong secret or a sender that is not GitHub);
with several instances, `sum(scp_cluster_leader) != 1` for 5 minutes. On the instances that do not lead the queue metrics are 0.

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
