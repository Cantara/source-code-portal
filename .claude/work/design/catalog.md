# Catalog
Status: as built. Binding order: ../plan.md > this note.

## Scope
`internal/catalog` discovers the repositories of the configured organisations, gives each a stable identity and its `{owner}/{name}`
address, assigns them to config-defined groups, owns the `scp.yaml` descriptor (fetch and parse), and contributes repositories, groups and
aliases to the snapshot. It owns the removal rule; the listing every `catalog.reconcile_interval` is also the `pushed_at` freshness sweep.

## Decisions
- Identity is `snapshot.RepoID{Source, GitHubID}`: the numeric id survives rename and transfer.
- The address is `lower(owner/name)` (`snapshot.RepoKey`), the same two segments in every URL; when two listed repositories share an
  address, the lower id keeps it.
- Renames and transfers keep the old address as an alias (at most 8 per repository) answered with 308: links in chat and docs keep working.
- Listing is `GET /orgs/{owner}/repos?type=all&sort=pushed&direction=desc` for App and token auth alike; each item's `visibility` is checked
  fail closed: `public` is admitted, `private` and `internal` only with `include_private` (access.md); stored rows are checked again at publish.
- Admission filters per source (`github.sources[].include_forks` false, `include_archived` true): forks are usually noise, archived systems
  are still part of the map.
- Matchers are typed and always match the whole name: a pattern means what it reads.
- Membership is many to many; group display order is config order: operators control the dashboard.
- A `default_repo` not among the members degrades the group card (`defaultRepoState: missing`) and the group stays visible.
- Removal rule (the one statement; github.md refers here): a repository is removed after two consecutive complete listings miss it; an
  empty listing for a source that had repositories fails the run; a listed repository that is no longer admitted is removed at once.
- The descriptor is catalogue metadata read by pages, `llms` and the API: `catalog` fetches it in its own job and parses it bounded and
  field by field; it affects display only, membership and access come from config.
- Groups config is validated completely at startup (and by `scp config check`); any error refuses start.

## Shapes
```go
// internal/snapshot (core): shared value types; catalog builds them.
type Repo struct {
    ID RepoID; Owner, Name string                    // Key() RepoKey = lower(owner/name)
    Description, HTMLURL, Homepage, DefaultBranch, License, Visibility string
    Topics []string; Archived, Fork bool; Stars, OpenIssues int; PushedAt time.Time
}
type Group struct{ ID, Name, Description string; DefaultRepo RepoKey; DefaultMissing bool; Members []RepoKey } // members sorted
type Descriptor struct {
    Present bool; File string                        // ".github/scp.yaml" etc.
    Title, Description, Lifecycle string             // Lifecycle: "" | experimental | production | deprecated
    Owners, Tags []string; Links []Link; Problems []Problem
}
type Link struct{ Title, URL string }
type Problem struct{ Pointer, Code, Message string }

// internal/catalog
type Tracked struct{ Repo snapshot.Repo; Aliases []snapshot.RepoKey; Misses int } // the listing item's payload
type Filter struct{ IncludeForks, IncludeArchived, IncludePrivate bool }
type Finding struct{ Error bool; Message string }
func Compile(groups []config.Group) (*Groups, []Finding)                  // nil Groups when any finding is an error
func (gs *Groups) Resolve(repos map[snapshot.RepoKey]*snapshot.RepoEntry) []snapshot.Group
func Plan(listed []snapshot.Repo, current []Tracked, f Filter) []Tracked   // pure, order independent, sorted by address
func ParseDescriptor(b []byte) snapshot.Descriptor                         // pure, bounded, total
```

## Behaviour
1. Reconcile (sync job `catalog.reconcile/<source>`, TTL `catalog.reconcile_interval`, and on `EventSourceChanged`): refresh the App
   installations, then list every org of the source, all pages, with ETags; any page failure, or more than 100 pages, fails the run and
   the last good catalogue stays published.
2. Normalise each item: zero id, owner/name outside `[A-Za-z0-9._-]{1,100}`, `.`/`..` are invalid; the description is cleaned with
   `untrusted.Text` (1,000 runes), at most 20 topics matching `^[a-z0-9][a-z0-9-]{0,49}$` are kept, `html_url` and `homepage` only as
   `https` URLs without credentials.
3. Admit: invalid, not visible (decision above), archived and not included, fork and not included are left out.
4. Tracked repositories of the source: listed and admitted -> `Misses = 0`, facts updated, and when `pushed_at` changed
   `Notify(EventPush, key)` (commits, docs, descriptor, manifests jobs); listed but not admitted -> removed at once; absent -> `Misses++`,
   removed at 2.
