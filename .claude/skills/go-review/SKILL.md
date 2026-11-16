---
name: go-review
description: Review the Go code of Source Code Portal (SCP) as a whole, one area at a time, not a diff - its correctness, its fit with SCP's rules (read-only GitHub, write-through store, feature boundaries, bounded parsing) and its structure (single responsibility, coupling, size). Use when the owner asks for a full or deep code review, a review of a package or area, an architecture or structure review, or "what should we fix or refactor" in the Go code. Not for reviewing a pull request's diff (use /code-review for that).
---

# Go review of SCP

`/code-review` reviews a change. This skill reviews the code that is there, **one area per run**, so the reader keeps every file of
the area in mind at once. A full pass is the eight areas below, in order. Each run ends in a short report of verified findings,
ranked, with the fix for each.

## The areas

| # | Area | Packages |
|---|---|---|
| 1 | Listing and GitHub | `internal/catalog`, `internal/github` (+ `githubtest`), `internal/prereq` |
| 2 | Engine and data | `internal/sync`, `internal/store`, `internal/snapshot`, `internal/docstore`, `internal/cluster` |
| 3 | Events | `internal/webhook`, `cmd/scp` `inbox.go`, the sink in `serve.go`, `catchup.go` |
| 4 | HTTP, access, agents | `internal/httpapi`, `internal/auth`, `internal/mcpserver`, `internal/tools`, `internal/assistant`, `internal/llm` |
| 5 | Repository reads | `internal/commits`, `internal/pulls`, `internal/issues`, `internal/releases`, `internal/jsonstream`, `internal/actor`, `internal/scope` |
| 6 | Untrusted content | `internal/docs`, `internal/safehtml`, `internal/untrusted`, `internal/egress`, `internal/llms`, `internal/search` |
| 7 | Packages and builds | `internal/packages`, `internal/manifests`, `internal/lru` |
| 8 | Modes, config, wiring | `internal/config`, `internal/people`, `internal/vault`, `internal/setup`, `internal/ghlogin`, `internal/visibility`, `internal/layout`, `internal/announce`, the rest of `cmd/scp` |

The owner may name one area, a package, or "all" (then the next area not reviewed yet, by the dates of the earlier reports).

## Steps

1. **Read the rules first**: `.claude/work/plan.md` (sections 2 and 3), `.claude/work/decisions.md` for the area's decisions, the
   area's note in `.claude/work/design/`, and the code map row of each package in `.claude/CLAUDE.md`. A finding that contradicts a
   recorded decision is a finding about the decision, and says so.
2. **Measure the area** (facts, not opinions):
   - `go vet ./<pkgs>` and `golangci-lint run ./<pkgs>/...` (version 2.15.0; the ceilings are in `.golangci.yml`).
   - `go tool govulncheck ./...` when the area touches a dependency.
   - The import graph: `go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./internal/... ./cmd/...`, filtered to the area.
   - Sizes: lines per file and per package, and the functions closest to the gocyclo and gocognit ceilings (`golangci-lint run
     --enable-only gocyclo` with a lower `min-complexity` through `-E`/a temporary config in the scratchpad, never edited in place).
   - The tests: `go test -race -count=1 -cover ./<pkgs>/...`.
3. **Read every file of the area**, tests included. Read the code, not a summary of it: a subagent may search, but the judgement is
   made on code read in this run.
4. **Look for, in this order**:
   - **Correctness**: wrong results, lost or duplicated work, a state that never heals, an error swallowed or misclassified (the
     engine pauses a source only for errors it sees: `sync.classify`), retries without backoff, a cache or map that grows without
     bound, a goroutine or timer that outlives its owner, a context not passed or not honoured, races (what the mutex guards and what
     escapes it), time compared across clocks.
   - **SCP's rules**: GitHub is read only (P-71); data is a row in the store before it is acknowledged (P-97) and comes back after a
     restart; untrusted bytes pass a bounded parser and `safehtml`/`untrusted`; a private repository never reaches a public view, a
     public registry or a log line; features never import features; request handlers read only the snapshot; no globals or `init()`;
     tests never touch the network, use `goleak` and `synctest`, and wait on events, not sleeps.
   - **Structure**: one job per package and per file; logic in `cmd/scp` that belongs to a feature or to a composition package; the
     generated `oapi` types used beyond a feature's API edge; a dependency pointing from the core to a feature; a file over about 500
     lines that holds more than one job; duplicated code across features.
   - **Simplicity**: code that can be removed, a config key or interface no one needs (plan.md: "keep it small").
5. **Verify each finding before reporting it**: name the file and line, the input or state, and the wrong outcome. For a correctness
   finding write the failing test (in the scratchpad or on a branch) and run it; a finding without a failing test, or a line of code that
   proves it, is reported as *plausible*, never as confirmed. Drop what does not survive.
6. **Rank** most severe first: data loss or a leak of a private repository; then wrong results; then wasted rate limit or work; then
   structure; then simplicity. At most 10 findings a run; more go in one line at the end.
7. **Report**, in simple English, short sentences, one finding per entry: what is wrong, where (`path:line`), how it shows, the fix,
   and *confirmed* or *plausible*. Then the area's measurements in a few lines. Write it outside the repository: the owner's dossier
   (`~/.dossier/<org>/<repo>/reviews/<date>-<area>.md`) when there is one, else the scratchpad, and give the owner the path. The
   repository gets nothing from a review run itself.

## After the report

Fixes are ordinary work: a branch, the failing test first, the fix, the documents and the code map, the full gate
(`make GOLANGCI_LINT_VERSION=2.15.0 check`), one PR. A structure finding that moves code changes no behaviour and goes in its own PR.
Record a decision that changes a rule in `.claude/work/decisions.md`.
