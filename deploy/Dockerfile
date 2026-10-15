# Source Code Portal image: node (web bundle) -> golang (static binary) -> distroless Node.js, non-root.
# Base images are pinned by tag and digest; Renovate updates both.
#   docker build -f deploy/Dockerfile --build-arg VERSION=2.0.0 --build-arg BUILD_ID=$(git rev-parse HEAD) -t scp:dev .

FROM node:24.22.0-alpine@sha256:44fce696f38ba8c987c4db89954051000bbd05ffd8d8659ba15ed4a8602e937f AS web
ARG PNPM_VERSION=12.11.2
RUN npm install --global --ignore-scripts pnpm@${PNPM_VERSION}
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,id=pnpm-store,target=/root/.local/share/pnpm/store pnpm install --frozen-lockfile
COPY web/ ./
ARG BUILD_ID=dev
RUN BUILD_ID=${BUILD_ID} pnpm build

FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS go
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
FROM gcr.io/distroless/nodejs24-debian13:nonroot@sha256:59c479c9b8c123e66047315c968aa2d35e31ae5149c856e4110a79c8b3563f0b
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
