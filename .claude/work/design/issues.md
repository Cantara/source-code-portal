# Open issues: what waits to be triaged and taken (IS1, IS2)

Status: IS1 and IS2 built (plan.md section 4, P-69). Binding order: ../plan.md > this note > `pulls.md`, `layout.md`, `github.md`.

The portal counts open issues per repository (GitHub's `open_issues_count` less the open pull requests, `pulls.md`) but does not
hold them. Across many repositories the open issues are the work asked for: what nobody has looked at, what is looked at but nobody
took, what someone has. The portal lists them for the fleet, a product and a repository, beside the pull requests, and says for each
**where it stands**. Closed issues are not kept.

## The choice

**An `issues` feature with a job per repository: GitHub's REST listing of open issues, read page by page through the ETag
transport (an unchanged page answers 304, which costs no rate limit), sorted by the last update so any change lands on the first
page. No GraphQL: the listing has every field the portal shows. At most 100 open issues per repository, the most recently updated
first, with the exact total when every page was read.**

- **The listing.** `GET /repos/{o}/{n}/issues?state=open&per_page=100&sort=updated&direction=desc`. It holds pull requests too (an
  item with `pull_request`), which the job skips. It reads the next page while a page is full and fewer than 100 issues are held, at
  most 3 pages: a repository with many open pull requests still reaches its issues.
- **Fields.** Number, title (`untrusted.Text`, 200), URL, author and whether its account is a bot, created and updated, labels (5,
  50 each, with colour), assignees (3 logins and their count), comments, milestone title, GitHub's issue type (`type.name`). No
  bodies and no comments' text.
- **Triggers.** The webhook event `issues` (passed through by `webhook.Normalise` under its name; not `issue_comment`, P-73) and the
  TTL: 15 minutes, and `EventOpen`: the catalogue's listing (every 5 minutes) notices that a repository's count of open issues and
  pull requests moved, the safety net when no webhook came. A run an event caused (`Job.Event`) that found nothing new asks to run again in 30 seconds
  (`ConfirmAfter`; one that found a change needs no second read): GitHub's listing can be seconds behind the webhook that announced the issue. An archived repository is read once.
- **Measured on Cantara (2026-11-05)**: 351 open issues in 136 repositories, at most 17 in one; 128 are Renovate's dependency
  dashboards. One page per repository, nearly always 304: at most 4 free requests per repository an hour.
- **Cost at 1,000 repositories.** 4,000 conditional requests an hour, nearly all 304; the first read 1,000 requests and a few more
  for repositories whose pull requests fill a page.

Rejected:
- **GraphQL `issues(states: OPEN)`**: only issues and an exact count, but no conditional request: every run costs points.
- **GitHub's search API across a source**: 1,000 results, a lagging index, 30 requests a minute.
- **Keeping closed issues**: what was done is the commits and the pull requests.

## Where it stands

Each issue gets one **state**, the first that holds:

| State | When | Waits for |
|---|---|---|
| Assigned | someone is assigned | the assignee |
| Needs triage | no label, no milestone, no issue type | a maintainer to look at it |
| Unassigned | triaged but nobody assigned | someone to take it |

Its **kind** is GitHub's issue type when set, else read from its labels: `bug` (bug, defect, regression, crash), `feature`
(enhancement, feature, improvement), `question` (question, support), else `other`. **Who opened it** is a person or automation by
`internal/actor` (Renovate's dependency dashboard is automation), and the reader's work lens counts automation's only when it holds
automation, as for pull requests. The lists sort by "waiting": people's before automation's, needing triage first, then
unassigned, then assigned, the longest idle first.

## Where it shows

1. **`/issues`, "Issues" in the header** after Pull requests: every open issue of the viewer's view, server-rendered, the filters in
   the panel at the left (`design/filters.md`): the states with their counts, which filter; kind, product, repository, opened by, order.
   Each row: state, kind, title linking to GitHub, `repo #12`, the author, assignees, comments, idle and age, labels, milestone.
   Numbered pages, the page size remembered (P-26).
2. **Repository page**: a panel "Open issues" (`issues/panels.tsx`): the ten that wait longest, the counts by state, links to
   `/issues?repo=` and GitHub's list.
3. **Group page**: the repositories' table's Issues column links each count to `/issues?repo=`; a panel "Issues" below the pull
   requests' with the five that wait longest and a link to `/issues?group=`.
4. **Agents and the rest**: the tool `open_issues` (assistant, MCP, WebMCP), the search kind `issue` (title, labels, author,
   `in:issue`), a section in a repository's Markdown answer, `/issues` in the sitemap. The issues badge keeps its count.

The dashboard and `/present` stay as they are: the product card's finding `issues` already carries the signal (layout.md "Signals,
not attributes"). A private repository's issues are in its facet and so only in a private view; the leak test walks `/issues`, its
API and the tool.

## Shape

- `internal/issues` (feature, facet `issues`, `Of`): `Issue`, `Set` (`Total`, `Exact`, `Issues`), `StateOf`, `KindOf`; the job
  `issues` (`github.go`: the listing streamed through `jsonstream`); `Feeds()` (fleet, group, repository, waiting first); `Counts`;
  `api.go`: `GET /api/v1/issues`.
- Contract: `GET /api/v1/issues` (paged; `group`, `repo`, `state`, `kind`, `by`, `sort`), `Issue`, `IssueState`, `IssueKind`,
  `IssueCounts`, `IssuePage`; the labels of pull requests and issues are one schema, `IssueLabel` (GitHub's labels are issue labels);
  the order is one schema, `QueueSort`, for both lists.
- The fake GitHub: `SetIssues(owner, name, ...Issue)`, served by the issues listing with the pull requests of `SetPulls` among them.
- Every list shares the filter panel and the row (`design/filters.md`: `shared/filters.tsx`, `shared/list-row.tsx`).
- The GitHub App needs **Issues: read** and the event **Issues**.

## Milestones

- **IS1, the data and the page**: the job and facet, the contract and `/api/v1/issues`, the fake, `/issues` in the header, the
  repository's panel and the group's links, the leak test. Done when: an unchanged repository costs one 304; pull requests in the
  listing are left out; a private repository's issues never reach the public; Playwright filters `/issues` with and without
  JavaScript; axe clean in both themes.
- **IS2, the rest**: the tool, search, llms and sitemap, docs, the Guide, the plugin.
