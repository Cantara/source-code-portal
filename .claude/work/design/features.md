# Feature switches (FT1)

An operator can turn off the open pull requests (`internal/pulls`) and the open issues (`internal/issues`), together or apart,
for a GitHub App without those permissions or a portal that should not show them. Everything else keeps working and says
only what it still knows: best effort (owner, 2026-11-06, P-77).

## Configuration

```yaml
features:
  pulls: false    # default true
  issues: false   # default true
```

Global, for every source. Unknown keys fail `scp config check` (`UnmarshalExact`). Instances of a cluster share the file, so they
agree; the setting is not shared through the cluster. The catalogue has no switch: it is the portal.

## A switched-off feature

It is absent, not empty:

| Where | Pull requests off | Issues off |
|---|---|---|
| Job and data | no `pulls` job, no fetchers, no `Feeds()` index; the warm start's `RetainSources` drops its stored items | the same for `issues` |
| Contract | `GET /api/v1/pulls` answers 404 (problem `feature off`) | `GET /api/v1/issues` the same |
| Dashboard, repository | no `openPulls` on a repository, the `pulls` hook off | no issue panel data |
| Tools (assistant, `/api/v1/tools`, MCP, WebMCP) | no `open_pull_requests`; `search`'s `in` enum without `pull` | no `open_issues`; no `issue` |
| Search | no `pull` documents; `in:pull` finds nothing | no `issue` documents |
| Badges | `pulls.svg` (repository and group) 404 | `issues.svg` stays: GitHub's listing count (below) |
| llms | no pull request line | no issue summary |
| Sitemap | no `/pulls` | no `/issues` |
| Layout | the element `pulls` is skipped when a page resolves (a stored layout holding it stays valid); design mode does not offer it | — |
| Prerequisites | its `Needs` row not checked: no warning for permissions or events it would need | the same |
| Webhook | its events are accepted and change nothing (no job listens) | the same |
| Web | no *Pull requests* in the header, `/pulls` and the repository's pull request panel 404 and hide; the dashboard and `/present` without the element | no *Issues* in the header, `/issues` and the issue panel 404 and hide |

The web UI learns the switches from `GET /api/v1/rules` (`features: {pulls, issues}`), which every page already reads through
`RulesProvider`; a page's own data still answers 404 when its feature is off.

## Health: best effort

The findings judge only what the portal reads:

- **Waiting for review** and **Old updates** come from the pull requests: with pull requests off they never show, and
  `GET /api/v1/rules` leaves them out, so the guide's table and every Health card list only the rules that apply.
- **Backlog** needs no switch: GitHub's repository listing (*Metadata*) carries `open_issues_count`, issues and pull requests
  together. The count it judges is the best one held, in this order:
  1. the Issues feature's own count, when it is on and read the whole list (`Set.Exact`): exact, pull requests excluded;
  2. GitHub's count less the open pull requests read, when pull requests are on;
  3. GitHub's count as it is, when neither is on: issues and pull requests together. The finding then says
     "open issues and pull requests", the same words the tool already uses for that case.

  The order lives in `cmd/scp` (`openIssues`), the only place that sees both features. The repository's `openIssues` on the API,
  the cards, `issues.svg` and the finding all read it, so they agree.

## Tests

- `internal/config`: the keys, their defaults, an unknown key refused.
- `cmd/scp`: a portal with each switch off and both off against the fake GitHub: no request of a switched-off kind reaches it
  (the ledger), the routes, tools, MCP list, search kinds, badges, sitemap and rules as above; the Backlog count in each of the
  three cases.
- `internal/health`: `Describe` without the pull request rules.
- `web`: Vitest for the header and the repository page without each feature; one Playwright run of the e2e server with
  `features.pulls: false` is not added (the Go test covers the answers; the web reads one flag).
- `TestPrerequisitesDocumented` keeps the docs' table equal to `prereq.Needs`; the docs say which rows a switch removes.

## Documents

`docs/operator-guide.md` (the keys), `docs/production.md` (an App without *Issues* or *Pull requests*: switch them off),
`deploy/config.example.yaml`, the Guide's *Connect GitHub* step, the plugin's `run-scp` skill, `.claude/CLAUDE.md`'s config keys.
