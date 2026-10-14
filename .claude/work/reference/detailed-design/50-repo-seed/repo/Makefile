# Makefile of Source Code Portal (design 16 section 4.6). Works with the GNU make 3.81 that macOS ships: no .ONESHELL, no
# grouped targets, no $(file ...). Every target is a thin wrapper over one command that CI also runs, so "make X" locally and
# the CI step of the same name cannot drift (CI calls these targets; see .github/workflows/ci.yml).
#
#   make help        list targets
#   make check       what a pull request must pass, minus browsers and containers (about the CI go + api + web jobs)
#   make build       pnpm build, then the single static binary with the SPA embedded -> bin/scp
#   make dev         fake GitHub + portal + Vite dev server (three processes, Ctrl-C stops all)

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c   # honoured by GNU make 4+; make 3.81 ignores it, so no recipe relies on pipefail
.DEFAULT_GOAL := help

# ---- versions: ONE place per tool; Renovate updates these through the customManagers in renovate.json ----------------
GOLANGCI_LINT_VERSION := 2.14.0
GORELEASER_VERSION    := 2.18.3
PNPM_VERSION          := 12.10.1

# ---- build parameters ------------------------------------------------------------------------------------------------
GO       ?= go
TOOLS    := $(GO) tool -modfile=tools/go.mod
# The build id is the git SHA: the Go binary reports it (main.build), the SPA sends it as X-SCP-Build (BUILD_ID) so a
# stale browser tab can tell it is talking to a newer server. Outside git (a source tarball) it is "dev".
BUILD_ID ?= $(shell git rev-parse --verify -q HEAD 2>/dev/null || echo dev)
VERSION  ?= $(patsubst v%,%,$(shell git describe --tags --always --dirty 2>/dev/null || echo dev))
LDFLAGS  := -s -w -X main.version=$(VERSION) -X main.build=$(BUILD_ID)
IMAGE    ?= scp:dev
PNPM     := pnpm -C web

export BUILD_ID   # web/ reads BUILD_ID (vite.config.ts, scripts/finalize.ts): one name for the git SHA on both sides

.PHONY: help
help: ## list the targets
	@grep -E '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/:.*## /\t/' | sort | awk -F'\t' '{printf "  %-16s %s\n", $$1, $$2}'

# ---- tools --------------------------------------------------------------------------------------------------------------
.PHONY: tools
tools: ## verify the nested tools module (oapi-codegen, govulncheck, go-licenses) and print the binaries' versions
	cd tools && $(GO) mod verify
	$(TOOLS) govulncheck -version
	$(TOOLS) oapi-codegen --version

.PHONY: need-golangci
need-golangci:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint $(GOLANGCI_LINT_VERSION) is not installed: https://golangci-lint.run/welcome/install/ (binary release, not 'go install')"; exit 1; }
	@golangci-lint version 2>&1 | grep -q "version $(GOLANGCI_LINT_VERSION) " || { echo "golangci-lint is not $(GOLANGCI_LINT_VERSION): $$(golangci-lint version 2>&1 | head -1)"; exit 1; }

# ---- generate ---------------------------------------------------------------------------------------------------------
.PHONY: gen
gen: ## go generate ./..., pnpm gen (OpenAPI bundle, TS types, route tree) and THIRD_PARTY.md; generated source is committed
	$(GO) generate ./...
	$(PNPM) install --frozen-lockfile
	$(PNPM) gen
	$(MAKE) third-party

.PHONY: gen-check
gen-check: gen ## fail when generation changed anything (CI drift check; untracked files count)
	@if [ -n "$$(git status --porcelain --untracked-files=all -- . ':!web/dist' ':!bin' ':!dist')" ]; then \
		echo "generated files are out of date; run 'make gen' and commit:"; git status --short -- . ':!web/dist' ':!bin' ':!dist'; exit 1; fi

