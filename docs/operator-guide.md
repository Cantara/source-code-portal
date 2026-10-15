# Operator guide

Source Code Portal (SCP) is one static binary, `scp`, and a web UI bundle it runs with Node.js. It reads the repositories of one or more GitHub
organisations and user accounts, keeps a local SQLite cache and serves the portal. One instance serves up to 1,000 repositories; several instances
can serve one portal together through Hazelcast (section 3.1).

- Public listener `:9090`: the web UI, the JSON API `/api/v1`, badges, `/llms.txt` and the GitHub webhook. Put it behind your proxy.
- Admin listener `:9091`: `/livez`, `/readyz`, `/metrics`, `/api/v1/status` and optional profiling. Keep it inside the cluster.

## 1. Connect GitHub

SCP needs read access to the repositories of the organisations and user accounts it shows. Use a GitHub App (recommended: its own rate limit of up to 12,500
requests an hour per installation) or a personal access token (5,000 an hour).

**GitHub App.** Create an App in the organisation's settings, or for a user account in your own (Developer settings, GitHub Apps):
1. Repository permissions: *Contents* read-only and *Metadata* read-only. Nothing else.
2. Webhook: active, URL `https://<portal>/webhooks/github`, a random secret (for example `openssl rand -hex 32`), and subscribe to the
   events *Push*, *Release* and *Repository*. Installation events are sent to Apps without subscribing.
3. Generate a private key (a `.pem` file) and note the App ID.
4. Install the App on each organisation or user account, on all repositories (or the ones to show). An App that may be installed only on
   its own account ("Only on this account") needs one App per account: give each its own source, with the same webhook URL and secret.

**Token.** A fine-grained token with public repository read access, or a classic token without scopes, in a file. Listed with a token, a
user account shows its public repositories, or all of them, private ones included, when it is the token's own account (the token needs
read access to them). For updates within seconds, add an organisation webhook (for a user account, a webhook on each repository) (Settings, Webhooks) with content type `application/json`, the same secret and the events *Pushes*,
*Releases* and *Repositories*.

