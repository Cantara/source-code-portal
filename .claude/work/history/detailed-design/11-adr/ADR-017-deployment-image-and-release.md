# ADR-017: Deployment image and release

Status: Accepted. Date: 2026-10-14. Refines 04 D-11 (A-10).

## Context
D-11: static `CGO_ENABLED=0` binary, distroless nonroot image by digest, GitHub Actions CI, Renovate, optional cosign. D-16 adds a Node build stage. An image that updates itself to a moving snapshot at start is neither reproducible nor verifiable, so artefacts are immutable and pinned by digest (GR-45).

## Decision
- Multi-stage Dockerfile: `node:24.21.0-alpine@sha256:...` (pnpm 12.10.1, `pnpm build`) -> `golang:1.27.2-alpine@sha256:...` (`CGO_ENABLED=0 go build -tags spa -trimpath -ldflags "-s -w -X main.build=$BUILD_ID"`) -> `gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3` (the distroless README now lists only `-debian13` images; digests read 2026-10-13, re-read at implementation).
- Runtime: UID 65532, read-only root filesystem, writable `/data` volume for SQLite, no shell, `GOMEMLIMIT` at about 85% of the limit, CPU limit (GOMAXPROCS follows cgroups since Go 1.25), `terminationGracePeriodSeconds: 45`, startup probe on `/readyz` with a 10-15 minute budget.
- Release: GoReleaser v2.18.3 builds `linux/{amd64,arm64}` and `darwin/arm64` with the SPA embedded, checksums and a CycloneDX SBOM; optional cosign; the container is built from the same binary. Renovate (not Dependabot: no `tool` directive support) with grouped rules for modernc sqlite/libc and OTel contrib modules.
- Node 24.21 now, 26 LTS after 2026-10-28; Go 1.27.2 (1.25 is unsupported).

## Alternatives considered
`scratch` (no CA bundle or timezone data), alpine runtime (shell and package manager), ko (no Node stage), Debian-based Go image (larger).

## Consequences
The image has no shell, so debugging uses the admin listener and pprof. Digest refresh is a Renovate task.

## Evidence
`05-research/02-go-stack-currency.md` sections 2, 3.9, 7; `05-research/08-frontend-stack.md` section 2.21; Go 1.27.2 release verified 2026-10-14 at https://go.dev/doc/devel/release.

## Amendment 2026-10-14 (review revision)

Tool versions live in the nested module `tools/go.mod`, not in `tool` directives of the root `go.mod`, because a `tool` directive adds the tool dependencies to the product's module graph; Renovate updates the nested module. The image smoke job also asserts that `VACUUM INTO` and a large sort succeed with a read-only root filesystem (`temp_store=MEMORY`).
