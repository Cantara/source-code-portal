# Workflows of later bootstrap steps

`.github/workflows/` holds what is active at the current step: `ci.yml` (jobs `go` and `gate`) and a `nightly.yml` stub. This directory
holds the complete files of design 16 section 4.7 and 4.9, **inactive** (GitHub runs only `.github/workflows/`), with every action
already pinned to a full commit SHA. Renovate maintains them here too (`renovate.json`, `github-actions.managerFilePatterns`).

| Step (design 16 section 4.12) | Subject lands | Move into `ci.yml` | `gate.needs` |
|---|---|---|---|
| 1 (this seed) | skeleton, archtest | `go`, `gate` | `[go]` |
| 2 | first `go:generate` (`gen-check` already runs locally) | job `gen` | `[go, gen]` |
| 4 | units 06-08: contract | job `api` | `+ api` |
| 6 | units 17-18: `web/` | jobs `web`, `build` | `+ web, build` |
| 7 | image, smoke, compose | jobs `image`, `e2e` | `+ image, e2e` |
| 8 | GoReleaser | copy `release.yml` to `.github/workflows/` | n/a |

Nightly: copy a job from `nightly.full.yml` into `nightly.yml` when its subject exists (`fuzz` with the first `Fuzz*` test, `api-extended`
and `lighthouse` with the image, `canary` with the manifests unit). Before moving a job, run `actionlint` on the result.
