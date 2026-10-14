# Reference prototype for design unit 08 (catalog)

Throw-away, compile-checked and tested reference code for `20-design/08-catalog.md`. It is NOT the shipped code: the implementation
copies `mod/internal/catalog/**` into `internal/catalog` of the real module. Since the verification of 2026-10-14 the module carries
the **real** kernel and storage packages of units 01 and 03 (`mod/internal/{scm,freshness,job,readmodel,reactor,routes,store}` and `httpapi/mux.go`),
no stubs; only `httpapi` is partial (the handlers and `Mount` are not built, `catalogapi` does not exist yet).
`mod/internal/crosscheck` stands in for the tests that live in `cmd/scp` (they need both the YAML library and the catalog packages).

    cd mod && go vet ./... && go test -count=1 -race ./...                   # about 45 s (the sqlite store suite dominates)
    go test -run xxx -bench BuildMembership -benchmem ./internal/catalog
    go test -run xxx -fuzz FuzzMatcherCompile -fuzztime 8s ./internal/catalog
    go test -run xxx -fuzz FuzzNormaliseListedAgainstKernel -fuzztime 8s ./internal/catalog
    go run ./cmd/lintdemo ../fixtures/example.catalog.yaml ../fixtures/example-repos.json > ../out/example.lint.txt

Layout

| Path | Content |
|---|---|
| `catalog.schema.json` | the hand-written JSON Schema 2020-12 of the `catalog:` block (also embedded by `mod/internal/catalog/schema.go`) |
| `fixtures/example-repos.json` | 16 synthetic public repositories of an organisation `acme` in the shape of `GET /orgs/{org}/repos` (id, name, visibility, archived, fork, default_branch, topics, license); it includes a dotted name, a leading-underscore name, a fork, two archived repositories and a non-`main` default branch |
| `fixtures/example.catalog.yaml` | an example `catalog:` block with five groups and four deliberate mistakes for the lint demonstration |
| `out/example.lint.txt` | `Lint` output for the example catalogue against the example repositories (compared byte for byte by `TestExampleCatalogLintGolden`) |

Verified 2026-10-14 with go1.27.1 darwin/arm64, go.yaml.in/yaml/v3 v3.0.5, github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 (tests only),
go.opentelemetry.io/otel{,/metric,/trace} v1.47.0.
