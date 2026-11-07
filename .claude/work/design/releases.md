# Releases: what was shipped and when (RL1 to RL3)

Status: RL1 to RL4 built (plan.md section 4, P-82, P-84). Binding order: ../plan.md > this note > `pulls.md`, `github.md`, `packages.md`.

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

`/releases` in the header: the variants with their counts and the sorts in the filter panel at the left (`design/filters.md`), a row per release (repository,
tag, name, how long ago with the exact time in a hint, a *pre-release* chip, the link to GitHub). A repository page gets a *Releases*
panel (its last ten); a group page the latest of each of its repositories, with the way to its retired repositories. Server-rendered, canonical, in the sitemap. There is no `releases` element on the
dashboard or `/present` (P-84): the side panel already shares its height among three sections and the work-that-counts lens does not apply
to a release.

## Agents and the rest (RL3)

Built: the tool `recent_releases` (same parameters), the search kind `release` (tag, title and the first line of the notes; a hit opens
the repository's releases, `/releases?repo=&variant=all`), the repository Markdown page's *Latest release* section and, in `llms.txt`, the
JSON links to the releases and the retired repositories; the badges are unchanged in address. Docs, the Guide, the plugin.

## Published packages (RL4)

A release is also what a repository publishes to a registry: the product page already shows the newest package where a repository has no
GitHub release, and `/releases` listed none of them (owner, 2026-11-07). The packages feature holds each package's newest release
only (version, when published, the registry's link), not a history, so a package is **one row**: its newest release.

- **Rows.** `source` is `github` or the package's ecosystem (`maven`, `npm`, ...), `package` the package's name (empty for GitHub's),
  `tag` the version. A package is never a pre-release (the registries' newest stable is kept), has no author and no summary.
- **Left out.** A package whose version is the tag of one of the repository's GitHub releases (`v` ignored) is that release, listed
  once, as GitHub's. A package whose registry gives no date is not listed: a list of releases is in time.
- **Wiring.** `releases.API.Packages` is a hook (`cmd/scp/releases.go`, `publishedOf`, from the `packages` facet): the releases
  feature does not import the packages feature.
- **Latest.** `variant=latest` is one row per repository: the newest of its latest stable GitHub release and its packages' newest
  (GitHub's when they are as new), so a repository that publishes only to a registry is listed. The product page's Release column
  uses the same rule (`entities/repo/packages.ts`, `shipped`).
- **`source`** (`all`, `github`, `packages`; default `all`) limits the rows to one kind; with `latest` it is the newest of that kind.
  The counts of the variants follow `source` and those of the sources follow `variant`, so each choice shows what it lists.
- **Page.** The filter panel has a *Source* group (GitHub releases, Packages; each its count); a row shows the version, the package's
  name, a source chip (GitHub, Maven, npm, ...) and, as before, the repository chip and when it was published. A repository's
  and a group's panels list them too, with the package's name.
- **Agents.** `recent_releases` takes `source` and says each package row's package and registry.

## Tests

The package rows: a repository whose only release is a package, a package that is GitHub's release (listed once), an undated one,
the latest of both, the counts by source and variant, `since`; the public view hides a private repository's packages (twin).

The job against the fake GitHub: the listing's pagination bound, 304 replay, a pre-release and a draft, a repository without
releases (not a failure), a webhook `release` event that makes the new release appear, the badge reading the same data, a private
repository's releases hidden from the public (twin test). The API: each variant and sort, `since`, paging. Playwright: the page with
and without JavaScript, axe in both themes.
