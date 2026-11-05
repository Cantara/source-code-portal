# Prerequisites: what each feature needs from GitHub, and a warning when it is missing (PQ1)

Status: PQ1 built (plan.md section 4, P-71). Binding order: ../plan.md > this note > `github.md`, `pulls.md`, `issues.md`.

The portal reads GitHub with the operator's credential. Each feature needs read permissions and, for updates within seconds,
webhook events. When the GitHub App lacks one, GitHub refuses the calls and the feature's pages stay empty without saying why: on
2026-11-05 the Cantara App had only *Contents* and *Metadata*, so `/pulls` and `/issues` would have been empty. The portal now says
what is missing, where it shows, and how to fix it; the documentation, the Guide, the skills and the example configuration list the
same prerequisites.

## What each feature needs

One table in the core package `internal/prereq`, the single source the code, the status and the documentation follow:

| Feature | Read permissions | Webhook events |
|---|---|---|
| The catalogue: repositories, README and documents, manifests, commits, releases, packages | Contents, Metadata | Push, Release, Repository |
| Pull requests | Pull requests, Checks, Commit statuses | Pull request, Pull request review |
| Issues | Issues | Issues |

Every permission is read-only; the portal never writes to GitHub (P-71): it sends only GET requests and GraphQL queries, never a
mutation, and the fake GitHub fails a test on anything else (but the App's own token request). Events matter only when `webhook.secret_file` is set: without
webhooks every feature still refreshes on its schedule (15 minutes for pull requests and issues).

## How the portal knows

- **A GitHub App** tells its grants: the installation listing the portal already reads at every reconcile (`Apps.ListInstallations`)
  carries each installation's permissions and events. `github.Source.Grants()` keeps them per account. A missing permission is a
  certain answer, known before any job runs.
- **A token** cannot be asked for its scopes (a fine-grained token has none to read), so the portal tells from the jobs: a feature
  whose items in a source fail with GitHub's 403 (`forbidden`) for most of its repositories is reported as refused.
- `prereq.Check(needs, grants, failures, webhooks)` turns both into the unmet prerequisites: the feature, the source and accounts,
  the missing permissions and events, the effect ("`/issues` stays empty") and the fix.

## Where it shows

- **The page of the feature**: `/pulls` and `/issues` show a warning above their list when their feature is off ("Issues are not read:
  the portal's GitHub App lacks the Issues permission"); an administrator sees which permissions, events and accounts, and the link to
  the fix; others see that an administrator can fix it.
- **Admin**: a card "Prerequisites" on `/admin` lists every unmet one with its fix; `/api/v1/status` carries them in `warnings`.
- **The log**: a warning once when the set changes (`prerequisites unmet`, with the feature and what is missing).
- **The API**: `GET /api/v1/prerequisites` (contract): the unmet ones; the missing permissions, events and accounts only for an
  administrator, the feature and its effect for everyone.

## Shape

- `internal/github`: `Grant{Owner, Permissions, Events}`, `Source.Grants() (grants, known)`: known false in token mode.
- `internal/prereq` (core): `Need`, `Needs` (the table), `Unmet`, `Check`.
- `cmd/scp/prereq.go`: reads the grants and the jobs' failures from the snapshot, serves the contract's method, adds the status
  warnings, logs changes.
- The fake GitHub: `SetGrants(login, permissions, events)` for an installation; by default every permission and event.
- Web: `prerequisitesQuery`, `PrerequisiteNotice` (shared) on `/pulls` and `/issues`, the admin card.

## Documentation

`docs/operator-guide.md` section 1 and `docs/production.md` hold the table; `deploy/config.example.yaml` says above
`github.sources` what the App or token needs and what each event gives; the Guide's step "Create the App"; the operators' plugin
(`run-scp`: the setup step and a "features are off" check) and `configure-scp`. A new feature that reads GitHub adds its row to
`prereq.Needs` and to these in the same commit.

## Done when

An App without *Issues* shows the warning on `/issues` and in Admin and logs it; a token refused for pull requests shows it on
`/pulls`; nothing is shown when every prerequisite holds; the public answer names no permission or account; docs, Guide, skills and
the example agree with `prereq.Needs` (a test compares the documentation's table with it).
