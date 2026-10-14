# Design principles (v1)
Status: v1 design note, 2026-10-14. Binding order: ../plan.md > this note > the other notes > ../reference/.

How SCP is built. Every change keeps these; each names how it is checked.

1. **One binary, one composition root.** `cmd/scp` builds every component in one ordered startup and passes dependencies explicitly.
   Check: lint (`gochecknoinits`, `gochecknoglobals`); an integration test boots the real `run()` against the fake GitHub; `goleak`.
2. **Built on demand.** A package, interface, config key or metric arrives with the code that uses it; names say what a value is.
   Check: lint `unused`; `scp config check` prints the effective config from the code.
3. **One strict configuration.** Defaults < file < `SCP_*` env < flags; typed, validated at startup, all problems reported at once.
   Check: unit tests for unknown key, wrong type and precedence; every example config passes `scp config check`.
4. **Secrets are a redacting type**, sent only in headers. Commits store author name and login.
   Check: a test with sentinel secrets scans all log output and errors.
5. **Value-typed identities.** Repositories are `{source, id}`, addressed as `{owner}/{name}`, commits `{repo, sha}`, groups by id; membership is an index and
   output is deterministic. Check: shuffle tests (same input in any order gives the same snapshot).
6. **Typed, anchored group matchers** (exact, prefix, glob, RE2, topic, catch-all); a pattern that matches nothing is reported.
   Check: golden matcher table per kind; fuzz of matcher compile and match.
7. **Track each repository's own default branch.** Push events count for `refs/heads/<default branch>`.
   Check: webhook table test (tags, other branches, names with `/`, renamed default branch).
8. **Handlers read the current immutable snapshot.** Indexes are built at publish time; collections are bounded; a restart serves the last
   good state from SQLite before the first GitHub call. Check: race test with readers during publish; page benchmark at 1,000 repositories;
   restart test.
9. **Freshness is data.** Each item carries fetched-at, validator, status and last error; a failure keeps the last good value marked stale;
   absence ("no README", "too large") is a typed state. Check: fake GitHub outage test; golden test per state.
10. **One GitHub client** (REST) with timeouts and context on every call, typed errors and ETags to stay within budget; `sync` retries
    transient errors and pauses until the rate-limit reset. Check: classification table test; fault injection in the fake GitHub; call-budget test for a
    cold start at 1,000 repositories.
11. **The job queue keeps every job.** Jobs are keyed and coalesced, retries back off with jitter, panics are contained and counted, success
    is recorded on completion. Check: `testing/synctest` unit tests.
12. **Webhooks authenticate first.** Cap the body, verify HMAC-SHA256 on the raw bytes with `hmac.Equal`, then parse; de-duplicate by
    delivery id; events enqueue jobs; reconcile heals a lost event. Check: GitHub's signature vectors; fuzz; outcome table (202/400/401/413/415/503).
13. **Repository content becomes `SafeHTML` only through the sanitiser.** Inputs are size-limited, links resolve against repository and ref,
    rendering happens once per content hash. Check: sanitiser fuzz (idempotent, no script survives); XSS corpus; link-rewrite table.
14. **Badges are rendered locally** from held data with `encoding/xml`. Check: fuzz (well-formed XML for any label); golden SVGs.
15. **Precise HTTP.** Real 404s, RFC 9457 problems with a stable code, `charset=utf-8`, URLs from route builders, wire types from the
    OpenAPI contract. Check: generated strict server and `tsc --noEmit`; an e2e crawl of SPA, docs, `llms.txt` and API links.
16. **A strict browser policy and a small public surface.** CSP, `nosniff`, referrer policy, self-hosted hashed assets; metrics and pprof
    on the admin port. Check: golden header test per response class; Playwright run with zero CSP violations and only self requests.
17. **Health from outcomes.** `/livez` and `/readyz` answer from memory; readiness opens once a snapshot with a catalogue is published (warm
    start or first complete listing) and stays open through a GitHub outage; logs are JSON `slog` with request id, job key and route pattern. Check: readiness state-machine test.
18. **Offline, asserting tests and one CI gate.** Tests run against the fake GitHub and the real `run()`; CI runs vet, lint, `govulncheck`,
    `go test -race`, web build and tests, and an image smoke test; the image ships the binary CI tested; times are UTC with a zone; the SPA
    passes an axe scan. Check: required CI checks.

Background for each principle: `../reference/detailed-design/02-design-guardrails.md`.
