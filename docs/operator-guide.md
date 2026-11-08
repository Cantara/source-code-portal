# Operator guide

Source Code Portal (SCP) is one static binary, `scp`, and a web UI bundle it runs with Node.js. It reads the repositories of one or more GitHub
organisations and user accounts, keeps a local SQLite cache and serves the portal. One instance serves up to 1,000 repositories; several instances
can serve one portal together through Hazelcast (section 3.1).

- Public listener `:9090`: the web UI, the JSON API `/api/v1`, badges, `/llms.txt` and the GitHub webhook. Put it behind your proxy.
- Admin listener `:9091`: `/livez`, `/readyz`, `/metrics`, `/api/v1/status` and optional profiling. Keep it inside the cluster.

## 1. Connect GitHub

SCP needs read access to the repositories of the organisations and user accounts it shows. Use a GitHub App (recommended: its own rate limit of up to 12,500
requests an hour per installation) or a personal access token (5,000 an hour).

**What each feature needs.** The portal only reads: every permission below is *Read-only*, and it never writes to GitHub. A feature
whose permissions are missing stays empty; the events give updates within seconds (without them, or without a webhook, each feature
refreshes on its schedule).

<!-- prerequisites: kept equal to internal/prereq by cmd/scp's TestPrerequisitesDocumented -->
| Feature | Read permissions | Webhook events |
|---|---|---|
| The catalogue: repositories, README and documents, manifests, commits, releases, packages | Contents, Metadata | Push, Release, Repository |
| Pull requests | Pull requests, Checks, Commit statuses | Pull request, Pull request review |
| Issues | Issues | Issues |
<!-- /prerequisites -->

**When something is missing, the portal says so.** It reads the App's grants from its installations at every listing and warns where
it matters: the page of the feature (`/pulls`, `/issues`) says the feature is off, the *Admin* page's *GitHub prerequisites* card and
`/api/v1/status`'s warnings say which permissions and events are missing on which installation and how to fix it, and the log has a
`prerequisites unmet` line when that changes. With a token, which GitHub does not let the portal ask, the warning comes when GitHub
refuses a feature's reads (403) for most repositories. `GET /api/v1/prerequisites` gives the same; only an administrator sees the
missing permissions, the accounts and the fix.

**Doing without pull requests or issues.** An App without the *Pull requests*, *Checks* and *Commit statuses* permissions, or
without *Issues*, can switch the feature off instead of leaving it empty: `features.pulls: false`, `features.issues: false` (section
2). A switched-off feature is not there at all: GitHub is never asked for it, its row above is not checked (no warning), and it has no
page, header link, panel, dashboard element, tool, search kind, badge or sitemap entry; leave its events unticked too. The rest
works with what it still reads: the findings of pull requests (*waiting for review*, *old updates*) are not made, and the *backlog*
finding judges the best count held: the issues read, else GitHub's count less the open pull requests read, else GitHub's own count
of issues and pull requests together, which the finding then says.

**After changing an App's permissions** GitHub asks each installation to accept them: an owner of the organisation (Organization
settings, GitHub Apps, the App, *Review request*). Until then the installation keeps its old permissions and the warning stays. Check
it with `gh api /orgs/<org>/installations --jq '.installations[] | {app_slug, permissions, events}'`.

**GitHub App.** Create an App in the organisation's settings, or for a user account in your own (Developer settings, GitHub Apps):
1. Repository permissions, all read-only: *Contents*, *Metadata*, *Issues*, *Pull requests*, *Checks* and *Commit statuses*. Nothing
   else. *Issues* lists the open issues; the last three show the open pull requests and whose turn each is; without them the portal
   shows none.
2. Webhook: active, URL `https://<portal>/webhooks/github`, a random secret (for example `openssl rand -hex 32`), and subscribe to the
   events *Push*, *Release*, *Repository*, *Issues*, *Pull request* and *Pull request review*. Leave out *Issue comment*, *Check suite*
   and *Status*: they fire on every comment and every CI update, and the portal does not need them. Installation events
   are sent to Apps without subscribing.
3. Generate a private key (a `.pem` file) and note the App ID.
4. Install the App on each organisation or user account, on all repositories (or the ones to show). An App that may be installed only on
   its own account ("Only on this account") needs one App per account: give each its own source, with the same webhook URL and secret.

