# Feature modules (M12)
Status: in progress. Binding order: ../plan.md > this note.
From the backend and frontend package-by-feature reviews of 2026-10-15. Goal: a new feature is its own Go package or web folder plus
one wiring line; no core package or other feature changes.

## Go

1. **Facets.** A feature's data on a repository entry is a facet: `snapshot.Facet[T]` (`NewFacet[T](name)`, `Get(e) (T, Freshness)`,
   `Set(e, v, fresh)`, used inside `Builder.UpdateRepo`, which copies the entry; `Set` copies the facet map). `RepoEntry` keeps `Repo`
   and `Fresh`; `Facets(e)` yields name and freshness for metrics. The value types live in their feature: `commits.Commit`,
   `docs.Doc` (with `Image`, `Heading`), `badges.Release`, `manifests.Manifest` (with `Toolchain`, `Dependency`), `catalog.Descriptor`
   (with `Link`). `snapshot.Problem` stays shared (a finding in a repository file).
2. **Indexes.** Derived data over all repositories is an index the feature registers: `snapshot.Index[T]` (`NewIndex(name, build)`,
   `Get(s)`), passed to `NewPublisher`; the builder runs every index at publish, the public view runs them over its own repositories.
   Commits registers the feeds (`Latest`, per group); manifests registers `Providers` and `UsedBy`.
3. **Consumers declare.** A feature that shows another's data takes a function in its options, wired in `cmd/scp`:
   catalog's repository summary takes `Enrich []func(*snapshot.RepoEntry, *oapi.Repo)` (commits sets `lastCommitAt`, badges
   `release`, manifests `language`); badges takes `LastCommit`; llms takes the README and documents, the descriptor and the technology
   section as functions. The technology summary has one implementation, in manifests.
4. **GitHub.** `github.Source` keeps auth, the ETag and rate transport, listings, trees and blobs (shared by catalog, docs and
   manifests) and exposes `REST(ctx, owner) (*gh.Client, error)` plus `Classify(err)`; feature-only calls live in the feature
   (commits: list commits; badges: latest release).
5. **Events.** `sync.Event` is a string: `push` (a branch push or a changed `pushed_at`), `tag`, `release` and the engine's
   `SourceChanged`. The webhook maps a delivery with a repository to the GitHub event name (a push splits into `push` and `tag`);
   specs subscribe by name; the cluster sends the string.
6. **Boundary.** `cmd/scp/boundary_test.go` lists the core packages; every other `internal/*` package is a feature.
7. **Contract.** One contract, `api/openapi.yaml`, generated into `internal/httpapi/oapi`: it is the published API and features share
   its schemas. Feature-specific conversion (`CommitOf`) lives in the feature.
8. **Publisher.** Builds run one at a time; a panicking update is dropped alone; the public view's version is the sum of its own items'
   digests, so a private change does not move it.
9. Small: one ETag matcher (`httpapi.Matches`); the logger is passed, never set as the slog default.

## Web

1. **Route modules.** Each page of a feature is a route module in the feature, `route.ts`: `load(api, params)` (fills the query cache,
   throws not-found or redirects) and `metadata(api, params)`. `lib/page.tsx` turns a module and its view into a page
   (`default` and `generateMetadata`), so `app/**/page.tsx` is three lines.
2. **Shared resources.** Queries for resources several features read (repository, repositories, groups, documents, commits) and
   their keys live in `lib/api/queries.ts`; a feature's `api.ts` holds only its own. `loadRepo` (unknown is 404, a renamed repository
   308 to its address plus the sub-path) is in `lib/load.ts` and every repository sub-route uses it; canonicals come from the loaded
   repository.
3. **Panels.** The repository page renders panels from one composition list, `app/repos/[owner]/[name]/panels.ts`
   (`{ id, column, prefetch(api, owner, name), Panel }`); the manifests panel (technology, dependencies, uses, used by) is
   `features/manifests`. `app/` is the only place that composes features.
4. **Layers.** `components/ui` and `lib` import nothing above them; `entities/` (repository health and visibility, the commit list)
   imports `lib` and `components`; `shared/` is generic UI; a feature imports itself, `entities`, `shared`, `lib` and `components`.
   ESLint enforces it, including relative escapes and dynamic imports.
5. **One clock.** The server passes its instant to `NowProvider`; hydration renders against it, then the ticker takes over.
6. **Filters in the URL** are written with `history.replaceState` (no server round trip); the inputs read the URL.
7. **Dashboard cost.** Group members built in one pass; activity computed once per tick in the view and passed down.
8. Hook components carry `"use client"`; server modules import `server-only`; `global-error.tsx`; a failed panel offers Retry.
9. Tests: route modules against MSW (not-found, redirect, metadata), health thresholds and `pulse`, path encoding, filter parsing.

## Done when
`boundary_test.go` and the ESLint layers pass with a sample feature added in a test tree only; `make check` green; the Playwright
suite unchanged except the added redirect check.
