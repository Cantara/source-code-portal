# Enterprise (EN1 to EN8)

Status: EN1, EN3, EN4 and EN5 built, EN6 to EN8 in progress, EN2 planned. Binding order: ../plan.md > this note.

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

## Done when (EN1)

Code and tests as above, `make check` green, `docs/operator-guide.md` and the plugin's reference describe the section, the code map
in `.claude/CLAUDE.md` is current. Push and pull request only on the owner's go.

# The enterprise mode (EN3 to EN8, P-110)

## Goal

`scp enterprise` serves GitHub Enterprise organisations to everyone who signs in through the organisation's OIDC provider and has
linked their own GitHub token. Each viewer sees only the repositories GitHub lets them read, everywhere in the portal. Repository data
is polled once with the people's tokens and shared; only the access list is per person. No admin rights are needed on GitHub or on the
identity provider: no App, no webhook.

## The rules every milestone keeps

1. **A mode of its own.** A third command beside `scp serve` and `scp personal`. Its store belongs to the mode `enterprise` (`store.OpenAs`)
   and refuses the other modes' stores, as they refuse its. No public view, no anonymous request but the open paths, no webhook
   receiver, no source with a credential of its own, no proxy identity headers: a viewer is who OIDC signed in.
2. **Fail closed, decided in one place.** The view a request reads is chosen in `server.views` (`cmd/scp/serve.go`), the one hook
   every surface reads. In the enterprise mode it is `peopleViews.of`: the published snapshot restricted to the viewer's access set
   (`snapshot.Restrict`). A viewer without a fresh access list reads the empty view. No feature decides access.
3. **Data lives in SQLite**: people, their sealed tokens and their access lists are rows, written through before acknowledged, loaded at
   the start; additive migrations only (P-89).
4. **Generic**: any GitHub Enterprise (Server, Cloud, data residency) and any OIDC provider.

## Configuration

```yaml
auth:
  session_key_file: /run/secrets/session
  oidc: {issuer: https://login.example/tenant/v2.0, client_id: scp, client_secret_file: /run/secrets/oidc}
github:
  sources:
    - {id: ghe, base_url: https://api.octocorp.ghe.com/, orgs: [octocorp]}   # no app, token_file or credential
enterprise:
  enabled: true
  token_key_file: /run/secrets/tokens   # at least 32 bytes: seals the people's tokens in the store
  github:
    - {source: ghe, web_url: https://octocorp.ghe.com/, people: true, login: managed}
  access: {refresh: 15m, max_age: 1h}
```

| Key | Checked by `scp config check` |
|---|---|
| `github[].people` | true: the source is read with its viewers' tokens; then every source must be, and has no `app`, `token_file` or `credential` |
| `github[].login` | only with `people`; `managed` (default) or `exact` |
| `token_key_file` | required with `people`, refused without; at least 32 bytes |
| `access.refresh` | 1m..24h, default 15m |
| `access.max_age` | from `refresh` to 168h, default 1h |
| with `people` | `auth.oidc.issuer` required; `webhook.*`, `auth.tokens` and `access.private_groups` refused; `access.require_auth` implied; `include_private` implied (the access list decides) |

`scp serve` refuses a configuration with `people`, and `scp enterprise` one without (`deps.checkedMode`).

## EN3: the mode (built)

- `scp enterprise` (`cmd/scp/enterprise.go`, also in the server build) runs `runServer` with `store.Enterprise`.
- `store.Enterprise`: a fresh store is marked for it; a store holding a catalogue or credentials without its mark is refused.
- A `people` source has no credential: every call of it fails with the `Auth` error `no_person` before a request is sent
  (`github.Source.client`), so GitHub is never asked anything anonymously.
