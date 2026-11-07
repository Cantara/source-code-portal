# Open pull requests: whose turn it is (PR1 to PR3)

Status: PR1 to PR3 built (plan.md section 4, P-68). Binding order: ../plan.md > this note > `layout.md`, `health.md`, `github.md`.

Across many repositories the open pull requests are the work in flight: what waits for a reviewer, what waits for its author, what is
ready and nobody merges, what dependency bots keep opening. GitHub shows them per repository or through a search one has to write.
The portal shows them for the fleet, a product and a repository, and says for each **whose turn it is**. Closed and merged pull
requests are not kept: their trace is the commits the portal already reads.

Today the portal knows only GitHub's `open_issues_count`, which counts issues and pull requests together (`snapshot.Repo.OpenIssues`,
the health finding `issues`, the badge, the group table's Issues column). PR1 splits them.

## The choice

**A `pulls` feature with a job per repository: a conditional REST listing of the open pull requests decides whether anything changed
(a 304 costs no rate limit), then one GraphQL query reads every open pull request's state (review decision, checks, mergeability,
diff stats, labels) and the repository's open issue count. The facet holds at most 100 open pull requests per repository, newest
activity first, with the exact total.**

- **Change detection for free.** `GET /repos/{o}/{n}/pulls?state=open&per_page=100&sort=updated&direction=desc` through the existing
  ETag transport: a repository without change answers 304, which GitHub does not count against the primary rate limit. A repository
  with no open pull request answers `[]` once and 304 after. Its ETag changes when a pull request opens, closes, gets a commit (head
  SHA), a review, a comment or a label (`updated_at`).
- **The state in one query.** GraphQL `repository(owner:, name:) { issues(states: OPEN) { totalCount } pullRequests(states: OPEN,
  first: 100, orderBy: {field: UPDATED_AT, direction: DESC}) { totalCount nodes { number title url isDraft createdAt updatedAt author
  { login __typename } baseRefName headRefName isCrossRepository reviewDecision mergeable additions deletions changedFiles
  reviewRequests { totalCount } labels(first: 5) { nodes { name color } } commits(last: 1) { nodes { commit { oid statusCheckRollup
  { state } } } } } } }`, its `first` sized to the listing's count (GitHub prices a query by its `first` arguments, so a repository
  with 3 open pull requests costs 1 point and one with 100 about 3), only when the listing changed, when a held pull request's checks are pending or its
  mergeability unknown (GitHub computes it lazily), or once an hour (checks change without touching the listing).
- **Triggers.** The job runs on the webhook events `pull_request` and `pull_request_review` (not `check_suite` or `status`, P-73: running checks are asked again on their steps) (already passed
  through by `webhook.Normalise` under their own names), on `EventPush` (a new branch is often a new pull request), on `EventOpen` (the catalogue's listing saw the repository's count of open issues and pull requests move: the safety net when no webhook came), and on its TTL; a run an event caused (`Job.Event`) that found nothing new reads again within 30 seconds (`ConfirmAfter`), because GitHub's listing can be seconds behind the webhook:
  15 minutes for the listing, `Result.Again` while checks are pending or mergeability unknown, at 5, 10, 20 then 60 minutes (a
  required check that never reports cannot keep a repository at 12 queries an hour). An archived repository is read once and again
  only when the listing says it was unarchived: its pull requests cannot change.
- **Measured on Cantara (2026-11-05)**: 180 repositories (12 archived), 888 open pull requests in 116 of them, 525 by bots, 386
  updated in the last 30 days across 100 repositories (Renovate and Dependabot rebasing). So most listings are not `[]` and many
  change daily; the cost is still bounded by the TTL: at most 4 listings and 4 queries of at most 3 points per repository an hour,
  about 700 REST requests and 1,600 points in the worst hour against at least 10,000 of each, and far less in practice.
- **Cost at 1,000 repositories.** 4,000 conditional REST requests an hour, nearly all 304 (free); GraphQL only for changed
  repositories, a few hundred points an hour against a budget of 5,000 or more. The first read is 1,000 listings and one query per
  repository that has open pull requests.
