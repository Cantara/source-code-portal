# Landing page and layouts
Status: as built (L1 to L5). Binding order: ../plan.md > this note.

## Terms
The words the owner's instructions and these notes use for the pages built from layouts (owner, 2026-11-06). Internal: `docs/` keeps
the readers' words. A new part of a page gets its term here first.

| Term | What it is | In code |
|---|---|---|
| **Layout** | A page's ordered list of elements: the built-in one, or the one an admin stored | `/api/v1/pages/{page}`, `<layout.dir>/<page>.json` |
| **Element** | One block of a page, of a kind: `announcements`, `telemetry`, `fleet-map`, `products`, `pulls`, `issues`, `latest-commits`, `heading` | `LayoutElement`; `Elements` in `features/dashboard/elements.tsx` |
| **Design mode** | Where an admin adds, moves, changes and removes elements and saves the layout | `features/dashboard/design.tsx` |
| **Main column** | At the left on a wide screen: the `products` element. It grows with its content; the **page scrolls** | `main` of `SidePanel` |
| **Product card** | One product (a group) in the products' **cards** view; the other view is the **products table** | `ProductCard` in `products.tsx` |
| **Side panel** | At the right on a wide screen: the elements right after `products` (the latest commits, the pull requests, the issues), in order, each a section | `SidePanel` in `side.tsx` |
| **Section** | One element in the side panel: its header (title, count, its hide button) stays at its top, its list has an inner scroll; the open sections share the panel's height equally (three open: thirds) | `Section` in `side.tsx` |
| **Hide a section** | A section's button (the panel icon at its header's end) folds it to its header and shows it again; the open ones take its space. Each section's state is kept | cookie `scp-side-sections` |
| **Sticky** | The side panel stays in place while the page scrolls under it | `lg:sticky lg:top-20` |
| **Viewport-high** | The side panel is as tall as the window below the header, however long the main column is | `lg:max-h-[calc(100dvh-6rem)]` |
| **Inner scroll** | A part that scrolls on its own, apart from the page: the side panel when its content is taller than it | `lg:overflow-y-auto` |
| **Loads more at the end** | A list that reads its next page as its end nears the view of its scroll, 300 px before it (the latest commits, the pull requests, the issues; up to 10 pages, then the link to their page) | `latest.tsx`, `more.tsx` |
| **Fold**, **rail** | With every section hidden the side panel folds to a thin rail, whose button shows them all again | cookie `scp-side-commits` |
| **Resize edge** | The side panel's left edge, dragged (or moved with the arrow keys) to set its width, 18 to 40 rem | cookie `scp-side-commits` |
| **Stacked** | Below `lg`: no side panel, every element in one column in layout order | — |
| **Telemetry tiles** | The fleet's numbers in a row at the top of the dashboard, each with its hint | `telemetry.tsx` |
| **Fleet map** | Every repository as one tile by product, coloured by its last change or its health | `fleet-map.tsx` |

The presentation page (`present.md`) is one screen that never scrolls, with words of its own:

| Term | What it is |
|---|---|
| **Status strip** | The telemetry as one compact line across the top, the brand at its start and the freshness at its end; every signal fits a tablet on its side |
| **Product tiles** | The products as tiles that fold the good ones when they do not fit |
| **Side column** | The present side panel: the latest commits, the pull requests and the issues as sections at the right at full height (below the rest in portrait), each hidden by its button and scrolling inside, equal shares, a rail when all are hidden, its resize edge |
| **Tap bar** | The controls a tap or a key shows for a few seconds: the profile and the window's size, back to the dashboard, the theme, *On a wall*, full screen, the design mode (admins) |
| **Profile** | How the page is set for a screen: Handheld portrait, Handheld landscape, Desk, Wall |

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
   added to it; a product kept alive only by dependency updates reads as quiet. Agents are co-workers (P-40): `commits.Classify` tells
   each commit's origin, a person, a person with an AI (`assisted`: an AI `Co-authored-by` trailer, or an AI tool's footer), a coding
   agent as the author (`agent`), or automation (CI, dependency and release bots, by `[bot]` or a bot-like name, and a bot's pull
   request merged by a person). Assisted and agent commits are people's work and counted within it as "with AI", the assistant's
   family named (Claude, Copilot); inline completion leaves no trace, so the count is a lower bound. A cut body keeps its trailers.
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

