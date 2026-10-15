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
- `access.require_auth: true` closes the public view: an unauthenticated request gets 401 (a page redirects to sign-in), except
  `/auth/*`, the sign-in page and its static assets; `robots.txt` disallows everything.
- `github.sources[].exclude` (exact names, globs, topics) keeps repositories out of the catalogue even where the credential reads them.
- One log line per answer that includes a private repository: viewer, repository, route, no content, on the logger `access`; a
  counter without repository labels. Metrics and `/api/v1/status` carry no repository names.
- `serve` warns at start when `include_private` is on, the public listener is not loopback and `trusted_proxies` holds a range wider
  than /24 (IPv4) or /64 (IPv6).
- A verified `repository` `deleted` delivery removes the repository at once; otherwise two complete listings must miss it.
- `docs/security-model.md` states the guarantees, what the operator provides (an encrypted or tmpfs volume for `store.path`, the
  network layout, the App's permissions, key rotation) and what is out of scope.

## Order
Leak test (built) -> `require_auth` -> `exclude` -> access log -> start warning -> removal on `deleted` -> `security-model.md`.
