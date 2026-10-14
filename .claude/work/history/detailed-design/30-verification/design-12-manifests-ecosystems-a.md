# Verification report: design unit 12-manifests-ecosystems-a

Document verified: `20-design/12-manifests-ecosystems-a.md` (revision 1 to 1.1, 2026-10-14) and `12-manifests-ecosystems-a.assets/`. Verifier: independent of the author. The same defect table is Appendix F of the document; this report adds method, evidence and the open items. Defect ids are `VF-12-nn`.

Verdict: **ready_with_caveats**. 18 of 21 findings are fixed in the document and the reference code, each pinned by a regression test that fails on the unfixed code (mutation-checked). Three are recorded for other owners (unit 11 and the architecture matrix). The code was rebuilt from scratch with `apply.sh` after the fixes: gofmt clean, `go vet`, `go mod tidy -diff`, purity guard and `go test -race ./internal/manifests/...` all green (Go 1.27.1).

## 1. Method

- Read the whole document (1,570 lines) against the 15 required sections, `04-decisions.md` (D-15, A-15, A-18, A-20, A-26), `10-architecture.md` (5.1 to 5.4, 13, 21, appendix A), ADR-021 and ADR-022, and the core contract `11-manifests-core.md` with its assets.
- `apply.sh` on the unchanged revision: green. Measured imports with `go list` (also `-test`). Extracted `Spec().Diagnostics` of the six packages and diffed with the 92 codes in section 4: identical, severities included.
- Wrote probe trees for every ecosystem, scaling inputs for every format at the size limits, URL redaction, PEP 508 and `ExactPin`, Maven `PomSource` coordinates; 40 s `FuzzParsers` on all six packages (no finding); include and BOM diamonds and a 24-pattern workspace glob.
- Re-fetched versions (proxy.golang.org), purl-spec, PEP 751, the Maven 4.1.0 XSD, `go doc` of go-toml `unstable`.
- Scratch work only under the session scratchpad; other units' documents were not touched.

## 2. Conformance and completeness

- **Imports** equal the table of 2.1. Every edge decreases rank; no ecosystem imports another; only `cmd/scp` and tests import an ecosystem. Gaps against the matrix (5.2): `expand` and `redact` imported `limits` at equal rank (VF-12-11, fixed by ranking them 2), `ecosystems/integration` has no row and imports six adapters in tests (VF-12-12, D-12), the table omits `go.yaml.in/yaml/v3` for npm (13.1 and ADR-021 have it; the document had attributed it to the table).
- **Interfaces** (`Parser`, `Assembler`, `FileSet`, `PomSource`, `VersionedAssembler`) equal architecture 13.2 and doc 11 and are compile-asserted by `surface_test.go`.
- **Sections**: all 15 present and concrete; open points are listed with defaults (OQ-1 to OQ-14). Sections 5, 6, 7 and 9 state "owns nothing" with the consuming root keys listed, which is acceptable. Appendices A to F present.
- **Deviations not recorded by the author, now recorded**: D-12(b) yaml in the matrix row, D-13 `go.sum` not parsed although D-15 lists it, D-14 Maven model built rather than adopting `deps.dev/util/maven` (ADR-021).

## 3. Guardrail traceability

Trace search over `02-design-guardrails.md` and architecture 21 for this unit: present with a mechanism: package manifest intelligence (D-15), GR-03, GR-09, GR-10, GR-13, GR-20, GR-22, GR-23, GR-30, GR-34, GR-04, A-15, A-18, A-26. XML is parsed with `encoding/xml` (no DTD, no external entities) within limits (GR-34), a parse failure is a diagnostic and never logs the document (GR-42), manifests are read at the resolved default branch (GR-10), and workspace globs are typed and anchored (GR-09). **Missing and added**: GR-24 (parse failures retried identically: the never-fail rule), GR-16 (encoding) (VF-12-16).

## 4. Findings

