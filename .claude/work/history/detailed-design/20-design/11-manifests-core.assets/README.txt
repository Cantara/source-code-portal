Reference implementation scaffold for design unit 11-manifests-core (NON-NORMATIVE; the design document 11-manifests-core.md wins on any difference).
Module: github.com/cantara/source-code-portal, go 1.27. Built and tested on go1.27.1 darwin/arm64 on 2026-10-14:
  go vet ./...   and   go test -race -count=1 ./...   (all green; go mod tidy -diff clean)
REAL code (owned by this unit, lift it into the repository at milestone M1):
  internal/manifests  internal/manifests/internal/{purl,version,limits}  internal/manifests/parsertest
STAND-IN for generated code (the oapi-codegen output of unit 07's contract is the real thing):
  internal/manifests/manifestsapi
STUBS (signatures copied from the sibling designs, NOT the real packages; other units own them):
  internal/scm  internal/freshness  internal/job  internal/store  internal/readmodel  internal/httpapi  internal/httpapi/authz  internal/httpapi/openapi
Run the measurements quoted in the design:
  go test -run TestResidentHeap -v ./internal/manifests/
  go test -run xxx -bench . -benchmem ./internal/manifests/
  go test -fuzz FuzzNormalizeIdempotent -fuzztime 30s ./internal/manifests/internal/purl

Independent verification (2026-10-14, see ../11-manifests-core.md section 17 and 30-verification/design-11-manifests-core.md):
  The reference code was changed by the verifier (VF-01..VF-21); tests: 97 (80 original plus 17 regression tests in internal/manifests/verify_test.go).
  verification/ holds NON-COMPILED files (extension .txt or .diff, so that go vet ./... ignores them): the checks that were run against the REAL
  sibling packages (kernel, readmodel/store, httpapi/authz, platform/config, apitest) in an overlay copy of this module:
    assertchk_a_test.go.txt     compile-time proofs: manifests.Store is satisfied by store/sqlite and store/memory, Service.React is a reactor.Func
    assertchk_desc_test.go.txt  config.Describe(manifests.Config) is accepted (every key has jsonschema_description)
    assertchk_load_test.go.txt  config.Load end to end: defaults round trip, comma-separated list variables, scp:min/max and Validate paths
    api_test_contract_validation.diff  hook that validates every /api/v1 response of the API tests against the bundled OpenAPI of unit 07
                                       (kin-openapi through apitest.ValidateResponse; the SBOM is skipped: no decoder for application/vnd.cyclonedx+json)
  To repeat: copy internal/manifests into a checkout of the repository once units 01, 03, 06, 07 exist, put the .txt files under internal/assertchk/ as .go, apply the diff.

