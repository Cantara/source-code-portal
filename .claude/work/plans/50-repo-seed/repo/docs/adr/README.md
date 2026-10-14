# Architecture decision records

ADR-001 to ADR-025 were written during design and are binding; they live in `docs/design/11-adr/` (index in `docs/design/README.md`).
New decisions are recorded here, numbered from `026`, one file per decision: `0NN-short-title.md`, copied from `0000-template.md`.

An ADR is required when a change touches the dependency matrix (`internal/archtest/rules.go`), an HTTP contract guarantee, a persisted format,
a security control, a toolchain pin policy, or when it deviates from `10-architecture.md`, `04-decisions.md` or an existing ADR (then it also says
which text it supersedes). Statuses: Proposed, Accepted, Superseded by ADR-nnn, Rejected. Accepted ADRs are not edited except for the
status line and appended amendments with a date.
