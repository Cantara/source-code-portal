# Landing page and layouts
Status: L1 built, L2 planned. Binding order: ../plan.md > this note.

## Scope
The landing page exists so an engineer sees which products need a look and why. Today it lists every repository with every attribute:
on a portal of 134 repositories it is 11 screens long, a catch-all group repeats every repository, and about 1,270 chips mostly repeat
a default ("no release", the common licence, the main language). The few signals that matter are lost.

L1 rebuilds the landing page from a small catalogue of page elements that carry signals, not attributes. L2 makes the page's
composition data: an admin arranges the elements in a design mode and the portal keeps the result in a layout store, the file system
first.

## Signals, not attributes
The rule every element keeps, and the test for any new one:

1. **Show the exception, not the default.** A value most repositories share is said once in the product's summary; a chip appears only
   where it differs: no licence, a release with unreleased changes, more than 20 open issues, failing to read.
2. **People, not automation.** Activity counts commits by people. Automation (`commits.Automated`) is counted beside it, in grey, never
   added to it; a product kept alive only by dependency updates reads as quiet.
3. **A sweep is not activity.** A commit whose subject lands in more than 10 repositories on the same day (UTC) is one change to the
   fleet, not activity in each repository: it counts neither as people nor for the activity levels.
4. **Rank by what changes.** While every product scores good, the score does not tell products apart: the card leads with people's
   work and the findings, the score and level stay, smaller. A product at watch or attention moves its level to the top.
5. **One clock per sentence.** A line that names a time names whose: "last change by a person 3 weeks ago", never a person count beside
   a bot's commit time.
6. **The landing page answers "which product, and why"; the group page lists members.** No element on the landing page lists every
   repository of a product.

## Data (L1)
Added to `DashboardGroup`, computed per request from the viewer's view in the commits hook (`commits.Dashboard`):

- `activity`: commits by people and by automation in the last 30 days, 12 weekly counts of commits by people, the people (author
  names, at most 5, then a count), and the 3 repositories people changed last.
- `exceptions`: the members whose chips are exceptions (rule 1), with the exception codes.
- `summary`: the defaults said once: the common licence, language and release state when at least 80% of the members share them.

Sweeps are found once per snapshot by the commits index (`Feeds()`): subjects grouped by day over the default branches. The health
score's activity signal (`lastAuthoredAt`) skips sweeps the same way, so the strip, the card and the score agree.

`unclaimed: true` (issue #8) is a matcher that selects the repositories no other group selects, evaluated after the others; at most
one group uses it. `catch_all` keeps its meaning. The dashboard's ungrouped count stays.

Latest commits are by people by default, automation behind a toggle (`/commits?automation=false` already exists).

## Elements (L1)
The landing page is a list of elements, each a component of `web/src/features/dashboard` with typed options:

| Element | Shows | Options |
|---|---|---|
| `fleet-map` | every counted repository as one tile, grouped by product, coloured by its activity level (people only); tooltips and a text summary | groups |
| `products` | the products as cards or as a table, the reader's switch kept in the cookie `scp-view-products` (as the page size is, P-26). A card has the same five rows on every product: name and level (the score small while good, the level chip when not), people's work (30-day counts and the 12-week sparkline) against automation's, who and the last change by a person, the 3 repositories people changed last, and the 3 costliest findings with what the members share said once. The table has a row per product, sortable by any column | groups, view |
| `latest-commits` | the newest commits by people; "include automation" shows all of them | count |
| `telemetry` | the fleet tiles (repositories, products, people this month, findings, freshness) | tiles |
| `heading` | a plain-text heading and line of text, no HTML | text |

`groups` selects products: all, or a list of group ids, in that order. Elements select groups, never repositories, so a layout never
names a repository. The default landing page is a `telemetry` row, the `fleet-map` band, `products` as cards, then `latest-commits`.
Per-repository rows leave the landing page for the group page. The telemetry tiles: people (who committed in 30 days), active
repositories, automation (and sweeps), findings, products (and the repositories in no group), in sync.

## Layouts (L2)
A layout is a page's elements and their options, as data:

```json
{"page": "dashboard", "elements": [
  {"kind": "telemetry", "tiles": ["repos", "people", "findings"]},
  {"kind": "fleet-map", "groups": "all"},
  {"kind": "products", "groups": ["core", "tools"], "view": "cards"}
]}
```

- **Validated on both sides**: Go checks a layout against the element catalogue (`internal/layout`, unknown kinds and fields refused,
  bounded size and element count) before it is saved or served; the web UI renders only known kinds. No HTML, no scripts, no URLs.
- **Store**: `layout.Store` (`Load(page)`, `Save(page, layout, revision)`, `List()`). The file system is the one store: `layout.dir`,
  one `<page>.json` per page, written to a temporary file and renamed. The revision is the content's SHA-256; a save with a stale
  revision is refused (412), so two admins never overwrite each other. Without `layout.dir`, or without a page's file, the built-in
  default renders and design mode is off; deleting the file resets the page.
- **Not a cache, not a user store**: layouts are the site's, edited by admins; they never go into SQLite (rule 5) and nothing is kept
  per person (P-27).
- **Reload**: each instance stats the files every 5 s and reloads a changed one, so a shared volume serves several instances and an
  operator can also write the files by hand or from configuration management. A file that fails validation is logged, shown on the
  status page and in design mode, and the last good layout (else the default) keeps serving.
- **API**: `GET /api/v1/pages/{page}` (contract) answers the page's elements resolved against the viewer's view: groups the viewer
  cannot see are dropped, so the leak test covers it. `GET` and `PUT /api/v1/admin/layouts/{page}` (admin, same origin) read and write
  the stored layout with its revision.
- **Design mode**: an admin's toggle on the page (the viewer menu). Elements can be added from the catalogue, moved, removed and set up
  in a side panel with a live preview; Save writes the layout, Discard reloads it. Keyboard moves as well as dragging (axe gate).
- **Pages**: `dashboard` first. The format names its page, so further pages (a product's page, named dashboards) come without a
  format change.

## Not now
Layouts per person or per sign-in group, repository-level selection in elements, other stores (a Git repository, S3), and sharing layouts
between instances through Hazelcast. Each needs its own decision against P-27 and P-35.
