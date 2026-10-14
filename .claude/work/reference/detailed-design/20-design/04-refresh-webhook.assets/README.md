# 04-refresh-webhook reference prototype

Throw-away Go module that compile-checks and tests the contracts of `../04-refresh-webhook.md`.
It is a reference for implementers, not production code: `internal/{freshness,job,scm,httpapi}` are stubs. After the
independent verification (2026-10-14) the stubs follow the sibling units: `job.Priority` and `job.Scope` are 1-based
(`01-kernel.md`), `httpapi.Problem` has the Go-only `Header` and `WriteProblem` copies `Retry-After` only from it
(`06-httpapi.md` 4.1).

- `internal/refresh`: engine, planner, scheduler, gate, backoff, metrics (31 tests, including `regress_test.go`).
- `internal/webhook`: receiver, de-dup set, redelivery job (21 tests, 1 fuzz target, including `regress_test.go`).
- `internal/ghverify`: reference signature verification and push/event decoding that `github.Verifier` must match (7 tests, 1 fuzz target).
- `internal/e2e`: cold start of 3,000 items; signed webhook to planner to engine; instrument names; import-matrix check; compile-time assertions (3 tests).
- `testdata/`: `push_two_commits.json` (trimmed 2018 recording), `create_tag.json`, `release_published.json`, `ping.json` (trimmed from
  recorded 2018 organisation-webhook deliveries) and three hand-written minimal shapes (`repository_renamed`, `installation_created`,
  `installation_repositories_added`) to be replaced by real recordings in M0.

Run: `GOFLAGS=-mod=mod go vet ./... && go test -race -timeout 5m -count=10 ./...` (Go 1.27.1; otel metric/trace/sdk-metric v1.47.0).
Verified 2026-10-14 (62 tests, 2 fuzz targets; seven mutation checks, see design section 12.3 item 0).
