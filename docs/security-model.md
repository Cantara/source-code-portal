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
  `/knowledge.yaml`, the sitemap, the event stream, the assistant and the old Java-portal addresses all read the viewer's view. A test
  (`cmd/scp/leak_test.go`) fetches every GET of the API contract and every other public route as the public, and fails if any answer
  carries a trace of a private repository; the assistant has its own (`cmd/scp/assistant_test.go`). It also fails if an address naming a private repository answers differently from one that does not
  exist: the public cannot tell that a private repository exists.
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
- **Webhooks** are accepted only with a valid HMAC-SHA256 signature (current or previous secret).

## What you provide

- **One instance per audience.** The portal knows one boundary: private or not. Run one instance for each group of people who may see
  the same repositories, with its own GitHub App installed on **only those repositories** (*Selected repositories*), with *Contents*
  and *Metadata* read-only and nothing else. The installation is the boundary GitHub enforces; `exclude` is a second fence.
- **Secrets from files.** The App key, webhook secret, session key, token secrets and LLM key are read from files (a secrets manager's
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