# ---- build --------------------------------------------------------------------------------------------------------------
.PHONY: web
web: ## pnpm install + pnpm build -> web/dist (hashed assets, .br/.gz, routes.json)
	$(PNPM) install --frozen-lockfile
	$(PNPM) build

.PHONY: build
build: web ## the release shape: SPA embedded, CGO off, static, trimmed -> bin/scp
	CGO_ENABLED=0 $(GO) build -tags spa -trimpath -ldflags "$(LDFLAGS)" -o bin/scp ./cmd/scp
	$(GO) vet -tags spa ./web/...

.PHONY: build-go
build-go: ## Go only, placeholder SPA (no Node needed): what 'go test ./...' sees -> bin/scp-placeholder
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/scp-placeholder ./cmd/scp

.PHONY: build-fakegithub
build-fakegithub: ## the fake GitHub server for development and end-to-end tests -> bin/fakegithub
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/fakegithub ./cmd/fakegithub

# ---- test and lint --------------------------------------------------------------------------------------------------
.PHONY: test
test: test-go test-web ## everything that needs no browser and no container

.PHONY: test-go
test-go: ## go test -race ./... (cgo on for the race detector only; the release build is cgo-free)
	CGO_ENABLED=1 $(GO) test -race -count=1 ./...

.PHONY: test-web
test-web: ## vitest
	$(PNPM) install --frozen-lockfile
	$(PNPM) test

.PHONY: e2e
e2e: build build-fakegithub ## Playwright against the real -tags spa binary and the fake GitHub
	$(PNPM) exec playwright install --with-deps chromium
	$(PNPM) e2e

.PHONY: lint
lint: need-golangci vet ## golangci-lint (generated depguard included) and go vet
	golangci-lint run ./...

.PHONY: vet
vet: ## go vet with the default tags, and with the spa tag when web/dist has been built (the tag needs the bundle to embed)
	$(GO) vet ./...
	@if [ -d web/dist ]; then $(GO) vet -tags spa ./web/...; else echo "web/dist is not built: spa-tag vet skipped (make build vets it)"; fi

.PHONY: lint-web
lint-web: ## dependency pins, tsc, eslint (boundaries), prettier --check
	$(PNPM) install --frozen-lockfile
	$(PNPM) check-pins
	$(PNPM) typecheck
	$(PNPM) lint
	$(PNPM) format:check

.PHONY: arch
arch: ## closed-world import matrix, rank proof, depguard drift, source rules (ADR-018)
	$(GO) test -count=1 ./internal/archtest/...

.PHONY: arch-fix
arch-fix: ## regenerate the depguard block of .golangci.yml from internal/archtest/rules.go
	$(GO) test -count=1 ./internal/archtest -run TestDepguardInSync -update

# The contract gates of design 07 section 12: redocly lint + drift (pnpm api:lint, api:bundle-check), apicheck, oasdiff against the
# last release. API_BASE is a git ref whose bundled documents are the compatibility baseline (CI passes the base branch on a PR
# and the previous tag on a release; with no tag yet there is nothing to compare and the breaking check is skipped).
API_BASE ?= $(shell git describe --tags --abbrev=0 --match 'v*' 2>/dev/null)
API_DOCS := internal/apispec/openapi.bundled.yaml internal/apispec/openapi.admin.bundled.yaml

.PHONY: api-check
api-check: ## OpenAPI: redocly lint, bundle drift, apicheck rules, breaking changes against API_BASE
	$(PNPM) install --frozen-lockfile
	$(PNPM) run api:lint
	$(PNPM) run api:bundle-check
	cd tools && $(GO) run ./apicheck $(addprefix ../,$(API_DOCS))
	@if [ -z "$(API_BASE)" ]; then echo "no release tag yet: skipping oasdiff"; else \
	  for d in $(API_DOCS); do \
	    tmp=$$(mktemp -d); \
	    if git show "$(API_BASE):$$d" > "$$tmp/base.yaml" 2>/dev/null; then \
	      $(TOOLS) oasdiff breaking "$$tmp/base.yaml" "$$d" --flatten-allof --severity-levels api/oasdiff-severity.txt --fail-on ERR || exit 1; \
	    else echo "$$d is new since $(API_BASE)"; fi; \
	  done; fi

