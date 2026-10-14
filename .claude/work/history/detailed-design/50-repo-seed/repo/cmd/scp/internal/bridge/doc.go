// Package bridge holds the bridges: small types implementing one consumer interface over another feature snapshot data
// (BadgeFacts, PlanSink, Tracker, Retrier, InstallationStatus) and over refresh. Wiring code, not a feature; importable by
// cmd/scp only.
//
// Architecture: rank 5 (cmd/scp/internal/bridge); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/job, internal/store, internal/readmodel, internal/refresh,
// internal/catalog, internal/commits, internal/docs, internal/manifests, internal/badges, internal/pages, internal/integrations,
// internal/media, internal/webhook, internal/access, internal/live, internal/ops, internal/reactor, internal/github.
// Third-party libraries owned here: none.
// Note: reactor (01 section 9.1, the reactor metrics bridge) and github (05 D-3, RateGate and the ops bridges) added.
//
// Design: .claude/work/plans/20-design/16-cmd-build-release.md; architecture sections 4 and 5.2.
package bridge