- `activity`: commits by people (and of them, with AI) and by automation in the last 30 days, 12 weekly counts of commits by people
  (and with AI, drawn at the foot of each bar in the `--ai` colour), the assistants (at most 3 families), the people (name and
  GitHub login, at most 5, then a count; the web shows the login, the name on hover, as the commit lists do), and the 3 repositories people changed last.
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
| `latest-commits` | the newest commits of the work that counts, or of the kinds the reader shows them (its label is a button, `ShowPicker` in `show.tsx`: a dialog of the three kinds as checkboxes, each in its colour with its count in the rules' window and what it covers, one at least; kept in the cookie `scp-latest-work`, read by the server; "Same as the work that counts" forgets it; the presentation page's column has the same). Its end in view reads the next pages of the same lens (`/commits?work=&page=`, 50 each, a commit once however new ones moved the pages; at most 10 pages, then "Every commit, with filters" links `/commits`; without `IntersectionObserver` the list stays and links the same). Right after `products` it is a section of their side panel (below) | count |
| `pulls` | the open pull requests whose turn it is first (`pulls.md`) | count |
| `issues` | the open issues that wait first, of the work that counts (people's, and automation's when it counts): where each stands and its kind, a line of how many need triage and how many nobody took, "All N open" to `/issues`; nothing when the feature is off (`features.md`) | count |
| `telemetry` | the fleet tiles (repositories, products, people this month, findings, freshness) | tiles |
| `heading` | a plain-text heading and line of text, no HTML | text |
| `announcements` | the announcements the page layout's answer carries for this viewer and page (`announcements.md`), dismissible on the dashboard (the cookie `scp-announcements`), with QR codes on `/present`; nothing when there are none. In the built-in layouts the first row below the top bar: first on the dashboard, after the strip on `/present` | none |

`groups` selects products: all, or a list of group ids, in that order. Elements select groups, never repositories, so a layout never
names a repository. The default landing page is a `telemetry` row, the `fleet-map` band, `products` as cards, then `latest-commits`.
Per-repository rows leave the landing page for the group page. The telemetry tiles (P-61, the same signals as the presentation page's strip): people (commits by hand), AI (with an AI or by an
agent), automation (and sweeps), active repositories (and those in no group), findings (of how many products), in sync.