.PHONY: vuln
vuln: ## govulncheck on the source (the nightly job also scans the released binary)
	$(TOOLS) govulncheck ./...

.PHONY: licenses
licenses: ## licence allow-list over the dependency closure of cmd/scp (THIRD_PARTY.md is generated from the same data)
	$(TOOLS) go-licenses check ./cmd/scp --allowed_licenses=Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC,MIT,MPL-2.0,0BSD,Unlicense

# THIRD_PARTY.md ships in every release archive. Go part: go-licenses over the dependency closure of cmd/scp (the module's own
# packages are ignored); npm part: the production dependencies of the embedded bundle (pnpm 12 `licenses list --json` is a map
# licence -> packages, read with jq). `gen` runs it, so `gen-check` fails a pull request that changed dependencies and not the notice.
.PHONY: third-party
third-party: ## regenerate THIRD_PARTY.md (Go modules and the npm packages embedded in the SPA)
	$(TOOLS) go-licenses report ./cmd/scp --ignore github.com/cantara/source-code-portal --template tools/third_party.tmpl > THIRD_PARTY.md
	@printf '\n## npm packages of the embedded single-page application\n\n| Package | Version | Licence |\n|---|---|---|\n' >> THIRD_PARTY.md
	$(PNPM) install --frozen-lockfile
	$(PNPM) licenses list --prod --json | jq -r 'to_entries | sort_by(.key)[] | .key as $$l | (.value | sort_by(.name))[] | "| `\(.name)` | \(.versions | join(", ")) | \($$l) |"' >> THIRD_PARTY.md

FUZZTIME ?= 30s
SHARDS   ?= 1
SHARD    ?= 0
# "dir Target" per line in a stable order; tools/, .claude/ (the design package), node_modules and .git hold no product fuzz targets.
FUZZ_LIST = grep -rl --include='*_test.go' '^func Fuzz' . | grep -v -e '^\./tools/' -e '^\./\.claude/' -e '/node_modules/' -e '^\./\.git/' | sort -u | xargs -n1 dirname | sort -u | while read -r dir; do \
	  grep -h '^func Fuzz' $$dir/*_test.go | sed -E 's/^func (Fuzz[A-Za-z0-9_]*).*/\1/' | sort -u | while read -r t; do echo "$$dir $$t"; done; done

.PHONY: fuzz-list
fuzz-list: ## print "dir target" for every Fuzz target (one per line, stable order)
	@$(FUZZ_LIST)

.PHONY: fuzz
fuzz: ## run the Fuzz targets of shard SHARD of SHARDS for FUZZTIME each (go test -fuzz accepts one target per invocation)
	@set -e; { $(FUZZ_LIST); } | awk -v n=$(SHARDS) -v s=$(SHARD) '(NR - 1) % n == s' | while read -r dir t; do \
	  echo "== $$dir $$t"; $(GO) test -run '^$$' -fuzz "^$$t$$" -fuzztime $(FUZZTIME) $$dir; done

.PHONY: modverify
modverify:
	$(GO) mod verify
	@test -z "$$($(GO) mod tidy -diff 2>&1)" || { echo "go.mod/go.sum are not tidy: run 'go mod tidy'"; exit 1; }

