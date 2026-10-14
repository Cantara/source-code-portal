# ADR-023: Repository identity, sources, slugs, visibility and optional access

Status: Accepted. Date: 2026-10-14. Refines 04 D-13, D-17, Q-03, Q-08, Q-13 (A-13).

## Context
D-13 (revised by D-17) makes the catalogue model sources (App installations, PATs, GHES hosts) with identity `source/org/repo`; routes may omit the org when exactly one is configured; Q-03 makes v1 public-only unless an auth mode is on. D-12 uses single-segment `{repo}` routes.

## Decision
- `RepoRef{Source, ID, Owner, Name}` is the identity; `RepoID` (GitHub numeric id) survives rename and transfer; the portal-visible `Slug` is one URL segment, defaulting to the repository name, unique across the catalogue; collisions require `slug_prefix` per source and duplicates are a startup error. Renames add an alias so the old slug redirects.
- Default branch is resolved from the repository unless a group or repository sets `ref` (Q-08); the resolved ref is stored with each item.
- Matchers are typed (exact, prefix, anchored glob, anchored RE2 regexp, topic, catch-all); a repository may belong to several groups; `scp config lint` reports multi-group repos and unmatched patterns.
- Visibility policy `public_only` (default) or `installation_scope`; the latter (and any private/internal repository) requires `access.mode: trusted-proxy`, otherwise startup refuses. `access` honours identity headers only from trusted peers, strips them otherwise, and implements `httpapi.Authorizer`; every slug resolution goes through `httpapi.LookupRepo`; invisible repositories answer 404; an anonymous full-route enumeration test guards leaks.
- GHES and GHEC-DR require an explicit `access_acknowledgement` for `installation_scope`. OIDC is a documented later extension.

## Alternatives considered
Using `org/repo` in URLs (two segments, conflicts with the single-segment route shape), numeric ids in URLs (opaque), implicit private visibility (GR-35).

## Consequences
Single-org deployments see plain `/repos/{name}` URLs; multi-source deployments must name prefixes. Visibility becomes a tested security control, not a UI filter.

## Evidence
`05-research/09-github-app-integration.md` sections 2, 4, 8.3; `05-research/06-web-ui-badges-security.md` section 10.

## Amendment 2026-10-14 (review revision)

Renamed repositories: `catalog` publishes `catalog.aliases` (old slug -> current slug) and `overview` answers `GET /repos/{repo}` and `GET /repos/{repo}/{rest...}` for an alias with 308 to the current slug (m1). Reserved slugs: `ext`, `groups`, `avatars`. **Visibility classes** (M2): every global projection that summarises many repositories (`commits.latest`, `manifests.graph`, `site.meta`, group badge counts, `llms-full.txt`, `dependencies.md`) is computed per `scm.ViewClass` and selected with `authz.ClassOf`; the anonymous enumeration test compares counts, not only names.
