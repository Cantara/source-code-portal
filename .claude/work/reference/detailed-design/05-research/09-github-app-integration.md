# 09 - GitHub App-first, flexible integration (research for D-17)

Verified against the live web on **2026-10-13** (today). Evidence grades used throughout:

- **[V]** verified this session on a primary source (docs.github.com, github.blog, githubstatus.com, proxy.golang.org, git-scm.com, upstream repo files). The source tag `[Sn]` points into the register in section 11, which carries URL and verification date (2026-10-13 for all).
- **[S]** secondary source only (search-result summary, third-party mirror); treat as a lead.
- **[K]** prior knowledge, not re-verified this session; must be re-checked before it is built on.
- **[E]** my estimate or arithmetic derived from [V] numbers.

Caveat on method: the page-fetch tool summarises pages with a small model, so every number below that matters for a hard decision was cross-read against a second page where possible (rate limits against GraphQL page and best-practices page, SBOM deprecation against the changelog, stateless tokens against three changelogs). Anything single-sourced is flagged.

---

## 0. Executive summary (10 lines)

1. **Adopt GitHub App as default, exactly as D-17 says; but implement the client ourselves (about 1,500 lines including tests), not `go-github` + `ghinstallation`.** `go-github` shipped 8 major versions in 5 months (v85 2026-04-20 to v92 2026-09-14) [V:S38]; `ghinstallation` v2.19.0 is pinned to `go-github/v88` and `golang-jwt/jwt/v4` [V:S38]; `go-github-ratelimit` last released 2025-03-12 [V:S38]. A thin typed client (about 14 endpoints) keeps GitHub shapes out of the feature packages (D-17 `scm` model) and RS256 signing is 30 lines of stdlib behind a `crypto.Signer` (so KMS/Vault signers plug in).
2. **Minimal read-only permission set: Metadata:read (mandatory) + Contents:read** covers discovery, README/docs, commits, releases/tags, git trees/blobs and manifests. Optional per feature: Pull requests:read, Issues:read, Commit statuses:read, Checks:read, Actions:read. No organisation permissions, no write anywhere [V:S19, S24].
3. **Rate limits are better than the decision record assumed but not "5,000":** installation tokens get 5,000/h base +50 per repo beyond 20 (+50 per org user beyond 20), **capped at 12,500/h** (cap reached at 170 repos for a small org); GHEC org installations 15,000/h REST, 10,000 GraphQL points [V:S1, S28]. 304 responses do not count against the primary limit when sent with correct `Authorization` [V:S30]. 1,000 repos: cold start about 8,000-10,000 REST calls (fits one hour under the App cap, 1.6-2 h under a 5,000/h PAT), steady state about 600 charged calls/h with webhooks healthy (section 9).
4. **Webhooks are accelerators, reconcile is the correctness mechanism.** Ordering is undocumented, there is no automatic redelivery, the redelivery window is **3 days** on github.com (7 on GHES) and GitHub itself lost 2.4M delivery records for 20 minutes on 2026-07-14 (unrecoverable via redelivery) [V:S10, S11, S15, S17]. Design: verify, dedupe, enqueue idempotent "refresh X" jobs; at start-up and every few hours call `GET /app/hook/deliveries` + redeliver failures, and always run a periodic list reconcile.
5. **Two things changed in 2026 that the earlier documents do not know:** (a) installation tokens became **stateless `ghs_` JWT-like strings of about 520 characters** on github.com/GHEC (rollout finished 2026-10-02; GHES unaffected) - treat tokens as opaque, size buffers/redaction accordingly [V:S36]; (b) the **synchronous SBOM endpoint is removed on 2026-11-13** (31 days from today); only the async `generate-report` + `fetch-report/{uuid}` flow survives [V:S20, S21]. REST API version `2026-03-10` exists alongside `2022-11-28` (supported to 2028-03-10) [V:S5].
6. **Discovery:** `GET /installation/repositories` (100/page, 304 supported, **no `type/sort/direction` params**) as primary, reconcile by numeric repo id; event-driven via `installation`, `installation_repositories`, `repository` events (note: `repositories_removed` is empty when selection flips all to selected, so that case needs a full list) [V:S7, S13].
7. **Git mode: use the `git` binary, not go-git.** go-git cannot backfill partial-clone blobs, has no LFS and no protocol v2 in v5, v6 is still beta, and five CVEs were published in 2026 (symlink traversal, ref-name traversal, credential leak on redirect, object-parse differential) [V:S39, S:CVE search]. Git mode is rarely worth it: the Trees API handles 100,000 entries / 7 MB; build it last, behind the same interface.
8. **Security hot spots:** App private key compromise = read access to every installed account until the key is deleted (keys never expire; up to 25 per app so rotate without downtime) [V:S23]; webhook payloads must be treated as untrusted hints (no timestamp in the signature, replays possible); the SSRF guard cannot blanket-block private ranges because GHES lives there, so use per-source host pinning plus same-origin redirect/`Link` policy.
9. **A decision conflict to resolve:** GHEC with data residency has **no public repositories and rejects anonymous requests** [V:S33], so `public_only` yields an empty portal and Q-03's "private requires auth mode" rule needs an explicit operator acknowledgement path (section 10, deviation proposal).
10. **Deliverables in this report:** interface signatures (section 7), YAML for app/PAT/multi-source/GHES (7.4), `scp doctor` spec (8.4), budget arithmetic (9), corrections to earlier assumptions (10), risks and unverified list (11).

---

## 1. GitHub App model today

### 1.1 Registration and the minimal permission set

Registration: owner = user, organisation or enterprise; "private" app (installable only on the owning account) vs "public" app [V:S26]. Recommendation: **private app owned by the organisation being cataloged** (an App owned by a personal account breaks when the person leaves).

Permission matrix (endpoint to permission, all `read`) [V:S19]:

| SCP feature | Endpoint(s) | Permission |
|---|---|---|
| Discovery, repo facts (name, default branch, archived, topics, languages, visibility, `open_issues_count`) | `GET /installation/repositories`, `GET /repos/{o}/{r}`, `/languages`, `/topics` | **Metadata** (mandatory, always read-only) |
| README, docs, arbitrary files | `GET /repos/.../readme`, `/contents/{path}` | **Contents** |
| Commits (latest N, day grouping) | `GET /repos/.../commits` | Contents |
| Releases and tags | `GET /repos/.../releases`, `/releases/latest`, `/tags` | Contents |
| Manifest discovery and fetch (D-15) | `GET /repos/.../git/trees/{ref}`, `/git/blobs/{sha}` | Contents |
| SBOM (async flow) | `GET /repos/.../dependency-graph/sbom/generate-report`, `/fetch-report/{uuid}` | Contents (page says "at least read access to the repository") [V:S20] |
| Open PR count (`open_issues_count` combines issues and PRs, see note) | `GET /repos/.../pulls` | Pull requests (optional) |
| Open issue count without PRs | `GET /repos/.../issues`, GraphQL `issues.totalCount` | Issues (optional) |
| Build status badge from GitHub | `GET /repos/.../commits/{ref}/status`, `/check-runs`, `/actions/runs` | Commit statuses / Checks / Actions (optional, off by default) |
| Webhook delivery list/redeliver | `/app/hook/deliveries*` | none (JWT, app-level) [V:S9] |
| Dependabot/code-scanning alerts | out of v1 (D-15: OSV later) | not requested |

Note: `open_issues_count` in the repository object includes pull requests [K]; if the UI needs "issues" separately, either request Pull requests:read and subtract, or use GraphQL counts. Decide in the badges design.

**Default manifest permission set: `metadata:read`, `contents:read`.** Everything else is behind a config feature flag and the doctor prints exactly which extra permissions each enabled feature needs. Adding permissions later makes every installation owner re-approve; installations keep their old permissions until approved, and `installation.new_permissions_accepted` tells you when [V:S22, S24, S13]. Therefore request the optional permissions at registration only if you are sure; otherwise the pending-approval state must be a first-class status ("permission X not yet accepted for installation Y").

Events to subscribe on the App: `push`, `release`, `create`, `repository` (plus `installation`, `installation_repositories`, `github_app_authorization`, `meta`, which are sent by default and cannot be unsubscribed) [V:S13]. Do **not** subscribe to `issues`, `pull_request`, `check_run`, `status`, `workflow_run` unless a feature needs them; they are high-volume and SCP polls those with conditional requests anyway. (Q-24 lists `push, release, create, repository, ping`; add the three default App events in the handler, see section 10.)

### 1.2 Private keys

- Up to **25 keys per app**; keys **never expire**, must be revoked/deleted manually; you must create the replacement before deleting the last key [V:S23].
- Downloaded as PEM, **PKCS#1 `RSA PRIVATE KEY`** [V:S23]. Key vaults often export PKCS#8 (`PRIVATE KEY`) [K]. Parser: PEM decode, try `x509.ParsePKCS1PrivateKey`, then `ParsePKCS8PrivateKey`, require `*rsa.PrivateKey` with at least 2048 bits, reject encrypted PEM (`Proc-Type: 4,ENCRYPTED`) with a clear error, accept `\n`-escaped single-line env values and base64-wrapped PEM as conveniences.
- Rotation procedure SCP should document and the doctor should support: (1) generate key B in the App settings, (2) deploy `private_keys: [A, B]`-capable config or swap the file and `SIGHUP`/mtime-reload, (3) verify with `scp doctor`, (4) delete key A in GitHub. Because JWTs carry no `kid` that GitHub requires, both keys are valid simultaneously [K]; SCP uses the first loadable key and falls back to the next on `401` (keeps rotation downtime-free).
- GitHub's own guidance: prefer a **key vault configured sign-only**; env var storage is explicitly called weaker [V:S22, S23]. So the signing abstraction is `crypto.Signer`, with `file`, `env`/`*_FILE` implemented in v1 and `kms`/`vault_transit` as later optional signers.

### 1.3 JWT

Requirements [V:S3]: algorithm **RS256**; `iat` set **60 s in the past** (clock drift); `exp` **at most 10 minutes** in the future; `iss` = **client ID (recommended)** or app ID; must be sent as `Authorization: Bearer` (not `token`).

Implementation notes: `exp = now + 9m` is too close to the edge under skew; use **`iat = now-60s`, `exp = now+8m`**. JWTs are only needed to (a) mint installation tokens, (b) call `/app`, `/app/installations*`, `/app/hook/*`; sign a fresh one per use (cheap, no cache needed). On GHES older than the client-ID change, `iss` may need to be the numeric app id [K]; make `app_id` and `client_id` both configurable (prefer client ID on github.com/GHEC).

### 1.4 Installation access tokens

- `POST /app/installations/{id}/access_tokens` with the JWT; token **expires after 1 hour**; optional body `repositories` (names) or `repository_ids` to downscope (**up to 500 repositories**) and `permissions` to downscope; omitted = everything the installation was granted; a token can never exceed the installation grant [V:S2].
- **Format changed in 2026:** newly minted tokens are stateless `ghs_<APPID>_<JWT>`, about **520 characters** (was 40); GitHub's pattern to match both is `ghs_[A-Za-z0-9\.\-_]{36,}`; permissions, scoping, 1-hour expiry and the endpoint are unchanged; existing tokens keep working until expiry; the temporary `X-GitHub-Stateless-S2S-Token` request header is deprecated **2026-11-30**; **GHES is not affected**, GHEC and data residency are [V:S36]. Consequences: never validate token length; DB columns/env/secret stores need at least 520 bytes; proxies must not truncate long `Authorization` headers; log redaction must use the new pattern; **revocation semantics of stateless tokens (`DELETE /installation/token`) are not described in the changelogs [unverified, section 11]**, so do not make a security argument that depends on instant revocation.
- Token cache: GitHub recommends caching installation and user tokens [V:S22]. See 4.2 for the design.
- Rate limit of the mint endpoint itself: not documented [V:S2 silent]; at one mint per installation per hour it is irrelevant.

