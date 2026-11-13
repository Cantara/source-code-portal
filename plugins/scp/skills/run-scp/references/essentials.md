# SCP configuration essentials

A summary of the operator guide for when the release's own documents cannot be fetched. The release's
`docs/operator-guide.md` is the reference and wins on any difference; `scp config check` is the judge.

## Starter `scp.yaml`

```yaml
server:
  public_url: https://scp.example.org     # absolute links; https turns on HSTS
  trusted_proxies: [172.30.0.0/24]        # the reverse proxy's network only
store:
  path: /data/scp.db                      # rebuildable from GitHub, except the saved layouts and announcements: back those up
github:
  sources:
    - id: github
      orgs: [example]                     # organisations and user accounts
      app: { id: 123456, private_key_file: /run/secrets/scp-app.pem }
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

## Sections and their main keys

| Section | Keys | Notes |
|---|---|---|
| `server` | `public_addr` (`:9090`), `admin_addr` (`:9091`), `shutdown_grace` (`20s`), `public_url`, `trusted_proxies` | a proxy range wider than /24 with private repositories draws a warning |
| `log` | `level` (`info`), `format` (`json`, or `text`) | |
| `store` | `path` (`/data/scp.db`) | one SQLite file: block or local storage, not NFS |
| `sync` | `concurrency` (`4`) | GitHub jobs at once |
| `github.sources[]` | `id`, `base_url` (GitHub Enterprise: `https://<host>/api/v3/`), `orgs`, `app.id`, `app.private_key_file`, `app.installations`, `token_file`, `include_forks` (`false`: forks are hidden unless a group names one with `default_repo` or `exact`, or the reader asks), `include_archived` (`true`), `exclude[]`, `include_private` (`false`) | an account belongs to one source |
| `catalog` | `reconcile_interval` (`5m`), `groups[]`: `id`, `name`, `description`, `default_repo`, `sources`, `match[]` | at most 500 groups, 64 matchers each |
| `web` | `dir`, `node` (`node`), `theme_dir`, `product_name` | `theme_dir`: `theme.css` (colour tokens: `:root:not(.dark)` and `.dark`), `logo.svg`, `favicon.svg`, served under `/theme/`, read at the start; `product_name` replaces the product's name in the pages (operator guide 2.7) |
| `webhook` | `secret_file`, `previous_secret_file`, `catch_up` | the receiver exists only with a secret; `catch_up: true` (needs an App source) reads the App's delivery log every 2 minutes instead, for a portal GitHub cannot reach |
| `access` | `user_header`, `groups_header`, `private_groups` (`["*"]`: everyone signed in), `require_auth` | `include_private` needs `private_groups` and a way to know the viewer |
| `auth` | `session_key_file`, `session_ttl` (`12h`), `admin_emails`, `admin_groups`, `tokens[]` (`name`, `secret_file`, `groups`, `admin`), `oidc.issuer`, `oidc.client_id`, `oidc.client_secret_file`, `oidc.scopes`, `oidc.groups_claim` (`groups`), `oidc.require_verified_email` (`false`: set `true` where the provider sends `email_verified`, or use `admin_groups`) | redirect URI `<public_url>/auth/callback` |
| `llm` | `provider` (`anthropic`, `openai`, `gemini`, `openrouter`, `ollama`), `model`, `base_url`, `api_key_file` | what the tools read goes to the provider |
| `assistant` | `access` (`signed-in`, `admin`, `everyone`), `concurrency` and the `max_*` budgets | |
| `cluster` | `addresses`, `name`, `namespace` | several instances through Hazelcast 5 on a private network |
| `web` | `dir`, `node` | set in the image; for the binary, the unpacked `bundle/` |
| `layout` | `dir` | the administrators' dashboard designs; back it up |
| `packages` | `public` (`true`), `registries[]`: `id`, `ecosystem` (`maven`, `npm`, `pypi`, `cargo`, `go`, `nuget`, `rubygems`, `packagist`), `url`, `network` (`public`, or `private`: on your network, http allowed), `auth` (`user` and `password_file`, or `token_file` with an optional `header`) | credentials go only to the registry's own URL; GitHub Packages needs a token with `read:packages`, not the App |
| `ops` | `pprof` | profiles on the admin listener |
| `enterprise` | `enabled` (`false`), `github[]`: `source`, `web_url`, `graphql_url`, `people` (`false`), `login` (`managed`); `oidc.identity_claim` (`email`), `oidc.userinfo` (`true`); `token_key_file`; `access.refresh` (`15m`), `access.max_age` (`1h`) | GitHub Enterprise Cloud (`https://api.<name>.ghe.com/` with web `https://<name>.ghe.com/`) and Entra ID (`identity_claim: preferred_username`, `userinfo: false`); every key but `enabled` is refused while it is off; `people: true` makes it a configuration of `scp enterprise`, each viewer reading with their own fine-grained token linked at `/github` (operator guide 2.8, 2.9) |

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
| `unclaimed: true` | what no other group selects |

A repository may be in several groups; repositories in none are counted on the dashboard.

## For one person: `scp personal` (experimental)

Someone who wants a portal only for themselves on their own computer needs no `scp.yaml`: `scp personal` opens a page in the
browser where they choose the GitHub host (github.com, an Enterprise Server or a GHE.com tenant), sign in (a code with their own
app's client id, or a pasted token), pick their repositories and sort them into products; it seals the credential in the store and
serves on loopback with a one-time sign-in link (`--terminal` asks in the terminal instead). Its settings are `personal.yaml` in the
directory it prints; read `docs/personal-edition.md` of the release. Use `scp serve` for a portal several people use.
