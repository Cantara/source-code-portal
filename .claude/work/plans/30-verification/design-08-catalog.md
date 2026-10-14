# Verification report: design unit 08-catalog (2026-10-14)

Verifier: independent (not the author). Document verified and edited: `20-design/08-catalog.md` (companion `08-catalog.assets/` also corrected: `mod/`, `out/`, `README.md`, both copies of `catalog.schema.json`). Scratch module: the session scratchpad `verify-08-catalog/` (`proto` = original prototype, `real` = prototype ported to the real kernel; the assets `mod/` is a copy of `real`). The original document is kept at `.../verify-08-catalog/08-catalog.md.orig`. The document's own log is its new section 16; ids below are the same V-nn.

## Verdict

**ready_with_caveats.** The design is complete (sections 1-15 present and concrete, section 16 added), the algorithms are sound, and after the fixes the prototype compiles and passes `go vet`, `gofmt -l` (empty) and `go test -race -count=1 ./...` over the whole module **against the real kernel, readmodel and store packages**, where the original prototype had only reduced stubs. The stubs hid 20 defects: five high-severity ones (slug case, store key, hide-versus-commit race, stale freshness, warm start ignoring the visibility policy) plus a class-blind membership that made the confidentiality rule depend on every handler. All are fixed in document and code with regression tests. Caveats: the HTTP handlers and `Mount` are specified but not built (they need `catalogapi` from the `apispec` unit), T-8/T-9/T-15 are not implemented, and two policy questions remain with the owner.

## Checks

(a) **Architecture conformance.** Package name `internal/catalog` (rank 4) matches architecture 4/5.2. Non-test imports checked with `go list -deps`: non-test `catalog` imports exactly `scm`, `freshness`, `job`, `readmodel`, `store` and `httpapi` (all lower rank or allowed by the matrix of 5.2; events leave through the consumer-defined `EventSink`, so `reactor` is not imported). Consumer-defined interfaces of 2.2 compile against the real kernel. Found: the document contradicted the kernel on the slug grammar (V-01), on `NewMembership` (V-13, D-2), on class-aware membership (V-12, D-7 withdrawn), on the zero visibility (V-14, D-8) and on `store.KeyOf` (V-02). All corrected; D-15, D-16, D-18 proposed (default_repo is a name, `catalog.sync` on failure, `Tracker` under the publish lock); D-2, D-7, D-8, D-9 withdrawn or resolved.

(b) **Completeness.** All 15 sections present and concrete; each open question has a default. Added section 16. The trace table lacked relevant rows (V-19), now present with mechanisms and tests: the site metadata, startup refresh and refresh-cadence features, and GR-01, GR-02, GR-07, GR-12, GR-14, GR-16, GR-26, GR-28.

(c) **Guardrail traceability.** The catalogue's guardrails were each checked for a mechanism and a test: listings are paginated and reconciled with a mass-removal guard (GR-26), matchers are typed and anchored (GR-09), the default repository is a configured repository name matched case-insensitively and must be a member (V-11), the default branch is resolved from the repository unless `ref` is set (GR-10), every repository has its own identity and group membership is an index (GR-07), and group ids are matched exactly in their canonical lower-case form (GR-08).

(d) **Technical correctness.** Compiled and ran the original prototype (green on its own stubs), then ported it to the real kernel and found the defects below by writing the test first and watching it fail. Each hypothesis is a permanent test (`regress_test.go`, 11 `TestVerify*` tests plus `FuzzNormaliseListedAgainstKernel`). Mutation checks: removing the `hideGen` replan, or removing `pubMu` from `hide`, makes `TestVerifyHideDuringCommitIsNotUndone` and the stress test fail (38 candidate snapshots rejected). Fuzz: 8 s on the new target (1.1 M executions, found V-17 before the fix), 5 s slug, 5 s matcher, clean. Benchmarks re-measured (V-21). Library and platform facts of 12.8 re-checked (module proxy, pkg.go.dev; the obsolete YAML unmarshaler form confirmed in source). Golden outputs regenerated: only the `R205` text changed ("no repository named").

(e) **Testability.** Every defect has a named test; coverage of `internal/catalog` 80.3% (was 78.7%); 46 + 4 test functions, 3 fuzz targets, 2 benchmarks; service tests now run a `memStore` that applies the real `store.ValidateRepo/ValidateItem` rules and a hook that lets a test act in the middle of a store write (this is how V-03 is reproduced deterministically under `testing/synctest`). New specified tests T-17 (ETag versus freshness) and T-18 (anonymous enumeration property over `Slots.Groups`).

(f) **Security / races / lifecycle.** Confidentiality: V-03 (a private repository could reappear after a hide), V-10 (privatized under `installation_scope` removed instead of demoted), V-12 (class filter left to every handler), V-20 (warm start served rows excluded by the tightened policy). Input hygiene: V-16 (http URLs, length), V-17 (name `..` accepted). Lifecycle: V-05 (everything stale after 1.5 x TTL), V-06 (failure invisible to `/status` and readiness), V-08 (75 useless listings during a hold). Lock order is now `commitMu` > `pubMu` > `mu`, documented in 8.2, with the rule that `Tracker.SetRepos` must not call back (D-18).

(g) **Buildability.** An engineer can implement from the document plus the assets: the module builds, the tests run with one command (`README.md`), the tasks of section 13 reference real files (`groups.go` added), and the cross-unit needs name the kernel functions that now exist. What they cannot yet copy: handlers and `Mount`.

## Findings

