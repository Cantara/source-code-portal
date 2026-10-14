## What and why

<!-- One logical change. Link the design unit and task (for example "unit 09, task 3") or the issue. -->

## Checklist

- [ ] `make check` passes locally (at the current bootstrap step).
- [ ] Generated files are regenerated and committed (`make gen`; CI runs `make gen-check`).
- [ ] New or moved package: row in `internal/archtest/rules.go`, `make arch-fix`, `doc.go` updated.
- [ ] No feature imports another feature; any new edge in the matrix is justified below and recorded in an ADR if it changes the architecture.
- [ ] Config key, metric, error code or route added: its catalogue or table is updated in the same change.
- [ ] Tests added or changed with the behaviour; no `t.Skip` without an issue number, no `TODO` without `(#123)`.
- [ ] Docs and `CHANGELOG.md` updated when behaviour visible to operators or users changes.

## Deviations from the design (if any)

<!-- "None", or: which document and section, why, the evidence, and the owner who decides. Never diverge silently. -->
