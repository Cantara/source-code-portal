Unit 15 prototype (live, ops, overview). Design: ../15-live-ops-overview.md

Module github.com/cantara/source-code-portal, go 1.27.1.

  go vet ./...
  go test -race ./...
  go test -run xxx -bench . -benchmem ./internal/overview/
  go test -run xxx -fuzz FuzzLine -fuzztime 20s ./internal/overview/

Real code (the deliverable):  internal/live, internal/ops, internal/overview (and their *api stand-ins for generated types).
Stand-ins copied from sibling units so that the code compiles:  internal/scm, internal/freshness, internal/routes,
internal/readmodel (changes_stub.go is the proposed ChangeSet.Slots), internal/httpapi (unit 06 prototype),
internal/platform/logx.  internal/httpapi/codes_unit15.go holds the constants and WithRetry that unit 06 must export (R-06-1, R-06-2).

Verification (2026-10-14, independent): 65 top-level tests pass under -race (live 9, ops 20, overview 36; plus the FuzzLine seed run and 6 benchmarks); each package runs goleak.
Verifier additions: ops/verify_test.go, TestHeadOnEventsOpensNoStream, TestDefaultClientKeyIsTheHostNotTheConnection, TestNegotiationFollowsLLMsEnabled.
Non-test, non-comment lines: live 497, ops 1228, overview 2214 (tripwire fails at 2500). See the Verification log at the end of the design document.
