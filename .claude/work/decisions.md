# Decision log

Owner and agent decisions taken while bringing the design package to a stable baseline, newest last. Each entry names where the decision is
recorded with binding force (usually `plans/04-decisions.md` section 7) and the follow-up edits it requires in lower-ranked documents. The
owner's standing instruction (2026-10-14): the agent decides as it goes and reports; decisions are not put to the owner as questions.

| Date | ID | Decision | Recorded in | Follow-up |
|---|---|---|---|---|
| 2026-10-14 | — | Repository is `git@github.com:Cantara/source-code-portal.git`, branch `main`; module `github.com/cantara/source-code-portal`, image `ghcr.io/cantara/source-code-portal` (all `descoped` identity strings replaced). `Cantara/SourceCodePortal` (Java) is read-only reference | `.claude/CLAUDE.md`; seed notes | done |
| 2026-10-14 | — | Layout: planning and design under `.claude/work/`; `docs/` is user documentation only. The seed is laid into this repository in place (no design mirror); `bootstrap.sh` is a copy with one overwrite check | `.claude/CLAUDE.md`; `plans/50-repo-seed/README.md` (S-12 superseded) | done |
| 2026-10-14 | A-34 | CLI on Cobra, configuration loaded with Viper (one instance per run, `UnmarshalExact`, schema validation kept) | `plans/04-decisions.md` A-34; `plans/10-architecture.md` Appendix A | design 02 (`platform/config` loader, env naming), design 16 (CLI on Cobra; archtest drops the `cobra` ban, adds owner rules for `cobra` and `viper`), `90-config-reference.md` (env names stay `SCP_<PATH>`), seed `internal/archtest/rules.go` + `make arch-fix`, `10-architecture.md` sections 16 and 19 and the matrix rows of 5.2, research 02 note |
| 2026-10-14 | A-35 | Hazelcast is the distributed-state backend (Redis not supported); v1 stays single-replica; the optional Hazelcast adapter lands with multi-replica in v1.1 | `plans/04-decisions.md` A-35; `plans/10-architecture.md` Appendix A | ADR-026 (interfaces, package row, config keys) before v1.1; `40-implementation-plan.md` v1.1 scope; README section 4 scope line |
