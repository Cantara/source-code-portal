# The personal edition (rung 0 of the ladder, PE1)

Status: in progress (plan.md section 4, P-92). Binding order: ../plan.md > this note > `store-model.md`, `auth.md`, `access.md`.

One person runs the portal for themselves, on a laptop or a server of their own, against one GitHub host (github.com or a GitHub
Enterprise Server), and sees every repository that person can read. It is the first rung of the ladder in `store-model.md` (P-91): one
audience, one credential holder, the security model of today unchanged (I1 to I6 there). `scp personal` is the command.

## The choice

**`scp personal` keeps everything in one directory (`--dir`, default `<user config dir>/scp`), signs the person in to GitHub in a
terminal wizard, seals the credential in the store, writes an ordinary `personal.yaml` the person may edit, and serves on loopback
only.** A person who already runs `scp serve` with a token file is unaffected.

- **The directory** holds `scp.db` (the store), `personal.yaml` (the configuration: no secret in it), `session.key` and `owner.token`
  (local secrets, mode 0600, the directory 0700). The portal reads no other file of the person's.
- **The credential** is a row of `credential` (migration `0003`: `principal`, `credential`), sealed with AES-256-GCM under a key
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
