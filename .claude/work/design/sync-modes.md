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
    (`ghlogin.Installations`; the App's JWT is signed by `ghinstallation`, the exchange and the listing are go-github's); `app/forget` drops an App.
  - Result: the installed Apps' keys (`Result.Apps`) are sealed in the vault (`app-<account>`, kind `app-key`); `personal.yaml` gets a
    source `app-<account>` per App (`app: {id, credential: true}`), the token source `github` for the other accounts, and
    `webhook: {catch_up: true}` when there is an App. `app.credential` is `personal`'s like `credential`: `scp serve` refuses it.
  - Acceptance: setup tests against a fake host (manifest: read only, host-only form target, state once, no cookie needed on the
    callback, conversion, installations signed by the App, key never in a page body, installed Apps only in the result); a `cmd/scp`
    test that the written file loads, `serve` refuses the vault key, `resolve` fills it and the portal reads the App's account with
    its delivery log; the Playwright walk makes the App in a real browser against a fake GitHub (CSP, the cross-site return).
  - Checked against GitHub's documentation (registering a GitHub App from a manifest): `url` and `hook_attributes.url` are required,
    `redirect_url` gets the one-hour `code` and the `state` back, `hook_attributes.active` defaults to true, and the flow is
    documented for Enterprise Server too (3.0 onward). Not verified against a real host: that it accepts a loopback webhook and
    redirect URL for a new App and logs deliveries to an unreachable URL. If it refuses, the account stays polled and the page says
    what GitHub answered.

## Rules

- No change for a configuration that sets neither `webhook.secret_file` nor `webhook.catch_up` except EV2's faster release poll
  (conditional requests: a 304 is free); a receiver's behaviour is untouched.
- The App is read-only: its permissions and events come from `internal/prereq.Needs`.
- Not verified against a real GitHub (needs an App to be created): whether GitHub accepts a loopback webhook URL on a new App and
  keeps logging deliveries to it; GitHub Enterprise Server's manifest flow and `/app/hook/deliveries` by version.

## Durability (EV4, P-103)

What the portal guarantees of events, as built and as limited. The goal is that no change GitHub tells the portal about is missed
for good: an event is either acted on, or the poll that backs its subject repairs it, and an operator can see which.

- **Received.** The receiver writes each verified event to the store, synced to disk, before it answers 202; a write that fails is
  answered 503 and the delivery id is forgotten, so GitHub's resend is not a duplicate. A parked event is passed on again at every
  start until `Engine.Handled` says every job that listens for it has run since, or 72 hours have passed.
- **Sent while the portal could not take it.** An App's delivery log (`GET /app/hook/deliveries`) is read every 2 minutes from five
  minutes before the last complete read; every delivery without a 2xx answer is passed on. A read of 100 pages (10,000 deliveries)
  that still does not reach the last complete read is `ErrLogTruncated`: the newest deliveries are passed on, the cursor moves on
  (older ones cannot be reached, GitHub lists newest first), and the status (`webhook.catchUps[].truncated`, a warning for a day), the
  log (a warning) and `scp_webhook_deliveries_total{event="log",result="truncated"}` say so. A log that cannot be read at all keeps
  the cursor, is a warning in the log at every try (the wait grows to an hour) and a status warning once it has gone 10 minutes
  without a complete read.
- **Removal.** A `repository` event (`deleted`, `transferred`, `renamed`, `privatized`...) starts a listing of the account at once; a
  repository is removed after two complete listings in a row miss it, the second 30 seconds after the first. The count is kept with
  the listing's state, so a restart never resets it, and a partial listing never counts. A verified `deleted` delivery does not
  remove at once: one wrong or partial answer must not drop data.
- **Limits, by design.** A source with a token and an organisation or repository webhook has no delivery log: a delivery sent while
  the portal is down is lost, and the polls repair it (pull requests and issues 15 minutes, the listing 5 minutes, releases 6 hours,
  commits 2 hours). Pushes to branches other than the default, deleted tags and branches, and the event of a repository the portal
  does not yet know are not acted on by themselves; the commits poll and the listing pick them up. Not verified against a real
  GitHub: that deliveries to an unreachable URL are logged with no success status and returned by the log, on github.com and on
  Enterprise Server.