- `snapshot.Restrict(allowed, forks)`: the published snapshot cut to a set, with only the groups that keep a member, its own indexes
  (built with no fallback to the full snapshot's), its own public view. `peopleViews` keeps the restricted views per snapshot, set key and
  choice of forks (LRU, 64). Until EN5 every viewer's set is empty (`nobody`).
- Readiness does not wait for a catalogue in the mode: it has none until a person links a token.

Acceptance: `TestEnterpriseModeKeepsApart` (each command refuses the other's configuration and store; the mode's routes are the shared
site's), `TestEnterpriseFailsClosed` (an empty store starts and is ready; every contract path and page answers 401 anonymously, a
forged proxy header included; a signed-in person without a list reads none of what the snapshot holds; the fake GitHub saw no
request), `TestRestrict`, `TestPeopleSourceSendsNothingWithoutAToken`, `TestTheEnterpriseModeKeepsItsOwnStore`.

## EN4: people link their tokens (built)

`GET`, `POST` and `DELETE /api/v1/me/github/tokens` (signed in, same origin): a fine-grained token per organisation, checked before it is
kept (`GET /user` answers; its login is bound to the identity claim by `login`; it reads the organisation's repositories), sealed
(AES-256-GCM, a key derived with HKDF from `token_key_file`, the person and organisation as additional data) in the mode's own tables
(migration `0002`), never shown again. The page builds GitHub's pre-filled creation link from the web address, the organisation and
the read permissions. Token health: its expiry from GitHub's answer header; a refused token marks the person's list unknown.

Built as `internal/people` (`People`: `Seen`, `Link`, `Unlink`, `Tokens`, `Overview`, `Routes`), the store's `person`, `person_token`
and `person_access` (migration `0002_people.sql`) and `github.Source.CheckToken` (`GET /user`, then one repository of
`GET /orgs/{owner}/repos`). Only a token beginning `github_pat_` is taken: a classic token carries write scopes. A refusal is a 422
problem with its code: `unknown-owner`, `not-fine-grained`, `not-yours`, `unauthorized` (wrong, expired, revoked, or the enterprise's
network rules), `forbidden` (awaiting the organisation's approval), `reads-nothing`, `expired`, `unavailable`. Replacing a token drops
what its predecessor listed. The person is `<issuer>#<subject>`; a person row is written at most hourly on use.

Acceptance: `TestLink` (each refusal, a token sealed, kept across a restart, removed), `TestBound`, `TestCreationLink`, `TestSealing`,
`TestCheckToken`, `TestPeopleSurviveARestart`, `TestEnterpriseLinksTokens` (the API signed in, anonymous and cross-origin refused, the
secret never answered).

**Binding (`login`).** `managed`: the identity claim's part before `@`, every character but letters, digits and `-` as `-`, must equal
the login before its last `_` (a managed user's login is that, `_` and the enterprise's short code), compared without case. `exact`: the
login equals the identity claim, without case.

## EN5: access lists (built)

Per person, after linking and every `access.refresh`: the repositories each of their tokens lists (the organisation's listing read with
it, `GET /orgs/{owner}/repos?type=all`, private and internal), written in one transaction with the time read. Fresh means read within
`access.max_age`; older, refused or never read is unknown: the empty view. The view keeps only what the catalogue holds.

Built as `People.Run` (the loop `cmd/scp` runs beside the engine and stops before the store closes), `listDue` and `list` (through
`github.Source.ListWith`: a credential per token, its own rate book and conditional answers, under a key that changes with the token),
`refused` (a 401, a 403 or a 404 drops what the token read and records why), a minute's retry for any other failure, and `Allowed`
(the person's fresh repositories by GitHub id with a key equal for equal sets). `peopleViews` maps the ids to the snapshot's addresses
and restricts. A list read before a restart counts from the store until it is read again; the listed repositories themselves are held
in memory for EN6.

Acceptance: `TestAccessLists` (two people, two lists, kept across a restart; a revoked repository leaves at the next refresh; a refused
token reads nothing; GitHub unavailable past `max_age` reads nothing, and the list returns once GitHub does; a removed token reads
nothing).

## EN6: the shared fetch through people's tokens

The source's credential for a repository is a fresh token of a person whose list holds it (`github.People`), the one with the most rate
left; another when it fails. The catalogue is the union of what linked people list; nothing is fetched twice.

## EN7: per-viewer views

The access set's view everywhere (`peopleViews` with the person's list); the leak and twin tests with two people of different access;
the load check with many people and few distinct sets.

## EN8: the experience and the documents

The onboarding page (the empty view: what to do, the pre-filled links, each token's and list's state), the operator guide's section for
the mode, the plugin's reference and the Guide.
