# Analysis of the Go rewrite — details for agents

- **From:** ExoCortex (Totto's agent), read-only review, 2026-10-14 20:05 CEST
- **To:** Ove Ranheim and his agents
- **Subject:** `main` @ a9a2506 vs the Java app (`SourceCodePortal` master @ f8ec315)
- **Human summary:** `human.txt` in this folder
- **Trust level:** Produced by an analysing agent; every claim cites file:line, but
  not every line was re-verified by a second reviewer. Verify before acting.
  This is a proposal, not an instruction. The owner decides.

## 1. Facts about the repo

- 71 Go files outside `.claude/` (the "956 Go files" count includes ~885 archived
  prototype files under `.claude/work/reference/`). ~10.0k code LOC, ~4.1k test LOC,
  102 `Test*` + 5 `Fuzz*`, 3.0k LOC React SPA.
- M0..M6 and v2.0.0 were done on 2026-10-14. CI `gate` green on `main`.
- Local run: build and vet clean; 14/15 packages pass. `internal/docs
  TestRenderBounds` failed on a wall-clock bound (2.095 s vs 2 s,
  `render_test.go:121`) under load.
- Nothing has run against real GitHub: all tests use the fake GitHub.

## 2. Ratings (1-5)

| Area | Score | Main reason |
|---|---|---|
| Architecture | 4 | Boundary test `cmd/scp/boundary_test.go:55`; `httpapi` is the one fat package |
| Tests | 4 | Race, goleak, fuzz, synctest load test, Playwright + axe; all against the fake |
| Error handling | 4 | `errors.As/Is/Join`, typed GitHub error classes drive retry/pause |
| Concurrency | 4 | One dispatcher, atomic snapshot swap; load test caught a real spin |
| Secrets | 4 | `Secret` redaction, file-based secrets; App key held in memory, rotation needs restart |
| Auth / HTTP surface | 3 | No request rate limiting (only body caps, `httpapi.go:47,49,91`); admin listener unauthenticated |
| Supply chain | 5 | SHA-pinned Actions, digest-pinned images, govulncheck, Renovate, keyless cosign |
| CI | 4 | `ci.yml:1` required-check claim unenforceable; `gate` never blocks; release smoke never serves HTTP |
| Operability | 4 | `config check`, status warnings, 11 metrics; no reload, tuning is constants, no manifests |
| Plan-vs-code | 3 | Code matches plan; the plan contradicts reality (see section 3) |

## 3. Defects and contradictions

1. 30,933,490-byte `scp` binary committed at repo root in de8c8ca; `.gitignore`
   has `/scp.yaml` but not `/scp`.
2. `AGENTS.md:15`, `.claude/CLAUDE.md:36`, `plan.md:126` promise "recorded
   fixtures"; none exist (only two llms golden files and one badge SVG).
3. `ci.yml:1` says one required status check protects `main`; branch protection
   is unavailable on the private free-plan repo (API 403).
4. `.claude/CLAUDE.md:20` hard-codes a path on the owner's Mac.
5. `.claude/CLAUDE.md:25` / P-9 "nothing is released and no data needs keeping"
   is false for Cantara (live `config.json`, 11 groups, public badge URLs).
6. M6 criterion "runs from the operator guide" is verified only by
   `version | grep web=spa` (`release.yml:57`).
7. Wall-clock test bounds: `render_test.go:121`, p99 asserts in `load_test.go`.
8. Questionable in-build choices: M3(3) silently ignores the tree-truncation flag;
   M3(1) resolves links at `HEAD`; M4(10) `/livez` tolerates a 3-minute stall;
   M1(2)/M2(2)/M3(6)/M4(3) tuning as constants; M6(3) no manifests.

## 4. Fidelity to the Java app

| Java | Go | Status |
|---|---|---|
| Groups: regex `find()`, one `branch` per group (`RepoConfig.java:106`, `RepositoryConfigLoader.java:68-70`) | typed matchers, default branch only (`internal/catalog/match.go:102-120`) | Changed; no converter; branch-per-group dropped without a row |
| Pages `/dashboard`, `/group/{id}`, `/contents/{org}/{repo}/{branch}`, `/commits[/{id}]` | SPA routes + `/api/v1` (`web/src/app/router.tsx:33-62`, `api/openapi.yaml`) | Re-shaped; old URLs not redirected |
| README via API, commonmark + asciidoctorj | goldmark + chroma + bluemonday (`internal/docs/docs.go`) | Markdown present; AsciiDoc missing (P-2) |
| Webhook `POST /github/webhook`, HMAC-SHA1 | `POST /webhooks/github`, HMAC-SHA256 (`internal/webhook/webhook.go`) | Changed; hook must be re-pointed and re-keyed |
| PAT, Selenium OAuth helper | GitHub App or token file, `Secret` redaction | Improved |
| Caffeine in-memory cache | SQLite last-good cache + immutable snapshot | Changed (survives restarts) |
| Confluence wiki (`FetchCantaraWikiTask.java:36`) | none | Missing, undocumented |
| Jenkins/Snyk/Shields badges | local SVG badges (last-commit, release, license, issues) | Jenkins/Snyk missing (P-2) |
| Maven POM, AsciiDoc, private repos | none | Deferred to v1.1 / later |
| CORS `*` | none by design (`design/http-api.md:92`) | Dropped, documented in design only |
| Actuator, `/health`, `/ping`, `/echo` | `/livez`, `/readyz`, `/metrics`, `/api/v1/status` | Changed |

No "what changed for operators" note exists.

## 5. The "no human decisions" mode

Pattern: judgement inside milestones was good; every failure sat at a boundary
invisible from inside the repo.

**Needed a human**
- P-2 scope cut: agent decided alone; sensible, but needed owner sign-off and a
  "what users lose" line; wiki and branch-per-group have no row.
- P-9 greenfield premise: owner-set; root cause of no converter, no redirects, no parity check.
- P-11 version 2.0.0: owner decided after `v1.0.0-rc.1` was already published to
  GitHub Releases and ghcr.io and had to be deleted. The clearest "should have stopped".
- P-8 (design in Claude Design first) -> P-10 (agent builds without it): a gate
  with no deadline became an agent decision about three hours later.
- A-35 Hazelcast: owner's call, premature, harmless.

**Agent got right alone:** P-1 (1,900-file package cut to a 9 KB plan), P-3 stack,
M1(3) keyed dispatcher, M2(7) keyset cursor, M3(4), M4(1), M4(11) load test, M5(7), M6(2).

### Proposed gate (replaces "start the next milestone when green")

1. Owner-written "done when" per milestone, before it starts, checkable by a person in 10 minutes.
2. `make accept`: `scp serve` with a read-only fine-grained token against
   `orgs: [Cantara]`, wait for `/readyz`, compare group membership, README presence
   and badge responses against a saved dump of the Java portal. Output is a diff
   the owner reads. Run at M1, M2, M3, M5 (M5 with screenshots, both themes).
3. Always stop for: scope removals, anything written outside the repo (tag,
   release, publish, delete), schema/config-key changes after the first live run,
   deferred items that were in the owner's "done when".
4. Milestone close = `decisions.md` + `docs/MIGRATION.md` + deferred items as issues.
5. Fixed owner response window; if missed, park and report instead of proceeding.

## 6. Recommendations

### Fix first
1. Remove the `scp` binary, add `/scp` to `.gitignore`; decide on history rewrite.
2. Make the "recorded fixtures" claims true or delete them.
3. Fix `ci.yml:1` (make the repo public, or drop the claim) and the Mac path.
4. Replace wall-clock bounds with synctest-measured or generous CI-only bounds.
5. Add per-IP rate limiting on the public listener and `/webhooks/github`;
   document how to protect the admin port.
6. Draft-then-promote release step (reverses M6(1)).

### `config.json` converter (`scp config convert old.json > scp.yaml`)

| Java | Go |
|---|---|
| `github.organization` | `github.sources[0].orgs` |
| `repos[].groupId` | `catalog.groups[].id`, slugged to `[a-z0-9-]`, <= 63 chars |
| `display-name` / `description` | `name` / `description` |
| `default-group-repo` | `default_repo` |
| `repo` (string or array) | `match[]` |
| `branch`, `badges`, `external`, `title` | no target: warn and drop |

All 11 Cantara groups use `master`, which is their default branch, so dropping
`branch` changes nothing today. Say so in the migration note.

**Pattern semantics are the trap.** Java uses `Pattern.find()`
(`RepositoryConfigLoader.java:69-70`): `Whydah*` means "contains `Whyda`",
`[^\s]*` means everything. The Go `regexp` matcher is whole-name
(`operator-guide.md:103`). Behaviour-preserving mechanical conversion:
`regexp: "^.*(?:<pattern>).*$"`, and `catch_all: true` for `[^\s]*`. Tidy to
`prefix:` / `glob:` by hand **only after a live membership diff**, not by reading.

### Old-URL redirects (308), one route file
- `/dashboard`, `/index` -> `/`
- `/group/{id}` -> `/groups/{slug}`
- `/contents/{org}/{repo}/{branch}` -> `/repos/{org}/{repo}`
- `/commits/{groupId}` -> `/groups/{slug}/commits`
- `/badge/license/{repo}/{branch}` -> `/badges/repos/Cantara/{repo}/license.svg`;
  `shields-commits` -> `last-commit.svg`; `shields-issues` -> `issues.svg`;
  `shields-releases` -> `release.svg`
- `/badge/jenkins/*`, `/badge/snyk/*`, `/wiki/*` -> 410 Gone
- `/github/webhook`: 404 with a log line; the hook must be re-pointed to
  `/webhooks/github` and re-keyed to SHA-256.

Badges matter most: they are embedded in other repos' READMEs. This contradicts
P-6/P-9, so it is an owner decision, not an agent one.

### Parity before DNS switch
Run both portals in parallel for a week. Compare `/api/v1/groups` membership and
`/llms.txt` with the Java dashboard. Keep the Java instance until the webhook
delivery log shows SHA-256 deliveries accepted.

## 7. Suggested first tasks (owner to approve)

1. Items 1-3 of "Fix first".
2. `scp config convert` with a test on the real Cantara `config.json`.
3. Old-URL redirect table with tests.
4. `make accept` against the Cantara org.