### 1.5 Installation vs user-to-server tokens

| | Installation token | User-to-server token |
|---|---|---|
| Acts as | the App | the user and the App (intersection of both) |
| Lifetime | 1 h | 8 h (expiring), refresh token 6 months [V:S22] |
| Rate limit | per installation (scaling) | per user, 5,000/h (shared with PATs/OAuth for that user) [V:S1] |
| Use in SCP | **all data acquisition** | future UI sign-in and per-user visibility |

Future use worth recording: `GET /user/installations/{id}/repositories` (with a user token) lists the repositories *this user* can access in the installation [V:S7], which is the clean way to implement per-user private-repo visibility later (Q-03 OIDC extension) without trusting proxy headers. Not v1. Device flow exists but GitHub warns it enables phishing impersonation and should only be used for constrained clients [V:S22].

### 1.6 Manifest flow (one-command onboarding)

Facts [V:S4]: HTTP `POST` of a form field `manifest` (JSON string) to `https://github.com/settings/apps/new` (personal) or `https://github.com/organizations/ORG/settings/apps/new`; optional `state` query param returned unchanged on redirect; GitHub redirects to `redirect_url` with a temporary `code`; `POST /app-manifests/{code}/conversions` returns `id`, `pem`, `webhook_secret` (and in practice `client_id`, `client_secret`, `slug`, `html_url` [K]); **the whole flow must complete within 1 hour**; the conversion endpoint is rate limited; manifest fields: `name`, `url` (required), `hook_attributes{url,active}`, `redirect_url`, `callback_urls` (max 10), `setup_url`, `description`, `public`, `default_events`, `default_permissions`, `request_oauth_on_install`, `setup_on_update`. A GHES 3.22 docs page for the manifest flow exists [V:S44], but it shows github.com URLs, so the host substitution for GHES (`https://HOSTNAME/settings/apps/new`, `https://HOSTNAME/api/v3/app-manifests/{code}/conversions`) is **[K] and must be tested on a real GHES**.

`scp github app manifest` design:
1. Flags: `--host` (default github.com), `--org` (owner org; omit for personal), `--name`, `--webhook-url` (public URL of SCP, or `--no-webhook` to set `hook_attributes.active=false`), `--features` (selects optional permissions), `--out-dir`.
2. Starts a listener on `127.0.0.1:<random>` (never 0.0.0.0), serves a page with an auto-submitting HTML form that POSTs the manifest and `state` (128-bit random, single use) to the target host; `redirect_url=http://127.0.0.1:<port>/callback`.
3. On callback: constant-time check of `state`; exchange `code`; write the PEM with mode `0600` (refuse if the target path is a symlink or group/world-writable dir), write `webhook_secret` to a `0600` file, print App ID/client ID/slug, never print the PEM or secret; print the installation URL (`https://HOST/apps/<slug>/installations/new`) and the exact `scp.yaml` snippet.
4. Headless fallback: `--code <code>` to paste the `code` from the redirect URL when the browser is on another machine (the code is valid for the 1-hour window).
5. Default manifest: `public:false`, `default_permissions:{metadata:read, contents:read}`, `default_events:[push, release, create, repository]`, `request_oauth_on_install:false`. App names must be unique on the host [K]; default `scp-<orgslug>`.
6. Manifest flow does not need and must not receive any token from SCP; the registrant must be an owner of the org for org-owned apps [K].

### 1.7 Installation scope and lifecycle

- `repository_selection`: `all` or `selected` [V:S8]. For a portal that groups by pattern, `all` + `public_only` is operationally easiest; for any non-public content use `selected` (least privilege, and "exactly the repositories the admin picked" is the safest visibility policy). Doctor warns on `all` + `installation_scope` (everything, including newly created private repos, becomes visible to the portal).
- Suspension: `PUT /app/installations/{id}/suspended` blocks the app's **API access and webhook events** for that account; the account owner or the app owner can suspend; an owner-suspended app cannot be unsuspended by the account [V:S8, S25]. `installation` events carry `suspend`/`unsuspend`/`created`/`deleted`/`new_permissions_accepted` [V:S13]. Exact HTTP status for minting a token on a suspended installation is **not documented** in the pages read (expect 403 or 404 [K]); classify both as "installation unavailable" and confirm via `GET /app/installations/{id}` (JWT) which returns `suspended_at`/`suspended_by` [V:S8].
- Uninstall: `DELETE /app/installations/{id}` now returns **202** (was 204) in API version 2026-03-10 [V:S6]; app loses access to that account's resources [V:S25].
- State machine per installation: `active` / `suspended` / `permissions_pending` / `removed` / `unreachable`. Policy: `suspended` keeps last-good data, flagged stale in the snapshot, with a JWT-only probe every 15 min; `removed` is confirmed by a JWT `GET /app/installations/{id}` 404 and then the repositories leave the snapshot (tombstone kept in `/status`). **Mass-removal guard:** if a reconcile would delete more than `max_removal_ratio` (default 0.3) of a source's repos, or go from N>0 to 0, require two consecutive observations before applying (protects against transient 403/404/permission glitches).

---

## 2. Repository discovery

### 2.1 `GET /installation/repositories` vs `GET /orgs/{org}/repos`