.PHONY: config-lint
config-lint: build-go ## every shipped example must lint (scp config lint --strict) and the schema must not drift
	./bin/scp-placeholder config lint --strict deploy/dev.yaml $(wildcard docs/examples/*.yaml)
	./bin/scp-placeholder config schema | diff -u docs/reference/config.schema.json -

# SEED STAGING (design 16 section 4.12): `check` is the design's list minus the two targets whose subject does not exist yet: `config-lint`
# joins when `scp config lint` exists (unit 02, task B-4) and `api-check` when `tools/apicheck` exists (unit 07, step 4).
.PHONY: check
check: gen-check modverify lint arch test lint-web vuln licenses ## the pull-request gate, locally (browsers and containers excluded)

# ---- image ------------------------------------------------------------------------------------------------------------
.PHONY: image
image: ## multi-stage image (node -> golang -> distroless) tagged $(IMAGE)
	docker build -f deploy/Dockerfile --build-arg BUILD_ID=$(BUILD_ID) --build-arg VERSION=$(VERSION) -t $(IMAGE) .

.PHONY: smoke
smoke: ## run deploy/smoke.sh against $(IMAGE): read-only, non-root, /livez, /readyz, SPA served, no shell
	deploy/smoke.sh $(IMAGE)

# ---- release (local rehearsal; the real one runs in .github/workflows/release.yml) --------------------------------------------
.PHONY: release-snapshot
release-snapshot: web ## goreleaser build + archives + checksums + SBOMs without publishing or signing -> dist/
	command -v goreleaser >/dev/null || { echo "goreleaser $(GORELEASER_VERSION) is not installed"; exit 1; }
	goreleaser release --snapshot --clean --skip=publish,sign,docker,docker_manifest

.PHONY: release-check
release-check: ## goreleaser check (schema and semantic validation of .goreleaser.yaml)
	goreleaser check

# ---- development --------------------------------------------------------------------------------------------------------
# Three processes: the fake GitHub (testdata/worlds/demo on 127.0.0.1:9191), the portal (:9090 public, :9091 admin) and Vite
# (:5173, which proxies /api /badges /media /llms.txt /schemas to :9090). `make -j3` runs them side by side; Ctrl-C stops all three.
# The portal is built with the `devtools` tag: only that build lets its SSRF policy reach a loopback fake (internal/platform/netguard
# denies loopback otherwise, even with allow_private_addresses). Release and image builds never use the tag (check-binary.sh).
DEMO := testdata/worlds/demo

.PHONY: dev-keys
dev-keys: ## demo App key, webhook secret and admin token for the fake world (git-ignored; throwaway values, files are world-readable on purpose)
	@test -f $(DEMO)/app.pem || $(GO) run ./cmd/fakegithub keygen --out $(DEMO)
	@test -f $(DEMO)/webhook-secret || printf 'dev-webhook-secret' > $(DEMO)/webhook-secret
	@test -f $(DEMO)/admin-token || od -An -N16 -tx1 /dev/urandom | tr -d ' \n' > $(DEMO)/admin-token
	@chmod 0644 $(DEMO)/app.pem $(DEMO)/webhook-secret $(DEMO)/admin-token

.PHONY: dev
dev: dev-keys ## fake GitHub + portal + Vite dev server
	$(MAKE) -j3 dev-fake dev-scp dev-web

.PHONY: dev-fake
dev-fake:
	$(GO) run ./cmd/fakegithub serve --world $(DEMO) --listen 127.0.0.1:9191 --path-prefix /api/v3 --control --webhook-target http://127.0.0.1:9090/webhooks/github/acme --webhook-secret dev-webhook-secret

.PHONY: dev-scp
dev-scp:
	mkdir -p .dev
	SCP_GITHUB_SOURCES_0_AUTH_PRIVATE_KEY_FILE=$(CURDIR)/$(DEMO)/app.pem \
	SCP_GITHUB_SOURCES_0_WEBHOOK_SECRET=dev-webhook-secret \
	$(GO) run -tags devtools ./cmd/scp serve --config deploy/dev.yaml

.PHONY: dev-web
dev-web:
	$(PNPM) install --frozen-lockfile
	$(PNPM) dev

.PHONY: dev-stack
dev-stack: dev-keys ## the same stack in containers (the portal IMAGE, no devtools tag): docker compose up --build
	docker compose -f deploy/compose.dev.yaml up --build

.PHONY: clean
clean: ## remove build outputs (never source or generated source)
	rm -rf bin dist web/dist .dev
