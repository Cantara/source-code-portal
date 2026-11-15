# Parked repositories (PK1)

Status: built (plan.md section 4, P-120). Binding order: ../plan.md > this note > `forks.md`, `indexing.md`.

## Goal

Some repositories should have been archived on GitHub and were not. An admin parks them: the portal keeps reading them from GitHub,
webhook events included, and stores everything as for any repository, but shows them nowhere. Taking one off the list shows it again
at once, with everything that happened while it was parked.

## What is built

- **The list.** The document `parked` in the store (`internal/docstore`): `{"repos": ["owner/name", ...]}`, lower-case addresses,
  sorted, at most 2,000; `internal/parked` (`Parse`, `Set.Has`, `Set.Repo`). A repository is parked by its address: a rename on
  GitHub shows it again under its new address, and the old address stays on the list as *missing* until it is unticked.
- **The snapshot.** `snapshot.Publisher.Park(fn)` (as `Retire`): at every publish the base snapshot names the repositories `fn` says
  are parked (`Snapshot.Parked`, `ParkedCount`) and builds every view from the base without them (`views()`: A is the base without the
  parked repositories, then the public, lean and both views as before; `View` reads the base's `fat`, which is A). The base keeps them:
  the next build starts from it, the jobs write to it, and the webhook's catch-up resolves a delivery's repository in it. A save calls
  `Publisher.Update` with no change, so the views are rebuilt at once.
- **What follows from the views, with no feature code.** Every page, API answer, list and feed (commits, pull requests, issues,
  releases, archive, insights), search, badges and images, llms, KCP, the sitemap, the tools, MCP and the assistant, the enterprise
  mode's per-person views (`Restrict` starts from `fat`), for the public, private viewers and admins alike. A parked address answers
  404 like one that does not exist; its old aliases too. A group whose members are all parked is in no view (P-112); a group whose
  default repository is parked says its default is missing.
- **Engine and store: unchanged.** A parked repository stays catalogued: its jobs run on their cadence and on webhook events, the
  inbox parks and replays its events, `Engine.Handled` counts them. Its items move the base's version, never a view's.
- **The admin's card** (`features/admin/parked.tsx`): every catalogued repository by product, private ones and forks marked, a box
  to tick per repository, a count, a filter, the parked addresses the catalogue no longer holds; Save sends the whole list with its
  revision (412 on someone else's save in between). `GET`/`PUT /api/v1/admin/parked` (admin, which implies private; same origin).
- **The status.** `/api/v1/status` `snapshot.parked`: how many of `snapshot.repos` are parked.

## Rules

- Parking overrides a group's `default_repo` and `exact` pins: a parked fork is parked.
- No switch shows parked repositories (no `?parked=show`): the four views stay four.
- Not access control: parking hides for noise, as a hidden fork; a private repository stays private by `access.*`.
- Read-only towards GitHub as ever; a parked repository costs what any other does.

## Acceptance

1. A parked repository, public or private, answers 404 to the public, a private viewer and an admin, and is named in no list, feed,
   search, llms answer or the sitemap (`cmd/scp/parked_test.go`).
2. Taken off the list it is back at the next publish with what was read meanwhile (the same test).
3. The four views and a restricted view leave it out, the base keeps it, a group of parked repositories only is in no view, a parked
   repository's change moves no view's version (`internal/snapshot` `TestParkedRepositoriesAreInNoView`).
4. The card lists every repository with its state and the missing ones, and saves with the revision (`parked.test.tsx`).

## Definition of done

Code and tests, the full gate, `docs/operator-guide.md` 2.12, the plugin's `SKILL.md`, the code map, the changelog, one PR with CI green.
