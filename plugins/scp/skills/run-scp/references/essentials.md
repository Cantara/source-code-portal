# SCP configuration essentials

A summary of the operator guide for when the release's own documents cannot be fetched. The release's
`docs/operator-guide.md` is the reference and wins on any difference; `scp config check` is the judge.

## Starter `scp.yaml`

```yaml
server:
  public_url: https://scp.example.org     # absolute links; https turns on HSTS
  trusted_proxies: [172.30.0.0/24]        # the reverse proxy's network only
store:
  path: /data/scp.db                      # what admins save lives here too: back it up, or download scp.yaml from Admin
github:
  sources:
    - id: github
      orgs: [example]                     # organisations and user accounts
      app: { id: 123456, private_key_file: /run/secrets/scp-app.pem }   # reads the repositories selected on the installation
      # token_file: /run/secrets/github-token   (instead of app)
webhook:
  secret_file: /run/secrets/webhook-secret
catalog:
  groups:
    - id: payments
      name: Payments
      description: Checkout, the ledger and the card gateway
      default_repo: payments-api
      match: [{ prefix: payments- }]
    - { id: other, name: Other, default_repo: handbook, match: [{ unclaimed: true }] }
```

Every key can be overridden by `SCP_<KEY>` with dots as underscores (`SCP_SERVER_PUBLIC_URL`); an `SCP_*` variable naming no key is
an error, as is an unknown key in the file.

## Every key

Defaults in brackets; "required" keys must be set. Secrets are always files.

