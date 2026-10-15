---
name: configure-scp
description: Configure a Source Code Portal (SCP) instance from plain wishes ("put all Whydah repos together", "a group for the libraries", "hide forks"), including the repository groups with regexp, prefix, glob, exact and topic matchers, and moving a legacy Java SCP config.json over. Use when the owner wants to set up, change or check scp.yaml, the groups, the GitHub source, webhooks or private repositories.
---

# Configure SCP

You help the portal's owner (Totto, owner of the repositories) get the portal configured the way they describe it, in their words. They
say what they want; you write the YAML, prove it with the portal's own tools and show the result in plain language. They never have
to know key names.

## Ground rules

- The configuration is `scp.yaml` (gitignored; start from `deploy/config.example.yaml` when there is none). Secrets are files outside
  the repository (for example `~/.config/scp/`): reference them by path (`token_file`, `app.private_key_file`, `webhook.secret_file`),
  never read, print or copy their contents.
- The full reference is `docs/operator-guide.md` section 2. Every key is listed there; the portal refuses unknown keys.
- Never call GitHub with write access, never change repositories. Reading the organisation's repository list with `gh` is fine.
- Keep the owner's own names and wording for groups (names and descriptions are shown on the dashboard).
- After every change: run the checks below and show the preview. Do not say "done" before `config check` passes.

## The tools

Build once: `make build` (gives `bin/scp`), or use `go run ./cmd/scp` in place of `bin/scp`.

```sh
bin/scp config check -c scp.yaml                      # validates exactly like serve; every problem with its key; exit 0 = good
gh api --paginate "orgs/<org>/repos?per_page=100" > /tmp/scp-repos.json   # an organisation's repositories (read only)
gh api --paginate "user/repos?affiliation=owner&visibility=all&per_page=100" >> /tmp/scp-repos.json   # their own user account
bin/scp config groups -c scp.yaml --repos /tmp/scp-repos.json            # which group each repository lands in
```

`config groups` prints each group with its members and default repository, then "In no group" and "Left out by the source filters"
(forks, archived or private repositories the source does not include). It uses the portal's own matching code, so what it shows is
what the dashboard will show. Without `gh`, a file with one repository name per line works too (topics, forks and visibility are then
unknown).

## Workflow

1. **Listen.** Ask what they want in their terms if it is unclear: which organisations, which groups, what goes where, what stays out.
   One short question at a time; offer a sensible default ("Shall repositories that fit no group go into an 'Other' group?").
2. **Look.** Fetch the repository list, read the current `scp.yaml` (if any) and run `config groups` on it, so you start from what is
   there.
3. **Write.** Edit `catalog.groups` (and other keys when asked). Show the change as a short diff.
4. **Prove.** `config check`, then `config groups`. Fix every error yourself (they name the key).
5. **Explain.** Summarise the preview in plain words: each group with how many repositories and a few names, the default page, what is
   in no group, what the filters leave out. Point out surprises ("`Whydah-old` is archived and left out: include it?").
6. **Iterate** until they are happy.

## Groups

```yaml
catalog:
  groups:
    - id: whydah                     # a-z, 0-9 and dashes; in the address /groups/whydah; unique
      name: Whydah IAM and SSO       # shown on the dashboard
      description: Identity and single sign-on   # optional
      default_repo: Whydah-SecurityTokenService  # the group's main repository; must be selected by the group's own matchers
      match:                         # 1..64 matchers; a repository joins when ANY matcher selects it
        - regexp: 'Whydah-.*'
        - topic: iam
    - id: other
      name: Other
      default_repo: source-code-portal
      match: [ { catch_all: true } ]
```

Matchers (each entry has exactly one):

| Matcher | Selects | Case |
|---|---|---|
| `exact: Name` | that repository | insensitive |
| `prefix: Whydah-` | names starting with it | insensitive |
| `glob: "*-service"` | whole name, `*` any run, `?` one character (at most 6 `*`) | insensitive |
| `regexp: 'Config(Service\|-.*)'` | **whole name** against an RE2 expression (it is anchored for you: `^(?:...)$`), at most 256 bytes | sensitive; start with `(?i)` for insensitive |
| `topic: iam` | repositories with that GitHub topic | lower-case topics |
| `catch_all: true` | everything (use once, typically last) | |

- A repository can be in several groups; the order of groups is the dashboard order.
- `sources: [github]` on a group limits it to repositories of those sources (only needed with several sources).
- Quote regexps in single quotes in YAML. RE2 has no look-ahead, look-behind or back-references; say so and offer an equivalent.
- Totto likes regexps: use `regexp` when they ask for one or when it states the intent best; for a plain "starts with" a `prefix` is
  clearer. Either way, show the preview.
- Never use `.*` at both ends of a new regexp to mean "starts with": write what is meant (`Whydah.*`), and check the preview.

Typical wishes:

| They say | You write |
|---|---|
| "everything starting with Whydah" | `prefix: Whydah` (or `regexp: 'Whydah.*'`) |
| "the services" (names ending in -service) | `glob: "*-service"` |
| "ConfigService and the JAU tools" | `regexp: 'ConfigService.*\|JAU\|Java-Auto-Update'` |
| "anything tagged iot" | `topic: iot` |
| "the rest in Other" | a last group with `catch_all: true` |
| "no forks / no archived ones" | `github.sources[].include_forks: false` / `include_archived: false` (source filters, not groups) |

## Moving the legacy Java SCP config over

The Java portal read `config.json` with `github.organization` and `github.repos[]` (or `groups[]` with `repository-match` in the newer
example). Map each entry:

| Legacy | SCP |
|---|---|
| `groupId` | `id`, lower-case with dashes (`ConfigService` -> `configservice` or `config-service`; ask if unsure) |
| `display-name` | `name` |
| `description` | `description` |
| `default-group-repo` / `default-group-repository` | `default_repo` |
| `repo` / `repository-match` patterns | `match` entries (below) |
| `github.organization` | `github.sources[0].orgs` |
| `branch`, `external.jenkins`, `external.snyk`, `title` | dropped: SCP reads the default branch and has no Jenkins or Snyk badges |

Legacy patterns were Java regular expressions searched **anywhere** in the name, case-sensitive (`Pattern.compile(p).matcher(name).find()`).
So `Whydah*` really meant "contains `Whyda`", and `JAU` meant "contains JAU". The intent was nearly always a name or a prefix. Translate
by intent, then let the preview confirm:

- `Name` (a plain name) -> `exact: Name`, unless the preview shows the old substring match caught more repositories they want; then a
  `regexp: '.*Name.*'`.
- `Name*` -> `prefix: Name` (or `regexp: 'Name.*'`).
- Anything else -> `regexp:` with the intended whole-name pattern; keep the legacy semantics with `'.*(?:pattern).*'` only when that is
  what they want.

Run `config groups` on the result and compare with what the old portal showed; list every repository whose group changed and ask.

## The rest, when asked

- **GitHub source** (`github.sources[]`): `id`, `orgs` (organisations and user accounts; an account belongs to one source), and either a GitHub App (`app.id`, `app.private_key_file`; preferred) or
  `token_file`; GitHub Enterprise sets `base_url`. Filters: `include_forks` (default false), `include_archived` (default true),
  `include_private` (default false; needs `access.private_groups` and `server.trusted_proxies`, operator guide 2.1).
- **Webhook**: `webhook.secret_file`; the GitHub side is in the operator guide section 1.
- **Public address**: `server.public_url` (absolute links, HSTS on https).

Finish with `config check` passing, the preview shown, and a one-paragraph summary of what the portal will show.
