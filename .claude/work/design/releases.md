# Releases: what was shipped and when (RL1 to RL3)

Status: RL1 built, RL2 and RL3 in progress (plan.md section 4, P-82). Binding order: ../plan.md > this note > `pulls.md`, `github.md`, `packages.md`.

The portal holds only the **latest** release of a repository (the badge's job, `badges.release`). A team that wants to know what the
fleet shipped this week has nothing to look at. The portal lists the recent releases of the fleet, a product and a repository, newest
first, sortable, with the variants a reader asks for.

## The choice

**A `releases` feature with a job per repository: GitHub's REST listing of releases (`GET /repos/{o}/{n}/releases?per_page=20`),
read through the ETag transport (unchanged: 304, free), the 20 newest published ones kept. It replaces the badges' latest-release job:
the latest release is the newest stable one in this list, so a repository is read once, not twice.**

- **Fields.** Tag, name (`untrusted.Text`, 200), URL, published time, whether it is a pre-release, the author's login and whether it
  is a bot, the number of assets. No notes' text (a release's body is repository-authored markup: the first line of it, cleaned
  with `untrusted.Text` and bounded to 200, is the only text kept). Drafts are never listed (GitHub shows them only with write
  access; the listing skips them anyway).
- **Latest.** The newest non-prerelease by publication time. `badges.release`, the `release` facet, `badges.Repo`'s release and the
  group badge read it through the same hook (`cmd/scp` hands the facet to `badges.Options.Data`; features never import each other);
  the job `badges.release` and its `FromGitHub` go away (greenfield).
- **Triggers.** The webhook event `release` (already normalised to `EventTag`, P-73) and a TTL of 6 hours, safety net only: a
  release is published by a person, rarely, and the webhook is the primary path (P-80, with the delivery log's catch-up every two
  minutes). A TTL of an hour was tried: a cold start of 1,000 repositories takes two hours, so every repository was read again before the
  last one was first read. An archived repository is read once.
- **Cost at 1,000 repositories.** 4,000 conditional requests a day, nearly all 304. Prerequisite (P-71): read access to
  *Contents* (already required), added to `prereq.Needs` with the documentation's table in the same commit.

## API

`GET /api/v1/releases` (contract): `group`, `repo` (as `scope.Params`), `variant` (`latest`: one row per repository, its newest
stable release, **the default**; `all`: every kept release; `stable`; `pre`: pre-releases only), `since` (an RFC 3339 time or a number
of days, `30d`), `sort` (`newest` the default, `oldest`, `repo`, `group`), numbered pages. The answer has the rows (repository
summary, group, release), the totals per variant (so the chips show their counts) and the scope's freshness. Per request from the
viewer's view; the leak test lists the route.

## Pages

`/releases` in the header: the variants as chips and the sorts as links, a GET form without JavaScript, a row per release (repository,
tag, name, how long ago with the exact time in a hint, a *pre-release* chip, the link to GitHub). A repository page gets a *Releases*
panel (its last ten); a group page its group's. Server-rendered, canonical, in the sitemap. RL3 adds the `releases` element to the
dashboard's elements catalogue (a section of the side panel and of `/present`'s column, as the pull requests).

## Agents and the rest (RL3)

The tool `recent_releases` (same parameters), the search kind `release` (tag and name, a hit opens the repository's release), the
`llms.txt` line per repository's latest release, the badges unchanged in address. Docs, the Guide, the plugin.

## Tests

The job against the fake GitHub: the listing's pagination bound, 304 replay, a pre-release and a draft, a repository without
releases (not a failure), a webhook `release` event that makes the new release appear, the badge reading the same data, a private
repository's releases hidden from the public (twin test). The API: each variant and sort, `since`, paging. Playwright: the page with
and without JavaScript, axe in both themes.
