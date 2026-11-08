# Security model

What the portal guarantees about private repositories, what you as the operator provide, and what it does not do. It is written for
a portal whose repositories are mostly private, where a leak is a confidentiality incident; a portal of public repositories needs
none of the operator duties below except the network layout.

## What the portal guarantees

- **Two views.** Each snapshot is built twice: the full view, and the public view without private and internal repositories (their
  names, descriptions, topics, READMEs, documents, commits, releases, dependency links and group memberships). A request is answered from
  the public view unless its viewer is in `access.private_groups` or is an administrator. The decision is made once per request, by
  the server, from the viewer's identity.
- **Every surface is the same.** Pages, the JSON API, badges, the image proxy, the Markdown answers, `/llms.txt`, `/llms-full.txt`,
  `/knowledge.yaml`, the sitemap, the event stream, the search (its hits, counts, corrections and suggestions: a view's index holds
  only that view's repositories), the assistant, the agents' tools (`/api/v1/tools`, `/mcp`, WebMCP) and the old
  Java-portal addresses all read the viewer's view. A test
  (`cmd/scp/leak_test.go`) fetches every GET of the API contract and every other public route as the public, and fails if any answer
  carries a trace of a private repository; the assistant has its own (`cmd/scp/assistant_test.go`), and every tool is called as the public too (`TestNoLeakThroughTools`,
  `cmd/scp/mcp_test.go`). It also fails if an address naming a private repository answers differently from one that does not
  exist: the public cannot tell that a private repository exists. A second test (`cmd/scp/twin_test.go`) runs the same portal
  twice, once without the private repository, and fails unless the public reads both word for word, through every route, every
  tool and MCP, anonymous or with a token of no private group: no count, score, total or order may move with a private repository.
  It also tries the private name's variants (case, escapes, a trailing slash), filters, misspellings and prefixes the search could
  correct or complete, a staff member's ETag replayed, and the admin routes on the public port; a push to a private repository must
  not move the public snapshot's version or its event stream. A group whose default repository is private reads to the public as a
  group whose default does not exist.
- **No public view at all, if you want.** With `access.require_auth: true`, every request without an identity gets 401, and a browser
  is sent to sign in. Only sign-in, `/api/v1/me` (the sign-in methods), `/webhooks/` (signed by GitHub) and `robots.txt` (which then
  disallows everything) stay open.
- **Identity.** A viewer is identified by a session (OIDC sign-in, sealed cookie), a bearer token (`auth.tokens`, compared in constant
  time), or the user and groups headers of an authenticating proxy, believed only from addresses in `server.trusted_proxies` and
  removed from every other request. A credential that names nobody is refused, never served the public view.
- **Caches.** Every answer to an identified viewer is `Cache-Control: private`. Every answer varies on the proxy's identity headers
  while private repositories are admitted, and on `Cookie` and `Authorization` when sign-in or tokens are configured, so a shared
  cache never serves one viewer's answer to another.
- **Never catalogued.** `github.sources[].exclude` keeps repositories out of the catalogue, the cache and every view, even where the
  credential can read them.
- **Removal.** A repository turned private leaves the public view at the next listing (at once with the webhook). A deleted or
  transferred repository is removed after two complete listings miss it; the second runs 30 seconds after the first.
- **Audit.** Every request from a viewer of the private view leaves a log line `audit` (user, how they signed in, method, route, path,
  status, request id), never the answer. Metrics and `/api/v1/status` carry no repository names.
- **Repository text is untrusted.** Rendered documents pass one sanitiser. Markdown answers, `/llms*.txt`, `/knowledge.yaml` and the
  assistant's tool results have control and invisible characters removed, and are marked as data, never instructions.
- **Read-only towards GitHub.** The portal asks for read permissions only and sends only reads: GET requests and GraphQL queries,
  never a write or a mutation. The fake GitHub every test runs against fails a test that sends anything else
  (`internal/github/githubtest`).
- **Webhooks anyone can cause.** On a public repository anyone may open an issue or a pull request, or comment: such events re-read
  that repository at most once a minute, so outsiders cannot spend the portal's GitHub rate limit (`internal/sync` `Settle`).
- **Agents.** Every tool agents can call only reads. `/mcp` takes a bearer token or the authenticating proxy's identity, never a
  browser's session cookie, and refuses a request whose `Origin` is another site (the DNS rebinding guard MCP asks for); a tool call
  carrying a cookie must come from the portal's own pages. The pages' WebMCP tools are for the page's own origin only (the
  draft's default for the `tools` feature is `'self'`: a page of another site framing the portal cannot reach them) and include no
  administrator's action.
- **Webhooks** are accepted only with a valid HMAC-SHA256 signature (current or previous secret).
- **Addresses in repositories are not trusted.** An external image in a README and a package registry a build file names are
  reached only over https at public addresses, checked on the address dialled, so neither a redirect nor a DNS answer reaches your
  network. What a registry answers is cleaned like repository text, and its addresses are linked only when https.
- **Registry credentials.** A configured package registry's credentials are sent only to URLs under its own `url`, never to a
  registry a build file names and never across a redirect that leaves it (`internal/packages/private_test.go`). A private
  repository's package names are asked only of those registries, never of a public one.

## What you provide

- **One instance per audience.** The portal knows one boundary: private or not. Run one instance for each group of people who may see
  the same repositories, with its own GitHub App installed on **only those repositories** (*Selected repositories*), with read-only
  permissions and nothing else: *Contents* and *Metadata* (the catalogue), *Issues* (the open issues), *Pull requests*, *Checks* and
  *Commit statuses* (the open pull requests); leave out the ones whose feature you do not want (operator guide, section 1). The
  installation is the boundary GitHub enforces; `exclude` is a second fence. Whoever holds the App's private key can read everything
  those permissions cover in every installed repository, issue and pull request discussions included, though the portal itself keeps
  only their titles, labels and logins.
- **Secrets from files.** The App key, webhook secret, session key, token secrets, registry credentials and LLM key are read from files (a secrets manager's
  mount). Rotate the webhook secret with `webhook.previous_secret_file`. Rotate the App key, session key and tokens by replacing the
  file and restarting; a new session key signs everyone out.
- **The network layout.** Clients reach the public listener only through your TLS proxy. If a proxy authenticates viewers, it must
  remove `X-Forwarded-User` and `X-Forwarded-Groups` from client requests, and `server.trusted_proxies` must name only that proxy's
  address or network (`serve` warns about ranges wider than /24 or /64). The admin listener (`:9091`: metrics, status, profiling)
  stays on an internal network. Rate limits per client belong in the proxy.
- **The cache at rest.** `store.path` holds private READMEs, documents, commit subjects and dependencies. Put it on an encrypted
  volume, or on tmpfs if you prefer a cold start after every restart to data at rest. Leave it out of backups: it is rebuilt from
  GitHub.
- **The logs.** Ship the `audit` lines to where your access records go, and keep the request log (which carries paths) on systems
  whose readers may see repository names.
- **Assistant.** If you turn on the assistant, the provider you configure receives the questions and the repository text the
  assistant reads for a viewer. Choose a provider your confidentiality terms allow, or run a local one (`ollama`).

## What the portal does not do

- Visibility per group, per GitHub team or per person within one instance: everyone who sees private repositories sees all of them.
- Access rules per document or per path inside a repository.
- Mutual TLS between the proxy and the portal, or rate limiting of its own.
- Writing to GitHub: the portal reads, and every change stays on GitHub.

## The personal edition

[`scp personal`](personal-edition.md) runs the same portal for one person, with the same guarantees (a request without that person's
session sees nothing; the public view never knew the private repositories), and adds a credential held by the portal itself:

- The token is sealed in the store (AES-256-GCM, with the credential's id, source and kind as additional data, so a copied value does
  not open elsewhere). The key is not in the store: it is in the operating system's keychain, else in a file readable by the owner
  only. The token is never written to a log, the status page, a snapshot or an API answer.
- The portal serves loopback addresses only (it refuses a configuration that does not) and refuses every request without a session.
  The owner signs in with a link whose code is random, valid ten minutes and for one use, and gets a sealed session cookie.
- A token held on a computer can be used by whoever controls that computer's files and keychain. A fine-grained token or a GitHub
  App with read-only permissions limits what that is; a classic OAuth token with the scope `repo` can also write to GitHub, which SCP
  never does itself (the fake GitHub of the tests fails on any write).
- It does not refresh a token that ends: it says so and asks the person to sign in again.
- The first-run page is served on a loopback address only, by the same process, for as long as the setup lasts. Its link carries a
  random code that works once and gives the browser a `SameSite=Strict`, `HttpOnly` cookie; a request with another `Host` header
  (DNS rebinding) or from another origin is refused, bodies are JSON and capped, and its script and style come from the page's own
  address under a strict content security policy. The token a person pastes, receives from GitHub or takes from the GitHub CLI (`gh auth token`, run by the portal on that person's click) stays in the process: the page
  is never sent it back, and it is sealed, not logged, when the setup ends.
- The optional GitHub App of an account is made by GitHub's manifest flow in the person's own browser: the manifest asks for read
  permissions only (taken from the one table of what each feature needs), the page may post it only to the host the person signed
  in to (`form-action`), and GitHub's return carries a random state that works once (the cookie does not travel on a redirect from
  another site, so the state is the proof). The one-hour code is exchanged by a single call that creates nothing and writes to no
  repository; it hands over the App's id and private key, which stay in the process, are never sent to the page, and are sealed in
  the vault like the token (kind `app-key`, with `scp serve` refusing `app.credential`, as it refuses a vault token). The App can
  only read; it is private to the account that made it and can be deleted on GitHub at any time.
