# ADR-014: HTTP server, middleware and routing

Status: Accepted. Date: 2026-10-14. Amends 04 D-12 and keeps the stands of D-05 (A-12).

## Context
D-05 keeps `net/http` ServeMux, a middleware chain, a named route table and separate listeners; D-12 lists canonical routes. ServeMux conflicts and wildcard rules invalidate two D-12 patterns: `/badges/{repo}/{kind}.svg` panics at registration ("bad wildcard segment"), and a method-less `/webhooks/github` conflicts with `GET /{path...}`. D-16 adds SPA and API classes.

## Decision
- Two listeners from `httpx.NewServer` (the only `*http.Server` constructor, G4): public `:9090` and admin `:9091`; timeouts `ReadHeaderTimeout 5s, ReadTimeout 30s, WriteTimeout 60s, IdleTimeout 120s, MaxHeaderBytes 16 KiB`, SSE extends per response.
- Middleware (outermost first): request id, recover, access log, otelhttp, body limit, trusted-proxy client IP, security headers per class, keyed rate limit, `http.CrossOriginProtection` (bypass for the webhook and CSP-report routes), mux; under `/api/` also `ProblemMux` and request validation.
- `routes.Table` names every route with method, pattern, class (`API`, `SPA`, `Server`, `Admin`) and owner; handlers register by name; **the API class registers through the table's recording mux** (`Table.Mux`, which satisfies oapi-codegen's `ServeMux` = `HandleFunc` + `http.Handler`, verified on the v2.8.0 tag), so generated registrations cannot bypass the table; `Verify()` at startup rejects missing or extra handlers; ServeMux panics on ambiguous patterns, so conflicts are start-time failures.
- Three public route classes plus admin (architecture section 11.2). Corrections: badges `GET /badges/{repo}/{file}`; webhooks `POST /webhooks/github[/{source}]`; assets `/assets/<name>-<hash>.<ext>`; purls travel as query parameters; added routes for docs paths, dependencies, components, llms scopes, sitemap, robots, schemas, media.
- Path values are validated against typed ids (`PathValue` is unescaped). Go 1.26 changed **trailing-slash** redirects to 307 (verified at go.dev/doc/go1.26, 2026-10-14); the earlier claim about `//` path cleaning is removed because the release notes say nothing about it.
- Only canonical routes are served (Q-02); unknown paths are real 404s.
- Client IP: right-to-left through trusted proxies only; XFF ignored from untrusted peers.

## Alternatives considered
chi/echo/gin (no benefit over 1.22+ patterns), one listener with path-based admin protection (GR-17).

## Consequences
The route table and the SPA's typed routes are two sources kept equal by a test; negotiated human routes (`/repos/{repo}`, `/groups/{id}`) are registered explicitly with `httpapi.Negotiate` and delegate to the SPA shell.

## Evidence
`05-research/06-web-ui-badges-security.md` sections 3.3, 5, 10-14 (experiments E4, E11 verified the panics and conflicts); `05-research/04-go-architecture-patterns.md` section 10.

## Amendment 2026-10-14 (review revision)

Further routing decisions recorded here: group badge route `GET /badges/groups/{groupId}/{file}`; health is served only as `/livez`, `/readyz` and admin `/status` (Q-11); renamed-slug aliases are answered with 308 by `overview` (`GET /repos/{repo}` and `GET /repos/{repo}/{rest...}` consult `catalog.aliases` first); reserved slugs `ext`, `groups`, `avatars`; access log and metrics carry `r.Pattern`.
