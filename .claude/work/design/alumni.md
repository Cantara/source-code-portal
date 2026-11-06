# Alumni: the retired repositories (AL1, AL2)

Status: AL1 and AL2 built (plan.md section 4, P-81) (plan.md section 4, P-81). Binding order: ../plan.md > this note > `catalog.md`, `health.md`, `layout.md`.

A catalogue only grows. A repository that was archived, or that its owners marked deprecated, is no longer work in progress, but it
is still a record: what it was, who built it, when it last moved, what it last released, and what still depends on it. The portal
keeps these apart from the live products under one name, **Alumni**, and says for each why it is there.

## The choice

**A repository is an alumnus when GitHub archived it or its `scp.yaml` says `lifecycle: deprecated`. Nothing else: no age rule (a
quiet repository is `dormant`, `health.md`, not retired) and no new key.** The portal already reads both facts, so the feature adds
no job, no GitHub call and no permission.

- **Reason.** `archived` wins over `deprecated` (the stronger fact); a page shows one word per row: *Archived* or *Deprecated*.
- **Retired since.** GitHub does not date an archive. The row says *last active* (the newest default-branch commit the portal
  holds, else `pushed_at`), which is the honest age of a retired repository. No date of retirement is invented.
- **What a row holds.** The repository summary, the reason, last active, the latest release (from the `releases` facet when it
  exists, RL1; absent until then), the number of repositories that still depend on it (`used by`, from the manifests' dependency
  graph) and the authors of its last 100 default-branch commits, **counted, not named** (`3 authors`), as the dashboard counts
  people (L1): the portal ranks no person.
- **Still used.** An alumnus that live repositories still depend on is the useful exception: the row says `used by N` and the page
  can filter to them (`?used=1`). It is a finding for the owners, not a health penalty (retired repositories are not scored).
- **Out of the live views.** Alumni stay out of the dashboard's product cards and health as today; the fleet map keeps showing them
  as `archived`. The group page and the repositories list keep their rows with the *Archived* badge; a group says `N retired` with a
  link to `/alumni?group=`.

## As built

The selection and the API live in `internal/catalog/alumni.go` (`Retired`, `API.ListAlumni`): the catalogue already owns `archived`,
the descriptor's `lifecycle` and the repository summary, so Alumni is a view of it, not a package. What the other features know is
added by hooks on `catalog.API.Alumnus`, wired in `cmd/scp` as the repository hooks are: `manifests.Alumnus` (`usedBy`, archived
dependants not counted) and `commits.Alumnus` (`lastActive` from the newest commit; `authors`, the distinct people among the newest
100 default-branch commits, automation left out). The page is `web/src/features/alumni`, the header's *Alumni* link is always shown,
and the reader's page size has its row in Settings.

## API

`GET /api/v1/alumni` (contract): `group`, `q` (name contains), `reason` (`archived` | `deprecated`), `used` (`1`: only those
something depends on), `sort` (`active` newest first, the default; `name`; `group`; `used`), numbered pages as `/pulls`. The answer
is the page of rows, the totals per reason, and the freshness of the scope. Computed per request from the viewer's view
(`snapshot.Current`), so a private alumnus is seen by a private viewer only; the public view is the twin of a portal that never
knew it (P-76, the leak test lists the route).

## Pages

`/alumni` in the header (after Pulls and Issues): a list, a GET form without JavaScript (search, reason chips, *still used*, sort),
numbered pages, each row a link to its repository page. Server-rendered, a canonical address, in the sitemap, JSON-LD breadcrumb.
Empty: one sentence ("No repository has been retired."), not an error. A repository page of an alumnus shows a notice at its top
saying why it is retired and since when it was last active.

## Agents and the rest (AL2)

The tool `retired_repositories` (the catalogue of `cmd/scp/tools.go`: same parameters as the API), the search kind `alumni`
filter is not needed (a retired repository is already found by name; its row says *Archived*), `llms.txt` marks a retired repository with
its reason (`archived`; `deprecated` is its lifecycle already) and links the JSON list, the badges are unchanged. Docs, the Guide's section on the Alumni, the plugin's reference.

## Tests

The catalogue filter: archived, deprecated and both, a private alumnus hidden from the public (twin test), the sorts, the used-by
count against the fixture dependency graph, authors counted not named, an empty portal. Playwright: the page with and without
JavaScript, axe in both themes.