5. A complete listing with zero repositories for a source that tracked some fails the run (token or scope problem).
6. Addresses: a repository whose owner/name changed (same id) pushes the old address onto `Aliases` (newest first, max 8, never the
   current address nor another listed repository's). Aliases of removed repositories go with them, so a deleted repository is a real 404.
7. Membership: a repository is in a group when the group's `sources` (empty = all) contain its source and any matcher selects it.
   Matchers see the repository name; `topic` sees topics. Repositories in no group are counted (`ungroupedCount`) and shown in their own
   dashboard section.
8. `default_repo` is a GitHub repository name, matched case-insensitively among the group's members (the lowest address when several);
   unresolved sets `DefaultMissing`.
9. Matchers: `exact`, `prefix` (case-insensitive); `glob` with `*` and `?`, whole name, case-insensitive, compiled to RE2, at most
   6 `*`; `regexp` RE2 wrapped as `^(?:p)$` after parsing `p` alone (an unbalanced `)` cannot close the wrapper), case-sensitive unless
   `(?i)`, pattern at most 256 bytes and program at most 1,000 instructions; `topic` by the topic grammar above; `catch_all: true`.
10. Commit: the job returns `Result.Catalogue` (the source's repositories) and `sync` writes and publishes it (sync-snapshot-store.md 6);
    the catalog `Publish` replaces the source's repositories and aliases and resolves the groups. The snapshot is `Catalogued` once every
    source has a listing. The warm start publishes the stored rows (checked again for visibility) before the first listing.
11. Descriptor job `catalog.descriptor/<owner>/<name>`: runs when a repository is added, on `EventPush`, and every 24 h; reads the `HEAD`
    tree and takes `.github/scp.yaml|yml`, else `scp.yaml|yml`; the blob is fetched when its SHA changed and is at most 64 KiB (larger is
    state `too_large`). `.github/` wins; both extensions in the winning location is problem `descriptor.ambiguous` and no descriptor. A
    fetch error keeps the last value.
12. Descriptor parsing: whole-document rejection (one problem, no value) for more than 64 KiB, not UTF-8, YAML syntax error, more than one
    document, any anchor, alias or merge key, nesting over 32, non-mapping top level, `schema` missing or not `1`. Then field by field:
    `title` 1..120 chars, `description` 1..500, `lifecycle` enum, `owners` at most 20 of `@user` or `@org/team`, `tags` at most 20 unique
    `^[a-z0-9][a-z0-9-]{0,62}$`, `links` at most 20 of `{title 1..80, url https only}`; an invalid field or item is dropped with a
    `field.invalid` problem, valid neighbours are kept; unknown keys are a `field.unknown` problem (key cut at 40 characters) and ignored.
13. Read-time defaults: title = descriptor title else repository name; description = descriptor else GitHub description. Owners are claims.
14. Snapshot contribution: repositories by address, groups in config order, alias map old address to current, per-source status
    (freshness of the listing, repository count).

## Config
`catalog.reconcile_interval` (5m, 1m..24h),
`catalog.groups[]` (`id` `^[a-z0-9]+(-[a-z0-9]+)*$` max 63, unique; `name` required max 120; `description` max 500; `default_repo`
required; `sources` (ids from `github.sources[]`); `match` 1..64 entries of exactly one of `exact`, `prefix`, `glob`, `regexp`, `topic`,
`catch_all: true`); at most 500 groups. Sources and admission filters are `github.sources[]` (github.md Config). Startup errors: a bad or
duplicate id, a matcher that does not compile, an unknown source, a `default_repo` that is not a legal name or not selected by its own
matchers (a warning when the group has a `topic` matcher). Warnings: more than one `catch_all`, a matcher that matches the empty name.
```yaml
catalog:
  groups:
    - id: whydah
      name: Whydah identity platform
      default_repo: Whydah-SSOLoginWebApp
      match: [ { prefix: Whydah- }, { topic: iam } ]
    - id: libraries
      name: Shared libraries
      default_repo: Cantara-Commons
      match: [ { exact: Cantara-Commons }, { glob: "*-lib" }, { regexp: '(Config|Messi)-.*' } ]
    - id: everything
      name: All repositories
      default_repo: source-code-portal
      match: [ { catch_all: true } ]
```

## Tests
- `match_test.go`: `TestMatchers` per kind (case rules, anchoring, caps), `TestMatcherRefused`, `TestCompileFindings`, `TestResolve`.
- `plan_test.go`: `TestPlan`, `TestPlanFilters`, `TestPlanCleans`, `TestPlanProperties` (random worlds with renames, deletions,
  visibility flips and duplicates in shuffled order: unique addresses, only admitted repositories, bounded aliases, removal after two
  misses, the same result for any order).
- `catalog_test.go`: `TestReconcilePublishes`, `TestReconcileListingErrorFails`, `TestPushedAtNotifies`.
- `descriptor_test.go`: `TestParseDescriptor` (anchors, size, two documents, `http` link, bad lifecycle, valid field beside an invalid
  one, unknown keys), `FuzzParseDescriptor`, `TestDescriptorJob`.
- `cmd/scp`: `TestConfigCheckRefusesBadGroups`; `TestServeEndToEnd` (discovery into the API, warm start, `pushed_at` push).
