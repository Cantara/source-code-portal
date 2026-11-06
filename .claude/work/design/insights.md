# Insights: what the held data says about the fleet (IN1, IN2)

Status: IN1 and IN2 built (plan.md section 4, P-83). Binding order: ../plan.md > this note > `health.md`, `layout.md`, `releases.md`.

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
   library that is archived or deprecated and still used is marked: the Alumni's *still used* (`alumni.md`).
4. **Single-author repositories.** The repositories where one author wrote at least 90% of the last 100 default-branch commits
   (people's commits only; automation and sweeps excluded as everywhere, P-40, P-42), with at least 20 commits held. **The page names
   the repositories, never the author:** the finding is "knowledge sits with one person", not who. Bus factor as a list of places
   to share, not of people to watch.
5. **Ageing.** The repositories by months since their last change in the portal's own activity bands (`rules`: active, recent,
   quiet, dormant), with the dormant ones listed: the candidates for retirement. It is the fleet map's data as a list, linking to
   the Alumni for what is already retired.

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
