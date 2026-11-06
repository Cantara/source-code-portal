# Source Code Portal image: node (web bundle) -> golang (static binary) -> distroless Node.js, non-root.
# Base images are pinned by tag and digest; Renovate updates both.
#   docker build -f deploy/Dockerfile --build-arg VERSION=2.0.0 --build-arg BUILD_ID=$(git rev-parse HEAD) -t scp:dev .

FROM node:26.13.0-alpine@sha256:8996bb5821d62a8fd9bb27c1cb07462f3fdce6e47d640c6a881ead0b064ce216 AS web
ARG PNPM_VERSION=12.17.0
RUN npm install --global --ignore-scripts pnpm@${PNPM_VERSION}
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm-store,target=/root/.local/share/pnpm/store pnpm install --frozen-lockfile
COPY web/ ./
ARG BUILD_ID=dev
RUN BUILD_ID=${BUILD_ID} pnpm build

FROM golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS go
WORKDIR /src
ENV GOFLAGS=-mod=readonly CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,id=go-mod,target=/go/pkg/mod go mod download && go mod verify
COPY . .
ARG BUILD_ID=dev
ARG VERSION=dev
RUN --mount=type=cache,id=go-mod,target=/go/pkg/mod --mount=type=cache,id=go-build,target=/root/.cache/go-build \
    go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=${VERSION} -X main.build=${BUILD_ID}" -o /out/scp ./cmd/scp \
 && /out/scp version

# nodejs26-debian13 carries CA certificates and Node.js for the web server scp supervises.
FROM gcr.io/distroless/nodejs26-debian13:nonroot@sha256:9b945e92c1b5463a4b75f11e8417515e4bea7355f6adfd79ec220fbbe905dda8
ARG BUILD_ID=dev
ARG VERSION=dev
LABEL org.opencontainers.image.title="Source Code Portal" \
      org.opencontainers.image.description="Organisational dashboard for GitHub repositories" \
      org.opencontainers.image.source="https://github.com/Cantara/source-code-portal" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${BUILD_ID}"
COPY --from=go /out/scp /scp
COPY --from=web /src/web/bundle /web
ENV SCP_WEB_DIR=/web SCP_WEB_NODE=/nodejs/bin/node
# /data is the one writable path (the SQLite file). distroless has no shell, so the directory is created by copying a file into it.
COPY --chown=65532:65532 <<EOF /data/.keep
EOF
USER 65532:65532
WORKDIR /data
EXPOSE 9090 9091
VOLUME ["/data"]
ENTRYPOINT ["/scp"]
CMD ["serve"]
