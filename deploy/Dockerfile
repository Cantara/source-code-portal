# Source Code Portal image: node (web bundle) -> golang (static binary) -> distroless Node.js, non-root.
# Base images are pinned by tag and digest; Renovate updates both.
#   docker build -f deploy/Dockerfile --build-arg VERSION=2.0.0 --build-arg BUILD_ID=$(git rev-parse HEAD) -t scp:dev .

FROM node:24.22.2-alpine@sha256:a34ab6d7f6f4813f4d0c5469d74685604b962fbdbf5793a7bc7b4998ecb867c8 AS web
ARG PNPM_VERSION=12.17.0
RUN npm install --global --ignore-scripts pnpm@${PNPM_VERSION}
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,id=pnpm-store,target=/root/.local/share/pnpm/store pnpm install --frozen-lockfile
COPY web/ ./
ARG BUILD_ID=dev
RUN BUILD_ID=${BUILD_ID} pnpm build

FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS go
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

# nodejs24-debian13 carries CA certificates and Node.js for the web server scp supervises.
FROM gcr.io/distroless/nodejs24-debian13:nonroot@sha256:b8cec4b2f2126ff7bd034ec9ac207ffb3272667dfdc48376e3b035d3270549ba
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
