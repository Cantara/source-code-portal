# Hardened private instance (H1)
Status: in progress. Binding order: ../plan.md > this note. Asked for in issue #5.

## Scope
A portal whose repositories are nearly all private, where a leak is a confidentiality incident. One instance serves one audience:
its GitHub App is installed on the repositories that audience may see, and `access.private_groups` names the audience.

## Decisions
- One instance per audience; the App installation is the boundary. The snapshot keeps two views (full, public); no per-group views.
- Every public surface is held to one test (`cmd/scp/leak_test.go`): every GET of the API contract, read from `api/openapi.yaml`,
  and every route outside it, fetched as the public (HTML and Markdown) must carry no trace of a private repository (name,
  description, topics, README, documents, commit subjects, dependency edges), and every address naming it must answer word for word
  as an address that does not exist. A new route goes on its list.
- `access.require_auth: true` closes the public view: the public listener's middleware refuses a request with no identity (401 with
  `WWW-Authenticate`; a browser's page request, `Accept: text/html`, gets 303 to `/signin?return_to=`). Open: `/auth/`, `/signin`,
  `/_next/static/`, the favicon and theme script, `/api/v1/me` (the sign-in methods), `/webhooks/` (signed by GitHub) and `robots.txt`,
  which then disallows everything. The web UI server's own API calls carry the page's viewer and pass. `config check` wants a way to
  know the viewer (`TestRequireAuth`).
- `github.sources[].exclude` keeps repositories out of the catalogue even where the credential reads them: the groups' matcher kinds
  (`exact`, `prefix`, `glob`, `regexp`, `topic`; no `catch_all`), applied in `admit` before anything is stored, so a tracked
  repository that becomes excluded leaves at the next listing (`catalog.SourceFilter`, also used by `config groups`).
- The audit trail: every request on the public listener from a viewer of the private view leaves a log line `audit` (user, via,
  method, pattern, path, status, request id; never the answer), written by the middleware after the request line; static assets
  (`/_next/`) and the web UI server's own API calls (the page request is the record) are left out (`TestAudit`). Metrics and
  `/api/v1/status` carry no repository names: patterns, counts and source ids only.
- `serve` warns at start when `include_private` is on, the public listener is not loopback and `trusted_proxies` holds a range wider
  than /24 (IPv4) or /64 (IPv6).
- A verified `repository` `deleted` delivery removes the repository at once; otherwise two complete listings must miss it.
- `docs/security-model.md` states the guarantees, what the operator provides (an encrypted or tmpfs volume for `store.path`, the
  network layout, the App's permissions, key rotation) and what is out of scope.

## Order
Leak test (built) -> `require_auth` (built) -> `exclude` (built) -> audit log (built) -> start warning -> removal on `deleted` -> `security-model.md`.