| Id | Sev | Issue | Fixed | Evidence |
|---|---|---|---|---|
| VF-12-01 | high | `tomlx.Decode` quadratic: `unstable.Parser.Shape` rescans the document per key; `Unmarshal` ran before the depth cap. `Cargo.lock` 1 MiB 6.8 s, 2 MiB 27.6 s, 8 MiB 443 s of uninterruptible CPU (watchdog abandons, goroutine runs on). All TOML formats affected | yes (newline table, binary search, walk first): 8 MiB 0.24 s | measured; `TestDecodeIsLinearInTheNumberOfTables`, scale test |
| VF-12-02 | medium | go-toml duplicate-key check quadratic per table: 40,000 keys 1.2 s, 90,000 about 6 s, x40 under `-race` | yes: `MaxKeysPerTable` 10,000 (`limit.nodes`) | `TestKeysPerTableCap` |
| VF-12-03 | high | `pyproject` array element line lookup quadratic: 1 MiB single-line array 11.4 s; also wrong line for substring elements | yes: one-pass index; 87 ms | `TestPyprojectLocatorIsLinear` (old code: 10.8 s, `c:3`) |
| VF-12-04 | medium | `PomSource` coordinates unsanitised (`..`, `?`, `#`, `%`, newline reach the satisfier that builds a URL path) | yes: `safeCoordinate`; task 7.6 | `TestPomSourceCoordinatesAreSafe` |
| VF-12-05 | medium | deadline during the last unit's `PomSource` call published "parent not found" as facts (GR-22: a timeout is a failure, never a result) | yes: final `ctx` check | `TestAssembleReturnsContextErrorAfterAFetchTimeout` |
| VF-12-06 | medium | stored Maven facts unchanged when `pom_source.enabled` toggles (assembler version constant, change detection on tree SHA) | yes: `AssemblerVersion` 1 or `1\|1<<16` | `TestPomSourceEnabledChangesTheRegistryHash` |
| VF-12-07 | medium | Cargo `[workspace.dependencies]` entry with `package = "real"` could not be inherited by key: `cargo.inherit-missing`, version lost | yes: key-based pool, `pool.real:<key>` | golden `workspace-rename` |
| VF-12-08 | low | `redact.URL` missed `//u:p@h`, `;`-separated query secrets, non-first fragment secrets | yes | `redact_test.go` |
| VF-12-09 | low | document promised Maven `ConfHeuristic`; code never emits it | document corrected | grep |
| VF-12-10 | low | 4.7.4 said the Cargo local rename is `TargetDisplay`; code uses the real crate | document corrected | probe |
| VF-12-11 | medium | equal-rank edges `expand`/`redact` to `limits` against architecture 5.1 | yes: rank 2, D-4 rewritten | `go list` |
| VF-12-12 | medium | `ecosystems/integration` has no matrix row, imports six adapters (rule 5.1.3) | no (needs the architecture owner): D-12, task 2.3 | `go list -test` |
| VF-12-13 | low | yaml dependency, `go.sum`, `deps.dev/util/maven` departures from architecture 5.2, D-15, ADR-021 not recorded | recorded as D-12(b), D-13, D-14 | architecture 13.1, ADR-021 |
| VF-12-14 | low | `go-mod.diff` left direct modules `// indirect`; `go mod tidy -diff` not clean | yes | `apply.sh` |
| VF-12-15 | low | `apply.sh` did not fail on unformatted files; purity guard missed `math/rand/v2`, `crypto/rand`, `sync/atomic`, `os/exec`, `net`, `runtime` | yes; planted import caught | `apply.sh` |
| VF-12-16 | low | trace gaps GR-24 (parse failures are not retried identically: the never-fail rule), GR-16 (encoding) | yes | 11.1 |
| VF-12-18 | low | "323 executions" not reproducible (measured 281 six packages, 353 with `integration`, 484 module) | yes | `go test -v` |
| VF-12-19 | low | E-A4 "37 s for a 100,000-segment header" not reproducible with go-toml v2.4.3 (16 ms) | claim withdrawn | probe |
| VF-12-20 | medium | graph-based `PrivacyGuard` is empty on cold start; Maven facts depending on `PomSource` outcome are not re-assembled while the tree is unchanged (`refresh.go` 59 and 72) | **no** (unit 11): RR-A9, OQ-14, L-11, L-12; operator note: set `private_group_prefixes` before enabling `pom_source` | `refresh.go`, `extras.go` |
| VF-12-21 | medium | no test bounded cost at realistic size (fuzz caps 64 KiB; VF-12-01 and 03 passed everything) | yes: `TestInputsAtScaleStayLinear` (18 shapes, 3 s, `-race`), acceptance criterion 8 | mutation: fails on the old `tomlx` |
| VF-12-22 | low | `python.ExactPin` returned whitespace or a marker inside the version | yes | `TestExactPin` |

Scale results (E-A8): all other formats at their limit parse in 0.04 to 0.6 s (8 MiB locks 0.24 to 0.58 s, `gradle.lockfile` 100,000 locked components 0.52 s, 1 MiB manifests 0.04 to 0.18 s); assemblers with 320 workspace members, 4,000 Maven properties and 800 crates under 0.1 s.

## 5. Unresolved and unverified

Unresolved (not fixable inside this unit):

1. VF-12-20 / RR-A9: cold-start behaviour of `Guard().Allow` and re-assembly of repositories with transient `PomSource` failures belong to unit 11 (default: accepted limitation L-11 and L-12 with an operator note).
2. VF-12-12 / D-12: archtest row for `manifests/ecosystems/integration` (or move the tests to `cmd/scp`) and the yaml column of the `manifests/ecosystems/*` row: architecture owner.
3. D-13 (`go.sum` not parsed) and D-14 (Maven model not adopted from deps.dev): owner decisions, defaults stated.

Not verified: the 21 tier-2 oracle files and their counts and timings (not downloaded); the `cmd/scp` composition and the `PomSource` HTTP satisfier (do not exist yet); pnpm lockfile documentation (404, as the author found); Gradle and Maven documentation facts V-6 to V-11 (not re-fetched). Transient versus definitive `PomSource` errors are still one "not found" (the interface has no sentinel); recorded in L-11.

Files changed by the verifier: the document; under `12-manifests-ecosystems-a.assets/`: `apply.sh`, `patches/go-mod.diff`, overlay files of `maven`, `cargo` (and golden `workspace-rename`), `python`, `tomlx`, `redact`, `expand` (comment) and the new `ecosystems/integration/scale_test.go`.
