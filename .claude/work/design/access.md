# Private repositories
Status: v1 design note (M8), 2026-10-14. Binding order: ../plan.md > this note > ../reference/.

## Scope
- Private and internal repositories in the catalogue, per source (`github.sources[].include_private`).
- Viewers identified by a trusted authenticating proxy in front of the public listener; members of permitted groups see private
  repositories, everyone else sees the public ones only, on every surface (pages, `/api/v1`, badges, Markdown, `/llms.txt`, feeds).

## Decisions
- SCP does not authenticate. The proxy (oauth2-proxy, an ingress with OIDC, a corporate SSO gateway) authenticates and passes the user
  and groups in headers: `access.user_header` (default `X-Forwarded-User`) and `access.groups_header` (default `X-Forwarded-Groups`,
  comma-separated, names compared exactly). Headers are believed only from a peer in `server.trusted_proxies`, the same list that governs `X-Forwarded-For`;
  from any other peer both are removed before routing.
- `access.private_groups` lists the groups whose members see private repositories; `"*"` admits every authenticated user. A source with
  `include_private` needs a non-empty list (config check), so private data is never admitted without a way to restrict it.
- The snapshot carries two views built at publish: the full one and the public one (private and internal repositories removed, with
  their aliases, group memberships, feed entries and dependency links). Handlers get their view through `Current(ctx)`; the viewer
  decision is made once per request by the middleware and features never see headers.
- When no repository is private the public view is the full view (one pointer), so a public-only portal pays nothing.
- A group's default repository that is private is left out of the public view without a "missing" warning.
- Responses to a private viewer are `Cache-Control: private` (a shared cache never stores them) and every response carries
  `Vary` on the two headers while private repositories are admitted.
- `/llms.txt` and `/llms-full.txt` are built once per view and snapshot.
- Admin surfaces (`/metrics`, `/api/v1/status`) read the full view; they are on the admin listener.
- Relative images in private READMEs point at `raw.githubusercontent.com`, which needs a GitHub session; the image proxy (M10) serves
  them through the portal.
- The SPA shows a "Private" or "Internal" badge on repository rows and pages; nothing else changes in the UI.

## Shapes
```go
// internal/snapshot
func (s *Snapshot) Public() *Snapshot // the public view; s itself when nothing is private

// internal/config
type Access struct {
    UserHeader, GroupsHeader string   // defaults X-Forwarded-User, X-Forwarded-Groups
    PrivateGroups            []string // "*" = every authenticated user
}
// Source gains IncludePrivate bool (include_private)

// internal/httpapi
type Access struct{ UserHeader, GroupsHeader string; PrivateGroups []string; Vary bool }
func Private(ctx context.Context) bool // set by the middleware
// Options gains Access
```
Features take `Current func(context.Context) *snapshot.Snapshot`; `cmd/scp` builds it as the full view for a private viewer, else
`Public()`.

## Tests
- Snapshot views: removed entries, aliases to private targets, group members and default repository, feeds, dependency links,
  pointer identity when nothing is private.
- Middleware: headers from an untrusted peer are removed and ignored; group matching (`*`, exact names, spaces around commas); cache headers.
- End to end (`cmd/scp/run_test.go`): a private repository is absent from every public surface (API list and detail, badge 404, group
  count, dashboard, commits feed, `/llms.txt`) and present for a request from the trusted proxy with a permitted group.
