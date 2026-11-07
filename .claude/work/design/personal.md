# The personal edition (rung 0 of the ladder, PE1)

Status: built, "my work" included (plan.md section 4, P-92). Binding order: ../plan.md > this note > `store-model.md`, `auth.md`, `access.md`.

One person runs the portal for themselves, on a laptop or a server of their own, against one GitHub host (github.com or a GitHub
Enterprise Server), and sees every repository that person can read. It is the first rung of the ladder in `store-model.md` (P-91): one
audience, one credential holder, the security model of today unchanged (I1 to I6 there). `scp personal` is the command.

## The choice

**`scp personal` keeps everything in one directory (`--dir`, default `<user config dir>/scp`), signs the person in to GitHub in a
terminal wizard, seals the credential in the store, writes an ordinary `personal.yaml` the person may edit, and serves on loopback
only.** A person who already runs `scp serve` with a token file is unaffected.

- **The directory** holds `scp.db` (the store), `personal.yaml` (the configuration: no secret in it), `session.key` and `owner.token`
  (local secrets, mode 0600, the directory 0700). The portal reads no other file of the person's.
- **The credential** is a row of `credential` (migration `0003`: only `credential` at this rung; `principal` arrives with rung 1, when more than one person exists), sealed with AES-256-GCM under a key
  derived (HKDF) from a 32-byte vault key, with the row's id, source and kind as additional data, so a value moved to another row
  does not open. The vault key lives in the operating system's keychain (macOS Keychain, the Secret Service on Linux, the Credential
  Manager on Windows); where there is none (a server without a desktop) it is a file `vault.key` (0600) in the directory and the wizard
  says so. The token is never written to a log, a status page, a snapshot or an API answer.
- **Signing in to GitHub**, in the wizard:
  1. *The host*: `github.com` (the default), a GitHub Enterprise Server (`https://host`, REST at `https://host/api/v3/`), or a
     GHE.com tenant (`https://<name>.ghe.com`, REST at `https://api.<name>.ghe.com/`).
  2. *How*: **device flow** with a client id of an OAuth app or a GitHub App the person registered on that host (`--client-id`, or
     asked): the wizard shows the code and the page, waits, and receives a token; or **paste a token** (a fine-grained token with read
     access, a classic one, or a GitHub App user token). A token that expires is recorded with its end; the portal says so when it
     has ended and the person runs the wizard again (no refresh is kept: GitHub's refresh needs the app's client secret, which a
     personal installation does not hold).
  3. *Which repositories*: the person's own account and the organisations `GET /user/orgs` lists, chosen by number (all by default).
     With the OAuth scopes the wizard asks for (`repo read:org` to read private repositories, `read:org` or none for public ones) it
     says plainly that a classic OAuth scope `repo` carries write access that SCP never uses: it only sends GET requests (P-71), and a
     GitHub App with read-only permissions is the safer grant.
- **Signing in to the portal**: the portal is on `127.0.0.1` and refuses every request without a session (`access.require_auth`).
  The command prints a launch link, `http://127.0.0.1:PORT/auth/launch?code=…`, whose code is random, valid ten minutes and for one
  use; it sets the sealed session cookie of the owner token (an administrator who sees the private repositories, the existing
  session for a token) and redirects to the dashboard. Another process or user on the machine has no code and no cookie.
- **The same code serves everything else**: the sync, the views, the features. `personal.yaml` is a normal configuration with one
  source whose token is `credential: true`, the credential of the vault, read when the command starts; the groups, the layouts and the
  rest can be set in it as in any configuration (the `configure-scp` skill works on it).

## What is not in it

No hosted multi-person mode (rung 1 and on), no refresh of expiring tokens, no web wizard (the terminal one is the first; a page of
`/admin` can follow), no GitHub App registration flow (the person registers the app on their host; `docs/personal-edition.md` says
how and with which permissions).

## Tests

The vault (a value opens only under its own row, source and kind, a flipped bit is refused, the file holds no plaintext, the key from
the keychain or the fallback file); the device flow against a fake host (pending, slow down, denied, expired, success); the host forms;
the wizard end to end against the fake GitHub (a token, the organisations offered and chosen, the files written, the credential
sealed); the command: start, launch link, session, the dashboard answering with the owner's private repository and a request without
a session refused.

## As built

