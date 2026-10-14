# Design principles
Status: as built. Binding order: ../plan.md > this note.

How SCP is built. Every change keeps these; each names how it is checked.

1. **One binary, one composition root.** `cmd/scp` builds every component in one ordered startup and passes dependencies explicitly.
   Check: lint (`gochecknoinits`, `gochecknoglobals`); `TestServeEndToEnd` boots the real `run()` against the fake GitHub; `goleak`.
2. **Built on demand.** A package, interface, config key or metric arrives with the code that uses it; names say what a value is.
   Check: lint `unused`; `scp config check` prints the effective config from the code.
3. **One strict configuration.** Defaults < file < `SCP_*` env < flags; typed, validated at startup, all problems reported at once.
   Check: `internal/config` tests for defaults, precedence, refused input and all problems at once.
4. **Secrets are a redacting type**, sent only in headers. Commits store author name and login.
   Check: `TestSecretRedacted`.
5. **Value-typed identities.** Repositories are `{source, id}`, addressed as `{owner}/{name}`, commits `{repo, sha}`, groups by id; membership is an index and
   output is deterministic. Check: `TestPlanProperties` (a shuffled listing gives the same plan).
6. **Typed, anchored group matchers** (exact, prefix, glob, RE2, topic, catch-all); a pattern that matches nothing is reported.
   Check: matcher tables per kind (`TestMatchers`, `TestMatcherRefused`, `TestCompileFindings`).
7. **Track each repository's own default branch.** Push events count for `refs/heads/<default branch>`.
   Check: `TestNormalise` and `FuzzNormalise`.
8. **Handlers read the current immutable snapshot.** Indexes are built at publish time; collections are bounded; a restart serves the last
   good state from SQLite before the first GitHub call. Check: `TestSnapshotsAreImmutable`; `TestLoad1000` (p99 per page at 1,000
   repositories, warm start).
9. **Freshness is data.** Each item carries fetched-at, validator, status and last error; a failure keeps the last good value marked stale;
   absence ("no README", "too large") is a typed state. Check: the outage step of `TestServeEndToEnd`; `TestRenderStates`.
10. **One GitHub client** (REST) with timeouts and context on every call, typed errors and ETags to stay within budget; `sync` retries
    transient errors and pauses until the rate-limit reset. Check: `TestClassify`; fault injection in the fake GitHub; the cold start of
    `TestLoad1000`.
11. **The job queue keeps every job.** Jobs are keyed and coalesced, retries back off with jitter, panics are contained and counted, success
    is recorded on completion. Check: `testing/synctest` unit tests.
12. **Webhooks authenticate first.** Cap the body, verify HMAC-SHA256 on the raw bytes with `hmac.Equal`, then parse; de-duplicate by
    delivery id; events enqueue jobs; reconcile heals a lost event. Check: `TestReceiver` (202/400/401/413/415), `TestDuplicateAndOversize`.
13. **Repository content becomes `SafeHTML` only through the sanitiser.** Inputs are size-limited, links resolve against repository and ref,
    rendering happens once per content hash. Check: `FuzzSanitizeFixpoint`; `TestSanitize`; `TestRender` (links and images).
14. **Badges are rendered locally** from held data with `encoding/xml`. Check: `TestRenderGolden`, `TestRenderHostile`.
15. **Precise HTTP.** Real 404s, RFC 9457 problems with a stable code, `charset=utf-8`, URLs from route builders, wire types from the
    OpenAPI contract. Check: generated strict server and `tsc`; the Playwright smoke and SEO runs (JavaScript off) over every page.
16. **A strict browser policy and a small public surface.** CSP, `nosniff`, referrer policy, self-hosted hashed assets; metrics and pprof
    on the admin port. Check: `TestHeaders`; every Playwright test fails on a CSP violation or console error.
17. **Health from outcomes.** `/livez` and `/readyz` answer from memory; readiness opens once a snapshot with a catalogue is published (warm
    start or first complete listing) and the web UI server answers, and stays open through a GitHub outage; logs are JSON `slog` with
    request id, job key and route pattern. Check: `TestProbes`; `TestServeEndToEnd`.
18. **Offline, asserting tests and one CI gate.** Tests run against the fake GitHub and the real `run()`; CI runs vet, lint, `govulncheck`,
    `go test -race` (with a Hazelcast service), web lint, typecheck, tests and build, Playwright, and an image smoke test; times are UTC
    with a zone; every page passes an axe scan in both themes. Check: the required `gate` job.
