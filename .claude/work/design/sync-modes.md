# Events and polling per account: GitHub Apps from the wizard (EV1 to EV3)

Status: built (EV1 to EV3; plan.md section 4, P-95, P-96). Binding order: ../plan.md > this note > `github.md`, `sync-snapshot-store.md`, `personal.md`.

Owner, 2026-11-08: the wizard tries to install a GitHub App per organisation; the accounts it gets installed on are kept fresh by
events, the others by polling, decided per account and per subject. Deployed instances must not be affected.

## What exists

- Every job is events plus a safety-net poll: `Spec.On` names the events that run it at once, `Spec.TTL` is the poll (pull requests
  and issues 15 minutes, commits 2 hours, releases 6 hours, the listing 5 minutes, which turns a changed `pushed_at` into a push).
- An account belongs to one source; a source has one credential (App or token). Mixed per-account credentials are two sources.
- `internal/webhook.CatchUp` reads an App's delivery log (`GET /app/hook/deliveries`, a read with the App's JWT) and passes on what
  was not answered with a success. GitHub logs a delivery whatever happens to it, so an App whose webhook URL nobody can reach still
  gives events, two minutes late. This is what makes events possible for a portal at home.

## The three steps

- **EV1: catch-up without a receiver** (`webhook.catch_up`, default off; needs a source with an App). `cmd/scp/serve.go` builds the
  inbox and the catch-ups when there is a secret or `catch_up`, the receiver route only with a secret. Status says which run.
- **EV2: a mode per source and a cadence per subject.** A source is in *events* mode when it is an App and the portal has a
  receiver or catch-up, else *poll*. Specs may carry `PollTTL`, the poll of a subject that is only a net when events flow; the
  engine uses it for the repositories of a poll-mode source (releases 6 hours becomes 30 minutes; the others already poll fast
  enough: pull requests and issues 15 minutes, commits through the listing's push events). The Admin status lists per source its mode
  and each subject's cadence ("events, net 6 h" or "polling, 30 min"). A deployment with a receiver or catch-up is unchanged.
- **EV3: the wizard's Connect step** (built; `design/personal.md`): the "Events" step between the accounts and the products.
  - Flow: `POST /setup/api/app/start` gives the manifest (`ghlogin.AppManifest`: private, read permissions and events from
    `prereq.Needs`, webhook URL and redirect URL on the setup address) and the address on the host (`/settings/apps/new` for the
    person's own account, `/organizations/<org>/settings/apps/new`); the page posts the form there (CSP `form-action` names the host
    only after sign-in). GitHub returns to `/setup/app/callback?code&state`; the cookie (`SameSite=Strict`) does not travel on that
    cross-site redirect, so the one-use state is the proof, the code is exchanged (`ghlogin.ConvertManifest`, the one POST) and the
    callback page hands back to `/setup/` by script. `POST /setup/api/app/check` lists the App's installations with an App JWT
    (`ghlogin.Installations`, RS256 signed in the package, no new dependency); `app/forget` drops an App.
  - Result: the installed Apps' keys (`Result.Apps`) are sealed in the vault (`app-<account>`, kind `app-key`); `personal.yaml` gets a
    source `app-<account>` per App (`app: {id, credential: true}`), the token source `github` for the other accounts, and
    `webhook: {catch_up: true}` when there is an App. `app.credential` is `personal`'s like `credential`: `scp serve` refuses it.
  - Acceptance: setup tests against a fake host (manifest: read only, host-only form target, state once, no cookie needed on the
    callback, conversion, installations signed by the App, key never in a page body, installed Apps only in the result); a `cmd/scp`
    test that the written file loads, `serve` refuses the vault key, `resolve` fills it and the portal reads the App's account with
    its delivery log; the Playwright walk makes the App in a real browser against a fake GitHub (CSP, the cross-site return).
  - Not verified against the real GitHub: that it accepts a loopback webhook URL and a loopback redirect for a new App and logs
    deliveries to an unreachable URL; GHE Server's manifest flow by version. If GitHub refuses, the account stays polled and the page
    says what GitHub answered.

## Rules

- No change for a configuration that sets neither `webhook.secret_file` nor `webhook.catch_up` except EV2's faster release poll
  (conditional requests: a 304 is free); a receiver's behaviour is untouched.
- The App is read-only: its permissions and events come from `internal/prereq.Needs`.
- Not verified against a real GitHub (needs an App to be created): whether GitHub accepts a loopback webhook URL on a new App and
  keeps logging deliveries to it; GitHub Enterprise Server's manifest flow and `/app/hook/deliveries` by version.