- `internal/vault` (sealing, `OpenKey`, `Keychain`), `store.credential` (migration 0003), `internal/ghlogin` (hosts, device flow,
  `Whoami` with the token's end from `github-authentication-token-expiration`, `Orgs`, `Newest`), `auth.Launch` (`GET /auth/launch`),
  `cmd/scp/personal.go`.
- A source says `credential: true`; `scp personal` opens the vault and puts the token in the source before `newServer`; `newServer`
  refuses a source with `credential` and no token, so `scp serve` never starts with an unresolved one.
- The wizard writes one catch-all group, "My repositories", whose default repository is the one the person pushed to last; the
  person edits `personal.yaml` for groups of their own.
- A credential that has ended (the device flow's `expires_in`, or the header a fine-grained token's `/user` answer carries) is signed
  in again at the next start without touching `personal.yaml`; `--reset` asks everything and keeps the old file as `.bak`.
- The portal refuses configurations whose listeners are not loopback (`requireLoopback`).
- Not verified against a real GitHub Enterprise Server: the device flow's availability on a given version, and the REST base paths
  (`/api/v3/`, `api.<name>.ghe.com`) are from GitHub's documentation. A pasted token needs only the REST calls.

## My work (PE2)

- The viewer may carry a GitHub login (`Identity.Login`): `auth.tokens[].login`, validated as a login; `scp personal` writes the
  person's own. It reaches the web UI server in `X-Scp-Login` and `GET /api/v1/me` as `login`. It only chooses what "mine" means in a
  list; it grants nothing and no list shows more than the viewer's view held already.
- Pull requests keep the logins of the people asked to review (`Pull.Reviewers`, up to 10, from `requested_reviewers` of the listing
  already read); teams stay a count. `GET /api/v1/pulls` takes `author` and `reviewer`, `GET /api/v1/issues` `author` and `assignee`
  (logins, case-insensitive); the counts cover the narrowed set, so the states' chips stay true.
- The filter panels of `/pulls` and `/issues` show a "Mine" group of switches (`MineToggles`) when `me.login` is known. Each sets the
  field to the login in the address, a plain GET form field, so it works without JavaScript and a page can be bookmarked.
- Not done: "my work" for commits and a dashboard element; the agents' tools do not take the parameters yet.

## The first run in the browser (PE3)

The terminal wizard asked what a person cannot be expected to know (a host, a client id, account numbers), and gave one group. The
default first run is a page (`internal/setup`, `cmd/scp/personal_setup.go`); `--terminal` keeps the wizard.

- **Flow.** `scp personal` (no `personal.yaml`, or `--reset`) listens on `127.0.0.1:<port>` (the portal's own port, so the page and
  the portal share an address), prints `http://127.0.0.1:<port>/setup/?code=<entry>` and opens it (`--no-browser`: only prints). Steps:
  sign in (device flow with `--client-id`, else a pasted token; host forms as `ghlogin.ParseHost`), accounts (`Orgs`), products
  (`Repos`, `Suggest`, edited on the page), start. `Finish` seals the credential, writes `personal.yaml`, shuts the setup server down
  and the portal starts on the same address; the page waits for `/api/v1/me` to answer JSON and follows the launch link.
- **Products.** `Suggest`: a first word shared by at least 3 repositories (`whydah-*`), then leftovers by owner when there is more
  than one, at most 50 products of 64 repositories (the group matcher limits). Each product becomes a group of `exact` matchers
  (`default_repo`: its first repository); hidden repositories go to the source's `exclude` (at most 64); repositories left in no
  product go to an `unclaimed: true` group "Other". The list is the token's view, 500 repositories at most; a person with more edits
  `personal.yaml`.
- **Security** (the portal's model unchanged; `docs/security-model.md`). The page is served on loopback only. The entry code is
  random, works once and turns into an `HttpOnly`, `SameSite=Strict` cookie. A `Host` other than `127.0.0.1:<port>` or
  `localhost:<port>` is refused (DNS rebinding), every POST must be same-origin (`Sec-Fetch-Site`, else `Origin`), JSON only, bodies
  at most 256 KiB. The CSP allows only the page's own script and style (no inline). The token lives in the setup server's memory and
  is never sent to the page; a sign-in cancelled by another is dropped by a generation counter. Nothing the page shows is
  repository-authored except names, which are drawn as text nodes.
- **The GitHub CLI.** When `gh` is on the `PATH`, the page offers its login: `POST /setup/api/cli` runs `gh auth token --hostname <host>` (10 s limit, the host name a parsed URL's) and checks the token like a pasted one; nothing is registered. The token is as broad as `gh` was granted (usually `repo`, write-capable on GitHub; SCP never writes), which the page says.
- **Client id.** `defaultClientID` in `personal_setup.go` is empty until a GitHub App with device flow and read-only permissions is
  registered; until then github.com signs in by pasted token or `--client-id`.
- **Not done.** A credential that has ended signs in again through the terminal wizard, not the page.
- **Tests.** `internal/setup` (the entry, refusals, a token, a code, a code for another host, `Suggest`); `TestPersonalBrowserSetup`
  drives the page's API against the fake host and checks `personal.yaml`, the sealed credential, file modes and the freed port; the
  flow was also walked in a real browser against a fake GitHub.
