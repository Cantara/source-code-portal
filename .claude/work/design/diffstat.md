# Commit diff stats: what each change weighs (D1)

Status: D1 built (plan.md section 4). Binding order: ../plan.md > this note > `commits-navigator.md`, `layout.md`.

The latest commits on the dashboard and `/present`, the commits page and a repository's history say who changed what and when, but
not how much. A one-line fix and a 4,000-line vendored update look the same. D1 gives every recent commit its diff stats, lines
added and removed and files changed, drawn as `+12 −3` with a five-block bar, as GitHub draws a diffstat.

## The choice

**The commits job asks GitHub's GraphQL API for the stats of the commits it holds without them, 100 per query by object id, and keeps
them with the commit for good (a commit never changes). Only commits of the last 90 days are asked, at most 300 per run.**

- REST lists commits without stats; only `GET /repos/{o}/{n}/commits/{sha}` has them, one request per commit: 500,000 requests for a
  first read of 1,000 repositories. GraphQL answers 100 commits by `object(oid:)` aliases in one query of about one point.
- A commit's stats never change, so a SHA is asked once. The next listing carries them over by SHA, as it carries a commit's first
  seen time (`normalise`).
- The window keeps the first read small: what the lists show is recent. 1,000 repositories cost about 1,000 to 3,000 points once,
  then a query per push. An old commit in the history navigator shows no stats, never a wrong one.
- Stats are an extra: a failed or rate-limited query keeps the history read and asks again in 10 minutes (`Result.Again`); it never
  fails the job or pauses the source. A commit GitHub answers `null` for is marked asked (`noStats`) and never asked again.
- The GraphQL endpoint follows the REST base: `https://api.github.com/graphql`, and `https://<host>/api/graphql` on GitHub Enterprise
  Server (REST at `/api/v3/`). Its rate limit is its own (`X-Ratelimit-Resource: graphql`); the rate book keeps the core one.

Rejected:
- **One REST request per commit**: see above.
- **The history connection** (`history(first: 100)`): one query per page of one branch, and it re-reads commits the job already
  holds; aliases ask exactly the SHAs without stats, on any branch.
- **Computing stats from trees**: two recursive trees and a blob comparison per commit, far more requests than GraphQL.

## Shape

- `commits.Commit.Stats` (`*Stats`: `Additions`, `Deletions`, `Files`), nil when unknown; in the stored payload as `stats`.
- `Fetcher.Stats(ctx, owner, name, shas)`; the contract's `Commit` gains `stats` (`additions`, `deletions`, `files`), absent when
  unknown.
- The fake GitHub answers `POST /graphql` for `object(oid:)` aliases from the commits' `Additions`, `Deletions` and `Files` (a commit
  without them is `null`).

## How it looks

`+12 −3` in mono (additions in the diff green, deletions in the diff red, tokens `--diff-add` and `--diff-del` for both themes), then
five blocks shared out as GitHub does: the added share of five, rounded, the rest removed, all grey for an empty change; a hint says
"12 additions, 3 deletions in 2 files". The latest commits (dashboard, `/present`), the commit lists (`/commits`, a group's and a
repository's) and the history navigator show it after the age; a commit without stats shows nothing.

## Done when

The job asks only commits without stats within the window, keeps them across reads, survives a failing query; the lists show `+a −d`
with its bar for the fake's commits in Playwright, axe clean in both themes.
