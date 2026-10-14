# Source Code Portal. Every target is one command that CI runs the same way. `make help` lists them.
.DEFAULT_GOAL := help
SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

GOLANGCI_LINT_VERSION := 2.14.0
PNPM ?= pnpm
VERSION ?= dev
CONFIG ?= scp.yaml
BUILD_ID ?= $(shell git rev-parse HEAD 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.build=$(BUILD_ID)

.PHONY: help web build gen lint test check image run dev clean need-golangci

help: ## list targets
	@grep -hE '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-8s %s\n", $$1, $$2}'

web: ## build the web UI into web/dist
	$(PNPM) -C web install --frozen-lockfile
	$(PNPM) -C web build

build: web ## build bin/scp with the web UI embedded
	CGO_ENABLED=0 go build -tags spa -trimpath -ldflags "$(LDFLAGS)" -o bin/scp ./cmd/scp

gen: ## regenerate generated sources
	go generate ./...

need-golangci:
	@golangci-lint version --short 2>/dev/null | grep -qx '$(GOLANGCI_LINT_VERSION)' \
		|| { echo "golangci-lint $(GOLANGCI_LINT_VERSION) is required (https://golangci-lint.run/docs/welcome/install/)"; exit 1; }

lint: need-golangci ## vet, golangci-lint, govulncheck, eslint, tsc, prettier
	go vet ./...
	golangci-lint run
	go tool govulncheck ./...
	$(PNPM) -C web lint
	$(PNPM) -C web typecheck
	$(PNPM) -C web format:check

test: ## Go tests with the race detector, web tests
	CGO_ENABLED=1 go test -race -count=1 -timeout 10m ./...
	$(PNPM) -C web test

check: gen ## the gate: generated code current, modules tidy, lint, test
	git diff --exit-code
	go mod tidy -diff
	$(MAKE) lint test

image: ## build the container image scp:dev
	docker build -f deploy/Dockerfile --build-arg VERSION=$(VERSION) --build-arg BUILD_ID=$(BUILD_ID) -t scp:dev .

run: build ## build and serve with scp.yaml (a copy of deploy/config.example.yaml; CONFIG= overrides)
	bin/scp serve --config $(CONFIG)

dev: ## serve the API on :9090 and the web UI with hot reload on :5173
	go run ./cmd/scp serve --config $(CONFIG) & \
	trap 'kill $$!' EXIT; $(PNPM) -C web dev

clean: ## remove build output
	rm -rf bin dist web/dist
