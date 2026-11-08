# The personal edition

A portal of your own, on your own computer: you sign in to GitHub once, and the portal shows every repository **you** can read, the
private ones too. It is for one person. Nothing is exposed beyond the computer, and there is no configuration to write first.

```sh
scp personal
```

The first run opens a page in your browser (a link on `127.0.0.1`, printed in the terminal too); every later run starts at once
and prints a link that signs you in to the portal. To share a portal with other people use `scp serve` ([operator
guide](operator-guide.md)).

You need the full `scp` program (`scp_<version>_<os>_<arch>`; the server build, `scp-server_*` and the container image, does not include `scp personal`), the web UI bundle beside it (`web/`, from the release's `scp-web_<version>.tar.gz`; `--web-dir` names it
elsewhere) and Node.js on the `PATH`.

## The first run

The page walks you through five steps:

1. **Sign in.** Choose the host (`github.com`, a GitHub Enterprise Server such as `https://github.example.org`, or a GHE.com tenant
   such as `https://name.ghe.com`) and either:
   - **Sign in with GitHub** (the device flow). The page shows a code and a link to GitHub; you type the code there, approve, and
     the page moves on by itself. This needs the *client id* of an OAuth app or a GitHub App registered on that host (below),
     given with `--client-id`.
   - **Use my GitHub CLI login.** Offered when the GitHub CLI (`gh`) is installed: SCP asks `gh auth token --hostname <host>` for the
     token `gh` already holds and uses it like a pasted one. Nothing is registered and nothing opens on GitHub. The token is
     whatever `gh auth login` granted (by default `repo`, which on GitHub also allows writing; SCP never writes), so a fine-grained
     token is the safer grant.
   - **Paste a token.** A fine-grained personal access token with read access to the repositories you want (Contents, Metadata, and
     for the pull requests and issues pages Pull requests, Issues and Commit statuses), or a classic one with the scopes `repo` and
     `read:org`. The input is not shown again.
2. **Accounts.** Your own account and the organisations GitHub lists for you; untick the ones you do not want. The page then reads
   their repositories as a background job with a progress bar (the account being read, how many of about how many, the time left)
   and a Cancel button; nothing is asked of you while it runs. There is no cap on the list (an account is read up to its 2,000 most
   recently pushed repositories).
3. **Events (optional).** For each account you can make a **GitHub App** of your own with one click: the page sends you to GitHub
   with a ready manifest (a private App that can only *read* what the portal needs: contents, metadata, pull requests, issues,
   checks and commit statuses, and the push, release, repository, pull request and issue events), you confirm there, GitHub brings
   you back, and you install the App on the account (for an organisation you must be an owner). "I installed it" checks with GitHub.
   An account with the App installed is kept fresh by GitHub's events within a couple of minutes: the portal reads the App's
   delivery log, so nothing has to reach your computer. Every other account (you skipped it, could not install, or have no
   permission) is polled with your token, as before. The App's private key is sealed in the same vault as the token and never shown.
   The App is yours: delete it on GitHub (Settings, Developer settings) whenever you like.
4. **Products.** Your repositories are listed per account at the left (search names and topics, show only those without a product)
   and your products stand at the right. A first draft is suggested from the names (`whydah-*` becomes *Whydah*; the rest by
   account). **Drag** repositories into a product (several at once if you ticked them; Shift-click ticks a range), or tick them and
   use the "Move" menu, which also works with the keyboard and on a phone. Drop on *New product* to make one, on *Other* to take a
   repository out of its product, on *Hidden* to leave it out of the portal.

   **The portal works out the rules.** From the repositories in a product the page derives what they share: a name that starts the
   same (`name starts with “whydah-”`) or a topic. Such a rule is *open*: a repository you create next week called `whydah-foo`
   joins the product by itself, so you do not come back to maintain it. It tells what each rule catches, shows repositories no
   product holds that start the same ("3 more repositories start with “kcp-” ... Add them with a rule") and never writes a rule that
   would also catch a repository you gave to another product. A repository no rule fits is pinned by name. You can switch the rules
   off for a product ("Work out rules so new repositories fit"), add your own (starts with, topic, a name pattern, or a regular
   expression, each tested against your repositories before you add it) and remove them. A product's matchers are what
   `personal.yaml` then holds, as `prefix`, `topic`, `glob`, `regexp` and `exact` ([the operator guide](operator-guide.md#2-configure)).
   A rule looks at a repository's name and topics, not at the account, so a product spans the accounts you chose.
5. **Start.** The portal starts on the same address and the page signs you in.

SCP seals the token (and the Apps' keys), writes `personal.yaml` (one source per way an account is read: a source `app-<account>` for
each account with its App, `webhook.catch_up: true` then, and the source `github` with the token for the others; the products become
groups) and starts. The page closes
the setup for good: it works once, from the browser the link was opened in, and only while `scp personal` waits for it.

`--no-browser` prints the link without opening a browser; `--terminal` asks the same questions in the terminal instead (the host,
the sign-in, which accounts; one group, "My repositories", holding everything).

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

- **The accounts, products, features**: edit `personal.yaml` and start again (the products are `catalog.groups`). The key `github.sources[].credential: true` means "the
  token is in the vault".
- **Another port**: `--port` on the first run (or `--reset`), or edit `server.public_addr`, `server.admin_addr` and `server.public_url`.
  The portal serves loopback addresses only.
- **Start over**: `scp personal --reset` opens the page again; the old file is kept as `personal.yaml.bak`.
- **A token that ends**: GitHub tells SCP when a fine-grained token or a code sign-in ends. SCP says so at the next start and signs
  you in again; it does not refresh tokens.
- **Sign out of GitHub**: revoke the token or the app's authorisation on GitHub, delete `scp.db` and `vault.key`.

## Kept apart from a shared portal

`scp personal` and `scp serve` are two modes of one program, chosen by the command and by nothing in a configuration. A store belongs to
one of them from its first use (the store records it): `scp serve` refuses a store that `scp personal` made, and `scp personal`
refuses a shared portal's store, before reading or changing anything, so pointing `--dir` or `store.path` at the other's file cannot
mix them. Give each its own directory.

## What it is not

One person, one computer, one host. It does not share your private repositories with anyone: the portal refuses every request that is
not yours. A portal for a team is `scp serve`, with its own sign-in, groups and a credential the operator chooses; see the
[security model](security-model.md).