**Token.** A fine-grained token with public repository read access, or a classic token without scopes, in a file. Listed with a token, a
user account shows its public repositories, or all of them, private ones included, when it is the token's own account (the token needs
read access to them). For updates within seconds, add an organisation webhook (for a user account, a webhook on each repository) (Settings, Webhooks) with content type `application/json`, the same secret and the events *Pushes*,
*Releases*, *Repositories*, *Issues*, *Pull requests* and *Pull request reviews*.

Each recent commit's diff stats (lines added and removed, files changed) come from GitHub's GraphQL API with the same credential and no other permission: up to 100 commits a query, only commits of the last 90 days, each asked once; GraphQL has its own rate limit, apart from the REST one.

The open pull requests are read per repository every 15 minutes, and at once on a pull request or review
webhook: the listing is a conditional request, free when nothing changed (GitHub does not count a 304), and only a changed
repository costs one GraphQL query for the reviews, checks and mergeability of the pull requests that changed. Running checks are asked
again after 5, 10, 20 and then 60 minutes; every pull request once an hour; an archived repository once.

The open issues are read per repository every 15 minutes, and at once on an issue webhook (opened, edited, labelled, assigned, closed; a comment waits for the next read): GitHub's issues listing,
the most recently updated first, as conditional requests (an unchanged page is free), up to 3 pages and 100 issues per repository; the
pull requests in that listing are left out. No GraphQL.

