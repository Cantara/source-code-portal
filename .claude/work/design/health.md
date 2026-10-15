# Health
Status: in progress (H1). Binding order: ../plan.md > this note.

## Scope
A product group exists so its owner sees at a glance that something needs attention. Health turns each repository's signals into a
score with the findings that cost it points, and weighs the members into one score per product and one for the fleet. Every point a
score loses is a finding the reader can see: the score summarises, the findings say what to do.

## Model
`internal/health` is pure (no feature, no snapshot): `Assess(Signals, now)` for a repository, `Weigh([]Member)` for a group.

Signals of a repository: archived, lifecycle (`scp.yaml`), visibility, the last change by a person, the newest commit, the latest
release (tag and time), the licence, open issues and pull requests, and whether SCP keeps failing to read it.

The last change is the newest default-branch commit by a person (`commits.LastAuthored`), else the last push. Automation is not a
person: an author ending in `[bot]` (Renovate, Dependabot, GitHub Actions) or a `[maven-release-plugin]` commit. With dependency
updates merging themselves, a repository nobody works on would otherwise look active. When every read commit (the latest 30) is
automated, the oldest of them stands in and the finding says "more than". The API carries it as `Repo.lastAuthoredAt`, and the web
UI's activity levels use it too, so the activity strip and the score agree.

Not counted: archived repositories and `lifecycle: deprecated` (on its way out; no finding, no weight). A counted repository starts at
100 and loses, at most once per row:

| Code | Penalty | When |
|---|---|---|
| `failing` | 25 | SCP cannot read it: the repository or its commits failed 3 times in a row, or never succeeded |
| `dormant` | 30 | no change for 2 years: archive it or revive it |
| `quiet` | 10 | no change for 6 months |
| `unreleased` | 20 | a release exists, a commit (any, automated too) landed after it and the release is older than 90 days |
| `no-licence` | 20 | public and GitHub detects no licence |
| `issues` | 10 / 20 | more than 20 / 50 open issues and pull requests |

The score is 100 minus the penalties, at least 0. Levels: `good` from 80, `watch` from 50, `attention` below.

A product weighs its counted members: weight 2 for a repository changed in the last 6 months (work in progress matters most), 1
otherwise, times 3 for the group's default repository. The product score is the weighted mean, rounded; its level is the mean's level,
at least `watch` while any member is at `attention`, so one neglected repository is never averaged away. The product's findings are its
members' findings, ordered by penalty times weight, then address. The fleet is weighed the same way over every repository, without a
default.

## Where it shows
- API: `Repo.health` (null when not counted), `DashboardGroup.health` and `Group.health` (`ProductHealth`: score, level, counted members,
  members at `attention` and `watch`, findings; the dashboard carries the first 5 and `findingCount`), `Dashboard.health` for the fleet.
  Computed per request from the viewer's view (`cmd/scp` maps the facets to signals; features stay apart).
- Web: the dashboard's Health tile (fleet score, products needing attention); each product card shows its score, level and first
  findings; the group page shows every finding; a repository's health badges add a chip when it is not `good`, its findings in the title.
- Badges: `/badges/repos/{owner}/{name}/health.svg` and `/badges/groups/{group}/health.svg` (score and level colour), the group's
  `last-commit.svg` (its newest commit) and `release.svg` (the default repository's latest release, else the newest in the group).