- **Bounded and untrusted.** Titles through `untrusted.Text` (200), labels (5, 50 each), logins as GitHub gives them; no bodies, no
  comments, no reviewers' text. The GraphQL answer read with `jsonstream` like the commits list.

Rejected:
- **GitHub's search API across a source** (`is:pr is:open org:x`): one query for a whole organisation, but capped at 1,000 results,
  an index that lags, no per-repository freshness, and a second rate limit of 30 a minute.
- **REST only**: the listing has no review decision, no checks and no diff stats; asking them is three more requests per pull request.
- **GraphQL only, every run**: no conditional requests in GraphQL, so every repository costs points on every run even when nothing
  changed.
- **Keeping closed pull requests**: the history is the commits; "merged this week" can come later from the default branch's merge
  commits without another call.

## The state: whose turn

Each pull request gets one **state**, the first that holds, and the party it waits for:

| State | When | Waits for |
|---|---|---|
| Draft | `isDraft` | the author |
| Conflicts | `mergeable: CONFLICTING` | the author |
| Failing | the head's checks `FAILURE` or `ERROR` | the author |
| Changes requested | `reviewDecision: CHANGES_REQUESTED` | the author |
| Needs review | review required, or no approval and no review yet | reviewers |
| Checks running | the head's checks `PENDING` or `EXPECTED` | CI |
| Ready | approved, or no review required, checks passing or none | a maintainer to merge |

**Who opened it** is told as the commits tell it: a person, or automation (Dependabot, Renovate, any `[bot]` or bot-like login, or a
dependency bot's branch: Snyk opens its pull requests under the account of the person who connected it, so on Cantara 195 of them
named people until `snyk-fix-` and `snyk-upgrade-` told them). The tests live in the core package `internal/actor` (`Bot`,
`Branch`), used by both features, and are applied again at every publish, so a change reaches the stored ones at a warm start. The
reader's work lens (P-42) applies: bots' pull requests count only when automation is in the work that counts; a separate count says
how many dependency updates wait either way.

**Age** is the time since it opened, **idle** the time since its last activity (`updatedAt`). The lists sort by "waits longest":
people's pull requests that wait for reviewers first, then by idle time.

## Health

`internal/health` (P-62: the rules live there and `Describe` tells them):
- `issues` counts issues only: GitHub's count minus the open pull requests once the facet is read.
- `reviews` (new): a person's pull request waiting for review longer than 7 days, a small penalty; longer than 30 days, a larger one.
- `updates` (new): automation's pull requests open longer than 30 days, a small penalty (dependency updates nobody takes).
- A draft never costs points. The thresholds and penalties are set in `rules.go` with the rest, and the guide's table follows.

## Where it shows

The rule of the landing page holds (layout.md "Signals, not attributes"): the dashboard shows the exception and the count, the lists
live on their own page.

1. **`/pulls`, "Pull requests" in the header** beside Commits: every open pull request of the viewer's view, server-rendered with a
   the filter panel at the left (`design/filters.md`; a GET form without JavaScript): the states with their counts, which filter; product, repository, opened by (people,
   automation), and drafts hidden by default. Each row: state and the party it waits for, title linking to GitHub, `repo #123`, the
   author, age and idle, `+a −d` with the five blocks (`DiffStat`), labels, the checks' mark. Numbered pages (`Pager`), the page size
   remembered (P-26).
2. **Dashboard, telemetry**: a tile "Pull requests" with the open ones of the work that counts; its hint: needs review, ready,
   failing, oldest waiting, dependency updates. It links to `/pulls`.
3. **Dashboard, products**: the card keeps its five rows; the findings row carries the new findings (`reviews`, `updates`) as any
   other. The products table gets a **PRs** column (open, the ones that need review in the hint).
4. **Group page**: a **PRs** column in the repositories' table (centred like Issues, sortable), and a side panel "Waiting for review"
   with the group's five longest-waiting pull requests and a link to `/pulls?group=`.
