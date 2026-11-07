# Insights: what the held data says about the fleet (IN1 to IN3)

Status: IN1 to IN3 built (plan.md section 4, P-83). Binding order: ../plan.md > this note > `health.md`, `layout.md`, `releases.md`.

The dashboard shows where things stand now. Insights shows what the portal already holds says over time and across products: how the
fleet is built, what it leans on, how often it ships. **Every insight is derived from the snapshot; the feature adds no job, no
GitHub call and no permission** (releases come from RL1). It answers a question; it does not rank people or products.

## The choice

**Five insights on one page, each a small card with its numbers and the repositories behind them, computed per request from the
viewer's view: Cadence, Technology, Shared libraries, Single-author repositories and Ageing.**

1. **Cadence.** Releases per month for the last 12 months across the scope (the `releases` facet), and per product the median days
   between its last five stable releases. A product that never released says so. Not a score; a product that ships weekly is not
   better than one that ships quarterly.
2. **Technology.** The primary language and build system per repository (the manifests' `Repo`), counted: how many repositories on
   each, the share of the fleet, the newest and oldest runtime version a manifest names (`go 1.21`, `node >=18`) so an ageing
   runtime is visible. Counts only what manifests say; a repository without a manifest is `unknown`.
3. **Shared libraries.** The internal dependencies with the most dependants (the manifests' dependency graph, `Graph()`): the
   blast radius of a change. Each row: the library, its dependants (count, then the list), its latest release if it has one. A
   library that is archived or deprecated and still used is marked: the Archive's *still used* (`archive.md`).
4. **Single-author repositories.** The repositories where one author wrote at least 90% of the last 100 default-branch commits
   (people's commits only; automation and sweeps excluded as everywhere, P-40, P-42), with at least 20 commits held. **The page names
   the repositories, never the author:** the finding is "knowledge sits with one person", not who. Bus factor as a list of places
   to share, not of people to watch.
5. **Ageing.** The repositories by months since their last change in the portal's own activity bands (`rules`: active, recent,
   quiet, dormant), with the dormant ones listed: the candidates for retirement. It is the fleet map's data as a list, linking to
   the Archive for what is already retired.

## API

`GET /api/v1/insights` (contract): `group`, `repo` (as `scope.Params`). One answer with the five insights and the scope's freshness;
the thresholds (90%, 20 commits, 12 months, 5 releases, 10 libraries) are constants of `internal/insights` told in the answer's
`thresholds` (not in `/api/v1/rules`: they are not health rules; the page reads them from the answer, so none is written in the page).
The feature gets what it reads of the others as `Facts` wired in `cmd/scp/insights.go` (releases, manifests, `commits.Authorship`). Per request, the viewer's view, the leak test lists the route.

## Pages

`/insights` in the header: the cards in the order above, each with its question as the heading and the answer first (a number or a
sentence), the detail below it; a group page links to its own insights (`?group=`). Charts are inline SVG from the server's numbers
(no chart library); every figure also exists as text or a table for screen readers; colours are the theme's tokens. Server-rendered,
canonical, in the sitemap. IN2 adds the tool `fleet_insights` (same parameters), the sitemap entry and the `llms.txt` link, the Guide and the docs. No search kind: an insight is derived, not text to find. The plugin is for operators and has nothing of it.

## Tests

Each insight against a fixture fleet with known answers (including empty scopes), automation and sweeps excluded from the author
rule, the author never present in the answer (a test greps the JSON for the fixture's names), a private repository's numbers absent
from the public answer (twin test), the thresholds equal to `rules`. Playwright with and without JavaScript, axe in both themes.

## IN3: the graphs (owner, 2026-11-06: "truly boring")

The first page was five cards of tables. Each insight now leads with a graph that shows the answer at a glance; the numbers stay as text
and tables for screen readers and for the exact value. Drawn as inline SVG from the API's numbers (no chart library, the JavaScript
budget is a sum, P-41), in the theme's tokens: one hue light to dark for magnitude, the fleet map's activity tokens for activity, the
emphasis colour for the thing the card is about and grey for the rest. The forms follow what engineering dashboards use for each
question (a deploy heatmap for rhythm, a dependency matrix for reach, a risk-banded distribution for bus factor):

1. **Cadence: release rhythm.** A heatmap of the last 52 weeks, a row per product (the most active first, at most 12), a column per
   week, the cell's depth the stable releases of that week, the row's median interval written at its end. Beneath it the monthly bars
   with their months and counts. It reads "who ships, how steadily, and where it went quiet". Only the 20 newest releases of a
   repository are held (P-82), so a very busy repository's earlier weeks are empty: the page says so.
2. **Technology: share and the version ladder.** Ranked bars of the languages and of the build tools (one hue); for each runtime a
   ladder of the versions the manifests name, a dot per version sized by its repositories, the oldest one emphasised: the stragglers are
   what to see.
3. **Shared libraries: reach and the dependency matrix.** Ranked bars of the dependants; below them a matrix, the ten libraries as
   rows and the 24 repositories that use most of them as columns, a filled cell where one uses the other. A column filled in many rows
   is a repository tied to many libraries, a full row a library everything leans on. A retired library is marked.
4. **One person's work: the concentration.** A histogram of the share the busiest author wrote, in bands of 10%, over the repositories
   with at least 20 people's commits held; the bands at or over 90% are the emphasis. The list of repositories stays; never a person.
5. **Ageing: the fleet by activity.** A bar per product split into active, recent, quiet and dormant (the fleet map's colours and
   bands, one scale), with the fleet's total on top; the dormant repositories listed.

Every mark has a title (its tooltip on hover and for assistive technology), every chart a text alternative and a hidden table, colour is
never the only carrier (the numbers are written on or beside the marks), and the dark theme uses the same tokens.

