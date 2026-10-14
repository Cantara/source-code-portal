# Source Code Portal image: node (SPA) -> golang (binary) -> distroless static, nonroot. Design 16 section 4.8.
#
# Built by `make image` and by the CI image job on every pull request. The release workflow builds its image from the
# GoReleaser binary instead (deploy/Dockerfile.release) and runs the same deploy/smoke.sh against it before promoting it.
# Every base image is pinned by tag AND digest: the tag says what it is, the digest makes it immutable (GR-45). Renovate
# updates both together (renovate.json, "docker" manager); never edit one without the other.
#
#   docker build -f deploy/Dockerfile --build-arg BUILD_ID=$(git rev-parse HEAD) --build-arg VERSION=1.2.3 -t scp:dev .

# ---- 1. SPA --------------------------------------------------------------------------------------------------------------
# pnpm is installed from the registry at an exact version: corepack is not shipped from Node 25 on, so it is not relied upon.
FROM node:24.21.0-alpine@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1 AS web
ARG PNPM_VERSION=12.10.1
RUN npm install --global --ignore-scripts pnpm@${PNPM_VERSION}
WORKDIR /src/web
# Dependencies first: this layer is rebuilt only when the manifest, the lockfile or the pnpm policy change.
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/.node-version ./
RUN --mount=type=cache,id=pnpm-store,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ ./
# The build id reaches the bundle (X-SCP-Build skew check) and the Go binary below: one value, the git SHA.
ARG BUILD_ID=dev
ENV VITE_BUILD_ID=${BUILD_ID}
RUN pnpm build

# ---- 2. binary -----------------------------------------------------------------------------------------------------------
FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS go
WORKDIR /src
ENV GOFLAGS=-mod=readonly CGO_ENABLED=0
COPY go.mod go.sum ./
RUN --mount=type=cache,id=go-mod,target=/go/pkg/mod go mod download && go mod verify
COPY . .
# web/dist is built in stage 1 and never committed; the `spa` tag embeds it. -trimpath and no VCS stamping make the output
# a function of the source, the toolchain and these flags only.
COPY --from=web /src/web/dist ./web/dist
ARG BUILD_ID=dev
ARG VERSION=dev
RUN --mount=type=cache,id=go-mod,target=/go/pkg/mod --mount=type=cache,id=go-build,target=/root/.cache/go-build \
    go build -tags spa -trimpath -buildvcs=false -ldflags "-s -w -X main.version=${VERSION} -X main.build=${BUILD_ID}" -o /out/scp ./cmd/scp \
 && /out/scp version

# ---- 3. runtime ----------------------------------------------------------------------------------------------------------
# static-debian13: CA certificates, no shell, no package manager; tzdata is NOT needed (the binary embeds time/tzdata).
# Everything below this line is identical to deploy/Dockerfile.release except the one line that supplies the binary
# (deploy/check-final-stage.sh diffs them in CI).
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
ARG BUILD_ID=dev
ARG VERSION=dev
LABEL org.opencontainers.image.title="Source Code Portal" \
      org.opencontainers.image.description="Dashboard and documentation portal for the repositories of a GitHub organisation" \
      org.opencontainers.image.source="https://github.com/Cantara/source-code-portal" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${BUILD_ID}"
COPY --from=go --chown=65532:65532 /out/scp /scp
# /data is the only writable path: the SQLite file and its WAL (a local or block volume, never NFS, EFS or SMB). distroless has
# no shell, so the directory is created by copying a file into it; the --chown applies to the directory too.
COPY --chown=65532:65532 <<EOF /data/.keep
EOF
USER 65532:65532
WORKDIR /data
EXPOSE 9090 9091
VOLUME ["/data"]
# No HEALTHCHECK instruction: Kubernetes uses httpGet probes on /livez and /readyz. For plain Docker or Compose use
#   healthcheck: { test: ["CMD", "/scp", "healthcheck"], ... }   (scp healthcheck probes http://127.0.0.1:9090/livez)
ENTRYPOINT ["/scp"]
CMD ["serve"]
