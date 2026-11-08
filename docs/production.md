# Production setup

The steps from nothing to a public portal at `https://scp.example.org` for a GitHub organisation (here `example`): one host with Docker, a reverse
proxy that holds the TLS certificate, a GitHub App, single sign-on for the people who see private repositories and the admin page, and
the assistant. The [operator guide](operator-guide.md) explains every key; Kubernetes and several instances are in its section 3.

What you need: a Linux host with Docker and ports 80 and 443 open, the DNS name of the portal (here `scp.example.org`) pointing at it, owner rights on the
GitHub organisation, an administrator at the organisation's OpenID Connect provider, and an API key of a language model
provider for the assistant.

## 1. Secrets

Make a directory on the host that only root and the container user (UID 65532) read, and keep everything secret in it. As root:

```sh
install -d -m 0750 -g 65532 /srv/scp/secrets && cd /srv/scp/secrets
openssl rand -hex 32 > webhook-secret                 # shared with GitHub (step 2)
head -c 32 /dev/urandom | base64 > session-key        # seals the sign-in cookies
chgrp 65532 * && chmod 0640 *
```

The later steps add `scp-app.pem` (GitHub App key), `oidc-secret` (sign-in client secret), `llm-key` (assistant) and any access tokens
(`openssl rand -hex 32`, one file each). None of these go into git or the configuration file.

## 2. GitHub App

In GitHub, the organisation's *Settings*, *Developer settings*, *GitHub Apps*, *New GitHub App*:

- Name: `Example Source Code Portal`; homepage `https://scp.example.org`.
- Webhook: active, URL `https://scp.example.org/webhooks/github`, secret: the contents of `webhook-secret`.
- Repository permissions, all read-only: *Contents*, *Metadata*, *Issues*, *Pull requests*, *Checks*, *Commit statuses*. Nothing else.
- Subscribe to events: *Push*, *Release*, *Repository*, *Issues*, *Pull request*, *Pull request review*. Not *Issue comment*, *Check
  suite* or *Status*: they fire on every comment and CI update, and the portal does not need them.
- Where can it be installed: *Only on this account*.

Each permission serves a feature: *Contents* and *Metadata* the catalogue, *Issues* the open issues, *Pull requests*, *Checks* and
*Commit statuses* the open pull requests and whose turn each is. Without one, that feature stays empty, and the portal says so on its
page and on the *Admin* page (operator guide, section 1). A portal that should do without pull requests or issues leaves their
permissions and events out and switches the feature off in `scp.yaml`: `features: {pulls: false}` or `{issues: false}`; then nothing
warns. Changing them later needs the installation to accept them: an organisation
owner, Organization settings, GitHub Apps, *Review request*.

Create it, note the *App ID*, generate a private key and store it as `/srv/scp/secrets/scp-app.pem` (group 65532, mode 0640). Then
*Install App* on the organisation, *All repositories* (or the ones the portal shows).

## 3. Sign-in

Register the portal at the organisation's OpenID Connect provider (Google Workspace, Entra ID, Keycloak, ...) as a web application:

- Redirect URI: `https://scp.example.org/auth/callback`; sign-out redirect: `https://scp.example.org/`.
- Scopes `openid email profile`, and a `groups` claim when the provider has groups (the claim's name is `auth.oidc.groups_claim`).

Store the client secret as `/srv/scp/secrets/oidc-secret`. Decide who sees private repositories (`access.private_groups`) and who is
an administrator (`auth.admin_emails` or `auth.admin_groups`). Without groups from the provider, `private_groups: ["*"]` admits everyone
who signs in.

## 4. The configuration

`/srv/scp/scp.yaml` (not secret: it names files, never their contents):

```yaml
server:
  public_url: https://scp.example.org
  trusted_proxies: [172.30.0.0/24]          # the compose network of step 5: the reverse proxy
log:
  level: info
store:
  path: /data/scp.db
github:
  sources:
    - id: github
      orgs: [example]
      app: { id: 123456, private_key_file: /run/secrets/scp-app.pem }   # the App ID of step 2
      include_private: true                  # only for viewers in access.private_groups
access:
  private_groups: [developers]       # a group of the provider, or ["*"]
auth:
  session_key_file: /run/secrets/session-key
  admin_emails: [<administrator>@example.org] # the administrators' verified addresses
  oidc:
    issuer: https://accounts.google.com      # the provider of step 3
    client_id: <client id>
    client_secret_file: /run/secrets/oidc-secret
    scopes: [openid, email, profile]
llm:
  provider: anthropic
  api_key_file: /run/secrets/llm-key
assistant:
  access: signed-in
webhook:
  secret_file: /run/secrets/webhook-secret
catalog:
  groups:
    - { id: identity, name: Identity, description: Sign-in and access management, default_repo: identity-service,
        match: [{ prefix: identity- }] }
    - { id: other, name: Other, default_repo: handbook, match: [{ unclaimed: true }] }
```

Shape the groups before going live: list the organisation's repositories and see where each lands (the operator guide, section 2):

```sh
gh api --paginate "orgs/example/repos?per_page=100" > /srv/scp/repos.json
portal() { docker run --rm -v /srv/scp:/srv/scp:ro -v /srv/scp/secrets:/run/secrets:ro ghcr.io/cantara/source-code-portal:2.0.0 "$@"; }
portal config groups -c /srv/scp/scp.yaml --repos /srv/scp/repos.json
portal config check -c /srv/scp/scp.yaml
```

`config check` must exit 0; it prints the effective settings with the secrets redacted.

## 5. Run

`/srv/scp/compose.yaml`, with Caddy as the reverse proxy (it gets and renews the certificate for `scp.example.org` itself):

```yaml
services:
  scp:
    image: ghcr.io/cantara/source-code-portal:2.0.0
    command: [serve, --config, /etc/scp/scp.yaml]
    restart: unless-stopped
    read_only: true
    cap_drop: [ALL]
    volumes:
      - scp-data:/data
      - ./scp.yaml:/etc/scp/scp.yaml:ro
      - ./secrets:/run/secrets:ro
    ports: ["127.0.0.1:9091:9091"]           # admin: health and metrics, on the host only
    networks: [edge]
  caddy:
    image: caddy:2
    restart: unless-stopped
    ports: ["80:80", "443:443"]
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
    networks: [edge]
networks:
  edge:
    ipam: { config: [{ subnet: 172.30.0.0/24 }] }
volumes:
  scp-data:
  caddy-data:
```

`/srv/scp/Caddyfile`:

```
scp.example.org {
	encode zstd gzip
	reverse_proxy scp:9090
}
```

Caddy passes `X-Forwarded-For` and streams the server-sent events (`/api/v1/events`, the assistant's answers) without buffering.
Start it and follow the log:

```sh
cd /srv/scp && docker compose up -d && docker compose logs -f scp
```

The first start reads every repository (with the App, a few minutes for a few hundred); the pages say the portal is loading meanwhile.

## 6. Check

- `curl -s localhost:9091/readyz` answers 200, and `curl -s localhost:9091/api/v1/status | jq .warnings` is empty.
- `https://scp.example.org` shows the dashboard with the groups; a private repository is absent while signed out.
- *Sign in*, then the private repositories show, and an administrator sees *Admin* in the header with the portal's status.
- On *Admin*, *Test the provider* answers with the time it took; *Ask* in the header answers a question with links into the portal.
- Push to a repository: the commit shows within seconds; GitHub's App settings, *Advanced*, list the deliveries with status 202.
- A badge, for example `https://scp.example.org/badges/groups/identity/health.svg`, renders in a README.

## 7. Operate

- **Upgrade**: change the image tag in `compose.yaml` and run `docker compose up -d`; the cache in `scp-data` carries over and is
  rebuilt from GitHub when it cannot.
- **Back up**: `scp.yaml`, `compose.yaml`, `Caddyfile` and `secrets/`, and the store (`scp-data`) if administrators save layouts or announcements in the
  portal: it holds them, and the rest of it comes back from GitHub (operator guide, section 4, *The store*).
- **Monitor**: scrape `localhost:9091/metrics`; the operator guide, section 5, lists the alerts worth having.
- **Rotate** the webhook secret with `webhook.previous_secret_file` (operator guide, section 4); replacing `session-key` signs everyone
  out; removing a token from `auth.tokens` revokes it at the restart.