| Section | Keys | Notes |
|---|---|---|
| `server` | `public_addr` (`:9090`), `admin_addr` (`:9091`), `shutdown_grace` (`20s`), `public_url`, `trusted_proxies` (`[]`) | `public_url`: absolute links, https turns on HSTS; a proxy range wider than /24 with private repositories draws a warning |
| `log` | `level` (`info`), `format` (`json`, or `text`) | |
| `store` | `path` (`/data/scp.db`) | one SQLite file on block or local storage, not NFS; it holds the catalogue, parked webhook events, and what admins save (layouts, announcements, who sees what): back it up, or download scp.yaml from Admin |
| `sync` | `concurrency` (`4`) | GitHub jobs at once |
| `github.sources[]` (required) | `id` (required, `[a-z0-9-]`), `base_url` (`https://api.github.com/`; Enterprise Server `https://<host>/api/v3/`), `orgs` (required: organisations and user accounts), one of `app` (`id`, `private_key_file`, `installations` (all), `webhook_secret_file`) or `token_file`, `include_forks` (`false`), `include_archived` (`true`), `exclude[]` (matchers), `include_private` (`false`) | an account belongs to one source. With an App the portal reads what each installation was given: *Only select repositories* gives exactly those, *All repositories* all; adding one on GitHub makes it appear at once, so no `exclude` is needed. `app.webhook_secret_file`: the App's own webhook secret when it is not `webhook.secret_file`. `credential` and `app.credential` (`false`) belong to `scp personal`'s vault; `scp serve` refuses them |
| `catalog` | `reconcile_interval` (`5m`, 1m to 24h), `groups[]`: `id` (required), `name` (required), `description`, `default_repo` (required), `sources` (all), `match[]` | at most 500 groups, 1 to 64 matchers each; a group with `sources` and no `match` takes every repository of those sources (an account's own group) |
| `webhook` | `secret_file`, `previous_secret_file`, `catch_up` (`false`) | the receiver exists with a secret (this one, or an App's `app.webhook_secret_file`); `previous_secret_file` during a rotation; `catch_up: true` (needs an App source) reads the App's delivery log every 2 minutes, for a portal GitHub cannot reach, or as the net under a receiver |
| `access` | `proxy_identity` (`true`), `user_header` (`X-Forwarded-User`), `groups_header` (`X-Forwarded-Groups`), `private_groups` (`[]`; `["*"]`: everyone signed in), `require_auth` (`false`) | `include_private` needs `private_groups` and a way to know the viewer. A trusted proxy is believed when it names a user and groups; a proxy that passes a client's headers on (a container platform's) makes them forgeable: strip them there, or set `proxy_identity: false` when viewers sign in |
| `auth` | `session_key_file` (at least 32 bytes; needed for sign-in), `session_ttl` (`12h`), `admin_emails`, `admin_groups`, `tokens[]` (`name`, `secret_file` (at least 32 characters), `groups`, `admin`, `login`), `oidc.issuer`, `oidc.client_id`, `oidc.client_secret_file` (empty: a public client with PKCE), `oidc.scopes` (`[openid, email, profile]`), `oidc.groups_claim` (`groups`), `oidc.require_verified_email` (`false`: set `true` where the provider sends `email_verified`, or use `admin_groups`) | redirect URI `<public_url>/auth/callback`, sign-out redirect `<public_url>/` |
| `llm` | `provider` (`anthropic`, `openai`, `gemini`, `openrouter`, `ollama`; empty: no assistant), `model`, `base_url`, `api_key_file` (required but for `ollama`) | what the tools read goes to the provider |
| `assistant` | `access` (`signed-in`, `admin`, `everyone`), `concurrency` (`4`), `max_turns` (`8`), `max_tool_calls` (`16`), `max_tokens` (`16000`), `max_history` (`40`), `max_input_chars` (`60000`), `tool_result_chars` (`12000`) | the budgets of one answer |
| `features` | `pulls` (`true`), `issues` (`true`) | `false` switches the feature off entirely (no page, element, tool or warning); leave its permissions and events out too |
| `announcements[]` | `id` (`[a-z0-9-]`), `title`, `text`, `tone` (`info`, `notice`, `event`), `starts` and `ends` (an event's own time, RFC 3339 with offset), `place`, `link` (`label`, an https `url`), `lang`, `pages` (`dashboard`, `present`; both by default), `audience` (`everyone`, or `signed-in`), `from` (shown at once) and `until` (shown until; required unless `ends`), `published` (`true`) | at most 20; the base of what admins change in Admin |
| `visibility` | `groups` and `repos` (`audience`: `everyone`, `signed-in`, `no-one`; `search: false` keeps what everyone sees from search engines), `rules[]` (`pattern`: an RE2 expression on `owner/name`, case-insensitive, the first match wins; `audience`, `search`) | the base of who sees what; Admin's changes are kept in the store on top of it |
| `layouts` | by page (`dashboard`, `present`): `elements` as the design mode saves them | the base of the pages' layouts |
| `layout` | `dir` | only for an upgrade from a release that kept layouts in files: imported once, then remove the key |
| `web` | `dir` (set in the image; for the binary the unpacked `bundle/`), `node` (`node`), `theme_dir`, `product_name`, `plausible_domain`, `plausible_endpoint` | `theme_dir`: `theme.css` (colour tokens: `:root:not(.dark)` and `.dark`), `logo.svg`, `favicon.svg`, served under `/theme/`, read at the start ([theme.md](theme.md)); `product_name` replaces the product's name; `plausible_domain` counts visits in Plausible Analytics (the site as added in Plausible, installation method NPM, nothing to paste; `plausible_endpoint` only for a self-hosted Plausible; a viewer who sees private repositories is never counted) |
| `packages` | `public` (`true`), `registries[]`: `id`, `ecosystem` (`maven`, `npm`, `pypi`, `cargo`, `go`, `nuget`, `rubygems`, `packagist`), `url`, `network` (`public`, or `private`: on your network, http allowed), `auth` (`user` and `password_file`, or `token_file` with an optional `header`) | credentials go only to the registry's own URL; GitHub Packages needs a token with `read:packages`, not the App |
| `commits` | `assistants[]`: `name`, `authors` (login or name: the agent's own commits), `co_authors` (a `Co-authored-by` name, address, or local part after a noreply's `<id>+` on any host: a person's commit with AI) | your own assistants beside the built-in ones; a configured `[bot]` co-author counts as AI, not automation; quote `"name[bot]"` in a bracketed list |
| `cluster` | `addresses` (`[]`), `name` (`dev`), `namespace` (`scp`) | several instances through Hazelcast 5 on a private network |
| `ops` | `pprof` (`false`) | profiles on the admin listener |
| `enterprise` | `enabled` (`false`), `github[]`: `source`, `web_url`, `graphql_url`, `people` (`false`), `login` (`managed`); `oidc.identity_claim` (`email`), `oidc.userinfo` (`true`); `token_key_file`; `access.refresh` (`15m`), `access.max_age` (`1h`) | GitHub Enterprise Cloud (`https://api.<name>.ghe.com/` with web `https://<name>.ghe.com/`) and Entra ID (`identity_claim: preferred_username`, `userinfo: false`); every key but `enabled` is refused while it is off; `people: true` makes it a configuration of `scp enterprise`, each viewer reading with their own fine-grained token linked at `/github` |

## Matchers

Each matcher has exactly one field and sees the repository name, except `topic`.

| Matcher | Selects |
|---|---|
| `exact: name` | that repository (case-insensitive) |
| `prefix: payments-` | names starting with it (case-insensitive) |
| `glob: "*-service"` | the whole name against `*` and `?` (case-insensitive) |
| `regexp: "^(api\|web)-.*$"` | the whole name, RE2, case-sensitive unless `(?i)` |
| `topic: payments` | repositories with that GitHub topic |
| `catch_all: true` | every repository |
| `unclaimed: true` | what no other group selects (in at most one group) |

A repository may be in several groups; repositories in none are counted on the dashboard. A group with `sources` and no `match`
takes every repository of those sources: `{id: oranheim, name: Ove Ranheim, default_repo: dossier, sources: [oranheim]}`.

## For one person: `scp personal` (experimental)

Someone who wants a portal only for themselves on their own computer needs no `scp.yaml`: `scp personal` opens a page in the
browser where they choose the GitHub host (github.com, an Enterprise Server or a GHE.com tenant), sign in (a code with their own
app's client id, or a pasted token), pick their repositories and sort them into products; it seals the credential in the store and
serves on loopback with a one-time sign-in link (`--terminal` asks in the terminal instead). Its settings are `personal.yaml` in the
directory it prints; read `docs/personal-edition.md` of the release. Use `scp serve` for a portal several people use.
