# ADR-0NN: Title as a decision, not a topic

Status: Proposed | Accepted | Superseded by ADR-nnn | Rejected. Date: YYYY-MM-DD. Refines or supersedes: ADR-nnn / 04 D-nn / architecture section n.n (or "none").
Deciders: names or roles.

## Context
The forces at play, in a few sentences: what changed or what was found, which requirement or defect (`GR-nn`, `F-nn`, `W-nn`, `VF-nn-nn`) it
touches, and the constraint that makes the decision necessary now. Facts carry a date and a source (primary documentation, a local run).

## Decision
What we will do, in the imperative, numbered if there are several parts. Name the packages, files, config keys or routes affected. If the
dependency matrix changes, quote the changed rows of `internal/archtest/rules.go`.

## Alternatives considered
Each real alternative in one or two lines, with the reason it lost. "Do nothing" is an alternative.

## Consequences
What becomes easier, what becomes harder, what must change elsewhere (tests, CI, generated files, documentation), and the exit condition if
the decision has one.

## Evidence
Measurements, prototypes, verified facts with the verification date, links to `.claude/work/plans/` sections or issues. Nothing here is recalled
from memory for fast-moving libraries.
