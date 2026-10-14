# Architecture decision records

ADR-001 to ADR-025 were written during design and are binding (index: `../README.md` section 5.3; map to code: `../50-repo-seed/design-map.md`).
New decisions are recorded in this directory too, numbered from `026`, one file per decision: `ADR-0NN-short-title.md`, copied from
`ADR-000-template.md`. ADRs are design records: they live here under `.claude/work/`, never in `docs/` (user documentation only).

An ADR is required when a change touches the dependency matrix (`internal/archtest/rules.go`), an HTTP contract guarantee, a persisted format,
a security control, a toolchain pin policy, or when it deviates from `10-architecture.md`, `04-decisions.md` or an existing ADR (then it also says
which text it supersedes). Statuses: Proposed, Accepted, Superseded by ADR-nnn, Rejected. Accepted ADRs are not edited except for the
status line and appended amendments with a date.