**A webhook is never lost.** GitHub sends a delivery once and does not send it again. The portal writes every event to its store before
it answers GitHub (an event it cannot keep is answered 503, so GitHub's delivery log shows it failed), passes it on, and after a
restart passes on what was still parked (an update restarts the portal). With an App it also reads the App's webhook delivery log
every two minutes (a read, with the App's own token; no permission needed) and acts on every delivery from the last days that it did
not answer with a success: one that arrived while the portal was down, full or restarting is found within minutes of its return. A
webhook on an organisation or a repository has no such log: there the listing below is the net. Issues and pull
requests are also read every 15 minutes whatever arrives (a 304 costs no rate limit), and the listing every five minutes asks for a
read when a repository's count of open issues and pull requests moved.

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
    - { id: other, name: Other, default_repo: source-code-portal, match: [{ unclaimed: true }] }
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
| `store.path` | `/data/scp.db` | the SQLite cache; before a version changes its schema the portal copies it to `<store.path>.pre-NNNN` (mode 0600, once), the way back to the previous version |
| `sync.concurrency` | `4` | GitHub jobs running at once |
| `github.sources[]` | required | one entry per GitHub host and credential |
| `github.sources[].id` | required | short id, `[a-z0-9-]` |
| `github.sources[].base_url` | `https://api.github.com/` | REST base ending in `/`; GitHub Enterprise Server: `https://<host>/api/v3/` |
| `github.sources[].orgs` | required | organisations and user accounts read with this credential; an account belongs to one source |
| `github.sources[].app.id` | | GitHub App ID |
| `github.sources[].app.private_key_file` | | the App's private key (PEM) |
| `github.sources[].app.installations` | all | account logins whose installations are used |
| `github.sources[].token_file` | | a token file, instead of `app` |
| `github.sources[].credential` | `false` | `true`: the token is in the vault of [`scp personal`](personal-edition.md); `scp serve` refuses it |
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
| `auth.session_ttl` | `12h` | how long a sign-in lasts, `5m` to `720h`; it is renewed while in use, for at most 7 days from the sign-in (then the provider is asked again), or `session_ttl` when that is longer |
| `auth.admin_emails` | `[]` | verified email addresses of the administrators (case-insensitive) |
| `auth.admin_groups` | `[]` | groups whose members are administrators |
| `auth.tokens[]` | `[]` | access tokens: `name` (`[a-z0-9-]`), `secret_file` (at least 32 characters), `groups`, `admin`, `login` (the GitHub login of the person the token stands for: Pull requests and Issues then offer "Mine" filters to that token's session; `scp personal` sets it) |
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
| `webhook.catch_up` | `false` | read each GitHub App's webhook delivery log every two minutes and act on what the portal did not answer with a success: events without a receiver, for a portal GitHub cannot reach (a computer at home, behind a firewall). It needs a source with `app`; with a receiver it is the net that runs anyway. The App still has to subscribe to the events and have a webhook URL (it need not be reachable) |
| `cluster.addresses` | `[]` | Hazelcast members `host:port`; set to run as one of several instances (section 3.1) |
| `cluster.name` | `dev` | the Hazelcast cluster name |
| `cluster.namespace` | `scp` | prefix of the Hazelcast objects, `[a-z0-9-]`, so several portals can share one Hazelcast cluster |
| `web.dir` | empty | the web UI bundle (the directory holding `server.js`); empty: the pages answer 503, the API, badges and Markdown still work |
| `web.node` | `node` | the Node.js executable that runs the bundle (26, the current LTS; 24 also runs it) |
| `layout.dir` | empty | a writable directory for the pages' layouts an administrator designs (section 2.4) and the announcements written in Admin (`announcements.json`, section 2.4.1); empty: the built-in layouts and the configured announcements, no saving |
| `announcements[]` | `[]` | the deployment's announcements (section 2.4.1): `id`, `title`, `text`, `tone`, `starts`, `ends`, `place`, `link` (`label`, `url`), `lang`, `pages`, `audience`, `from`, `until`, `published`; at most 20 |
| `packages.registries[]` | `[]` | package registries asked for what a repository publishes (section 2.5): `id` (`[a-z0-9-]`), `ecosystem` (`maven`, `npm`, `pypi`, `cargo`, `go`, `nuget`, `rubygems`, `packagist`), `url`: `maven` a repository root, `npm` a registry, `pypi` a Simple API root, `cargo` a sparse index, `go` a module proxy, `nuget` the feed whose `index.json` is the service index, `rubygems` a gem server, `packagist` a Composer repository |
| `packages.registries[].network` | `public` | `public`: reached through the public-only guard; `private`: on your network (http allowed, without credentials) |
| `packages.registries[].auth` | none | `user` and `password_file` (Basic; the password may be an access token), or `token_file` (a bearer token), with `header` to send it in another header (`X-JFrog-Art-Api`, or `Authorization` for Cargo's raw token) |
| `packages.public` | `true` | ask the public registries too (Maven Central, npm, PyPI, crates.io, proxy.golang.org, CocoaPods trunk, nuget.org, rubygems.org, Packagist) for public repositories; `false`: only the configured ones and those the build files name |
| `ops.pprof` | `false` | serve `/debug/pprof/` on the admin listener |
| `features.pulls` | `true` | the open pull requests (section 2.4.2); `false`: switched off, as if the portal had none (section 1) |
| `features.issues` | `true` | the open issues (section 2.4.3); `false`: switched off (section 1) |

Matchers (each has exactly one field) see the repository name, except `topic`:

| Matcher | Selects |
|---|---|
| `exact: name` | the repository with that name (case-insensitive) |
| `prefix: whydah-` | names starting with the prefix (case-insensitive) |
| `glob: "*-service"` | the whole name against `*` and `?` (case-insensitive) |
| `regexp: "^scp-(api\|web)$"` | the whole name against an RE2 expression (case-sensitive unless `(?i)`) |
| `topic: iam` | repositories with that GitHub topic |
| `catch_all: true` | every repository |
| `unclaimed: true` | the repositories no other group selects (in at most one group): the ones that still need a home |

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
tools (search what the portal holds, list and read groups, read a repository's README and technology, read a document, the recent commits,
the open pull requests, the open issues, a repository's health) and answers in Markdown with links into the portal. It sees exactly what the person asking may see: a private
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

### 2.4 The landing page and the design mode

The dashboard shows signals, not every attribute of every repository: a telemetry row (the commits by hand, with AI and by
automation in the last 30 days, the repositories changed, the findings, the open pull requests, whether the catalogue is current; the
presentation page's strip shows the same), a fleet map with every repository
as one tile coloured by its last change by a person, the products as cards or a sortable table, the newest open pull requests
and issues, and the latest commits by people. A
commit by a bot or a release plugin is automation; a commit whose subject landed in more than 10 repositories on one day is a sweep.
Neither counts as a person's work. A person's commit with an AI `Co-authored-by` trailer (as Claude Code, Copilot and Cursor write
them) or an AI tool's footer, and a coding agent's own commit, are people's work "with AI": the cards and the telemetry count them and
name the assistants, the sparklines draw them at the foot of each week, and the commit lists mark them. A tool that leaves no trace
(inline completion) is not counted, so the share is a lower bound. Each card says once what most of its repositories share (the licence, the language) and shows only
the exceptions; the members are on the product's page. A reader's choice of cards or table is kept in a cookie.

People and agents work together, so the cards rank neither: each leads with the team's commits and says how many were by hand and how
many with AI. **Settings** (in the header) lets every reader choose the work that counts, any of by hand, with AI and by automation
(maintenance: the bots and sweeps); all three until they choose. The choice changes the numbers, the sparklines, who made the work, the last change, the fleet map and the latest commits; it
is kept in the reader's browser, never on the server. Settings also hold the products view, the page sizes and the theme.

An administrator can arrange the page: *Design this page* on the dashboard opens the design mode, with the page's elements in a list
to add, move (the arrows, or drag them), set up and remove, beside a live preview. The elements are the telemetry, the fleet map and the
products (each for every product or for chosen ones, in your order), the pull requests (how many), the latest commits (how many) and a
heading with a line of text.
*Save* keeps the layout for every viewer; *Back to the built-in layout* removes it.

```yaml
layout:
  dir: /var/lib/scp/layouts   # writable by the portal; one JSON file per page (dashboard.json)
```

- Without `layout.dir` the built-in layout renders and the design mode can only try layouts, not save them.
- The files are the portal's own: keep them with your configuration's backup. They are not in the cache (`store.path`), so deleting the
  cache never loses a design; deleting `dashboard.json` brings back the built-in layout.
- A save is written to a temporary file and renamed, so a reader never sees half a layout. Two administrators never overwrite each
  other: a save based on a layout someone else changed since is refused with an offer to load theirs.
- Each instance reads the files again within 5 seconds of a change, so several instances share a layout through a shared volume, and a
  file written by hand or by configuration management is picked up too. A file that does not validate is not served: the last good
  layout stays, and `/api/v1/status` and the design mode say why.
- A layout names groups, never repositories; a viewer sees only the groups with repositories they may see.

**On a wall.** `/present` is the presentation page for a tablet or a screen beside the team: the fleet's numbers, every repository's
health, the products as tiles and the newest commits on one screen that never scrolls. A product at watch or attention gets a wide
tile with its findings; the good ones take a line each and fold into "N more good" when they do not fit. It says at all times how
current it is (*Live*, *Polling*, *Stale* after 2 minutes without new data, *Offline* after 10 or while the portal cannot be reached),
keeps the screen awake, follows the reader's theme (a screen told it is on a wall is dark unless the reader chose), reloads for a new release and once an hour, and shifts a pixel now and then
against burn-in. A tap shows its controls; an administrator designs it as the dashboard (`present.json` in `layout.dir`).

- One screen per team: `/present?groups=payments,identity` keeps to those products, `?work=hand,ai` fixes the work that counts.
- The page knows the kind of screen it is on, its *profile*: *Handheld portrait* (a tablet held upright), *Handheld landscape* (a
  tablet on its side, up to 1600 pixels wide), *Desk* (a wider monitor, or a window on half of one: the commit log takes 60% of it) and *Wall* (a screen read
  from across a room). A wall has a desk's pixels, so tell the screen once: `/present?profile=wall`, or *On a wall* in its controls;
  `?profile=auto` forgets it. The controls show the profile.
- The right side column holds the latest commits, the pull requests and the issues, each with its own scroll and a button that
  hides it; the others take its space, and with all three hidden the column folds to a thin rail that brings them back. Each screen
  remembers its own choice.
- Drag the side column's edge (or use the arrow keys on it) to make it wider, up to 60% of the screen; the device remembers it for
  each profile, so a width dragged on the desk leaves the tablet's alone, and a double tap brings back the profile's own. The
  left side then keeps what matters most: the products that need a look and how current the page is stay, the people, the counts and
  the counts give way first; the products then fold the good ones, then shrink and fold the mildest to watch.
- On an iPad, *Share › Add to Home Screen* opens it full screen without the browser (the page's web app manifest starts at
  `/present`). Turn off auto-lock or rely on the page's wake lock, which Safari grants after the first tap.
- With `access.require_auth`, sign in once on the tablet: through single sign-on, or on `/signin` with a token of its own from
  `auth.tokens` (a screen is not a person: give it a name such as `wall-payments` and only the groups it should show). The page
  reloads every hour, which renews the session within `auth.session_ttl`.

#### 2.4.1 Announcements

The portal carries the team's news: an event with a sign-up link, planned maintenance, a change everyone must know about. An
announcement shows at the top of the dashboard and of `/present` (the `announcements` element in the design mode; move it like any
other) while its time lasts, and goes away by itself. It is plain text with one link, never HTML or Markdown.

- **Administrators** write them on the *Admin* page's *Announcements* card: the title, a line, when it starts and ends, where, a link
  and its words, the tone (*Event*, *Announcement* or *Notice*, each in a colour of its own, never a health colour), the language of
  its text, the pages it shows on (the dashboard, `/present` or both), who sees it (everyone, or signed-in viewers only), when it shows
  and until when. *Published* off keeps it as a draft. The preview beside the form is the card the pages draw. They are saved in
  `layout.dir` (`announcements.json`), so without one the card cannot save.
- **The deployment** sets them in `announcements[]`, checked by `scp config check`; the *Admin* card lists them, locked:

```yaml
announcements:
  - id: juleol-2026
    title: Juleølsmaking på Rebel Makerspace
    text: Det avholdes juleølsmaking i år.
    tone: event                        # event, info, notice
    starts: "2026-11-26T18:00:00+01:00"
    ends: "2026-11-26T23:00:00+01:00"  # also when it stops showing; else until is required
    place: Rebel Makerspace
    link: { label: Meld deg på her, url: "https://example.org/pamelding" }
    lang: nb
    pages: [dashboard, present]        # one or both
    audience: everyone                 # or signed-in
```

- Times are RFC 3339 with their offset and are shown as written: 18:00 at `+01:00` reads 18:00 on every screen. `until` is required
  unless `ends` is set; `from` defaults to at once. A link is https only.
- At most three show at once: notices first, then events by their start, then the rest. A reader can dismiss one on the dashboard (it
  stays dismissed in that browser until it is edited); on `/present` none is dismissible, and on a desk or a wall its link also shows
  as a QR code for a phone.
- A signed-in announcement is never sent to the public, on any page or in the API.

### 2.4.2 Pull requests

**Pull requests** in the header (`/pulls`) lists every open pull request a reader may see, and says for each whose turn it is: the
author's (a draft, conflicts, failing checks, changes requested), the reviewers' (a review is required, or none approved yet), CI's
(checks running) or a maintainer's (ready to merge). People's come before automation's, those waiting for a review first, then the
longest idle; the states are filters, and the product, the repository, whose and the order are a form that works without JavaScript.
A pull request by Dependabot, Renovate, Snyk (its `snyk-fix-` and `snyk-upgrade-` branches, whoever's account it uses) or any
`[bot]` is automation, and counts only when the work that counts holds automation.

The dashboard's telemetry and the presentation page's strip count them, the dashboard shows the longest waiting beside the products,
`/present` as a band below them, the product and group tables have a column, and a group's and a repository's page a panel. A
repository's open issues no longer count its pull requests, and two findings cost points: a person's pull request idle while waiting
for a review (more than 7 days, more than 30), and automation's left open for more than 30 days. They need the App's *Pull requests*,
*Checks* and *Commit statuses* read permissions (section 1); without them GitHub refuses the listing and `/pulls` stays empty.
`features.pulls: false` switches them off (section 1).

### 2.4.3 Issues

**Issues** in the header (`/issues`) lists every open issue a reader may see, pull requests left out, and says for each where it
stands: *needs triage* (no label, milestone or issue type: nobody has looked at it), *unassigned* (looked at, nobody took it) or
*assigned*. Its kind is GitHub's issue type when set, else read from its labels: a bug (bug, defect, regression, crash), a feature
(enhancement, feature, improvement), a question (question, support) or other. People's come before automation's (Renovate's
dependency dashboard is automation), those needing triage first, then the longest idle; the states are filters, and the kind, the
product, the repository, whose and the order are a form that works without JavaScript. A group's and a repository's page have a panel
with the ones that wait longest, and the group table's issue counts link to them. The dashboard's side panel has the newest after the
latest commits and the pull requests, each a third of it with its own scroll that reads more as it nears its end; a reader hides
any of the three by its button, and the others take the space. `/present` has the same three in its side column. The dashboard keeps its signal, the *backlog*
finding. They need the App's *Issues* read permission (section 1); without it GitHub refuses the listing and `/issues` stays empty.
`features.issues: false` switches them off (section 1).

### 2.4.4 Releases

**Releases** in the header (`/releases`) lists what the repositories published, newest first. The portal keeps each repository's 20
newest published releases (drafts are never listed): it reads them when GitHub sends a *Release* event and every 6 hours as a safety
net (every 30 minutes when no events reach the portal: no receiver and no `webhook.catch_up`; `/api/v1/status` lists each source's mode and
how often each subject is refreshed), as a conditional request that costs no rate limit when nothing changed. By default the page shows the latest release of
each repository, the newer of its latest stable GitHub release and the newest release of the packages it publishes to registries
(Maven, npm, PyPI and the others the portal reads, section 2.5): a repository that publishes only to a registry is listed too, with the
registry's name beside its version. The *Source* choice limits the page to GitHub's releases or to the packages'; the portal holds a
package's newest release only, so the page shows no earlier versions of it, and a package whose registry gives no publication date is not
listed. A package published with the version of a GitHub release is that release and is listed once; the filter panel at the left switches to every release, the stable ones or the pre-releases, each with its count. The
product, the repository, a window (the last 7, 30, 90 days or year) and the order (newest, oldest, repository, product) are in the same
panel: every choice is in view, applied as it is made, and the panel hides and shows with its button (the choice is kept for every list
page). Without JavaScript the panel is a form with an Apply button. A
release shows its tag (a link to GitHub), its name, the first line of its notes, who published it and when; one published by a bot
says so. Under it, *Release notes* opens the notes of that version (the same on a repository's and a product's page): the GitHub release's
own text, cut at 6 KiB with a link to the rest, or, for a package release and a release without a body, the section of the repository's
root `CHANGELOG.md` (also `CHANGES`, `HISTORY`, `NEWS`, `RELEASES`, `RELEASE_NOTES`) for that version, marked *from the changelog*. A
section starts at a heading that names the version (`## 1.2.3`, `## [1.2.3] - 2026-01-01`, `## v1.2.3`); the ten newest are read. The latest stable release is also what the release badges and a repository's summary show. It needs the App's *Contents*
permission and the *Release* event, which the portal already asks for.

### 2.4.5 Archive

**Archive** in the header (`/archive`) lists the retired repositories a reader may see: those GitHub archived and those whose
`scp.yaml` says `lifecycle: deprecated` (archived wins). A quiet repository is not retired, only dormant. Each row says why, when the
repository was last active (its newest commit; GitHub dates no archive, so the portal does not invent a date of retirement), its
latest release, how many other repositories still depend on it (archived ones are not counted), and how many people wrote its
recent commits, counted and never named. The reasons are filters; the product, a name, *still depended on* and the order are a form
that works without JavaScript. The page needs no setting and nothing from GitHub that the portal does not already read, and a
private archive entry is shown only to those who may see the repository.

### 2.4.6 Insights

**Insights** in the header (`/insights`, and a product's page links to its own) answers five questions from what the portal already
holds; it reads nothing more from GitHub and needs no setting. *How often does the fleet ship?* counts the stable releases of the
last 12 months and gives each product's median days between its last five (a product that ships weekly is not better than one that
ships quarterly). *What is it built on?* counts the languages and build tools the manifests name and the oldest and newest runtime
version each names. *What does it lean on?* lists the internal libraries with the most dependants, marking a retired one that is
still used. *Where does knowledge sit with one person?* lists the repositories where one person wrote 90% or more of the last
people's commits (at least 20 held; automation and sweeps do not count): it names repositories and never the person. *What has gone
quiet?* counts the repositories by how long ago they changed and lists the dormant ones, the candidates for retirement. Retired
repositories are left out of all of them but the libraries. A private repository counts only for those who may see it.

### 2.5 Published packages

A repository's release is often a package, not a GitHub release: a Java library published to a Maven repository by a CI server, a
TypeScript one to npm. The portal reads the repository's build files and asks the registries for the newest release, which then shows
where a GitHub release would: as the release, with its registry (`3.0.21`, from Maven), in the repository page's *Packages* panel, in the group's
released count and in the health's unreleased check.

- **Maven** (`pom.xml`, up to two directories deep): the package is the POM's `groupId:artifactId`. It is looked up where the POM
  publishes it, its `distributionManagement` repository, inherited through its `parent` POMs when it has none of its own (each parent
  read from the repositories the POM lists, then Maven Central, at most five levels; a released parent is read once and kept), then in
  the configured Maven registries, then Maven Central. The newest release is the one `maven-metadata.xml` names.
- **npm** (`package.json`): the package is its `name`, unless it is `private`. It is looked up at its `publishConfig.registry`, the
  configured npm registries, then registry.npmjs.org; the newest release is the `latest` tag.
- **Gradle** (Kotlin and Java) publishing with `maven-publish`, **PyPI** (`pyproject.toml`, `setup.cfg`), **Cargo** (`Cargo.toml`),
  **Go** (`go.mod`: the module proxy, or the repository's tags), **Swift** (`Package.swift`: the tags), **CocoaPods**, **NuGet** (C# and F#), **RubyGems** and **Packagist** (PHP): the
  repository guide lists what each reads and where it asks.
- A **private** repository's packages are never named to a public registry: only to the registries its build files name and the
  configured ones.

```yaml
packages:
  registries:
    - { id: example, ecosystem: maven, url: https://maven.example.org/repository/releases/ }
```

A registry a build file names is reached only over https and only at public addresses: a repository cannot make the portal call into
your network. Lookups run at most four at a time per registry host; a registry that answers 429 or 503 is paused (its `Retry-After`,
else one minute doubling to fifteen), and one that fails keeps the release last known. Packages are looked up every six hours and on
every push and tag.

**Private registries.** Configure each registry the portal should sign in to. Its credentials are files, like every secret, and are
sent only to URLs under the registry's own `url` (scheme, host, port and path): never to a registry a build file names, never to
another path on the same host, and never across a redirect that leaves it. Use a read-only account or token.

```yaml
packages:
  registries:
    # Nexus or Reposilite: a user and its password or a user token
    - { id: nexus, ecosystem: maven, url: https://nexus.example.org/repository/maven-releases/,
        auth: { user: scp-reader, password_file: /run/secrets/nexus-password } }
    # Artifactory: an identity token as a bearer token, or an API key in its own header
    - { id: art-npm, ecosystem: npm, url: https://example.jfrog.io/artifactory/api/npm/npm/,
        auth: { token_file: /run/secrets/artifactory-token } }
    - { id: art-pypi, ecosystem: pypi, url: https://example.jfrog.io/artifactory/api/pypi/pypi/simple/,
        auth: { token_file: /run/secrets/artifactory-key, header: X-JFrog-Art-Api } }
    # GitHub Packages: a classic token with read:packages (Basic for Maven, bearer for npm)
    - { id: github-maven, ecosystem: maven, url: https://maven.pkg.github.com/example/,
        auth: { user: scp, password_file: /run/secrets/github-packages-token } }
    - { id: github-npm, ecosystem: npm, url: https://npm.pkg.github.com/,
        auth: { token_file: /run/secrets/github-packages-token } }
    # A Cargo registry's sparse index: the token as it is, in Authorization
    - { id: acme, ecosystem: cargo, url: https://cargo.example.org/index/,
        auth: { token_file: /run/secrets/cargo-token, header: Authorization } }
    # Verdaccio, devpi or Athens on your network, without credentials
    - { id: lan-npm, ecosystem: npm, url: http://verdaccio.internal:4873/, network: private }
    - { id: athens, ecosystem: go, url: https://athens.internal/, network: private }
    # NuGet (Nexus, Artifactory, GitHub Packages): the feed's root, where its index.json is
    - { id: nuget, ecosystem: nuget, url: https://nexus.example.org/repository/nuget-hosted/,
        auth: { user: scp-reader, password_file: /run/secrets/nexus-password } }
    # A gem server (Artifactory, Nexus, Gemstash) with a user and its password
    - { id: gems, ecosystem: rubygems, url: https://gems.example.org/, auth: { user: scp-reader, password_file: /run/secrets/gems-password } }
    # Private Packagist: Basic with the user "token" and the token as its password
    - { id: php, ecosystem: packagist, url: https://repo.packagist.com/example/, auth: { user: token, password_file: /run/secrets/packagist-token } }
```

GitHub Packages does not take the portal's GitHub App: its registries need a token with `read:packages`. A Cargo crate whose
`publish = ["acme"]` names a registry is looked up in the configured Cargo registry of that `id`. `config check` refuses a registry
without an ecosystem the portal reads, with credentials over http, or with both kinds of credentials.

### 2.6 Agents (MCP and WebMCP)

Agents call the portal's read-only tools (the assistant's) without any configuration: MCP clients at `/mcp`, browser agents through
WebMCP on the pages, scripts through `/api/v1/tools` (the [API guide](api.md#tools-for-agents-mcp-and-webmcp) shows how). No language
model is involved on the portal's side: the agent brings its own.

- **Who sees what.** An MCP call without a token answers from the public view (refused with `access.require_auth`); give a team's
  agents an access token (`auth.tokens`) whose `groups` are in `access.private_groups` for the private view. A browser's agent uses the
  reader's own sign-in.
- **Behind a proxy.** `/mcp` answers plain JSON over POST; nothing needs configuring in nginx or Caddy. It refuses a request whose
  `Origin` is not `server.public_url`'s, so set the public URL when the portal is reached under another name than its own host.
- **Limits.** A call reads the snapshot in memory; the 30-second request deadline and your proxy's rate limits apply.

**Testing WebMCP in Chrome.** Chrome offers WebMCP behind a flag for testing. The extension *Model Context Tool Inspector* shows a
page's tools and lets a Gemini model call them:

1. Use Chrome 146 or newer (Beta, Dev or Canary when the stable one is older). Open `chrome://flags/#enable-webmcp-testing`, set
   *WebMCP for testing* to *Enabled* and press *Relaunch*.
2. Install [Model Context Tool Inspector](https://chromewebstore.google.com/detail/model-context-tool-inspec/gbpdfapgefenggkahomfgkhfehlcenpd)
   from the Chrome Web Store.
3. Create a Gemini API key in [Google AI Studio](https://aistudio.google.com/api-keys) (*Create API key*; the free tier is enough).
   The key stays in the extension and goes to Google only, never to the portal.
4. Open a page of the portal (a repository's page is a good start), sign in for the private view, and click the extension's icon. Its
   side panel lists the page's tools under *WebMCP Tools*, and its badge shows how many there are. Then press *Set Gemini API key* and
   paste the key.
5. Under *Interact with the Page*, choose a *Tool* (`current_page` first), adjust its *Input Arguments* and press *Execute Tool*. The
   answer in *Tool Results* is what an agent gets.
6. Write a *User Prompt* ("what repository am I looking at, its open bugs waiting for triage, and its health score?") and press
   *Send*: Gemini picks the tools and fills in their arguments. *Copy trace* copies every call and answer as JSON, for a bug report.

The page adds three tools of its own to the catalogue's: `current_page` (what the reader looks at), `open_page` (go to a page of the
portal) and `set_preferences` (the reader's Settings). None reaches an administrator's actions. When the badge shows 0, the flag is off
or Chrome was not relaunched. The Guide (`/guide#webmcp`) shows the same steps with a picture of the inspector.

## 3. Run

For yourself alone on your own computer, [`scp personal`](personal-edition.md) signs you in to GitHub and needs no configuration.

**Binary.** Download the archive for your platform and the web UI bundle (`scp-web_2.0.0.tar.gz`, the same for every platform) from
the GitHub release, check them and run them with Node.js 26 on the `PATH` (or `web.node`):

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
timeout above 30 seconds (the stream sends a `ping` event every 25 seconds; nginx honours the `X-Accel-Buffering: no` it sends).

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
| `scp_webhook_deliveries_total` | `event`, `result` | webhook deliveries: `accepted`, `ignored`, `duplicate`, `ping`, `invalid_signature`, `malformed`, `too_large`, `busy`, `unavailable` (the event could not be kept; 503), `caught_up` (found in the App's delivery log, not received), `unsupported_media_type` |
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
| `scp_search_requests_total` | `op` | searches (`search`) and search-box suggestions (`suggest`) answered |
| `scp_search_duration_seconds` | `op` | time a search took |
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
| `/pulls` or `/issues` says the feature is off, or stays empty | the *Admin* page's *GitHub prerequisites* card names the missing permissions and events; grant them in the App's *Permissions & events* and accept them on the installation, or switch the feature off with `features.pulls` or `features.issues` (section 1) |
| `/readyz` stays 503 | `/api/v1/status` sources and warnings; the log for `401`/`403` (credential, App not installed, missing permission) |
| A repository is missing | it must be public, not a fork (unless `include_forks`), not matched by the source's `exclude`, and in a configured account; a private repository disappears at the next listing, a deleted one after two (the second runs 30 seconds after the first misses it) |
| Pushes, issues or pull requests show only after minutes | the webhook: GitHub's delivery log for the App, organisation or repository (a delivery shown as failed is read from that log within minutes: `scp_webhook_deliveries_total{result="caught_up"}`), `scp_webhook_deliveries_total` by result; the App must subscribe to *Issues*, *Pull request* and *Pull request review* (the *GitHub prerequisites* card) |
| The assistant answers with an error | *Test the provider* on the *Admin* page; `scp_assistant_requests_total` by outcome; the log's `assistant provider` lines carry the provider's message |
| Pages show *Delayed* | `scp_github_rate_remaining_ratio`; with a token, consider a GitHub App |