| | `/installation/repositories` | `/orgs/{org}/repos` |
|---|---|---|
| Auth | installation token only (not JWT, not PAT) [V:S7] | any token with access |
| Scope | exactly what the admin granted | everything visible to the token in the org |
| Params | **`per_page` (max 100, default 30), `page` only**; no `type`, `sort`, `direction` [V:S7] | `type` (all/public/private/forks/sources/member), `sort` (created/updated/pushed/full_name), `direction`, `per_page` max 100 [V:S7b] |
| Response | `{total_count, repository_selection, repositories[]}`; 200/304/401/403 [V:S7] | array |
| Conditional | 304 is a documented response [V:S7]; ETag header itself not named on the page **[verify with a recorded fixture]** | 304 [K] |
| Fields | repository objects incl. `id`, `node_id`, `name`, `full_name`, `private`/`visibility`, `archived`, `disabled`, `fork`, `default_branch`, `pushed_at`, `topics`, `license` [K; the org endpoint's minimal-repository fields are [V:S7b]] | same minimal set |
| Pitfall | ordering fixed, so cannot sort by `pushed`; payload is large (full repo objects, about 5-8 KB each [E]: 1,000 repos = 5-8 MB per full pass) | needs a token that sees the org |

GitHub caps `per_page` at 100 and silently reduces larger values, so every listing must paginate (GR-26); the cap is confirmed [V:S7]. Pagination: follow the `Link: rel="next"` header; the `last` link may be absent; never construct page URLs [V:S31, S30].

ETag reality: list ETags change whenever any repo in the list changes (`pushed_at`, `updated_at`), so on an active org the 30-minute reconcile will usually be 200s. Budget it as full cost: 10 calls per pass for 1,000 repos = 20/h (negligible). Store per-page validators anyway (free when quiet).

### 2.2 Identity, renames, transfers, archival

- **Key everything by numeric `repository.id` (and `node_id`)**, never `full_name`: GitHub's own App guidance says identify users, repos, orgs by numeric id because handles and slugs change [V:S22]. `repo name` is a display attribute. `catalog` matchers run on the current `name`.
- Rename: `repository.renamed` event; old URLs redirect (docs: assume any request may redirect; on `301` repeat at `Location` and update stored URL) [V:S30]. Handle by id lookup on reconcile; never treat a 301 as an error. Transfer: `repository.transferred`; the installation may lose access (appears as `installation_repositories.removed` or disappears from the list) [V:S13].
- Archived: `archived` flag; keep the repo, mark in the model, stop scheduled refresh beyond a slow cadence (24 h); `unarchived` event re-enables. `disabled` repos: exclude. Forks: policy flag `include_forks` (default false).
- `repository` event actions: `archived, created, deleted, edited, privatized, publicized, renamed, transferred, unarchived` [V:S13]. `privatized` under `public_only` must remove the repo immediately (security-relevant: do not wait for the reconcile).

### 2.3 Event-driven discovery plus reconcile

```
installation.created           -> registry.Add; GET /installation/repositories (full list)
installation.deleted           -> verify by JWT GET /app/installations/{id}; then remove
installation.suspend/unsuspend -> state change; unsuspend -> full reconcile
installation.new_permissions_accepted -> refresh permissions, clear "pending", reconcile
installation_repositories.added   -> add repositories_added (fetch full repo by name/id), refresh jobs
installation_repositories.removed -> remove repositories_removed
   (if repository_selection changed all->selected, repositories_removed is EMPTY [V:S13]) -> full list reconcile
repository.* (renamed, archived, privatized, publicized, deleted, transferred, ...) -> targeted refresh/remove
push/release/create            -> refresh jobs (hints, section 3.4)
every reconcile_every (30 min, jittered): list installation repos, diff by id, apply with removal guard
every 6 h: GET /app/installations (JWT, paginated, `since`) reconcile of installations themselves
```
The `installation.created` payload lists repositories [V:S13] but is not guaranteed complete for large "all" installs [K]; always follow with the paginated list. All these handlers produce the same jobs as the sync engine (D-03) keyed by `(source, installation, repo id, kind)`.

---

## 3. Webhooks

### 3.1 App webhook

One endpoint, one secret, configured on the App (settings or manifest `hook_attributes`). Headers [V:S13]: `X-GitHub-Event`, `X-GitHub-Delivery` (GUID), `X-Hub-Signature-256`, `X-GitHub-Hook-ID`, `X-GitHub-Hook-Installation-Target-ID`, `X-GitHub-Hook-Installation-Target-Type`, `User-Agent: GitHub-Hookshot/...`. `X-Hub-Signature` (HMAC-SHA-1) is **still sent** for compatibility; use only the `-256` header [V:S13]. (For an App the Target-ID is the app id and the Type string is `integration` **[K]**; check against a fixture and then enforce `Target-ID == configured app_id`.)

Validation [V:S14]: HMAC-SHA256 over the **raw body bytes**, hex, prefix `sha256=`, constant-time compare, header absent when no secret configured (so missing header = reject). Official test vector (use it as a unit test): secret `It's a Secret to Everybody`, payload `Hello, World!`, expected `sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17`.

Limits and behaviour:
- Respond **2XX within 10 seconds** or the delivery is a failure; process asynchronously [V:S12, S16].
- Payload cap **25 MB**; larger payloads are not delivered (example: a `create` event for many branches/tags) [V:S13]. `push` has no event if more than 5,000 branches are pushed at once, and `create` is not sent when more than three tags are created at once; `push.commits` is capped at 2,048 [V:S13]. Consequence: a push without commits detail is possible; the manifest-change heuristic must fall back to "refresh tree" when `commits` is truncated or absent.
- **Ordering and delivery guarantees: not documented** [V:S16 silent on both]. Assume at-least-once, unordered, possibly delayed (2026-06-11: delayed up to 62 min at p99, nothing lost [V:S18]) or lost (2026-07-14: about 2.4M delivery records unsaved, not retried, **cannot be redelivered**, about 5% never reached endpoints [V:S17]).
- Allow-listing: GitHub publishes webhook IPs via `GET /meta` (`hooks`) and says to refresh periodically [V:S12, S35]; useful as defence in depth behind a reverse proxy, not as authentication.
- Subscribe to the minimum events [V:S12].

### 3.2 Redelivery API as downtime recovery

- `GET /app/hook/deliveries` (JWT only): `per_page` max 100, `cursor` pagination, filter `status` (`success`/`failure`)... items carry `id, guid, delivered_at, redelivery, duration, status, status_code, event, action, installation_id, repository_id, throttled_at` [V:S9]. (I found no `redelivered` filter parameter; one note I started with was wrong.)
- `POST /app/hook/deliveries/{delivery_id}/attempts` redelivers (202) [V:S9]. `GET /app/hook/config` / `PATCH` read and change URL/content type/secret/insecure_ssl [V:S9].
- Retention: **3 days** on github.com (the UI/docs state "all deliveries from the past 3 days"), **7 days** on GHES docs [V:S15; GHES figure S: search result of GHES 3.6-3.22 docs].
- GitHub's reference recovery script runs every 6 h, looks back since last run (24 h if never run), groups deliveries by GUID because a redelivery **keeps the same GUID**, redelivers one delivery per GUID that has no `status == OK`, and persists the watermark [V:S11].

SCP design (`internal/webhook`, App mode only):
1. On start-up, and every `redelivery.interval` (default 6 h, jittered), if the App JWT works: list deliveries newest-first until `delivered_at < watermark` (or 3 days), group by GUID, redeliver failed GUIDs (cap per run, default 200, rate 1/s), advance the watermark only after the whole window was scanned.
2. If SCP was down longer than the retention window (or the watermark is older than 3 days, or any delivery page is missing), skip redelivery and run a **forced full reconcile** (all sources).
3. Because lost records cannot be redelivered [V:S17], the periodic reconcile (30 min lists, per-repo conditional polls) stays on even with perfect webhooks; webhook health only lowers poll frequency (hot/cold tiers), it never turns polling off.
4. Doctor/status show: last delivery time, 24 h failure count, oldest unredelivered failure, watermark.

### 3.3 Plain org/repo webhooks (PAT mode and GHES system hooks)

Same signature and headers [V:S13, S14]; no `installation` events, no redelivery from SCP (org/repo hooks have delivery lists too [V:S10], but they need `admin:org_hook`/webhooks permission; SCP will not request that by default). Per-repo hooks for 1,000 repos are impractical, so document **one organisation webhook**: URL `https://scp.example/webhooks/github/{source-id}`, content type `application/json`, secret, events `push, release, create, repository` (matches Q-24). PAT-mode recovery after downtime = full reconcile (no JWT, no delivery API). `ping` is answered 204.

### 3.4 Receiver rules (what makes it safe and idempotent)

1. Read at most `max_body` (25 MiB + small margin) bytes; compute HMAC over the raw bytes **before** any JSON parsing; reject missing/malformed header with 401, mismatch with 401 (log the delivery id, never the secret or body).
2. Identify the source by URL path (`/webhooks/github/{source}`; `/webhooks/github` without a source segment maps to the only/default source), then verify with that source's secret; do not "try every secret" (cheap, but it lets a leaked secret of source A forge events into source B).
3. For App sources enforce `Target-ID == app_id` and that `payload.installation.id` is in the registry (unknown installation: `202` and drop, count a metric; it is either not ours or a new installation we will learn on reconcile).
4. **Dedupe on `X-GitHub-Delivery` only after durable enqueue** (Store table with TTL of at least 7 days, longer than the 3-day redelivery window and the GHES 7 days). A redelivery reuses the GUID by design [V:S11, S12], so a dedupe that records the GUID before success would swallow the very redeliveries that recover failures.
5. Webhooks are **authenticated hints, never state**: the handler only enqueues coalesced `refresh(source, repo id, kind)` jobs and applies installation lifecycle changes after re-verifying through the API. This makes replay (a captured valid signed body can be re-sent; no timestamp is covered by the signature [K]), duplication, reordering and delay harmless by construction. Idempotency key: `(delivery GUID)` for the dedupe table, `(source, repo id, kind)` for job coalescing.
6. Return `202` quickly (target under 1 s, hard 5 s); persist before ack if the queue is in memory only.
7. `payload.sender` may be the `ghost` placeholder [V:S13]; never use it for authorisation.

---

## 4. Multi-installation and multi-organisation design

### 4.1 Model

`Source` (config) = `{id, host, auth, discovery policy, acquisition mode}`. A `github_app` source owns **one App identity (app id + key) and N installations** (one per account: org, user or enterprise). `installations.mode`: `pinned` (explicit installation ids), `allow_list` (account logins; ids resolved via `GET /orgs/{org}/installation` / `/users/{u}/installation` with the JWT [V:S37]), or `all` (every installation of the App; only valid when the App is private, doctor warns otherwise). Repository identity in the catalogue is `source/account/repo-name` plus the numeric id (D-13).

### 4.2 Token cache (per installation, per scope)

- Key: `(source, installationID, scopeKey)`, where `scopeKey` is a hash of the sorted permission map and repository id set (empty = full grant). Value: `{token Secret, expiresAt, permissions, repositorySelection}`.
- `singleflight` per key so a thundering herd mints once; refresh when remaining validity is under 5 min (plus up to 60 s jitter); callers ask for `MinValidity` (default 2 min); on a `401` from the API, `Invalidate(rejected)` drops the entry only if it is still the same token (avoids invalidating a freshly minted one), then retry once.
- Mint failure: backoff 5 s to 5 min, installation marked `degraded` after 3 failures; classify 401 (JWT problem: key wrong/clock skew/app deleted) separately from 403/404 (installation suspended/removed) [section 1.7].
- Memory only; tokens are never persisted (1 h life, trivially re-minted). With stateless tokens the cached value is a 520-character string; keep it in the typed `Secret`.
- Downscoped tokens are for **git mode and per-feature isolation**, not for the default REST path (the default grant is already read-only): e.g. git mode mints `contents:read` for one repository id.

### 4.3 Rate-limit state per installation

Keyed by `(source, installationID, resource)` where resource is `core`, `graphql`, `search` (header `x-ratelimit-resource`) [V:S1]. Updated from **every response** (`x-ratelimit-limit/remaining/used/reset`), not from `/rate_limit` polling [V:S1 says the headers are authoritative; `/rate_limit` is free for primary but can cost secondary points]. Limiter behaviour:

- `remaining < reserve` (default max(250, 10% of limit)) admits only `priority=webhook|interactive` jobs; `remaining == 0` pauses the whole `(installation, resource)` until `reset` plus jitter; primary limit responses are `403` or `429` with `x-ratelimit-remaining: 0` [V:S1].
- Secondary limits: if `retry-after` is present wait that long; else if remaining is 0 wait until reset; else wait at least 60 s and then exponential backoff; **"continuing to make requests while rate limited may result in banning of your integration"** [V:S1, S30]. Do not count rate-limit responses toward the circuit breaker.
- Limits to design against [V:S1, S28]: at most **100 concurrent requests** (shared REST and GraphQL), **900 points/minute per REST endpoint** and 2,000/minute GraphQL, **90 s CPU per 60 s** (60 s max for GraphQL), content creation about 80/min (irrelevant: SCP is read-only). GitHub's own advice is to make requests serially rather than concurrently [V:S30]. Default SCP admission: per-installation concurrency 4 (config max 16), global token bucket 8 req/s per host.
- The same structure holds for PAT sources, keyed by credential. **A user's PATs, user-to-server tokens and OAuth tokens share one 5,000/h bucket** [V:S1], so a PAT source competes with that human's own tooling: the doc must say "use a dedicated machine account".

### 4.4 Config and status implications

- Status view per installation: account, selection, repo count, permissions, suspended?, last reconcile, token expiry, rate remaining/reset per resource, webhook last delivery.
- Per-source isolation: separate `http.Transport`, breaker (per host, tripping on 5xx/dial/timeouts only), limiter, token cache, Store key prefix, webhook secret. Multi-source with the same host shares only the process-wide global limiter.

---

## 5. GitHub Enterprise Server and Enterprise Cloud with data residency

| | github.com | GHES | GHEC with data residency (`ghe.com`) |
|---|---|---|---|
| REST base | `https://api.github.com` | `http(s)://HOSTNAME/api/v3` [V:S34] | `https://api.SUBDOMAIN.ghe.com` [V:S33] |
| GraphQL | `https://api.github.com/graphql` | `http(s)://HOSTNAME/api/graphql` [V:S34] | `https://api.SUBDOMAIN.ghe.com/graphql` **[K, derive and verify]** |
| Web / html_url | `https://github.com` | `https://HOSTNAME` | `https://SUBDOMAIN.ghe.com` (web `*.SUBDOMAIN.ghe.com`, `auth.ghe.com`) [V:S33] |
| Upload URL | `uploads.github.com` | `HOSTNAME/api/uploads` [K] | not documented in pages read |
| Public repos / anonymous | yes | yes (if admin allows) | **no public repositories; Enterprise Managed Users; all API requests must be authenticated** [V:S33] |
| Rate limits | 5,000 user; App 5,000-12,500; GHEC org 15,000 | admin configured; defaults not stated on the page read [V:S43] | "apply", values not given [V:S33] |
| Stateless installation tokens | yes (2026) | **not affected** | yes [V:S36] |
| Webhook delivery retention | 3 days | 7 days (docs) | 3 days (assumed same as cloud, **[K]**) |

Design implications:

1. **SCP never uploads** (read-only), so no `upload_url` is needed; do not offer it. Derive `web_url` from `html_url` fields returned by the API instead of building github.com links; do not derive raw-content hosts (`raw.githubusercontent.com` does not exist on GHES/ghe.com the same way [V:S33 notes raw content URLs differ]). **Serve README images/assets through an SCP asset proxy that reads via the contents API** (`Accept: application/vnd.github.raw+json`), which works for private repos, any host, and avoids SSRF through README-controlled URLs. This is a change to D-06's "link/image rewriting to GitHub raw".
2. **Version skew.** Supported GHES today: 3.22 (released 2026-09-08), 3.21, 3.20, 3.19, 3.18; **3.18 closes down 2026-10-14 (tomorrow)**, 3.19 on 2026-12-09 [V:S32]. Recommended SCP floor: GHES 3.19 for the product's first major version, tested with fixtures. REST API versions are decoupled from GHES versions, and a version works only if the GHES release includes it; which GHES releases include `2026-03-10` is **not stated** in the pages read [V:S5, S44 note; unverified]. Hence: **per-source `api_version`, default by host kind: `2026-03-10` on github.com and GHEC-DR (aligned with 03-github-api.md), `2022-11-28` on GHES** (supported until 2028-03-10 [V:S5], present everywhere) until a GHES instance proves `2026-03-10`; always sent explicitly; contract-tested against both; readiness fails with a clear message on 400/410 for the configured version. The adapter must not rely on anything removed or changed in `2026-03-10` [V:S6]: `has_downloads` and `merge_commit_sha` fields (we do not read them), `rate` in `/rate_limit` (read `resources.core`), directory-listing submodule entries now typed `submodule` instead of `file` (handle both), `assignee` singular on issues (use `assignees`).
3. **Feature probing instead of version arithmetic:** async SBOM endpoints (new), `X-GitHub-Enterprise-Version` response header and `/meta` fields **[K]**; treat 404 from a feature endpoint as "unavailable on this host" and degrade (manifest data from tree+blobs only). Dependency graph on GHES must be enabled by a site admin [K]. On ghe.com, SBOM licence fields and org-level dependency insights are empty [V:S33].
4. **Apps on GHES/ghe.com:** App registration on GHES is documented (user/org/enterprise level) [V:S44b]; whether the manifest flow works unchanged on GHES is documented by page existence only [V:S44]. App availability on ghe.com is **not stated in the pages I read [unverified]**; since PAT mode must work everywhere, GHEC-DR is the first place PAT or `token_command` may be the pragmatic fallback.
5. **Network:** Go `net/http` honours `HTTP_PROXY/HTTPS_PROXY/NO_PROXY` via `ProxyFromEnvironment` [K]; add per-source `proxy_url` override. Custom CA: `tls.ca_file` is **appended to the system pool** (distroless/static ships CA certificates [K]); optional mTLS client cert; **no `insecure_skip_verify` option exists** (document "add your CA instead"). For git mode pass `GIT_SSL_CAINFO` and `http.proxy` explicitly (env is scrubbed, section 6.4).
6. **Visibility policy for GHEC-DR and GHES-internal installations:** repositories have `visibility: internal` in addition to public/private [K]. See section 10 for the deviation proposal.

---

## 6. Alternatives and complements to REST polling

### 6.1 Conditional requests

Send `If-None-Match` with the stored `ETag` (or `If-Modified-Since`); **a 304 does not count against the primary rate limit when the request carried a correct `Authorization` header** [V:S30]. 304s still cost secondary points (1 each for GET [V:S1]). Cacheability rules from GitHub [V:S30]: use the same parameters every time (stable sort; `sort=updated` reorders), request only what you need.

Open risk that the GitHub docs do not address: ETags can depend on the authenticated principal (responses vary by `Authorization`), and **installation tokens rotate hourly**. The sibling report `03-github-api.md` (section 2.4) found third-party evidence (a closed GitHub docs issue, a contributor's reply "your analysis is correct", and a conditional-transport library that recomputes ETags) that the ETag **is** derived from the `Authorization` header; I have not reproduced it either (no token available to me this session). **Plan on it being true**: if an ETag minted under token A is not accepted under token B, every hourly App-token rotation (and every restart with persisted ETags) degrades into a full-cost pass, and persisted ETags are useful only for PAT sources, whose token is stable. Mitigation by construction: (a) key stored validators by `(source, installation, URL, Accept)` not by token; (b) store a **content hash** of the parsed value so a 200 with identical content is a cheap "no change" (no downstream churn); (c) gate refresh by `fetched_at + TTL` from the persisted store so restarts inside the TTL make no calls at all; (d) **spike test** with a real installation in the first implementation sprint (two tokens, same URL, check for 304) and record the result in the fixtures notes. Listed as unverified in section 11.

### 6.2 GraphQL batching

Facts [V:S28]: primary limit points/hour: user 5,000; **App installation outside GHEC: 5,000 + 50/repo over 20 + 50/user over 20, cap 12,500**; installation on a GHEC org 10,000; **cost = (sum over connections of the requests needed to fill them, assuming each hits its `first`/`last`) / 100, minimum 1**; every connection needs `first` or `last` in 1..100; **500,000 node cap per call**; **10 s timeout (502/504) and timeouts cost extra points**; secondary: 100 concurrent (shared with REST), 2,000 points/min, CPU 60 s of 90. `rateLimit { cost remaining resetAt }` is queryable. The GraphQL bucket is separate from REST core (`x-ratelimit-resource: graphql`) [V:S1 header definition; separation itself [K]].

When it pays for 1,000 repos:
- Counts (open PRs, open issues) and repo facts in batches: `query($ids:[ID!]!){ nodes(ids:$ids){ ... on Repository { id nameWithOwner isArchived pushedAt defaultBranchRef{name target{... on Commit{oid committedDate}}} pullRequests(states:OPEN){totalCount} issues(states:OPEN){totalCount} latestRelease{tagName publishedAt} } } rateLimit{cost remaining resetAt} }` using the REST list's `node_id`s as **variables** (no string building, no injection). Batch 50 repos per request [E: keep under the 10 s timeout; the maximum `ids` length is not documented in the pages read, 100 is the commonly cited cap **[K]**]. Estimated cost about 2 points per batch of 50 [E] versus 50-150 REST calls; 1,000 repos is 20 queries, about 40 points, replacing about 2,000 REST calls per full pass.
- It cannot use ETags (POST), so polling the same data repeatedly costs real points; fine at about 40 points per pass, but it never gets the free 304.
- README text via `object(expression:"HEAD:README.md")` needs known file names and can return multi-MB `text`; keep README on REST (`/readme` resolves variants, supports conditional requests).
- Tree discovery has no recursive GraphQL equivalent; use REST trees.

Recommendation (revised after reading `03-github-api.md`, which measured the schema and proposes GraphQL as the bulk read path, about 80 GraphQL points for a full 1,000-repo sweep): given that persisted ETags are probably unusable across App-token rotation (6.1), **adopt GraphQL as the bulk read path for listing, repo facts, latest commits, latest release and issue/PR counts** (`nodes(ids:$ids)` batches with variables), and keep REST for README (preferred-README resolution), Trees, blobs, SBOM, webhook-redelivery and anything GraphQL cannot express. REST + conditional requests remains the full fallback when GraphQL is `off` (GHES instances where the schema lags, or operators who prefer it), so both paths implement the same narrow reader interfaces (7.3) and the sync engine does not know which ran. Control: `acquisition.graphql: auto|off|on` (`auto` = on for github.com/GHEC, probe on GHES). Static query strings plus `encoding/json`; no reflection query-builder dependency. Doctor measures `rateLimit{cost}` (or `dryRun` per 03) of the production query and prints it. The budget tables in section 9 are the REST-only worst case; 03-github-api.md section 7 has the GraphQL-hybrid numbers.

### 6.3 Git Data (Trees) API for manifest discovery

`GET /repos/{o}/{r}/git/trees/{sha-or-ref}?recursive=1`: accepts a SHA or a ref name; **max 100,000 entries and 7 MB** when recursive; `truncated: true` means incomplete; remedy is non-recursive, one subtree at a time [V:S29]. Any non-empty value of `recursive` (even `"false"`) turns recursion on [V:S29]: send the parameter only when you want recursion.

Algorithm:
1. Skip if the root tree SHA is unchanged (get it from the cheap commits call or `push.head_commit.tree_id` **[K]**).
2. One recursive call; filter entries by the ecosystem registry's path patterns (D-15) and `max_depth`/`ignore` globs (`node_modules`, `vendor`, `target`, `.git`).
3. If `truncated`: walk non-recursively, only directories that could contain manifests (root, plus configured `scan_dirs`, plus depth-limited descent), with a per-repo call cap (default 60); mark the repo `manifest_scan: partial` in the model (never silent); or use git mode (6.4) when enabled.
4. Fetch blobs by SHA (`GET /repos/{o}/{r}/git/blobs/{sha}` or contents with raw media type); blobs are **immutable by SHA, so cache forever with no conditional request**; unchanged manifests are never re-fetched or re-parsed (D-15). Size cap per manifest (default 1 MiB).
5. Use push payloads (`commits[].added/modified/removed`, all commits not just the head, GR-21) to decide whether to re-scan at all; fall back to re-scan when the list is absent/truncated (2,048 cap) [V:S13].

SBOM as a complement: the **synchronous `GET .../dependency-graph/sbom` is removed 2026-11-13**; replacement is `GET .../sbom/generate-report` (201 with `sbom_url`) then poll `GET .../sbom/fetch-report/{sbom_uuid}` (202 while processing, 302 to a temporary SPDX JSON URL when ready; result cached up to one week; 403/404 possible) [V:S20, S21]. The docs page does not say how to obtain `sbom_uuid` other than `sbom_url` (parse the last path segment, do not assume) [V:S20]. SBOM covers only what GitHub's dependency graph understands, requires the graph to be enabled, and has empty licence data on ghe.com [V:S33]; it is a fallback/enrichment, never the primary source (D-15 stands).

### 6.4 Optional local git mode

**When it is worth it** (otherwise stay on the API): (a) tree truncation (more than 100,000 entries) on monorepos; (b) policies needing many file reads per repo (rule of thumb: more than about 30 blob fetches per repo per refresh, where a blobless clone plus batched checkout is cheaper than N API calls) [E]; (c) a customer who forbids API-heavy polling but allows git traffic; (d) read-only private mirrors. Git smart-HTTP traffic is not counted against the REST primary limit **[K, unverified]**. For the stated target (1,000 repos, README/commits/manifests) the API is sufficient; **git mode is a post-v1 feature**, specified now so the interface stays honest.

**go-git vs git binary** (as of today):

| | go-git v5.19.3 (2026-10-04, Apache-2.0) | go-git v6.0.0-beta.1 (2026-10-04) | `git` binary |
|---|---|---|---|
| Maturity | stable line, backports | **beta**; Go 1.26 | reference implementation |
| Partial clone filter | **partial: fetch with filter works, but it cannot fetch withheld objects on demand, so reading a missing blob fails** [V:S39] | same gap noted in COMPATIBILITY (not re-tested) | full |
| Shallow | partial (`deepen-not`/`deepen-relative` missing) [V:S39] | | full |
| Sparse checkout | supported (example) [V:S39] | | full |
| LFS | **not supported** [V:S39] | | needs git-lfs, which we do not want anyway |
| Protocol | v1 pack protocol only; smart HTTP only (no dumb) [V:S39] | protocol v2 for upload-pack added in alpha.5; dumb HTTP removed [V:S39 releases] | v2 |
| gc/prune/fsck | **no** [V:S39] | | yes |
| 2026 security record | CVE-2026-71556 symlink path traversal (fixed 5.19.2), CVE-2026-71557 ref-name traversal, CVE-2026-45571 `.git` modification (fixed alpha.4), CVE-2026-45022 malformed-object parse differential (fixed 5.19.0), CVE-2026-41506 credential leak across redirects (fixed 5.18.0); v5.19.3 further "contains credentials across HTTP redirects" [S: advisory aggregators; V:S39 release notes] | | git itself has had submodule/hook RCEs (CVE-2024-32002 critical; CVE-2025-48384 fixed in 2.50.1 line) [V:S42 (2024 post); S: NVD search result for 2025] |

**Recommendation: shell out to `git`** with a hardened environment, because (i) go-git's blob-on-demand gap defeats the main benefit (`--filter=blob:none`), (ii) no LFS/gc, (iii) the 2026 CVE stream is about exactly the operations a cache does (checkout, refs, redirects), (iv) the process boundary lets us apply timeouts, resource limits and kill the process group. Pin a **minimum git version at start-up and in doctor: at least 2.50.1** (contains the CVE-2025-48384 fix) **[S: verify against git release notes when implementing]**.

**Safe design (`internal/gitmode`, implements the same `SourceReader` narrow interfaces):**

- Layout: one **bare repository per (source, repo id)** under `cache_dir` (owned by the SCP user, mode 0700, never inside the web root). No working tree except a throwaway sparse worktree when blobs are needed.
- Commands (argv slice, never a shell, `exec.CommandContext`, `Setpgid`, `WaitDelay`): `git init --bare`; `git fetch --depth=1 --filter=blob:none --no-tags --no-recurse-submodules origin <ref>`; `git ls-tree -r -z --long FETCH_HEAD` (tree objects only, no blob download); then fetch only wanted blobs by a **throwaway sparse checkout in cone mode** built from the manifest allow-list (checkout batches missing blobs in one request), or `git cat-file --batch` for a few files **[spike required: confirm batching behaviour of promisor lazy fetch on github.com and GHES]**. Cheap change detection: `git ls-remote origin <ref>` compared with the stored head SHA before any fetch.
- Shallow clones: GitHub's own guidance is **"Never fetch from a shallow clone!"** (server cost) and treeless/blobless trade-offs [V:S41]. So the cache is **stateless per refresh**: re-init and `fetch --depth=1` when the head moved, then delete; bounded disk and no `gc` needed. If history-based features appear later use blobless non-shallow clones and `git maintenance` on a schedule.
- Environment (scrubbed; start from an empty env and add): `HOME=<private empty dir>`, `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_SYSTEM=/dev/null` (and `GIT_CONFIG_NOSYSTEM=1`), `GIT_TERMINAL_PROMPT=0`, `GIT_ASKPASS=` a no-op program, `GIT_ALLOW_PROTOCOL=https` (this forces `protocol.allow=never` with https always) and `GIT_PROTOCOL_FROM_USER=0` [V:S40], `GIT_NO_LAZY_FETCH` unset (lazy fetch is the point; set it only for pure-read commands that must never hit the network) [V:S40], `GIT_LFS_SKIP_SMUDGE=1`, `GIT_TRACE*` unset, `PATH` fixed, `LC_ALL=C`. `-c` flags on every invocation: `core.hooksPath=/dev/null`, `core.fsmonitor=false`, `core.symlinks=false`, `core.protectNTFS=true`, `transfer.fsckObjects=true`, `fetch.fsckObjects=true`, `fetch.recurseSubmodules=false`, `submodule.recurse=false`, `protocol.version=2`, `credential.helper=`, `http.followRedirects=initial`, `http.lowSpeedLimit=1000 http.lowSpeedTime=30`, `gc.auto=0`, `maintenance.auto=false`. These option semantics are standard git behaviour **[K; the git-config page was only partially readable this session, verify each key on the pinned git version in a unit test that asserts `git config --show-origin`]**.
- **`safe.directory`**: values are only honoured from protected scopes (system/global/command line), never from repository config [K]. SCP creates and owns the cache directory, so the check passes without any `safe.directory` entry; **never set `safe.directory=*`**. If the cache is on a volume with a different owner (Kubernetes fsGroup/PVC) fix ownership at deploy time (init container) instead of loosening the check.
- **Credentials**: never in the URL or `.git/config` (`https://x-access-token:TOKEN@...` is documented but stores the token in the remote URL [V:S37]). Pass `Authorization` through `GIT_CONFIG_COUNT/KEY_n/VALUE_n` env (`http.https://HOST/.extraheader = Authorization: Basic base64(x-access-token:TOKEN)`) **[K: git >= 2.31]**, scoped to the configured host; env is readable only by the same uid. Token is a downscoped installation token (`contents:read`, `repository_ids=[id]`) [V:S2, S37]. `GIT_TRACE_REDACT` stays at its default (redacts `Authorization`) [V:S40].
- **Submodules**: never recurse; parse `.gitmodules` with our own tiny parser (report as a dependency edge `git submodule url`, do not fetch). **LFS**: pointer files are read as plain text; no `git-lfs` installed; detect `version https://git-lfs.github.com/spec/v1` and report "LFS pointer, content not fetched".
- Limits: per-command timeout (default 120 s, context + kill the process group), `--filter=blob:limit=1m` as defence against large blobs, total cache budget (`max_cache_bytes`, default 2 GiB) with LRU eviction, per-repo cap checked after fetch (delete and mark `too_large`), concurrency 2, nice/ionice where available; container `readOnlyRootFilesystem` with `cache_dir` an `emptyDir`/volume.
- Doctor checks in 8.4. Tests: a hostile-repository fixture suite (symlinked `.git`, `.gitmodules` with `ext::` and `file://` URLs, hooks in the tree, oversize blob) run against the pinned git in CI.

---

## 7. Go libraries and interface design

### 7.1 Versions today (checked via proxy.golang.org, raw LICENSE files, 2026-10-13) [V:S38]

| Module | Latest | Date | Licence | Notes |
|---|---|---|---|---|
| `github.com/google/go-github/v92` | v92.0.0 | 2026-09-14 | BSD-3-Clause | requires Go 1.26; majors: v85 04-20, v86 05-08, v87 05-18, v88 05-21, v89 07-06, v90 08-04, v91 09-03, v92 09-14 (about one breaking major every 2-4 weeks) |
| `github.com/bradleyfalzon/ghinstallation/v2` | v2.19.0 | 2026-06-11 | Apache-2.0 | go.mod requires **go-github/v88** and **jwt/v4 v4.5.2**; `Transport` embeds `github.InstallationTokenOptions`; sends `Accept: application/vnd.github.v3+json` |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | 2026-01-28 | MIT | healthy; not needed for sign-only |
| `github.com/gofri/go-github-ratelimit/v2` | v2.0.2 | 2025-03-12 | MIT | 19 months without release; designed around go-github |
| `github.com/ShurcooL/githubv4` | pseudo-version 2026-02-09 | 2026-02-09 | MIT | reflection-built queries |
| `github.com/hasura/go-graphql-client` | v0.16.0 | 2026-04-11 | MIT | pre-1.0 |
| `github.com/go-git/go-git/v5` | v5.19.3 | 2026-10-04 | Apache-2.0 | v5.19.3 dropped Go 1.25; `/v6` is `v6.0.0-beta.1` (2026-10-04) |
| Go toolchain | go1.27.2 (current), go1.26.9 | | | `go.mod` floor for SCP: `go 1.26` |

### 7.2 Adopt vs build (decision for D-04/D-17)

| Concern | Decision | Why |
|---|---|---|
| REST client and DTOs | **Build** a thin `internal/github` client with hand-written narrow DTOs for about 14 endpoints (list repos, repo, readme, contents/blob, commits, releases, tree, languages/topics, pulls count, rate_limit, app, installations, token, hook deliveries, SBOM async) | go-github churn and surface; DTOs leak GitHub shapes; breaking-major every few weeks creates Renovate noise for no benefit. CI contract check: a test that every JSON field we decode exists in `github/rest-api-description` (OpenAPI), plus recorded fixtures per host/version |
| App auth (JWT + token mint + cache) | **Build** (about 200 lines): `crypto.Signer` RS256 | ghinstallation drags go-github/v88 + jwt/v4, no ETag/rate/scope-keyed cache, no multi-installation registry |
| JWT library | **Do not depend**; stdlib `crypto/rsa`/`crypto.Signer`, `encoding/base64` | sign-only; fewer deps; KMS signer pluggable. (If verification of GitHub tokens were ever needed: do not; they are opaque [V:S36]) |
| Rate-limit transport | **Build** (about 150 lines) around header state per installation/resource | go-github-ratelimit is stale and per-client, not per-installation |
| ETag/conditional cache | **Build**; validators persisted in `Store` (SQLite) next to parsed values | generic HTTP caches store bodies; we store parsed items + validators + content hash |
| GraphQL | **Build** static query strings + `encoding/json` | batch queries use `nodes(ids:$ids)` with variables, so no dynamic query builder is needed |
| Retry/breaker/limits | stdlib + `golang.org/x/time/rate`, `x/sync/singleflight` + `sony/gobreaker/v2` as already planned in the stack baseline (02-go-stack-currency.md) | |
| Git mode | **`git` binary** via `os/exec` | section 6.4 |
| Webhook verification | stdlib `crypto/hmac`, `crypto/sha256`, `hmac.Equal` | |

### 7.3 Interfaces (consumer-side, `internal/github` and consumers)

Normalised model lives in a tiny shared `scm` package (types only, no behaviour): `SourceID string`, `RepoID int64`, `InstallationID int64`, `RepoRef{Source SourceID; ID RepoID; Owner, Name string}`, `Repository`, `Ref`, `Commit`, `Tree`, `TreeEntry`, `Blob`, `Release`, `Event`.

```go
// package auth (internal/github/auth) ------------------------------------

// Secret is an opaque, redacting string (String/GoString/MarshalJSON/LogValue return "[REDACTED]").
type Secret struct{ /* unexported */ }
func NewSecret(s string) Secret
func (s Secret) Reveal() string // only called at the point of use (header write)

type TokenKind string // "installation", "pat", "oauth_user", "external", "anonymous"

type Permissions map[string]string // "contents" -> "read"

// Scope narrows what a token may do; the zero value means "the full grant".
type Scope struct {
    Installation  scm.InstallationID // 0 for PAT/anonymous/external
    Permissions   Permissions        // nil = all granted
    RepositoryIDs []scm.RepoID       // nil = all granted; max 500 for installation tokens
}
func (s Scope) Key() string // stable hash for cache keys

type Token struct {
    Value     Secret
    Kind      TokenKind
    ExpiresAt time.Time // zero = no expiry
    Permissions Permissions // as reported by the mint response; nil if unknown
    Selection   string      // "all" | "selected" | ""
}

// TokenSource is implemented by github_app, pat, oauth_user, token_command, anonymous.
type TokenSource interface {
    // Token returns a token valid for at least minValidity. Concurrent calls for the same
    // scope are coalesced. Errors are typed (ErrCredential, ErrInstallationUnavailable, ErrClock).
    Token(ctx context.Context, scope Scope, minValidity time.Duration) (Token, error)
    // Reject tells the source that the API refused this exact token (401); the cache drops it
    // only if it is still the current one.
    Reject(scope Scope, rejected Token)
    // Info is safe to log and display: kind, app id/login, key id/fingerprint, never secrets.
    Info() CredentialInfo
}

type CredentialInfo struct {
    Kind        TokenKind
    Identity    string // app slug, user login, "anonymous"
    KeyFingerprint string // sha256 of public key, app mode
    ExpiresAt   time.Time // PAT expiry if known
}

// Signer is what the app TokenSource needs; *rsa.PrivateKey satisfies crypto.Signer, so do KMS/Vault clients.
type AppKeySource interface {
    Signers(ctx context.Context) ([]crypto.Signer, error) // current keys in preference order; reloads on SIGHUP/mtime
}

// package github (internal/github) ----------------------------------------

type InstallationState string // active, suspended, permissions_pending, removed, unreachable

type Installation struct {
    ID         scm.InstallationID
    Account    Account // ID, Login, Type (Organization/User/Enterprise)
    Selection  string  // all | selected
    Permissions auth.Permissions
    Events     []string
    State      InstallationState
    SuspendedAt *time.Time
    UpdatedAt  time.Time
    LastReconcile time.Time
    RepoCount  int
}

// InstallationRegistry is the per-source, concurrency-safe view of installations.
type InstallationRegistry interface {
    List() []Installation
    Get(id scm.InstallationID) (Installation, bool)
    ByAccount(login string) (Installation, bool)
    // Apply folds a (verified) installation / installation_repositories webhook into state.
    Apply(ev scm.Event) (changed []scm.InstallationID)
    // Reconcile calls GET /app/installations (JWT) and, per installation, repository lists on demand.
    Reconcile(ctx context.Context) error
    // Subscribe delivers state changes to the sync engine.
    Subscribe() <-chan InstallationChange
}

type RateSnapshot struct {
    Resource  string // core | graphql | search
    Limit, Remaining, Used int
    Reset     time.Time
    UpdatedAt time.Time
}
type RateState interface { // per (source, installation, resource)
    Snapshot(inst scm.InstallationID, resource string) (RateSnapshot, bool)
    Admit(ctx context.Context, inst scm.InstallationID, resource string, prio Priority) error // blocks or returns ErrRateLimited{ResetAt}
}

// Validators and results (conditional requests are first-class).
type Validators struct{ ETag, LastModified string }
type Result[T any] struct {
    Value       T
    NotModified bool       // true: Value is zero, caller keeps its stored value
    Validators  Validators
    FetchedAt   time.Time
    Rate        RateSnapshot
}

// Typed errors: errors.Is(err, scm.ErrNotFound) etc.
type Error struct {
    Kind       ErrKind // NotFound, Auth, PermissionMissing, RateLimited, Suspended, Transient, Permanent, Canceled
    Status     int
    RetryAfter time.Duration
    ResetAt    time.Time
    RequestID  string // x-github-request-id
    Err        error
}

// package scm or consumers: narrow reader interfaces (each defined by its consumer;
// the github adapter implements all, composed at cmd/scp) -------------------

type RepositoryLister interface {
    ListRepositories(ctx context.Context, prev RepoListState) (Result[RepoList], error)
}
type FileReader interface {
    Readme(ctx context.Context, r scm.RepoRef, ref string, v Validators) (Result[scm.File], error)
    ReadFile(ctx context.Context, r scm.RepoRef, ref, path string, v Validators) (Result[scm.File], error)
    ReadBlob(ctx context.Context, r scm.RepoRef, sha string, maxBytes int64) (scm.Blob, error) // immutable by SHA
}
type TreeLister interface {
    ListTree(ctx context.Context, r scm.RepoRef, ref string, opt TreeOptions) (Result[scm.Tree], error) // Tree.Truncated is explicit
}
type CommitLister interface {
    LatestCommits(ctx context.Context, r scm.RepoRef, ref string, n int, v Validators) (Result[[]scm.Commit], error)
}
type ReleaseReader interface {
    LatestRelease(ctx context.Context, r scm.RepoRef, v Validators) (Result[*scm.Release], error)
}
type CountReader interface { // REST fallback or GraphQL batch, caller does not know which
    Counts(ctx context.Context, repos []scm.RepoRef) (map[scm.RepoID]scm.Counts, error)
}

// The composition-root convenience union (D-17 "SourceReader"):
type SourceReader interface {
    RepositoryLister
    FileReader
    TreeLister
    CommitLister
    ReleaseReader
    CountReader
    Capabilities() Capabilities // graphql, asyncSBOM, internalVisibility, apiVersion, hostKind
}

// Webhook side: verified, source-routed, normalised.
type EventSink interface{ Handle(ctx context.Context, ev scm.Event) error }
```

### 7.4 Config outlines (D-07 conventions: strict, `_FILE`, env `SCP_`)

```yaml
version: 1
github:
  defaults:
    api_version: auto              # auto = 2026-03-10 (github.com, ghec_dr), 2022-11-28 (ghes); per-source override
    http: { timeout: 30s, response_header_timeout: 20s, max_idle_conns_per_host: 16 }
    budget: { reserve: "10%", concurrency: 4, rps: 8 }
    webhook: { max_body: 25MiB, dedupe_ttl: 168h }

  sources:
    # 1) GitHub App on github.com (recommended)
    - id: acme
      host: { kind: github.com }               # api/web/graphql derived
      auth:
        kind: github_app
        client_id: Iv23liExample               # preferred; or app_id: 123456
        private_key: { file: /run/secrets/scp-app.pem }   # or env: SCP_ACME_APP_KEY ; or signer: {kind: kms, ...} later
        installations: { mode: allow_list, accounts: [acme-org] }   # or pinned: [12345678] | all
      webhook:
        secret: { file: /run/secrets/scp-webhook-secret }
        public_url: https://scp.example.com/webhooks/github/acme    # doctor compares with GET /app/hook/config
        redelivery: { enabled: true, interval: 6h, max_per_run: 200 }
      discovery:
        mode: installation                      # installation | org (for PAT) | explicit
        visibility: public_only                 # public_only | installation_scope
        include_archived: true
        include_forks: false
        reconcile_every: 30m
        max_removal_ratio: 0.3
      acquisition: { mode: api, graphql: auto, webhook_assisted: true }   # git: see below

    # 2) Fine-grained PAT (single org; shares the user's 5,000/h)
    - id: partner
      host: { kind: github.com }
      auth: { kind: fine_grained_pat, token: { file: /run/secrets/partner-pat } }
      webhook: { secret: { file: /run/secrets/partner-hook }, public_url: https://scp.example.com/webhooks/github/partner }
      discovery: { mode: org, org: partner-org, visibility: public_only }

    # 3) GitHub Enterprise Server behind an internal CA and proxy
    - id: corp-ghes
      host:
        kind: ghes
        web_url: https://git.corp.example
        api_url: https://git.corp.example/api/v3         # defaults derived from web_url
        graphql_url: https://git.corp.example/api/graphql
      network:
        proxy_url: http://proxy.corp.example:3128         # default: environment
        tls: { ca_file: /etc/scp/corp-ca.pem }
        allow_private_addresses: true                     # this host only; metadata IPs always blocked
        pin_hosts: [git.corp.example]                     # redirects/Link must stay on these hosts
      api_version: "2022-11-28"
      auth: { kind: github_app, app_id: 42, private_key: { file: /run/secrets/ghes-app.pem },
              installations: { mode: pinned, ids: [7] } }
      discovery: { mode: installation, visibility: installation_scope,
                   access_acknowledgement: network_restricted }   # see section 10 (deviation proposal)

    # 4) GHEC with data residency, token from a secret manager
    - id: eu-ghec
      host: { kind: ghec_dr, subdomain: octocorp }      # api.octocorp.ghe.com, octocorp.ghe.com
      auth:
        kind: token_command
        command: ["/usr/local/bin/vault-gh-token", "--role", "scp"]   # absolute path, no shell
        timeout: 10s
        refresh_before: 5m        # stdout JSON: {"token":"...","expires_at":"RFC3339"}
      discovery: { mode: org, org: octocorp-eng, visibility: installation_scope,
                   access_acknowledgement: oidc_proxy }

    # optional git mode (post-v1) per source
    # acquisition:
    #   mode: api
    #   git: { enabled: true, binary: /usr/bin/git, min_version: "2.50.1", cache_dir: /var/cache/scp/git,
    #          max_cache_bytes: 2GiB, timeout: 120s, use_for: [manifest_tree_when_truncated] }
```
Other auth kinds: `oauth_user_token` (token file, optional), `anonymous` (refused unless `dev: true`; refused on ghec_dr).

---

## 8. Security review

### 8.1 Findings and mitigations

| Topic | Risk | Mitigation (binding for design) |
|---|---|---|
| **Secret handling** | PEM, webhook secret, PAT, tokens in env/logs/dumps | `Secret` type everywhere (redacts in `%v`, `%+v`, JSON, slog `LogValuer`); sources: `file`, `env`, `*_FILE`, never CLI flags; key file must be `0400/0600`, doctor fails on world-readable; k8s Secret volumes; `readOnlyRootFilesystem`; pprof/heap only on the admin listener; do not put tokens in URLs, remote URLs or error strings |
| **Least privilege** | over-permissioned App | default `metadata:read` + `contents:read`; optional permissions by feature flag; doctor lists granted-but-unused permissions and fails on any **write** permission or on admin-level ones |
| **Key compromise blast radius** | App private key can mint tokens for **every installation** of the App with the App's full permission set, and **keys never expire** [V:S23] | read-only permission set caps impact to reading what installations grant; single-owner private App (an attacker cannot add installations to your App, but can use existing ones); use `selected` repositories for sensitive orgs; rotate via second key and delete the old one; detection: GitHub's org audit log shows App token creation [K], SCP logs every mint with installation id and key fingerprint; optional sign-only KMS key so the PEM is never on disk [V:S22]; runbook "key leaked": create new key, deploy, delete old, review audit log, `DELETE /installation/token` for tokens in flight (note stateless-token revocation uncertainty [section 11]) |
| **Installation-token leakage** | 1 h bearer, ~520 chars, readable repo content | tokens only in memory and in `Authorization` headers; downscope for git mode; redact patterns `ghs_`, `ghu_`, `gho_`, `ghp_`, `ghr_`, `github_pat_` (GitHub's own regex for installation tokens is `ghs_[A-Za-z0-9\.\-_]{36,}` [V:S36]) in slog `ReplaceAttr`, error wrapping, panic recovery, OTel (no header capture) |
| **Webhook spoofing** | forged events | HMAC-SHA256 on raw body, constant time, header required, 401 otherwise [V:S14]; optional IP allow-list from `GET /meta` hooks as defence in depth [V:S12]; webhooks are hints only (3.4) so even a forged but signed event cannot inject data |
| **Replay** | valid signed body re-sent | idempotent hint model; dedupe on GUID after durable enqueue; rate-limit per source; no state change without re-fetch |
| **Installation confusion / confused deputy** | (a) a public App installed by an unknown org makes SCP index someone else's repos; (b) payload for installation X applied to source Y; (c) cache key collision across sources | private App; `installations.mode: allow_list` default (never `all` by default); `Target-ID == app_id`; `payload.installation.id` must be in the registry; every Store/cache key prefixed `(source, installation)`; a repo id seen under two sources is an error state surfaced in `/status`; tokens are used only for URLs whose owner is the installation account |
| **SSRF via configurable hosts** | `api_url`/`web_url`/`graphql_url`, `token_command`, README-controlled URLs, `Link` headers, redirects | hosts are operator-trusted config, validated at load: `https` required (`http` only with explicit `allow_insecure_http: true` and a doctor warning), no userinfo, no query; dialer resolves and checks IPs: **always block** loopback/link-local/metadata (169.254.169.254, fd00:ec2::254), **block private ranges unless `allow_private_addresses: true` for that source's pinned host list** (GHES is normally private, so a blanket block is not an option); pin: redirects and `Link rel=next` must be same origin as the source (reject otherwise), `Authorization` never sent cross-origin; never follow URLs from API/webhook payloads (`url`, `download_url`, `clone_url`) - build URLs from base + templates + ids; asset proxy fetches through the API, not arbitrary URLs; DNS-rebinding safe because the check runs in `DialContext` on the resolved address |
| **`token_command`** | arbitrary command execution from config | absolute path, argv only, no shell, 10 s timeout, stdout capped at 16 KiB, env scrubbed, stderr logged redacted, never run as root; config files must not be writable by others |
| **Manifest flow** | CSRF/code interception | loopback listener only, random `state`, single use, 1 h expiry [V:S4], PEM written 0600 |
| **Clock skew** | JWT `iat` in the future rejected | `iat=now-60s` [V:S3]; doctor compares to GitHub's `Date` header; fails over 60 s skew, warns over 20 s |
| **Prompt/markup injection from repo content** | README is untrusted | already covered by D-06 (sanitiser); mention that SBOM/manifest fields (names, licences) are untrusted strings in the UI and LLM outputs |

### 8.2 PAT-mode specifics

Fine-grained PATs: single resource owner (user or one org) per token, org approval may be required (pending tokens read public data only), lifetime up to 366 days or none unless policy blocks it, always include read-only access to all public repos, cannot call Checks and Packages APIs, GitHub recommends Apps for org/long-lived integrations [V:S27]. SCP PAT mode requires a fine-grained token with Metadata:read + Contents:read (+ optional Pull requests/Commit statuses read); classic PATs are accepted with a doctor warning that `repo` scope is read/write (broad) [K]. A PAT's expiry should be surfaced in `/status` when GitHub sends it **[K: `github-authentication-token-expiration` response header]**.

### 8.3 Visibility enforcement is a security control

`public_only` filters on repo `visibility`/`private` at ingestion and **again at render time** (defence in depth against a reconcile lag after `privatized`); the startup validation of D-17 (refuse `installation_scope` without access control) must run before the first sync, not after.

### 8.4 `scp doctor` specification

Usage: `scp doctor [--config PATH] [--source ID] [--json] [--offline] [--fix-hints] [--timeout 60s]`. Exit codes: `0` all checks pass or only INFO; `1` any FAIL; `2` WARN only with `--strict`. Never prints secrets; prints fingerprints and lengths only. Output: one table per source, then a summary with remediation lines. `--json` emits `[{id, severity, source, title, detail, remediation}]`.

Checks (id, what, FAIL vs WARN):
1. `cfg.*` config loads strictly, secrets resolvable, key/secret files not group/world-readable (FAIL), `access` consistency with visibility policy (FAIL: `installation_scope` without access control or acknowledgement).
2. `net.dns/tls/proxy`: resolve host, show resolved IPs and SSRF-policy verdict, TLS handshake, certificate expiry (WARN under 14 days), chain trusted with configured CA (FAIL), proxy reachable.
3. `host.version`: GitHub.com/GHEC/DR/GHES detection; for GHES the release from `X-GitHub-Enterprise-Version` **[K]** compared with the supported list (WARN if the release is past its close-down date, e.g. 3.18 after 2026-10-14 [V:S32]); probe `X-GitHub-Api-Version` acceptance for the configured and for `2026-03-10` (INFO).
4. `time.skew`: compare `Date` header with local time (FAIL over 60 s).
5. `app.key`: parse PEM (PKCS#1/#8), RSA bits (FAIL under 2048), fingerprint, key count/rotation hint; sign a JWT and call `GET /app` (FAIL on 401 with the exact GitHub message); print app id, slug, owner, **granted permissions and subscribed events**; diff against permissions required by enabled features (FAIL on missing, WARN on unused, FAIL on any write/admin).
6. `app.hook`: `GET /app/hook/config` (URL equals configured `public_url` else WARN, `content_type` json, `insecure_ssl` `0`); `GET /app/hook/deliveries?per_page=30`: last delivery time, failure ratio in the last 24 h, oldest failed GUID not redelivered (WARN), 3-day retention reminder.
7. `app.installations`: `GET /app/installations` (paginated): account, selection, suspended (WARN), `permissions` vs app permissions (pending approval = WARN `permissions_pending`), events; mismatch with configured allow-list (FAIL if a configured account has no installation, WARN if extra installations exist and mode is `all`).
8. `inst.token`: mint a token per installation: expires_at, length (INFO, expect about 520 on github.com), repository count from `GET /installation/repositories` (total_count), selection, visibility mix; **WARN if private repos exist and policy is `public_only`** (they will be hidden, say so), FAIL if policy is `installation_scope` and `repository_selection=all` without acknowledgement.
9. `inst.read`: per installation pick one repository and exercise each enabled feature endpoint (readme, commits, tree, release, counts, optionally SBOM generate); map `403 Resource not accessible by integration` to "permission X missing".
10. `rate`: `GET /rate_limit` (free for primary [V:S1]): limit/remaining/reset for `core` and `graphql`; verify the scaling rule (expected limit given repo count: 5,000 + 50 x max(0,repos-20) + 50 x max(0,users-20), cap 12,500 [V:S1]); print estimated cold-start requests and ETA (section 9 model) and warn if `estimate > 0.8 x limit`.
11. `conditional`: issue the same GET twice with `If-None-Match` and confirm `304` and unchanged `x-ratelimit-remaining`; optionally repeat across two freshly minted tokens to answer the ETag-across-tokens question (INFO result recorded in `/status`).
12. `webhook.self`: with `--webhook-selftest`, post an HMAC-signed synthetic `ping` to the local receiver (verifies body limits, signature path and routing, not GitHub's side); show path per source.
13. `pat`: for PAT sources, `GET /user` shows login, scopes header for classic (WARN `repo` scope), fine-grained expiry (WARN under 14 days) **[K]**.
14. `git` (if enabled): binary version at least the pinned minimum (FAIL), `GIT_ALLOW_PROTOCOL` effective, hooks disabled (create a repo with a hook and assert it does not run), cache dir ownership/free space/`max_cache_bytes`, `safe.directory` not wildcard.
15. `store`: Store writable, schema version, validators table size.

---

## 9. Rate-limit and request-budget arithmetic (1,000 repositories)

### 9.1 Limits (verified numbers) [V:S1, S28, S30]

| Item | Value |
|---|---|
| Unauthenticated REST | 60/h per IP |
| User (PAT, user-to-server, OAuth tokens, shared) | 5,000/h; GHEC-org-owned App/OAuth for a member: 15,000/h |
| **App installation (non-GHEC)** | **5,000 + 50 per repo beyond 20 + 50 per org user beyond 20, cap 12,500/h** |
| App installation on GHEC org | 15,000/h REST; **10,000 points/h GraphQL** (the two pages disagree on whether the GHEC figure is "installation on GHEC org" or "App owned by GHEC org"; treat as 10,000-15,000 and read the headers) |
| `GITHUB_TOKEN` in Actions | 1,000/h per repo (n/a) |
| Secondary | 100 concurrent; 900 points/min REST endpoint; 2,000/min GraphQL; CPU 90 s per 60 s (60 s GraphQL); GET/HEAD/OPTIONS are 1 point, writes 5 |
| 304 | not charged to primary if authorised; still a secondary point |
| GraphQL | node cap 500,000 per call; 10 s timeout; first/last 1..100 |

Scaling examples [E from V formula]: 20 repos = 5,000; 100 repos (<=20 users) = 9,000; **170 repos = 12,500 (cap)**; 1,000 repos = 12,500; 100 repos in a 50-user org = 10,500.

### 9.2 Per-repository call model (v1 REST data per D-15/Q-09)

| Data | Calls | Notes |
|---|---|---|
| Repo list (all repos) | 10 per full pass | 100/page |
| README | 1 | conditional |
| Latest commits (N=20) | 1 | conditional |
| Latest release | 1 | conditional |
| Tree (recursive) | 1 | skipped when root tree SHA unchanged |
| Manifest blobs | avg 3 | immutable by SHA, never re-fetched |
| Open PR count (`per_page=1` and read `Link last`) | 1 | or GraphQL batch |
| Languages | 1 (optional) | |
| Build status | 1-2 (optional) | |

Core = 8 calls/repo; with optionals 10.

### 9.3 Cold start

- REST only: 1,000 x 8 + 10 = **8,010 calls** (10,010 with optionals).
  - App (cap 12,500/h): fits in one hour; at 8 req/s = **about 17 min** (21 min with optionals). Secondary ceiling 900 points/min = 10 min minimum.
  - PAT (5,000/h): **1.6 h (2.0 h with optionals)**; readiness must gate on "tier-1 data loaded" (README + commits first: 2,000 calls = 24 min), not on everything.
- With GraphQL batches for counts/metadata (v1.1): about 20 queries (about 40 points) replace about 2,000-3,000 REST calls; cold REST drops to about 5,000.
- Warm start: items carry `fetched_at`/validators in SQLite; refresh only items older than their TTL, so a restart inside the TTL makes about 0 calls.

### 9.4 Steady state with healthy webhooks (App, 1,000 repos)

Assumption [E]: 150 pushes/h org-wide, 2 releases/h, 5 repository events/h; hot repos (pushed in the last 30 days) are 30% of the catalogue.

| Source of calls | Requests/h | Charged to primary |
|---|---|---|
| Webhook-triggered refresh (commits 1 + README 0.1 + tree/manifests 0.2 x 4) about 2 per push | about 300 | about 300 |
| Safety-net conditional poll of commits every 15 min (hot) / 60 min (cold) | 300 x 4 + 700 x 1 = 1,900 | 304s free; about 5% are 200: about 100 |
| README + release hourly conditional (all) | 2,000 | about 50 |
| Repo list reconcile every 30 min | 20 | 20 (list ETags rarely 304 on an active org) |
| Webhook redelivery sweep (JWT list, every 6 h) | about 1 | 1 |
| Build status (optional) every 10 min on hot repos | 1,800 | about 100 |
| **Total** | **about 6,000** | **about 600 (5-12% of 5,000-12,500)** |

Secondary load: 6,000 requests/h is 1.7/s average; trivial against 900/min.

### 9.5 Steady state without webhooks (poll-only, Q-09 targets)

Commits every 5 min for all repos = **12,000 requests/h**; free only if ETags validate (about 5% 200s, about 600 charged). If 304s do not validate (the ETag-across-tokens risk in 6.1) this is 12,000 charged calls: **over the 5,000 PAT budget and at the 12,500 App cap**, so poll-only mode must use activity tiers (hot 5 min, warm 15 min, cold 60 min: about 3,100 requests/h) and the status page must say "webhooks not configured: freshness degraded".

### 9.6 Restart with persisted ETags

- Inside TTL: 0 calls (store gating).
- After downtime longer than TTLs: conditional pass of about 8,000 requests spread by jitter over 17-20 min; charged about 0 if validators hold across the new token, **else up to 8,000** (App: fits in the hourly cap; PAT: 1.6 h).
- After downtime above 3 days: webhooks cannot be redelivered; forced reconcile as cold start.

### 9.7 What to measure

Doctor prints the estimate; `/status` exposes per installation `remaining`, `reset`, cost per job kind, 304 ratio. Alert when 304 ratio falls under 70% (indicates the validator problem) or `remaining` stays under the reserve.

---

## 10. Corrections to earlier assumptions (decision record, other 05 reports)

Directory check at the end of this run: `05-research/` contained `01-docs-format-llm.md`, `03-github-api.md`, `05-store-sync-engine.assets/` and this file. I cross-read `03-github-api.md` (same upstream, overlapping facts): the rate-limit, SBOM, go-github-churn, ETag and permission facts agree; the three points where the reports **differ** are resolved here and must be reflected in the architecture: (R1) **bulk read path**: 03 proposes GraphQL as primary; adopted in 6.2; (R2) **API version default**: 03 proposes default `2026-03-10`; I proposed `2022-11-28` everywhere because GHES support is unverified. Resolution: default **per host kind** - `2026-03-10` on github.com and GHEC-DR, `2022-11-28` on GHES until a GHES test instance proves otherwise; always sent explicitly; both contract-tested; (R3) **go-github**: 03 leaves it as a candidate with an own-client alternative (about 600 lines estimate); I recommend the own client for App auth + transport + 14 endpoints, and the own client's DTOs are kept separate from `scm` types either way. `01-docs-format-llm.md` and the store/sync-engine material were not cross-checked (no GitHub-integration overlap beyond what is quoted). Corrections below are against `04-decisions.md` plus these resolutions.

| # | Where | Earlier statement | Correction (evidence) |
|---|---|---|---|
| C1 | earlier library shortlist | Use `google/go-github` (current major) + `bradleyfalzon/ghinstallation` + `gofri/go-github-ratelimit` | Do not adopt: go-github has a major every 2-4 weeks (v92 on 2026-09-14); ghinstallation v2.19.0 is pinned to go-github v88 + jwt v4; ratelimit lib stale since 2025-03 [V:S38]. Build thin client (section 7.2) |
| C2 | Q-09, D-17 text | "5,000 requests/hour per credential" | PAT/user: 5,000 shared across all of the user's tokens; **App installation 5,000-12,500 (scaling, cap 12,500)**; GHEC 15,000 REST/10,000 GraphQL [V:S1, S28]. Plan against 5,000 as the floor, App usually higher |
| C3 | D-17 rate bullet "higher and scaling rate limits" | qualitative | Quantified: +50/repo beyond 20, +50/user beyond 20, cap 12,500 (170 repos reaches the cap) |
| C4 | D-15 acquisition "evaluate the GitHub dependency-graph SBOM endpoint" | implied sync endpoint | **Sync SBOM endpoint removed 2026-11-13**; use async `generate-report`/`fetch-report`; permission Contents:read; GHES availability not verified; empty licences on ghe.com [V:S20, S21, S33] |
| C5 | D-17 auth examples | `Authorization: token` scheme | Use `Authorization: Bearer` for all token kinds and JWTs (JWT requires Bearer) [V:S3, S37]; always send `Accept: application/vnd.github+json`, `X-GitHub-Api-Version`, and a `User-Agent` [K] |
| C6 | all docs: no API version policy | none | New REST version `2026-03-10` exists; default without header is `2022-11-28` (supported until 2028-03-10); each version supported at least 24 months after a successor; unsupported version returns 410 [V:S5]. Add per-source `api_version`, default `2022-11-28` |
| C7 | D-17 token description | "1 h installation tokens" | still true; but tokens are now **stateless about 520 chars** on github.com/GHEC (GHES unaffected), revocation semantics unverified [V:S36]. Add to D-09 redaction and storage rules |
| C8 | Q-24 events | `push, release, create, repository, ping` | Add the App default events `installation`, `installation_repositories`, `github_app_authorization`, `meta` (not subscribable, always sent to Apps) [V:S13] |
| C9 | D-09 "delivery-id dedupe" | unqualified | Dedupe only **after durable enqueue**, TTL at least 7 days; redeliveries reuse the GUID [V:S11, S12]; webhooks are hints (3.4) |
| C10 | D-03/Q-15 "webhooks are accelerators" | correct in spirit | Strengthened by evidence: delivery loss on 2026-07-14 and delays on 2026-06-11 [V:S17, S18]; GitHub has no automatic redelivery [V:S10]; add periodic `GET /app/hook/deliveries` sweep and a forced reconcile after more than 3 days of downtime |
| C11 | D-09 SSRF guard "allow-list" | generic | GHES is private-network by nature; the guard must be per-source host pinning + same-origin redirect/`Link` rule + always-blocked metadata ranges (section 8.1) |
| C12 | D-06 "relative link/image rewriting to GitHub raw/blob" | raw host assumed | raw/asset hosts differ on GHES and ghe.com [V:S33]; use an API-backed asset proxy and `html_url`-derived blob links |
| C13 | Q-03/D-17 visibility `public_only` default | assumed public repos exist everywhere | GHEC with data residency has **no public repositories** and requires authentication on every request [V:S33]; `anonymous` mode is invalid there. **Deviation proposal (for the Deviations section of the architecture doc):** add `access_acknowledgement: network_restricted | oidc_proxy | trusted_proxy_headers` to `discovery`, satisfying Q-03's "private visibility requires access control" when the operator asserts the portal is only reachable by entitled users; doctor prints the assertion; startup refuses `installation_scope` with neither real access mode nor an acknowledgement |
| C14 | D-17 "installation id auto-discovered" | fine | Discovery endpoints: `GET /app/installations` (JWT, 100/page, `since`), `GET /orgs/{org}/installation`, `GET /repos/{o}/{r}/installation`, `GET /users/{u}/installation` [V:S8, S37]; default mode should be `allow_list`, not `all` (section 8.1 confused deputy) |
| C15 | D-17 "`GET /installation/repositories` preferred" | fine | No `type/sort/direction` params; 100/page max; full repo objects; list ETag changes on any repo change; rename-safe keying by numeric id [V:S7, S22] |
| C16 | GR-20 | verify SHA-256 | still right; note `X-Hub-Signature` (SHA-1) is still sent by GitHub, ignore it; official test vector available [V:S13, S14] |
| C17 | D-04 circuit breaker | per upstream | breaker must trip on 5xx/dial/timeouts only, not on 304/404/rate-limit responses; rate-limit has its own per-installation pause (section 4.3) |
| C18 | D-17 git mode "go-git or git binary: research decides" | open | Decision: **git binary**; go-git rejected (section 6.4); post-v1 |
| C19 | Q-09 staleness SLO "commits 5 min" | assumed cheap | Only cheap with working conditional requests or webhooks; add activity tiers (section 9.5) and measure the 304 ratio |
| C20 | 04 D-02/D-04 ("ETag persistence makes restart cheap", GR-25) | persisted ETags make restarts cheap | Probably true only for PAT sources: App installation tokens rotate hourly and the ETag appears credential-bound (03-github-api.md 2.4, third-party evidence). Persist **content identifiers** (default-branch head oid, README blob oid, release id, pushedAt, body hash) in the Store and gate refresh by `fetched_at + TTL`; treat validators as an in-token-lifetime optimisation (agrees with 03-github-api.md section 9 item 5) |
| C21 | Section 9 of this report vs 03-github-api.md section 7 | REST-only budget here | 03's GraphQL-hybrid budget is the planning baseline; section 9 here is the REST worst case and the poll-only warning |

---

## 11. Risks, then Unverified

### Risks

1. **ETag validity across rotating installation tokens** (6.1) could make poll-only mode unaffordable on PATs and hourly rotation costly; mitigated by TTL gating, content hashes, tiers; needs the spike.
2. **Stateless installation tokens**: revocation behaviour unknown; token size breaks naive storage/proxies (520 chars); header `X-GitHub-Stateless-S2S-Token` goes away 2026-11-30 (do not send it at all).
3. **SBOM sync endpoint removal on 2026-11-13**: anyone prototyping with the old endpoint will break in 31 days; async flow has unclear `sbom_uuid` retrieval in the docs.
4. **go-git avoidance means a runtime dependency on a `git` binary** in the image (distroless static has none): git mode needs a different base image or a bundled static git; another reason it is optional and post-v1.
5. **API-version drift**: GHES support for `2026-03-10` unknown; mixing sources on different versions multiplies fixtures. Mitigated by default `2022-11-28` and a contract test per version.
6. **Webhook delivery loss/delay** is real (2026 incidents); correctness depends on reconcile quality and the removal guard; a bug in either leads to stale or wrongly empty portals.
7. **Key custody**: most deployments will mount a PEM file; compromise exposes all installed accounts. Mitigation is process (rotation runbook, KMS signer later), not code.
8. **Permission growth**: adding permissions later forces re-approval by every installation owner; "permissions_pending" must be a visible state or features silently degrade.
9. **GHEC with data residency** is the least documented target for Apps/manifest/webhook retention; may force PAT/`token_command` there.
10. **Single-source reliance on summarised pages**: numbers were cross-checked but the fetch tool summarises; fixtures recorded from real hosts should replace any doubtful value before the design is frozen.

### Unverified (must be checked before building on them)

1. Whether a 304 is returned when `If-None-Match` carries an ETag minted under a **different installation token** (third-party evidence in 03-github-api.md says no; not reproduced by either report). Spike test with a real installation.
2. Whether `DELETE /installation/token` immediately invalidates **stateless** `ghs_` tokens; whether leaked stateless tokens can be revoked before expiry.
3. ETag header presence on `GET /installation/repositories` (page lists 304 but does not name ETag).
4. HTTP status/body when minting a token for a **suspended** installation (403 vs 404) and the exact message for removed installations.
5. Maximum `ids` length for GraphQL `nodes(ids:)` (100 assumed) and whether `totalCount`-only connections still need `first`; GraphQL cost of the proposed batch (use `rateLimit{cost}`).
6. Whether the GraphQL rate-limit bucket is independent of REST core for installation tokens (header semantics suggest so).
7. Which GHES releases include REST API version `2026-03-10`; whether GHES 3.22 has the async SBOM endpoints; `installed_version`/`X-GitHub-Enterprise-Version` and `X-GitHub-Enterprise-Host` header behaviour on GHES.
8. GitHub App availability and manifest-flow behaviour on **GHEC with data residency**; GraphQL URL (`api.SUBDOMAIN.ghe.com/graphql`) and webhook retention there; GHES manifest flow host substitution.
9. `X-GitHub-Hook-Installation-Target-Type` value for Apps (`integration` assumed) and whether `installation` is always present in repo-scoped App payloads (a third-party summary mentioned missing installation info on 2026-06-17; the primary status page I fetched for the adjacent incident did not confirm it).
10. Whether git smart-HTTP requests are exempt from REST primary rate limits; batching behaviour of promisor lazy fetch for explicit blob lists (`git cat-file --batch`, sparse checkout) on github.com/GHES; `GIT_CONFIG_COUNT` extraheader approach on the pinned git version; git minimum version for CVE-2025-48384 (2.50.1 per a search summary, not the git release notes).
11. `GET /app/hook/deliveries` completeness for deliveries lost on 2026-07-14 (documented as not recoverable) and any automatic-redelivery feature (docs describe only user-run scripts).
12. App-owned installation limit/Marketplace constraints and the exact PAT count cap (50 per user in a summary).
13. Behaviour of `open_issues_count` (includes PRs) and the exact repository-object field set returned by `/installation/repositories` versus the minimal-repository schema.
14. `Retry-After`/secondary-limit specifics on GHES with rate limits configured by an admin (defaults not stated [V:S43]).

---

## 12. Source register (all fetched or queried on 2026-10-13)

- S1 REST rate limits: https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api
- S2 Installation access token: https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app
- S3 JWT: https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app
- S4 Manifest flow: https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-from-a-manifest
- S5 API versions: https://docs.github.com/en/rest/about-the-rest-api/api-versions
- S6 Breaking changes 2026-03-10: https://docs.github.com/en/rest/about-the-rest-api/breaking-changes?apiVersion=2026-03-10
- S7 Installations REST: https://docs.github.com/en/rest/apps/installations ; S7b org repos: https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#list-organization-repositories
- S8 Apps REST: https://docs.github.com/en/rest/apps/apps?apiVersion=2026-03-10
- S9 App webhooks REST: https://docs.github.com/en/rest/apps/webhooks
- S10 Failed deliveries: https://docs.github.com/en/webhooks/using-webhooks/handling-failed-webhook-deliveries
- S11 Auto-redelivery script: https://docs.github.com/en/webhooks/using-webhooks/automatically-redelivering-failed-deliveries-for-a-github-app-webhook
- S12 Webhook best practices: https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks
- S13 Events and payloads: https://docs.github.com/en/webhooks/webhook-events-and-payloads
- S14 Validating deliveries: https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries
- S15 Redelivering webhooks (3 days): https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/redelivering-webhooks (GHES 7 days via GHES 3.6-3.22 versions of the same page, search result)
- S16 Handling deliveries: https://docs.github.com/en/webhooks/using-webhooks/handling-webhook-deliveries
- S17 Incident 2026-07-14: https://www.githubstatus.com/incidents/ydpk76bj34z8 ; S18 incident 2026-06-11: https://www.githubstatus.com/incidents/4yz3c18qmdxn
- S19 Permissions per endpoint: https://docs.github.com/en/rest/authentication/permissions-required-for-github-apps (page truncated at 100,000 of 102,085 chars; SBOM and installation endpoints partly outside the read range)
- S20 SBOM REST: https://docs.github.com/en/rest/dependency-graph/sboms?apiVersion=2026-03-10 ; S21 changelog: https://github.blog/changelog/2026-05-12-synchronous-sbom-api-deprecated
- S22 App best practices: https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/best-practices-for-creating-a-github-app
- S23 Private keys: https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/managing-private-keys-for-github-apps
- S24 Choosing permissions: https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app
- S25 Installed apps (suspend/uninstall): https://docs.github.com/en/apps/using-github-apps/reviewing-and-modifying-installed-github-apps
- S26 About GitHub Apps: https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/about-creating-github-apps
- S27 PATs: https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens
- S28 GraphQL limits: https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api
- S29 Trees: https://docs.github.com/en/rest/git/trees
- S30 REST best practices: https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api
- S31 Pagination: https://docs.github.com/en/rest/using-the-rest-api/using-pagination-in-the-rest-api
- S32 GHES releases: https://docs.github.com/en/enterprise-server@latest/admin/all-releases
- S33 GHEC data residency: https://docs.github.com/en/enterprise-cloud@latest/admin/data-residency/about-github-enterprise-cloud-with-data-residency , .../feature-overview-for-github-enterprise-cloud-with-data-residency , .../network-details-for-ghecom
- S34 GHES REST/GraphQL URLs: https://docs.github.com/en/enterprise-server@latest/rest/overview/resources-in-the-rest-api , https://docs.github.com/en/enterprise-server@3.22/graphql/guides/forming-calls-with-graphql
- S35 `/meta`: https://docs.github.com/en/rest/meta/meta?apiVersion=2026-03-10
- S36 Stateless installation tokens: https://github.blog/changelog/2026-04-24-notice-about-upcoming-new-format-for-github-app-installation-tokens/ , https://github.blog/changelog/2026-05-15-github-app-installation-tokens-per-request-override-header/ , https://github.blog/changelog/2026-10-02-stateless-github-app-installation-tokens-rolled-out/
- S37 Authenticating as installation: https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation
- S38 Go module facts: `https://proxy.golang.org/<module>/@latest`, `@v/list`, `@v/<ver>.mod`; LICENSE files on raw.githubusercontent.com (go-github, ghinstallation, jwt, ratelimit, githubv4, go-graphql-client, go-git); `https://go.dev/dl/?mode=json`
- S39 go-git: https://github.com/go-git/go-git/blob/main/COMPATIBILITY.md , https://github.com/go-git/go-git/releases ; CVE list from web-search aggregators (SentinelOne, Mend, GitLab advisory DB, CIRCL) [S]
- S40 git environment variables: https://git-scm.com/docs/git (read to character 100,000; `GIT_CONFIG_COUNT` not in range)
- S41 Partial/shallow clones: https://github.blog/open-source/git/get-up-to-speed-with-partial-clone-and-shallow-clone/
- S42 Git 2024 vulnerabilities: https://github.blog/open-source/git/securing-git-addressing-5-new-vulnerabilities/ (CVE-2025-48384 from NVD via search result [S])
- S43 GHES rate-limit config: https://docs.github.com/en/enterprise-server@3.22/admin/configuring-settings/configuring-user-applications-for-your-enterprise/configuring-rate-limits
- S44 GHES manifest page: https://docs.github.com/en/enterprise-server@3.22/apps/sharing-github-apps/registering-a-github-app-from-a-manifest ; S44b GHES app registration: .../registering-a-github-app/registering-a-github-app
