Reference code of design unit 10 (docs, pages, media). Verified and corrected 2026-10-14 (see section 16 of ../10-docs-pages-media.md).

What is here
  internal/docs/                    package docs (descriptor loader, Resolve, Discover/Glob/Order/Admit, codec, React, config; the docs.refresh run loop,
                                    RenderPage, raw handlers and the API handlers are STUBS: they are specified in section 4 of the design)
  internal/docs/internal/render/    package render: the complete Markdown/bridge pipeline, shape guard, highlight budgets, normaliser, tests, fuzz targets
  internal/docs/schema/             scp-v1.json, frontmatter-v1.json and the embed package
  internal/media/                   package media: Signer, Canonical, Sniff, LRU, flight, External(), Repo(), config, tests (complete)
  internal/pages/                   package pages: Load, Reload, announcement, config, tests (complete except the generated strict-server handler)
  cmd/scp/assert_test.go            composition test: the unit satisfies the consumer interfaces of the real job/readmodel/store/httpapi
  testdata/md, testdata/bridge      13 real READMEs (public repositories, kept for measurement only), one real GitHub-rendered
                                    AsciiDoc page and one hand-built page in the same format (synthetic_billing-service.html)
  safehtml-D2-D3.patch              the three regular-expression changes of deviations D-2/D-3 against 01-kernel.assets/internal/safehtml/policy.go
  assemble.sh                       builds a complete scratch module from this directory and the reference code of units 01, 02, 03 and 06

How to build and test
  ./assemble.sh /path/to/scratch
  cd /path/to/scratch && GOFLAGS=-mod=mod go vet ./internal/docs/... ./internal/media ./internal/pages ./cmd/...
  GOFLAGS=-mod=mod go test -race ./internal/docs/... ./internal/media ./internal/pages ./cmd/...
  GOFLAGS=-mod=mod go test -run 'Worst|Budget|Scaling' ./internal/docs/internal/render      # adversarial timing tests: skipped under -race

The first test file of the unit the engineer should read is internal/docs/internal/render/render_test.go; the sibling code is copied by assemble.sh, not kept here.
