# Enterprise (EN1, EN2)

Status: EN1 in progress, EN2 planned. Binding order: ../plan.md > this note.

A portal that reads GitHub Enterprise (Server, or Cloud with data residency on `*.ghe.com`) and signs people in with Microsoft
Entra ID needs a few settings that github.com and a generic OpenID Connect provider do not: GitHub addresses that cannot be worked
out from each other, and the claims an Entra ID token carries. They live in one place, the `enterprise` section, behind its own
switch (owner, 2026-11-13, P-109). Off, which is the default, the portal behaves exactly as without the section.

## Goal

An operator points SCP at a GitHub Enterprise host by configuring its three addresses, and signs people in with Entra ID, matching
administrators by the claim Entra ID always sends, without a call to an endpoint that adds nothing.

## Configuration

```yaml
enterprise:
  enabled: true                       # default false: every other enterprise key is refused while it is off
  github:
    - source: ghe                      # a github.sources[].id
      web_url: https://octocorp.ghe.com/          # links to repositories and files
      graphql_url: https://api.octocorp.ghe.com/graphql   # optional: default beside the source's base_url
  oidc:
    identity_claim: preferred_username # default email: the claim matched against auth.admin_emails and shown as the viewer
    userinfo: false                    # default true: false never calls the provider's userinfo endpoint
```

The source's API address stays `github.sources[].base_url`. Without an `enterprise.github` entry for it, a source keeps today's
derived addresses: the web address is the base without `/api/v3/` (github.com for `api.github.com`), GraphQL is `/api/graphql`
beside an Enterprise Server base and `/graphql` beside any other.

| Key | Checked by `scp config check` |
|---|---|
| `enterprise.*` | any key but `enabled` while `enabled` is false is an error |
| `github[].source` | names a configured source, at most once |
| `github[].web_url` | required; an absolute `https` URL (`http` only on loopback) ending in `/` |
| `github[].graphql_url` | optional; an absolute `https` URL (`http` only on loopback) |
| `oidc.*` | needs `auth.oidc.issuer` |
| `oidc.identity_claim` | a claim name; with `auth.oidc.require_verified_email` it must be `email` (`email_verified` speaks only of the email) |

## Behaviour

- **Addresses (EN1).** The web links the portal builds itself from a source (`github.Source.WebURL`: the documents' links to
  repository files) use the configured web address; links GitHub's answers carry (`html_url`) are used as given. GraphQL (diff
  stats, pull request state) posts to the configured endpoint. REST stays on `base_url`.
- **Identity (EN1).** The session's email, matched case-insensitively against `auth.admin_emails` and shown as the viewer, is the
  `identity_claim`'s value. The `email_verified` claim is consulted only when the identity claim is `email`. The displayed name is
  unchanged (`name`, then `preferred_username`, then `email`).
- **Userinfo (EN1).** With `userinfo: false` the callback uses the ID token's claims only. Groups still come from
  `auth.oidc.groups_claim` (`groups` or `roles`).
- **Logout (EN2, planned).** Sending `id_token_hint` (and `logout_hint`) to the provider's end-session endpoint, as an option, once
  a real sign-in shows whether the provider needs it. Not built.

## Acceptance

1. With no `enterprise` section, every existing test passes unchanged.
2. A source configured for `octocorp.ghe.com` gives the web address `https://octocorp.ghe.com/` and posts GraphQL to
   `https://api.octocorp.ghe.com/graphql`; an Enterprise Server shape (`https://ghe.example/api/v3/`) keeps `/api/graphql`; a document's
   link to a repository file points at the configured web host.
3. A token whose `preferred_username` is in `auth.admin_emails` and that has no `email` signs in as an administrator with
   `identity_claim: preferred_username`; with `userinfo: false` the fake provider's userinfo endpoint is never called.
4. `scp config check` refuses each invalid combination in the table, with the key in the message.

## Done when

Code and tests as above, `make check` green, `docs/operator-guide.md` and the plugin's reference describe the section, the code map
in `.claude/CLAUDE.md` is current. Push and pull request only on the owner's go.