5. **Repository page**: a panel "Open pull requests" (`pulls/panels.tsx`, in `panels.ts`): the ten that wait longest, the counts by
   state, a link to `/pulls?repo=` and to GitHub's list; the header's figures say issues and pull requests apart.
6. **Commits navigator**: a branch that has an open pull request carries its `#123` and state beside its name (the job holds
   `headRefName`; the commits job already reads the ten newest branches).
7. **The `pulls` element** ("Pull requests", its one option how many): the newest open pull requests first (owner, 2026-11-06), each
   two short lines with its state, `repo #123`, title, author and last activity, and "All N open" to `/pulls`; nothing when none is
   open. In both built-in layouts: a section of the side panel after the latest commits (5) on the dashboard and of the side column (8) on `/present`
   (`layout.md`, L5 and L6), beside the strip's signal "Pull requests" (the telemetry's, P-61); a single product named in the address,
   `?groups=`, gives its own. An admin moves or removes it in design mode.
8. **Agents and the rest**: the tool `open_pull_requests` in the catalogue (assistant, MCP, WebMCP; the `/pulls` filters), the
   search index's kind `pull` (title, head branch, state, author; `in:pull`, a hit opens `/pulls?repo=`), the badge
   `/badges/repos/{o}/{n}/pulls.svg` and the group's (open, and of them needing review), a "Pull requests" section in a
   repository's Markdown answer and `/pulls` in the sitemap; the job's runs in the engine's metrics like every job.

A private repository's pull requests are in its facet and so only in a private view; the leak test walks `/pulls`, its API, the tool
and the badges.

## Shape

- `internal/pulls` (feature, facet `pulls`, `Of`): `Pull` (the listing's fields; GraphQL's `Review`, `Approved`, `Changes`,
  `Checks`, `Mergeable`, `Stats` and `Asked`), `StateOf(p)`, `Set` (`Total`, `Pulls`, `Full`, `Pending`, `PendingAt`); the job
  `pulls` (`merge` decides what to ask: new or changed by update time or head, all hourly, the unsettled ones by their step); an
  index per snapshot (`Feeds()`: per repository, group and fleet, whose turn first); `Counts`, `Issues` (GitHub's count less the
  pull requests), `Waiting` (for health), the hooks `Repo` and `Dashboard` (automation's only when the work that counts holds it).
  The REST listing gives every field but the reviews, checks, mergeability and stats, so a failed GraphQL query still lists them.
- `internal/github`: `GraphQL(ctx, client, query, vars, out)` beside `REST`, the endpoint rule `graphQL(base)` moved from `commits`; D1's
  stats query uses it.
- Contract: `GET /api/v1/pulls` (paged; `group`, `repo`, `state`, `by` (people, automation), `drafts`, `sort`), `PullRequest`;
  `RepoSummary.openPulls` beside `openIssues` (issues only); `DashboardGroup.pulls` (counts by state); `PullSummary` on the
  dashboard's fleet.
- The fake GitHub: `SetPull(owner, name, Pull)` serving the REST listing with ETags and the GraphQL fields.

## Milestones

- **PR1, the data**: `internal/actor`, `github.GraphQL`, the `pulls` job and facet, the issue count split, the health findings, the
  contract and `/api/v1/pulls`, the fake, the leak test. Done when: an unchanged repository costs one 304 and no GraphQL; a review
  webhook re-reads one repository; pending checks are asked again; a private repository's pull requests never reach the public.
- **PR2, the pages**: `/pulls` (the header's "Pull requests"), the telemetry tile and the strip's signal, the `pulls` element on the
  dashboard and `/present`, the PRs columns, the group's panel, the repository's panel, the navigator's
  branch chip. Done when: Playwright filters `/pulls` by state with and without JavaScript, each page shows the fake's pull requests,
  axe clean in both themes.
- **PR3, the rest**: the tool, search, badges, llms, docs, the Guide, the plugin.
