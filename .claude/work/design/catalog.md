# Catalog
Status: v1 design note, 2026-10-14. Binding order: ../plan.md > this note > ../reference/.

## Scope (v1)
`internal/catalog` discovers the public repositories of the configured organisations, gives each a stable identity and its
`{owner}/{name}` address, assigns them to config-defined groups, owns the `scp.yaml` descriptor (fetch and parse), and contributes
repositories, groups and aliases to the snapshot. It owns the removal rule; the 5-minute listing is also the `pushed_at` freshness sweep.

## Decisions
- Identity is `snapshot.RepoID{Source, GitHubID}`: the numeric id survives rename and transfer.
- The address is `lower(owner/name)` (`snapshot.RepoKey`), the same two segments in every URL; each owner belongs to exactly one source
  (github.md validation), so addresses are unique.
- Renames and transfers keep the old address as an alias (at most 8 per repository) answered with 308: links in chat and docs keep working.
- Listing is `GET /orgs/{owner}/repos?type=all&sort=pushed&direction=desc` for App and token auth alike; each item's `visibility` is checked
  fail closed: only `public` is admitted.
- Admission filters per source (`github.sources[].include_forks` false, `include_archived` true): forks are usually noise, archived systems
  are still part of the map.
- Matchers are typed and always match the whole name (GR-09): a pattern means what it reads.
- Membership is many to many; group display order is config order: operators control the dashboard.
- A wrong `default_repo` at runtime degrades the group card (`defaultRepoState: missing`) and the group stays visible (GR-29).
- Removal rule (the one statement; github.md refers here): a repository is removed after two consecutive complete listings miss it; an
  empty listing for a source that had repositories counts as a failed run; a repository that turns private is removed at once.
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
    Groups []string                                  // group ids, config order
    Descriptor Descriptor
    Fresh Freshness
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
type MatcherKind string // exact | prefix | glob | regexp | topic | catch_all
type Matcher struct{ Kind MatcherKind; Value string }
type GroupConfig struct{ ID, Name, Description, DefaultRepo string; Sources []string; Match []Matcher }
type Tracked struct{ Repo snapshot.Repo; Aliases []snapshot.RepoKey; FirstSeen, SeenAt time.Time; Misses int } // store repo.meta
type notifier interface{ Notify(sync.Event, snapshot.RepoKey) error } // *sync.Engine, wired in cmd/scp
func Compile(groups []GroupConfig, sourceIDs []string) (*Groups, []Finding)               // pure; Finding{Path, Severity, Message}
func Plan(source string, listed []snapshot.Repo, current []Tracked, now time.Time) PlanResult // pure, order independent
func ParseDescriptor(b []byte) snapshot.Descriptor                                         // pure, bounded, total
```

## Behaviour
1. Reconcile (sync job `catalog.reconcile/<source>`, every `catalog.reconcile_interval`, on start and on `EventSourceChanged`): list
   every org of the source, all pages, with ETags; any page failure, or more than 100 pages (10,000 repositories), fails the run and the
   last good catalogue stays published.
2. Normalise each item: zero id, owner/name outside `[A-Za-z0-9._-]{1,100}`, `.`/`..` are invalid; strip control and bidi characters from
   the description (cut at 1,000 bytes), keep at most 20 topics of GitHub grammar, keep only `https` URLs without credentials.
3. Admit in order (first reason wins, counted per reason): invalid, visibility not `public`, archived and not included, fork and not included.
4. Tracked repositories of the source: listed and admitted -> `Misses = 0`, facts updated, and when `pushed_at` changed
   `Notify(EventPush, key)` (commits, docs, descriptor jobs); listed but not public -> removed at once; listed but excluded by the
   archived/fork filter -> removed; absent -> `Misses++`, removed at 2.
5. A complete listing with zero repositories for a source that tracked some counts as a failed run (token or scope problem).
6. Addresses: a repository whose owner/name changed (same `RepoID`) pushes the old address onto `Aliases` (newest first, max 8, never the
   current address). Aliases of removed repositories go with them, so a deleted repository is a real 404.
7. Membership: a repository is in a group when the group's `sources` (empty = all) contain its source and any matcher selects it.
   Matchers see the repository name; `topic` sees topics. Repositories in no group are listed (API and an "Other" dashboard section).
8. `default_repo` is a GitHub repository name, matched case-insensitively among the group's members; unresolved sets `DefaultMissing`.
9. Matchers: `exact`, `prefix` (case-insensitive); `glob` with `*` and `?`, whole name, case-insensitive, compiled to RE2, at most
   6 `*`; `regexp` RE2 wrapped as `^(?:p)$` after parsing `p` alone (an unbalanced `)` stays inside), case-sensitive unless `(?i)`,
   pattern at most 256 bytes and program at most 1,000 instructions; `topic` by GitHub topic grammar; `catch_all: true` (false is an error).
10. Commit: the job returns `Result.Catalogue` (the source's tracked rows) and `sync` writes and publishes it (sync-snapshot-store.md 6);
    the catalog `Publish` builds repositories, groups and aliases. Warm start publishes the stored rows (re-checked as public) before the
    first listing, which runs at once.
11. Events (github.md behaviour 10): `EventRepoChanged` runs `catalog.repo/<owner>/<name>` (`Repositories.Get`): not public or not found
    returns `Result.Remove` (removed at once), otherwise facts and groups are refreshed. `EventSourceChanged` runs the source reconcile;
    coalescing in `sync` folds a burst into one listing.
12. Descriptor job `catalog.descriptor/<owner>/<name>`: runs when a repository is added, on `EventPush`, and every
    `catalog.descriptor.refresh_interval`; reads the default-branch tree and takes `.github/scp.yaml|yml`, else `scp.yaml|yml`; the blob is
    fetched when its SHA changed. `.github/` wins across locations; both extensions in one location is problem `descriptor.ambiguous` and
    the descriptor is left empty. A fetch error keeps the last value.
13. Descriptor parsing: whole-document rejection (one problem, no value) for more than 64 KiB, YAML syntax error, more than one
    document, any anchor, alias or merge key, nesting over 32, non-mapping top level, `schema` missing or not `1`. Then field by field:
    `title` 1..120 chars, `description` 1..500, `lifecycle` enum, `owners` at most 20 of `@user` or `@org/team`, `tags` at most 20 unique
    `^[a-z0-9][a-z0-9-]{0,62}$`, `links` at most 20 of `{title 1..80, url https only}`; an invalid field or item is dropped with a
    problem, valid neighbours are kept; unknown keys are a warning problem and ignored. Messages echo at most 80 bytes of input.
14. Read-time defaults: title = descriptor title else repository name; description = descriptor else GitHub description. Owners are claims.
15. Snapshot contribution: repositories sorted by address, groups in config order, alias map old address to current, per-source status
    (last attempt, last success, state `never|warm|ok|failed`, counts).

## Config
`catalog.reconcile_interval` (5m, 1m..24h), `catalog.descriptor.refresh_interval` (24h, 1h..168h),
`catalog.groups[]` (`id` `^[a-z0-9]+(-[a-z0-9]+)*$` max 63, `name` required max 120, `description` max 500, `default_repo` required,
`sources` (ids from `github.sources[]`), `match` 1..64 one-key entries); at most 500 groups. Sources and admission filters are
`github.sources[]` (github.md Config). Matchers decode through a Viper decode hook that rejects zero or two keys, unknown kinds and bare
scalars. Startup errors: duplicate or bad id, matcher does not compile, unknown source in `sources`, `default_repo` not a legal name or not
selected by its own matchers (a warning when a `topic` matcher makes it undecidable). Warnings: two groups with identical matchers, more
than one `catch_all`, a regexp or glob matching the empty name.
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
- Table tests per matcher kind incl. case rules, anchoring (`IoT*` as glob vs regexp), caps; `FuzzMatcherCompile` and a test that the
  anchor wrapper is one concatenation for every pattern that parses.
- `Plan` property test over random worlds (renames, transfers, deletions, visibility flips, duplicate entries, shuffled order): no duplicate
  address, no non-public repository, aliases bounded and never current, removal only after two misses, result independent of listing order.
- `pushed_at` changes notify exactly the changed repositories; an unchanged sweep notifies none.
- Config validation table (every error and warning) and a golden output of `scp config check` for the example above.
- `FuzzParseDescriptor`; cases: billion-laughs anchors, 64 KiB + 1, two documents, both extensions, `http` link, bad lifecycle, valid
  field beside an invalid one, 60,000-character unknown key.
- Against `githubtest`: discovery end to end into `/api/v1/groups`, page failure keeps last good, privatized webhook removes at once,
  rename redirects 308, warm start from SQLite, empty listing counts as failed.

## Reference
`../reference/detailed-design/20-design/08-catalog.md` sections 3-4, 4.9 (lint codes) and 12; `20-design/10-docs-pages-media.md` 4.3
(descriptor loader); `11-adr/ADR-023-*`, `11-adr/ADR-006-*`; `04-decisions.md` Q-07, Q-08, A-13; `02-design-guardrails.md` GR-09,
GR-26, GR-29, GR-35. Prototype code: `20-design/08-catalog.assets/mod/internal/catalog/` (matchers, `PlanReconcile`).