## The side panel (L5)
The elements right after `products` of the kinds `latest-commits`, `pulls` and `issues` are their side panel on a wide screen (the
dashboard; `/present` has the same sections in its side column, `present.md`). The built-in dashboard has all three,
the latest commits first, then the pull requests, then the issues (owner, 2026-11-06). The panel is sticky at the right and
viewport-high (`100dvh` less the header) and resized by its edge (or the arrow keys). Each element is a section: its header (the
title, the count that links its page, its hide button) stays at the section's top, its list scrolls inside it, and the latest
commits, the pull requests and the issues each load more at the end of theirs (`more.tsx`: the next pages of 25 read 300 px
before the end of the section's scroll, each item once, up to 10 pages, then the link to their page). The open sections share the height equally: three open, thirds; one hidden, halves. Every
section's header ends with the same button, the panel icon (`aria-expanded`), that hides the section to its header and shows it
again; the hidden kinds are kept in the cookie `scp-side-sections` (`pulls,issues`), read by the server so the first paint is right.
Hiding the last open one folds the whole panel to its rail, whose button shows every section again; the rail and the width are kept
in `scp-side-commits` (`open:26`, 18 to 40 rem). Below `lg` the sections follow the products at their own height, each still hiding.
A switched-off feature's element is not there, and the others share the panel.

## Layouts (L2)
A layout is a page's elements and their options, as data; the file is the API's element list (`LayoutElement` in the contract, one
flat object per element whose kind reads only its own fields):

```json
{"elements": [
  {"kind": "telemetry"},
  {"kind": "heading", "text": "Products with work this month"},
  {"kind": "products", "groups": ["core", "tools"], "view": "table"},
  {"kind": "latest-commits", "count": 10}
]}
```

- **Validated in Go** (`internal/layout`, a feature): one JSON value of at most 64 KiB, unknown fields refused, 1..50 elements, a known
  kind with only its own options (`groups` 1..100 unique group ids for `fleet-map` and `products`, `view` for `products`, `count` 1..50
  for `latest-commits`, `text` (required) and `note` for `heading`, cleaned to one plain line with `untrusted.Text`). No HTML, no
  scripts, no URLs. The web UI renders only known kinds. Absent `groups` means every group.
- **Store**: `layout.Store` over `layout.dir`, one `<page>.json` per page, written to a temporary file, synced and renamed. The revision
  is the file's content digest (SHA-256, 128 bits), empty when there is no file; a save or reset on another revision is refused (412),
  so two admins never overwrite each other. A broken file keeps the last good layout serving and reports why; its revision is the broken
  content's, so an admin can save over it. Without `layout.dir` the built-in layout renders and saves answer 409.
- **Not a cache, not a user store**: layouts are the site's, edited by admins; they never go into SQLite (rule 5) and nothing is kept
  per person (P-27).
- **Reload**: each instance stats the files every 5 s and reads a changed one, so a shared volume serves several instances and a file
  written by hand or by configuration management is picked up. A file that fails validation is logged and shown on the status page
  (`/api/v1/status` warnings) and in the design mode.
- **API**: `GET /api/v1/pages/{page}` (contract) answers the elements resolved against the viewer's view: a group with no repository the
  viewer sees is dropped, and an element whose chosen groups all are, so the leak test (which walks every contract GET) covers it.
  `GET`, `PUT` and `DELETE /api/v1/admin/layouts/{page}` (outside the contract, as the admin status is; admin role; writes from the
  portal's own pages) read the stored layout with its revision, whether it can be saved and its problem, save `{elements, revision}`
  (422 with the reason when invalid) and reset (`?revision=`).
- **Design mode** (`web/src/features/dashboard/design.tsx`): *Design this page* in the dashboard's header for admins. The elements in a
  list (move with the arrow buttons or by dragging, remove, the options of each), a new element from the catalogue with what it shows,
  and the draft drawn live beside it. *Save*, *Discard changes*, *Back to the built-in layout*, *Done* (asks before dropping unsaved
  changes). A refused save offers to load the newer layout. The reader's products view (the cookie) wins over a layout's `view`.
- **Pages**: `dashboard` and `present` (the presentation page, `design/present.md`). The page is in the address, so further pages
  (a product's page, named dashboards) come without a format change.

## The work that counts and Settings (L4)

People and agents work together, and automation maintains, so none is ranked: a card leads with the commits of the work that counts and
names every kind under it. The reader's lens (P-42, P-43) is a set of kinds, any one or more of **By hand**, **With AI** and **By
automation** (CI, dependency and release bots, and sweeps): a preference in the cookie `scp-work` (`hand,ai`), every kind until the
reader chooses. The dashboard route reads it and asks `GET /dashboard?work=hand,ai` (comma-separated, the
clients' `querySerializer`), and Go computes the lens (`commits.Work`, a bit set; `oapi.WorkOrDefault` orders the echo): the counted
number, the last change, the recent repositories, the members' last change (so the fleet map and the Active tile) and the latest
commits; who made the work is people only (automation names none). Every kind's counts are always in `Activity` (`hand`, `ai`,
`automation`, `sweeps`, `weeklyHand`, `weeklyAi`, `weeklyAutomation`), so a card names the rest, with a coloured dot for each kind
that counts; the sparkline stacks the counted kinds, with AI at the foot (`--ai`), by hand, then automation (`--muted-foreground`).
Sentences name the lens with `by(work)`: "by the team", "by hand", "with AI", "by automation", "by hand or automation", "by the team
or automation". The latest commits' label says what they show and chooses it (P-63). `/commits?work=` keeps
the kinds it names (every kind without it). Health, the group page and the repository page count people's work
(`commits.WorkTeam`): they score and list repositories, they are not the reader's lens.

`/settings` (in the header, for every viewer, not indexed) holds the reader's preferences: the work that counts (a checkbox per kind,
the last one ticked stays), the products view,
the page sizes of the commit timelines and the histories (Default forgets the cookie), and the theme (its own storage, as before).
Each is kept at once, the page announces it, and the server's pages follow it on their next render; `lib/preferences.ts` holds the
cookies and their parsing, so the features share them without importing each other. Nothing is stored per person (P-38).

## Not now
Layouts per person or per sign-in group, repository-level selection in elements, other stores (a Git repository, S3), and sharing layouts
between instances through Hazelcast. Each needs its own decision against P-27 and P-35.
