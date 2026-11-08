---
name: run-scp
description: Set up, configure, deploy, upgrade and troubleshoot a Source Code Portal (SCP) from its released binary or container image (ghcr.io/cantara/source-code-portal) - the scp.yaml (GitHub App or token, organisations, product groups with prefix, glob, regexp, exact and topic matchers, webhooks, private repositories, OpenID Connect sign-in, access tokens, the assistant, a theme with the owner's colours, logo and name, several instances), the secrets, Docker Compose with a reverse proxy, Kubernetes or a plain binary, and checking it works. Use when someone wants to run their own SCP, change its configuration or groups, upgrade it, or find out why it misbehaves.
---

# Run Source Code Portal

You help someone run Source Code Portal (SCP) for their own GitHub organisations: from their wishes ("a portal for our org", "put
the payment repos together", "only staff see private repositories") to a configuration that passes the portal's own checks, a
deployment, and a portal that answers. They need not know key names; you do, and you explain the result in their words.

SCP is one static binary, `scp`, that serves the API and runs the web UI bundle with Node.js 26 (24 also runs it); the container image holds both. It
reads GitHub, keeps a SQLite cache that it can always rebuild, and serves the public listener `:9090` (the portal) and the admin
listener `:9091` (`/readyz`, `/livez`, `/metrics`, `/api/v1/status`).

## Ground rules

- **The release decides.** Settle the version first: the one running (`scp version`, the image tag, or the header of the portal), or
  for a new portal the latest release (`gh release view -R Cantara/source-code-portal --json tagName -q .tagName`). Read that
  release's documents, not your memory, before you write a key: `https://raw.githubusercontent.com/Cantara/source-code-portal/<tag>/docs/operator-guide.md`
  (every key and its default, running, monitoring, troubleshooting), `docs/production.md` (one host from nothing),
  `docs/security-model.md` and `docs/repository-guide.md`. The portal refuses unknown keys, so a key from another release fails.
  [references/essentials.md](references/essentials.md) summarises them for when you cannot fetch them; the documents win.
- **Secrets are files.** The configuration names files (`token_file`, `app.private_key_file`, `webhook.secret_file`,
  `auth.session_key_file`, `auth.oidc.client_secret_file`, `llm.api_key_file`, token `secret_file`s). Make them with the commands
  below, owned by the container user (UID 65532) with mode 0640; never read, print, paste or commit their contents, and never put a
  secret in the YAML or an environment variable.
- **Read-only on GitHub.** The portal needs *Contents* and *Metadata* read. Never ask for more, never change repositories. Listing
  repositories with `gh api` to shape the groups is fine.
- **Ask before you act outside the files.** Writing `scp.yaml`, compose files and manifests is yours to do; starting, stopping or
  upgrading a running portal, applying to a cluster, creating a GitHub App and changing DNS or a proxy are the owner's to approve.
- **Prove, then say done.** After every change run `scp config check` (exit 0) and, when groups changed, `scp config groups`. Show
  what changed in plain language.

## Running the release's own tools

With the image, no installation is needed:

```sh
scp() { docker run --rm -v "$PWD:/work:ro" -w /work ghcr.io/cantara/source-code-portal:<version> "$@"; }
```

Mount the secrets directory at the paths the configuration names (for example `-v "$PWD/secrets:/run/secrets:ro"`) so the check can
open them. With the binary: download `scp_<version>_<os>_<arch>.tar.gz` and `scp-web_<version>.tar.gz` from the release, verify
them with `sha256sum -c scp_<version>_checksums.txt --ignore-missing`, unpack.

- `scp config check -c scp.yaml`: validates exactly as `serve` does, lists every problem with its key, prints the effective settings
  with secrets redacted. Exit 0 valid, 2 not.
- `scp config groups -c scp.yaml --repos repos.json`: which group each repository lands in, the ones no group selects and the ones the
  source filters leave out, from a listing (no GitHub call): `gh api --paginate "orgs/<org>/repos?per_page=100" > repos.json`
  (a user account: `users/<login>/repos`, or `user/repos?affiliation=owner&visibility=all` for your own).
- `scp version`.

## The way through

1. **Find out what they want.** Ask only what you cannot infer, in one go: the GitHub organisations or user accounts; where it runs
   (one host with Docker, Kubernetes, a binary on a server) and its public address; whether private repositories are shown and to
   whom; sign-in (their OpenID Connect provider) and who the administrators are; the assistant and its model provider; webhooks
   (yes, when the portal has a public address). Offer the defaults: a GitHub App, one host with Docker Compose and Caddy, public
   repositories only, webhooks on.
2. **Access to GitHub.** A GitHub App is best (its own rate limit, webhooks, private repositories by installation): *Contents*,
   *Metadata*, *Issues*, *Pull requests*, *Checks* and *Commit statuses* read-only, webhook URL `<public_url>/webhooks/github` with a
   random secret, events *Push*, *Release*, *Repository*, *Issues*, *Pull request*, *Pull request review* (not *Issue comment*,
   *Check suite* or *Status*: every comment and CI update, not needed), installed on the
   accounts. Each permission serves a feature, and a missing one leaves it empty (the table is the operator guide's section 1):
   *Contents* and *Metadata* the catalogue, *Issues* `/issues`, *Pull requests*, *Checks* and *Commit statuses* `/pulls`. Tell
   them that the portal only reads (every permission *Read-only*) and that a later change must be accepted on each installation by an
   organisation owner (Organization settings, GitHub Apps, *Review request*). If they do without pull requests or issues, leave
   those permissions and events out and set `features: {pulls: false}` or `{issues: false}` (released after 2026-11-06; check the
   operator guide of their version): the feature is then gone, not empty, and nothing warns. A token (fine-grained, public repositories read; or a
   classic one without scopes) is fine to start.
   Walk them through the GitHub pages; they create it.
3. **Secrets.** In a directory only root and UID 65532 read:
   ```sh
   install -d -m 0750 -g 65532 secrets && cd secrets
   openssl rand -hex 32 > webhook-secret                # the same value goes into the GitHub App's webhook
   head -c 32 /dev/urandom | base64 > session-key       # only with sign-in
   chgrp 65532 * && chmod 0640 *
   ```
   They add `scp-app.pem` (or `github-token`), `oidc-secret` and `llm-key` themselves.
4. **The groups.** Groups are the products on the dashboard, in order. Turn their words into matchers (each has exactly one field):
   `prefix: payments-`, `glob: "*-service"`, `regexp: "^(api|web)-.*$"` (RE2, whole name, case-sensitive unless `(?i)`),
   `exact: handbook`, `topic: payments`, `catch_all: true`, `unclaimed: true` (what no other group took: give the last group this
   so nothing is homeless). Each group needs an `id` (`[a-z0-9-]`), a `name`, a `default_repo` (its main repository) and 1 to 64
   matchers; keep their own names and descriptions. Keep repositories out with the source's `exclude` (same matchers); forks are hidden
   unless `include_forks`, a group names one (as its `default_repo` or with `exact`) or the reader asks. Run `scp config groups` and show the landing in a small table; iterate until it matches their picture.
5. **The configuration.** Start from the release's `deploy/config.example.yaml` or the starter in
   [references/essentials.md](references/essentials.md). Set `server.public_url` (absolute links, HSTS on https) and
   `server.trusted_proxies` (only the reverse proxy's network). Add only the sections they asked for: `webhook`, `access` and
   `include_private`, `auth` (OIDC: redirect URI `<public_url>/auth/callback`, sign-out redirect `<public_url>/`), `llm` and
   `assistant`, `announcements[]` (the deployment's own, locked in Admin), `web.theme_dir` and `web.product_name` (their colours, logo and
   name: [references/theme.md](references/theme.md), for a release after v2.2.10), `cluster` (several instances through
   Hazelcast). `config check` until it passes.
6. **Deploy.** Write the files for their target from [references/deploy.md](references/deploy.md): Docker Compose with Caddy (the
   default), Kubernetes, or the binary under systemd. Pin the image to the version, never `latest`. Expose only `:9090` through the
   proxy; keep `:9091` on the host or inside the cluster. The proxy must pass `X-Forwarded-For`, allow 25 MiB bodies on
   `/webhooks/github` and stream `/api/v1/events` unbuffered (Caddy does all of it). With their go, start it and follow the log.
7. **Check.** `curl -s localhost:9091/readyz` is 200 (the first start reads every repository: minutes for hundreds), and
   `curl -s localhost:9091/api/v1/status | jq .warnings` is empty. The dashboard shows the groups; a private repository is absent
   while signed out and present after sign-in; *Admin* shows for an administrator; *Test the provider* on *Admin* answers when the
   assistant is on; a push shows within seconds and GitHub's delivery log says 202.
8. **Hand over.** Tell them what to back up (the configuration, the compose or manifests, `secrets/`, and the store (`store.path`, which holds the layouts and announcements too); never the
   cache), how to upgrade, and the alerts worth having (below).

## Changing a running portal

- Configuration changes apply at a restart; `config check` first, always. Roles (who sees private repositories, administrators)
  follow the configuration on every request.
- **Upgrade**: read the release notes between the two versions (`gh release view <tag> -R Cantara/source-code-portal`) and the new
  version's operator guide for renamed or new keys; `config check` with the new image; then change the image tag (or replace the
  binary and the bundle together) and restart. The cache carries over or is rebuilt from GitHub. Published tags never move.
- **Rotate the webhook secret**: the new one in `webhook.secret_file`, the old one in `webhook.previous_secret_file`, restart, change
  it on GitHub, then drop the old one. Replacing `session-key` signs everyone out. Removing a token from `auth.tokens` revokes it.

## When something is wrong

| Symptom | Look at |
|---|---|
| `config check` exits 2 | each problem names its key; unknown keys and unknown `SCP_*` variables are errors |
| `/pulls` or `/issues` says the feature is off, or stays empty | *Admin*, *GitHub prerequisites* (or `/api/v1/status` warnings, the log's `prerequisites unmet`): the missing permissions and events and the installation; grant them in the App's *Permissions & events*, accept them on the installation, then check with `gh api /orgs/<org>/installations --jq '.installations[] \| {app_slug, permissions, events}'` |
| `/readyz` stays 503 | `/api/v1/status` sources and warnings; the log for 401/403 (credential, App not installed, missing permission) |
| A repository is missing | public (or `include_private` with access), not a fork, not in `exclude`, in a listed account; `scp config groups` |
| Pushes show only after minutes | the webhook: GitHub's delivery log, `scp_webhook_deliveries_total` by result; without one the portal lists every 5 minutes (releases every 30 minutes); a portal GitHub cannot reach can set `webhook.catch_up: true` with an App source to read the App's delivery log |
| Pages say *Delayed* | `scp_github_rate_remaining_ratio`; with a token, move to a GitHub App |
| Sign-in fails | `/signin?error=`, the provider's redirect URI, `auth.oidc.groups_claim`, the session key the same on every instance |
| The assistant errs | *Test the provider* on *Admin*; `scp_assistant_requests_total` by outcome; the log's `assistant provider` lines |

Alerts: `scp_github_rate_remaining_ratio < 0.1` for 15 minutes; `scp_items{freshness="error"} > 0` for an hour; a rise of
`scp_webhook_deliveries_total{result="invalid_signature"}`; with several instances `sum(scp_cluster_leader) != 1` for 5 minutes.