Without a webhook the portal still refreshes: it lists every account every five minutes and notices pushes from that listing;
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
| `github.sources[].orgs` | required | organisations and user accounts read with this credential; an account belongs to one source |
| `github.sources[].app.id` | | GitHub App ID |
| `github.sources[].app.private_key_file` | | the App's private key (PEM) |
| `github.sources[].app.installations` | all | account logins whose installations are used |
| `github.sources[].token_file` | | a token file, instead of `app` |
| `github.sources[].include_forks` | `false` | list forks |
| `github.sources[].include_archived` | `true` | list archived repositories |
| `github.sources[].exclude` | `[]` | repositories kept out of the catalogue even where the credential reads them: matchers as in the groups (`exact`, `prefix`, `glob`, `regexp`, `topic`) |
| `github.sources[].include_private` | `false` | list private and internal repositories too (section 2.1) |
| `catalog.reconcile_interval` | `5m` | how often every account is listed (1m to 24h) |
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
| `access.require_auth` | `false` | no public view: an anonymous request gets 401 (a browser's page request goes to `/signin`), except sign-in, `/api/v1/me`, `/webhooks/` and `robots.txt` (which then disallows everything) |
| `auth.session_key_file` | empty | at least 32 random bytes that seal the sign-in cookies; the same file on every instance; needed for sign-in (section 2.2) |
| `auth.session_ttl` | `12h` | how long a sign-in lasts, `5m` to `720h`; it is renewed while in use |
| `auth.admin_emails` | `[]` | verified email addresses of the administrators (case-insensitive) |
| `auth.admin_groups` | `[]` | groups whose members are administrators |
| `auth.tokens[]` | `[]` | access tokens: `name` (`[a-z0-9-]`), `secret_file` (at least 32 characters), `groups`, `admin` |
| `auth.oidc.issuer` | empty | the OpenID Connect provider (https; http only on loopback); empty: no single sign-on |
| `auth.oidc.client_id` | empty | the portal's client at the provider |
| `auth.oidc.client_secret_file` | empty | the client's secret; empty for a public client (PKCE only) |
| `auth.oidc.scopes` | `[openid, email, profile]` | the scopes asked for; `openid` is required |
| `auth.oidc.groups_claim` | `groups` | the claim, in the ID token or from userinfo, that lists the user's groups |
| `llm.provider` | empty | the assistant's language model provider: `anthropic`, `openai`, `gemini`, `openrouter` or `ollama`; empty: no assistant (section 2.3) |
| `llm.model` | the provider's | the model, for example `claude-haiku-5-5` (anthropic), `gpt-4o` (openai) |
| `llm.base_url` | the provider's | the provider's API base (https; http only on loopback or without a key), for a proxy or a local Ollama |
| `llm.api_key_file` | empty | the provider's API key; required except for `ollama` |
| `assistant.access` | `signed-in` | who may ask: `signed-in`, `admin` or `everyone` |
| `assistant.concurrency` | `4` | answers at once per instance; more are refused with 429 |
| `assistant.max_turns` | `8` | model turns for one answer (1 to 32); the last turn must answer without tools |
| `assistant.max_tool_calls` | `16` | tool calls for one answer (0 to 64) |
| `assistant.max_tokens` | `16000` | tokens for one answer, input and output, before the last turn (256 to 200,000) |
| `assistant.max_history` | `40` | messages of a conversation (1 to 200) |
| `assistant.max_input_chars` | `60000` | characters of a conversation (1,000 to 1,000,000) |
| `assistant.tool_result_chars` | `12000` | characters of one tool result given to the model (1,000 to 200,000) |
| `webhook.secret_file` | empty | webhook secret; the receiver is served only when set |
| `webhook.previous_secret_file` | empty | the old secret, accepted too while you rotate |
| `cluster.addresses` | `[]` | Hazelcast members `host:port`; set to run as one of several instances (section 3.1) |
| `cluster.name` | `dev` | the Hazelcast cluster name |
| `cluster.namespace` | `scp` | prefix of the Hazelcast objects, `[a-z0-9-]`, so several portals can share one Hazelcast cluster |
| `web.dir` | empty | the web UI bundle (the directory holding `server.js`); empty: the pages answer 503, the API, badges and Markdown still work |
| `web.node` | `node` | the Node.js 24 executable that runs the bundle |
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

Try the groups before you deploy them: `scp config groups` shows which group each repository lands in, the repositories no group
selects and those the source filters leave out, from a listing of the accounts (it does not call GitHub itself):

```sh
gh api --paginate "orgs/Cantara/repos?per_page=100" > repos.json   # or one repository name per line
gh api --paginate "user/repos?affiliation=owner&visibility=all&per_page=100" >> repos.json   # your own user account
scp config groups -c scp.yaml --repos repos.json
```

### 2.1 Private repositories

The portal can show private and internal repositories to some people and keep them from everyone else. It learns who someone is from
its own sign-in (section 2.2) or from your authenticating proxy (oauth2-proxy, an OIDC ingress, an SSO gateway), which it believes.
The steps below are for a proxy; with the portal's sign-in, step 2 needs no `server.trusted_proxies` and step 3 falls away.

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
varies on the two headers. `config check` refuses `include_private` without `access.private_groups` and a way to know the viewer: `server.trusted_proxies`,
`auth.oidc` or `auth.tokens`. `serve` and `config check` warn when a range in `server.trusted_proxies` is wider than /24 (IPv4) or /64
(IPv6) while private repositories are served on an address other than loopback: any host in it could claim any user.
Images in private READMEs load from GitHub and show for users signed in there.

**Signed-in viewers only.** For a portal whose repositories are nearly all private, set `access.require_auth: true`: there is no
public view, and every page, API answer, badge, image, Markdown and `llms`/KCP file, the sitemap and the event stream answer only an
identified viewer (the proxy's headers, a session or a token). Badges then show in a README only to readers signed in to the portal.

**Public and private on one address.** Visitors without a session should reach the portal directly and see the public repositories,
while signed-in users pass the authenticating proxy. With Traefik and oauth2-proxy: one router sends ``Host(`scp.example.org`)`` to the portal, a
higher-priority router sends ``Host(`scp.example.org`) && (PathPrefix(`/oauth2`) || HeaderRegexp(`Cookie`, `(^|;\s*)_oauth2_proxy(_[0-9]+)?=`))``
to oauth2-proxy, whose upstream is the portal. Let oauth2-proxy reach the portal over a network of its own and put only that network in
`server.trusted_proxies`, so identity headers that arrive through Traefik are dropped. Sign in at `/oauth2/start?rd=/`, sign out at
`/oauth2/sign_out?rd=/`.

### 2.2 Sign-in and administrators

The portal signs people in itself through your organisation's OpenID Connect provider (Entra ID, Google, Okta, Keycloak, Authentik,
...) and lets scripts and people without the provider in with access tokens. Nothing about a user is stored: a sign-in is a cookie
sealed with `auth.session_key_file`, so every instance with the same key file accepts it. Replacing the key signs everyone out.

1. Make the key: `head -c 32 /dev/urandom | base64 > /run/secrets/scp-session-key` (keep it with the other secrets).
2. Register the portal at the provider as a web application with the redirect URI `<server.public_url>/auth/callback` and the
   sign-out redirect `<server.public_url>/`; have it put the user's groups in a claim (`groups` unless you set `auth.oidc.groups_claim`).
3. Configure it:

```yaml
server:
  public_url: https://scp.example.org
access:
  private_groups: [engineering]
auth:
  session_key_file: /run/secrets/scp-session-key
  admin_emails: [ops@example.org]
  oidc:
    issuer: https://login.example.org
    client_id: source-code-portal
    client_secret_file: /run/secrets/scp-oidc-secret
    scopes: [openid, email, profile, groups]
  tokens:
    - { name: ci, secret_file: /run/secrets/scp-token-ci, groups: [engineering] }
```

**Sign in** from the header's *Sign in*: `/signin` offers single sign-on and, when tokens are configured, a form that takes a token. A
failed attempt comes back to `/signin` with what went wrong. *Sign out* ends the portal's session and the provider's.

**Roles** follow from the configuration on every request, so a change applies at once: a member of `access.private_groups` sees private
repositories; an administrator (a verified email in `auth.admin_emails`, a member of `auth.admin_groups`, or a token with `admin: true`)
sees them too and the *Admin* page with the portal's status. A session keeps only the groups those lists name.

**Tokens** are for API clients and agents: `Authorization: Bearer <token>` on any request reads as the token's groups (`curl -H
"Authorization: Bearer $(cat token)" https://scp.example.org/llms-full.txt`); a wrong token is refused with 401, never served as
anonymous. Remove a token from the configuration to revoke it, the sessions it opened included.

Behind a proxy that names its users, both work: a page from the portal's web UI, then a token or a sign-in cookie, then the proxy's
headers decide who the viewer is.

### 2.3 The assistant

The portal can answer questions about its catalogue in plain language on its *Ask* page (`/ask`): which repositories use a library,
what a group holds, what changed lately, why a repository's health is low. A language model reads the catalogue through read-only
tools (search the repositories, list and read groups, read a repository's README and technology, read a document, the recent commits,
a repository's health) and answers in Markdown with links into the portal. It sees exactly what the person asking may see: a private
repository only for a viewer who sees private repositories.

Nothing is stored: the conversation lives in the browser's page and is sent with each question. What the tools read is sent to the
provider you configure, so choose one your organisation may send its repository contents to (for private repositories especially).

1. Get an API key from the provider and put it in a file, for example `/run/secrets/scp-llm-key`.
2. Configure it:

```yaml
llm:
  provider: anthropic                 # openai, gemini, openrouter, or ollama (no key) for a model on your own machines
  model: claude-haiku-5-5             # optional: the provider's default
  api_key_file: /run/secrets/scp-llm-key
assistant:
  access: signed-in                   # admin, or everyone (also anonymous visitors: mind the provider's bill)
```

3. Restart, sign in as an administrator and press *Test the provider* on the *Admin* page: it asks the model for one short completion
   and shows the time it took or the provider's error.

The header shows *Ask* to the viewers `assistant.access` admits. `signed-in` and `admin` need a way to know the viewer (section 2.2 or
a proxy, section 2.1). Each answer is bounded by the `assistant.*` budgets; `assistant.concurrency` limits the answers in progress on
one instance. Everything a tool returns is marked to the model as repository content written by others, never instructions. Watch the
cost with `scp_assistant_tokens_total` (section 5).

## 3. Run

**Binary.** Download the archive for your platform and the web UI bundle (`scp-web_2.0.0.tar.gz`, the same for every platform) from
the GitHub release, check them and run them with Node.js 24 on the `PATH` (or `web.node`):

```sh
sha256sum -c scp_2.0.0_checksums.txt --ignore-missing
tar xzf scp_2.0.0_linux_amd64.tar.gz && tar xzf scp-web_2.0.0.tar.gz   # the bundle unpacks into bundle/
./scp config check -c scp.yaml && SCP_WEB_DIR=bundle ./scp serve -c scp.yaml
```

`scp serve` starts the bundle's server (`node server.js`) on a free loopback port, passes it nothing from its own environment,
restarts it when it exits or when the bundle is replaced (so a new bundle can be unpacked in place), and stops it at shutdown; its output appears in the log with `component=web`. Every page goes through `scp`,
so the public listener stays the one address to expose.

**Container.** The image `ghcr.io/cantara/source-code-portal` (linux/amd64 and linux/arm64) holds `scp`, Node.js and the bundle
(`SCP_WEB_DIR` and `SCP_WEB_NODE` are set). It runs as UID 65532 on a read-only root file system; `/data` is the one writable path.

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
          resources: { requests: { cpu: 100m, memory: 256Mi }, limits: { memory: 768Mi } }
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

**Start.** A start loads the cache and serves it at once, then lists every account and starts the web UI server. On the first start (empty cache) the pages
say the portal is loading and fill in as data arrives; `/readyz` turns 200 once every source has a complete listing and the web UI server answers. A cold start of
1,000 repositories needs about 5,500 GitHub requests; with a token it can wait for the hourly rate-limit reset to finish.

**GitHub outages.** When GitHub fails or the rate limit runs out, the portal keeps serving the last good data, marks the affected
items *Delayed* or *Update failing*, and retries with backoff. `/readyz` stays 200.

**Stop.** SIGTERM or SIGINT: `/readyz` answers 503, in-flight requests and jobs get `server.shutdown_grace`, the web UI server is
stopped, the cache is closed and the process exits 0.

**Upgrade.** Replace the binary and the bundle together (or the image tag) and restart. The cache holds only what GitHub can give again: if it is lost, deleted or
unreadable, the portal rebuilds it like a first start. There is nothing to back up beyond the configuration and the secrets.

**Rotating the webhook secret.** Set the new secret in `webhook.secret_file` and the old one in `webhook.previous_secret_file`,
restart, change the secret on GitHub, then remove the previous one.

**Replacing the Java portal.** On the same host name, the Java portal's addresses keep working: `/dashboard`, `/group/{id}`,
`/contents/...` and `/commits/...` redirect (308) to the new pages, and the badges in other repositories' READMEs
(`/badge/license|shields-issues|shields-commits|shields-releases/{repo}/{branch}`) to `/badges/repos/{owner}/{repo}/...svg`. A group is
found by its old id with case and dashes ignored, a repository by its name. Jenkins and Snyk badges and the wiki answer 410 Gone. The old webhook
`POST /github/webhook` answers 410: point the GitHub webhook at `/webhooks/github` with a new secret (SHA-256 signatures).

## 5. Monitor

| Endpoint (admin) | Answers |
|---|---|
| `/livez` | 200 while the process and its sync loop run; restart on failure |
| `/readyz` | 200 once a catalogue is loaded, 503 before and during shutdown |
| `/metrics` | Prometheus exposition |
| `/api/v1/status` | JSON for people: readiness, snapshot version and age, queue, per-source freshness and rate limit left, webhook deliveries, the cluster role, warnings |
| `/debug/pprof/` | Go profiles, when `ops.pprof` is true |

**Audit.** Every request from a viewer who sees private repositories leaves a log line with the message `audit`: `user`, `via`
(`proxy`, `oidc`, `token`), `method`, `pattern`, `path`, `status` and `request_id`, never the answer. Static assets and the web UI
server's own calls are left out. Ship these lines where your access records go.

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
| `scp_assistant_requests_total` | `outcome` | assistant questions: `answered`, `refused` (access, validation, busy), `budget`, `aborted` (the viewer left) or the provider's error (`auth`, `rate_limited`, `overloaded`, `bad_request`, `tools_unsupported`, `network`, `provider`) |
| `scp_assistant_tokens_total` | `kind` | language model tokens the assistant used, `input` and `output` |
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
| A repository is missing | it must be public, not a fork (unless `include_forks`), not matched by the source's `exclude`, and in a configured account; a private repository disappears at the next listing, a deleted one after two (the second runs 30 seconds after the first misses it) |
| Pushes show only after minutes | the webhook: GitHub's delivery log for the App, organisation or repository, `scp_webhook_deliveries_total` by result |
| The assistant answers with an error | *Test the provider* on the *Admin* page; `scp_assistant_requests_total` by outcome; the log's `assistant provider` lines carry the provider's message |
| Pages show *Delayed* | `scp_github_rate_remaining_ratio`; with a token, consider a GitHub App |