| Id | Severity | Finding | Evidence | Resolution |
|---|---|---|---|---|
| V-01 | high | Slugs kept GitHub's case and allowed 132 bytes; kernel `scm.Slug` is `^[a-z0-9._-]{1,100}$`, public API `Slug` the same | compile error and failing tests on the real kernel; `scm.ParseSlug` | fixed: `MakeSlug` lower-cases, 100 bytes, kernel is the final check; doc 2.3, 3.1, 4.2, D-4, App. A, schema |
| V-02 | high | Store rows keyed by raw-case `RepoKey`; real store rejects it; `repoMeta` had no owner/name so the display case was lost on restart | `TestVerifyRawCaseKeyIsRejectedByTheRealStore`, `TestVerifyMixedCaseOrganisationPersistsAndWarms` | fixed: `store.KeyOf`, owner/name in meta, warm checks key equals `KeyOf(meta)`; doc 5.2, 4.11 |
| V-03 | high | Event hide during a commit's store write was overwritten by the commit's older plan; concurrent `Apply`s interleaved | `TestVerifyHideDuringCommitIsNotUndone`; mutation: no `pubMu` / no replan fails it | fixed: `pubMu`, `hideGen`, replan under the lock; doc 4.4 R8, 4.8, 8.2 |
| V-04 | medium | `hide` looked up by event name; renamed-not-yet-reconciled repository not found | `TestVerifyHideFindsRenamedRepositoryByID` | fixed: `lookupRef` (source+id, then folded key) |
| V-05 | high | `catalog.repo` `Info.FetchedAt` never refreshed; every unchanged repository `stale` after 1.5 x TTL | `TestVerifyRepoFreshnessIsRefreshedByEveryCompleteListing` | fixed: refresh when older than TTL/2 (bounded revision churn); doc 5.1 |
| V-06 | medium | Failed listing not published; `listed` published as 0 | `TestVerifyStatusShowsFailureAndListedCount` | fixed: `markFailed` + `publishStatus`; D-16; doc 4.4, 8.4 |
| V-07 | medium | R301/R302 findings erased by a commit of another source | review of `buildStatus`; service tests | fixed: findings kept per source |
| V-08 | medium | Held removal re-listed every 2 minutes for the whole hold | `TestVerifyHeldRemovalDoesNotPollEveryTwoMinutes` (75 listings before) | fixed: follow-up only while `0 < Misses < removal_confirmations` |
| V-09 | medium | `catalog.stats` job key from `React` unfolded, never coalesced with the planner's key | `TestVerifyStatsKeyIsFoldedLikeThePlannersKey` | fixed: `RepoRef.KeyParts` |
| V-10 | high | `privatized` under `installation_scope` removed the repository for everyone | `TestVerifyPrivatizedUnderInstallationScopeIsDemotedNotRemoved` | fixed: demote in place, hint remembers the kind; doc 4.8 |
| V-11 | medium | `default_repo` specified as slug; a slug is derived (lower-cased, changes on rename) while the configuration author writes the repository name, so the field must be a name matched case-insensitively | lint golden | fixed: name semantics, C032, R205 text, schema; D-15 |
| V-12 | high | Class-blind membership with per-handler filtering (D-7) although the kernel membership is class-aware | kernel `scm/membership.go`; `TestVerifyAnonymousCardsAreIdenticalToAPublicOnlyDeployment` | fixed: `groups.go` (`Slots.Groups`, `Slots.Group`); D-7 withdrawn; doc 4.6, 7.2, 10.3 |
| V-13 | low | `NewMembership` signature of D-2 does not exist; kernel's takes `MembershipSpec` and returns an error | compile error | fixed: code and doc 3.3, 4.6, 13, D-2 |
| V-14 | medium | D-8 (non-public zero visibility) already done by the kernel (`VisUnknown`); doc still described a fail-open limitation | kernel `scm` | fixed: doc 4.3, 10.2, D-8 |
| V-15 | medium | ETag of `listGroups`/`getGroup` ignored clock-derived freshness; 304 could precede a 400 | reasoning against 06-httpapi (`FreshnessDigest`, `WithDigest`) | fixed in the specification (7.2, 7.3, T-17); code not built (handlers) |
| V-16 | low | `cleanURL` accepted http and unlimited length | API `HttpsUrl` type | fixed |
| V-17 | medium | `normaliseListed` accepted the name `..` | `FuzzNormaliseListedAgainstKernel` | fixed: `RepoRef.Validate` |
| V-19 | low | Stale counts; guardrail rows missing from the trace table | grep of `02-design-guardrails.md` against the trace table | fixed |
| V-20 | high | `Warm` ignored the current visibility policy | `TestVerifyWarmAppliesTheCurrentVisibilityPolicy` | fixed: `Admit` on every warm row; doc 4.11, guarantee 9 |
| V-21 | low | `BenchmarkBuildMembership` generated duplicate slugs the stub accepted | benchmark failed on the real kernel | fixed |

## Unresolved

1. **Handlers, `Mount` and `catalogapi` are not built** (they depend on oapi-codegen output of the `apispec` unit). The ETag/freshness rule (V-15), the 400-before-304 order and tests T-17/T-18 are specification only. Owner: implementer of M4.
2. **T-8, T-9, T-15** (real stores through `storetest`, fake-GitHub fault injection, the request budget test) are not implemented; the service tests use `memStore` with the real validation rules. Owner: M3 implementer.
3. **Zero-visibility trade-off (Q-4):** visibility removal is immediate and unguarded; a listing that returns every repository with an unset visibility would exclude them all at once without the mass-removal guard seeing it. Mitigated by the `github` contract test (every listed repository sets visibility) and by `R301` for absences; needs the owner's decision.
4. **Q-1:** whether a group with no visible member is omitted for every principal (also in a public-only deployment) is for units 07 and 14.
5. Not verifiable offline: the exact `repository.transferred` payload field list on GitHub's documentation (the design needs only `Repo` and `Action`); recorded as fixture test T-16.
