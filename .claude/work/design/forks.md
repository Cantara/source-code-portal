# Forks on demand (FK1 to FK3)

Status: in progress (plan.md section 4, P-105; issue 9). Binding order: ../plan.md > this note > `catalog.md`, `layout.md`.

## Goal

A fork is hidden by default and shown on demand, and a fork an organisation really works in can be named so that it is always shown.
Today forks are all in (`github.sources[].include_forks: true`: every fork in every group and in the catch-all) or all out (not synced,
so a group cannot even name one as its `default_repo`).

## What is built

- **FK1, the portal.** The listing admits forks always. A fork is *hidden* when it is a fork, its source does not set `include_forks`
  (which now means: forks are shown by default) and no group *pins* it: a group's `default_repo` (by name, within the group's
  sources) or an `exact` matcher of a group names it. A pinned fork is an ordinary repository. Hidden forks are in the store and the
  engine (every job runs for them, so showing them is instant), not in the views a request reads by default.
- **Views.** A snapshot has four views, by private/public and by forks hidden/shown: `Snapshot.View(private, forks)`. The default
  has them hidden. Every handler already reads `view(ctx)`; the request's choice is `?forks=show` on any GET of the public listener
  (the web UI's cookie `scp-forks`, FK3, turns into it). Versions, ETags, search index, llms, sitemap and counts follow the view. A hidden
  fork's own address answers 404 in the default view like an address that does not exist. Two routes are the exception: the
  badges and the documents' images are fetched by the browser as pictures, with nothing to carry `?forks=show`, and are named by
  the repository's own address, so they answer for hidden forks too (found by running the branch on a real organisation).
- **FK2, the API.** `GET /api/v1/dashboard` and `GET /api/v1/groups` say how many forks are hidden (`hiddenForks`), so a page can offer
  to show them; `scp config groups` says why a fork is not in a group ("fork; pin it with default_repo or exact, or include_forks").
- **FK3, the web UI.** A "Show forks (N)" switch on the dashboard and the group page and in Settings (cookie `scp-forks`), only where
  forks are hidden; a fork carries a "fork" mark where repositories are listed.

## Rules

- No change for a portal without forks. A portal with `include_forks: true` is unchanged (its forks are shown by default).
- Not private: hiding is for noise, not for secrecy. Nothing in the leak tests changes; a hidden fork leaks nothing it did not.
- Agents (the tools, MCP, the assistant) read the default view: pinned forks are in it, hidden ones are not.
- Read-only towards GitHub as ever; the only cost is the forks' own jobs (about the fork count of the requests at a cold start).

## Acceptance

- A fork no group pins is absent from the default view's repositories, groups, counts, search, llms and sitemap, present with
  `?forks=show`, and counted in `hiddenForks`.
- A fork named by `default_repo` or an `exact` matcher is in every view; with `include_forks: true` every fork is.
- The views are consistent: versions differ only when hidden forks exist; with none, the views are the same objects (no extra cost).
- A pinned fork as the only default of a group makes the group whole (`defaultRepoState` ok).
- The web switch sets the cookie and the page shows the forks after it; the switch is absent when none are hidden.

## Definition of done

Code and tests, the full gate, `docs/` (operator guide key row, repository guide, API guide), the plugin's `essentials.md` and
`SKILL.md`, the code map, one PR with CI green.
