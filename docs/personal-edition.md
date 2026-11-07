# The personal edition

A portal of your own, on your own computer: you sign in to GitHub once, and the portal shows every repository **you** can read, the
private ones too. It is for one person. Nothing is exposed beyond the computer, and there is no configuration to write first.

```sh
scp personal
```

The first run asks a few questions; every later run starts at once and prints a link that signs you in to the portal. To share a
portal with other people use `scp serve` ([operator guide](operator-guide.md)).

You need the `scp` program, the web UI bundle beside it (`web/`, from the release's `scp-web_<version>.tar.gz`; `--web-dir` names it
elsewhere) and Node.js on the `PATH`.

## The first run

1. **The host.** `github.com`, a GitHub Enterprise Server (`https://github.example.org`) or a GHE.com tenant (`https://name.ghe.com`).
2. **How you sign in.** Either of:
   - **A code on GitHub** (the device flow). You need the *client id* of an OAuth app or a GitHub App you registered on that host
     (below). SCP shows a code and a page; you confirm on GitHub and SCP receives a token. Give the id with `--client-id` or at the
     question.
   - **A pasted token.** A fine-grained personal access token with read access to the repositories you want (Contents, Metadata, and
     for the pull requests and issues pages Pull requests, Issues and Commit statuses), or a classic one with the scopes `repo` and
     `read:org`. The input is not shown.
3. **Which accounts.** Your own account and the organisations GitHub lists for you; choose by number, or Enter for all.

SCP checks the token (it asks GitHub who you are), seals it and writes `personal.yaml`. Then it starts.

### Registering an app for the code sign-in

On GitHub: *Settings, Developer settings*. Either:

- **A GitHub App** (the safer grant). Give it only read-only permissions (Contents, Metadata, and optionally Pull requests, Issues,
  Commit statuses), tick *Enable Device Flow*, and let it be installed on the accounts you want. Use its client id.
- **An OAuth app.** Tick *Enable Device Flow*. Asking for private repositories needs the scope `repo`, which on GitHub also carries
  write access. SCP never writes (it sends GET requests and GraphQL queries only), but the token could; prefer the GitHub App, or a
  fine-grained token.

A GitHub Enterprise Server must offer the device flow for the code sign-in; a pasted token works everywhere.

## Every run

`scp personal` opens the sealed credential, starts the portal on `127.0.0.1:9090` and prints

```
http://127.0.0.1:9090/auth/launch?code=…
```

Open it once: it signs you in (the link works for ten minutes and one time, from the first browser that uses it). Your session lasts
until you sign out, up to 12 hours at a time; close the terminal's portal with Ctrl-C and start it again for a new link. Anybody else
on the computer has no link and no session, and the portal refuses them.

## The files

Everything is in one directory, `scp` under your user configuration directory (`~/Library/Application Support/scp`,
`~/.config/scp`, `%AppData%\scp`) or the one `--dir` names. It is private to you (mode 0700).

| File | What it is |
|---|---|
| `personal.yaml` | The settings, an ordinary [configuration](operator-guide.md#2-configure) you may edit (groups, features, the assistant, ...); no secret is in it. `scp config check -c personal.yaml` checks it. |
| `scp.db` | The store: what the portal learned, and your credential **sealed** (AES-256-GCM). Deleting it costs a resync and the credential: run `scp personal` again. |
| `session.key`, `owner.token` | Local secrets that sign you in to the portal. |
| `vault.key` | Only where the computer has no keychain: the key that seals the credential. |
| `layout/` | The layouts and announcements you edit in the portal. |

The key that seals the credential lives in the operating system's keychain (macOS Keychain, the Secret Service on Linux, the Windows
Credential Manager). Without one (a server with no desktop) it is the file `vault.key`, readable by you only, and the first run
says so; then anyone who can read your files can read the credential, so keep the directory private.

## Your own work

Because `personal.yaml` gives your GitHub login to your sign-in (`login:` of the owner token), the Pull requests and Issues pages
show a **Mine** group in their filter panel: *Opened by me* and *Asking my review* for pull requests, *Opened by me* and *Assigned
to me* for issues. They are plain address fields (`?reviewer=your-login`), so a filtered page can be bookmarked and the counts of the
states cover what is yours. Review requests to a team are not expanded to its members.

## Changing things

- **The accounts, groups, features**: edit `personal.yaml` and start again. The key `github.sources[].credential: true` means "the
  token is in the vault".
- **Another port**: `--port` on the first run (or `--reset`), or edit `server.public_addr`, `server.admin_addr` and `server.public_url`.
  The portal serves loopback addresses only.
- **Start over**: `scp personal --reset` asks everything again; the old file is kept as `personal.yaml.bak`.
- **A token that ends**: GitHub tells SCP when a fine-grained token or a code sign-in ends. SCP says so at the next start and signs
  you in again; it does not refresh tokens.
- **Sign out of GitHub**: revoke the token or the app's authorisation on GitHub, delete `scp.db` and `vault.key`.

## What it is not

One person, one computer, one host. It does not share your private repositories with anyone: the portal refuses every request that is
not yours. A portal for a team is `scp serve`, with its own sign-in, groups and a credential the operator chooses; see the
[security model](security-model.md).
