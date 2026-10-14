#!/usr/bin/env bash
# CI guard (design 16 section 4.8): the runtime stage of the source-built image (deploy/Dockerfile) and of the release image
# (deploy/Dockerfile.release) must not drift. They differ in exactly the lines that supply the binary.
set -euo pipefail
cd "$(dirname "$0")"

final() {
  # shellcheck disable=SC2016 # the ${TARGETPLATFORM} in the pattern is Dockerfile text, not a shell expansion
  sed -n '/^FROM gcr.io\/distroless/,$p' "$1" |
    grep -v -e '^COPY --from=go ' -e '^COPY --chown=65532:65532 \${TARGETPLATFORM}/scp ' -e '^ARG TARGETPLATFORM$'
}

if diff -u <(final Dockerfile) <(final Dockerfile.release); then
  echo "final stages agree"
else
  echo "deploy/Dockerfile and deploy/Dockerfile.release have diverged below their FROM line" >&2
  exit 1
fi
